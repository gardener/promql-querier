// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package plan

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gardener/promql-querier/labels"
)

func TestRewriteAggregations(t *testing.T) {
	labelNames := labels.NewLabelNames("env")
	labelSets := map[string]labels.LabelSet{
		"http://prod:9090":    {"env": "prod"},
		"http://staging:9090": {"env": "staging"},
	}
	labelSets = labels.ExpandLabelSets(labelSets)

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "sum decomposes into outer sum over inner sum keeping virtual label",
			input: `sum(rate(up[5m]))`,
			want:  `sum(sum by (env) (rate(up[5m])))`,
		},
		{
			name:  "count decomposes into outer sum over inner count",
			input: `count(up)`,
			want:  `sum(count by (env) (up))`,
		},
		{
			name:  "max decomposes into outer max over inner max",
			input: `max(up)`,
			want:  `max(max by (env) (up))`,
		},
		{
			name:  "avg decomposes into inner sum divided by inner count",
			input: `avg(rate(up[5m]))`,
			want:  `(sum(sum by (env) (rate(up[5m]))) / sum(count by (env) (rate(up[5m]))))`,
		},
		{
			name:  "residual grouping is preserved on both inner and outer",
			input: `sum by (region) (up)`,
			want:  `sum by (region) (sum by (region, env) (up))`,
		},
		{
			name:  "avg with residual grouping preserves it on both sides",
			input: `avg by (region) (up)`,
			want:  `(sum by (region) (sum by (region, env) (up)) / sum by (region) (count by (region, env) (up)))`,
		},
		{
			name:  "without drops the virtual label from inner grouping",
			input: `sum without (env) (up)`,
			want:  `sum without (env) (sum without () (up))`,
		},
		{
			name:  "non-decomposable aggregation is left untouched",
			input: `stddev(up)`,
			want:  `stddev(up)`,
		},
		{
			name:  "aggregation that pushes down whole is not decomposed",
			input: `sum by (env) (up)`,
			want:  `sum by (env) (up)`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := NewParser().ParseExpr(tc.input)
			require.NoError(t, err)

			node := annotate(expr, labelSets, labelNames)
			rewriteAggregations(node, labelNames)

			require.Equal(t, tc.want, (*node.expr).String())
		})
	}
}

func TestRewriteAbsent(t *testing.T) {
	labelNames := labels.NewLabelNames("env")
	labelSets := map[string]labels.LabelSet{
		"http://prod:9090":    {"env": "prod"},
		"http://staging:9090": {"env": "staging"},
	}
	labelSets = labels.ExpandLabelSets(labelSets)

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "absent collapses argument to existence group and re-synthesizes matcher",
			input: `absent(up{job="x"})`,
			want:  `label_replace(absent(group by (env) (up{job="x"})), "job", "x", "", "")`,
		},
		{
			name:  "absent without matchers only collapses argument",
			input: `absent(up)`,
			want:  `absent(group by (env) (up))`,
		},
		{
			name:  "absent with multiple matchers wraps one label_replace per matcher",
			input: `absent(up{job="x", instance="y"})`,
			want:  `label_replace(label_replace(absent(group by (env) (up{instance="y",job="x"})), "job", "x", "", ""), "instance", "y", "", "")`,
		},
		{
			name:  "absent_over_time collapses to absent over present_over_time",
			input: `absent_over_time(up{job="x"}[5m])`,
			want:  `label_replace(absent(group by (env) (present_over_time(up{job="x"}[5m]))), "job", "x", "", "")`,
		},
		{
			name:  "absent over an aggregation is left untouched",
			input: `absent(sum by (job) (up))`,
			want:  `absent(sum by (job) (sum by (job, env) (up)))`,
		},
		{
			name:  "absent over a decomposed count is left untouched",
			input: `absent(count(up))`,
			want:  `absent(sum(count by (env) (up)))`,
		},
		{
			name:  "absent over a function call collapses to an existence group",
			input: `absent(rate(up[5m]))`,
			want:  `absent(group by (env) (rate(up[5m])))`,
		},
		{
			name:  "absent_over_time over a subquery collapses to absent over present_over_time",
			input: `absent_over_time(rate(up[5m])[10m:])`,
			want:  `absent(group by (env) (present_over_time(rate(up[5m])[10m:])))`,
		},
		{
			name:  "non-absent call is left untouched",
			input: `rate(up[5m])`,
			want:  `rate(up[5m])`,
		},
		{
			name:  "absent does not wrap matchers if the argument is not a vector selector",
			input: `absent(rate(up{job="x"}[5m]))`,
			want:  `absent(group by (env) (rate(up{job="x"}[5m])))`,
		},
		{
			name:  "absent_over_time does not wrap matchers if the argument is not a matrix selector",
			input: `absent_over_time(up{job="x"}[5m:])`,
			want:  `absent(group by (env) (present_over_time(up{job="x"}[5m:])))`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := NewParser().ParseExpr(tc.input)
			require.NoError(t, err)

			node := annotate(expr, labelSets, labelNames)
			rewriteAggregations(node, labelNames)
			rewriteAbsent(node, labelNames)

			require.Equal(t, tc.want, (*node.expr).String())
		})
	}
}
