// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

// Package promql provides helpers for working with PromQL label matchers.
package promql

import (
	"github.com/prometheus/prometheus/model/labels"
)

// EnsureNonEmptySelector returns matchers that select at least one series. If
// the given matchers already contain a non-empty matcher they are returned
// unchanged, otherwise a __name__ != "" matcher is appended.
func EnsureNonEmptySelector(matchers []*labels.Matcher) []*labels.Matcher {
	if HasNonEmptyMatcher(matchers) {
		return matchers
	}

	return append(matchers, &labels.Matcher{Name: "__name__", Type: labels.MatchNotEqual, Value: ""})
}

// HasNonEmptyMatcher reports whether any of the matchers does not match the
// empty string, meaning it constrains the selection to actual series.
func HasNonEmptyMatcher(matchers []*labels.Matcher) bool {
	for _, m := range matchers {
		if !m.Matches("") {
			return true
		}
	}
	return false
}
