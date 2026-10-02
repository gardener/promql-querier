// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package fanout

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLimitAdd(t *testing.T) {
	// It allows adding up to the limit, but returns an error if the limit is exceeded.
	l := NewFetchedBytesLimit(100)
	require.NoError(t, l.Add(50))
	require.NoError(t, l.Add(50))
	require.Error(t, l.Add(1))

	// It fails immediately if the addition exceeds the limit.
	l = NewFetchedBytesLimit(100)
	require.NoError(t, l.Add(50))
	require.Error(t, l.Add(51))
}

/** FetchedBytesLimit **/

func TestFetchedBytesLimitAddRaisesFetchedBytesLimitError(t *testing.T) {
	l := NewFetchedBytesLimit(1)
	err := l.Add(2)
	require.Error(t, err)
	var limitErr *FetchedBytesLimitError
	require.True(t, errors.As(err, &limitErr), "want *FetchedBytesLimitError, got %T", err)
	require.EqualError(t, err, "fetched bytes limit exceeded (1 bytes)")
}

func TestFetchedBytesLimitFromContextReturnsNilWhenAbsent(t *testing.T) {
	require.Nil(t, fetchedBytesLimitFromContext(context.Background()))
}

func TestFetchedBytesLimitContextRoundTrip(t *testing.T) {
	l := NewFetchedBytesLimit(42)
	ctx := contextWithFetchedBytesLimit(context.Background(), l)
	require.Same(t, l, fetchedBytesLimitFromContext(ctx))
}

/** FetchedSamplesLimit **/

func TestFetchedSamplesLimitAddRaisesFetchedSamplesLimitError(t *testing.T) {
	l := NewFetchedSamplesLimit(1)
	err := l.Add(2)
	require.Error(t, err)
	var limitErr *FetchedSamplesLimitError
	require.True(t, errors.As(err, &limitErr), "want *FetchedSamplesLimitError, got %T", err)
	require.EqualError(t, err, "fetched samples limit exceeded (1 samples)")
}

func TestFetchedSamplesLimitFromContextReturnsNilWhenAbsent(t *testing.T) {
	require.Nil(t, fetchedSamplesLimitFromContext(context.Background()))
}

func TestFetchedSamplesLimitContextRoundTrip(t *testing.T) {
	l := NewFetchedSamplesLimit(42)
	ctx := contextWithFetchedSamplesLimit(context.Background(), l)
	require.Same(t, l, fetchedSamplesLimitFromContext(ctx))
}

/** FetchLimiter **/

func TestFetchLimiter(t *testing.T) {
	const (
		maxInFlight = 3
		total       = 12
		hold        = 20 * time.Millisecond
	)
	limiter := NewFetchLimiter(maxInFlight)

	var (
		inFlight atomic.Int32
		peak     atomic.Int32
		wg       sync.WaitGroup
	)

	for range total {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := limiter.Acquire(context.Background())
			require.NoError(t, err)
			defer release()

			cur := inFlight.Add(1)
			for {
				old := peak.Load()
				if cur <= old || peak.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(hold)
			inFlight.Add(-1)
		}()
	}
	wg.Wait()

	require.LessOrEqual(t, int(peak.Load()), maxInFlight, "in-flight fetches exceeded the cap")
	require.Positive(t, peak.Load(), "expected some concurrency")
}

func TestFetchLimitWithCancelledContext(t *testing.T) {
	limiter := NewFetchLimiter(1)

	// Take the only slot.
	release, err := limiter.Acquire(context.Background())
	require.NoError(t, err)
	defer release()

	// A second acquire on a cancelled context returns the context error instead of
	// blocking forever, so a cancelled query unblocks its waiting fetch goroutines.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = limiter.Acquire(ctx)
	require.ErrorIs(t, err, context.Canceled)
}
