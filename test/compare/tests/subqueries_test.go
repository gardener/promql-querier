// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

//go:build compare

package compare_test

import "testing"

func TestSubqueries(t *testing.T) {
	queries := []struct {
		name  string
		query string
	}{
		{name: "subquery avg", query: `avg_over_time(container_memory_working_set_bytes[10m:1m])`},
		{name: "subquery matrix result", query: `container_memory_working_set_bytes[5m:]`},
		{name: "nested subquery range combination", query: `min_over_time(sum(container_cpu_usage_seconds_total)[1h:])[10m:]`},
		{name: "nested subquery before pushdown boundary", query: `sum_over_time(avg_over_time(sum(container_cpu_usage_seconds_total)[307s:23s])[610s:61s])`},
	}

	for _, tc := range queries {
		t.Run(tc.name, func(t *testing.T) {
			s.comparator.CompareQuery(t, tc.query, s.harness.EvalTimeMs)
		})
	}
}
