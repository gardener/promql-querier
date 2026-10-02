// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package fanout

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFetchedBytesReadCloserEnforcesLimit(t *testing.T) {
	data := strings.Repeat("x", 200)
	limit := NewFetchedBytesLimit(100)
	readCloser := &fetchedBytesReadCloser{
		readCloser: io.NopCloser(strings.NewReader(data)),
		limit:      limit,
	}

	buf := make([]byte, 64)
	n, err := readCloser.Read(buf)
	require.NoError(t, err)
	require.Equal(t, 64, n)

	_, err = readCloser.Read(buf)
	require.Error(t, err)
	require.EqualError(t, err, "fetched bytes limit exceeded (100 bytes)")

	// Even after the limit is exceeded, the ReadCloser should continue to return the same error.
	_, err = readCloser.Read(buf)
	require.Error(t, err)
	require.EqualError(t, err, "fetched bytes limit exceeded (100 bytes)")
}

func TestFetchedBytesReadCloserPassesThroughEOF(t *testing.T) {
	data := []byte("short")
	limit := NewFetchedBytesLimit(1000)
	readCloser := &fetchedBytesReadCloser{
		readCloser: io.NopCloser(bytes.NewReader(data)),
		limit:      limit,
	}

	out, err := io.ReadAll(readCloser)
	require.NoError(t, err)
	require.Equal(t, data, out)
}
