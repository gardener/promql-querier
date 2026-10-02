// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package plan

import (
	"fmt"
	"sort"
	"time"

	promlabels "github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/gardener/promql-querier/labels"
	"github.com/gardener/promql-querier/promql"
)

const (
	// PartitionIdxLabel is the synthetic matcher that carries a partition's index.
	PartitionIdxLabel = "__partition_idx__"

	// EmptyPartitionIdx is the PartitionIdxLabel value for a selector that must resolve to
	// no series, used when an expression has no routes.
	EmptyPartitionIdx = "n/a"
)

type eligibility int

var verdict = struct {
	unset       eligibility
	constant    eligibility
	canPushDown eligibility
	pushDown    eligibility
}{
	unset:       0,
	constant:    1,
	canPushDown: 2,
	pushDown:    3,
}

func (s eligibility) isConstant() bool  { return s == verdict.constant }
func (s eligibility) canPushDown() bool { return s == verdict.canPushDown }
func (s eligibility) isPushDown() bool  { return s == verdict.pushDown }

type modifiers struct {
	rangeDur   time.Duration
	step       time.Duration
	offset     time.Duration
	timestamp  *int64
	startOrEnd parser.ItemType
}

type annotation struct {
	eligibility
	downstreams    []string
	outerModifiers *modifiers
}

type annotatedNode struct {
	annotation
	expr     *parser.Expr
	children []*annotatedNode
}

type annotatedAggregationNode struct {
	annotation
	expr  *parser.Expr
	inner *annotatedNode
	param *annotatedNode
}

// annotatedNode and annotatedAggregationNode are two small utilities that tighten
// the contract on annotated aggregations that the first child is the aggregation
// expression and the second child is the optional parameter, e.g., topk(5, example).
// The caller is expected to make sure it is dealing with an annotated aggregation node.
func (a *annotatedAggregationNode) annotatedNode() *annotatedNode {
	children := []*annotatedNode{a.inner}
	if a.param != nil {
		children = append(children, a.param)
	}
	return &annotatedNode{
		annotation: a.annotation,
		expr:       a.expr,
		children:   children,
	}
}

func (an *annotatedNode) annotatedAggregationNode() *annotatedAggregationNode {
	var param *annotatedNode
	if len(an.children) > 1 {
		param = an.children[1]
	}
	return &annotatedAggregationNode{
		annotation: an.annotation,
		expr:       an.expr,
		inner:      an.children[0],
		param:      param,
	}
}

func annotate(expr parser.Expr, labelSets map[string]labels.LabelSet, labelNames labels.LabelNames) *annotatedNode {
	ann := &annotator{labels: labelSets, labelNames: labelNames}
	annotatedNode := ann.annotate(&expr, nil)
	if annotatedNode.canPushDown() {
		annotatedNode.eligibility = verdict.pushDown
	}

	return annotatedNode
}

func partition(annotatedNode *annotatedNode, labelNames labels.LabelNames, mode executionMode) (parser.Expr, []Partition) {
	var partitions []Partition
	annotatedNode.collectPartitions(&partitions, labelNames, mode)
	return *annotatedNode.expr, partitions
}

func (an *annotatedNode) collectPartitions(partitions *[]Partition, labelNames labels.LabelNames, mode executionMode) {
	if an.isPushDown() {
		if _, ok := (*an.expr).(*parser.BinaryExpr); ok {
			if len(an.downstreams) == 0 {
				*an.expr = emptyVectorSelector()
				return
			}
		}

		idx := len(*partitions)
		partition, syntheticSelector := an.pushDown(idx, labelNames, mode)
		*partitions = append(*partitions, partition)
		*an.expr = syntheticSelector
		return
	}

	c, ok := (*an.expr).(*parser.Call)
	isAbsent := ok && isAbsentCall(c)

	for _, child := range an.children {
		child.collectPartitions(partitions, labelNames, mode)
	}

	if isAbsent {
		// absent and absent_over_time need to strip the __partition_idx__ label from
		// the result if the argument is a synthetic selector from a pushdown.
		child := an.children[0]
		if child.isPushDown() {
			*an.expr = &parser.Call{
				Func: parser.Functions["label_replace"],
				Args: parser.Expressions{
					c,
					&parser.StringLiteral{Val: PartitionIdxLabel},
					&parser.StringLiteral{Val: ""},
					&parser.StringLiteral{Val: ""},
					&parser.StringLiteral{Val: ""},
				},
			}
		}
	}
}

func isAbsentCall(c *parser.Call) bool {
	return c.Func.Name == "absent" || c.Func.Name == "absent_over_time"
}

func (an *annotatedNode) pushDown(idx int, labelNames labels.LabelNames, mode executionMode) (Partition, parser.Expr) {
	expr := *an.expr
	if an.outerModifiers != nil {
		outerRange := an.outerModifiers.rangeDur

		// Downstreams will not accept a range selector against the /api/v1/query_range endpoints,
		// we rewire it as an instant subquery spanning for the time range.
		if mode.isRange {
			outerRange += mode.end.Sub(mode.start)
			mode = executionMode{isRange: false, ts: mode.end}
		}

		// Only outer subqueries need to propagate their range and step to inner expressions.
		switch e := (*an.expr).(type) {
		case *parser.MatrixSelector:
			// If the inner expression is a matrix selector, we widen its range as required by the outer subquery.
			expr = &parser.MatrixSelector{
				VectorSelector: e.VectorSelector,
				Range:          e.Range + outerRange,
			}
		default:
			// If not, we wrap the expression in a subquery with the outer range, step, and time modifiers.
			expr = &parser.SubqueryExpr{
				Expr:           e,
				Range:          outerRange,
				Step:           an.outerModifiers.step,
				OriginalOffset: an.outerModifiers.offset,
				Timestamp:      an.outerModifiers.timestamp,
				StartOrEnd:     an.outerModifiers.startOrEnd,
			}
		}
	}

	syntheticSelector := newSyntheticSelector(*an.expr, idx)
	partition := Partition{
		expr:           expr,
		downstreamExpr: stripLabels(expr, labelNames),
		downstreams:    an.downstreams,
		executionMode:  mode,
	}

	return partition, syntheticSelector
}

// newSyntheticSelector builds a synthetic selector for the given partition index.
// Matrix selectors and subqueries that are pushed need to preserve their range, step
// and time modifiers locally. The range and step tells local evaluation how to
// evaluate the matrix selector or subquery, and the time modifiers tells how to "find"
// the samples since the @ and offset modifiers might alter the sample timestamps:
//
//   - For matrix selectors, the @ and offset modifiers still preserve the original sample
//     timestamps.
//   - For subqueries, the @ and offset modifiers alter the sample timestamps to match
//     the subquery evaluation time.
//
// The @ and offset modifiers on bare vector selectors alter the sample timestamp to
// the evaluation time so we don't need to preserve them locally.
func newSyntheticSelector(expr parser.Expr, idx int) parser.Expr {
	idxMatcher := promlabels.MustNewMatcher(promlabels.MatchEqual, PartitionIdxLabel, fmt.Sprintf("%d", idx))

	switch e := expr.(type) {
	case *parser.MatrixSelector:
		// As per Prometheus docs, it is safe to assume the inner expression is a vector selector.
		innerVectorSelector := e.VectorSelector.(*parser.VectorSelector)
		return &parser.MatrixSelector{
			VectorSelector: &parser.VectorSelector{
				LabelMatchers:  []*promlabels.Matcher{idxMatcher},
				OriginalOffset: innerVectorSelector.OriginalOffset,
				Timestamp:      innerVectorSelector.Timestamp,
				StartOrEnd:     innerVectorSelector.StartOrEnd,
			},
			Range: e.Range,
		}
	case *parser.SubqueryExpr:
		return &parser.SubqueryExpr{
			Expr: &parser.VectorSelector{
				LabelMatchers: []*promlabels.Matcher{idxMatcher},
			},
			Range:          e.Range,
			OriginalOffset: e.OriginalOffset,
			Timestamp:      e.Timestamp,
			StartOrEnd:     e.StartOrEnd,
			Step:           e.Step,
		}
	default:
		return &parser.VectorSelector{
			LabelMatchers: []*promlabels.Matcher{idxMatcher},
		}
	}
}

func emptyVectorSelector() *parser.VectorSelector {
	idxMatcher := promlabels.MustNewMatcher(promlabels.MatchEqual, PartitionIdxLabel, EmptyPartitionIdx)
	return &parser.VectorSelector{
		LabelMatchers: []*promlabels.Matcher{idxMatcher},
	}
}

type annotator struct {
	labels     map[string]labels.LabelSet
	labelNames labels.LabelNames
}

func (a *annotator) annotate(expr *parser.Expr, outerModifiers *modifiers) *annotatedNode {
	switch e := (*expr).(type) {
	case *parser.VectorSelector, *parser.MatrixSelector:
		ann := annotation{
			eligibility:    verdict.canPushDown,
			downstreams:    resolveDownstreams(*expr, a.labels),
			outerModifiers: outerModifiers,
		}
		return &annotatedNode{annotation: ann, expr: expr}

	case *parser.AggregateExpr:
		return a.annotateAggregation(expr, e, outerModifiers)

	case *parser.BinaryExpr:
		return a.annotateBinary(expr, e, outerModifiers)

	case *parser.Call:
		return a.annotateCall(expr, e, outerModifiers)

	case *parser.ParenExpr:
		inner := a.annotate(&e.Expr, outerModifiers)
		ann := annotation{
			eligibility:    inner.eligibility,
			downstreams:    inner.downstreams,
			outerModifiers: outerModifiers,
		}
		return &annotatedNode{annotation: ann, expr: expr, children: []*annotatedNode{inner}}

	case *parser.UnaryExpr:
		inner := a.annotate(&e.Expr, outerModifiers)
		ann := annotation{
			eligibility:    inner.eligibility,
			downstreams:    inner.downstreams,
			outerModifiers: outerModifiers,
		}
		return &annotatedNode{annotation: ann, expr: expr, children: []*annotatedNode{inner}}

	case *parser.SubqueryExpr:
		// Build a fresh modifiers for the inner subquery and combine it with outer subquery
		// modifiers if there are any.
		subquery := modifiers{
			rangeDur:   e.Range,
			step:       e.Step,
			offset:     e.OriginalOffset,
			timestamp:  e.Timestamp,
			startOrEnd: e.StartOrEnd,
		}
		if outerModifiers != nil {
			subquery.rangeDur += outerModifiers.rangeDur
		}

		inner := a.annotate(&e.Expr, &subquery)
		ann := annotation{
			eligibility:    inner.eligibility,
			downstreams:    inner.downstreams,
			outerModifiers: outerModifiers,
		}
		return &annotatedNode{annotation: ann, expr: expr, children: []*annotatedNode{inner}}

	case *parser.NumberLiteral, *parser.StringLiteral:
		ann := annotation{
			eligibility:    verdict.constant,
			outerModifiers: outerModifiers,
		}
		return &annotatedNode{annotation: ann, expr: expr}
	}

	return nil
}

func (a *annotator) annotateAggregation(expr *parser.Expr, aggExpr *parser.AggregateExpr, outerModifiers *modifiers) *annotatedNode {
	inner := a.annotate(&aggExpr.Expr, outerModifiers)
	ann := annotation{downstreams: inner.downstreams, outerModifiers: outerModifiers}

	var param *annotatedNode
	if aggExpr.Param != nil {
		param = a.annotate(&aggExpr.Param, outerModifiers)
	}

	aggNode := &annotatedAggregationNode{annotation: ann, expr: expr, inner: inner, param: param}
	node := aggNode.annotatedNode()
	if inner.isConstant() && (param == nil || param.isConstant()) {
		node.eligibility = verdict.constant
		return node
	}

	// An aggregation parameter can either be a constant or a scalar() call, and the scalar() call never pushes down.
	aggr := aggregation{expr: aggExpr}
	if inner.canPushDown() && (param == nil || param.isConstant()) && aggr.staysWithinDownstream(a.labelNames) {
		node.eligibility = verdict.canPushDown
		return node
	}

	if inner.canPushDown() {
		inner.eligibility = verdict.pushDown
	}

	return node
}

func (a *annotator) annotateBinary(expr *parser.Expr, binExpr *parser.BinaryExpr, outerModifiers *modifiers) *annotatedNode {
	lhs := a.annotate(&binExpr.LHS, outerModifiers)
	rhs := a.annotate(&binExpr.RHS, outerModifiers)

	var nodeDownstreams []string
	switch {
	case lhs.isConstant():
		nodeDownstreams = rhs.downstreams
	case rhs.isConstant():
		nodeDownstreams = lhs.downstreams
	default:
		nodeDownstreams = intersect(lhs.downstreams, rhs.downstreams)
	}

	ann := annotation{
		downstreams:    nodeDownstreams,
		outerModifiers: outerModifiers,
	}
	node := &annotatedNode{annotation: ann, expr: expr, children: []*annotatedNode{lhs, rhs}}

	if lhs.isConstant() && rhs.isConstant() {
		node.eligibility = verdict.constant
		return node
	}

	bin := binary{expr: binExpr}
	if (lhs.isConstant() || lhs.canPushDown()) && (rhs.isConstant() || rhs.canPushDown()) &&
		bin.staysWithinDownstream(a.labelNames, lhs, rhs) {
		node.eligibility = verdict.canPushDown
		return node
	}

	if lhs.canPushDown() {
		lhs.eligibility = verdict.pushDown
	}

	if rhs.canPushDown() {
		rhs.eligibility = verdict.pushDown
	}

	return node
}

func (a *annotator) annotateCall(expr *parser.Expr, callExpr *parser.Call, outerModifiers *modifiers) *annotatedNode {
	children := make([]*annotatedNode, len(callExpr.Args))
	for i := range callExpr.Args {
		children[i] = a.annotate(&callExpr.Args[i], outerModifiers)
	}

	var (
		childrenConstant    = true
		childrenCanPushDown = true

		nodeDownstreams []string
	)

	for _, child := range children {
		if len(child.downstreams) > 0 {
			// Assume there is only one argument with downstreams that carries the expression
			// argument. In PromQL, all functions expect only a vector or matrix selector.
			// Other parameters are either constants or scalar() calls, which do not push down.
			nodeDownstreams = child.downstreams
		}
		if !child.isConstant() {
			childrenConstant = false
			if !child.canPushDown() {
				childrenCanPushDown = false
			}
		}
	}

	ann := annotation{
		downstreams:    nodeDownstreams,
		outerModifiers: outerModifiers,
	}
	node := &annotatedNode{annotation: ann, expr: expr, children: children}

	if childrenConstant {
		node.eligibility = verdict.constant
		return node
	}

	c := call{expr: callExpr}
	if childrenCanPushDown && c.staysWithinDownstream(a.labelNames) {
		node.eligibility = verdict.canPushDown
		return node
	}

	for _, child := range children {
		if child.canPushDown() {
			child.eligibility = verdict.pushDown
		}
	}

	return node
}

func stripLabels(expr parser.Expr, labelNames labels.LabelNames) parser.Expr {
	stripped := mustParseExpr(expr.String())
	parser.Inspect(stripped, func(node parser.Node, _ []parser.Node) error {
		switch e := node.(type) {
		case *parser.VectorSelector:
			e.LabelMatchers = promql.EnsureNonEmptySelector(stripMatchers(e.LabelMatchers, labelNames))
		case *parser.AggregateExpr:
			e.Grouping = stripGrouping(e.Grouping, labelNames)
		case *parser.BinaryExpr:
			if e.VectorMatching != nil {
				e.VectorMatching.MatchingLabels = stripGrouping(e.VectorMatching.MatchingLabels, labelNames)
				e.VectorMatching.Include = stripGrouping(e.VectorMatching.Include, labelNames)
			}
		}
		return nil
	})

	return stripped
}

func stripMatchers(matchers []*promlabels.Matcher, labelNames labels.LabelNames) []*promlabels.Matcher {
	var kept []*promlabels.Matcher
	for _, m := range matchers {
		if !labelNames.Has(m.Name) {
			kept = append(kept, m)
		}
	}
	return kept
}

func stripGrouping(grouping []string, labelNames labels.LabelNames) []string {
	var kept []string
	for _, l := range grouping {
		if !labelNames.Has(l) {
			kept = append(kept, l)
		}
	}
	return kept
}

// intersect returns the keys present in both a and b. It assumes
// both inputs are ascending and returns an ascending result, so chained calls
// stay ordered.
func intersect(a, b []string) []string {
	var result []string
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] < b[j]:
			i++
		case a[i] > b[j]:
			j++
		default:
			result = append(result, a[i])
			i++
			j++
		}
	}
	return result
}

func resolveDownstreams(expr parser.Expr, labelSets map[string]labels.LabelSet) []string {
	var matchers []*promlabels.Matcher
	switch e := expr.(type) {
	case *parser.VectorSelector:
		matchers = e.LabelMatchers
	case *parser.MatrixSelector:
		matchers = e.VectorSelector.(*parser.VectorSelector).LabelMatchers
	}

	var result []string
	for ds, set := range labelSets {
		if !set.Contradicts(matchers) {
			result = append(result, ds)
		}
	}

	sort.Strings(result)
	return result
}
