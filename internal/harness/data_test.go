// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package harness

import (
	"math/rand"
	"slices"
	"testing"

	promparser "github.com/prometheus/prometheus/promql/parser"
)

func parseDesc(t *testing.T, desc string) (map[string]string, int64) {
	t.Helper()
	parser := promparser.NewParser(promparser.Options{})
	lbls, values, err := parser.ParseSeriesDesc(desc)
	if err != nil {
		t.Fatalf("invalid series descriptor %q: %v", desc, err)
	}
	return lbls.Map(), int64(len(values))
}

func TestGenerateInfo(t *testing.T) {
	var samples int64 = 5

	gotLabels, gotSamples := parseDesc(t, generateInfo("kube_pod_info", "cluster-10", samples))
	if gotSamples != samples {
		t.Errorf("want %d samples, got %d", samples, gotSamples)
	}
	if gotLabels["__name__"] != "kube_pod_info" || gotLabels["cluster"] != "cluster-10" {
		t.Errorf("unexpected labels: %v", gotLabels)
	}
}

func TestGenerateCounter(t *testing.T) {
	var samples int64 = 5

	rng := rand.New(rand.NewSource(1))
	gotLabels, gotSamples := parseDesc(t, generateCounter(rng, "cpu_total", "backend", "cluster-10", samples))
	if gotSamples != samples {
		t.Errorf("want %d samples, got %d", samples, gotSamples)
	}
	if gotLabels["__name__"] != "cpu_total" || gotLabels["container"] != "backend" || gotLabels["cluster"] != "cluster-10" {
		t.Errorf("unexpected labels: %v", gotLabels)
	}
}

func TestGenerateGauge(t *testing.T) {
	var samples int64 = 5

	rng := rand.New(rand.NewSource(1))
	gotLabels, gotSamples := parseDesc(t, generateGauge(rng, "mem_bytes", "backend", "cluster-10", samples))
	if gotSamples != samples {
		t.Errorf("want %d samples, got %d", samples, gotSamples)
	}
	if gotLabels["__name__"] != "mem_bytes" || gotLabels["container"] != "backend" || gotLabels["cluster"] != "cluster-10" {
		t.Errorf("unexpected labels: %v", gotLabels)
	}
}

func TestGenerateUnitGauge(t *testing.T) {
	var samples int64 = 5

	rng := rand.New(rand.NewSource(1))
	gotLabels, gotSamples := parseDesc(t, generateUnitGauge(rng, "correlation", "backend", "cluster-10", samples))
	if gotSamples != samples {
		t.Errorf("want %d samples, got %d", samples, gotSamples)
	}
	if gotLabels["__name__"] != "correlation" || gotLabels["container"] != "backend" || gotLabels["cluster"] != "cluster-10" {
		t.Errorf("unexpected labels: %v", gotLabels)
	}
}

func TestGenerateClassicHistogram(t *testing.T) {
	var samples int64 = 5

	rng := rand.New(rand.NewSource(1))
	descs := generateClassicHistogram(rng, "request_duration_seconds", "cluster-10", samples)

	// Six buckets (0.1, 0.5, 1, 2.5, 5, +Inf) plus _count and _sum.
	const wantSeries = 6 + 2
	if len(descs) != wantSeries {
		t.Fatalf("want %d series, got %d", wantSeries, len(descs))
	}

	var gotLe []string
	nameCounts := map[string]int{}
	for _, desc := range descs {
		gotLabels, gotSamples := parseDesc(t, desc)
		if gotSamples != samples {
			t.Errorf("descriptor %q: want %d samples, got %d", desc, samples, gotSamples)
		}
		if gotLabels["cluster"] != "cluster-10" {
			t.Errorf("descriptor %q: unexpected cluster %q", desc, gotLabels["cluster"])
		}

		nameCounts[gotLabels["__name__"]]++
		if gotLabels["__name__"] == "request_duration_seconds_bucket" {
			gotLe = append(gotLe, gotLabels["le"])
		}
	}

	wantLe := []string{"0.1", "0.5", "1.0", "2.5", "5.0", "+Inf"}
	if !slices.Equal(gotLe, wantLe) {
		t.Errorf("bucket le values: want %v, got %v", wantLe, gotLe)
	}
	if nameCounts["request_duration_seconds_count"] != 1 {
		t.Errorf("want exactly one _count series, got %d", nameCounts["request_duration_seconds_count"])
	}
	if nameCounts["request_duration_seconds_sum"] != 1 {
		t.Errorf("want exactly one _sum series, got %d", nameCounts["request_duration_seconds_sum"])
	}
}

func TestGenerateData(t *testing.T) {
	var samples int64 = 5
	const clusters = 2
	descs := generateSeries(1, 0, clusters, samples)
	if len(descs) == 0 {
		t.Fatal("generateData returned no series")
	}
	for _, desc := range descs {
		parseDesc(t, desc)
	}
}
