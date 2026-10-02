// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

//go:build compare

package compare_test

import (
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/prometheus/promql"

	"github.com/gardener/promql-querier/fanout"
	"github.com/gardener/promql-querier/internal/harness"
	"github.com/gardener/promql-querier/labels"
	"github.com/gardener/promql-querier/plan"
	"github.com/gardener/promql-querier/server"
	"github.com/gardener/promql-querier/test/compare"
)

type suite struct {
	comparator    *compare.Compare
	promqlQuerier *httptest.Server
	harness       *harness.Harness
}

var s *suite

func TestMain(m *testing.M) {
	var err error
	s, err = setup()
	if err != nil {
		if s != nil {
			s.teardown()
		}
		log.Fatalf("setup: %v", err)
	}

	code := m.Run()
	s.teardown()
	os.Exit(code)
}

func setup() (*suite, error) {
	s := &suite{}

	root := harness.RepoRoot()
	suiteDir := filepath.Join(root, "tmp", "compare")

	prometheusBinary := filepath.Join(root, "tmp", "bin", "prometheus")
	if _, err := os.Stat(prometheusBinary); err != nil {
		return s, fmt.Errorf("prometheus binary not found at %s", prometheusBinary)
	}

	var downstreamPorts [4]int
	for i := range downstreamPorts {
		port, err := freePort()
		if err != nil {
			return s, fmt.Errorf("allocate downstream port: %w", err)
		}
		downstreamPorts[i] = port
	}

	referencePort, err := freePort()
	if err != nil {
		return s, fmt.Errorf("allocate reference port: %w", err)
	}

	harn, err := harness.Start(suiteDir, prometheusBinary, downstreamPorts, referencePort)
	if err != nil {
		return s, err
	}

	s.harness = harn

	virtualLabelSets := labels.ExpandLabelSets(harn.VirtualLabels)
	virtualLabelNames := labels.UnionLabelNames(virtualLabelSets)

	downstreams, err := fanout.BuildDownstreams(virtualLabelSets, 0)
	if err != nil {
		return s, fmt.Errorf("build downstreams: %w", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	processorOpts := fanout.ProcessorDefaultOpts()
	engineOpts := fanout.EngineDefaultOpts()
	engineOpts.Logger = logger

	planner := plan.NewPlanner(virtualLabelSets, virtualLabelNames, plan.WithPlannerLogger(logger))
	processor := fanout.NewProcessor(slog.Default(), promql.NewEngine(engineOpts), planner, downstreams, processorOpts)
	metadata := fanout.NewMetadata(slog.Default(), downstreams, virtualLabelNames, 0)
	config := server.NewConfig(downstreams)

	srv := server.New(slog.Default(), processor, metadata, config)
	s.promqlQuerier = httptest.NewServer(srv.Handler())

	promqlQuerierClient, err := api.NewClient(api.Config{Address: s.promqlQuerier.URL})
	if err != nil {
		return s, fmt.Errorf("create promql-querier api client: %w", err)
	}

	referenceClient, err := api.NewClient(api.Config{Address: harn.Reference.URL})
	if err != nil {
		return s, fmt.Errorf("create reference api client: %w", err)
	}

	s.comparator = compare.NewCompare(promv1.NewAPI(referenceClient), promv1.NewAPI(promqlQuerierClient))

	return s, nil
}

func (s *suite) teardown() {
	if s.promqlQuerier != nil {
		s.promqlQuerier.Close()
	}
	if s.harness != nil {
		s.harness.Stop()
	}
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("failed to allocate free port: %w", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port, nil
}
