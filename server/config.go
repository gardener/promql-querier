// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"sort"

	"github.com/gardener/promql-querier/fanout"
	"github.com/gardener/promql-querier/labels"
)

type endpoint struct {
	URL    string          `json:"url"`
	Labels labels.LabelSet `json:"labels"`
}

// DefaultQueryMaxConcurrency is the number of concurrent query and query_range
// evaluations allowed by default.
const DefaultQueryMaxConcurrency = 20

// Config holds a server configuration.
type Config struct {
	Endpoints           []endpoint `json:"endpoints"`
	queryMaxConcurrency int
}

// ConfigOption configures a Config.
type ConfigOption func(*Config)

// WithQueryMaxConcurrency caps how many query and query_range evaluations may run at
// once. Requests over the cap are rejected with 429 rather than queued, so the
// proxy sheds load visibly.
func WithQueryMaxConcurrency(queryMaxConcurrency int) ConfigOption {
	return func(c *Config) { c.queryMaxConcurrency = queryMaxConcurrency }
}

// NewConfig builds a Config from the downstream map and applies the supplied options.
func NewConfig(downstreams map[string]fanout.Downstream, opts ...ConfigOption) *Config {
	endpoints := make([]endpoint, 0, len(downstreams))
	for url, ds := range downstreams {
		endpoints = append(endpoints, endpoint{URL: url, Labels: ds.LabelSet()})
	}

	sort.Slice(endpoints, func(i, j int) bool {
		return endpoints[i].URL < endpoints[j].URL
	})

	config := &Config{Endpoints: endpoints, queryMaxConcurrency: DefaultQueryMaxConcurrency}
	for _, opt := range opts {
		opt(config)
	}

	return config
}
