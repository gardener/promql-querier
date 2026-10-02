// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

// Package labels models label sets and names used to determine push-down
// eligibility and to route queries across downstreams.
package labels

import (
	"fmt"
	"slices"
	"strings"

	promlabels "github.com/prometheus/prometheus/model/labels"
)

// LabelSet maps label names to their values for a single downstream.
type LabelSet map[string]string

// Contradicts returns true if the label set contradicts the given Prometheus matchers.
func (s LabelSet) Contradicts(matchers []*promlabels.Matcher) bool {
	for _, m := range matchers {
		v, ok := s[m.Name]
		if ok && !m.Matches(v) {
			return true
		}
	}
	return false
}

// String formats the set as a Prometheus-style selector with sorted keys, for
// example {env="prod",region="eu"}. Sorting keeps the output deterministic
// despite map iteration order.
func (s LabelSet) String() string {
	keys := make([]string, 0, len(s))
	for k := range s {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%q", k, s[k])
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// LabelNames is a set of label names.
type LabelNames map[string]struct{}

// NewLabelNames builds a LabelNames set from the given names.
func NewLabelNames(names ...string) LabelNames {
	n := make(LabelNames, len(names))
	for _, name := range names {
		n[name] = struct{}{}
	}
	return n
}

// Has reports whether the set contains the given label name.
func (n LabelNames) Has(label string) bool {
	_, ok := n[label]
	return ok
}

// Sorted returns the label names in ascending order.
func (n LabelNames) Sorted() []string {
	s := make([]string, 0, len(n))
	for l := range n {
		s = append(s, l)
	}
	slices.Sort(s)
	return s
}

// UnionLabelNames returns the set of all label names appearing across the given
// label sets.
func UnionLabelNames(sets map[string]LabelSet) LabelNames {
	names := make(LabelNames)
	for _, ls := range sets {
		for k := range ls {
			names[k] = struct{}{}
		}
	}
	return names
}

// ExpandLabelSets returns a copy of label sets where every set carries the union of
// all label names across the sets. Any missing name in a set is filled with the empty
// string. After ExpandLabelSets all label sets carry the same label names.
func ExpandLabelSets(sets map[string]LabelSet) map[string]LabelSet {
	names := UnionLabelNames(sets)
	result := make(map[string]LabelSet, len(sets))
	for key, ls := range sets {
		expanded := make(LabelSet, len(names))
		for name := range names {
			expanded[name] = ls[name]
		}
		result[key] = expanded
	}
	return result
}
