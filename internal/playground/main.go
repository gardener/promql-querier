// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

// Command playground brings up the shared harness fleet and a real bin/promql-querier
// process against it, prints the URLs, and blocks until interrupted. It does not build
// promql-querier.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/gardener/promql-querier/internal/harness"
)

const (
	promqlQuerierAddr = "127.0.0.1:55590"
	referencePort     = 55595
	readinessLimit    = 10 * time.Second
)

var downstreamPorts = [4]int{55591, 55592, 55593, 55594}

type config struct {
	Endpoints []endpoint `yaml:"endpoints"`
}

type endpoint struct {
	URL    string            `yaml:"url"`
	Labels map[string]string `yaml:"labels"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "playground: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	root := harness.RepoRoot()

	promqlQuerierBinary := filepath.Join(root, "bin", "promql-querier")
	if _, err := os.Stat(promqlQuerierBinary); err != nil {
		return fmt.Errorf("promql-querier binary not found at %s", promqlQuerierBinary)
	}

	prometheusBinary := filepath.Join(root, "tmp", "bin", "prometheus")
	if _, err := os.Stat(prometheusBinary); err != nil {
		return fmt.Errorf("prometheus binary not found at %s", prometheusBinary)
	}

	playgroundDir := filepath.Join(root, "tmp", "playground")
	harn, err := harness.Start(playgroundDir, prometheusBinary, downstreamPorts, referencePort)
	if err != nil {
		return err
	}
	defer harn.Stop()

	endpointsFile := filepath.Join(playgroundDir, "endpoints.yaml")
	if err := writeEndpointsFile(endpointsFile, harn); err != nil {
		return err
	}

	promqlQuerierLog := filepath.Join(playgroundDir, "promql-querier.log")
	promqlQuerier, logFile, err := startPromqlQuerier(promqlQuerierBinary, endpointsFile, promqlQuerierLog)
	if err != nil {
		return err
	}
	defer func() {
		harness.Terminate(promqlQuerier)
		_ = logFile.Close()
	}()

	if !harness.WaitForReady("http://"+promqlQuerierAddr+"/-/ready", readinessLimit) {
		return fmt.Errorf("promql-querier at %s did not become ready within %s", promqlQuerierAddr, readinessLimit)
	}

	printURLs(harn, promqlQuerierLog)

	<-sig
	fmt.Printf("\nShutting down...\n")
	return nil
}

func writeEndpointsFile(path string, h *harness.Harness) error {
	cfg := config{}
	for _, inst := range h.Instances {
		vl := make(map[string]string, len(h.VirtualLabels[inst.URL]))
		for name, value := range h.VirtualLabels[inst.URL] {
			if value != "" {
				vl[name] = value
			}
		}
		cfg.Endpoints = append(cfg.Endpoints, endpoint{URL: inst.URL, Labels: vl})
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal endpoints config: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write endpoints config: %w", err)
	}

	return nil
}

func startPromqlQuerier(binary, endpointsFile, logPath string) (*os.Process, *os.File, error) {
	logFile, err := os.Create(logPath)
	if err != nil {
		return nil, nil, fmt.Errorf("create promql-querier log file: %w", err)
	}

	cmd := exec.Command(binary,
		"--config.endpoints-file", endpointsFile,
		"--server.listen-address", promqlQuerierAddr,
		"--log.level", "debug",
	)

	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return nil, nil, fmt.Errorf("start promql-querier: %w", err)
	}

	return cmd.Process, logFile, nil
}

func printURLs(h *harness.Harness, promqlQuerierLog string) {
	fmt.Printf("\nThe PromQL Querier playground is running. Press Ctrl+C to stop.\n\n")

	fmt.Printf("  promql-querier           http://%s\n\n", promqlQuerierAddr)
	for _, inst := range h.Instances {
		vl := make([]string, 0, len(h.VirtualLabels[inst.URL]))
		for name, value := range h.VirtualLabels[inst.URL] {
			vl = append(vl, fmt.Sprintf("%s=%s", name, value))
		}
		slices.Sort(vl)
		fmt.Printf("  prometheus (downstream)  %s  %s\n", inst.URL, strings.Join(vl, "\t"))
	}

	fmt.Printf("\n  prometheus (reference)   %s\n", h.Reference.URL)
	fmt.Printf("\n  promql-querier log       %s\n", promqlQuerierLog)
}
