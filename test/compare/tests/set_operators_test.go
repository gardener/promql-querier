// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

//go:build compare

package compare_test

import (
	"testing"

	"github.com/gardener/promql-querier/test/compare"
)

func TestSetOperators(t *testing.T) {
	tests := []struct {
		name  string
		query string
		opts  []compare.Option
	}{
		{name: "or same metric all engines", query: `container_memory_working_set_bytes{container="backend"} or container_memory_working_set_bytes{container="frontend"}`},
		{name: "or different virtual labels", query: `container_memory_working_set_bytes{env="prod"} or container_memory_working_set_bytes{env="staging"}`},
		{name: "or different virtual labels different metrics", query: `container_memory_working_set_bytes{env="prod"} or container_cpu_usage_seconds_total{env="staging"}`},
		{name: "or ignoring virtual label", query: `container_memory_working_set_bytes{env="prod"} or ignoring(env) container_memory_working_set_bytes{env="staging"}`},
		{name: "unless same virtual labels", query: `container_memory_working_set_bytes{env="prod"} unless container_memory_working_set_bytes{env="prod"}`, opts: []compare.Option{compare.ShouldBeEmpty()}},
		{name: "unless different virtual labels", query: `container_memory_working_set_bytes{env="prod"} unless container_memory_working_set_bytes{env="staging"}`},
		{name: "unless ignoring virtual label", query: `container_memory_working_set_bytes{env="prod"} unless ignoring(env) container_memory_working_set_bytes{env="staging"}`},
		{name: "and same virtual label", query: `container_memory_working_set_bytes{env="prod"} and container_memory_working_set_bytes{env="prod"}`},
		{name: "and different virtual labels", query: `container_memory_working_set_bytes{env="prod"} and container_memory_working_set_bytes{env="staging"}`, opts: []compare.Option{compare.ShouldBeEmpty()}},
		{name: "and ignoring virtual label", query: `container_memory_working_set_bytes{env="prod"} and ignoring(env, cluster) container_memory_working_set_bytes{env="staging"}`},
		{name: "and with vector lhs", query: `vector(1) and container_memory_working_set_bytes`, opts: []compare.Option{compare.ShouldBeEmpty()}},
		{name: "and with vector rhs", query: `container_memory_working_set_bytes and vector(1)`, opts: []compare.Option{compare.ShouldBeEmpty()}},
		{name: "or with vector no phantom duplication", query: `container_memory_working_set_bytes{env="prod", region="eu"} or vector(1)`},
		{name: "or with vector lhs", query: `vector(1) or container_memory_working_set_bytes`},
		{name: "unless with vector lhs", query: `vector(1) unless container_memory_working_set_bytes`},
		{name: "unless with vector rhs", query: `container_memory_working_set_bytes unless vector(1)`},
		{name: "unless with vector rhs filtered", query: `container_memory_working_set_bytes{env="prod"} unless vector(1)`},
		{name: "and same engines no split", query: `container_memory_working_set_bytes{container="backend"} and container_memory_working_set_bytes{container="frontend"}`, opts: []compare.Option{compare.ShouldBeEmpty()}},
		{name: "unless same engines no split", query: `container_memory_working_set_bytes{container="backend"} unless container_memory_working_set_bytes{container="frontend"}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s.comparator.CompareQuery(t, tc.query, s.harness.EvalTimeMs, tc.opts...)
		})
	}
}
