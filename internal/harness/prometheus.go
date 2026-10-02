// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package harness

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const prometheusReadinessTimeout = 30 * time.Second

func closeQuietly(c io.Closer) {
	_ = c.Close()
}

// PrometheusInstance is a running Prometheus process started for tests.
type PrometheusInstance struct {
	URL     string
	process *os.Process
	logFile *os.File
}

// Stop terminates the Prometheus process and closes its log file.
func (p *PrometheusInstance) Stop() {
	if p == nil {
		return
	}
	Terminate(p.process)
	closeQuietly(p.logFile)
}

// StartPrometheus launches a Prometheus process serving dataDir.
func StartPrometheus(dataDir, binary string, port int) (*PrometheusInstance, error) {
	configPath := filepath.Join(dataDir, "prometheus.yml")
	_, err := os.Create(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create prometheus config file: %w", err)
	}

	logPath := filepath.Join(dataDir, "prometheus.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create log file: %w", err)
	}

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	cmd := exec.Command(binary,
		"--config.file", configPath,
		"--storage.tsdb.path", dataDir,
		"--web.listen-address", addr,
		"--enable-feature", "promql-experimental-functions",
	)

	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return nil, fmt.Errorf("failed to start prometheus: %w", err)
	}

	if !isPrometheusReady(addr) {
		Terminate(cmd.Process)
		_ = logFile.Close()
		return nil, fmt.Errorf("prometheus at %s did not become ready within %s", addr, prometheusReadinessTimeout)
	}

	return &PrometheusInstance{
		URL:     "http://" + addr,
		process: cmd.Process,
		logFile: logFile,
	}, nil
}

func isPrometheusReady(addr string) bool {
	return WaitForReady("http://"+addr+"/-/ready", prometheusReadinessTimeout)
}
