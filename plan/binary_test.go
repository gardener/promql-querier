// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package plan

import (
	"testing"

	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/require"

	"github.com/gardener/promql-querier/labels"
)

func parseBinary(t *testing.T, input string) *parser.BinaryExpr {
	t.Helper()
	expr, err := NewParser().ParseExpr(input)
	require.NoError(t, err)
	bin, ok := expr.(*parser.BinaryExpr)
	require.True(t, ok)
	return bin
}

func TestBinaryPreservesLabelNames(t *testing.T) {
	labelNames := labels.NewLabelNames("env", "region")

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "on includes all virtual labels", input: `foo + on (env, region) bar`, want: true},
		{name: "on missing one virtual label", input: `foo + on (env) bar`, want: false},
		{name: "on with extra non-virtual labels", input: `foo + on (env, region, job) bar`, want: true},
		{name: "ignoring non-virtual label", input: `foo + ignoring (job) bar`, want: true},
		{name: "ignoring virtual label", input: `foo + ignoring (env) bar`, want: false},
		{name: "no matching clause", input: `foo + bar`, want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bin := parseBinary(t, tc.input)
			require.Equal(t, tc.want, binary{bin}.preservesLabelNames(labelNames))
		})
	}
}

func TestBinaryHasOn(t *testing.T) {
	require.True(t, binary{parseBinary(t, `foo + on (env) bar`)}.hasOn())
	require.False(t, binary{parseBinary(t, `foo + ignoring (env) bar`)}.hasOn())
	require.False(t, binary{parseBinary(t, `foo + bar`)}.hasOn())
}

func TestBinaryHasIgnoring(t *testing.T) {
	require.False(t, binary{parseBinary(t, `foo + on (env) bar`)}.hasIgnoring())
	require.True(t, binary{parseBinary(t, `foo + ignoring (env) bar`)}.hasIgnoring())
	require.False(t, binary{parseBinary(t, `foo + bar`)}.hasIgnoring())
}

func TestBinaryHasGroupLeft(t *testing.T) {
	require.True(t, binary{parseBinary(t, `foo + on (env) group_left () bar`)}.hasGroupLeft())
	require.False(t, binary{parseBinary(t, `foo + on (env) group_right () bar`)}.hasGroupLeft())
	require.False(t, binary{parseBinary(t, `foo + on (env) bar`)}.hasGroupLeft())
	require.False(t, binary{parseBinary(t, `foo + bar`)}.hasGroupLeft())
}

func TestBinaryHasGroupRight(t *testing.T) {
	require.False(t, binary{parseBinary(t, `foo + on (env) group_left () bar`)}.hasGroupRight())
	require.True(t, binary{parseBinary(t, `foo + on (env) group_right () bar`)}.hasGroupRight())
	require.False(t, binary{parseBinary(t, `foo + on (env) bar`)}.hasGroupRight())
	require.False(t, binary{parseBinary(t, `foo + bar`)}.hasGroupRight())
}

func TestBinaryStaysWithinDownstream(t *testing.T) {
	labelNames := labels.NewLabelNames("env")

	tests := []struct {
		name  string
		input string
		lhs   annotation
		rhs   annotation
		want  bool
	}{
		{
			name:  "or with same routes",
			input: `foo or bar`,
			lhs:   annotation{downstreams: []string{"http://a:9090"}},
			rhs:   annotation{downstreams: []string{"http://a:9090"}},
			want:  true,
		},
		{
			name:  "or with different routes",
			input: `foo or bar`,
			lhs:   annotation{downstreams: []string{"http://a:9090"}},
			rhs:   annotation{downstreams: []string{"http://b:9090"}},
			want:  false,
		},
		{
			name:  "or with lhs constant",
			input: `foo or bar`,
			lhs:   annotation{downstreams: []string{"http://a:9090"}, eligibility: verdict.constant},
			rhs:   annotation{downstreams: []string{"http://b:9090"}},
			want:  false,
		},
		{
			name:  "unless with same routes",
			input: `foo unless bar`,
			lhs:   annotation{downstreams: []string{"http://a:9090"}},
			rhs:   annotation{downstreams: []string{"http://a:9090"}},
			want:  true,
		},
		{
			name:  "unless with different routes",
			input: `foo unless bar`,
			lhs:   annotation{downstreams: []string{"http://a:9090"}},
			rhs:   annotation{downstreams: []string{"http://b:9090"}},
			want:  false,
		},
		{
			name:  "unless with rhs constant",
			input: `foo unless bar`,
			lhs:   annotation{downstreams: []string{"http://a:9090"}},
			rhs:   annotation{downstreams: []string{"http://b:9090"}, eligibility: verdict.constant},
			want:  true,
		},
		{
			name:  "unless with lhs constant",
			input: `foo unless bar`,
			lhs:   annotation{downstreams: []string{"http://a:9090"}, eligibility: verdict.constant},
			rhs:   annotation{downstreams: []string{"http://a:9090"}},
			want:  false,
		},
		{
			name:  "and",
			input: `foo and bar`,
			lhs:   annotation{downstreams: []string{"http://a:9090"}},
			rhs:   annotation{downstreams: []string{"http://b:9090"}},
			want:  true,
		},
		{
			name:  "and with rhs constant",
			input: `foo and bar`,
			lhs:   annotation{downstreams: []string{"http://a:9090"}},
			rhs:   annotation{downstreams: []string{"http://b:9090"}, eligibility: verdict.constant},
			want:  true,
		},
		{
			name:  "and with lhs constant",
			input: `foo and bar`,
			lhs:   annotation{downstreams: []string{"http://a:9090"}, eligibility: verdict.constant},
			rhs:   annotation{downstreams: []string{"http://b:9090"}},
			want:  false,
		},
		{
			name:  "and ignoring virtual label",
			input: `foo and ignoring (env) bar`,
			lhs:   annotation{downstreams: []string{"http://a:9090"}},
			rhs:   annotation{downstreams: []string{"http://b:9090"}},
			want:  false,
		},
		{
			name:  "arithmetic with no matching clause",
			input: `foo + bar`,
			lhs:   annotation{downstreams: []string{"http://a:9090"}},
			rhs:   annotation{downstreams: []string{"http://b:9090"}},
			want:  true,
		},
		{
			name:  "arithmetic on missing virtual label",
			input: `foo + on (job) bar`,
			lhs:   annotation{downstreams: []string{"http://a:9090"}},
			rhs:   annotation{downstreams: []string{"http://b:9090"}},
			want:  false,
		},
		{
			name:  "arithmetic with rhs constant and group_left",
			input: `foo + on (job) group_left () bar`,
			lhs:   annotation{downstreams: []string{"http://a:9090"}},
			rhs:   annotation{downstreams: []string{"http://b:9090"}, eligibility: verdict.constant},
			want:  true,
		},
		{
			name:  "arithmetic with lhs constant and group_right",
			input: `foo + on (job) group_right () bar`,
			lhs:   annotation{downstreams: []string{"http://a:9090"}, eligibility: verdict.constant},
			rhs:   annotation{downstreams: []string{"http://b:9090"}},
			want:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bin := parseBinary(t, tc.input)
			lhs := &annotatedNode{annotation: tc.lhs}
			rhs := &annotatedNode{annotation: tc.rhs}
			require.Equal(t, tc.want, binary{bin}.staysWithinDownstream(labelNames, lhs, rhs))
		})
	}
}
