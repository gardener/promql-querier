// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package plan

import (
	"slices"

	"github.com/prometheus/prometheus/promql/parser"

	"github.com/gardener/promql-querier/labels"
)

type binary struct {
	expr *parser.BinaryExpr
}

func (b binary) preservesLabelNames(labelNames labels.LabelNames) bool {
	if b.hasOn() {
		for l := range labelNames {
			if !slices.Contains(b.expr.VectorMatching.MatchingLabels, l) {
				return false
			}
		}
		return true
	}

	if b.hasIgnoring() {
		for _, l := range b.expr.VectorMatching.MatchingLabels {
			if labelNames.Has(l) {
				return false
			}
		}
	}

	return true
}

func (b binary) hasOn() bool {
	return b.expr.VectorMatching != nil && b.expr.VectorMatching.MatchingLabels != nil && b.expr.VectorMatching.On
}

func (b binary) hasIgnoring() bool {
	return b.expr.VectorMatching != nil && b.expr.VectorMatching.MatchingLabels != nil && !b.expr.VectorMatching.On
}

func (b binary) hasGroupLeft() bool {
	return b.expr.VectorMatching != nil && b.expr.VectorMatching.Card == parser.CardManyToOne
}

func (b binary) hasGroupRight() bool {
	return b.expr.VectorMatching != nil && b.expr.VectorMatching.Card == parser.CardOneToMany
}

func (b binary) staysWithinDownstream(labelNames labels.LabelNames, lhs, rhs *annotatedNode) bool {
	lhsConstant := lhs.isConstant()
	rhsConstant := rhs.isConstant()

	switch b.expr.Op {
	case parser.LOR:
		// LOR stays within a downstream only if labels are preserved, both sides have the same downstreams, and neither side
		// is a constant. A constant on either side must cross downstreams, because pushing it down could return it for each
		// downstream. Sides with different downstreams must cross too, so that LOR behaves as a union.
		return b.preservesLabelNames(labelNames) && slices.Equal(lhs.downstreams, rhs.downstreams) &&
			!lhsConstant && !rhsConstant
	case parser.LUNLESS:
		// LUNLESS stays within a downstream only if labels are preserved and the left side is not a constant. A constant on
		// the left side must cross downstreams, because pushing it down returns the same constant for each downstream. The
		// right side only removes matching elements from the left side, so a constant on the right side stays within a
		// downstream. A non-constant right side with different downstreams must cross too, so that LUNLESS returns
		// the left side without routing intersecting both sides.
		return b.preservesLabelNames(labelNames) && (rhsConstant || slices.Equal(lhs.downstreams, rhs.downstreams)) &&
			!lhsConstant
	case parser.LAND:
		// LAND stays within a downstream only if labels are preserved and the left side is not a constant. A constant on
		// the left side must cross downstreams, because pushing it down returns the same constant for each downstream.
		return b.preservesLabelNames(labelNames) && !lhsConstant
	default:
		return b.preservesLabelNames(labelNames) ||
			(lhsConstant && b.hasGroupRight()) ||
			(rhsConstant && b.hasGroupLeft())
	}
}
