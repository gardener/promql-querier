// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package fanout

import (
	"context"
	"fmt"
	"sync/atomic"

	"golang.org/x/sync/semaphore"
)

// Limit is an atomic accumulator with a maximum. A maximum of 0 means
// unlimited.
type Limit struct {
	max    int64
	used   atomic.Int64
	newErr func(maxValue int64) error
}

// Add increases the accumulated total by n and returns a non-nil error when the
// total exceeds the configured maximum.
func (l *Limit) Add(n int64) error {
	if l.max <= 0 {
		return nil
	}
	if l.used.Add(n) > l.max {
		return l.newErr(l.max)
	}
	return nil
}

/** FetchedBytesLimit **/

// NewFetchedBytesLimit returns a Limit that fails with a FetchedBytesLimitError
// once the accumulated total exceeds maxValue.
func NewFetchedBytesLimit(maxValue int64) *Limit {
	return &Limit{max: maxValue, newErr: func(maxValue int64) error {
		return &FetchedBytesLimitError{Max: maxValue}
	}}
}

// FetchedBytesLimitError is returned when a query fetches more uncompressed
// bytes from its downstreams than the per-query limit allows.
type FetchedBytesLimitError struct {
	Max int64
}

func (e *FetchedBytesLimitError) Error() string {
	return fmt.Sprintf("fetched bytes limit exceeded (%d bytes)", e.Max)
}

type fetchedBytesLimitKey struct{}

func contextWithFetchedBytesLimit(ctx context.Context, l *Limit) context.Context {
	return context.WithValue(ctx, fetchedBytesLimitKey{}, l)
}

func fetchedBytesLimitFromContext(ctx context.Context) *Limit {
	l, _ := ctx.Value(fetchedBytesLimitKey{}).(*Limit)
	return l
}

/** FetchedSamplesLimit **/

// NewFetchedSamplesLimit returns a Limit that fails with a
// FetchedSamplesLimitError once the accumulated total exceeds maxValue.
func NewFetchedSamplesLimit(maxValue int64) *Limit {
	return &Limit{max: maxValue, newErr: func(maxValue int64) error {
		return &FetchedSamplesLimitError{Max: maxValue}
	}}
}

// FetchedSamplesLimitError is returned when a query decodes more samples from
// its downstreams than the per-query limit allows.
type FetchedSamplesLimitError struct {
	Max int64
}

func (e *FetchedSamplesLimitError) Error() string {
	return fmt.Sprintf("fetched samples limit exceeded (%d samples)", e.Max)
}

type fetchedSamplesLimitKey struct{}

func contextWithFetchedSamplesLimit(ctx context.Context, l *Limit) context.Context {
	return context.WithValue(ctx, fetchedSamplesLimitKey{}, l)
}

func fetchedSamplesLimitFromContext(ctx context.Context) *Limit {
	l, _ := ctx.Value(fetchedSamplesLimitKey{}).(*Limit)
	return l
}

/** FetchConcurrency **/

// FetchLimiter bounds how many outbound downstream requests one query may
// have in flight at once. Only the leaf goroutines that make an outbound call
// acquire a token, so a coordinator goroutine never holds a token while waiting
// for the leaves it spawned to acquire one, which would deadlock.
type FetchLimiter struct {
	sem *semaphore.Weighted
}

// NewFetchLimiter returns a limiter capping in-flight fetches at maxFetches.
func NewFetchLimiter(maxFetches int) *FetchLimiter {
	var sem *semaphore.Weighted
	if maxFetches > 0 {
		sem = semaphore.NewWeighted(int64(maxFetches))
	}
	return &FetchLimiter{sem: sem}
}

// Acquire blocks until a slot is free or ctx is done. It returns a release
// function that the caller must invoke once the fetch completes.
func (c *FetchLimiter) Acquire(ctx context.Context) (func(), error) {
	if c.sem == nil {
		return func() {}, nil
	}
	if err := c.sem.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	return func() { c.sem.Release(1) }, nil
}

type concurrentFetchesKey struct{}

func contextWithConcurrentFetches(ctx context.Context, c *FetchLimiter) context.Context {
	return context.WithValue(ctx, concurrentFetchesKey{}, c)
}

func concurrentFetchesFromContext(ctx context.Context) *FetchLimiter {
	c, _ := ctx.Value(concurrentFetchesKey{}).(*FetchLimiter)
	return c
}
