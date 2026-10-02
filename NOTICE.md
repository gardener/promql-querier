# PromQL Querier

Copyright Contributors to the Gardener project.

## Third-party components

The PromQL Querier is built using Prometheus (https://github.com/prometheus/prometheus), Copyright The Prometheus Authors, licensed under the Apache License, Version 2.0 (https://github.com/prometheus/prometheus/blob/main/LICENSE).

The PromQL Querier compiles further third-party Go modules into its binary and bundles third-party npm packages into its web UI.

- Web UI package licenses are served by the running server at `/assets/third-party-licenses.txt`.
- Go module licenses can be reproduced from the pinned module set in `go.mod` and `go.sum`.
