// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

//go:build compare

package compare_test

import (
	"testing"
	"time"
)

func TestSeries(t *testing.T) {
	start := time.UnixMilli(s.harness.EvalTimeMs - 10*60*1000)
	end := time.UnixMilli(s.harness.EvalTimeMs)

	tests := []struct {
		name     string
		matchers []string
	}{
		{name: "filtered by metric", matchers: []string{`container_memory_working_set_bytes`}},
		{name: "filtered by metric and label", matchers: []string{`container_memory_working_set_bytes{container="backend"}`}},
		{name: "union of virtual labels", matchers: []string{`container_memory_working_set_bytes{region="eu"}`, `container_memory_working_set_bytes{region="us"}`}},
		{name: "all series", matchers: []string{`{__name__!=""}`}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s.comparator.CompareSeries(t, tc.matchers, start, end)
		})
	}
}

func TestSeriesError(t *testing.T) {
	start := time.UnixMilli(s.harness.EvalTimeMs - 10*60*1000)
	end := time.UnixMilli(s.harness.EvalTimeMs)

	tests := []struct {
		name     string
		matchers []string
	}{
		{name: "no match", matchers: nil},
		{name: "empty selector", matchers: []string{`{}`}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s.comparator.CompareSeriesError(t, tc.matchers, start, end)
		})
	}
}

func TestLabelNames(t *testing.T) {
	start := time.UnixMilli(s.harness.EvalTimeMs - 10*60*1000)
	end := time.UnixMilli(s.harness.EvalTimeMs)

	tests := []struct {
		name     string
		matchers []string
	}{
		{name: "all", matchers: nil},
		{name: "filtered by metric", matchers: []string{`container_memory_working_set_bytes`}},
		{name: "virtual label empty on routed partition", matchers: []string{`{env="staging",region=""}`}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s.comparator.CompareLabelNames(t, tc.matchers, start, end)
		})
	}
}

func TestLabelValues(t *testing.T) {
	start := time.UnixMilli(s.harness.EvalTimeMs - 10*60*1000)
	end := time.UnixMilli(s.harness.EvalTimeMs)

	tests := []struct {
		name     string
		label    string
		matchers []string
	}{
		{name: "virtual label values", label: "env", matchers: nil},
		{name: "label values", label: "container", matchers: nil},
		{name: "metric name values", label: "__name__", matchers: nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s.comparator.CompareLabelValues(t, tc.label, tc.matchers, start, end)
		})
	}
}
