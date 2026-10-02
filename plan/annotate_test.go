// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package plan

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gardener/promql-querier/labels"
)

func TestStripLabels(t *testing.T) {
	labelNames := labels.NewLabelNames("env")

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "selector with virtual label matcher",
			input: `up{env="prod"}`,
			want:  `up`,
		},
		{
			name:  "bare selector with only virtual label matcher",
			input: `{env="prod"}`,
			want:  `{__name__!=""}`,
		},
		{
			name:  "bare selector with virtual and empty-value matcher",
			input: `{env="prod", foo=""}`,
			want:  `{__name__!="",foo=""}`,
		},
		{
			name:  "selector with virtual and non-virtual matcher",
			input: `up{env="prod", job="prometheus"}`,
			want:  `up{job="prometheus"}`,
		},
		{
			name:  "selector without virtual label matcher",
			input: `up{job="prometheus"}`,
			want:  `up{job="prometheus"}`,
		},
		{
			name:  "aggregation by with virtual label",
			input: `sum by (env, pod) (up)`,
			want:  `sum by (pod) (up)`,
		},
		{
			name:  "aggregation by with only virtual label",
			input: `sum by (env) (up)`,
			want:  `sum(up)`,
		},
		{
			name:  "aggregation without with virtual label",
			input: `sum without (env, pod) (up)`,
			want:  `sum without (pod) (up)`,
		},
		{
			name:  "aggregation without with only virtual label",
			input: `sum without (env) (up)`,
			want:  `sum without () (up)`,
		},
		{
			name:  "binary ignoring virtual label",
			input: `foo + ignoring (env) bar`,
			want:  `foo + bar`,
		},
		{
			name:  "binary ignoring virtual and other label",
			input: `foo + ignoring (env, pod) bar`,
			want:  `foo + ignoring (pod) bar`,
		},
		{
			name:  "binary on with virtual label",
			input: `foo + on (env, pod) bar`,
			want:  `foo + on (pod) bar`,
		},
		{
			name:  "binary group_left with virtual label in include",
			input: `foo + on (pod) group_left (env) bar`,
			want:  `foo + on (pod) group_left () bar`,
		},
		{
			name:  "rate with matrix selector",
			input: `rate(up{env="prod"}[5m])`,
			want:  `rate(up[5m])`,
		},
		{
			name:  "nested aggregation and selector",
			input: `sum by (env) (rate(up{env="prod"}[5m]))`,
			want:  `sum(rate(up[5m]))`,
		},
		{
			name:  "no virtual labels to strip",
			input: `sum by (pod) (up{job="prometheus"})`,
			want:  `sum by (pod) (up{job="prometheus"})`,
		},
		{
			name:  "subquery",
			input: `sum by (env) (up{env="prod"})[1h:5m]`,
			want:  `sum(up)[1h:5m]`,
		},
		{
			name:  "paren expr",
			input: `(up{env="prod"})`,
			want:  `(up)`,
		},
		{
			name:  "unary negation",
			input: `-up{env="prod"}`,
			want:  `-up`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := NewParser().ParseExpr(tc.input)
			require.NoError(t, err)
			got := stripLabels(expr, labelNames)
			require.Equal(t, tc.want, got.String())
		})
	}
}

func TestResolveDownstreams(t *testing.T) {
	labelSets := map[string]labels.LabelSet{
		"http://prod-eu:9090":    {"env": "prod", "region": "eu"},
		"http://prod-us:9090":    {"env": "prod", "region": "us"},
		"http://staging-eu:9090": {"env": "staging", "region": "eu"},
	}

	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "no matchers returns all",
			input: `up`,
			want:  []string{"http://prod-eu:9090", "http://prod-us:9090", "http://staging-eu:9090"},
		},
		{
			name:  "equality matcher filters",
			input: `up{env="prod"}`,
			want:  []string{"http://prod-eu:9090", "http://prod-us:9090"},
		},
		{
			name:  "two equality matchers narrow further",
			input: `up{env="prod", region="eu"}`,
			want:  []string{"http://prod-eu:9090"},
		},
		{
			name:  "regex matcher filters",
			input: `up{env=~"prod|staging"}`,
			want:  []string{"http://prod-eu:9090", "http://prod-us:9090", "http://staging-eu:9090"},
		},
		{
			name:  "negative matcher excludes",
			input: `up{env!="prod"}`,
			want:  []string{"http://staging-eu:9090"},
		},
		{
			name:  "non-virtual label matcher does not filter",
			input: `up{job="prometheus"}`,
			want:  []string{"http://prod-eu:9090", "http://prod-us:9090", "http://staging-eu:9090"},
		},
		{
			name:  "no matches returns empty",
			input: `up{env="dev"}`,
			want:  nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := NewParser().ParseExpr(tc.input)
			require.NoError(t, err)
			result := resolveDownstreams(expr, labelSets)
			if tc.want == nil {
				require.Nil(t, result)
			} else {
				require.Equal(t, tc.want, result)
			}
		})
	}
}
