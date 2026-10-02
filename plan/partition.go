// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package plan

import (
	"time"

	"github.com/prometheus/prometheus/promql/parser"
)

// Partition carries the exact request the runtime must issue to a downstream.
type Partition struct {
	expr           parser.Expr
	downstreamExpr parser.Expr
	downstreams    []string
	executionMode  executionMode
}

// Expr returns the expression the runtime evaluates locally for this partition.
func (p Partition) Expr() parser.Expr { return p.expr }

// DownstreamExpr returns the expression pushed down to the downstreams.
func (p Partition) DownstreamExpr() parser.Expr { return p.downstreamExpr }

// Downstreams returns the names of the downstreams this partition targets.
func (p Partition) Downstreams() []string { return p.downstreams }

// IsRange reports whether the partition is a range query.
func (p Partition) IsRange() bool { return p.executionMode.isRange }

// Time returns the evaluation time of the instant query.
func (p Partition) Time() time.Time { return p.executionMode.ts }

// Start returns the start time of the range query.
func (p Partition) Start() time.Time { return p.executionMode.start }

// End returns the end time of the range query.
func (p Partition) End() time.Time { return p.executionMode.end }

// Step returns the step of the range query.
func (p Partition) Step() time.Duration { return p.executionMode.step }
