// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

//go:build compare

package compare_test

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestTimeModifier(t *testing.T) {
	queries := []struct {
		name  string
		query string
	}{
		{name: "matrix selector", query: `container_memory_working_set_bytes[5m]`},
		{name: "subquery selector", query: `container_memory_working_set_bytes[5m:30s]`},
		{name: "at start on vector selector", query: `container_memory_working_set_bytes @ start()`},
		{name: "at end on vector selector", query: `container_memory_working_set_bytes @ end()`},
		{name: "at one day ago on vector selector", query: `container_memory_working_set_bytes @ <1d-ago>`},
		{name: "at start on matrix selector", query: `container_cpu_usage_seconds_total{container="backend"}[5m] @ start()`},
		{name: "at end on matrix selector", query: `container_cpu_usage_seconds_total{container="backend"}[5m] @ end()`},
		{name: "at one day ago on matrix selector", query: `container_cpu_usage_seconds_total{container="backend"}[5m] @ <1d-ago>`},
		{name: "at start on subquery selector", query: `container_cpu_usage_seconds_total{container="backend"}[5m:30s] @ start()`},
		{name: "at end on subquery selector", query: `container_cpu_usage_seconds_total{container="backend"}[5m:30s] @ end()`},
		{name: "at one day ago inside subquery selector", query: `container_memory_working_set_bytes[5m:30s] @ <1d-ago>`},
		{name: "offset on vector selector", query: `container_memory_working_set_bytes offset 1d`},
		{name: "offset on matrix selector", query: `container_cpu_usage_seconds_total{container="backend"}[5m] offset 1d`},
		{name: "offset on subquery selector", query: `container_memory_working_set_bytes[5m:30s] offset 1d`},
		{name: "offset and at on vector selector", query: `container_memory_working_set_bytes offset 1d @ <1d-ago>`},
		{name: "offset and at on matrix selector", query: `container_memory_working_set_bytes[5m] offset 1d @ <1d-ago>`},
		{name: "offset and at on subquery selector", query: `container_memory_working_set_bytes[5m:30s] offset 1d @ <1d-ago>`},
		{name: "at end under pushed down aggregation", query: `sum by (env) (container_memory_working_set_bytes @ end())`},
	}

	for _, tc := range queries {
		t.Run(tc.name, func(t *testing.T) {
			oneDayAgoSec := strconv.FormatInt((s.harness.EvalTimeMs-(24*time.Hour).Milliseconds())/1000, 10)
			query := strings.ReplaceAll(tc.query, "<1d-ago>", oneDayAgoSec)
			s.comparator.CompareQuery(t, query, s.harness.EvalTimeMs)
		})
	}
}
