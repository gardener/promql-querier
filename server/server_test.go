// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"

	"github.com/gardener/promql-querier/fanout"
	"github.com/gardener/promql-querier/labels"
	"github.com/gardener/promql-querier/plan"
)

func TestParseTime(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    time.Time
		wantErr bool
	}{
		{name: "unix seconds", in: "1600000000", want: time.Unix(1600000000, 0)},
		{name: "unix seconds with fraction", in: "1600000000.500", want: time.Unix(1600000000, 500*int64(time.Millisecond))},
		{name: "unix seconds sub-millisecond fraction rounds", in: "1600000000.1236", want: time.UnixMilli(1600000000124)},
		{name: "rfc3339", in: "2020-09-13T12:26:40Z", want: time.Unix(1600000000, 0).UTC()},
		{name: "rfc3339 sub-millisecond is truncated", in: "2020-09-13T12:26:40.123456789Z", want: time.Unix(1600000000, 123*int64(time.Millisecond)).UTC()},
		{name: "empty", in: "", wantErr: true},
		{name: "garbage", in: "not-a-time", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseTime(tc.in)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.True(t, tc.want.Equal(got), "want %v, got %v", tc.want, got)
			require.Zero(t, got.UnixNano()%int64(time.Millisecond))
		})
	}
}

func TestParseStep(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    time.Duration
		wantErr bool
	}{
		{name: "seconds as float", raw: "60", want: 60 * time.Second},
		{name: "integer seconds", raw: "120", want: 120 * time.Second},
		{name: "fractional seconds", raw: "0.5", want: 500 * time.Millisecond},
		{name: "duration string", raw: "5m", want: 5 * time.Minute},
		{name: "days unit", raw: "1d", want: 24 * time.Hour},
		{name: "compound duration", raw: "1h30m", want: 90 * time.Minute},
		{name: "empty", raw: "", wantErr: true},
		{name: "garbage", raw: "abc", wantErr: true},
		{name: "overflows int64", raw: "1e30", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			form := url.Values{}
			if tc.raw != "" {
				form.Set("step", tc.raw)
			}
			req := httptest.NewRequest(http.MethodGet, "/api/v1/query_range?"+form.Encode(), nil)
			got, err := parseStep(req)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestParseLimit(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    int
		wantErr bool
	}{
		{name: "absent means disabled", raw: "", want: 0},
		{name: "explicit zero means disabled", raw: "0", want: 0},
		{name: "positive limit", raw: "5", want: 5},
		{name: "negative is rejected", raw: "-1", wantErr: true},
		{name: "non-integer is rejected", raw: "abc", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			form := url.Values{}
			if tc.raw != "" {
				form.Set("limit", tc.raw)
			}
			req := httptest.NewRequest(http.MethodGet, "/api/v1/series?"+form.Encode(), nil)
			got, err := parseLimit(req)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

type fakeAPI struct {
	promv1.API

	queryFn       func(context.Context) (model.Value, promv1.Warnings, error)
	queryRangeFn  func(context.Context) (model.Value, promv1.Warnings, error)
	seriesFn      func(context.Context) ([]model.LabelSet, promv1.Warnings, error)
	labelNamesFn  func(context.Context) (model.LabelNames, promv1.Warnings, error)
	labelValuesFn func(context.Context) (model.LabelValues, promv1.Warnings, error)
}

func testServer(t *testing.T, clients ...promv1.API) *Server {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Assume at most two clients are passed.
	regions := []string{"eu", "us"}
	virtualLabels := make(map[string]labels.LabelSet, len(clients))
	for i, client := range clients {
		region := regions[i]
		srv := httptest.NewServer(testHandler(client))
		t.Cleanup(srv.Close)
		virtualLabels[srv.URL] = labels.LabelSet{"region": region}
	}

	downstreams, err := fanout.BuildDownstreams(virtualLabels, 0)
	require.NoError(t, err)

	labelNames := labels.NewLabelNames("region")
	planner := plan.NewPlanner(virtualLabels, labelNames)
	engine := promql.NewEngine(fanout.EngineDefaultOpts())
	processor := fanout.NewProcessor(logger, engine, planner, downstreams, fanout.ProcessorDefaultOpts())
	metadata := fanout.NewMetadata(logger, downstreams, labelNames, 0)

	config := NewConfig(downstreams)
	return New(logger, processor, metadata, config)
}

func testHandler(client promv1.API) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		switch {
		case r.URL.Path == "/api/v1/query_range":
			result, _, _ := client.QueryRange(ctx, "", promv1.Range{})
			writeQueryResult(w, result)
		case r.URL.Path == "/api/v1/query":
			result, _, _ := client.Query(ctx, "", time.Time{})
			writeQueryResult(w, result)
		case r.URL.Path == "/api/v1/series":
			result, _, _ := client.Series(ctx, nil, time.Time{}, time.Time{})
			writeMetadataResult(w, result)
		case r.URL.Path == "/api/v1/labels":
			result, _, _ := client.LabelNames(ctx, nil, time.Time{}, time.Time{})
			writeMetadataResult(w, result)
		case strings.HasPrefix(r.URL.Path, "/api/v1/label/"):
			result, _, _ := client.LabelValues(ctx, "", nil, time.Time{}, time.Time{})
			writeMetadataResult(w, result)
		default:
			http.NotFound(w, r)
		}
	}
}

func (f fakeAPI) Query(ctx context.Context, _ string, _ time.Time, _ ...promv1.Option) (model.Value, promv1.Warnings, error) {
	return f.queryFn(ctx)
}

func (f fakeAPI) QueryRange(ctx context.Context, _ string, _ promv1.Range, _ ...promv1.Option) (model.Value, promv1.Warnings, error) {
	return f.queryRangeFn(ctx)
}

func (f fakeAPI) Series(ctx context.Context, _ []string, _, _ time.Time, _ ...promv1.Option) ([]model.LabelSet, promv1.Warnings, error) {
	return f.seriesFn(ctx)
}

func (f fakeAPI) LabelNames(ctx context.Context, _ []string, _, _ time.Time, _ ...promv1.Option) (model.LabelNames, promv1.Warnings, error) {
	return f.labelNamesFn(ctx)
}

func (f fakeAPI) LabelValues(ctx context.Context, _ string, _ []string, _, _ time.Time, _ ...promv1.Option) (model.LabelValues, promv1.Warnings, error) {
	return f.labelValuesFn(ctx)
}

func writeQueryResult(w http.ResponseWriter, val model.Value) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "success",
		"data": map[string]any{
			"resultType": val.Type().String(),
			"result":     val,
		},
	})
}

func writeMetadataResult(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "success",
		"data":   data,
	})
}

func hasSeries() fakeAPI {
	return fakeAPI{seriesFn: func(context.Context) ([]model.LabelSet, promv1.Warnings, error) {
		return []model.LabelSet{{"__name__": "up"}}, nil, nil
	}}
}

// respondingClient sets every closure so a request to any endpoint reaches a
// real response instead of a nil closure. Routing tests need a downstream that
// answers on all paths, not just series.
func respondingClient() fakeAPI {
	return fakeAPI{
		queryFn:      queryResult(vector(1)),
		queryRangeFn: queryResult(vector(1)),
		seriesFn: func(context.Context) ([]model.LabelSet, promv1.Warnings, error) {
			return []model.LabelSet{{"__name__": "up"}}, nil, nil
		},
		labelNamesFn: func(context.Context) (model.LabelNames, promv1.Warnings, error) {
			return model.LabelNames{"__name__"}, nil, nil
		},
		labelValuesFn: func(context.Context) (model.LabelValues, promv1.Warnings, error) {
			return model.LabelValues{"up"}, nil, nil
		},
	}
}

func queryResult(value model.Value) func(context.Context) (model.Value, promv1.Warnings, error) {
	return func(context.Context) (model.Value, promv1.Warnings, error) {
		return value, nil, nil
	}
}

func vector(value float64) model.Vector {
	return model.Vector{{
		Metric: model.Metric{"__name__": "up", "job": "api"},
		Value:  model.SampleValue(value),
	}}
}

func TestHandleReady(t *testing.T) {
	rec := httptest.NewRecorder()
	testServer(t).handleReady(rec, httptest.NewRequest(http.MethodGet, "/-/ready", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "Gardener PromQL Querier is Ready.", rec.Body.String())
}

func TestHandleQueryMissingParameter(t *testing.T) {
	rec := httptest.NewRecorder()
	testServer(t).handleQuery(rec, httptest.NewRequest(http.MethodGet, "/api/v1/query", nil))

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, errorTypeBadData, decodeErrorType(t, rec.Body.Bytes()))
}

func TestHandleQueryInvalidTime(t *testing.T) {
	form := url.Values{"query": {"up"}, "time": {"not-a-time"}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/query?"+form.Encode(), nil)

	rec := httptest.NewRecorder()
	testServer(t).handleQuery(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, errorTypeBadData, decodeErrorType(t, rec.Body.Bytes()))
}

func TestHandleQueryInvalidQueryExpression(t *testing.T) {
	form := url.Values{"query": {"this is )( not promql"}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/query?"+form.Encode(), nil)

	rec := httptest.NewRecorder()
	testServer(t).handleQuery(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, errorTypeBadData, decodeErrorType(t, rec.Body.Bytes()))
}

func TestHandleQuerySuccess(t *testing.T) {
	client := fakeAPI{queryFn: queryResult(vector(1))}
	form := url.Values{"query": {"up"}, "time": {"0"}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/query?"+form.Encode(), nil)

	rec := httptest.NewRecorder()
	testServer(t, client, client).handleQuery(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp apiResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "success", resp.Status)
}

func TestHandleQueryRangeInvalidParameter(t *testing.T) {
	valid := url.Values{"query": {"up"}, "start": {"0"}, "end": {"60"}, "step": {"15"}}
	tests := map[string]struct {
		param string
		value string
	}{
		"invalid start": {param: "start", value: "bad"},
		"invalid end":   {param: "end", value: "bad"},
		"invalid step":  {param: "step", value: "bad"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			form := url.Values{}
			for k, v := range valid {
				form[k] = v
			}
			form.Set(tc.param, tc.value)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/query_range?"+form.Encode(), nil)

			rec := httptest.NewRecorder()
			testServer(t).handleQueryRange(rec, req)

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Equal(t, errorTypeBadData, decodeErrorType(t, rec.Body.Bytes()))
		})
	}
}

func TestHandleQueryRangeRejectsRangeVector(t *testing.T) {
	form := url.Values{"query": {"up[5m]"}, "start": {"0"}, "end": {"60"}, "step": {"15"}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/query_range?"+form.Encode(), nil)

	rec := httptest.NewRecorder()
	testServer(t).handleQueryRange(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, errorTypeBadData, decodeErrorType(t, rec.Body.Bytes()))
}

func TestHandleLabelValuesMissingLabel(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/label//values", nil)

	rec := httptest.NewRecorder()
	testServer(t).handleLabelValues(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, errorTypeBadData, decodeErrorType(t, rec.Body.Bytes()))
}

func TestHandleLabelValuesVirtualLabel(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/label/region/values", nil)

	rec := httptest.NewRecorder()
	testServer(t, hasSeries(), hasSeries()).handleLabelValues(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp apiResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "success", resp.Status)
	values := resp.Data.([]any)
	require.ElementsMatch(t, []any{"eu", "us"}, values)
}

func TestHandleStatusConfig(t *testing.T) {
	rec := httptest.NewRecorder()
	testServer(t, hasSeries(), hasSeries()).handleStatusConfig(rec, httptest.NewRequest(http.MethodGet, "/api/v1/status/config", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	var resp apiResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "success", resp.Status)

	data := resp.Data.(map[string]any)
	endpoints := data["endpoints"].([]any)
	require.Len(t, endpoints, 2)

	var gotLabels []any
	for _, ep := range endpoints {
		endpoint := ep.(map[string]any)
		require.Regexp(t, "http://127.0.0.1:[0-9]+", endpoint["url"])
		gotLabels = append(gotLabels, endpoint["labels"])
	}
	require.ElementsMatch(t, []any{
		map[string]any{"region": "eu"},
		map[string]any{"region": "us"},
	}, gotLabels)
}

func TestHandlerRoutesPaths(t *testing.T) {
	handler := testServer(t, respondingClient(), respondingClient()).Handler()

	paths := []string{
		"/-/ready",
		"/api/v1/query",
		"/api/v1/query_range",
		"/api/v1/plan",
		"/api/v1/plan_range",
		"/api/v1/series",
		"/api/v1/labels",
		"/api/v1/label/region/values",
		"/api/v1/status/config",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			require.NotEqual(t, http.StatusNotFound, rec.Code, "path should be routed to a handler")
		})
	}

	t.Run("unregistered path returns 404", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/does-not-exist", nil))
		require.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestHandlerServesQueryPage(t *testing.T) {
	handler := testServer(t).Handler()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/query", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "text/html")
	require.Contains(t, rec.Body.String(), "PromQL Querier")
}

func TestHandlerServesConfigPage(t *testing.T) {
	handler := testServer(t).Handler()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/config", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "text/html")
	require.Contains(t, rec.Body.String(), "PromQL Querier")
}

func TestHandlerRedirectsRootToQuery(t *testing.T) {
	handler := testServer(t).Handler()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	require.Equal(t, http.StatusFound, rec.Code)
	require.Equal(t, "/query", rec.Header().Get("Location"))
}

func TestHandlerServesUIAsset(t *testing.T) {
	handler := testServer(t).Handler()

	asset := firstStaticAssetPath(t, handler)
	if asset == "" {
		t.Skip("UI bundle not built; no /static/ asset to serve")
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, asset, nil))
	require.Equal(t, http.StatusOK, rec.Code)
}

func firstStaticAssetPath(t *testing.T, handler http.Handler) string {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/query", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	staticAssetPattern := regexp.MustCompile(`/static/assets/[^"']+`)
	return staticAssetPattern.FindString(rec.Body.String())
}

func TestHandlerUnknownPathIsNotFound(t *testing.T) {
	handler := testServer(t).Handler()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/does-not-exist", nil))

	require.Equal(t, http.StatusNotFound, rec.Code)
}

func vectorWithSamples(count int) model.Vector {
	vec := make(model.Vector, count)
	for i := range vec {
		instance := "instance-with-a-fairly-long-name-" + strconv.Itoa(i)
		vec[i] = &model.Sample{
			Metric: model.Metric{"__name__": "up", "job": "api", "instance": model.LabelValue(instance)},
			Value:  model.SampleValue(i),
		}
	}
	return vec
}

func TestHandlerCompressesResponseWhenClientAcceptsGzip(t *testing.T) {
	client := fakeAPI{queryFn: queryResult(vectorWithSamples(100))}
	form := url.Values{"query": {"up"}, "time": {"0"}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/query?"+form.Encode(), nil)
	req.Header.Set("Accept-Encoding", "gzip")

	rec := httptest.NewRecorder()
	testServer(t, client, client).Handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))
	require.Equal(t, "Accept-Encoding", rec.Header().Get("Vary"))

	zr, err := gzip.NewReader(rec.Body)
	require.NoError(t, err)
	t.Cleanup(func() { _ = zr.Close() })
	body, err := io.ReadAll(zr)
	require.NoError(t, err)
	require.Equal(t, "success", decodeStatus(t, body))
}

func TestHandlerLeavesResponseUncompressedWithoutAcceptEncoding(t *testing.T) {
	client := fakeAPI{queryFn: queryResult(vectorWithSamples(100))}
	form := url.Values{"query": {"up"}, "time": {"0"}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/query?"+form.Encode(), nil)

	rec := httptest.NewRecorder()
	testServer(t, client, client).Handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Empty(t, rec.Header().Get("Content-Encoding"))
	require.Equal(t, "success", decodeStatus(t, rec.Body.Bytes()))
}

func TestHandleQueryRangeSuccess(t *testing.T) {
	matrix := model.Matrix{{
		Metric: model.Metric{"__name__": "up", "job": "api"},
		Values: []model.SamplePair{{Timestamp: 0, Value: 1}, {Timestamp: 15000, Value: 2}},
	}}
	client := fakeAPI{queryRangeFn: queryResult(matrix)}
	form := url.Values{"query": {"up"}, "start": {"0"}, "end": {"60"}, "step": {"15"}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/query_range?"+form.Encode(), nil)

	rec := httptest.NewRecorder()
	testServer(t, client, client).handleQueryRange(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "success", decodeStatus(t, rec.Body.Bytes()))
}

func TestHandleSeriesSuccess(t *testing.T) {
	client := fakeAPI{seriesFn: func(context.Context) ([]model.LabelSet, promv1.Warnings, error) {
		return []model.LabelSet{{"__name__": "up", "job": "api"}}, nil, nil
	}}
	form := url.Values{"match[]": {`{job="api"}`}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/series?"+form.Encode(), nil)

	rec := httptest.NewRecorder()
	testServer(t, client, client).handleSeries(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "success", decodeStatus(t, rec.Body.Bytes()))
}

func TestHandleSeriesEmitsWarningsInJSON(t *testing.T) {
	client := fakeAPI{seriesFn: func(context.Context) ([]model.LabelSet, promv1.Warnings, error) {
		return []model.LabelSet{{"__name__": "up"}}, nil, nil
	}}
	form := url.Values{"limit": {"1"}, "match[]": {`{__name__!=""}`}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/series?"+form.Encode(), nil)

	rec := httptest.NewRecorder()
	testServer(t, client, client).handleSeries(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Status   string   `json:"status"`
		Warnings []string `json:"warnings"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "success", resp.Status)
	require.Equal(t, resp.Warnings, []string{"results truncated due to limit"})
}

func TestHandleSeriesInvalidMatcher(t *testing.T) {
	form := url.Values{"match[]": {`{not a selector`}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/series?"+form.Encode(), nil)

	rec := httptest.NewRecorder()
	testServer(t, fakeAPI{}, fakeAPI{}).handleSeries(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Equal(t, errorTypeInternal, decodeErrorType(t, rec.Body.Bytes()))
}

// TestHandleSeriesEmptyResultEncodesEmptyArray pins the Prometheus API contract:
// an empty series result must serialize as "data": [], not be omitted. A missing
// data field makes the Prometheus client fail to decode the response.
func TestHandleSeriesEmptyResultEncodesEmptyArray(t *testing.T) {
	empty := fakeAPI{seriesFn: func(context.Context) ([]model.LabelSet, promv1.Warnings, error) {
		return nil, nil, nil
	}}
	form := url.Values{"match[]": {`{job="nonexistent"}`}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/series?"+form.Encode(), nil)

	rec := httptest.NewRecorder()
	testServer(t, empty, empty).handleSeries(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))
	data, ok := raw["data"]
	require.True(t, ok, "response must include a data field")
	require.Equal(t, "[]", string(data))
}

func TestHandleSeriesInvalidTime(t *testing.T) {
	form := url.Values{"start": {"bad"}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/series?"+form.Encode(), nil)

	rec := httptest.NewRecorder()
	testServer(t).handleSeries(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, errorTypeBadData, decodeErrorType(t, rec.Body.Bytes()))
}

func TestHandleLabelNamesSuccess(t *testing.T) {
	client := fakeAPI{labelNamesFn: func(context.Context) (model.LabelNames, promv1.Warnings, error) {
		return model.LabelNames{"job", "instance"}, nil, nil
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/labels", nil)

	rec := httptest.NewRecorder()
	testServer(t, client, client).handleLabelNames(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "success", decodeStatus(t, rec.Body.Bytes()))
}

func TestHandleLabelNamesInvalidTime(t *testing.T) {
	form := url.Values{"end": {"bad"}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/labels?"+form.Encode(), nil)

	rec := httptest.NewRecorder()
	testServer(t).handleLabelNames(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, errorTypeBadData, decodeErrorType(t, rec.Body.Bytes()))
}

func TestParseOptionalTimeZeroParsesValue(t *testing.T) {
	form := url.Values{"start": {"0"}, "end": {"60"}}
	req := httptest.NewRequest(http.MethodGet, "/?"+form.Encode(), nil)
	require.NoError(t, req.ParseForm())

	start, err := parseOptionalTimeZero(req, "start")
	require.NoError(t, err)
	require.True(t, time.Unix(0, 0).Equal(start))

	end, err := parseOptionalTimeZero(req, "end")
	require.NoError(t, err)
	require.True(t, time.Unix(60, 0).Equal(end))
}

func TestParseOptionalTimeZeroEmptyIsZeroTime(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	require.NoError(t, req.ParseForm())

	start, err := parseOptionalTimeZero(req, "start")
	require.NoError(t, err)
	require.True(t, start.IsZero())

	end, err := parseOptionalTimeZero(req, "end")
	require.NoError(t, err)
	require.True(t, end.IsZero())
}

func TestParseOptionalTimeNowParsesValue(t *testing.T) {
	form := url.Values{"time": {"60"}}
	req := httptest.NewRequest(http.MethodGet, "/?"+form.Encode(), nil)
	require.NoError(t, req.ParseForm())

	ts, err := parseOptionalTimeNow(req, "time")
	require.NoError(t, err)
	require.True(t, time.Unix(60, 0).Equal(ts))
}

func TestParseOptionalTimeNowEmptyDefaultsToNow(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	require.NoError(t, req.ParseForm())

	before := time.Now().Truncate(time.Millisecond)
	ts, err := parseOptionalTimeNow(req, "time")
	after := time.Now()

	require.NoError(t, err)
	require.False(t, ts.Before(before), "default %v is before the call started %v", ts, before)
	require.False(t, ts.After(after), "default %v is after the call returned %v", ts, after)
	// The default path does not flow through parseTime, so assert the millisecond
	// alignment invariant holds here too.
	require.Zero(t, ts.UnixNano()%int64(time.Millisecond))
}

func decodeStatus(t *testing.T, body []byte) string {
	t.Helper()
	var resp apiResponse
	require.NoError(t, json.Unmarshal(body, &resp))
	return resp.Status
}

func decodeErrorType(t *testing.T, body []byte) string {
	t.Helper()
	var resp apiResponse
	require.NoError(t, json.Unmarshal(body, &resp))
	require.Equal(t, "error", resp.Status)
	return resp.ErrorType
}

// planResponse is the decoded plan endpoint envelope, with Data typed to the
// plan description so the test reads the fetch mode and times.
type planResponse struct {
	Status string           `json:"status"`
	Data   plan.Description `json:"data"`
}

func decodePlanResponse(t *testing.T, body []byte) planResponse {
	t.Helper()
	var resp planResponse
	require.NoError(t, json.Unmarshal(body, &resp))
	return resp
}

func TestHandlePlanInstant(t *testing.T) {
	form := url.Values{"query": {"count(foo)"}, "time": {"300"}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/plan?"+form.Encode(), nil)

	rec := httptest.NewRecorder()
	testServer(t, fakeAPI{}, fakeAPI{}).handlePlan(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	resp := decodePlanResponse(t, rec.Body.Bytes())
	require.Equal(t, "success", resp.Status)
	require.Len(t, resp.Data.Partitions, 1)
	require.True(t, time.Unix(300, 0).Equal(resp.Data.Partitions[0].Time), "requested eval time must reach the plan")
}

func TestHandlePlanMissingQuery(t *testing.T) {
	rec := httptest.NewRecorder()
	testServer(t, fakeAPI{}, fakeAPI{}).handlePlan(rec, httptest.NewRequest(http.MethodGet, "/api/v1/plan", nil))

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, errorTypeBadData, decodeErrorType(t, rec.Body.Bytes()))
}

func TestHandlePlanRejectsMalformedQuery(t *testing.T) {
	form := url.Values{"query": {"this is )( not promql"}, "time": {"0"}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/plan?"+form.Encode(), nil)

	rec := httptest.NewRecorder()
	testServer(t, fakeAPI{}, fakeAPI{}).handlePlan(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, errorTypeBadData, decodeErrorType(t, rec.Body.Bytes()))
}

func TestHandlePlanRangeMissingTimeParams(t *testing.T) {
	form := url.Values{"query": {"up"}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/plan_range?"+form.Encode(), nil)

	rec := httptest.NewRecorder()
	testServer(t, fakeAPI{}, fakeAPI{}).handlePlanRange(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, errorTypeBadData, decodeErrorType(t, rec.Body.Bytes()))
}

func TestHandlePlanRangeInvalidParameter(t *testing.T) {
	valid := url.Values{"query": {"up"}, "start": {"0"}, "end": {"60"}, "step": {"15"}}
	tests := map[string]struct {
		param string
		value string
	}{
		"invalid start": {param: "start", value: "bad"},
		"invalid end":   {param: "end", value: "bad"},
		"invalid step":  {param: "step", value: "bad"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			form := url.Values{}
			for k, v := range valid {
				form[k] = v
			}
			form.Set(tc.param, tc.value)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/plan_range?"+form.Encode(), nil)

			rec := httptest.NewRecorder()
			testServer(t, fakeAPI{}, fakeAPI{}).handlePlanRange(rec, req)

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Equal(t, errorTypeBadData, decodeErrorType(t, rec.Body.Bytes()))
		})
	}
}

// blockingServer builds a Server whose single downstream blocks inside its query
// handler until release is closed, with the given max concurrency. The entered
// channel is closed the first time the downstream handler is reached, so a test
// can wait until a query holds a slot before probing the gate.
func blockingServer(t *testing.T, maxConcurrency int, entered chan<- struct{}, release <-chan struct{}) *Server {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	var once sync.Once
	responder := func(w http.ResponseWriter, _ *http.Request) {
		// net/http recovers panics in this per-connection goroutine, so a panic
		// here would otherwise be swallowed and the test would still pass. Fail
		// the test explicitly instead. t.Errorf is safe from a non-test goroutine.
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("downstream responder panicked: %v", r)
			}
		}()
		once.Do(func() {
			if entered != nil {
				close(entered)
			}
		})
		<-release
		writeQueryResult(w, model.Vector{})
	}
	srv := httptest.NewServer(http.HandlerFunc(responder))
	t.Cleanup(srv.Close)

	virtualLabels := map[string]labels.LabelSet{srv.URL: {"region": "eu"}}
	downstreams, err := fanout.BuildDownstreams(virtualLabels, 0)
	require.NoError(t, err)

	labelNames := labels.NewLabelNames("region")
	planner := plan.NewPlanner(virtualLabels, labelNames)
	engine := promql.NewEngine(fanout.EngineDefaultOpts())
	processor := fanout.NewProcessor(logger, engine, planner, downstreams, fanout.ProcessorDefaultOpts())
	metadata := fanout.NewMetadata(logger, downstreams, labelNames, 0)
	config := NewConfig(downstreams, WithQueryMaxConcurrency(maxConcurrency))
	return New(logger, processor, metadata, config)
}

func TestHandleQueryRejectsOverConcurrencyLimit(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	srv := blockingServer(t, 1, entered, release)

	form := url.Values{"query": {"up"}, "time": {"0"}}
	newReq := func() *http.Request {
		return httptest.NewRequest(http.MethodGet, "/api/v1/query?"+form.Encode(), nil)
	}

	// Hold the single slot with a query blocked in its downstream.
	inFlight := httptest.NewRecorder()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		srv.handleQuery(inFlight, newReq())
	}()

	// Wait until the in-flight query has reached the downstream, which means it has
	// taken and is holding the only slot.
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("in-flight query never reached the downstream")
	}

	// A second query while the slot is held is rejected with 429.
	rejected := httptest.NewRecorder()
	srv.handleQuery(rejected, newReq())
	require.Equal(t, http.StatusTooManyRequests, rejected.Code)
	require.Equal(t, errorTypeUnavailable, decodeErrorType(t, rejected.Body.Bytes()))

	// Release the in-flight query; the slot frees and a new query succeeds.
	close(release)
	wg.Wait()
	require.Equal(t, http.StatusOK, inFlight.Code)

	accepted := httptest.NewRecorder()
	srv.handleQuery(accepted, newReq())
	require.Equal(t, http.StatusOK, accepted.Code)
}
