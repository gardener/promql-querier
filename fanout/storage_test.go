// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package fanout

import (
	"context"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/storage"
	"github.com/stretchr/testify/require"

	"github.com/gardener/promql-querier/plan"
)

func idxMatcher(value string) *labels.Matcher {
	return labels.MustNewMatcher(labels.MatchEqual, plan.PartitionIdxLabel, value)
}

func TestFetchedSeriesSelectReturnsPartitionByIndex(t *testing.T) {
	var (
		idx0 = []promql.Series{{Metric: labels.FromStrings("__name__", "part0")}}
		idx1 = []promql.Series{{Metric: labels.FromStrings("__name__", "part1a")}, {Metric: labels.FromStrings("__name__", "part1b")}}
	)

	fetched := fetchedSeries{idx0, idx1}
	set := fetched.Select(context.Background(), false, nil, idxMatcher("1"))

	var names []string
	for set.Next() {
		names = append(names, set.At().Labels().Get("__name__"))
	}

	require.NoError(t, set.Err())
	require.Equal(t, []string{"part1a", "part1b"}, names)
}

func TestFetchedSeriesSelectEmptyPartitionIdx(t *testing.T) {
	fetched := fetchedSeries{{{Metric: labels.FromStrings("__name__", "up")}}}
	set := fetched.Select(context.Background(), false, nil, idxMatcher(plan.EmptyPartitionIdx))

	require.Equal(t, storage.EmptySeriesSet(), set)
}

func TestFetchedSeriesSelectRejectsBadIndex(t *testing.T) {
	fetched := fetchedSeries{{{Metric: labels.FromStrings("__name__", "up")}}}

	tests := []struct {
		name    string
		matcher *labels.Matcher
		wantMsg string
	}{
		{"non-numeric index", idxMatcher("abc"), `selector reached storage with invalid partition index "abc", want a valid index in [0, 1)`},
		{"negative index", idxMatcher("-1"), `selector reached storage with invalid partition index "-1", want a valid index in [0, 1)`},
		{"index out of range", idxMatcher("5"), `selector reached storage with invalid partition index "5", want a valid index in [0, 1)`},
		{"no index matcher", labels.MustNewMatcher(labels.MatchEqual, "job", "api"), `selector reached storage with invalid partition index "", want a valid index in [0, 1)`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			set := fetched.Select(context.Background(), false, nil, tc.matcher)
			require.EqualError(t, set.Err(), tc.wantMsg)
			require.False(t, set.Next())
		})
	}
}

func TestSliceSeriesSetIteration(t *testing.T) {
	s0 := promql.NewStorageSeries(promql.Series{Metric: labels.FromStrings("__name__", "a")})
	s1 := promql.NewStorageSeries(promql.Series{Metric: labels.FromStrings("__name__", "b")})

	set := newSliceSeriesSet([]storage.Series{s0, s1})

	require.True(t, set.Next())
	require.Equal(t, "a", set.At().Labels().Get("__name__"))
	require.True(t, set.Next())
	require.Equal(t, "b", set.At().Labels().Get("__name__"))
	require.False(t, set.Next())
	require.NoError(t, set.Err())
	require.Nil(t, set.Warnings())
}

func TestSliceSeriesSetEmpty(t *testing.T) {
	set := newSliceSeriesSet(nil)
	require.False(t, set.Next())
	require.NoError(t, set.Err())
}
