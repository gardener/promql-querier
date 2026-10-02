// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package fanout

import (
	"context"
	"fmt"
	"strconv"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/util/annotations"

	"github.com/gardener/promql-querier/plan"
)

type fetchedSeries [][]promql.Series

func (fs fetchedSeries) Querier(_, _ int64) (storage.Querier, error) {
	return fs, nil
}

func (fs fetchedSeries) Select(_ context.Context, _ bool, _ *storage.SelectHints, matchers ...*labels.Matcher) storage.SeriesSet {
	var idxStr string
	for _, m := range matchers {
		if m.Name == plan.PartitionIdxLabel && m.Type == labels.MatchEqual {
			idxStr = m.Value
			break
		}
	}
	if idxStr == plan.EmptyPartitionIdx {
		return storage.EmptySeriesSet()
	}

	idx, err := strconv.Atoi(idxStr)
	if err != nil || idx < 0 || idx >= len(fs) {
		return storage.ErrSeriesSet(errUnroutedSelector(idxStr, len(fs)))
	}

	storageSeries := make([]storage.Series, len(fs[idx]))
	for i, s := range fs[idx] {
		storageSeries[i] = promql.NewStorageSeries(s)
	}

	return newSliceSeriesSet(storageSeries)
}

func errUnroutedSelector(idxStr string, partitions int) error {
	return fmt.Errorf("selector reached storage with invalid partition index %q, want a valid index in [0, %d)", idxStr, partitions)
}

func (fs fetchedSeries) LabelValues(_ context.Context, _ string, _ *storage.LabelHints, _ ...*labels.Matcher) ([]string, annotations.Annotations, error) {
	return nil, nil, fmt.Errorf("LabelValues: not implemented")
}

func (fs fetchedSeries) LabelNames(_ context.Context, _ *storage.LabelHints, _ ...*labels.Matcher) ([]string, annotations.Annotations, error) {
	return nil, nil, fmt.Errorf("LabelNames: not implemented")
}

func (fs fetchedSeries) Close() error {
	return nil
}

type sliceSeriesSet struct {
	series []storage.Series
	idx    int
}

func newSliceSeriesSet(series []storage.Series) *sliceSeriesSet {
	return &sliceSeriesSet{series: series, idx: -1}
}

func (s *sliceSeriesSet) Next() bool {
	s.idx++
	return s.idx < len(s.series)
}

func (s *sliceSeriesSet) At() storage.Series {
	return s.series[s.idx]
}

func (s *sliceSeriesSet) Err() error {
	return nil
}

func (s *sliceSeriesSet) Warnings() annotations.Annotations {
	return nil
}
