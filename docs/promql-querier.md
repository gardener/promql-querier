# How PromQL Querier works

The PromQL Querier sits in front of multiple Prometheus instances and runs distributed PromQL queries.
It determines whether expressions can be pushed down to downstream Prometheus instances or must be evaluated locally, fans the work out, and recombines the results into a single answer as if it was a single Prometheus instance holding the data.

## Virtual labels

A virtual label is a label that does not exist in any downstream TSDB, but queries are answered as if it were part of the stored data.
Virtual labels are the mechanism the PromQL Querier uses to identify each downstream.

For example, with two downstreams labeled `env="prod"` and `env="staging"` respectively, a query for `up` returns series carrying `env="prod"` and `env="staging"` even though neither Prometheus stores an `env` label on its series.
The PromQL Querier injects those labels into the results after fetching from each downstream.

## Push-down eligibility

The PromQL Querier walks the AST to identify expressions that can be pushed down to downstreams.
The moment a node cannot be pushed down, it becomes a push-down boundary and its children are pushed down instead.
Push-down eligibility is mostly determined by whether the expression preserves virtual labels in its output.
If it does not, then the expression crosses downstreams and must be evaluated locally.
If it does, it might be pushed down depending on the expression semantics.

### Vector and matrix selectors

Selectors can always be pushed down.
They are the leaves of the expression tree and carry all labels:

```promql
up                   -- can push down
up[5m]               -- can push down
up{env="prod"}       -- can push down (also routes to a single downstream)
```

### Functions

Most functions preserve virtual labels because they operate per series without dropping labels, so they can be pushed down:

```promql
abs(up)                                          -- preserves
changes(up[5m])                                  -- preserves
rate(up[5m])                                     -- preserves
...
label_replace(up, "dst", "$1", "src", "(.*)")    -- preserves
label_replace(up, "env", "$1", "src", "(.*)")    -- cannot push down (uses virtual label as destination)
label_replace(up, "dst", "$1", "env", "(.*)")    -- cannot push down (uses virtual label as source)
label_join(up, "dst", ",", "src1", "src2", ...)  -- preserves
label_join(up, "env", ",", "src1", "src2", ...)  -- cannot push down (uses virtual label as destination)
label_join(up, "dst", ",", "env", "src2", ...)   -- cannot push down (uses virtual label as source)
sort(up)                                         -- cannot push down (needs series locally for sorting)
```

#### `absent` and `absent_over_time`

`absent` and `absent_over_time` produce a single output series, so they do not preserve virtual labels and must be evaluated locally.
Their argument, however, can still be pushed down: to decide absence, the local engine only needs to know whether the argument produced any series in each downstream, not the series themselves.

The PromQL Querier rewrites the argument into a `group` aggregation by the virtual labels, which downstreams evaluate and which collapses each downstream's result to one series:

```promql
-- Original query:
absent(http_requests_total)

-- Rewritten to:
absent(group by (env) (http_requests_total))
--     ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
--     inner: sent to each downstream
```

`absent_over_time` wraps its ranged argument in `present_over_time` first, so the same existence check works over a range:

```promql
-- Original query:
absent_over_time(http_requests_total[5m])

-- Rewritten to:
absent(group by (env) (present_over_time(http_requests_total[5m])))
--     ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
--     inner: sent to each downstream
```

When `absent` matches an argument with equality matchers, those matchers must appear in the output series.
The PromQL Querier re-injects them with `label_replace` around the rewritten expression since the rewrite above replaced the original vector selector argument with a `group` expression.

```promql
-- Original query:
absent(http_requests_total{job="api"})

-- Rewritten to:
label_replace(absent(group by (env) (http_requests_total{job="api"})), "job", "api", "", "")
```

### Aggregations

An aggregation that drops virtual labels from its output crosses downstreams.
It combines data from multiple downstreams and must be evaluated locally:

```promql
sum by (env) (up)            -- can push down (env is the virtual label)
sum by (env, job) (up)       -- can push down
sum by (job) (up)            -- crosses downstreams (drops env)
sum(up)                      -- crosses downstreams
sum without (pod) (up)       -- can push down (env is not dropped)
sum without (env) (up)       -- crosses downstreams
```

#### Aggregation rewriting

When a decomposable aggregation crosses downstreams, the PromQL Querier rewrites it for decomposition.
The inner level groups by the virtual labels so it can be pushed down, and the outer level performs the final reduction locally:

```promql
-- Original query:
sum by (job) (requests)

-- Rewritten to:
sum by (job) (sum by (job, env) (requests))
--            ^^^^^^^^^^^^^^^^^^^^^^^^^^^^
--            inner: sent to each downstream
```

`count` is rewritten as sum of counts:

```promql
-- Original query:
count by (job) (requests)

-- Rewritten to:
sum by (job) (count by (job, env) (requests))
--            ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
--            inner: sent to each downstream
```

`avg` is rewritten as sum divided by count:

```promql
-- Original query:
avg by (job) (x)

-- Rewritten to:
sum by (job) (sum by (job, env) (x)) / sum by (job) (count by (job, env) (x))
--            ^^^^^^^^^^^^^^^^^^^^^                  ^^^^^^^^^^^^^^^^^^^^^^^
--            inner: sent to each downstream        inner: sent to each downstream
```

This not only delegates work to downstreams, but also reduces the amount of data sent to the local engine.

Non-decomposable aggregations are not rewritten.

### Binary expressions

#### Arithmetic and comparison

An arithmetic expression crosses downstreams when the vector matching clause excludes virtual labels from the matching labels.
Such operations require local evaluation because series from different downstreams cannot be matched on the downstream side:

```promql
up + rate(errors[5m])                   -- can push down (default matching includes all labels)
up * on(env, job) group_left() info     -- can push down (on includes virtual label)
up + ignoring(pod) errors               -- can push down (ignoring does not exclude virtual label)
up + on(job) group_left() info          -- crosses downstreams (on excludes virtual label from matching)
```

When one side is a constant, push-down depends on whether virtual labels survive through the operation:

```promql
up + 1                                  -- can push down
up + on() vector(1)                     -- cannot push down (on() drops virtual labels)
up * on() group_left() vector(2)        -- can push down (group_left keeps left-hand-side labels)
```

#### Set operations

The set operators `and`, `or`, and `unless` preserve virtual labels the same way arithmetic does, so the matching clause is checked the same way.
However, `or` and `unless` have an additional requirement for push-down eligibility: both sides must target the same downstreams.
The PromQL Querier calculates routing of binary expressions by intersecting the downstreams targeted by both sides, but `or` and `unless` behave differently since they might keep series: `or` from either side, `unless` from the left side.

`or` can push down only when both sides target the same downstreams.
A constant on either side prevents push-down, because each downstream would return the constant and duplicate it.

```promql
up{env="prod"} or errors{env="prod"}     -- can push down (same downstreams)
up{env="prod"} or errors{env="staging"}  -- crosses downstreams (different downstreams)
up{env="prod"} or vector(1)              -- crosses downstreams (constant duplicated across downstreams)
vector(1) or up{env="prod"}              -- crosses downstreams (constant duplicated across downstreams)
```

`unless` can push down when both sides target the same downstreams, or when the right side is a constant.
A constant on the right side only filters the left side, so it never becomes part of the result and is safe to push down.
A constant on the left side prevents push-down, because it would be evaluated once per downstream and returned for each.

```promql
up{env="prod"} unless errors{env="prod"}     -- can push down (same downstreams)
up{env="prod"} unless errors{env="staging"}  -- crosses downstreams (different downstreams)
up{env="prod"} unless vector(1)              -- can push down (constant on the right only filters)
vector(1) unless up{env="prod"}              -- crosses downstreams (constant on the left evaluated per downstream)
```

`and` needs no downstream check.
Its result is already the intersection of both sides, so intersecting their downstreams during routing loses nothing.
Constants follow the same rule as `unless`: a constant on the right side only filters and is safe, while a constant on the left side prevents push-down.

```promql
up and on(env) info          -- can push down (and matches the routing intersection)
up and on(job) info          -- crosses downstreams (on excludes virtual label)
up and vector(1)             -- can push down (right side only filters, up is returned)
vector(1) and up             -- crosses downstreams (constant on the left)
```

### Subqueries

A subquery can be pushed down if its inner expression can be pushed down:

```promql
avg_over_time(up[5m:1m])                     -- can push down
avg_over_time(sum by (job) (up)[5m:1m])      -- cannot push down (inner crosses downstreams); up[5m:1m] is pushed down instead
```

## Planning and evaluation

After rewriting, the planner produces two things:

1. A **local expression** where each pushed-down subtree is replaced by a synthetic placeholder selector `{__partition_idx__="N"}`, where `N` is the index of the partition that produces its data.
2. A list of **partitions**, each with a PromQL expression and the set of downstreams it targets.

The combination of these two is called a **plan**.

The planner strips virtual label references from the partition expression before sending it to the downstream, because the downstream does not know about virtual labels.

Each downstream responds with its local results.
The PromQL Querier injects the virtual label values into the returned series and loads them into an in-memory series set indexed by `__partition_idx__`.

The local expression is then evaluated by a standard Prometheus PromQL engine.
When the engine encounters a `{__partition_idx__="N"}` vector selector, it reads from the in-memory series set for partition `N`.
The engine handles all remaining operations: the outer aggregations, local binary expressions, subqueries, and so on, as if it were querying a normal TSDB.

### Example

Configuration: two downstreams, `env="prod"` and `env="staging"`.

```promql
-- Original query:
avg by (job) (rate(http_requests_total[5m]))

-- avg decomposes into sum divided by count, giving two partitions
-- whose inner aggregations keep env so they can be pushed down:

-- Partition 0:
sum by (job, env) (rate(http_requests_total[5m]))
-- Partition 1:
count by (job, env) (rate(http_requests_total[5m]))

-- The same partitions with the virtual label stripped are sent to each downstream:
sum by (job) (rate(http_requests_total[5m]))
count by (job) (rate(http_requests_total[5m]))

-- Each downstream responds with its partial result.
-- The PromQL Querier injects env="prod" or env="staging" into each series,
-- and stores each partition's series under its index.

-- Local expression evaluated by the local engine:
sum by (job) ({__partition_idx__="0"}) / sum by (job) ({__partition_idx__="1"})
```

### Contradictory routes

When a binary expression matches no common downstream, for example `up{env="prod"} + up{env="staging"}`, the planner detects this at plan time and returns an empty series set without making any network call.

### Subquery evaluation range and step

When a push-down boundary sits inside a subquery:

- the subquery stays in the local expression so the local engine re-evaluates it at each step.
- the inner expression is pushed down, and the partition fetches enough data for the local engine to evaluate the subquery over its whole window.

```promql
-- Original query:
up[2h:5m]

-- Partition 0:
up[2h:5m]

-- Local expression evaluated by the local engine:
{__partition_idx__="0"}[2h:5m]
```

#### Nested subqueries

When the push-down boundary is between two subqueries, the pushed-down partition absorbs the outer subquery range so it fetches enough data for the local engine to evaluate the inner subquery at every step of the outer one:

```promql
-- Original query:
rate(count(ALERTS)[2h:5m])[1d:]

-- Partition 0 (inner 2h range widened by the outer 1d range = 1d2h, 5m step kept):
count by(env) (ALERTS)[1d2h:5m]

-- Local expression evaluated by the local engine:
rate(sum({__partition_idx__="0"})[2h:5m])[1d:]
```

#### Rewiring range partitions to instant queries

For a range query over `[start, end]`, a subquery push-down cannot go to `/api/v1/query_range` because downstreams do not accept matrix selectors or subqueries at the query root on that endpoint.
The PromQL Querier rewires it as an instant query against `/api/v1/query` at `end`, and widens the subquery range to span the whole query window: `subquery range + (end - start)`.

```promql
-- Range query rate(count(ALERTS)[2h:5m]) over start..end (e.g., end - start = 1d):

-- The push-down count by (env) (ALERTS)[2h:5m] cannot run against /api/v1/query_range,
-- so it is rewired to an instant query against /api/v1/query,
-- with the range widened to 2h + (end - start) = 1d2h and the 5m step kept:
count by (env) (ALERTS)[1d2h:5m]

-- Local expression, re-evaluated at each step by the local engine as a range query over start..end:
rate(sum({__partition_idx__="0"})[2h:5m])
```

## Native histograms

Native histograms are not supported in the PromQL Querier yet.
