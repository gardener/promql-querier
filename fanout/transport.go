// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package fanout

import (
	"io"
	"net/http"
)

// FetchedBytesTransport wraps a downstream response body so the bytes read from
// it are charged against the per-query fetched-bytes limit.
type FetchedBytesTransport struct {
	Base http.RoundTripper
	URL  string
}

// RoundTrip executes the request through the base transport and wraps the
// response body so the bytes read from it are charged against the per-query
// fetched-bytes limit carried on the request context.
func (t *FetchedBytesTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.Base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if limit := fetchedBytesLimitFromContext(req.Context()); limit != nil {
		resp.Body = &fetchedBytesReadCloser{readCloser: resp.Body, limit: limit}
	}
	return resp, nil
}

type fetchedBytesReadCloser struct {
	readCloser io.ReadCloser
	limit      *Limit
	tripped    error
}

// Read charges the limit for the bytes read and, once the limit is exceeded,
// returns that error with no data.
func (r *fetchedBytesReadCloser) Read(p []byte) (int, error) {
	if r.tripped != nil {
		return 0, r.tripped
	}
	n, err := r.readCloser.Read(p)
	if n > 0 {
		if limitErr := r.limit.Add(int64(n)); limitErr != nil {
			r.tripped = limitErr
			return 0, limitErr
		}
	}
	return n, err
}

// Close closes the underlying response body.
func (r *fetchedBytesReadCloser) Close() error {
	return r.readCloser.Close()
}
