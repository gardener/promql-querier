// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

// Package fanout executes query plans against downstream Prometheus endpoints.
// It resolves a plan into concurrent downstream fetches, feeds the results into
// the PromQL engine, and serves the merged result back to the HTTP layer.
package fanout

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/promql"
	promparser "github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/util/annotations"
	"golang.org/x/sync/errgroup"

	"github.com/gardener/promql-querier/plan"
)

// EngineDefaultOpts returns the PromQL engine options the PromQL Querier runs with by default.
func EngineDefaultOpts() promql.EngineOpts {
	// The local engine needs to run with a 1ns LookbackDelta as if it was effectively
	// disabled. Lookback deltas needs to be resolved by downstreams. The local engine
	// only merges the results from downstreams - it should not apply lookback again.
	const lookbackDelta = 1 * time.Nanosecond

	return promql.EngineOpts{
		LookbackDelta:            lookbackDelta,
		Timeout:                  2 * time.Minute,
		EnableAtModifier:         true,
		EnableNegativeOffset:     true,
		MaxSamples:               50_000_000,
		NoStepSubqueryIntervalFn: func(int64) int64 { return time.Minute.Milliseconds() },
		Parser:                   plan.NewParser(),
	}
}

// Query is a planned instant or range query that is ready to execute against
// its downstreams.
type Query struct {
	plan      *plan.Plan
	processor *Processor
	limit     int
}

// Exec runs the query and returns its result.
func (q *Query) Exec(ctx context.Context) *promql.Result {
	return q.processor.exec(ctx, q)
}

// Processor plans queries, fetches the required series from downstreams, and
// evaluates the resulting expression with the PromQL engine.
type Processor struct {
	logger      *slog.Logger
	parser      promparser.Parser
	engine      *promql.Engine
	planner     *plan.Planner
	downstreams map[string]Downstream
	opts        ProcessorOpts
}

// ProcessorOpts configures the per-query resource limits a Processor enforces.
type ProcessorOpts struct {
	// QueryMaxFetchedBytes caps the total uncompressed bytes one query may fetch
	// across all its downstreams. 0 means unlimited.
	QueryMaxFetchedBytes int64
	// QueryMaxFetchedSamples caps the total samples one query may fetch across
	// all its downstreams.
	QueryMaxFetchedSamples int64
	// QueryMaxFetchConcurrency caps how many outbound downstream requests one
	// query may have in flight at once, bounding goroutine and connection fan-out
	// regardless of plan shape. 0 means unlimited.
	QueryMaxFetchConcurrency int
	// QueryTimeout bounds the total query processing time: downstream fetch plus
	// local engine evaluation. It is applied as a context deadline over both
	// phases.
	QueryTimeout time.Duration
	// QueryLookbackDelta is sent to every downstream as the lookback_delta query
	// parameter, overriding the downstream's own configured lookback delta so the
	// whole fleet resolves lookback consistently.
	QueryLookbackDelta model.Duration
}

// QueryDefaultTimeout bounds the total query processing time by default:
// downstream fetch plus local engine evaluation.
const QueryDefaultTimeout = 5 * time.Minute

// ProcessorDefaultOpts returns the default per-query resource limits.
func ProcessorDefaultOpts() ProcessorOpts {
	return ProcessorOpts{
		QueryMaxFetchedBytes:     512 * 1024 * 1024, // 512 MB
		QueryMaxFetchedSamples:   50_000_000,        // 50 million samples
		QueryMaxFetchConcurrency: 100,
		QueryTimeout:             QueryDefaultTimeout,
		QueryLookbackDelta:       model.Duration(5 * time.Minute),
	}
}

// NewProcessor returns a Processor for query planning and execution.
func NewProcessor(logger *slog.Logger, engine *promql.Engine, planner *plan.Planner, downstreams map[string]Downstream, opts ProcessorOpts) *Processor {
	return &Processor{
		logger:      logger,
		engine:      engine,
		planner:     planner,
		downstreams: downstreams,
		opts:        opts,
		parser:      plan.NewParser(),
	}
}

// NewInstantQuery plans an instant query and returns it ready to execute.
// The limit caps the number of returned series.
func (proc *Processor) NewInstantQuery(query string, ts time.Time, limit int) (*Query, error) {
	expr, err := proc.parser.ParseExpr(query)
	if err != nil {
		return nil, err
	}

	p, err := proc.planner.PlanInstant(expr, ts)
	if err != nil {
		return nil, err
	}

	return &Query{
		plan:      p,
		processor: proc,
		limit:     limit,
	}, nil
}

// NewRangeQuery plans a range query over [start, end] at step and returns
// it ready to execute. The limit caps the number of returned series.
func (proc *Processor) NewRangeQuery(query string, start, end time.Time, step time.Duration, limit int) (*Query, error) {
	expr, err := proc.parser.ParseExpr(query)
	if err != nil {
		return nil, err
	}

	p, err := proc.planner.PlanRange(expr, start, end, step)
	if err != nil {
		return nil, err
	}

	return &Query{
		plan:      p,
		processor: proc,
		limit:     limit,
	}, nil
}

func (proc *Processor) newLocalQuery(ctx context.Context, p *plan.Plan, fetched fetchedSeries, expr string) (promql.Query, error) {
	opts := promql.NewPrometheusQueryOpts(false, 0)
	if p.IsRange() {
		return proc.engine.NewRangeQuery(ctx, fetched, opts, expr, p.Start(), p.End(), p.Step())
	}
	return proc.engine.NewInstantQuery(ctx, fetched, opts, expr, p.Time())
}

// PlanInstant builds the instant query plan for query at ts without executing it.
func (proc *Processor) PlanInstant(query string, ts time.Time) (*plan.Plan, error) {
	expr, err := proc.parser.ParseExpr(query)
	if err != nil {
		return nil, err
	}
	return proc.planner.PlanInstant(expr, ts)
}

// PlanRange builds the range query plan for query over [start, end] with step
// without executing it.
func (proc *Processor) PlanRange(query string, start, end time.Time, step time.Duration) (*plan.Plan, error) {
	expr, err := proc.parser.ParseExpr(query)
	if err != nil {
		return nil, err
	}
	return proc.planner.PlanRange(expr, start, end, step)
}

func (proc *Processor) exec(ctx context.Context, query *Query) *promql.Result {
	ctx = contextWithFetchedBytesLimit(ctx, NewFetchedBytesLimit(proc.opts.QueryMaxFetchedBytes))
	ctx = contextWithFetchedSamplesLimit(ctx, NewFetchedSamplesLimit(proc.opts.QueryMaxFetchedSamples))
	ctx = contextWithConcurrentFetches(ctx, NewFetchLimiter(proc.opts.QueryMaxFetchConcurrency))

	ctx, timeout := context.WithTimeout(ctx, proc.opts.QueryTimeout)
	defer timeout()

	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	partitions := query.plan.Partitions()
	fetched := make(fetchedSeries, len(partitions))

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		errs     []error
		warnings annotations.Annotations
	)

	for i, part := range partitions {
		wg.Add(1)
		go func(i int, part plan.Partition) {
			defer wg.Done()
			series, warns, err := proc.fetchPartition(ctx, i, part)
			mu.Lock()
			defer mu.Unlock()
			warnings.Merge(warns)
			if err != nil {
				errs = append(errs, err)
				cancel(err)
				return
			}
			fetched[i] = series
		}(i, part)
	}
	wg.Wait()

	if len(errs) > 0 {
		return &promql.Result{Err: errors.Join(errs...)}
	}

	expr := query.plan.Expr().String()
	localQuery, err := proc.newLocalQuery(ctx, query.plan, fetched, expr)
	if err != nil {
		return &promql.Result{Err: fmt.Errorf("failed to create local query: %w", err)}
	}
	defer localQuery.Close()

	log := proc.logger.With("request_uid", requestUID(ctx))
	log.Debug("execute local query", "expr", expr)

	result := localQuery.Exec(ctx)
	truncateResult(result, query.limit)
	result.Warnings.Merge(warnings)

	return result
}

func truncateResult(result *promql.Result, limit int) {
	if limit <= 0 {
		return
	}
	switch v := result.Value.(type) {
	case promql.Vector:
		if len(v) > limit {
			result.Value = v[:limit]
			result.Warnings.Add(errors.New("results truncated due to limit"))
		}
	case promql.Matrix:
		if len(v) > limit {
			result.Value = v[:limit]
			result.Warnings.Add(errors.New("results truncated due to limit"))
		}
	}
}

// fetchPartition queries every downstream of the partition. A downstream that returns a
// a promv1.Error or breaches a hard limit (a FetchedBytesLimitError or
// FetchedSamplesLimitError) fails the whole query. The promv1.Error error
// means the downstream understood the request and rejected it. The hard limit
// is a deliberate cap that must not be silently dropped. A downstream that
// fails at the transport level is reported as a warning and skipped, so the
// query can still proceed with other downstreams. Because the PromQL Querier queries
// disjoint downstreams, a dropped downstream means missing series, so each such warning
// states the result may be inaccurate.
func (proc *Processor) fetchPartition(ctx context.Context, idx int, partition plan.Partition) ([]promql.Series, annotations.Annotations, error) {
	var (
		expr        = partition.DownstreamExpr().String()
		downstreams = partition.Downstreams()
		fetches     = concurrentFetchesFromContext(ctx)

		log = proc.logger.With("request_uid", requestUID(ctx))

		mu       sync.Mutex
		warnings annotations.Annotations
		series   []promql.Series
	)

	group, ctx := errgroup.WithContext(ctx)
	for _, url := range downstreams {
		ds := proc.downstreams[url]
		log.Debug("fetch partition", "partition", idx, "query", expr, "downstream", url)
		group.Go(func() error {
			release, err := fetches.Acquire(ctx)
			if err != nil {
				// The context was cancelled while waiting for a slot, so a sibling
				// already failed the query or the client went away. The cancel cause
				// carries the real error; nothing to add here.
				return nil
			}
			defer release()

			val, remoteWarnings, err := proc.fetchRemote(ctx, ds, partition, expr)

			mu.Lock()
			defer mu.Unlock()

			for _, w := range remoteWarnings {
				warnings.Add(fmt.Errorf("%s: %s", url, w))
			}

			if err != nil {
				var (
					apiErr     *promv1.Error
					bytesErr   *FetchedBytesLimitError
					samplesErr *FetchedSamplesLimitError
				)

				switch {
				case errors.As(err, &apiErr):
					log.Error("failed to fetch partition", "query", expr, "downstream", url, "error", err)
					return fmt.Errorf("partition: %s: %s: %s", expr, url, apiErr.Msg)
				case errors.As(err, &bytesErr), errors.As(err, &samplesErr):
					log.Error("failed to fetch partition", "query", expr, "downstream", url, "error", err)
					// The limit is a query-wide budget the fan-out blew collectively,
					// so return the error without naming any partition or downstream.
					return err
				default:
					warnings.Add(fmt.Errorf("partition: %s: inaccurate result due to incomplete data: %s: %w", expr, url, err))
					// Do not log context cancelled errors on siblings.
					if !errors.Is(ctx.Err(), context.Canceled) {
						log.Error("failed to fetch partition", "query", expr, "downstream", url, "error", err)
					}
					return nil
				}
			}

			series = append(series, val...)
			return nil
		})
	}

	if err := group.Wait(); err != nil {
		return nil, nil, err
	}

	return series, warnings, nil
}

func (proc *Processor) fetchRemote(ctx context.Context, ds Downstream, partition plan.Partition, expr string) ([]promql.Series, promv1.Warnings, error) {
	if partition.IsRange() {
		r := promv1.Range{Start: partition.Start(), End: partition.End(), Step: partition.Step()}
		return ds.querier.queryRange(ctx, expr, r, proc.opts.QueryLookbackDelta, ds.labels)
	}
	return ds.querier.query(ctx, expr, partition.Time(), proc.opts.QueryLookbackDelta, ds.labels)
}
