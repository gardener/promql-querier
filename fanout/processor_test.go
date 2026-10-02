// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package fanout

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"

	"github.com/gardener/promql-querier/labels"
	"github.com/gardener/promql-querier/plan"
)

func testProcessor(t *testing.T, opts ProcessorOpts, dsHandlers ...http.HandlerFunc) (*Processor, []string) {
	t.Helper()

	var (
		// Assume at most two downstream handlers.
		regions          = []string{"eu", "us"}
		virtualLabelSets = make(map[string]labels.LabelSet)
		urls             = make([]string, len(dsHandlers))
	)

	for i, h := range dsHandlers {
		srv := httptest.NewServer(h)
		t.Cleanup(srv.Close)

		urls[i] = srv.URL
		virtualLabelSets[srv.URL] = labels.LabelSet{"region": regions[i]}
	}

	downstreams, err := BuildDownstreams(virtualLabelSets, 0)
	require.NoError(t, err)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	planner := plan.NewPlanner(virtualLabelSets, labels.NewLabelNames("region"))
	engine := promql.NewEngine(EngineDefaultOpts())
	return NewProcessor(logger, engine, planner, downstreams, opts), urls
}

type queryEnvelope struct {
	Status   string   `json:"status"`
	Data     any      `json:"data,omitempty"`
	Error    string   `json:"error,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

type queryResult struct {
	ResultType string `json:"resultType"`
	Result     any    `json:"result"`
}

func valueResponse(val model.Value, warnings ...string) http.HandlerFunc {
	body, _ := json.Marshal(queryEnvelope{
		Status:   "success",
		Data:     queryResult{ResultType: val.Type().String(), Result: val},
		Warnings: warnings,
	})
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}
}

func prometheusErrorResponse(_, msg string) http.HandlerFunc {
	body, _ := json.Marshal(queryEnvelope{Status: "error", Error: msg})
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write(body)
	}
}

func transportErrorResponse() http.HandlerFunc {
	return func(_ http.ResponseWriter, _ *http.Request) {
		panic(http.ErrAbortHandler)
	}
}

// blockingResponse returns a handler that writes response headers and then
// blocks until its request context is cancelled, together with a channel that
// is closed once the block is released.If nothing cancels the fetch, it blocks
// until the query timeout.
func blockingResponse() (http.HandlerFunc, <-chan struct{}) {
	released := make(chan struct{})
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done()
		close(released)
	}
	return handler, released
}

func capturingResponse(t *testing.T) (http.HandlerFunc, *url.Values) {
	t.Helper()

	var captured url.Values
	handler := func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		captured = r.PostForm
		w.Header().Set("Content-Type", "application/json")
	}
	return handler, &captured
}

func TestProcessorSendsLookbackDeltaDownstream(t *testing.T) {
	opts := ProcessorDefaultOpts()
	opts.QueryLookbackDelta = model.Duration(90 * time.Second)

	handler, captured := capturingResponse(t)
	processor, _ := testProcessor(t, opts, handler)

	query, err := processor.NewInstantQuery("up", time.Unix(0, 0), 0)
	require.NoError(t, err)

	result := query.Exec(context.Background())
	require.NoError(t, result.Err)

	require.Equal(t, "1m30s", captured.Get("lookback_delta"))
}

func TestEngineReturnsPartialResultAndWarnsOnDownstreamFailure(t *testing.T) {
	processor, _ := testProcessor(t, ProcessorDefaultOpts(), valueResponse(vectorWithSamples(1)), transportErrorResponse())

	query, err := processor.NewInstantQuery("up", time.Unix(0, 0), 0)
	require.NoError(t, err)

	result := query.Exec(context.Background())
	require.NoError(t, result.Err)

	vec, ok := result.Value.(promql.Vector)
	require.True(t, ok)
	require.Len(t, vec, 1)

	warnings, _ := result.Warnings.AsStrings("", 0, 0)
	require.Len(t, warnings, 1)
	require.Regexp(t, `^partition: up: inaccurate result due to incomplete data: http://127.0.0.1:[0-9]+: `, warnings[0])
}

func TestEngineWarnsWhenAllDownstreamsFail(t *testing.T) {
	processor, _ := testProcessor(t, ProcessorDefaultOpts(), transportErrorResponse(), transportErrorResponse())

	query, err := processor.NewInstantQuery("up", time.Unix(0, 0), 0)
	require.NoError(t, err)

	result := query.Exec(context.Background())
	require.NoError(t, result.Err)

	vec, ok := result.Value.(promql.Vector)
	require.True(t, ok)
	require.Empty(t, vec)

	warnings, _ := result.Warnings.AsStrings("", 0, 0)
	require.Len(t, warnings, 2)
	require.Regexp(t, `^partition: up: inaccurate result due to incomplete data: http://127.0.0.1:[0-9]+: `, warnings[0])
	require.Regexp(t, `^partition: up: inaccurate result due to incomplete data: http://127.0.0.1:[0-9]+: `, warnings[1])

	require.NotEqual(t, warnings[0], warnings[1], "each downstream must produce a distinct warning")
}

func TestEngineReturnsErrorWhenFetchedBytesLimitExceeded(t *testing.T) {
	opts := ProcessorDefaultOpts()

	// QueryMaxFetchedBytes should fail the query.
	opts.QueryMaxFetchedSamples = 100_000_000
	opts.QueryMaxFetchedBytes = 100

	processor, _ := testProcessor(t, opts, valueResponse(vectorWithSamples(200)))

	query, err := processor.NewInstantQuery("up", time.Unix(0, 0), 0)
	require.NoError(t, err)

	result := query.Exec(context.Background())
	require.Error(t, result.Err)
	require.EqualError(t, result.Err, "fetched bytes limit exceeded (100 bytes)")
}

func TestEngineHardLimitBreachCancelsSiblingFetches(t *testing.T) {
	tests := []struct {
		name          string
		maxBytes      int64
		maxSamples    int64
		wantErrSubstr string
	}{
		{
			name:          "fetched bytes limit",
			maxBytes:      100,
			maxSamples:    100_000_000,
			wantErrSubstr: "fetched bytes limit exceeded (100 bytes)",
		},
		{
			name:          "fetched samples limit",
			maxBytes:      100_000_000,
			maxSamples:    10,
			wantErrSubstr: "fetched samples limit exceeded (10 samples)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := ProcessorDefaultOpts()
			opts.QueryMaxFetchedBytes = tt.maxBytes
			opts.QueryMaxFetchedSamples = tt.maxSamples
			opts.QueryTimeout = 5 * time.Second

			blocker, released := blockingResponse()
			processor, _ := testProcessor(t, opts, valueResponse(vectorWithSamples(200)), blocker)

			query, err := processor.NewInstantQuery("up", time.Unix(0, 0), 0)
			require.NoError(t, err)

			start := time.Now()
			result := query.Exec(context.Background())
			elapsed := time.Since(start)

			require.Error(t, result.Err)
			require.Contains(t, result.Err.Error(), tt.wantErrSubstr)

			require.Less(t, elapsed, opts.QueryTimeout, "query should return promptly, not wait for the timeout")

			// The blocking downstream's fetch must actually have been cancelled.
			select {
			case <-released:
			case <-time.After(time.Second):
				t.Fatal("blocking downstream fetch was not cancelled by the limit breach")
			}
		})
	}
}

func TestEngineReturnsErrorWhenFetchedSamplesLimitExceeded(t *testing.T) {
	opts := ProcessorDefaultOpts()

	// QueryMaxFetchedSamples limit should fail the query.
	opts.QueryMaxFetchedSamples = 100
	opts.QueryMaxFetchedBytes = 100_000_000

	processor, _ := testProcessor(t, opts, valueResponse(vectorWithSamples(200)))

	query, err := processor.NewInstantQuery("up", time.Unix(0, 0), 0)
	require.NoError(t, err)

	result := query.Exec(context.Background())
	require.Error(t, result.Err)
	require.EqualError(t, result.Err, "fetched samples limit exceeded (100 samples)")
}

func TestEngineFailsOnDownstreamPrometehusError(t *testing.T) {
	processor, _ := testProcessor(t, ProcessorDefaultOpts(),
		valueResponse(vectorWithSamples(1)),
		prometheusErrorResponse("bad_data", "invalid expression: unexpected end of input"),
	)

	query, err := processor.NewInstantQuery("up", time.Unix(0, 0), 0)
	require.NoError(t, err)

	result := query.Exec(context.Background())
	require.Error(t, result.Err)
	require.Regexp(t, `^partition: up: http://127.0.0.1:[0-9]+: invalid expression: unexpected end of input$`, result.Err.Error())
}

func TestEngineForwardsDownstreamWarnings(t *testing.T) {
	processor, _ := testProcessor(t, ProcessorDefaultOpts(),
		valueResponse(vectorWithSamples(1), "some warning from eu"),
		valueResponse(vectorWithSamples(1)),
	)

	query, err := processor.NewInstantQuery("up", time.Unix(0, 0), 0)
	require.NoError(t, err)

	result := query.Exec(context.Background())
	require.NoError(t, result.Err)

	warnings, _ := result.Warnings.AsStrings("", 0, 0)
	require.Len(t, warnings, 1)
	require.Regexp(t, `^http://127.0.0.1:[0-9]+: some warning from eu$`, warnings[0])
}

func TestEngineTruncatesResultToLimitAndWarns(t *testing.T) {
	// Each downstream returns three series, so the evaluated vector has six. A
	// limit of two caps the result and adds the truncation warning.
	processor, _ := testProcessor(t, ProcessorDefaultOpts(),
		valueResponse(vectorWithSamples(3), "some warning from eu"),
		valueResponse(vectorWithSamples(3)),
	)

	query, err := processor.NewInstantQuery("up", time.Unix(0, 0), 2)
	require.NoError(t, err)

	result := query.Exec(context.Background())
	require.NoError(t, result.Err)

	vec, ok := result.Value.(promql.Vector)
	require.True(t, ok)
	require.Len(t, vec, 2)

	warnings, _ := result.Warnings.AsStrings("", 0, 0)
	sort.Strings(warnings)

	require.Len(t, warnings, 2)
	require.Regexp(t, `^http://127.0.0.1:[0-9]+: some warning from eu$`, warnings[0])
	require.Equal(t, "results truncated due to limit", warnings[1])
}
