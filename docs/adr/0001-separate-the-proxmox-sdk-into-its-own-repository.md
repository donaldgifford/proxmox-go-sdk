---
id: ADR-0001
title: "Separate the Proxmox SDK into its own repository"
status: Accepted
author: Donald Gifford
created: 2026-06-22
---

<!-- markdownlint-disable-file MD025 MD041 -->

# 0001. Separate the Proxmox SDK into its own repository

<!--toc:start-->

- [0001. Separate the Proxmox SDK into its own repository](#0001-separate-the-proxmox-sdk-into-its-own-repository)
  - [Status](#status)
  - [Context](#context)
  - [Decision](#decision)
    - [SDK shape (modeled on AWS SDK v2 + bpg + pvetui)](#sdk-shape-modeled-on-aws-sdk-v2--bpg--pvetui)
    - [The SDK / consumer boundary (held deliberately)](#the-sdk--consumer-boundary-held-deliberately)
    - [naos and any future provider](#naos-and-any-future-provider)
  - [Consequences](#consequences)
    - [Positive](#positive)
    - [Negative](#negative)
    - [Neutral](#neutral)
  - [Alternatives Considered](#alternatives-considered)
  - [References](#references)
  <!--toc:end-->

## Status

Accepted

## Context

We are building a Go management plane for Proxmox VE, in the same spirit as the
Python project PegaProx (a Flask + gevent monolith: ~102k LOC, 815 routes, one
765 KB per-cluster "manager" god-object, a self-transpiling React frontend).
PegaProx mixes the hypervisor client, multi-cluster orchestration, auth, the
realtime/console plumbing, and the HTTP surface into a single process. Our port
deliberately narrows scope to **Proxmox first**, with the explicit expectation
that a second provider (our **naos VMM**) may be added later. We also fix the
platform floor up front: **we target Proxmox VE 9.x exclusively** and will not
run against 8.x or earlier.

Two facts shape the architecture:

1. **The Proxmox API has no official OpenAPI/Swagger spec and is not
   versioned.** It is described by JSON Schema embedded in Perl (the source
   behind the API viewer's `apidoc.js`), which is auto-generated, "not 100%
   JSONSchema compatible," and prone to silent behavior changes across minor PVE
   releases. Keeping a client in sync with Proxmox is therefore an ongoing,
   version-pinned effort that benefits from its own release cadence and test
   suite.
2. **The mature Go Proxmox clients all hand-write their clients** (no codegen
   from a spec): `bpg/terraform-provider-proxmox` (the gold-standard layered
   client, but trapped inside a TF provider), `Telmate/proxmox-api-go` (used by
   the official Packer plugin; rich VM config types but half-`map[string]any`),
   and `devnullvoid/pvetui` (a TUI whose reusable client lives in an exported
   `pkg/api`). None is published as a clean, standalone, reusable Proxmox SDK.

We explored whether to keep everything in one service (monolith-first) or to
factor a library out. The deciding lens was the **AWS Go SDK** model: the AWS
SDK is provider-_specific_ (`service/s3` is S3, not "blob storage"), with
primitive types and request machinery at its core and typed per-service clients
on top; cloud-_neutral_ abstractions (Terraform, Crossplane, libcloud) are built
by _consumers_ on top of the SDK, never inside it. `pvetui` independently
validates this shape in the Proxmox world: an exported `pkg/api` client (core
`client.go`/`http.go`/`auth.go`/`options.go` + per-domain `vm*.go`, `node*.go`,
`cluster*.go`, `storage*.go`, a mockable `interfaces/` package, `testutils/`,
and a `mockpve` server), consumed by a thin `internal/` app — and a separate
`pve-openapi-gen` tool that emits an OpenAPI spec from `apidoc.js` as a
reference artifact while the client itself stays hand-written.

This supersedes the multi-provider framing in the provider/API design doc: the
abstraction question is resolved by making the SDK provider-specific rather than
neutral.

## Decision

Split the system into **two repositories minimum**:

1. **A standalone, provider-specific Proxmox SDK** (its own Go module, its own
   versioning, fully tested, mockable). It is unapologetically Proxmox: it
   speaks `vmid`, `node`, `qemu`/`lxc`, UPID, ticket/CSRF natively and does not
   pretend to be hypervisor-neutral.
2. **The VM management service** (the PegaProx-equivalent), in its own repo, as
   the **first consumer** of the SDK.

### SDK shape (modeled on AWS SDK v2 + bpg + pvetui)

- **Primitive types at the core**, plus a low-level transport: one
  `DoRequest(ctx, method, path, req, resp)` + `ExpandPath` path templating, a
  `Connection` (endpoint, TLS incl. self-signed/IP handling, min-TLS), retry,
  and a `Credentials`/`Authenticator` split with precedence **auth ticket > API
  token > username/password** (with 2 h ticket refresh and CSRF injection on
  writes).
- **Typed per-domain services** hanging off the core: `nodes`, `qemu`, `lxc`,
  `storage`, `cluster`, `access`, `tasks` — each a typed client with typed
  request/response structs.
- **Waiters in the SDK** — `WaitForTask`/`WaitForStatus` over PVE's UPID tasks
  (every consumer needs them; AWS-style poll-until-state).
- **Functional options + consumer-injected cross-cutting** — `WithLogger`,
  `WithCache`, `WithHTTPClient`, `WithTLS`, where `Logger`/`Cache` are
  interfaces _supplied by the consumer_. The SDK is opinionated about its own
  API but prescribes neither logging, caching, transport, **nor an API/gRPC
  layer**.
- **Version floor: Proxmox VE 9.x only** (decided in ADR-0002). For the SDK this
  means a `version` service with `MinimumProxmoxVersion = 9.0` and `Support*()`
  helpers that gate only **drift across 9.x minors** (the API is unversioned
  within a major) — no 8.x compatibility shims, one HA model, DEB822 sources.
- **Mockability is a first-class deliverable** — ship interfaces plus a mock PVE
  server (à la `mockpve`) so the service integration-tests without a live
  cluster.
- **SSH side-channel** for the operations the REST API genuinely cannot do
  (snippet/backup upload via SFTP, hardware mappings needing root PAM).
- **Codegen is reference-only** — a small `apidoc.js → OpenAPI/types/diff` tool
  for docs and CI version-drift detection; the client stays hand-written.

### The SDK / consumer boundary (held deliberately)

The dividing question is **not** "is it orchestration?" — it is **"does Proxmox
expose this server-side?"** If the PVE API offers a capability, the SDK wraps
it, however high-level it is.

- **SDK** = primitives + per-domain operations + waiters + read-only
  cross-endpoint aggregation, **plus every PVE-native capability the API
  exposes** — including `ha-manager`, the Cluster Resource Scheduler,
  storage/ZFS replication, and affinity rules (all standard on our 9.x target).
  These are API calls like any other.
- **Service** = only the orchestration Proxmox has **no API for**:
  load-balancing and migration _across independent clusters_ (the cross-cluster,
  ProxLB-style logic), any placement/policy we **compute ourselves** rather than
  delegate to PVE, plus persistence, auth/RBAC, transport (HTTP/SSE/WS and any
  internal gRPC), the realtime fan-out hub, and the plugin/extension surface.
- The AWS parallel, stated correctly, supports this: autoscaling lives in the
  AWS SDK _because_ it is a server-side AWS service. The test is platform
  exposure, not how high-level the feature feels — so single-cluster HA/CRS is
  SDK, while cross-cluster balancing (which no single PVE endpoint provides) is
  the consumer.

### naos and any future provider

- naos gets its **own SDK** when it is real — idiomatic to naos, not contorted
  to resemble Proxmox (the way AWS and GCP each ship separate SDKs).
- A provider-_neutral_ interface, **if it is ever warranted**, is defined in the
  **consumer** (or a thin third library layered over both SDKs), and only once a
  second concrete implementation exists to shape it — validated by a provider
  conformance test suite. It never lives inside either SDK.

## Consequences

### Positive

- **Versioning/sync to Proxmox is decoupled.** The SDK tracks PVE on its own
  cadence with its own tests; the service pins a known-good SDK tag.
- **Clean, testable boundary.** The dependency arrow points one way (service →
  SDK); the SDK cannot absorb service concerns.
- **Multiple consumers become cheap** — the service, a CLI, scripts, and tests
  all build on the same primitives, exactly like building on the AWS SDK.
- **No premature/leaky abstraction.** A Proxmox-specific SDK has nothing to
  "generalize" wrongly; the risky neutral layer is deferred until naos exists.
- **An ecosystem gap is filled** — a standalone, idiomatic Go Proxmox SDK does
  not currently exist in clean form.

### Negative

- **Two repos to coordinate.** Early co-evolution means cross-repo churn; a
  breaking SDK change is a tag bump plus a service update rather than one
  commit.
- **We own the SDK's maintenance** against an unversioned, drifting upstream
  API.
- **Some duplication of plumbing** (CI, lint, release) across two repos.

### Neutral

- The SDK/consumer line has genuine judgment calls (e.g. read aggregation and
  where waiters live); `pvetui` made slightly different choices (polling in the
  consumer, grouped-cluster reads in the SDK) — both are defensible and we may
  revisit specific calls.
- Everything above the SDK (auth, store, realtime, console, scheduler, plugins)
  is unchanged by this decision; it sits in the consumer regardless.

## Alternatives Considered

- **Single monolith repo (monolith-first).** Simplest early velocity, but
  entangles Proxmox-sync with the service lifecycle and reproduces PegaProx's
  coupling; rejected given the unversioned upstream and the ≥2-consumer
  expectation.
- **One repo with an exported `pkg/api` (the `pvetui` approach).** Lighter than
  two repos and gives atomic cross-changes, but no independent SDK versioning or
  clean external-consumer story; rejected in favor of two repos, while borrowing
  `pvetui`'s package layout wholesale. (Mitigate early churn with a local
  `replace` directive and frequent SDK tags.)
- **A provider-neutral "homelab abstraction lib" up front.** Designs an
  abstraction from a single implementation — the classic leaky-abstraction trap;
  deferred to the consumer until naos provides a second data point.
- **Codegen the whole client from `apidoc.js`.** The source is lossy and
  unversioned; owning a generator for 800+ endpoints is a liability. Adopted
  only for types/docs/version-diff, not as the client's source of truth.
- **Depend on an existing client** (`bpg`, `Telmate`,
  `luthermonson/go-proxmox`). `bpg` is coupled to a TF provider and not
  published as a standalone client; `Telmate` is loosely maintained and
  half-`map[string]any`. We need console proxying, task streaming, and our own
  domain mapping — none target a live management plane — so we own the SDK and
  borrow type definitions where useful.

## References

- ADR-0002 — Target Proxmox VE 9.x only (the platform floor this SDK's `version`
  service enforces)
- IMPL-0001 — Proxmox VE 9.x SDK coverage (per-capability tracking ledger)
- `PEGAPROX-GO-OVERVIEW.md` — architecture overview and Python→Go package map
- `PEGAPROX-GO-COMPARISON.md` — Go-vs-Python rewrite analysis (gRPC scope, etc.)
- `PEGAPROX-GO-PROVIDER-AND-API.md` — Proxmox API strategy + provider seam (this
  ADR supersedes its neutral-abstraction framing)
- `PEGAPROX-GO-PLUGINS.md` — plugin inventory and tiered extension model
- `devnullvoid/pvetui` — `pkg/api` reusable Proxmox client + `pve-openapi-gen` +
  `mockpve`
- `bpg/terraform-provider-proxmox` — layered Proxmox client (template for our
  SDK)
- `Telmate/proxmox-api-go` + `hashicorp/packer-plugin-proxmox` — VM config type
  prior art
- AWS SDK for Go v2 — core/services split, functional options, waiters, mockable
  interfaces
- Proxmox VE API wiki + `apidoc.js` — the (unversioned, JSON-Schema-derived) API
  surface
