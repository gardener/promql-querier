// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package fanout

import (
	"context"

	"github.com/google/uuid"
)

type requestUIDKey struct{}

// NewRequestUID returns a fresh request identifier the server stamps onto each
// incoming request so engine log lines can be correlated to it.
func NewRequestUID() string {
	return uuid.NewString()
}

// ContextWithRequestUID stores uid in ctx so the engine can read it back with
// requestUID when it logs.
func ContextWithRequestUID(ctx context.Context, uid string) context.Context {
	return context.WithValue(ctx, requestUIDKey{}, uid)
}

func requestUID(ctx context.Context) string {
	uid, _ := ctx.Value(requestUIDKey{}).(string)
	return uid
}
