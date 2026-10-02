// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

//go:build compare

package compare_test

import "testing"

func TestRangeQueries(t *testing.T) {
	var (
		tenHoursMs int64 = 10 * 60 * 60 * 1000

		start = s.harness.EvalTimeMs - tenHoursMs
		end   = s.harness.EvalTimeMs
		step  = 100
	)

	queries := []struct {
		name  string
		query string
	}{
		{name: "selector with virtual filter", query: `container_memory_working_set_bytes{env="prod"}`},
		{name: "aggregation over rate", query: `sum by (env) (rate(container_cpu_usage_seconds_total[5m]))`},
		{name: "binary arithmetic", query: `sum by (env) (container_memory_working_set_bytes) / count by (env) (container_memory_working_set_bytes)`},
		{name: "subquery pushdown fetched as instant", query: `rate(count(container_cpu_usage_seconds_total)[5m:])`},
		{name: "delta from range start via @ start()", query: `container_memory_working_set_bytes - container_memory_working_set_bytes @ start()`},
		{name: "delta from range start via @ end()", query: `container_memory_working_set_bytes - container_memory_working_set_bytes @ end()`},
		{name: "start scalar builtin", query: `container_memory_working_set_bytes - start()`},
		{name: "end scalar builtin", query: `end() - container_memory_working_set_bytes`},
		{name: "step scalar builtin", query: `container_memory_working_set_bytes * 0 + step()`},
		{name: "range scalar builtin", query: `container_memory_working_set_bytes * 0 + range()`},
	}

	for _, tc := range queries {
		t.Run(tc.name, func(t *testing.T) {
			s.comparator.CompareQueryRange(t, tc.query, start, end, step)
		})
	}
}
