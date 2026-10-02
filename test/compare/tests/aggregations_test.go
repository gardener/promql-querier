// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

//go:build compare

package compare_test

import "testing"

func TestAggregations(t *testing.T) {
	queries := []struct {
		name  string
		query string
	}{
		{name: "avg by one virtual label", query: `avg by (env) (container_memory_working_set_bytes)`},
		{name: "avg by virtual labels", query: `avg by (env, region) (container_memory_working_set_bytes)`},
		{name: "avg of avg", query: `avg(avg by (env) (container_memory_working_set_bytes))`},
		{name: "avg without non virtual", query: `avg without (container) (container_memory_working_set_bytes)`},
		{name: "avg without virtual", query: `avg without (env) (container_memory_working_set_bytes)`},
		{name: "binary with offset on one side", query: `sum(container_memory_working_set_bytes offset 5m) / sum(container_memory_working_set_bytes)`},
		{name: "bottomk a constant with a constant parameter", query: `bottomk(scalar(vector(1)), vector(100))`},
		{name: "bottomk by virtual labels", query: `bottomk by (env, region) (1, container_memory_working_set_bytes)`},
		{name: "bottomk by virtual", query: `bottomk by (env) (1, container_memory_working_set_bytes)`},
		{name: "bottomk with non constant param reaching a different downstream", query: `bottomk(scalar(count(container_cpu_usage_seconds_total{env="prod"})), container_cpu_usage_seconds_total{env="staging"})`},
		{name: "bottomk", query: `bottomk(2, container_memory_working_set_bytes)`},
		{name: "count by one virtual label", query: `count by (env) (container_memory_working_set_bytes)`},
		{name: "count by virtual labels", query: `count by (env, region) (container_memory_working_set_bytes)`},
		{name: "count of avg", query: `count(avg by (env) (container_memory_working_set_bytes))`},
		{name: "count without virtual", query: `count without (env) (container_memory_working_set_bytes)`},
		{name: "count", query: `count(container_memory_working_set_bytes)`},
		{name: "count_values by virtual labels", query: `count_values by (env, region) ("val", container_memory_working_set_bytes)`},
		{name: "count_values", query: `count_values("val", container_memory_working_set_bytes)`},
		{name: "group by non virtual", query: `group by (container) (container_memory_working_set_bytes)`},
		{name: "group by virtual labels", query: `group by (env, region) (container_memory_working_set_bytes)`},
		{name: "group", query: `group(container_memory_working_set_bytes)`},
		{name: "max by virtual labels", query: `max by (env, region) (container_cpu_usage_seconds_total)`},
		{name: "max", query: `max(container_cpu_usage_seconds_total)`},
		{name: "min by virtual labels", query: `min by (env, region) (container_cpu_usage_seconds_total)`},
		{name: "min", query: `min(container_cpu_usage_seconds_total)`},
		{name: "quantile by env", query: `quantile by (env) (0.5, container_memory_working_set_bytes)`},
		{name: "quantile by virtual labels", query: `quantile by (env, region) (0.5, container_memory_working_set_bytes)`},
		{name: "quantile with non constant param reaching a different downstream", query: `quantile(scalar(count(container_cpu_usage_seconds_total{env="prod"})) / 1000, container_cpu_usage_seconds_total{env="staging"})`},
		{name: "quantile", query: `quantile(0.9, container_memory_working_set_bytes)`},
		{name: "scalar divided by avg requires parenthesized decomposition", query: `1 / avg by (env) (container_memory_working_set_bytes)`},
		{name: "stddev by virtual labels", query: `stddev by (env, region) (container_memory_working_set_bytes)`},
		{name: "stddev", query: `stddev(container_memory_working_set_bytes)`},
		{name: "stdvar by env", query: `stdvar by (env) (container_memory_working_set_bytes)`},
		{name: "stdvar by virtual labels", query: `stdvar by (env, region) (container_memory_working_set_bytes)`},
		{name: "stdvar", query: `stdvar(container_memory_working_set_bytes)`},
		{name: "sum by a virtual and a non virtual label", query: `sum by (env, container) (container_memory_working_set_bytes)`},
		{name: "sum by non virtual label", query: `sum by (container) (container_memory_working_set_bytes)`},
		{name: "sum by one virtual label", query: `sum by (env) (container_memory_working_set_bytes)`},
		{name: "sum by virtual labels of sum by one virtual label", query: `sum by (env, region) (sum by (env) (container_memory_working_set_bytes))`},
		{name: "sum by virtual labels", query: `sum by (env, region) (container_memory_working_set_bytes)`},
		{name: "sum of a constant by virtual labels", query: `sum by(env, region) (vector(1))`},
		{name: "sum of a constant", query: `sum(vector(1))`},
		{name: "sum of avg by non virtual label", query: `sum(avg by (container) (container_memory_working_set_bytes))`},
		{name: "sum of avg by one virtual label and one non virtual label", query: `sum by (env) (avg by (env, container) (container_memory_working_set_bytes))`},
		{name: "sum with offset", query: `sum(container_memory_working_set_bytes offset 5m)`},
		{name: "sum without non virtual", query: `sum without (container) (container_memory_working_set_bytes)`},
		{name: "sum without virtual labels", query: `sum without (env, region) (container_memory_working_set_bytes)`},
		{name: "sum without virtual", query: `sum without (env) (container_memory_working_set_bytes)`},
		{name: "sum", query: `sum(container_memory_working_set_bytes)`},
		{name: "topk a constant with a constant parameter", query: `topk(scalar(vector(1)), vector(100))`},
		{name: "topk by non virtual", query: `topk by (container) (2, container_memory_working_set_bytes{env="prod", region="eu"})`},
		{name: "topk by virtual labels", query: `topk by (env, region) (3, container_memory_working_set_bytes)`},
		{name: "topk with non constant param reaching a different downstream", query: `topk(scalar(count(container_cpu_usage_seconds_total{env="prod"})), container_cpu_usage_seconds_total{env="staging"})`},
		{name: "topk", query: `topk(3, container_memory_working_set_bytes)`},
	}

	for _, tc := range queries {
		t.Run(tc.name, func(t *testing.T) {
			s.comparator.CompareQuery(t, tc.query, s.harness.EvalTimeMs)
		})
	}
}
