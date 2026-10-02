// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package plan

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gardener/promql-querier/labels"
)

func TestDescriptionJSONRoundTrip(t *testing.T) {
	end := time.Unix(300, 0).UTC()
	desc := Description{
		Expr:    `sum({__partition_idx__="0"})`,
		IsRange: false,
		Partitions: []PartitionDescription{
			{
				Expr:           `count by (env) (foo)[10m:]`,
				DownstreamExpr: `count(foo)[10m:]`,
				Downstreams:    []string{"http://prod:9090"},
				IsRange:        false,
				Time:           end,
				Step:           120 * time.Second,
			},
		},
	}

	raw, err := json.Marshal(desc)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))

	require.Equal(t, `sum({__partition_idx__="0"})`, got["expr"])
	require.Equal(t, false, got["is_range"])
	require.NotContains(t, got, "step", "zero step should be omitted by omitempty")
	require.Equal(t, "0001-01-01T00:00:00Z", got["time"], "omitempty should not omit a zero time")

	partitions, ok := got["partitions"].([]any)
	require.True(t, ok, "partitions should serialize as an array")
	require.Len(t, partitions, 1)

	part := partitions[0].(map[string]any)
	require.Equal(t, `count by (env) (foo)[10m:]`, part["expr"])
	require.Equal(t, `count(foo)[10m:]`, part["downstream_expr"])
	require.Equal(t, []any{"http://prod:9090"}, part["downstreams"])
	require.EqualValues(t, 120*time.Second, part["step"], "step should encode as integer nanoseconds")
	require.Equal(t, "1970-01-01T00:05:00Z", part["time"], "time should encode as RFC3339")
	require.Equal(t, "0001-01-01T00:00:00Z", part["start"], "a zero start should be emitted, not omitted")

	roundTripped := Description{}
	require.NoError(t, json.Unmarshal(raw, &roundTripped))
	require.Equal(t, desc, roundTripped, "plan description should survive a marshal/unmarshal round trip")
}

func TestPlanInstant(t *testing.T) {
	labelSets := map[string]labels.LabelSet{
		"http://prod:9090":    {"env": "prod"},
		"http://staging:9090": {"env": "staging"},
	}

	labelSets = labels.ExpandLabelSets(labelSets)
	labelNames := labels.NewLabelNames("env")

	tests := []struct {
		name           string
		input          string
		wantLocal      string
		wantPartitions []PartitionDescription
	}{
		{
			name:      "bare selector pushes down to all downstreams",
			input:     `up`,
			wantLocal: `{__partition_idx__="0"}`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `up`,
					DownstreamExpr: `up`,
					Downstreams:    []string{"http://prod:9090", "http://staging:9090"},
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:      "virtual label matcher narrows routing",
			input:     `up{env="prod"}`,
			wantLocal: `{__partition_idx__="0"}`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `up{env="prod"}`,
					DownstreamExpr: `up`,
					Downstreams:    []string{"http://prod:9090"},
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:      "aggregation grouping by virtual label pushes down whole",
			input:     `sum by (env) (rate(up[5m]))`,
			wantLocal: `{__partition_idx__="0"}`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `sum by (env) (rate(up[5m]))`,
					DownstreamExpr: `sum(rate(up[5m]))`,
					Downstreams:    []string{"http://prod:9090", "http://staging:9090"},
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:      "aggregation without grouping stays local over pushed-down inner",
			input:     `sum(rate(up[5m]))`,
			wantLocal: `sum({__partition_idx__="0"})`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `sum by (env) (rate(up[5m]))`,
					DownstreamExpr: `sum(rate(up[5m]))`,
					Downstreams:    []string{"http://prod:9090", "http://staging:9090"},
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:      "count decomposes into local sum over pushed-down count",
			input:     `count(up)`,
			wantLocal: `sum({__partition_idx__="0"})`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `count by (env) (up)`,
					DownstreamExpr: `count(up)`,
					Downstreams:    []string{"http://prod:9090", "http://staging:9090"},
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:      "residual grouping keeps outer aggregation local",
			input:     `sum by (region) (rate(up[5m]))`,
			wantLocal: `sum by (region) ({__partition_idx__="0"})`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `sum by (region, env) (rate(up[5m]))`,
					DownstreamExpr: `sum by (region) (rate(up[5m]))`,
					Downstreams:    []string{"http://prod:9090", "http://staging:9090"},
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:      "avg decomposes into sum divided by count with two partitions",
			input:     `avg(rate(up[5m]))`,
			wantLocal: `(sum({__partition_idx__="0"}) / sum({__partition_idx__="1"}))`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `sum by (env) (rate(up[5m]))`,
					DownstreamExpr: `sum(rate(up[5m]))`,
					Downstreams:    []string{"http://prod:9090", "http://staging:9090"},
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
				{
					Expr:           `count by (env) (rate(up[5m]))`,
					DownstreamExpr: `count(rate(up[5m]))`,
					Downstreams:    []string{"http://prod:9090", "http://staging:9090"},
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:      "absent with matchers wraps idx-strip inside per-matcher label_replace",
			input:     `absent(up{job="x", instance="y"})`,
			wantLocal: `label_replace(label_replace(label_replace(absent({__partition_idx__="0"}), "__partition_idx__", "", "", ""), "job", "x", "", ""), "instance", "y", "", "")`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `group by (env) (up{instance="y",job="x"})`,
					DownstreamExpr: `group(up{instance="y",job="x"})`,
					Downstreams:    []string{"http://prod:9090", "http://staging:9090"},
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:      "absent without matchers only strips the idx label",
			input:     `absent(up)`,
			wantLocal: `label_replace(absent({__partition_idx__="0"}), "__partition_idx__", "", "", "")`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `group by (env) (up)`,
					DownstreamExpr: `group(up)`,
					Downstreams:    []string{"http://prod:9090", "http://staging:9090"},
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:      "absent over a non-pushable binary is not rewritten",
			input:     `absent(rate(up[5m]) + on(job) rate(up[5m]))`,
			wantLocal: `absent({__partition_idx__="0"} + on (job) {__partition_idx__="1"})`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `rate(up[5m])`,
					DownstreamExpr: `rate(up[5m])`,
					Downstreams:    []string{"http://prod:9090", "http://staging:9090"},
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
				{
					Expr:           `rate(up[5m])`,
					DownstreamExpr: `rate(up[5m])`,
					Downstreams:    []string{"http://prod:9090", "http://staging:9090"},
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:           "binary with disjoint routes collapses to empty vector",
			input:          `up{env="prod"} + up{env="staging"}`,
			wantLocal:      `{__partition_idx__="n/a"}`,
			wantPartitions: []PartitionDescription{},
		},
		{
			name:      "at modifier on a matrix selector is preserved locally",
			input:     `up[5m] @ end()`,
			wantLocal: `{__partition_idx__="0"}[5m] @ end()`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `up[5m] @ end()`,
					DownstreamExpr: `up[5m] @ end()`,
					Downstreams:    []string{"http://prod:9090", "http://staging:9090"},
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:      "offset on a matrix selector is preserved locally",
			input:     `up[5m] offset 1d`,
			wantLocal: `{__partition_idx__="0"}[5m] offset 1d`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `up[5m] offset 1d`,
					DownstreamExpr: `up[5m] offset 1d`,
					Downstreams:    []string{"http://prod:9090", "http://staging:9090"},
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:      "offset and at on a matrix selector are both preserved locally",
			input:     `up[5m] offset 1d @ 1600000000`,
			wantLocal: `{__partition_idx__="0"}[5m] @ 1600000000.000 offset 1d`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `up[5m] @ 1600000000.000 offset 1d`,
					DownstreamExpr: `up[5m] @ 1600000000.000 offset 1d`,
					Downstreams:    []string{"http://prod:9090", "http://staging:9090"},
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:      "at modifier on a bare vector selector is not preserved locally",
			input:     `up @ end()`,
			wantLocal: `{__partition_idx__="0"}`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `up @ end()`,
					DownstreamExpr: `up @ end()`,
					Downstreams:    []string{"http://prod:9090", "http://staging:9090"},
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:      "offset on a bare vector selector is not preserved locally",
			input:     `up offset 1d`,
			wantLocal: `{__partition_idx__="0"}`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `up offset 1d`,
					DownstreamExpr: `up offset 1d`,
					Downstreams:    []string{"http://prod:9090", "http://staging:9090"},
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:      "at modifier on a subquery is preserved locally",
			input:     `up[5m:30s] @ end()`,
			wantLocal: `{__partition_idx__="0"}[5m:30s] @ end()`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `up[5m:30s] @ end()`,
					DownstreamExpr: `up[5m:30s] @ end()`,
					Downstreams:    []string{"http://prod:9090", "http://staging:9090"},
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:      "offset on a subquery is preserved locally",
			input:     `up[5m:30s] offset 1d`,
			wantLocal: `{__partition_idx__="0"}[5m:30s] offset 1d`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `up[5m:30s] offset 1d`,
					DownstreamExpr: `up[5m:30s] offset 1d`,
					Downstreams:    []string{"http://prod:9090", "http://staging:9090"},
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:      "offset and at on a subquery are both preserved locally",
			input:     `up[5m:30s] offset 1d @ 1600000000`,
			wantLocal: `{__partition_idx__="0"}[5m:30s] @ 1600000000.000 offset 1d`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `up[5m:30s] @ 1600000000.000 offset 1d`,
					DownstreamExpr: `up[5m:30s] @ 1600000000.000 offset 1d`,
					Downstreams:    []string{"http://prod:9090", "http://staging:9090"},
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:      "scalar argument and vector argument route to different downstreams",
			input:     `histogram_quantile(scalar(threshold{env="prod"}), buckets{env="staging"})`,
			wantLocal: `histogram_quantile(scalar({__partition_idx__="0"}), {__partition_idx__="1"})`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `threshold{env="prod"}`,
					DownstreamExpr: `threshold`,
					Downstreams:    []string{"http://prod:9090"},
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
				{
					Expr:           `buckets{env="staging"}`,
					DownstreamExpr: `buckets`,
					Downstreams:    []string{"http://staging:9090"},
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:      "topk with non-constant param partitions the param separately",
			input:     `topk(scalar(count(cpu{env="prod"})), cpu{env="staging"})`,
			wantLocal: `topk(scalar(sum({__partition_idx__="1"})), {__partition_idx__="0"})`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `cpu{env="staging"}`,
					DownstreamExpr: `cpu`,
					Downstreams:    []string{"http://staging:9090"},
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
				{
					Expr:           `count by (env) (cpu{env="prod"})`,
					DownstreamExpr: `count(cpu)`,
					Downstreams:    []string{"http://prod:9090"},
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:      "quantile with non-constant param partitions the param separately",
			input:     `quantile(scalar(count(cpu{env="prod"})) / 1000, cpu{env="staging"})`,
			wantLocal: `quantile(scalar(sum({__partition_idx__="1"})) / 1000, {__partition_idx__="0"})`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `cpu{env="staging"}`,
					DownstreamExpr: `cpu`,
					Downstreams:    []string{"http://staging:9090"},
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
				{
					Expr:           `count by (env) (cpu{env="prod"})`,
					DownstreamExpr: `count(cpu)`,
					Downstreams:    []string{"http://prod:9090"},
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:      "binary with a fully constant operand pushes down whole",
			input:     `container_cpu_usage_seconds_total + on() group_left() topk(scalar(vector(1)), vector(100))`,
			wantLocal: `{__partition_idx__="0"}`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `container_cpu_usage_seconds_total + on () group_left () topk(scalar(vector(1)), vector(100))`,
					DownstreamExpr: `container_cpu_usage_seconds_total + on () group_left () topk(scalar(vector(1)), vector(100))`,
					Downstreams:    []string{"http://prod:9090", "http://staging:9090"},
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:           "fully constant topk stays local with nothing to fetch",
			input:          `topk(scalar(vector(1)), vector(100))`,
			wantLocal:      `topk(scalar(vector(1)), vector(100))`,
			wantPartitions: []PartitionDescription{},
		},
		{
			name:           "fully constant bottomk stays local with nothing to fetch",
			input:          `bottomk(scalar(vector(1)), vector(100))`,
			wantLocal:      `bottomk(scalar(vector(1)), vector(100))`,
			wantPartitions: []PartitionDescription{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := NewParser().ParseExpr(tc.input)
			require.NoError(t, err)

			plan, err := NewPlanner(labelSets, labelNames).PlanInstant(expr, time.Unix(0, 0))
			require.NoError(t, err)

			desc := plan.Describe()
			require.Equal(t, tc.wantLocal, desc.Expr)
			require.Equal(t, tc.wantPartitions, desc.Partitions)
		})
	}
}

func TestPlanInstantTwoVirtualLabels(t *testing.T) {
	labelSets := labels.ExpandLabelSets(map[string]labels.LabelSet{
		"http://prod-eu:9090":    {"env": "prod", "region": "eu"},
		"http://prod-us:9090":    {"env": "prod", "region": "us"},
		"http://staging-eu:9090": {"env": "staging", "region": "eu"},
	})

	labelNames := labels.NewLabelNames("env", "region")
	downstreams := []string{"http://prod-eu:9090", "http://prod-us:9090", "http://staging-eu:9090"}

	tests := []struct {
		name           string
		input          string
		wantLocal      string
		wantPartitions []PartitionDescription
	}{
		{
			name:      "topk keeps the parameter on both local and pushed-down aggregation",
			input:     `topk(3, up)`,
			wantLocal: `topk(3, {__partition_idx__="0"})`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `topk by (env, region) (3, up)`,
					DownstreamExpr: `topk(3, up)`,
					Downstreams:    downstreams,
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:      "bottomk keeps the parameter on both local and pushed-down aggregation",
			input:     `bottomk(2, up)`,
			wantLocal: `bottomk(2, {__partition_idx__="0"})`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `bottomk by (env, region) (2, up)`,
					DownstreamExpr: `bottomk(2, up)`,
					Downstreams:    downstreams,
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:      "topk grouped by all virtual labels pushes down whole",
			input:     `topk by (env, region) (3, up)`,
			wantLocal: `{__partition_idx__="0"}`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `topk by (env, region) (3, up)`,
					DownstreamExpr: `topk(3, up)`,
					Downstreams:    downstreams,
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:      "limitk keeps the parameter on both local and pushed-down aggregation",
			input:     `limitk(3, up)`,
			wantLocal: `limitk(3, {__partition_idx__="0"})`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `limitk by (env, region) (3, up)`,
					DownstreamExpr: `limitk(3, up)`,
					Downstreams:    downstreams,
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:      "limitk grouped by all virtual labels pushes down whole",
			input:     `limitk by (env, region) (3, up)`,
			wantLocal: `{__partition_idx__="0"}`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `limitk by (env, region) (3, up)`,
					DownstreamExpr: `limitk(3, up)`,
					Downstreams:    downstreams,
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:      "stddev with no virtual grouping stays local over a raw downstream fetch",
			input:     `stddev(up)`,
			wantLocal: `stddev({__partition_idx__="0"})`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `up`,
					DownstreamExpr: `up`,
					Downstreams:    downstreams,
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:      "stdvar grouped by one virtual label stays local over a raw downstream fetch",
			input:     `stdvar by (env) (up)`,
			wantLocal: `stdvar by (env) ({__partition_idx__="0"})`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `up`,
					DownstreamExpr: `up`,
					Downstreams:    downstreams,
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:      "quantile with no virtual grouping keeps its parameter local over a raw fetch",
			input:     `quantile(0.9, up)`,
			wantLocal: `quantile(0.9, {__partition_idx__="0"})`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `up`,
					DownstreamExpr: `up`,
					Downstreams:    downstreams,
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:      "stddev grouped by all virtual labels pushes down whole",
			input:     `stddev by (env, region) (up)`,
			wantLocal: `{__partition_idx__="0"}`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `stddev by (env, region) (up)`,
					DownstreamExpr: `stddev(up)`,
					Downstreams:    downstreams,
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:      "quantile grouped by all virtual labels pushes down whole keeping its parameter",
			input:     `quantile by (env, region) (0.5, up)`,
			wantLocal: `{__partition_idx__="0"}`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `quantile by (env, region) (0.5, up)`,
					DownstreamExpr: `quantile(0.5, up)`,
					Downstreams:    downstreams,
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:      "count_values stays local over a raw fetch keeping its value label",
			input:     `count_values("val", up)`,
			wantLocal: `count_values("val", {__partition_idx__="0"})`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `up`,
					DownstreamExpr: `up`,
					Downstreams:    downstreams,
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:      "count_values grouped by all virtual labels still stays local over a raw fetch",
			input:     `count_values by (env, region) ("val", up)`,
			wantLocal: `count_values by (env, region) ("val", {__partition_idx__="0"})`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `up`,
					DownstreamExpr: `up`,
					Downstreams:    downstreams,
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:      "count grouped by all virtual labels pushes down whole without decomposing",
			input:     `count by (env, region) (up)`,
			wantLocal: `{__partition_idx__="0"}`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `count by (env, region) (up)`,
					DownstreamExpr: `count(up)`,
					Downstreams:    downstreams,
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:      "max grouped by all virtual labels pushes down whole",
			input:     `max by (env, region) (up)`,
			wantLocal: `{__partition_idx__="0"}`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `max by (env, region) (up)`,
					DownstreamExpr: `max(up)`,
					Downstreams:    downstreams,
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
		{
			name:      "group grouped by all virtual labels pushes down whole",
			input:     `group by (env, region) (up)`,
			wantLocal: `{__partition_idx__="0"}`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `group by (env, region) (up)`,
					DownstreamExpr: `group(up)`,
					Downstreams:    downstreams,
					IsRange:        false,
					Time:           time.Unix(0, 0),
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := NewParser().ParseExpr(tc.input)
			require.NoError(t, err)

			plan, err := NewPlanner(labelSets, labelNames).PlanInstant(expr, time.Unix(0, 0))
			require.NoError(t, err)

			desc := plan.Describe()
			require.Equal(t, tc.wantLocal, desc.Expr)
			require.Equal(t, tc.wantPartitions, desc.Partitions)
		})
	}
}

func TestPlanRange(t *testing.T) {
	labelSets := labels.ExpandLabelSets(map[string]labels.LabelSet{
		"http://prod:9090":    {"env": "prod"},
		"http://staging:9090": {"env": "staging"},
	})

	labelNames := labels.NewLabelNames("env")

	var (
		startTs int64 = 100
		endTs   int64 = 100 + 5*60 // 5 minutes later.

		start = time.Unix(startTs, 0)
		end   = time.Unix(endTs, 0)
		step  = 120 * time.Second
	)

	downstreams := []string{"http://prod:9090", "http://staging:9090"}

	tests := []struct {
		name           string
		input          string
		wantLocal      string
		wantPartitions []PartitionDescription
	}{
		{
			name:      "subquery with omitted step becomes instant with widened window, step left omitted",
			input:     `rate(count(foo)[5m:])`,
			wantLocal: `rate(sum({__partition_idx__="0"})[5m:])`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `count by (env) (foo)[10m:]`,
					DownstreamExpr: `count(foo)[10m:]`,
					Downstreams:    downstreams,
					IsRange:        false,
					Time:           end,
				},
			},
		},
		{
			name:      "subquery with explicit step keeps its step and widens only the range",
			input:     `rate(count(foo)[2m:5m])`,
			wantLocal: `rate(sum({__partition_idx__="0"})[2m:5m])`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `count by (env) (foo)[7m:5m]`,
					DownstreamExpr: `count(foo)[7m:5m]`,
					Downstreams:    downstreams,
					IsRange:        false,
					Time:           end,
				},
			},
		},
		{
			name:      "bare matrix push-down stays a range fetch",
			input:     `rate(up[5m])`,
			wantLocal: `{__partition_idx__="0"}`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `rate(up[5m])`,
					DownstreamExpr: `rate(up[5m])`,
					Downstreams:    downstreams,
					IsRange:        true,
					Start:          start,
					End:            end,
					Step:           step,
				},
			},
		},
		{
			name:      "virtual label matcher narrows routing and is stripped from the downstream form",
			input:     `rate(count(foo{env="prod"})[5m:])`,
			wantLocal: `rate(sum({__partition_idx__="0"})[5m:])`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `count by (env) (foo{env="prod"})[10m:]`,
					DownstreamExpr: `count(foo)[10m:]`,
					Downstreams:    []string{"http://prod:9090"},
					IsRange:        false,
					Time:           end,
				},
			},
		},
		{
			name:      "absent_over_time over a subquery collapses to an instant vector and stays a range fetch",
			input:     `absent_over_time(rate(up[5m])[1h:])`,
			wantLocal: `label_replace(absent({__partition_idx__="0"}), "__partition_idx__", "", "", "")`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `group by (env) (present_over_time(rate(up[5m])[1h:]))`,
					DownstreamExpr: `group(present_over_time(rate(up[5m])[1h:]))`,
					Downstreams:    downstreams,
					IsRange:        true,
					Start:          start,
					End:            end,
					Step:           step,
				},
			},
		},
		{
			name:      "avg under a subquery decomposes into two instant partitions",
			input:     `sum_over_time(avg(rate(up[5m]))[10m:])`,
			wantLocal: `sum_over_time((sum({__partition_idx__="0"}) / sum({__partition_idx__="1"}))[10m:])`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `sum by (env) (rate(up[5m]))[15m:]`,
					DownstreamExpr: `sum(rate(up[5m]))[15m:]`,
					Downstreams:    downstreams,
					IsRange:        false,
					Time:           end,
				},
				{
					Expr:           `count by (env) (rate(up[5m]))[15m:]`,
					DownstreamExpr: `count(rate(up[5m]))[15m:]`,
					Downstreams:    downstreams,
					IsRange:        false,
					Time:           end,
				},
			},
		},
		{
			name:      "offset on a subquery is preserved through the widened window",
			input:     `rate(count(foo)[5m:] offset 1d)`,
			wantLocal: `rate(sum({__partition_idx__="0"})[5m:] offset 1d)`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `count by (env) (foo)[10m:] offset 1d`,
					DownstreamExpr: `count(foo)[10m:] offset 1d`,
					Downstreams:    downstreams,
					IsRange:        false,
					Time:           end,
				},
			},
		},
		{
			name:      "absolute @ modifier on a subquery is preserved",
			input:     `rate(count(foo)[5m:] @ 1600000000)`,
			wantLocal: `rate(sum({__partition_idx__="0"})[5m:] @ 1600000000.000)`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `count by (env) (foo)[10m:] @ 1600000000.000`,
					DownstreamExpr: `count(foo)[10m:] @ 1600000000.000`,
					Downstreams:    downstreams,
					IsRange:        false,
					Time:           end,
				},
			},
		},
		{
			name:      "@ start() on a subquery is preserved",
			input:     `rate(count(foo)[5m:] @ start())`,
			wantLocal: `rate(sum({__partition_idx__="0"})[5m:] @ start())`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `count by (env) (foo)[10m:] @ start()`,
					DownstreamExpr: `count(foo)[10m:] @ start()`,
					Downstreams:    downstreams,
					IsRange:        false,
					Time:           end,
				},
			},
		},
		{
			name:      "@ end() on a subquery is preserved",
			input:     `rate(count(foo)[5m:] @ end())`,
			wantLocal: `rate(sum({__partition_idx__="0"})[5m:] @ end())`,
			wantPartitions: []PartitionDescription{
				{
					Expr:           `count by (env) (foo)[10m:] @ end()`,
					DownstreamExpr: `count(foo)[10m:] @ end()`,
					Downstreams:    downstreams,
					IsRange:        false,
					Time:           end,
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := NewParser().ParseExpr(tc.input)
			require.NoError(t, err)

			plan, err := NewPlanner(labelSets, labelNames).PlanRange(expr, start, end, step)
			require.NoError(t, err)

			desc := plan.Describe()
			require.Equal(t, tc.wantLocal, desc.Expr)
			require.Equal(t, tc.wantPartitions, desc.Partitions)
		})
	}
}

func TestPlanRangeRejectsRangeVectorResult(t *testing.T) {
	labelSets := labels.ExpandLabelSets(map[string]labels.LabelSet{
		"http://prod:9090": {"env": "prod"},
	})

	labelNames := labels.NewLabelNames("env")
	start := time.Unix(100, 0)
	end := time.Unix(300, 0)
	step := 120 * time.Second

	for _, input := range []string{`up[5m]`, `up[5m:1m]`} {
		t.Run(input, func(t *testing.T) {
			expr, err := NewParser().ParseExpr(input)
			require.NoError(t, err)

			_, err = NewPlanner(labelSets, labelNames).PlanRange(expr, start, end, step)
			require.EqualError(t, err, `invalid expression type "range vector" for range query, must be Scalar or instant Vector`)
		})
	}
}
