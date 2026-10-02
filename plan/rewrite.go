// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package plan

import (
	"slices"

	promlabels "github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/gardener/promql-querier/labels"
)

func rewriteAggregations(node *annotatedNode, labelNames labels.LabelNames) {
	for _, child := range node.children {
		rewriteAggregations(child, labelNames)
	}

	agg, ok := (*node.expr).(*parser.AggregateExpr)
	if !ok {
		return
	}

	aggNode := node.annotatedAggregationNode()
	inner := aggNode.inner
	if !inner.isPushDown() {
		return
	}

	param := aggNode.param
	if param != nil && !param.isConstant() {
		return
	}

	if decomposable, ok := supportedAggregations[agg.Op]; !ok || !decomposable {
		return
	}

	if agg.Op == parser.AVG {
		rewriteAvg(node, agg, inner, labelNames)
		return
	}

	*inner.expr = innerAggregation(agg.Op, *inner.expr, agg, labelNames)
	agg.Op = outerAggregation(agg.Op)
}

// rewriteAbsent reduces the data an absent boundary pulls from downstreams.
//
//	absent(foo)              -> absent(group by (<vl>) (foo))
//	absent_over_time(foo[X]) -> absent(group by (<vl>) (present_over_time(foo[X])))
//
// It wraps equality matchers in label_replace calls to re-inject them in the output.
func rewriteAbsent(node *annotatedNode, labelNames labels.LabelNames) {
	for _, child := range node.children {
		rewriteAbsent(child, labelNames)
	}

	c, ok := (*node.expr).(*parser.Call)
	if !ok || !isAbsentCall(c) {
		return
	}

	arg := node.children[0]
	if !arg.isPushDown() {
		return
	}

	rewrittenArg := *arg.expr
	if c.Func.Name == "absent_over_time" {
		rewrittenArg = &parser.Call{
			Func: parser.Functions["present_over_time"],
			Args: parser.Expressions{*arg.expr},
		}
	}

	rewrittenArg = &parser.AggregateExpr{
		Op:       parser.GROUP,
		Expr:     rewrittenArg,
		Grouping: labelNames.Sorted(),
	}

	eqMatchers := extractEqualityMatchers(*arg.expr)

	c.Func = parser.Functions["absent"]
	*arg.expr = rewrittenArg
	arg.eligibility = verdict.pushDown
	arg.children = nil

	injectEqualityMatchers(node, eqMatchers)
}

// absent and absent_over_time only inject the equality matchers if the
// argument is a vector or matrix selector.
func extractEqualityMatchers(expr parser.Expr) []*promlabels.Matcher {
	var vs *parser.VectorSelector
	switch e := expr.(type) {
	case *parser.VectorSelector:
		vs = e
	case *parser.MatrixSelector:
		vs = e.VectorSelector.(*parser.VectorSelector)
	default:
		return nil
	}

	var result []*promlabels.Matcher
	for _, m := range vs.LabelMatchers {
		if m.Type == promlabels.MatchEqual && m.Name != "__name__" {
			result = append(result, m)
		}
	}

	return result
}

func injectEqualityMatchers(node *annotatedNode, eqMatchers []*promlabels.Matcher) {
	for _, m := range eqMatchers {
		replace := &parser.Call{
			Func: parser.Functions["label_replace"],
			Args: parser.Expressions{
				*node.expr,
				&parser.StringLiteral{Val: m.Name},
				&parser.StringLiteral{Val: m.Value},
				&parser.StringLiteral{Val: ""},
				&parser.StringLiteral{Val: ""},
			},
		}

		inner := *node
		inner.expr = &replace.Args[0]

		var replaceExpr parser.Expr = replace
		*node = annotatedNode{
			annotation: node.annotation,
			expr:       &replaceExpr,
			children:   []*annotatedNode{&inner},
		}

		node.eligibility = verdict.unset
	}
}

func rewriteAvg(node *annotatedNode, agg *parser.AggregateExpr, inner *annotatedNode, labelNames labels.LabelNames) {
	innerSum := innerAggregation(parser.SUM, *inner.expr, agg, labelNames)
	innerCount := innerAggregation(parser.COUNT, *inner.expr, agg, labelNames)
	outerSum := &parser.AggregateExpr{
		Op:       parser.SUM,
		Expr:     innerSum,
		Grouping: agg.Grouping,
		Without:  agg.Without,
	}
	outerCount := &parser.AggregateExpr{
		Op:       parser.SUM,
		Expr:     innerCount,
		Grouping: agg.Grouping,
		Without:  agg.Without,
	}
	bin := &parser.BinaryExpr{
		Op:  parser.DIV,
		LHS: outerSum,
		RHS: outerCount,
		VectorMatching: &parser.VectorMatching{
			Card: parser.CardOneToOne,
		},
	}
	paren := &parser.ParenExpr{Expr: bin}

	*node.expr = paren

	innerSumNode := &annotatedNode{
		annotation: annotation{eligibility: verdict.pushDown, downstreams: inner.downstreams, outerModifiers: inner.outerModifiers},
		expr:       &outerSum.Expr,
	}
	innerCountNode := &annotatedNode{
		annotation: annotation{eligibility: verdict.pushDown, downstreams: inner.downstreams, outerModifiers: inner.outerModifiers},
		expr:       &outerCount.Expr,
	}
	outerSumNode := &annotatedNode{
		annotation: annotation{},
		expr:       &bin.LHS,
		children:   []*annotatedNode{innerSumNode},
	}
	outerCountNode := &annotatedNode{
		annotation: annotation{},
		expr:       &bin.RHS,
		children:   []*annotatedNode{innerCountNode},
	}
	binNode := &annotatedNode{
		annotation: annotation{},
		expr:       &paren.Expr,
		children:   []*annotatedNode{outerSumNode, outerCountNode},
	}

	node.eligibility = verdict.unset
	node.children = []*annotatedNode{binNode}
}

func outerAggregation(op parser.ItemType) parser.ItemType {
	if op == parser.COUNT {
		return parser.SUM
	}
	return op
}

// innerAggregation returns only the parser expression, leaving the
// annotated subtree mismatched, which is safe because, once pushed
// down, the corresponding annotated subtree is not recursed anymore.
func innerAggregation(op parser.ItemType, operand parser.Expr, agg *parser.AggregateExpr, labelNames labels.LabelNames) *parser.AggregateExpr {
	return &parser.AggregateExpr{
		Op:       op,
		Expr:     operand,
		Param:    agg.Param,
		Grouping: groupingWithLabelNames(agg, labelNames),
		Without:  agg.Without,
	}
}

func groupingWithLabelNames(agg *parser.AggregateExpr, labelNames labels.LabelNames) []string {
	if agg.Without {
		return ensureMissingLabelNames(agg, labelNames)
	}
	return ensurePresentLabelNames(agg, labelNames)
}

func ensureMissingLabelNames(agg *parser.AggregateExpr, labelNames labels.LabelNames) []string {
	var grouping []string
	for _, label := range agg.Grouping {
		if !labelNames.Has(label) {
			grouping = append(grouping, label)
		}
	}
	return grouping
}

func ensurePresentLabelNames(agg *parser.AggregateExpr, labelNames labels.LabelNames) []string {
	grouping := slices.Clone(agg.Grouping)
	for _, l := range labelNames.Sorted() {
		if !slices.Contains(grouping, l) {
			grouping = append(grouping, l)
		}
	}
	return grouping
}
