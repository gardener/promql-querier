// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

//go:build compare

package compare_test

import (
	"testing"

	"github.com/gardener/promql-querier/test/compare"
)

func TestAbsent(t *testing.T) {
	tests := []struct {
		name  string
		query string
		opts  []compare.Option
	}{
		{name: "absent existing metric", query: `absent(container_memory_working_set_bytes)`, opts: []compare.Option{compare.ShouldBeEmpty()}},
		{name: "absent nonexistent metric", query: `absent(nonexistent)`},
		{name: "absent existing with virtual label", query: `absent(container_memory_working_set_bytes{env="prod"})`, opts: []compare.Option{compare.ShouldBeEmpty()}},
		{name: "absent existing with nonexistent virtual label", query: `absent(container_memory_working_set_bytes{env="dev"})`},
		{name: "absent nonexistent with virtual label", query: `absent(nonexistent{env="prod"})`},
		{name: "absent nonexistent with real label", query: `absent(nonexistent{container="backend"})`},
		{name: "absent nonexistent with both labels", query: `absent(nonexistent{container="backend", env="prod"})`},
		{name: "absent_over_time existing metric", query: `absent_over_time(container_memory_working_set_bytes[5m])`, opts: []compare.Option{compare.ShouldBeEmpty()}},
		{name: "absent_over_time nonexistent metric", query: `absent_over_time(nonexistent[5m])`},
		{name: "absent_over_time existing with virtual label", query: `absent_over_time(container_memory_working_set_bytes{env="prod"}[5m])`, opts: []compare.Option{compare.ShouldBeEmpty()}},
		{name: "absent_over_time nonexistent with virtual label", query: `absent_over_time(nonexistent{env="prod"}[5m])`},
		{name: "absent_over_time nonexistent with real label", query: `absent_over_time(nonexistent{container="backend"}[5m])`},
		{name: "absent over a function collapses to an existence group", query: `absent(rate(container_memory_working_set_bytes[5m]))`, opts: []compare.Option{compare.ShouldBeEmpty()}},
		{name: "absent_over_time over a subquery collapses to absent over present_over_time", query: `absent_over_time(rate(container_memory_working_set_bytes[5m])[10m:])`, opts: []compare.Option{compare.ShouldBeEmpty()}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s.comparator.CompareQuery(t, tc.query, s.harness.EvalTimeMs, tc.opts...)
		})
	}
}
