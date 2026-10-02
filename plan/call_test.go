// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package plan

import (
	"testing"

	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/require"

	"github.com/gardener/promql-querier/labels"
)

func parseCall(t *testing.T, input string) *parser.Call {
	t.Helper()
	expr, err := NewParser().ParseExpr(input)
	require.NoError(t, err)
	c, ok := expr.(*parser.Call)
	require.True(t, ok, "expected Call, got %T", expr)
	return c
}

func TestLabelReplacePreserves(t *testing.T) {
	labelNames := labels.NewLabelNames("env", "region")

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "label_replace not touching virtual labels",
			input: `label_replace(up, "dst", "$1", "src", "(.*)")`,
			want:  true,
		},
		{
			name:  "label_replace destination is virtual label",
			input: `label_replace(up, "env", "$1", "src", "(.*)")`,
			want:  false,
		},
		{
			name:  "label_replace source is virtual label",
			input: `label_replace(up, "dst", "$1", "env", "(.*)")`,
			want:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := parseCall(t, tc.input)
			require.Equal(t, tc.want, labelReplacePreserves(c, labelNames))
		})
	}
}

func TestLabelJoinPreserves(t *testing.T) {
	labelNames := labels.NewLabelNames("env", "region")

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "label_join destination is virtual label",
			input: `label_join(up, "env", ",", "src1")`,
			want:  false,
		},
		{
			name:  "label_join source is virtual label",
			input: `label_join(up, "dst", ",", "env")`,
			want:  false,
		},
		{
			name:  "label_join one of multiple sources is virtual label",
			input: `label_join(up, "dst", ",", "src1", "region")`,
			want:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := parseCall(t, tc.input)
			require.Equal(t, tc.want, labelJoinPreserves(c, labelNames))
		})
	}
}

func TestPreservesWithArgument(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "preserves",
			input: `year(foo)`,
			want:  true,
		},
		{
			name:  "does not preserve",
			input: `year()`,
			want:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := parseCall(t, tc.input)
			require.Equal(t, tc.want, preservesWithArgument(c, nil))
		})
	}
}

func TestCallStaysWithinDownstream(t *testing.T) {
	labelNames := labels.NewLabelNames("env", "region")

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "preserving function",
			input: `rate(up[5m])`,
			want:  true,
		},
		{
			name:  "non-preserving function",
			input: `absent(up)`,
			want:  false,
		},
		{
			name:  "ordering function",
			input: `sort(up)`,
			want:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := parseCall(t, tc.input)
			require.Equal(t, tc.want, call{c}.staysWithinDownstream(labelNames))
		})
	}
}
