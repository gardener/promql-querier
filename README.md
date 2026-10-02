# PromQL Querier

Global querying across independent Prometheus instances.

## What it is

The PromQL Querier sits in front of many independent Prometheus downstreams and queries them as if it was a single Prometheus instance.
Each downstream is configured with a set of virtual labels, for example `env="prod"` and `region="eu"`, that the downstream itself does not store.
The PromQL Querier owns those labels and injects them into the query results.

## How it works

The PromQL Querier parses the incoming PromQL into an AST and walks it, splitting the query into expressions that can be pushed down for evaluation in the downstreams directly and a local query that combines those pushed-down results and evaluates the remainder.
These expressions are the partitions, and the partitions plus the local query are the plan.

Each partition is routed by resolving its virtual label matchers against the declared downstreams, so a partition constrained to `region="eu"` only touches the `eu` downstreams.
The PromQL Querier decomposes aggregations in a way that allows them to be pushed down.
For example, an average is pushed down as a sum and a count and recombined as their ratio.

The PromQL Querier then executes the plan by fanning the partitions out to their downstreams over the Prometheus HTTP query API.
Finally, the Prometheus PromQL engine evaluates the local query over the partial results, and the answer is returned to the client.

See [docs/promql-querier.md](docs/promql-querier.md) for more details.

## Build

Build the UI and the binary:

```sh
make ui build
```

This produces `bin/promql-querier`.

The web UI is a bundle embedded into the binary.
If you change UI sources, rebuild the bundle before building the binary:

```sh
make ui
```

## Run

The PromQL Querier needs an endpoints file that lists each downstream and its virtual labels.
Each `url` is a plain Prometheus HTTP endpoint.
A minimal example:

```yaml
endpoints:
  - url: http://prometheus-eu:9090
    labels:
      env: prod
      region: eu
  - url: http://prometheus-us:9090
    labels:
      env: prod
      region: us
```

Start the PromQL Querier against it:

```sh
./bin/promql-querier --config.endpoints-file endpoints.yaml
```

The PromQL Querier serves the Prometheus HTTP query API on the listen address, defaulting to `:9090`.

Run `./bin/promql-querier --help` for the full list of flags and their defaults.

## Playground

To play around with the PromQL Querier without setting up your own Prometheus instances, build the UI and binary, download Prometheus once, and run:

```sh
make prometheus
make playground
```

`make prometheus` downloads a Prometheus binary into `tmp/bin/prometheus` and skips the download when it is already present.

This starts four Prometheus downstreams (`prod-eu`, `prod-us`, `staging-eu`, `staging-global`), and the PromQL Querier in front of them.
It also starts a reference Prometheus holding the union of all four downstreams, with the virtual labels baked into each series, to serve as ground truth.
It then prints the URLs and holds until you press Ctrl+C.

## Development

Run the unit tests:

```sh
make test
```

Run the compare test suite that diffs the PromQL Querier against a real Prometheus setup.
It needs the Prometheus binary that `make prometheus` downloads:

```sh
make prometheus
make test-compare
```

Vet and lint:

```sh
make check
```
