// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package plan

import (
	"testing"

	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/require"

	"github.com/gardener/promql-querier/labels"
)

func parseAggregation(t *testing.T, input string) *parser.AggregateExpr {
	t.Helper()
	expr, err := NewParser().ParseExpr(input)
	require.NoError(t, err)
	agg, ok := expr.(*parser.AggregateExpr)
	require.True(t, ok)
	return agg
}

func TestAggregationStaysWithinDownstream(t *testing.T) {
	labelNames := labels.NewLabelNames("env", "region")

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "no grouping",
			input: `sum(up)`,
			want:  false,
		},
		{
			name:  "by with all virtual labels",
			input: `sum by (env, region) (up)`,
			want:  true,
		},
		{
			name:  "by with one virtual label missing",
			input: `sum by (env) (up)`,
			want:  false,
		},
		{
			name:  "by with virtual and extra labels",
			input: `sum by (env, region, job) (up)`,
			want:  true,
		},
		{
			name:  "without with non-virtual label",
			input: `sum without (job) (up)`,
			want:  true,
		},
		{
			name:  "without with virtual label",
			input: `sum without (env) (up)`,
			want:  false,
		},
		{
			name:  "without with both virtual labels",
			input: `sum without (env, region) (up)`,
			want:  false,
		},
		{
			name:  "count_values always crosses downstreams",
			input: `count_values("val", up)`,
			want:  false,
		},
		{
			name:  "count_values by with virtual labels still crosses downstreams",
			input: `count_values by (env, region) ("val", up)`,
			want:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agg := parseAggregation(t, tc.input)
			require.Equal(t, tc.want, (aggregation{agg}).staysWithinDownstream(labelNames))
		})
	}
}
