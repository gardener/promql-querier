// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package plan

import (
	"testing"

	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/require"
)

func TestIsQuerySupported(t *testing.T) {
	tests := []struct {
		name    string
		expr    parser.Expr
		wantErr string
	}{
		{
			name:    "unsupported function",
			expr:    &parser.Call{Func: &parser.Function{Name: "made_up"}},
			wantErr: `unsupported PromQL function "made_up"`,
		},
		{
			name: "unsupported aggregation",
			expr: &parser.AggregateExpr{
				Op:   parser.ItemType(0),
				Expr: &parser.VectorSelector{Name: "up"},
			},
			wantErr: `unsupported PromQL aggregation "<Item 0>"`,
		},
		{
			name: "unsupported binary operator",
			expr: &parser.BinaryExpr{
				Op:  parser.ItemType(0),
				LHS: &parser.VectorSelector{Name: "up"},
				RHS: &parser.VectorSelector{Name: "up"},
			},
			wantErr: `unsupported PromQL binary operator "<Item 0>"`,
		},
		{
			name: "unsupported unary operator",
			expr: &parser.UnaryExpr{
				Op:   parser.MUL,
				Expr: &parser.VectorSelector{Name: "up"},
			},
			wantErr: `unsupported PromQL unary operator "*"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.EqualError(t, isQuerySupported(tc.expr), tc.wantErr)
		})
	}
}

func TestKnownFuncsCoversPromQL(t *testing.T) {
	for name := range parser.Functions {
		_, supported := supportedFuncs[name]
		_, unsupported := unsupportedFuncs[name]
		require.True(t, supported || unsupported, "PromQL function %q is unknown", name)
	}
}

func TestKnownAggregationsCoversPromQL(t *testing.T) {
	for item, name := range parser.ItemTypeStr {
		if item.IsAggregator() || item.IsExperimentalAggregator() {
			_, supported := supportedAggregations[item]
			_, unsupported := unsupportedAggregations[item]
			require.True(t, supported || unsupported, "PromQL aggregation %q is unknown", name)
		}
	}
}
