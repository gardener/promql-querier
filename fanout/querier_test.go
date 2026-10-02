// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package fanout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/gardener/promql-querier/labels"
)

func testQuerier(t *testing.T, body []byte) *streamingQuerier {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return &streamingQuerier{client: srv.Client(), baseURL: srv.URL}
}

type capturedRequest struct {
	form url.Values
}

func captureQuery(t *testing.T, body []byte) (*streamingQuerier, *capturedRequest) {
	t.Helper()
	captured := &capturedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		captured.form = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return &streamingQuerier{client: srv.Client(), baseURL: srv.URL}, captured
}

func vectorBody(t *testing.T, vec model.Vector, warnings ...string) []byte {
	t.Helper()
	body, err := json.Marshal(queryEnvelope{
		Status:   "success",
		Data:     queryResult{ResultType: "vector", Result: vec},
		Warnings: warnings,
	})
	require.NoError(t, err)
	return body
}

func matrixBody(t *testing.T, matrix model.Matrix) []byte {
	t.Helper()
	body, err := json.Marshal(queryEnvelope{
		Status: "success",
		Data:   queryResult{ResultType: "matrix", Result: matrix},
	})
	require.NoError(t, err)
	return body
}

func vectorWithSamples(count int) model.Vector {
	vec := make(model.Vector, count)
	for i := range vec {
		vec[i] = &model.Sample{
			Metric: model.Metric{"__name__": "up", "instance": model.LabelValue(fmt.Sprintf("inst-%d", i))},
			Value:  model.SampleValue(i),
		}
	}
	return vec
}

func TestStreamingQuerierDecodesVector(t *testing.T) {
	querier := testQuerier(t, vectorBody(t, vectorWithSamples(3), "heads up"))

	series, warnings, err := querier.query(context.Background(), "up", time.Unix(0, 0), 0, nil)
	require.NoError(t, err)

	require.Len(t, series, 3)
	require.Equal(t, promv1.Warnings{"heads up"}, warnings)
}

func TestStreamingQuerierDecodesMatrix(t *testing.T) {
	matrix := model.Matrix{{
		Metric: model.Metric{"__name__": "up"},
		Values: []model.SamplePair{{Timestamp: 0, Value: 1}, {Timestamp: 15000, Value: 2}},
	}}
	querier := testQuerier(t, matrixBody(t, matrix))

	series, _, err := querier.queryRange(context.Background(), "up", promv1.Range{Start: time.Unix(0, 0), End: time.Unix(60, 0), Step: 15 * time.Second}, 0, nil)
	require.NoError(t, err)

	require.Len(t, series, 1)
	require.Equal(t, "up", series[0].Metric.Get("__name__"))
	require.Len(t, series[0].Floats, 2)
}

func TestStreamingQuerierSendsLookbackDelta(t *testing.T) {
	querier, captured := captureQuery(t, vectorBody(t, vectorWithSamples(1)))

	_, _, err := querier.query(context.Background(), "up", time.Unix(0, 0), model.Duration(5*time.Minute), nil)
	require.NoError(t, err)

	require.Equal(t, "5m", captured.form.Get("lookback_delta"))
}

func TestStreamingQuerierRangeSendsLookbackDelta(t *testing.T) {
	querier, captured := captureQuery(t, matrixBody(t, model.Matrix{}))

	r := promv1.Range{Start: time.Unix(0, 0), End: time.Unix(60, 0), Step: 15 * time.Second}
	_, _, err := querier.queryRange(context.Background(), "up", r, model.Duration(5*time.Minute), nil)
	require.NoError(t, err)

	require.Equal(t, "5m", captured.form.Get("lookback_delta"))
}

func TestStreamingQuerierDecodesLargeVector(t *testing.T) {
	querier := testQuerier(t, vectorBody(t, vectorWithSamples(100)))

	series, _, err := querier.query(context.Background(), "up", time.Unix(0, 0), 0, nil)
	require.NoError(t, err)

	require.Len(t, series, 100)
}

func TestStreamingQuerierInjectsVirtualLabelsWhileDecoding(t *testing.T) {
	querier := testQuerier(t, vectorBody(t, model.Vector{
		{Metric: model.Metric{"__name__": "up", "job": "api"}, Value: 1},
	}))

	series, _, err := querier.query(context.Background(), "up", time.Unix(0, 0), 0, labels.LabelSet{"region": "eu", "replica": ""})
	require.NoError(t, err)

	require.Len(t, series, 1)
	require.Equal(t, "up", series[0].Metric.Get("__name__"))
	require.Equal(t, "api", series[0].Metric.Get("job"))
	require.Equal(t, "eu", series[0].Metric.Get("region"))
	require.False(t, series[0].Metric.Has("replica"))
}

func TestStreamingQuerierInjectsVirtualLabelsOntoMatrix(t *testing.T) {
	matrix := model.Matrix{{
		Metric: model.Metric{"__name__": "up", "job": "api"},
		Values: []model.SamplePair{{Timestamp: 0, Value: 1}, {Timestamp: 15000, Value: 2}},
	}}
	querier := testQuerier(t, matrixBody(t, matrix))

	series, _, err := querier.queryRange(context.Background(), "up", promv1.Range{Start: time.Unix(0, 0), End: time.Unix(60, 0), Step: 15 * time.Second}, 0, labels.LabelSet{"region": "eu", "replica": ""})
	require.NoError(t, err)

	require.Len(t, series, 1)
	require.Equal(t, "up", series[0].Metric.Get("__name__"))
	require.Equal(t, "api", series[0].Metric.Get("job"))
	require.Equal(t, "eu", series[0].Metric.Get("region"))
	require.False(t, series[0].Metric.Has("replica"))
}

func TestStreamingQuerierErrorEnvelopeBecomesAPIError(t *testing.T) {
	body := []byte(`{"status":"error","errorType":"bad_data","error":"invalid expression"}`)
	querier := testQuerier(t, body)

	_, _, err := querier.query(context.Background(), "bad(", time.Unix(0, 0), 0, nil)
	require.Error(t, err)

	var apiErr *promv1.Error
	require.True(t, errors.As(err, &apiErr), "want *promv1.Error, got %T", err)
	require.Equal(t, "invalid expression", apiErr.Msg)
}

func TestStreamingQuerierReadsWarningsAfterResult(t *testing.T) {
	querier := testQuerier(t, vectorBody(t, vectorWithSamples(1), "one", "two"))

	series, warnings, err := querier.query(context.Background(), "up", time.Unix(0, 0), 0, nil)
	require.NoError(t, err)
	require.Len(t, series, 1)
	require.Equal(t, promv1.Warnings{"one", "two"}, warnings)
}

func TestStreamingQuerierDecodesEscapedLabelValue(t *testing.T) {
	// A label value with an escaped quote and a backslash exercises the
	// strconv.Unquote branch of the metric scanner: the decoded label must be
	// unescaped, not carry the raw backslashes.
	body := []byte(`{"status":"success","data":{"resultType":"vector","result":[` +
		`{"metric":{"__name__":"up","path":"a\\b\"c"},"value":[0,"1"]}]}}`)
	querier := testQuerier(t, body)

	series, _, err := querier.query(context.Background(), "up", time.Unix(0, 0), 0, nil)
	require.NoError(t, err)
	require.Len(t, series, 1)
	require.Equal(t, `a\b"c`, series[0].Metric.Get("path"))
}

func TestStreamingQuerierDecodesUnicodeEscapeInLabelValue(t *testing.T) {
	// A \uXXXX escape is where a hand-rolled span scanner is most likely to
	// diverge from encoding/json: the scanner must hand strconv.Unquote a
	// well-formed quoted span so the multi-byte rune is reconstructed. The value
	// below is "café" written with a é escape.
	body := []byte(`{"status":"success","data":{"resultType":"vector","result":[` +
		`{"metric":{"__name__":"up","place":"café"},"value":[0,"1"]}]}}`)
	querier := testQuerier(t, body)

	series, _, err := querier.query(context.Background(), "up", time.Unix(0, 0), 0, nil)
	require.NoError(t, err)
	require.Len(t, series, 1)
	require.Equal(t, "café", series[0].Metric.Get("place"))
}

func TestStreamingQuerierDecodesEscapedLabelName(t *testing.T) {
	// stringValue reads both label names and values, so an escape in a name must
	// unescape the same way a value does. The name below is `a"b`.
	body := []byte(`{"status":"success","data":{"resultType":"vector","result":[` +
		`{"metric":{"__name__":"up","a\"b":"v"},"value":[0,"1"]}]}}`)
	querier := testQuerier(t, body)

	series, _, err := querier.query(context.Background(), "up", time.Unix(0, 0), 0, nil)
	require.NoError(t, err)
	require.Len(t, series, 1)
	require.Equal(t, "v", series[0].Metric.Get(`a"b`))
}

func TestStreamingQuerierDecodesBraceInLabelValue(t *testing.T) {
	// A closing brace inside a quoted label value (like an error message mentioning
	// "}") must not close the metric object early: the byte-by-byte scanner has to
	// track that it is inside a string. The brace has no matching opener on purpose,
	// so a mistaken depth count cannot self-heal by later balancing out.
	body := []byte(`{"status":"success","data":{"resultType":"vector","result":[` +
		`{"metric":{"__name__":"up","path":"a}b","job":"api"},"value":[0,"1"]}]}}`)
	querier := testQuerier(t, body)

	series, _, err := querier.query(context.Background(), "up", time.Unix(0, 0), 0, nil)
	require.NoError(t, err)
	require.Len(t, series, 1)
	require.Equal(t, "a}b", series[0].Metric.Get("path"))
	require.Equal(t, "api", series[0].Metric.Get("job"))
}

func TestStreamingQuerierRejectsMalformedMetric(t *testing.T) {
	malformed := map[string]string{
		"missing colon":    `{"__name__""up"}`,
		"truncated object": `{"__name__":"up"`,
		"non string value": `{"__name__":123}`,
		"missing comma":    `{"__name__":"up" "job":"api"}`,
	}

	for name, metric := range malformed {
		t.Run(name, func(t *testing.T) {
			body := []byte(`{"status":"success","data":{"resultType":"vector","result":[` +
				`{"metric":` + metric + `,"value":[0,"1"]}]}}`)
			querier := testQuerier(t, body)

			_, _, err := querier.query(context.Background(), "up", time.Unix(0, 0), 0, nil)
			require.Error(t, err)
		})
	}
}

func TestStreamingQuerierDecodesSpecialSampleValues(t *testing.T) {
	samples := map[string]struct {
		input string
		want  float64
	}{
		"positive":          {`"1.5"`, 1.5},
		"negative":          {`"-2.25"`, -2.25},
		"exponent":          {`"1e3"`, 1000},
		"positive infinity": {`"+Inf"`, math.Inf(1)},
		"negative infinity": {`"-Inf"`, math.Inf(-1)},
	}

	for name, tc := range samples {
		t.Run(name, func(t *testing.T) {
			body := []byte(`{"status":"success","data":{"resultType":"vector","result":[` +
				`{"metric":{"__name__":"up"},"value":[0,` + tc.input + `]}]}}`)
			querier := testQuerier(t, body)

			series, _, err := querier.query(context.Background(), "up", time.Unix(0, 0), 0, nil)
			require.NoError(t, err)
			require.Len(t, series, 1)
			require.Len(t, series[0].Floats, 1)
			require.Equal(t, tc.want, series[0].Floats[0].F)
		})
	}
}

func TestStreamingQuerierDecodesNaNSampleValue(t *testing.T) {
	body := []byte(`{"status":"success","data":{"resultType":"vector","result":[` +
		`{"metric":{"__name__":"up"},"value":[0,"NaN"]}]}}`)
	querier := testQuerier(t, body)

	series, _, err := querier.query(context.Background(), "up", time.Unix(0, 0), 0, nil)
	require.NoError(t, err)
	require.Len(t, series, 1)
	require.Len(t, series[0].Floats, 1)
	require.True(t, math.IsNaN(series[0].Floats[0].F))
}

func TestStreamingQuerierDecodesTimestamps(t *testing.T) {
	timestamps := map[string]struct {
		input string
		want  int64
	}{
		"integer seconds":     {"1", 1000},
		"fractional millis":   {"1.5", 1500},
		"sub milli truncated": {"1.23456", 1234},
		"negative integer":    {"-2", -2000},
		"negative sub second": {"-0.1", -100},
	}

	for name, tc := range timestamps {
		t.Run(name, func(t *testing.T) {
			body := []byte(`{"status":"success","data":{"resultType":"vector","result":[` +
				`{"metric":{"__name__":"up"},"value":[` + tc.input + `,"1"]}]}}`)
			querier := testQuerier(t, body)

			series, _, err := querier.query(context.Background(), "up", time.Unix(0, 0), 0, nil)
			require.NoError(t, err)
			require.Len(t, series, 1)
			require.Len(t, series[0].Floats, 1)
			require.Equal(t, tc.want, series[0].Floats[0].T)
		})
	}
}

func TestStreamingQuerierRejectsMalformedTimestamp(t *testing.T) {
	malformed := map[string]string{
		"exponent":        "1e10",
		"fractional expo": "1.2e3",
		"double dot":      "1.2.3",
		"embedded sign":   "1-2",
		"letters":         "abc",
	}

	for name, ts := range malformed {
		t.Run(name, func(t *testing.T) {
			body := []byte(`{"status":"success","data":{"resultType":"vector","result":[` +
				`{"metric":{"__name__":"up"},"value":[` + ts + `,"1"]}]}}`)
			querier := testQuerier(t, body)

			_, _, err := querier.query(context.Background(), "up", time.Unix(0, 0), 0, nil)
			require.Error(t, err)
		})
	}
}

func TestStreamingQuerierMatchesModelDecode(t *testing.T) {
	vec := model.Vector{
		{Metric: model.Metric{"__name__": "up", "path": `a\b"c`, "job": "api"}, Timestamp: 1234, Value: -2.5},
		{Metric: model.Metric{"__name__": "up", "job": "db"}, Timestamp: 15500, Value: model.SampleValue(math.Inf(1))},
	}
	body := vectorBody(t, vec)
	querier := testQuerier(t, body)

	series, _, err := querier.query(context.Background(), "up", time.Unix(0, 0), 0, nil)
	require.NoError(t, err)
	require.Len(t, series, len(vec))

	for i, sample := range vec {
		require.Equal(t, string(sample.Metric["__name__"]), series[i].Metric.Get("__name__"))
		require.Equal(t, string(sample.Metric["path"]), series[i].Metric.Get("path"))
		require.Equal(t, string(sample.Metric["job"]), series[i].Metric.Get("job"))
		require.Len(t, series[i].Floats, 1)
		require.Equal(t, int64(sample.Timestamp), series[i].Floats[0].T)
		if math.IsInf(float64(sample.Value), 0) {
			require.True(t, math.IsInf(series[i].Floats[0].F, 1))
		} else {
			require.Equal(t, float64(sample.Value), series[i].Floats[0].F)
		}
	}
}

func TestStreamingQuerierDecodesResultBeforeResultType(t *testing.T) {
	// The array decoder infers each element's shape from its own "value"
	// field, so a body that emits result before resultType decodes
	// correctly rather than erroring on an unseen resultType.
	body := []byte(`{"status":"success","data":{"result":[` +
		`{"metric":{"__name__":"up"},"value":[0,"1"]}],"resultType":"vector"}}`)
	querier := testQuerier(t, body)

	series, _, err := querier.query(context.Background(), "up", time.Unix(0, 0), 0, nil)
	require.NoError(t, err)
	require.Len(t, series, 1)
	require.Equal(t, "up", series[0].Metric.Get("__name__"))
	require.Len(t, series[0].Floats, 1)
}

func TestStreamingQuerierDecodesMatrixResultBeforeResultType(t *testing.T) {
	// The array decoder infers each element's shape from its own "values"
	// field, so a body that emits result before resultType decodes
	// correctly rather than erroring on an unseen resultType.
	body := []byte(`{"status":"success","data":{"result":[` +
		`{"metric":{"__name__":"up"},"values":[[0,"1"],[15,"2"]]}],"resultType":"matrix"}}`)
	querier := testQuerier(t, body)

	series, _, err := querier.queryRange(context.Background(), "up", promv1.Range{Start: time.Unix(0, 0), End: time.Unix(60, 0), Step: 15 * time.Second}, 0, nil)
	require.NoError(t, err)
	require.Len(t, series, 1)
	require.Len(t, series[0].Floats, 2)
}

func TestStreamingQuerierRejectsElementWithBothValueAndValues(t *testing.T) {
	body := []byte(`{"status":"success","data":{"resultType":"vector","result":[` +
		`{"metric":{"__name__":"up"},"value":[0,"1"],"values":[[0,"1"]]}]}}`)
	querier := testQuerier(t, body)

	_, _, err := querier.query(context.Background(), "up", time.Unix(0, 0), 0, nil)
	require.Error(t, err)
}

func TestStreamingQuerierEnforcesFetchedBytesLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(vectorBody(t, vectorWithSamples(200)))
	}))
	t.Cleanup(srv.Close)

	client := &http.Client{Transport: &FetchedBytesTransport{Base: http.DefaultTransport, URL: srv.URL}}
	querier := &streamingQuerier{client: client, baseURL: srv.URL}

	// The max-fetched-bytes limit should fail the query.
	ctx := contextWithFetchedSamplesLimit(context.Background(), NewFetchedSamplesLimit(100_000_000))
	ctx = contextWithFetchedBytesLimit(ctx, NewFetchedBytesLimit(10))

	_, _, err := querier.query(ctx, "up", time.Unix(0, 0), 0, nil)
	require.Error(t, err)
	require.Equal(t, "fetched bytes limit exceeded (10 bytes)", err.Error())

	var bytesErr *FetchedBytesLimitError
	require.True(t, errors.As(err, &bytesErr), "want *FetchedBytesLimitError, got %T", err)
}

func TestStreamingQuerierEnforcesFetchedSamplesLimit(t *testing.T) {
	querier := testQuerier(t, vectorBody(t, vectorWithSamples(200)))

	// The max-fetched-samples limit should fail the query.
	ctx := contextWithFetchedSamplesLimit(context.Background(), NewFetchedSamplesLimit(10))
	ctx = contextWithFetchedBytesLimit(ctx, NewFetchedBytesLimit(100_000_000))

	_, _, err := querier.query(ctx, "up", time.Unix(0, 0), 0, nil)
	require.Error(t, err)
	require.Equal(t, "fetched samples limit exceeded (10 samples)", err.Error())

	var samplesErr *FetchedSamplesLimitError
	require.True(t, errors.As(err, &samplesErr), "want *FetchedSamplesLimitError, got %T", err)
}

func TestStreamingQuerierSumsMatrixPointsAgainstSamplesLimit(t *testing.T) {
	matrix := model.Matrix{{
		Metric: model.Metric{"__name__": "up"},
		Values: []model.SamplePair{
			{Timestamp: 0, Value: 1},
			{Timestamp: 15000, Value: 2},
			{Timestamp: 30000, Value: 3},
		},
	}}

	querier := testQuerier(t, matrixBody(t, matrix))

	ctx := contextWithFetchedSamplesLimit(context.Background(), NewFetchedSamplesLimit(2))
	_, _, err := querier.queryRange(ctx, "up", promv1.Range{Start: time.Unix(0, 0), End: time.Unix(60, 0), Step: 15 * time.Second}, 0, nil)
	require.Error(t, err)
	require.Equal(t, "fetched samples limit exceeded (2 samples)", err.Error())
}

func TestElementSpansRejectsHistogramKeys(t *testing.T) {
	for _, key := range []string{"histogram", "histograms"} {
		elem := []byte(`{"metric":{"__name__":"up"},"` + key + `":{}}`)
		_, err := elementSpans(elem)
		require.EqualError(t, err, "histogram result type is not supported")
	}
}
