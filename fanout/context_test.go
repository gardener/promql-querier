// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package fanout

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequestUIDRoundTrip(t *testing.T) {
	uid := NewRequestUID()
	require.NotEmpty(t, uid)

	ctx := ContextWithRequestUID(context.Background(), uid)
	require.Equal(t, uid, requestUID(ctx))
}

func TestRequestUIDMissingReturnsEmpty(t *testing.T) {
	require.Equal(t, "", requestUID(context.Background()))
}
