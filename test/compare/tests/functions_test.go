// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

//go:build compare

package compare_test

import (
	"testing"

	"github.com/gardener/promql-querier/test/compare"
)

func TestFunctions(t *testing.T) {
	tests := []struct {
		name  string
		query string
		opts  []compare.Option
	}{
		{name: "abs per series transform", query: `abs(container_memory_working_set_bytes)`},
		{name: "acos", query: `acos(node_load_correlation)`},
		{name: "acosh", query: `acosh(clamp_min(container_memory_working_set_bytes, 1))`},
		{name: "asin", query: `asin(node_load_correlation)`},
		{name: "asinh", query: `asinh(container_memory_working_set_bytes)`},
		{name: "atan", query: `atan(container_memory_working_set_bytes)`},
		{name: "atanh", query: `atanh(node_load_correlation)`},
		{name: "avg subquery", query: `avg(container_memory_working_set_bytes)[10m:1m]`},
		{name: "avg_over_time of sum", query: `avg_over_time(sum by (env) (container_memory_working_set_bytes)[5m:1m])`},
		{name: "avg_over_time", query: `avg_over_time(container_memory_working_set_bytes[5m])`},
		{name: "ceil per series transform", query: `ceil(container_memory_working_set_bytes)`},
		{name: "changes", query: `changes(container_memory_working_set_bytes[10m])`},
		{name: "clamp", query: `clamp(kube_pod_info, 2, 3)`},
		{name: "clamp_max", query: `clamp_max(kube_pod_info, 2)`},
		{name: "clamp_min with scalar arg reaching a different downstream", query: `clamp_min(container_memory_working_set_bytes{env="staging"}, scalar(count(container_memory_working_set_bytes{env="prod"})))`},
		{name: "clamp_min", query: `clamp_min(kube_pod_info, 3)`},
		{name: "cos", query: `cos(container_memory_working_set_bytes)`},
		{name: "cosh", query: `cosh(node_load_correlation)`},
		{name: "count_over_time", query: `count_over_time(container_memory_working_set_bytes[5m])`},
		{name: "day_of_month", query: `day_of_month(container_memory_working_set_bytes)`},
		{name: "day_of_week no args", query: `day_of_week()`},
		{name: "day_of_year", query: `day_of_year(container_memory_working_set_bytes)`},
		{name: "days_in_month", query: `days_in_month(container_memory_working_set_bytes)`},
		{name: "deg", query: `deg(container_memory_working_set_bytes)`},
		{name: "delta range function", query: `delta(container_memory_working_set_bytes[5m])`},
		{name: "deriv range function", query: `deriv(container_memory_working_set_bytes[5m])`},
		{name: "double_exponential_smoothing", query: `double_exponential_smoothing(container_memory_working_set_bytes[10m], 0.5, 0.5)`},
		{name: "exp", query: `exp(node_load_correlation)`},
		{name: "first_over_time", query: `first_over_time(container_memory_working_set_bytes[10m])`},
		{name: "floor", query: `floor(container_memory_working_set_bytes)`},
		{name: "histogram_fraction classic", query: `histogram_fraction(0, 1, sum by (env, region, le) (request_duration_seconds_bucket))`},
		{name: "histogram_quantile classic", query: `histogram_quantile(0.9, sum by (env, region, le) (request_duration_seconds_bucket))`},
		{name: "histogram_quantile with scalar arg before vector reaching a different downstream", query: `histogram_quantile(scalar(count(container_memory_working_set_bytes{env="prod"})) / 1e9, request_duration_seconds_bucket{env="staging"})`},
		{name: "histogram_quantiles classic", query: `histogram_quantiles(sum by (env, region, le) (request_duration_seconds_bucket), "phi", 0.5, 0.9)`},
		{name: "hour", query: `hour(container_memory_working_set_bytes)`},
		{name: "idelta", query: `idelta(container_memory_working_set_bytes[10m])`},
		{name: "increase range function", query: `increase(container_cpu_usage_seconds_total{container="backend"}[5m])`},
		{name: "irate range function", query: `irate(container_cpu_usage_seconds_total{container="backend"}[5m])`},
		{name: "label_join non virtual sources", query: `label_join(container_memory_working_set_bytes{env="prod"}, "id", "/", "container")`},
		{name: "label_join virtual label as source", query: `label_join(container_memory_working_set_bytes, "loc", "-", "env", "region")`},
		{name: "label_replace non virtual source", query: `label_replace(container_memory_working_set_bytes, "service", "$1", "container", "(.*)")`},
		{name: "label_replace virtual label as source", query: `label_replace(container_memory_working_set_bytes, "environment", "$1", "env", "(.*)")`},
		{name: "last_over_time", query: `last_over_time(container_memory_working_set_bytes[10m])`},
		{name: "ln", query: `ln(abs(container_memory_working_set_bytes))`},
		{name: "log10", query: `log10(abs(container_memory_working_set_bytes))`},
		{name: "log2", query: `log2(abs(container_memory_working_set_bytes))`},
		{name: "mad_over_time", query: `mad_over_time(container_memory_working_set_bytes[10m])`},
		{name: "max_over_time", query: `max_over_time(container_cpu_usage_seconds_total{container="frontend"}[5m])`},
		{name: "min_over_time", query: `min_over_time(container_cpu_usage_seconds_total{container="frontend"}[5m])`},
		{name: "minute", query: `minute(container_memory_working_set_bytes)`},
		{name: "month", query: `month(container_memory_working_set_bytes)`},
		{name: "offset", query: `avg_over_time(container_memory_working_set_bytes offset 5m[10m:1m])`},
		{name: "pi scalar nonpreserving", query: `pi()`},
		{name: "predict_linear range function", query: `predict_linear(container_memory_working_set_bytes[5m], 60)`},
		{name: "present_over_time", query: `present_over_time(container_memory_working_set_bytes[10m])`},
		{name: "quantile_over_time", query: `quantile_over_time(0.9, container_memory_working_set_bytes[10m])`},
		{name: "rad", query: `rad(container_memory_working_set_bytes)`},
		{name: "rate with virtual label filter", query: `rate(container_cpu_usage_seconds_total{env="prod"}[5m])`},
		{name: "rate", query: `rate(container_cpu_usage_seconds_total{container="backend"}[5m])`},
		{name: "resets range function", query: `resets(container_cpu_usage_seconds_total{container="backend"}[1h])`},
		{name: "round", query: `round(container_memory_working_set_bytes)`},
		{name: "scalar nonpreserving over single downstream", query: `scalar(kube_pod_info{env="prod", region="eu"})`},
		{name: "sgn", query: `sgn(container_memory_working_set_bytes - 50)`},
		{name: "sin", query: `sin(container_memory_working_set_bytes)`},
		{name: "sinh", query: `sinh(node_load_correlation)`},
		{name: "sort", query: `sort(sum by (env)(container_memory_working_set_bytes))`, opts: []compare.Option{compare.DoNotSort()}},
		{name: "sort_by_label", query: `sort_by_label(sum by (env, region) (container_memory_working_set_bytes), "env")`, opts: []compare.Option{compare.DoNotSort()}},
		{name: "sort_by_label_desc", query: `sort_by_label_desc(sum by (env, region) (container_memory_working_set_bytes), "env")`, opts: []compare.Option{compare.DoNotSort()}},
		{name: "sort_desc", query: `sort_desc(sum by (env)(container_memory_working_set_bytes))`, opts: []compare.Option{compare.DoNotSort()}},
		{name: "sqrt", query: `sqrt(abs(container_memory_working_set_bytes))`},
		{name: "stddev_over_time", query: `stddev_over_time(container_memory_working_set_bytes[10m])`},
		{name: "stdvar_over_time", query: `stdvar_over_time(container_memory_working_set_bytes[10m])`},
		{name: "sum of rate", query: `sum by (env) (rate(container_cpu_usage_seconds_total{container="backend"}[5m]))`},
		{name: "tan", query: `tan(container_memory_working_set_bytes)`},
		{name: "tanh", query: `tanh(container_memory_working_set_bytes)`},
		{name: "time scalar nonpreserving", query: `time()`},
		{name: "timestamp per series transform", query: `timestamp(container_memory_working_set_bytes)`},
		{name: "ts_of_first_over_time", query: `ts_of_first_over_time(container_memory_working_set_bytes[10m])`},
		{name: "ts_of_last_over_time", query: `ts_of_last_over_time(container_memory_working_set_bytes[10m])`},
		{name: "ts_of_max_over_time", query: `ts_of_max_over_time(container_memory_working_set_bytes[10m])`},
		{name: "ts_of_min_over_time", query: `ts_of_min_over_time(container_memory_working_set_bytes[10m])`},
		{name: "vector nonpreserving", query: `vector(42)`},
		{name: "year no args", query: `year()`},
		{name: "year with series", query: `year(container_memory_working_set_bytes)`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s.comparator.CompareQuery(t, tc.query, s.harness.EvalTimeMs, tc.opts...)
		})
	}
}
