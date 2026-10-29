# Bramble Design Instructions

This document describes the design and architecture of Bramble, a production-ready GraphQL federation gateway written in Go. Use it as context when working on this repository.

## Overview

Bramble sits in front of multiple upstream GraphQL services ("federated services"), merges their schemas into one unified schema, and routes/splits incoming queries across those services at runtime. It is deliberately **stateless** — all state is the merged schema held in memory, rebuilt by polling services — so it scales horizontally.

```
Client ──▶ Gateway (gqlgen handler) ──▶ ExecutableSchema
                                            │  1. Plan (plan.go)
                                            │  2. Execute (execution.go)
                                            │  3. Merge results (execution_result.go)
                                            ▼
                              Upstream GraphQL services (HTTP)
```

## Key components

| File | Role |
|---|---|
| `gateway.go` | HTTP server setup. Wraps an `ExecutableSchema` in a gqlgen handler on `/query`, plus a private router for plugins. `UpdateSchemas` polls services on an interval. |
| `executable_schema.go` | The core orchestrator. Implements gqlgen's `graphql.ExecutableSchema`. Holds `MergedSchema`, `Locations` (field→service URL map), `IsBoundary`, `BoundaryQueries`, and `Services`. `UpdateSchema` fetches each service's schema (in parallel, capped at 64) and rebuilds the merged schema, supporting hot reload — a broken service is skipped rather than taking down the gateway. |
| `merge.go` | Schema merging (`MergeSchemas`) and construction of the routing maps (`buildFieldURLMap`, `buildIsBoundaryMap`, `buildBoundaryFieldsMap`). |
| `plan.go` | Query planning — converts an operation into a tree of `QueryPlanStep`s. |
| `execution.go` | Concurrent execution of the plan against upstream services. |
| `execution_result.go` | Stitches partial results together, merges boundary results by ID, bubbles up nulls, formats the final response. |
| `validate.go` | Validates each service schema follows Bramble's federation spec before merging. |
| `plugin.go` / `plugins/` | Plugin system (JWT auth, CORS, admin UI, headers, etc.) with hooks into the HTTP mux, gqlgen handler, and outgoing requests. |
| `auth.go` | Field-level permissions — a role/permission tree can filter which fields of the merged schema a caller may access. |
| `client.go` | GraphQL HTTP client used for all upstream requests. |
| `config.go` | Configuration loading with hot reload (`Watch`), including service list, poll interval, plugins, and the boundary ID field name. |

## Service discovery: the `service` root field

Instead of a registry, every federated service must expose:

```graphql
type Query { service: Service! }
type Service { name: String! version: String! schema: String! }
```

The gateway polls each configured service URL, queries `{ service { name version schema } }`, parses the returned SDL string, validates it, and re-merges. This is how hot-reloading of schemas works — no restart or registration step needed.

## The `@boundary` directive — federated (shared) types

`@boundary` marks an object type as **shared across services**. Each service owns a *disjoint subset of fields* on the type, keyed by a common `id: ID!` field:

```graphql
# service A                          # service B
type Movie @boundary {               type Movie @boundary {
  id: ID!                              id: ID!
  title: String!                       reviews: [Review!]!
}                                    }
```

**Merging rule** (`merge.go`, spec'd in `docs/federation.md`): boundary objects with the same name merge into one definition — union of fields, which **must not overlap** except `id`. The merged type keeps `@boundary`, all interfaces, and concatenated descriptions.

**Boundary queries**: any service defining a boundary type must expose a root query tagged `@boundary` to fetch instances by ID:

```graphql
movie(id: ID!): Movie @boundary
# or preferred array form:
movies(ids: [ID!]!): [Movie]! @boundary
```

`buildBoundaryFieldsMap` records these per service+type (`BoundaryFieldsMap`, in `plan.go`), preferring the array form since it lets the gateway batch lookups. This is how the gateway "joins" a boundary object across services: fetch part of it from service A, collect the `id`s, then call service B's boundary query with those IDs.

## The `@namespace` directive

`@namespace` types are pure **organizational containers that exist only on the gateway** — they never resolve on any service. Multiple services can declare the same namespace type; their fields are merged, and overlap of fields is allowed only if the overlapping field is itself a namespace type, non-null, and argument-less. Namespace type names must end in `Query`/`Mutation`/`Subscription` (so a namespace belongs to exactly one operation kind); the root `Query`/`Mutation`/`Subscription` types are implicit namespaces. In `buildFieldURLMap`, fields whose type is a namespace are *not* registered to any service — during planning, the planner descends through them until it hits real fields.

This yields Bramble's key simplifying invariant:

> Except for namespaces and boundary `id` fields, **every field in the merged schema is owned by exactly one service**.

So there is no resolver ambiguity, and no `@requires`/`@provides`/`@external` machinery like Apollo Federation.

## Schema merging (`merge.go`)

The merge operation is binary, associative, and commutative; schemas are folded pairwise:

- **Scalars**: standard scalars plus custom scalars from services (values forwarded as-is).
- **Directives**: only the standard `@skip`, `@include`, `@deprecated`, plus `@boundary`. Custom directives are not supported.
- **Interfaces, unions, input objects, enums, non-boundary objects**: copied verbatim; names must not overlap across services.
- **Boundary objects**: merged as described above.
- **Namespace objects**: merged with the overlap rules described above.
- Internal types (`Service`, the `service` root field, boundary queries) are pruned from the merged schema.

Alongside the merged schema, three routing maps are built:

- `FieldURLMap` (`"ParentType.field" → service URL`) — field ownership.
- `IsBoundary` (`type name → bool`) — which types are boundary types.
- `BoundaryFieldsMap` (`service URL → type → BoundaryField{Field, Argument, Array}`) — how to fetch boundary objects by ID from each service.

## Query planning (`plan.go`)

`Plan()` walks the operation's selection set against the routing maps:

1. **Routing**: `routeSelectionSet` looks up each field in `FieldURLMap` and buckets selections by service. Namespace fields are transparently traversed; `__typename`, `id` on boundary types, and gateway-internal fields (e.g. `service`) are handled by an internal "bramble" step.
2. **Step extraction**: for each service bucket it builds a `QueryPlanStep` containing:
   - `ServiceURL` / `ServiceName` — where to send it,
   - `ParentType` — `Query`/`Mutation` for root steps, or the boundary type name for child steps,
   - `SelectionSet` — the sub-query for that service (with `_bramble_id: id` and `_bramble__typename` injected as needed so the gateway can join/merge results),
   - `InsertionPoint` — the response path where its data must be spliced in,
   - `Then` — child steps that depend on this step's results.
3. Whenever a selection crosses into fields owned by *another* service on a boundary type, the planner cuts the selection there and emits a child step keyed on that boundary type.

The result is a tree: root steps per service, with nested boundary-fetch steps.

## Execution (`execution.go`)

`queryExecution.Execute` runs the plan concurrently with an `errgroup`:

- **Root steps** run in parallel; each sends its formatted document to its service.
- After a step returns, for each child step the gateway calls `extractAndDedupeBoundaryIDs` on the data at the child's `InsertionPoint`, then `executeChildStep`:
  - Looks up the service's `BoundaryField` for the parent type.
  - `buildBoundaryQueryDocuments` builds either one array query `{ _result: movies(ids: [...]) { ... } }` or a batched aliased query `{ _0: movie(id:"1"){...} _1: movie(id:"2"){...} }` (batch size 50).
  - Child steps recurse — a boundary result can itself trigger further boundary fetches.
- A `maxRequest` counter caps total upstream requests per query (`MaxRequestsPerQuery`).
- Each partial result is emitted on a channel as `executionResult{ServiceURL, InsertionPoint, Data, Errors}`.

## Result merging (`execution_result.go`)

Partial results are merged into a single response tree: results are sorted by insertion-point depth, boundary results are matched into the parent tree by `_bramble_id`, internal aliases (`_bramble_id`, `_bramble__typename`) are stripped/renamed, null values are bubbled up per the GraphQL spec (`bubbleUpNullValuesInPlace`), and the final map is formatted against the original selection set (respecting fragments, aliases, and `@skip`/`@include`).

## Deliberate restrictions

- No subscriptions.
- No shared unions, interfaces, scalars, enums, or inputs across services.
- No custom directives in federated services.
- No cross-service `extend` or implementing an interface defined in another service.
- The `schema` construct (renaming root types) is not supported.

These restrictions are what make the single-owner-per-field invariant hold, keeping planning/merging far simpler than Apollo Federation's spec — the tradeoff Bramble explicitly chose ("simple, reliable and scalable").

## Cross-cutting features

- **Plugins** (`plugins/`): admin UI with query playground, JWT auth, CORS, header forwarding, metrics/limits — hooked in via interfaces in `plugin.go` at mux, handler, and outgoing-request levels.
- **Field-level access control** (`auth.go`): an `OperationPermissions` tree (`query.movies.*` style) filters the schema/selections per role.
- **Observability**: OpenTelemetry tracing (`telemetry.go`, spans per plan/step) and Prometheus metrics (`metrics.go`), plus a debug middleware that can return the query plan and per-step timings in the response.
- **Configurable ID field**: the boundary key field name (`id` by default, `IdFieldName`) can be customised in config.

## Building and testing

- Build: `go build -o bramble ./cmd/bramble` (requires Go 1.23+)
- Test: `go test ./...`
- Run locally with examples: `docker-compose up` (gateway on `http://localhost:8082/query`)

## Further reading

- `docs/federation.md` — the full federation specification (syntax and merge/resolution semantics).
- `docs/algorithms.md` — pseudocode for the planning and execution algorithms.
- `docs/sharing-types.md`, `docs/schema.md`, `docs/configuration.md`, `docs/plugins.md` — user-facing documentation.
