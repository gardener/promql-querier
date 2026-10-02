// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

// Package harness provisions a Prometheus fleet with synthetic data the PromQL Querier
// can be configured with to try it out locally or run tests. It deploys four downstream
// Prometheus instances and one reference instance. The reference instance holds
// the same series as the downstreams, but with virtual labels baked in.
package harness

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gardener/promql-querier/labels"
)

// Harness holds the running Prometheus fleet: the downstream instances, the reference
// instance, the virtual label sets keyed by downstream URL, and the evaluation time
// and step the synthetic data was generated with.
type Harness struct {
	Instances     []*PrometheusInstance
	Reference     *PrometheusInstance
	VirtualLabels map[string]labels.LabelSet
	EvalTimeMs    int64
}

// Start provisions the fleet under dir using the Prometheus binary set in the arguments.
// dir is removed and recreated so each run starts from a clean state.
func Start(dir, binary string, downstreamPorts [4]int, referencePort int) (h *Harness, err error) {
	h = &Harness{
		EvalTimeMs: time.Now().UnixMilli(),
	}

	// Any early return with a non-nil error stops whatever was started so far.
	defer func() {
		if err != nil {
			h.Stop()
			h = nil
		}
	}()

	var stepMs int64 = 60_000

	// 20 days of data. 10 days ahead of the evaluation time, 10 days behind.
	numIntervals := 10 * 24 * 60 * 60 * 1000 / stepMs
	blockMaxMs := h.EvalTimeMs + numIntervals*stepMs
	blockSamples := 2 * numIntervals

	_ = os.RemoveAll(dir)

	referenceDir := filepath.Join(dir, "prometheus-reference")
	if err = os.MkdirAll(referenceDir, 0o755); err != nil {
		return nil, fmt.Errorf("create dir %s: %w", referenceDir, err)
	}

	h.Instances = make([]*PrometheusInstance, 0, len(downstreams))
	h.VirtualLabels = make(map[string]labels.LabelSet, len(downstreams))
	for i, ds := range downstreams {
		series := generateSeries(ds.seed, i, ds.clusters, blockSamples)

		downstreamDir := filepath.Join(dir, "prometheus-"+ds.name)
		if err := os.MkdirAll(downstreamDir, 0o755); err != nil {
			return nil, fmt.Errorf("create dir %s: %w", downstreamDir, err)
		}

		if err := WriteBlock(downstreamDir, blockMaxMs, stepMs, map[string]string{}, series...); err != nil {
			return nil, fmt.Errorf("WriteBlock %s: %w", ds.name, err)
		}
		if err := WriteBlock(referenceDir, blockMaxMs, stepMs, ds.labels, series...); err != nil {
			return nil, fmt.Errorf("WriteBlock reference %s: %w", ds.name, err)
		}

		prom, err := StartPrometheus(downstreamDir, binary, downstreamPorts[i])
		if err != nil {
			return nil, fmt.Errorf("start %s prometheus: %w", ds.name, err)
		}
		h.Instances = append(h.Instances, prom)
		h.VirtualLabels[prom.URL] = ds.labels
	}

	prom, err := StartPrometheus(referenceDir, binary, referencePort)
	if err != nil {
		return nil, fmt.Errorf("start reference prometheus: %w", err)
	}
	h.Reference = prom

	return h, nil
}

// Stop kills every downstream instance and the reference.
func (h *Harness) Stop() {
	for _, inst := range h.Instances {
		inst.Stop()
	}
	h.Reference.Stop()
}

// RepoRoot returns the repository root by walking up from the current working
// directory until it finds the directory containing go.mod. It panics if no
// such directory exists.
func RepoRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			panic("could not find repository root (go.mod)")
		}
		dir = parent
	}
}

// WaitForReady polls url every 100ms until it returns HTTP 200 or timeout elapses.
func WaitForReady(url string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// Terminate sends SIGTERM, waits up to a short grace period for the
// process to exit, then sends SIGKILL if it is still alive. It always waits so
// the process is reaped rather than left as a zombie or orphan holding its port.
func Terminate(process *os.Process) {
	if process == nil {
		return
	}

	_ = process.Signal(syscall.SIGTERM)

	done := make(chan struct{})
	go func() {
		_, _ = process.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = process.Kill()
		<-done
	}
}
