// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package plan

import (
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/gardener/promql-querier/labels"
)

type call struct {
	expr *parser.Call
}

func preserves(*parser.Call, labels.LabelNames) bool    { return true }
func notPreserves(*parser.Call, labels.LabelNames) bool { return false }

func labelReplacePreserves(c *parser.Call, labelNames labels.LabelNames) bool {
	// At this point we can assume we have a correct label_replace() call, so we can safely access the arguments.
	dst := c.Args[1].(*parser.StringLiteral)
	src := c.Args[3].(*parser.StringLiteral)
	if labelNames.Has(dst.Val) || labelNames.Has(src.Val) {
		return false
	}
	return true
}

func labelJoinPreserves(c *parser.Call, labelNames labels.LabelNames) bool {
	// At this point we can assume we have a correct label_join() call, so we can safely access the arguments.
	dst := c.Args[1].(*parser.StringLiteral)
	if labelNames.Has(dst.Val) {
		return false
	}

	for _, arg := range c.Args[3:] {
		src := arg.(*parser.StringLiteral)
		if labelNames.Has(src.Val) {
			return false
		}
	}

	return true
}

func preservesWithArgument(c *parser.Call, _ labels.LabelNames) bool {
	return len(c.Args) > 0
}

var (
	supportedFuncs = map[string]struct {
		preservesLabelNames func(*parser.Call, labels.LabelNames) bool
		ordering            bool
	}{
		"abs":                          {preserves, false},
		"absent":                       {notPreserves, false},
		"absent_over_time":             {notPreserves, false},
		"acos":                         {preserves, false},
		"acosh":                        {preserves, false},
		"asin":                         {preserves, false},
		"asinh":                        {preserves, false},
		"atan":                         {preserves, false},
		"atanh":                        {preserves, false},
		"avg_over_time":                {preserves, false},
		"ceil":                         {preserves, false},
		"changes":                      {preserves, false},
		"clamp":                        {preserves, false},
		"clamp_max":                    {preserves, false},
		"clamp_min":                    {preserves, false},
		"cos":                          {preserves, false},
		"cosh":                         {preserves, false},
		"count_over_time":              {preserves, false},
		"day_of_month":                 {preservesWithArgument, false},
		"day_of_week":                  {preservesWithArgument, false},
		"day_of_year":                  {preservesWithArgument, false},
		"days_in_month":                {preservesWithArgument, false},
		"deg":                          {preserves, false},
		"delta":                        {preserves, false},
		"deriv":                        {preserves, false},
		"double_exponential_smoothing": {preserves, false},
		"end":                          {notPreserves, false},
		"exp":                          {preserves, false},
		"first_over_time":              {preserves, false},
		"floor":                        {preserves, false},
		"histogram_fraction":           {preserves, false},
		"histogram_quantile":           {preserves, false},
		"histogram_quantiles":          {preserves, false},
		"hour":                         {preservesWithArgument, false},
		"idelta":                       {preserves, false},
		"increase":                     {preserves, false},
		"irate":                        {preserves, false},
		"label_join":                   {labelJoinPreserves, false},
		"label_replace":                {labelReplacePreserves, false},
		"last_over_time":               {preserves, false},
		"ln":                           {preserves, false},
		"log10":                        {preserves, false},
		"log2":                         {preserves, false},
		"mad_over_time":                {preserves, false},
		"max_over_time":                {preserves, false},
		"min_over_time":                {preserves, false},
		"minute":                       {preservesWithArgument, false},
		"month":                        {preservesWithArgument, false},
		"pi":                           {notPreserves, false},
		"predict_linear":               {preserves, false},
		"present_over_time":            {preserves, false},
		"quantile_over_time":           {preserves, false},
		"rad":                          {preserves, false},
		"range":                        {notPreserves, false},
		"rate":                         {preserves, false},
		"resets":                       {preserves, false},
		"round":                        {preserves, false},
		"scalar":                       {notPreserves, false},
		"sgn":                          {preserves, false},
		"sin":                          {preserves, false},
		"sinh":                         {preserves, false},
		"sort":                         {preserves, true},
		"sort_by_label":                {preserves, true},
		"sort_by_label_desc":           {preserves, true},
		"sort_desc":                    {preserves, true},
		"sqrt":                         {preserves, false},
		"start":                        {notPreserves, false},
		"stddev_over_time":             {preserves, false},
		"stdvar_over_time":             {preserves, false},
		"step":                         {notPreserves, false},
		"sum_over_time":                {preserves, false},
		"tan":                          {preserves, false},
		"tanh":                         {preserves, false},
		"time":                         {notPreserves, false},
		"timestamp":                    {preserves, false},
		"ts_of_first_over_time":        {preserves, false},
		"ts_of_last_over_time":         {preserves, false},
		"ts_of_max_over_time":          {preserves, false},
		"ts_of_min_over_time":          {preserves, false},
		"vector":                       {notPreserves, false},
		"year":                         {preservesWithArgument, false},
	}

	// The list of known unsupported functions is only used in unit tests to make sure
	// The PromQL Querier is aware of all Prometheus functions.
	unsupportedFuncs = map[string]struct{}{
		"histogram_avg":    {},
		"histogram_count":  {},
		"histogram_stddev": {},
		"histogram_stdvar": {},
		"histogram_sum":    {},
		"info":             {},
		"max_of":           {},
		"min_of":           {},
		"start_timestamp":  {},
	}
)

func (c call) staysWithinDownstream(labelNames labels.LabelNames) bool {
	fn, ok := supportedFuncs[c.expr.Func.Name]
	if !ok {
		return false
	}

	return !fn.ordering && fn.preservesLabelNames(c.expr, labelNames)
}
