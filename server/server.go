// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

// Package server exposes the HTTP API.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/klauspost/compress/gzhttp"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/gardener/promql-querier/fanout"
	"github.com/gardener/promql-querier/ui"
)

// Server exposes the HTTP API.
type Server struct {
	processor  *fanout.Processor
	metadata   *fanout.Metadata
	config     *Config
	logger     *slog.Logger
	querySlots chan struct{}
}

// New builds the HTTP server.
func New(logger *slog.Logger, processor *fanout.Processor, metadata *fanout.Metadata, config *Config) *Server {
	slots := make(chan struct{}, config.queryMaxConcurrency)
	return &Server{
		processor:  processor,
		metadata:   metadata,
		config:     config,
		logger:     logger,
		querySlots: slots,
	}
}

// acquireSlot tries to reserve a query evaluation slot without blocking. It
// returns a release function and true on success, or false when the concurrency
// cap is already reached.
func (s *Server) acquireSlot() (func(), bool) {
	if s.querySlots == nil {
		return func() {}, true
	}
	select {
	case s.querySlots <- struct{}{}:
		return func() { <-s.querySlots }, true
	default:
		return nil, false
	}
}

// Handler builds the HTTP handler exposing the query, plan, metadata, readiness
// and status endpoints together with the embedded UI, wrapped in gzip.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/-/ready", s.handleReady)
	mux.HandleFunc("/api/v1/query", s.handleQuery)
	mux.HandleFunc("/api/v1/query_range", s.handleQueryRange)
	mux.HandleFunc("/api/v1/plan", s.handlePlan)
	mux.HandleFunc("/api/v1/plan_range", s.handlePlanRange)
	mux.HandleFunc("/api/v1/series", s.handleSeries)
	mux.HandleFunc("/api/v1/labels", s.handleLabelNames)
	mux.HandleFunc("/api/v1/label/", s.handleLabelValues)
	mux.HandleFunc("/api/v1/status/config", s.handleStatusConfig)
	s.registerUI(mux)
	return gzhttp.GzipHandler(mux)
}

func (s *Server) registerUI(mux *http.ServeMux) {
	dist, err := fs.Sub(ui.Files, "dist")
	if err != nil {
		panic(fmt.Sprintf("embedded UI dist not found: %v", err))
	}

	assets := http.FileServer(http.FS(dist))
	mux.Handle("/static/", http.StripPrefix("/static/", assets))
	mux.HandleFunc("/assets/third-party-licenses.txt", func(w http.ResponseWriter, r *http.Request) {
		s.serveFile(w, r, dist, "assets/third-party-licenses.txt", "text/plain; charset=utf-8")
	})

	app := "app.html"
	if _, err := fs.Stat(dist, app); err != nil {
		app = "index.html"
	}

	serveApp := func(w http.ResponseWriter, r *http.Request) {
		s.serveFile(w, r, dist, app, "text/html; charset=utf-8")
	}

	mux.HandleFunc("/query", serveApp)
	mux.HandleFunc("/config", serveApp)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/query", http.StatusFound)
	})
}

func (s *Server) serveFile(w http.ResponseWriter, _ *http.Request, dist fs.FS, name, contentType string) {
	data, err := fs.ReadFile(dist, name)
	if err != nil {
		http.Error(w, "UI not found", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", contentType)
	if _, err := w.Write(data); err != nil {
		s.logger.Error("failed to write UI response", "err", err)
	}
}

func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprint(w, "Gardener PromQL Querier is Ready."); err != nil {
		s.logger.Error("failed to write ready response", "err", err)
	}
}

func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	release, ok := s.acquireSlot()
	if !ok {
		s.writeError(w, http.StatusTooManyRequests, errorTypeUnavailable, errors.New("too many concurrent queries"))
		return
	}
	defer release()

	if err := r.ParseForm(); err != nil {
		s.writeError(w, http.StatusBadRequest, errorTypeBadData, err)
		return
	}

	queryParam := r.FormValue("query")
	if queryParam == "" {
		s.writeError(w, http.StatusBadRequest, errorTypeBadData, errors.New(`missing required parameter "query"`))
		return
	}

	ts, err := parseOptionalTimeNow(r, "time")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, errorTypeBadData, err)
		return
	}

	limit, err := parseLimit(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, errorTypeBadData, err)
		return
	}

	var (
		uid = fanout.NewRequestUID()
		ctx = fanout.ContextWithRequestUID(r.Context(), uid)
		log = s.logger.With("request_uid", uid)
	)

	log.Debug("received query", "client", r.RemoteAddr, "query", queryParam,
		"time", ts, "limit", limit)

	query, err := s.processor.NewInstantQuery(queryParam, ts, limit)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, errorTypeBadData, err)
		return
	}

	result := query.Exec(ctx)
	if result.Err != nil {
		if !errors.Is(ctx.Err(), context.Canceled) {
			s.writeError(w, http.StatusUnprocessableEntity, errorTypeExecution, result.Err)
		}
		log.Debug("query execution finished with error")
		return
	}

	warnings, _ := result.Warnings.AsStrings(queryParam, 0, 0)
	s.writeValue(w, result.Value, warnings)

	log.Debug("query execution finished successfully")
}

func (s *Server) handleQueryRange(w http.ResponseWriter, r *http.Request) {
	release, ok := s.acquireSlot()
	if !ok {
		s.writeError(w, http.StatusTooManyRequests, errorTypeUnavailable, errors.New("too many concurrent queries"))
		return
	}
	defer release()

	if err := r.ParseForm(); err != nil {
		s.writeError(w, http.StatusBadRequest, errorTypeBadData, err)
		return
	}

	queryParam := r.FormValue("query")
	if queryParam == "" {
		s.writeError(w, http.StatusBadRequest, errorTypeBadData, errors.New(`missing required parameter "query"`))
		return
	}

	start, err := parseRequiredTime(r, "start")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, errorTypeBadData, err)
		return
	}

	end, err := parseRequiredTime(r, "end")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, errorTypeBadData, err)
		return
	}

	step, err := parseStep(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, errorTypeBadData, err)
		return
	}

	limit, err := parseLimit(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, errorTypeBadData, err)
		return
	}

	var (
		uid = fanout.NewRequestUID()
		ctx = fanout.ContextWithRequestUID(r.Context(), uid)
		log = s.logger.With("request_uid", uid)
	)

	log.Debug("received ranged query", "client", r.RemoteAddr, "query", queryParam,
		"start", start, "end", end, "step", step, "limit", limit)

	query, err := s.processor.NewRangeQuery(queryParam, start, end, step, limit)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, errorTypeBadData, err)
		return
	}

	result := query.Exec(ctx)
	if result.Err != nil {
		if !errors.Is(ctx.Err(), context.Canceled) {
			s.writeError(w, http.StatusUnprocessableEntity, errorTypeExecution, result.Err)
		}
		log.Debug("ranged query execution finished with error")
		return
	}

	warnings, _ := result.Warnings.AsStrings(queryParam, 0, 0)
	s.writeValue(w, result.Value, warnings)

	log.Debug("ranged query execution finished successfully")
}

func (s *Server) handlePlan(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.writeError(w, http.StatusBadRequest, errorTypeBadData, err)
		return
	}

	queryParam := r.FormValue("query")
	if queryParam == "" {
		s.writeError(w, http.StatusBadRequest, errorTypeBadData, errors.New(`missing required parameter "query"`))
		return
	}

	ts, err := parseOptionalTimeNow(r, "time")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, errorTypeBadData, err)
		return
	}

	plan, err := s.processor.PlanInstant(queryParam, ts)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, errorTypeBadData, err)
		return
	}

	s.writeSuccess(w, plan.Describe(), nil)
}

func (s *Server) handlePlanRange(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.writeError(w, http.StatusBadRequest, errorTypeBadData, err)
		return
	}

	queryParam := r.FormValue("query")
	if queryParam == "" {
		s.writeError(w, http.StatusBadRequest, errorTypeBadData, errors.New(`missing required parameter "query"`))
		return
	}

	start, err := parseRequiredTime(r, "start")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, errorTypeBadData, err)
		return
	}

	end, err := parseRequiredTime(r, "end")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, errorTypeBadData, err)
		return
	}

	step, err := parseStep(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, errorTypeBadData, err)
		return
	}

	plan, err := s.processor.PlanRange(queryParam, start, end, step)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, errorTypeBadData, err)
		return
	}

	s.writeSuccess(w, plan.Describe(), nil)
}

func (s *Server) handleStatusConfig(w http.ResponseWriter, _ *http.Request) {
	s.writeSuccess(w, s.config, nil)
}

func (s *Server) handleSeries(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.writeError(w, http.StatusBadRequest, errorTypeBadData, err)
		return
	}

	matchers := r.Form["match[]"]
	start, err := parseOptionalTimeZero(r, "start")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, errorTypeBadData, err)
		return
	}

	end, err := parseOptionalTimeZero(r, "end")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, errorTypeBadData, err)
		return
	}

	limit, err := parseLimit(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid limit parameter", err)
		return
	}

	result, warnings, err := s.metadata.Series(r.Context(), matchers, start, end, limit)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, errorTypeInternal, err)
		return
	}

	s.writeSuccess(w, result, warnings)
}

func (s *Server) handleLabelNames(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.writeError(w, http.StatusBadRequest, errorTypeBadData, err)
		return
	}

	matchers := r.Form["match[]"]
	start, err := parseOptionalTimeZero(r, "start")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, errorTypeBadData, err)
		return
	}

	end, err := parseOptionalTimeZero(r, "end")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, errorTypeBadData, err)
		return
	}

	limit, err := parseLimit(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid limit parameter", err)
		return
	}

	result, warnings, err := s.metadata.LabelNames(r.Context(), matchers, start, end, limit)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, errorTypeInternal, err)
		return
	}

	s.writeSuccess(w, result, warnings)
}

func (s *Server) handleLabelValues(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.writeError(w, http.StatusBadRequest, errorTypeBadData, err)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/v1/label/")
	label := strings.TrimSuffix(path, "/values")
	if label == "" {
		s.writeError(w, http.StatusBadRequest, errorTypeBadData, nil)
		return
	}

	matchers := r.Form["match[]"]
	start, err := parseOptionalTimeZero(r, "start")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, errorTypeBadData, err)
		return
	}

	end, err := parseOptionalTimeZero(r, "end")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, errorTypeBadData, err)
		return
	}

	limit, err := parseLimit(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid limit parameter", err)
		return
	}

	result, warnings, err := s.metadata.LabelValues(r.Context(), label, matchers, start, end, limit)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, errorTypeInternal, err)
		return
	}

	s.writeSuccess(w, result, warnings)
}

const (
	errorTypeBadData     = "bad_data"
	errorTypeExecution   = "execution"
	errorTypeInternal    = "internal"
	errorTypeUnavailable = "unavailable"
)

type apiResponse struct {
	Status    string   `json:"status"`
	Data      any      `json:"data,omitempty"`
	Error     string   `json:"error,omitempty"`
	ErrorType string   `json:"errorType,omitempty"`
	Warnings  []string `json:"warnings,omitempty"`
}

func (s *Server) writeValue(w http.ResponseWriter, value parser.Value, warnings promv1.Warnings) {
	data := struct {
		ResultType string `json:"resultType"`
		Result     any    `json:"result"`
	}{
		ResultType: string(value.Type()),
		Result:     value,
	}
	s.writeSuccess(w, data, warnings)
}

func (s *Server) writeSuccess(w http.ResponseWriter, data any, warnings promv1.Warnings) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(apiResponse{
		Status:   "success",
		Data:     data,
		Warnings: warnings,
	}); err != nil {
		s.logger.Error("failed to encode JSON response", "err", err)
	}
}

func (s *Server) writeError(w http.ResponseWriter, code int, errorType string, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	if encErr := json.NewEncoder(w).Encode(apiResponse{
		Status:    "error",
		ErrorType: errorType,
		Error:     errMsg,
	}); encErr != nil {
		s.logger.Error("failed to encode error response", "err", encErr)
	}
}

func parseOptionalTimeNow(request *http.Request, param string) (time.Time, error) {
	if ts := request.FormValue(param); ts != "" {
		return parseTime(ts)
	}
	return time.Now().Truncate(time.Millisecond), nil
}

func parseOptionalTimeZero(request *http.Request, param string) (time.Time, error) {
	if ts := request.FormValue(param); ts != "" {
		return parseTime(ts)
	}
	return time.Time{}, nil
}

func parseRequiredTime(request *http.Request, param string) (time.Time, error) {
	if ts := request.FormValue(param); ts != "" {
		return parseTime(ts)
	}
	return time.Time{}, fmt.Errorf("missing required parameter %q", param)
}

// parseTime parses either a float count of seconds since the Unix epoch or an
// RFC3339 string, into a millisecond-aligned time. The alignment ensures the
// local lookback delta of 1 nanosecond behaves like if there was no local
// lookback delta at all.
func parseTime(ts string) (time.Time, error) {
	if seconds, err := strconv.ParseFloat(ts, 64); err == nil {
		return time.UnixMilli(int64(math.Round(seconds * 1000))), nil
	}
	parsed, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.Truncate(time.Millisecond), nil
}

func parseStep(request *http.Request) (time.Duration, error) {
	raw := request.FormValue("step")
	if raw == "" {
		return 0, fmt.Errorf(`missing required parameter "step"`)
	}

	// A bare float means seconds
	if seconds, err := strconv.ParseFloat(raw, 64); err == nil {
		nanos := seconds * float64(time.Second)
		if nanos > math.MaxInt64 || nanos < math.MinInt64 {
			return 0, fmt.Errorf("step %q overflows int64 nanoseconds", raw)
		}
		return time.Duration(nanos), nil
	}

	// Otherwise fall back to using Proemtheus' duration parser.
	d, err := model.ParseDuration(raw)
	if err != nil {
		return 0, err
	}

	return time.Duration(d), nil
}

func parseLimit(request *http.Request) (int, error) {
	raw := request.FormValue("limit")
	if raw == "" {
		return 0, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("limit must be an integer: %w", err)
	}
	if limit < 0 {
		return 0, fmt.Errorf("limit must not be negative")
	}
	return limit, nil
}
