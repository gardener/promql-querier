// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package plan

import (
	"fmt"

	"github.com/prometheus/prometheus/promql/parser"
)

var supportedBinaryOperators = map[parser.ItemType]struct{}{
	parser.ADD:        {},
	parser.SUB:        {},
	parser.MUL:        {},
	parser.DIV:        {},
	parser.MOD:        {},
	parser.POW:        {},
	parser.EQLC:       {},
	parser.NEQ:        {},
	parser.LTE:        {},
	parser.LSS:        {},
	parser.GTE:        {},
	parser.GTR:        {},
	parser.LAND:       {},
	parser.LOR:        {},
	parser.LUNLESS:    {},
	parser.ATAN2:      {},
	parser.TRIM_UPPER: {},
	parser.TRIM_LOWER: {},
}

var supportedUnaryOperators = map[parser.ItemType]struct{}{
	parser.ADD: {},
	parser.SUB: {},
}

func isQuerySupported(expr parser.Expr) error {
	switch e := expr.(type) {
	case *parser.VectorSelector,
		*parser.MatrixSelector,
		*parser.NumberLiteral,
		*parser.StringLiteral,
		*parser.ParenExpr,
		*parser.SubqueryExpr:

	case *parser.Call:
		if _, ok := supportedFuncs[e.Func.Name]; !ok {
			return fmt.Errorf("unsupported PromQL function %q", e.Func.Name)
		}

	case *parser.AggregateExpr:
		if _, ok := supportedAggregations[e.Op]; !ok {
			return fmt.Errorf("unsupported PromQL aggregation %q", e.Op)
		}

	case *parser.BinaryExpr:
		if _, ok := supportedBinaryOperators[e.Op]; !ok {
			return fmt.Errorf("unsupported PromQL binary operator %q", e.Op)
		}

	case *parser.UnaryExpr:
		if _, ok := supportedUnaryOperators[e.Op]; !ok {
			return fmt.Errorf("unsupported PromQL unary operator %q", e.Op)
		}

	default:
		return fmt.Errorf("unsupported query node type %T", expr)
	}

	for _, child := range parser.Children(expr) {
		if childExpr, ok := child.(parser.Expr); ok {
			if err := isQuerySupported(childExpr); err != nil {
				return err
			}
		}
	}

	return nil
}

// NewParser returns a PromQL parser with options supported by the PromQL Querier.
func NewParser() parser.Parser {
	opts := parser.Options{EnableExperimentalFunctions: true}
	return parser.NewParser(opts)
}

func mustParseExpr(s string) parser.Expr {
	expr, err := NewParser().ParseExpr(s)
	if err != nil {
		panic(fmt.Sprintf("must parse expr: %v", err))
	}
	return expr
}
