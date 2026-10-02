// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

//go:build compare

package compare_test

import (
	"testing"

	"github.com/gardener/promql-querier/test/compare"
)

func TestBinaryOperators(t *testing.T) {
	tests := []struct {
		name  string
		query string
		opts  []compare.Option
	}{
		{name: "division same metric", query: `container_memory_working_set_bytes{env="prod"} / container_memory_working_set_bytes{env="prod"}`},
		{name: "group_left on job and env", query: `container_memory_working_set_bytes{env="prod"} * on (container, env, region) group_left () sum by (container, env, region) (container_cpu_usage_seconds_total{env="prod"})`},
		{name: "group_left on virtual label", query: `container_memory_working_set_bytes * on (container, env, region) group_left () sum by (container, env, region) (container_cpu_usage_seconds_total)`},
		{name: "group_right on virtual label", query: `sum by (container, env, region) (container_cpu_usage_seconds_total) * on (container, env, region) group_right () container_memory_working_set_bytes`},
		{name: "group_left virtual label in include", query: `container_memory_working_set_bytes{env="prod"} * on (container) group_left (env) sum by (container) (container_cpu_usage_seconds_total{env="prod"})`},
		{name: "group_right virtual label in include", query: `sum by (container) (container_cpu_usage_seconds_total{env="prod"}) * on (container) group_right (env) container_memory_working_set_bytes{env="prod"}`},
		{name: "scalar addition", query: `container_memory_working_set_bytes + 1`},
		{name: "vector group_left", query: `container_memory_working_set_bytes * on() group_left() vector(2)`},
		{name: "sum of scalar addition", query: `sum by (env) (container_memory_working_set_bytes + 1)`},
		{name: "constant binary", query: `vector(1) + vector(1)`},
		{name: "on virtual label group_left with aggregation", query: `container_memory_working_set_bytes{env="prod"} * on(env) group_left() sum by(env) (container_cpu_usage_seconds_total{env="prod"})`},
		{name: "ignoring virtual label", query: `sum by (env, container)(container_memory_working_set_bytes{env="prod"}) + ignoring(env) sum by (env, container)(container_memory_working_set_bytes{env="staging"})`},
		{name: "on non virtual label", query: `container_memory_working_set_bytes + on(container) group_left() sum by (container) (container_cpu_usage_seconds_total)`},
		{name: "count by job div scalar count", query: `count by (container) (container_memory_working_set_bytes) / scalar(count(container_memory_working_set_bytes))`},
		{name: "on all virtual labels preserves", query: `container_memory_working_set_bytes + on(env, region, container, cluster) container_cpu_usage_seconds_total`},
		{name: "disjoint partitions no pushdown", query: `container_memory_working_set_bytes{env="prod"} + container_memory_working_set_bytes{env="staging"}`, opts: []compare.Option{compare.ShouldBeEmpty()}},
		{name: "bool modifier", query: `kube_pod_info > bool 2`},
		{name: "greater than filter drops series", query: `container_memory_working_set_bytes > 50`},
		{name: "greater equal filter", query: `container_memory_working_set_bytes >= 50`},
		{name: "less than filter drops series", query: `container_memory_working_set_bytes < 50`},
		{name: "less equal filter", query: `container_memory_working_set_bytes <= 50`},
		{name: "not equal filter", query: `container_memory_working_set_bytes != 50`},
		{name: "equal filter drops all", query: `container_memory_working_set_bytes == 50`, opts: []compare.Option{compare.ShouldBeEmpty()}},
		{name: "less than filter on negative gauge", query: `node_load_correlation < 0`},
		{name: "greater than bool on virtual label metric", query: `container_memory_working_set_bytes > bool 50`},
		{name: "greater equal bool", query: `container_memory_working_set_bytes >= bool 50`},
		{name: "less than bool", query: `container_memory_working_set_bytes < bool 50`},
		{name: "less equal bool", query: `container_memory_working_set_bytes <= bool 50`},
		{name: "not equal bool", query: `container_memory_working_set_bytes != bool 50`},
		{name: "equal bool", query: `container_memory_working_set_bytes == bool 50`},
		{name: "scalar left operand filter", query: `50 < container_memory_working_set_bytes`},
		{name: "vector vs vector greater on container", query: `container_memory_working_set_bytes{container="backend"} > on (cluster, env, region) container_memory_working_set_bytes{container="frontend"}`},
		{name: "vector vs vector greater bool ignoring container", query: `container_memory_working_set_bytes{container="backend"} > bool ignoring (container) container_memory_working_set_bytes{container="frontend"}`},
		{name: "vector vs vector greater equal on virtual labels", query: `container_memory_working_set_bytes{container="backend"} >= on (cluster, env, region) container_memory_working_set_bytes{container="frontend"}`},
		{name: "vector vs vector not equal ignoring container", query: `container_memory_working_set_bytes{container="backend"} != ignoring (container) container_memory_working_set_bytes{container="frontend"}`},
		{name: "scalar operand reaching a different downstream", query: `container_memory_working_set_bytes{env="staging"} / scalar(count(container_memory_working_set_bytes{env="prod"}))`},
		{name: "sum with a constant aggregation", query: `container_cpu_usage_seconds_total + on() group_left() sum(vector(100))`},
		{name: "sum with a constant aggregation with parameter", query: `container_cpu_usage_seconds_total + on() group_left() topk(scalar(vector(1)), vector(100))`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s.comparator.CompareQuery(t, tc.query, s.harness.EvalTimeMs, tc.opts...)
		})
	}
}
