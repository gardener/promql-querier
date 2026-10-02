#!/usr/bin/env bash
# SPDX-FileCopyrightText: Contributors to the Gardener project
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cd "$(dirname "$0")/.."

PROMETHEUS_VERSION=$(go list -m -f '{{.Version}}' github.com/prometheus/prometheus | sed 's/^v0\.\([0-9]\)\([0-9]*\)\./\1.\2./')
PROMETHEUS_BINARY=tmp/bin/prometheus
mkdir -p tmp/bin
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')

if [ "${1:-}" = "--print-version" ]; then
    echo "$PROMETHEUS_VERSION"
    exit 0
fi

if [ ! -f "$PROMETHEUS_BINARY" ]; then
    echo "Downloading Prometheus v${PROMETHEUS_VERSION} for ${OS}/${ARCH}..."
    tmpdir=$(mktemp -d)
    trap 'rm -rf "$tmpdir"' EXIT

    base="https://github.com/prometheus/prometheus/releases/download/v${PROMETHEUS_VERSION}"
    archive="prometheus-${PROMETHEUS_VERSION}.${OS}-${ARCH}.tar.gz"

    curl --fail --show-error --location --silent "${base}/${archive}" -o "${tmpdir}/${archive}"
    curl --fail --show-error --location --silent "${base}/sha256sums.txt" -o "${tmpdir}/sha256sums.txt"

    (cd "$tmpdir" && grep " ${archive}\$" sha256sums.txt | shasum -a 256 -c -)

    tar -xz -C "$tmpdir" -f "${tmpdir}/${archive}"
    mv "$tmpdir/prometheus-${PROMETHEUS_VERSION}.${OS}-${ARCH}/prometheus" "$PROMETHEUS_BINARY"
    chmod +x "$PROMETHEUS_BINARY"
    echo "Downloaded $PROMETHEUS_BINARY"
fi
