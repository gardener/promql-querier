// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package labels

import (
	"testing"

	promlabels "github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func TestLabelSetContradicts(t *testing.T) {
	tests := []struct {
		name     string
		input    LabelSet
		matchers []*promlabels.Matcher
		want     bool
	}{
		{
			name:     "no matchers never contradict",
			input:    LabelSet{"env": "prod"},
			matchers: nil,
			want:     false,
		},
		{
			name:     "satisfied equal matcher does not contradict",
			input:    LabelSet{"env": "prod"},
			matchers: []*promlabels.Matcher{promlabels.MustNewMatcher(promlabels.MatchEqual, "env", "prod")},
			want:     false,
		},
		{
			name:     "unsatisfied equal matcher contradicts",
			input:    LabelSet{"env": "prod"},
			matchers: []*promlabels.Matcher{promlabels.MustNewMatcher(promlabels.MatchEqual, "env", "staging")},
			want:     true,
		},
		{
			name:  "any unsatisfied matcher contradicts",
			input: LabelSet{"env": "prod", "region": "eu"},
			matchers: []*promlabels.Matcher{
				promlabels.MustNewMatcher(promlabels.MatchEqual, "env", "prod"),
				promlabels.MustNewMatcher(promlabels.MatchEqual, "region", "us"),
			},
			want: true,
		},
		{
			name:     "matcher on absent label never contradicts",
			input:    LabelSet{"env": "prod"},
			matchers: []*promlabels.Matcher{promlabels.MustNewMatcher(promlabels.MatchEqual, "region", "eu")},
			want:     false,
		},
		{
			name:  "unsatisfied present matcher contradicts despite an absent label matcher",
			input: LabelSet{"env": "prod"},
			matchers: []*promlabels.Matcher{
				promlabels.MustNewMatcher(promlabels.MatchEqual, "region", "eu"),
				promlabels.MustNewMatcher(promlabels.MatchEqual, "env", "staging"),
			},
			want: true,
		},
		{
			name:     "satisfied not-equal matcher does not contradict",
			input:    LabelSet{"env": "prod"},
			matchers: []*promlabels.Matcher{promlabels.MustNewMatcher(promlabels.MatchNotEqual, "env", "staging")},
			want:     false,
		},
		{
			name:     "unsatisfied not-equal matcher contradicts",
			input:    LabelSet{"env": "prod"},
			matchers: []*promlabels.Matcher{promlabels.MustNewMatcher(promlabels.MatchNotEqual, "env", "prod")},
			want:     true,
		},
		{
			name:     "satisfied regex matcher does not contradict",
			input:    LabelSet{"env": "prod"},
			matchers: []*promlabels.Matcher{promlabels.MustNewMatcher(promlabels.MatchRegexp, "env", "pro.*")},
			want:     false,
		},
		{
			name:     "unsatisfied regex matcher contradicts",
			input:    LabelSet{"env": "prod"},
			matchers: []*promlabels.Matcher{promlabels.MustNewMatcher(promlabels.MatchRegexp, "env", "stag.*")},
			want:     true,
		},
		{
			name:     "equal matcher contradicts a present empty value",
			input:    LabelSet{"region": ""},
			matchers: []*promlabels.Matcher{promlabels.MustNewMatcher(promlabels.MatchEqual, "region", "eu")},
			want:     true,
		},
		{
			name:     "empty-value equal matcher does not contradict a present empty value",
			input:    LabelSet{"region": ""},
			matchers: []*promlabels.Matcher{promlabels.MustNewMatcher(promlabels.MatchEqual, "region", "")},
			want:     false,
		},
		{
			name:     "negative matcher does not contradict a present empty value",
			input:    LabelSet{"region": ""},
			matchers: []*promlabels.Matcher{promlabels.MustNewMatcher(promlabels.MatchNotEqual, "region", "eu")},
			want:     false,
		},
		{
			name:     "not-equal matcher contradicts a present empty value",
			input:    LabelSet{"env": ""},
			matchers: []*promlabels.Matcher{promlabels.MustNewMatcher(promlabels.MatchNotEqual, "env", "")},
			want:     true,
		},
		{
			name:     "not-equal matcher on absent label never contradicts",
			input:    LabelSet{"region": "eu"},
			matchers: []*promlabels.Matcher{promlabels.MustNewMatcher(promlabels.MatchNotEqual, "env", "")},
			want:     false,
		},
		{
			name:     "empty label set never contradicts",
			input:    LabelSet{},
			matchers: []*promlabels.Matcher{promlabels.MustNewMatcher(promlabels.MatchEqual, "env", "prod")},
			want:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.input.Contradicts(tc.matchers))
		})
	}
}

func TestLabelSetString(t *testing.T) {
	tests := []struct {
		name  string
		input LabelSet
		want  string
	}{
		{
			name:  "empty",
			input: LabelSet{},
			want:  "{}",
		},
		{
			name:  "single label",
			input: LabelSet{"region": "eu"},
			want:  `{region="eu"}`,
		},
		{
			name:  "keys are sorted",
			input: LabelSet{"region": "eu", "env": "prod"},
			want:  `{env="prod",region="eu"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.input.String())
		})
	}
}

func TestUnionLabelNames(t *testing.T) {
	tests := []struct {
		name  string
		input map[string]LabelSet
		want  LabelNames
	}{
		{
			name:  "empty",
			input: map[string]LabelSet{},
			want:  LabelNames{},
		},
		{
			name:  "single label set",
			input: map[string]LabelSet{"a": {"env": "prod", "region": "eu"}},
			want:  LabelNames{"env": {}, "region": {}},
		},
		{
			name:  "overlapping labels across sets",
			input: map[string]LabelSet{"a": {"env": "prod"}, "b": {"env": "staging", "region": "eu"}, "c": {"planet": "earth"}},
			want:  LabelNames{"env": {}, "region": {}, "planet": {}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, UnionLabelNames(tc.input))
		})
	}
}

func TestLabelNamesHas(t *testing.T) {
	names := NewLabelNames("env", "region")

	require.True(t, names.Has("env"))
	require.True(t, names.Has("region"))
	require.False(t, names.Has("job"))
	require.False(t, names.Has(""))

	names = NewLabelNames("env", "region", "")
	require.True(t, names.Has(""))
}

func TestExpandLabelSets(t *testing.T) {
	tests := []struct {
		name  string
		input map[string]LabelSet
		want  map[string]LabelSet
	}{
		{
			name:  "empty",
			input: map[string]LabelSet{},
			want:  map[string]LabelSet{},
		},
		{
			name:  "already complete sets unchanged",
			input: map[string]LabelSet{"a": {"env": "prod", "region": "eu"}},
			want:  map[string]LabelSet{"a": {"env": "prod", "region": "eu"}},
		},
		{
			name: "missing label filled with empty string",
			input: map[string]LabelSet{
				"a": {"env": "prod", "region": "eu"},
				"b": {"env": "prod"},
				"c": {"planet": "earth"},
			},
			want: map[string]LabelSet{
				"a": {"env": "prod", "region": "eu", "planet": ""},
				"b": {"env": "prod", "region": "", "planet": ""},
				"c": {"env": "", "region": "", "planet": "earth"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := ExpandLabelSets(tc.input)
			require.Equal(t, tc.want, result)
		})
	}
}
