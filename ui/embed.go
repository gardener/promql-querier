// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

// Package ui holds the embedded PromQL Querier web UI (a Prometheus-style query page).
package ui

import "embed"

// Files is the embedded filesystem holding the web UI assets under dist.
//
//go:embed dist
var Files embed.FS
