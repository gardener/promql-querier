// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

// Package plan turns a parsed PromQL expression into an execution plan. It
// annotates the expression with virtual label information, rewrites parts that
// can be pushed down, and splits the query into partitions that the runtime
// issues to the matching downstreams.
package plan

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/prometheus/prometheus/promql/parser"

	"github.com/gardener/promql-querier/labels"
)

type executionMode struct {
	isRange    bool
	ts         time.Time
	start, end time.Time
	step       time.Duration
}

// Plan is an execution plan for a single query. It holds the expression the
// runtime evaluates locally, the partitions it issues to downstreams, and the
// execution mode.
type Plan struct {
	expr       parser.Expr
	partitions []Partition
	mode       executionMode
}

func newPlan(expr parser.Expr, partitions []Partition, mode executionMode) *Plan {
	return &Plan{expr: expr, partitions: partitions, mode: mode}
}

// Expr returns the expression the runtime evaluates locally.
func (p *Plan) Expr() parser.Expr { return p.expr }

// Partitions returns the partitions the runtime issues to downstreams.
func (p *Plan) Partitions() []Partition { return p.partitions }

// IsRange reports whether the plan is a range query.
func (p *Plan) IsRange() bool { return p.mode.isRange }

// Time returns the evaluation time of the instant query.
func (p *Plan) Time() time.Time { return p.mode.ts }

// Start returns the start time of the range query.
func (p *Plan) Start() time.Time { return p.mode.start }

// End returns the end time of the range query.
func (p *Plan) End() time.Time { return p.mode.end }

// Step returns the step of the range query.
func (p *Plan) Step() time.Duration { return p.mode.step }

// PlannerOption configures a Planner.
type PlannerOption func(*Planner)

// WithPlannerLogger sets the logger used by the Planner.
func WithPlannerLogger(logger *slog.Logger) PlannerOption {
	return func(p *Planner) { p.logger = logger }
}

// Planner builds execution plans from parsed PromQL expressions using the known
// downstream label sets and virtual label names.
type Planner struct {
	labels     map[string]labels.LabelSet
	labelNames labels.LabelNames
	logger     *slog.Logger
}

// NewPlanner returns a Planner for the given downstream label sets and virtual
// label names. It applies the supplied options.
func NewPlanner(labelSets map[string]labels.LabelSet, labelNames labels.LabelNames, opts ...PlannerOption) *Planner {
	p := &Planner{
		labels:     labelSets,
		labelNames: labelNames,
		logger:     slog.New(slog.DiscardHandler),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// PlanInstant builds an instant query plan evaluated at ts.
func (p *Planner) PlanInstant(expr parser.Expr, ts time.Time) (*Plan, error) {
	return p.build(expr, executionMode{isRange: false, ts: ts})
}

// PlanRange builds a range query plan over [start, end] with step.
func (p *Planner) PlanRange(expr parser.Expr, start, end time.Time, step time.Duration) (*Plan, error) {
	if t := expr.Type(); t != parser.ValueTypeVector && t != parser.ValueTypeScalar {
		return nil, fmt.Errorf("invalid expression type %q for range query, must be Scalar or instant Vector", parser.DocumentedType(t))
	}
	return p.build(expr, executionMode{isRange: true, start: start, end: end, step: step})
}

func (p *Planner) build(expr parser.Expr, mode executionMode) (*Plan, error) {
	if err := isQuerySupported(expr); err != nil {
		return nil, err
	}

	annotated := annotate(expr, p.labels, p.labelNames)
	rewriteAggregations(annotated, p.labelNames)
	rewriteAbsent(annotated, p.labelNames)

	localExpr, partitions := partition(annotated, p.labelNames, mode)
	plan := newPlan(localExpr, partitions, mode)

	return plan, nil
}
