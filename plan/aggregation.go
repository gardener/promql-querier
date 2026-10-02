// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package plan

import (
	"slices"

	"github.com/prometheus/prometheus/promql/parser"

	"github.com/gardener/promql-querier/labels"
)

type aggregation struct {
	expr *parser.AggregateExpr
}

// the boolean indicates whether the aggregation is decomposable or not.
var supportedAggregations = map[parser.ItemType]bool{
	parser.SUM:          true,
	parser.AVG:          true,
	parser.COUNT:        true,
	parser.MIN:          true,
	parser.MAX:          true,
	parser.GROUP:        true,
	parser.TOPK:         true,
	parser.BOTTOMK:      true,
	parser.LIMITK:       true,
	parser.STDDEV:       false,
	parser.STDVAR:       false,
	parser.QUANTILE:     false,
	parser.COUNT_VALUES: false,
}

// The list of known unsupported aggregations is only used in unit tests to make sure
// The PromQL Querier is aware of all Prometheus aggregations. This is only used for
// testing during dependency updates.
var unsupportedAggregations = map[parser.ItemType]struct{}{
	parser.LIMIT_RATIO: {},
}

func (a aggregation) hasBy() bool {
	return !a.expr.Without
}

func (a aggregation) staysWithinDownstream(labelNames labels.LabelNames) bool {
	if a.expr.Op == parser.COUNT_VALUES {
		return false
	}

	if a.hasBy() {
		for l := range labelNames {
			if !slices.Contains(a.expr.Grouping, l) {
				return false
			}
		}
	} else {
		for l := range labelNames {
			if slices.Contains(a.expr.Grouping, l) {
				return false
			}
		}
	}

	return true
}
