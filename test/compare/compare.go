// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

//go:build compare

// Package compare provides test helpers that run the same query against a
// reference Prometheus and against the PromQL Querier and assert the
// results match.
package compare

import (
	"context"
	"math"
	"sort"
	"testing"
	"time"

	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

// Compare runs queries against both a reference Prometheus API and the
// PromQL Querier and asserts their responses agree.
type Compare struct {
	promqlQuerier promv1.API
	reference     promv1.API
}

// Option configures how a comparison treats the results.
type Option func(*compareOptions)

type compareOptions struct {
	doNotSort     bool
	shouldBeEmpty bool
}

// DoNotSort does not sort the query result for comparison.
func DoNotSort() Option {
	return func(o *compareOptions) { o.doNotSort = true }
}

// ShouldBeEmpty allows the reference result to be empty.
func ShouldBeEmpty() Option {
	return func(o *compareOptions) { o.shouldBeEmpty = true }
}

// NewCompare returns a Compare that checks the PromQL Querier against the given
// reference Prometheus API.
func NewCompare(reference, promqlQuerier promv1.API) *Compare {
	return &Compare{
		promqlQuerier: promqlQuerier,
		reference:     reference,
	}
}

// CompareQuery runs an instant query at ts against both APIs and asserts the
// values match.
func (c *Compare) CompareQuery(t *testing.T, query string, ts int64, opts ...Option) {
	t.Helper()
	var o compareOptions
	for _, opt := range opts {
		opt(&o)
	}

	wantResult, _, err := c.reference.Query(context.Background(), query, time.UnixMilli(ts))
	require.NoError(t, err, "reference query")
	if !o.shouldBeEmpty {
		requireResultNotEmpty(t, wantResult)
	}

	gotResult, _, err := c.promqlQuerier.Query(context.Background(), query, time.UnixMilli(ts))
	require.NoError(t, err, "promql-querier query")
	requireValuesEqual(t, wantResult, gotResult, o.doNotSort)
}

// CompareQueryRange runs a range query over [start, end] with the given step
// against both APIs and asserts the values match.
func (c *Compare) CompareQueryRange(t *testing.T, query string, start, end int64, step int, opts ...Option) {
	t.Helper()
	var o compareOptions
	for _, opt := range opts {
		opt(&o)
	}

	rng := promv1.Range{
		Start: time.UnixMilli(start),
		End:   time.UnixMilli(end),
		Step:  time.Duration(step) * time.Second,
	}

	wantResult, _, err := c.reference.QueryRange(context.Background(), query, rng)
	require.NoError(t, err, "reference query range")
	if !o.shouldBeEmpty {
		requireResultNotEmpty(t, wantResult)
	}

	gotResult, _, err := c.promqlQuerier.QueryRange(context.Background(), query, rng)
	require.NoError(t, err, "promql-querier query range")
	requireValuesEqual(t, wantResult, gotResult, o.doNotSort)
}

// CompareSeries runs a /api/v1/series request against both APIs and asserts the
// returned label sets match.
func (c *Compare) CompareSeries(t *testing.T, matchers []string, start, end time.Time) {
	t.Helper()
	wantResult, _, err := c.reference.Series(context.Background(), matchers, start, end)
	require.NoError(t, err, "reference series")
	require.NotEmpty(t, wantResult, "reference returned no data")
	sortLabelSets(wantResult)

	gotResult, _, err := c.promqlQuerier.Series(context.Background(), matchers, start, end)
	require.NoError(t, err, "promql-querier series")
	sortLabelSets(gotResult)
	require.Equal(t, wantResult, gotResult, "promql-querier mismatch: want %v, got %v", wantResult, gotResult)
}

// CompareSeriesError asserts that the reference and the PromQL Querier both reject
// the same /api/v1/series request.
func (c *Compare) CompareSeriesError(t *testing.T, matchers []string, start, end time.Time) {
	t.Helper()
	_, _, wantErr := c.reference.Series(context.Background(), matchers, start, end)
	require.Error(t, wantErr, "[%s] want error from reference, got nil", matchers)

	_, _, gotErr := c.promqlQuerier.Series(context.Background(), matchers, start, end)
	require.Error(t, gotErr, "[%s] want error from promql-querier, got nil", matchers)
}

// CompareLabelNames runs a /api/v1/labels request against both APIs and asserts
// the returned label names match.
func (c *Compare) CompareLabelNames(t *testing.T, matchers []string, start, end time.Time) {
	t.Helper()
	wantResult, _, err := c.reference.LabelNames(context.Background(), matchers, start, end)
	require.NoError(t, err, "reference label names")
	require.NotEmpty(t, wantResult, "reference returned no data")
	sort.Sort(wantResult)

	gotResult, _, err := c.promqlQuerier.LabelNames(context.Background(), matchers, start, end)
	require.NoError(t, err, "promql-querier label names")
	sort.Sort(gotResult)
	require.Equal(t, wantResult, gotResult, "promql-querier mismatch: want %v, got %v", wantResult, gotResult)
}

// CompareLabelValues runs a /api/v1/label/<name>/values request against both
// APIs and asserts the returned values match.
func (c *Compare) CompareLabelValues(t *testing.T, label string, matchers []string, start, end time.Time) {
	t.Helper()
	wantResult, _, err := c.reference.LabelValues(context.Background(), label, matchers, start, end)
	require.NoError(t, err, "reference label values(%s)", label)
	require.NotEmpty(t, wantResult, "reference returned no data")
	sort.Sort(wantResult)

	gotResult, _, err := c.promqlQuerier.LabelValues(context.Background(), label, matchers, start, end)
	require.NoError(t, err, "promql-querier label values(%s)", label)
	sort.Sort(gotResult)
	require.Equal(t, wantResult, gotResult, "promql-querier mismatch: want %v, got %v", wantResult, gotResult)
}

func sortLabelSets(sets []model.LabelSet) {
	sort.Slice(sets, func(i, j int) bool {
		return sets[i].Before(sets[j])
	})
}

func requireResultNotEmpty(t *testing.T, v model.Value) {
	t.Helper()
	switch val := v.(type) {
	case model.Vector, model.Matrix:
		require.NotEmpty(t, val, "reference returned no data")
	case *model.Scalar:
	case *model.String:
	default:
		t.Fatalf("reference returned unexpected result type %T", val)
	}
}

func almostEqual(a, b float64) bool {
	// valueTolerance is the relative and absolute slop allowed between the reference
	// and PromQL Querier sample values. The PromQL Querier and the reference Prometheus
	// runs floating-point operations in different order, which is not associative in float64.
	const valueTolerance = 1e-12

	if a == b {
		return true
	}
	if math.IsNaN(a) || math.IsNaN(b) {
		return math.IsNaN(a) && math.IsNaN(b)
	}
	if math.IsInf(a, 0) || math.IsInf(b, 0) {
		return (math.IsInf(a, 1) && math.IsInf(b, 1)) || (math.IsInf(a, -1) && math.IsInf(b, -1))
	}
	diff := math.Abs(a - b)
	return diff <= valueTolerance || diff <= valueTolerance*math.Max(math.Abs(a), math.Abs(b))
}

func requireValuesEqual(t *testing.T, want, got model.Value, preserveOrder bool) {
	t.Helper()
	require.IsType(t, want, got, "result type mismatch: want %T, got %T", want, got)

	switch w := want.(type) {
	case model.Vector:
		g := got.(model.Vector)
		requireVectorEqual(t, w, g, preserveOrder)
	case model.Matrix:
		g := got.(model.Matrix)
		requireMatrixEqual(t, w, g, preserveOrder)
	case *model.Scalar:
		g := got.(*model.Scalar)
		require.Equal(t, w.Timestamp, g.Timestamp, "scalar timestamp mismatch: want %v, got %v", w.Timestamp, g.Timestamp)
		require.True(t, almostEqual(float64(w.Value), float64(g.Value)), "scalar value mismatch: want %v, got %v", w.Value, g.Value)
	case *model.String:
		g := got.(*model.String)
		require.Equal(t, w.Timestamp, g.Timestamp, "string timestamp mismatch: want %v, got %v", w.Timestamp, g.Timestamp)
		require.Equal(t, w.Value, g.Value, "string value mismatch: want %v, got %v", w.Value, g.Value)
	default:
		t.Fatalf("unexpected result type %T", w)
	}
}

func requireVectorEqual(t *testing.T, want, got model.Vector, preserveOrder bool) {
	t.Helper()
	if !preserveOrder {
		sort.Sort(want)
		sort.Sort(got)
	}
	require.Equal(t, len(want), len(got), "result length mismatch: want %d, got %d", len(want), len(got))
	for i := range want {
		require.Equal(t, want[i].Metric, got[i].Metric, "vector metric mismatch: want %v, got %v", want[i].Metric, got[i].Metric)
		require.Equal(t, want[i].Timestamp, got[i].Timestamp, "vector timestamp mismatch at series %v: want %v, got %v", got[i].Metric, want[i].Timestamp, got[i].Timestamp)
		require.True(t, almostEqual(float64(want[i].Value), float64(got[i].Value)), "vector value mismatch at series %v: want %v, got %v", got[i].Metric, want[i].Value, got[i].Value)
	}
}

func requireMatrixEqual(t *testing.T, want, got model.Matrix, preserveOrder bool) {
	t.Helper()
	if !preserveOrder {
		sort.Sort(want)
		sort.Sort(got)
	}
	require.Equal(t, len(want), len(got), "result length mismatch: want %d, got %d", len(want), len(got))
	for i := range want {
		require.Equal(t, want[i].Metric, got[i].Metric, "matrix metric mismatch: want %v, got %v", want[i].Metric, got[i].Metric)
		require.Equal(t, len(want[i].Values), len(got[i].Values), "matrix length mismatch at series %v: want %d, got %d", got[i].Metric, len(want[i].Values), len(got[i].Values))
		for j := range want[i].Values {
			require.Equal(t, want[i].Values[j].Timestamp, got[i].Values[j].Timestamp, "matrix timestamp mismatch at series %v, point %d: want %v, got %v", got[i].Metric, j, want[i].Values[j].Timestamp, got[i].Values[j].Timestamp)
			require.True(t, almostEqual(float64(want[i].Values[j].Value), float64(got[i].Values[j].Value)), "matrix value mismatch at series %v, point %d: want %v, got %v", got[i].Metric, j, want[i].Values[j].Value, got[i].Values[j].Value)
		}
	}
}
