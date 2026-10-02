// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package fanout

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	promlabels "github.com/prometheus/prometheus/model/labels"
	promparser "github.com/prometheus/prometheus/promql/parser"

	"github.com/gardener/promql-querier/labels"
	"github.com/gardener/promql-querier/promql"
)

// Metadata serves the Prometheus metadata endpoints (Series, LabelNames,
// LabelValues) by fanning out to every relevant downstream, injecting virtual
// labels, and merging the results.
type Metadata struct {
	logger               *slog.Logger
	downstreams          map[string]Downstream
	labelNames           labels.LabelNames
	maxConcurrentFetches int
}

// NewMetadata returns a Metadata that queries the given downstreams, treats the
// given names as virtual labels, and caps concurrent downstream fetches at
// maxConcurrentFetches.
func NewMetadata(logger *slog.Logger, downstreams map[string]Downstream, labelNames labels.LabelNames, maxConcurrentFetches int) *Metadata {
	return &Metadata{
		logger:               logger,
		downstreams:          downstreams,
		labelNames:           labelNames,
		maxConcurrentFetches: maxConcurrentFetches,
	}
}

// Series serves the series metadata endpoint. It requires at least one match[]
// selector.
func (m *Metadata) Series(ctx context.Context, matchers []string, start, end time.Time, limit int) ([]model.LabelSet, promv1.Warnings, error) {
	// Series() is the only metadata endpoint that requires at least one match[] parameter.
	if len(matchers) == 0 {
		return nil, nil, fmt.Errorf("no match[] parameter provided")
	}

	if err := validateNonEmptyMatchers(matchers); err != nil {
		return nil, nil, err
	}

	plan, err := m.planMatchers(matchers)
	if err != nil {
		return nil, nil, err
	}

	call := func(ds Downstream, matches []string) ([]model.LabelSet, promv1.Warnings, error) {
		sers, warns, err := ds.client.Series(ctx, matches, start, end, withLimit(limit)...)
		if err != nil {
			return nil, nil, err
		}
		injected := make([]model.LabelSet, len(sers))
		for j, s := range sers {
			injected[j] = withVirtualLabelSet(s, ds.labels)
		}
		return injected, warns, nil
	}

	series, warnings, err := fanOut(ctx, m, plan, "series", call)
	if err != nil {
		return nil, nil, err
	}

	var (
		result = []model.LabelSet{}
		seen   = make(map[string]struct{})
	)

	for _, ser := range series {
		s := ser.String()
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			result = append(result, ser)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].String() < result[j].String()
	})

	result, warnings = truncate(result, warnings, limit)
	return result, warnings, nil
}

// LabelNames serves the label names metadata endpoint.
func (m *Metadata) LabelNames(ctx context.Context, matchers []string, start, end time.Time, limit int) ([]string, promv1.Warnings, error) {
	if err := validateNonEmptyMatchers(matchers); err != nil {
		return nil, nil, err
	}

	plan, err := m.planMatchers(matchers)
	if err != nil {
		return nil, nil, err
	}

	call := func(ds Downstream, matches []string) ([]string, promv1.Warnings, error) {
		labelNames, warnings, err := ds.client.LabelNames(ctx, matches, start, end, withLimit(limit)...)
		names := make([]string, len(labelNames))
		for i, name := range labelNames {
			names[i] = string(name)
		}
		for name, value := range ds.labels {
			if value != "" {
				names = append(names, name)
			}
		}
		return names, warnings, err
	}

	names, warnings, err := fanOut(ctx, m, plan, "labels", call)
	if err != nil {
		return nil, nil, err
	}

	var (
		result = []string{}
		seen   = make(map[string]struct{})
	)

	for _, name := range names {
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			result = append(result, name)
		}
	}

	sort.Strings(result)
	result, warnings = truncate(result, warnings, limit)
	return result, warnings, nil
}

// LabelValues serves the label values metadata endpoint. If the label is virtual,
// it uses the series endpoint with limit 1 to probe which downstreams contain
// series for the given matchers and time range, then returns the virtual label
// values for those downstreams.
func (m *Metadata) LabelValues(ctx context.Context, label string, matchers []string, start, end time.Time, limit int) (model.LabelValues, promv1.Warnings, error) {
	if err := validateNonEmptyMatchers(matchers); err != nil {
		return nil, nil, err
	}

	plan, err := m.planMatchers(matchers)
	if err != nil {
		return nil, nil, err
	}

	if m.labelNames.Has(label) {
		result, warnings, err := m.virtualLabelValues(ctx, label, plan, start, end)
		result, warnings = truncate(result, warnings, limit)
		return result, warnings, err
	}

	call := func(ds Downstream, matches []string) ([]model.LabelValue, promv1.Warnings, error) {
		return ds.client.LabelValues(ctx, label, matches, start, end, withLimit(limit)...)
	}

	values, warnings, err := fanOut(ctx, m, plan, "label/values", call)
	if err != nil {
		return nil, nil, err
	}

	var (
		result = model.LabelValues{}
		seen   = make(map[string]struct{})
	)

	for _, value := range values {
		v := string(value)
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			result = append(result, value)
		}
	}

	sort.Sort(result)
	result, warnings = truncate(result, warnings, limit)
	return result, warnings, nil
}

func fanOut[T any](
	ctx context.Context,
	m *Metadata,
	plan metadataPlan,
	name string,
	call func(ds Downstream, match []string) ([]T, promv1.Warnings, error),
) ([]T, promv1.Warnings, error) {
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		results  []T
		warnings promv1.Warnings
		errs     []error
	)

	fetches := NewFetchLimiter(m.maxConcurrentFetches)

	collect := func(url string, result []T, warns promv1.Warnings, err error) {
		mu.Lock()
		defer mu.Unlock()

		if err != nil {
			var apiErr *promv1.Error
			if errors.As(err, &apiErr) {
				errs = append(errs, fmt.Errorf("downstream %q: %s", url, apiErr.Msg))
				return
			}
			m.logger.Error("failed to execute metadata request", "name", name, "downstream", url, "error", err)
			warnings = append(warnings, fmt.Sprintf("%s: inaccurate result due to incomplete data: %s", url, err))
			return
		}

		warnings = appendWarnings(warnings, url, warns)
		results = append(results, result...)
	}

	for url := range plan {
		var (
			ds      = m.downstreams[url]
			matches = plan[url]
		)
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			release, err := fetches.Acquire(ctx)
			if err != nil {
				// The context was cancelled while waiting for a slot, so the client
				// went away. There is no result to collect and no failure to report.
				return
			}
			defer release()
			result, warns, err := call(ds, matches)
			collect(url, result, warns, err)
		}(url)
	}
	wg.Wait()

	if len(errs) > 0 {
		return nil, nil, errors.Join(errs...)
	}

	return results, warnings, nil
}

type metadataPlan map[string][]string

// planMatchers resolves each match[] selector to the downstreams its virtual
// matchers reach and removes those virtual label matchers from the selector.
func (m *Metadata) planMatchers(matchers []string) (metadataPlan, error) {
	if len(matchers) == 0 {
		// Return everything from all downstreams if not matchers are given.
		plan := make(metadataPlan, len(m.downstreams))
		for url := range m.downstreams {
			plan[url] = []string{`{__name__!=""}`}
		}
		return plan, nil
	}

	plan := make(metadataPlan)
	parser := promparser.NewParser(promparser.Options{})
	for _, raw := range matchers {
		parsed, err := parser.ParseMetricSelector(raw)
		if err != nil {
			return nil, fmt.Errorf("parsing matcher %q: %w", raw, err)
		}

		urls := m.downstreamURLs()
		var keep []*promlabels.Matcher
		for _, pm := range parsed {
			if m.labelNames.Has(pm.Name) {
				urls = matchingDownstreams(m.downstreams, urls, pm)
			} else {
				keep = append(keep, pm)
			}
		}

		keep = promql.EnsureNonEmptySelector(keep)
		ms := matchersToString(keep)
		for _, url := range urls {
			plan[url] = append(plan[url], ms)
		}
	}

	return plan, nil
}

func (m *Metadata) downstreamURLs() []string {
	result := make([]string, 0, len(m.downstreams))
	for url := range m.downstreams {
		result = append(result, url)
	}
	sort.Strings(result)
	return result
}

// virtualLabelValues returns the distinct values a virtual label takes across the
// downstreams. It uses Series() to probe if downstreams contain a series for the
// corresponding time window.
func (m *Metadata) virtualLabelValues(ctx context.Context, label string, plan metadataPlan, start, end time.Time) (model.LabelValues, promv1.Warnings, error) {
	call := func(ds Downstream, matches []string) ([]string, promv1.Warnings, error) {
		value, ok := ds.labels[label]
		if !ok || value == "" {
			return nil, nil, nil
		}

		series, warns, err := ds.client.Series(ctx, matches, start, end, withLimit(1)...)
		if err != nil {
			return nil, nil, err
		}
		if len(series) == 0 {
			return nil, warns, nil
		}
		return []string{value}, warns, nil
	}

	duplicated, warnings, err := fanOut(ctx, m, plan, "virtual-label/values", call)
	if err != nil {
		return nil, nil, err
	}

	var (
		result = model.LabelValues{}
		seen   = make(map[string]struct{})
	)

	for _, value := range duplicated {
		if _, ok := seen[value]; !ok {
			seen[value] = struct{}{}
			result = append(result, model.LabelValue(value))
		}
	}

	sort.Sort(result)
	return result, warnings, nil
}

func matchingDownstreams(downstreams map[string]Downstream, keys []string, m *promlabels.Matcher) []string {
	var result []string
	for _, key := range keys {
		if m.Matches(downstreams[key].labels[m.Name]) {
			result = append(result, key)
		}
	}
	return result
}

func matchersToString(matchers []*promlabels.Matcher) string {
	var parts []string
	for _, m := range matchers {
		parts = append(parts, m.String())
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func withLimit(limit int) []promv1.Option {
	if limit <= 0 {
		return nil
	}
	return []promv1.Option{promv1.WithLimit(uint64(limit))}
}

func appendWarnings(warnings promv1.Warnings, url string, downstream promv1.Warnings) promv1.Warnings {
	for _, w := range downstream {
		warnings = append(warnings, fmt.Sprintf("%s: %s", url, w))
	}
	return warnings
}

func truncate[T any](values []T, warnings promv1.Warnings, limit int) ([]T, promv1.Warnings) {
	if limit > 0 && len(values) > limit {
		warnings = append(warnings, "results truncated due to limit")
		return values[:limit], warnings
	}
	return values, warnings
}

func validateNonEmptyMatchers(matchers []string) error {
	parser := promparser.NewParser(promparser.Options{})
	for _, m := range matchers {
		parsed, err := parser.ParseMetricSelector(m)
		if err != nil {
			return fmt.Errorf("parsing matcher %q: %w", m, err)
		}

		if !promql.HasNonEmptyMatcher(parsed) {
			return fmt.Errorf("match[] must contain at least one non-empty matcher")
		}
	}

	return nil
}

func withVirtualLabelSet(m model.LabelSet, labelSet labels.LabelSet) model.LabelSet {
	injected := make(model.LabelSet, len(m)+len(labelSet))
	for k, v := range m {
		injected[k] = v
	}
	for k, v := range labelSet {
		if v != "" {
			injected[model.LabelName(k)] = model.LabelValue(v)
		}
	}
	return injected
}
