# proxmox-go-sdk — Project Review

**Date:** 2026-07-15 · **State reviewed:** `main` @ `12560f4` (post-IMPL-0002,
latest tag `v0.6.2`) · **Audience:** project owner / prospective consumers

<!--toc:start-->

- [Executive summary](#executive-summary)
- [What this is](#what-this-is)
- [The Go code](#the-go-code)
  - [Shape and numbers](#shape-and-numbers)
  - [The transport](#the-transport)
  - [The service-package pattern](#the-service-package-pattern)
  - [Type-system decisions](#type-system-decisions)
  - [Error taxonomy](#error-taxonomy)
  - [Library discipline](#library-discipline)
  - [Honest critiques](#honest-critiques)
- [What we are doing in Proxmox](#what-we-are-doing-in-proxmox)
  - [Coverage by domain](#coverage-by-domain)
  - [The honesty rule](#the-honesty-rule)
  - [Things learned from live PVE that no doc states](#things-learned-from-live-pve-that-no-doc-states)
- [The testing story](#the-testing-story)
- [Why this instead of the alternatives](#why-this-instead-of-the-alternatives)
  - [The field](#the-field)
  - [What actually differentiates this SDK](#what-actually-differentiates-this-sdk)
  - [The honest flip side](#the-honest-flip-side)
- [Risks and open items](#risks-and-open-items)
- [Bottom line](#bottom-line)
<!--toc:end-->

## Executive summary

`proxmox-go-sdk` is a typed Go SDK for Proxmox VE 9.x that is unusual in its
field for three reasons: **it ships its own test infrastructure** (an in-memory
PVE responder consumers can import, plus a version-certified go-vcr cassette
corpus replayed in CI), **it refuses to lie about the API** (ops PVE cannot
actually do return a documented `ErrUnsupported` instead of a fabricated
endpoint that would 404), and **it has been live-verified end-to-end on real
hardware** — including the hard-to-reach criteria (HA scheduler placement, raw
VNC/RFB byte streams) via `pvelab`, a harness where the SDK provisions the
nested PVE clusters it is then tested against.

The code is idiomatic, lint-clean under an aggressive golangci-lint config,
race-tested, and consistently patterned — every one of the 14 service packages
follows one template. The main caveats are youth (v0.6.x, API not yet frozen), a
single known consumer, and a handful of deliberately provisional endpoint shapes
that await confirmation on future PVE releases.

## What this is

A **Go library** (not a service) for driving Proxmox VE 9.x: one unified client
(`proxmox.NewClient`) fanning out to typed per-domain services — QEMU, LXC,
storage, HA, SDN, firewall, cluster, access/tokens, nodes admin, Ceph, PBS-side
backup, console, metrics — plus the supporting machinery a real integration
needs: task (UPID) waiters, per-minor capability gating, an error taxonomy,
streaming uploads, WebSocket console streams, and an SSH/SFTP side-channel for
the handful of operations PVE's REST API genuinely does not expose.

It targets **PVE 9.x only** (ADR-0002): a hard 9.0 floor enforced at client
construction, with named per-minor gates
(`caps.Require("LXC OCI templates", "9.1")`) instead of best-effort
compatibility across major versions. The first consumer is `pegaprox-go` (the
homelab VM service); the design intends general consumption via `go get` of a
pinned tag.

The design record is complete and checked in: ADR-0001/0002 (why a standalone
SDK; why 9.x-only), DESIGN-0001/0002 (public contract; dogfood harness),
IMPL-0001/0002 (capability ledgers — both **Completed**, every success criterion
carrying dated pass evidence), INV-0001/0002 (both Concluded).

## The Go code

### Shape and numbers

| Metric              | Value                                                                                                              |
| ------------------- | ------------------------------------------------------------------------------------------------------------------ |
| Packages            | 30 (`proxmox/` SDK + `cmd/` tools)                                                                                 |
| Non-test Go         | ~21.6k lines                                                                                                       |
| Test Go             | ~12.5k lines (≈0.58 test:code ratio)                                                                               |
| Coverage (core)     | `types` 94.7%, `pverr` 94.1%, `tasks` 90.9%, `api` 79.3%                                                           |
| Coverage (services) | 67–88% (qemu 82.6%, lxc 87.8%, storage 80.7%, cluster/console 84.6%)                                               |
| Committed cassettes | 11 (+ `certification.yaml` provenance, 3 PVE-version batches)                                                      |
| Lint                | golangci-lint (gosec, gocritic, errcheck `check-blank`, noctx, revive) + yamllint/actionlint/markdownlint/prettier |

The whole SDK lives under `proxmox/` so it lifts cleanly into its own module
when the repo eventually splits (DESIGN-0001); the repo root is a doc-only
package. `mockpve`'s low self-coverage (18.4%) is a measurement artifact — it is
exercised by every other package's test suite, not by its own.

### The transport

`proxmox/api` is a deliberately small surface with exactly four verbs:

- **`DoRequest`** — the JSON envelope path: auth (three credential strategies —
  API token, ticket/CSRF, and password re-ticketing), sticky cluster failover
  across endpoints, retry with backoff, and HTTP-status → error-taxonomy
  classification on every response.
- **`DoUpload`** — streaming multipart POST for ISO/disk images. The body is
  built through an `io.Pipe` so a multi-GB ISO is never buffered in memory, and
  it deliberately does **not** retry (an upload body is a single-use stream;
  silently retrying a half-sent stream would corrupt it).
- **`DoWebSocket`** — a native HTTP/1.1 101-upgrade (no third-party websocket
  dependency) returning a raw `io.ReadWriteCloser`, used by `console.Connect`
  for VNC/term streams.
- **`ExpandPath`** — the seam that keeps URL construction in one place.

Only `*transport` implements the `api.Client` interface, which is the
mockability seam for every service. Growing the interface (DoUpload in Phase 3,
DoWebSocket in Phase 6) was breaking-but-safe because no external doubles exist.

### The service-package pattern

Every domain package is the same shape, established by `qemu` and stamped 13
more times:

```go
type Service struct { c api.Client; node string; caps version.Capabilities }
func NewService(c api.Client, node string, caps version.Capabilities) *Service
type API interface { /* every op */ }
var _ API = (*Service)(nil)   // the consumer's test-double seam
```

Reads decode directly; task-returning writes read the UPID and return a
`tasks.Ref`. Action-style ops (stop, shutdown, suspend) use **per-op functional
option types** writing to an unexported config, so the PVE wire form
(`url.Values`) never leaks into a public signature — and an option that is
irrelevant to an op will not compile against it. Scope varies where the API
varies: `qemu`/`lxc` are node-bound, `storage`/`ha`/`sdn`/`cluster` are
cluster-scoped, `firewall` is one service with a scope value (cluster / node /
guest) instead of three near-identical packages.

Shared logic was extracted **only when a second consumer appeared** (the
`svcutil` internal package: spec encoding, UPID→Ref, sentinel errors); trivial
per-service scaffolding stays deliberately duplicated. That restraint — no
speculative framework — is why the 14th service package reads exactly like the
1st.

### Type-system decisions

- **Lossless reads.** Config-like reads (`qemu.Config`, `storage.Datastore`,
  `ha.HARule`, …) carry a custom `UnmarshalJSON` that routes unknown keys into
  an `Extra map[string]string`. A PVE point release adding a field can never
  silently drop data on the floor.
- **Escape-hatch writes.** Write specs model the common fields and carry an
  `Extra map[string]string` (`json:"-"`) merged into the form at encode time, so
  an unmodelled PVE parameter never blocks a consumer.
- **`types.PVEBool`** — PVE's `0`/`1` booleans as a public type consumers can
  embed, rather than an internal hack.
- **Pointer write specs** (`Create(ctx, *CreateSpec)`) — a documented deviation
  from DESIGN-0001's illustrative by-value signatures, per Uber's large-struct
  guidance.

### Error taxonomy

`proxmox/pverr` defines `*pverr.Error` plus sentinels (`ErrNotFound`,
`ErrForbidden`, `ErrTaskFailed`, `ErrUnsupported`, …); the transport classifies
every HTTP status through `pverr.Classify`, and every SDK error wraps with `%w`.
Consumers branch with `errors.Is`/`errors.As` — no string matching, no leaking
`*url.Error` internals. Task failures carry the task log tail, so
`ErrTaskFailed` is actionable, not just a boolean.

### Library discipline

The things that make a library safe to depend on are all present: no `init()`
behavior; no global logger (a consumer-supplied `slog` handler via `WithLogger`,
no-op default); `context.Context` first argument on every operation with no
uncancellable background work; one `*Client` safe for concurrent use; functional
options throughout; `internal/` reserved for genuinely private helpers rather
than used to wall off the SDK.

### Honest critiques

- `ssh` (56.9%) and `access` (67.0%) have the thinnest coverage — `ssh`'s live
  PAM-auth path is inherently hard to unit-test (it is exercised in-process
  against a loopback SSH+SFTP server, but the real-node path runs only in the
  dogfood lab).
- The lossless-read pattern requires keeping each `…KnownFields` set in sync
  with its struct by hand. It is convention-enforced, not compiler-enforced; a
  forgotten entry means a typed field also appears in `Extra`. A small
  `go:generate` or reflection-based test could close this.
- A few endpoint shapes are **provisional by declared policy**
  (REST-with-caveat: HA Dynamic Load Balancer at 9.2, SDN fabrics, DEB822 repo
  fields, SMART tables, ACME cert flows) — real endpoints, but their
  request/response shapes await confirmation against future PVE minors.
- `mockpve` emulates, it does not implement — cluster-join needs a wire-forced
  seam (`QueueClusterJoin`), and scheduler behavior (HA placement) is not
  emulated at all; those paths are covered by the live harness instead, which is
  the right split but worth knowing.

## What we are doing in Proxmox

### Coverage by domain

All six IMPL-0001 phases are implementation-complete **and live-verified**:

| Phase | Surface                                                                                                                                                        |
| ----- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1     | Transport, auth, version/capability gating, task (UPID) waiters, `mockpve`, unified client                                                                     |
| 2     | QEMU + LXC: full CRUD, power, migrate, disks/NICs, snapshots (+rollback), guest agent, OCI templates (9.1-gated)                                               |
| 3     | Storage: datastores, content, volume alloc/free, streaming ISO/disk upload, ZFS pools, SSH/SFTP side-channel                                                   |
| 4     | HA: resources, **9.x rules** (never the deprecated groups), CRS settings, DLB (9.2-gated), replication jobs                                                    |
| 5     | SDN (zones/VNets/subnets/fabrics), firewall (cluster/node/guest scopes), node networking                                                                       |
| 6     | Cluster options/resources, access/ACL/API tokens, nodes admin (apt/disks/certs/ACME), Ceph, PBS-side backup, console (VNC/SPICE/term + `Connect`), metrics/RRD |

### The honesty rule

The single most distinctive policy in the codebase: **if PVE's REST API cannot
do it, the SDK says so instead of pretending.** Ops shipped as documented
`pverr.ErrUnsupported` stubs — storage-level volume snapshots (confirmed absent
by reading a live node's own `apidoc.js`), RAIDZ expansion, HA arm/disarm, RBD
mirroring, SDN live status, OTel config, PBS-native verify — each with docs
pointing at the real path (usually the guest-level API or the SSH side-channel).
Several of these **reversed earlier guesses after live verification**, and the
reversal is recorded in the ledgers. Compare this with the common alternative:
an SDK method that compiles, looks plausible, and 404s at runtime.

### Things learned from live PVE that no doc states

Live verification produced knowledge that is now encoded in the SDK and its mock
— the kind of institutional knowledge you otherwise learn in production:

- Task exit status `WARNINGS: N` is **success**, not failure (`tasks.Wait`
  returns nil; `Status.Warnings()` flags it). Found on a debian-13 LXC create.
- Guest VNC tickets are **bound to their mint path** — presenting a guest ticket
  at the node-shell websocket path is a 401. Mockpve now enforces the binding so
  the misroute can never pass unit tests again.
- Cluster joins need a **two-stage convergence gate** (corosync nodelist, then a
  quorum + members-online check) because config presence precedes runtime
  health; a join fired into the settling window fails server-side.
- HA rule feasibility counts HA-**active** nodes, which lag `AddResource` by LRM
  cycles; rule updates must re-send the rule type's required properties.
- go-vcr must not use replayable interactions (task-status polls would replay
  "running" forever); upload bodies must not be chunk-encoded (501).

## The testing story

This is the project's strongest asset, and it is layered:

1. **`mockpve`** — an importable in-memory PVE responder (also a runnable
   server, the repo's only shipped binary). Consumers seed state (`AddVM`,
   `AddStorage`, …) rather than stubbing HTTP; every SDK op has unit tests
   against it, under the race detector.
2. **go-vcr cassettes, certified per PVE version.** 11 committed cassettes
   recorded against real PVE (9.2-1, 9.2.2, 9.1.1 batches), secrets and site
   topology scrubbed by an automated pipeline, replayed in CI on every push
   (`just test-replay`) — so "verified against real PVE" is a regression guard,
   not a one-time claim. `certification.yaml` records per-batch provenance and
   every mock-vs-real divergence found and fixed.
3. **The integration suite** (`//go:build integration`) — read-only checks for
   every phase plus env-gated destructive lifecycles (QEMU, LXC, ISO upload,
   console mint/RFB, HA placement).
4. **`pvelab` — the dogfood harness** (IMPL-0002). The SDK provisions ephemeral
   nested 3-node PVE clusters on the homelab host **using itself**: unattended
   ISO installs (~4m40s to a quorate 3-node cluster) or linked clones from a
   per-minor template (~3m10s, ~33% faster), runs the live suite, tears down to
   a clean host — with structural blast-radius guards (reserved VMID block
   9200–9399, name-prefix refusal that `-force` cannot override, teardown driven
   from config so a lost state file never strands VMs). The version matrix ran
   9.2 **and** 9.1 with zero SDK/mock divergences on 9.1.
5. **Schema-drift CI guard** (`pve-schemadiff`) — parses a PVE `apidoc.js` into
   a (method, path) set and fails CI on drift from the committed baseline.

The result: the two acceptance criteria that normally never get verified in an
SDK — _does the HA scheduler actually honor the placement rule_ and _does a real
RFB byte stream actually flow_ — are both verified on hardware, with the first
replaying in CI from a cassette.

## Why this instead of the alternatives

The comparison below reflects the ecosystem as of early 2026; the design axes,
not the point-in-time repo states, are the argument.

### The field

| Option                                      | What it is                                                               | Where it falls short for our purposes                                                                                                                                    |
| ------------------------------------------- | ------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **Telmate/proxmox-api-go**                  | The oldest Go client, built to serve the Telmate Terraform provider      | Largely untyped (`map[string]interface{}` responses, god-config structs), API shaped by the provider's needs, uneven context support, no shipped mock, bumpy maintenance |
| **luthermonson/go-proxmox**                 | The most polished general-purpose community client; typed, context-aware | No version-gating model (best-effort across PVE majors), basic error semantics, no importable test double, 9.x-era surfaces (HA rules, SDN fabrics, OCI templates) lag   |
| **bpg/terraform-provider-proxmox** (client) | The best-maintained Go code touching PVE                                 | Its client is **internal to the provider** — not an importable, supported SDK; semantics are shaped by Terraform CRUD                                                    |
| **DIY (`net/http` + pvesh docs)**           | Full control                                                             | You re-derive everything: ticket/CSRF auth, UPID polling, `0/1` bools, property-string encoding, error mapping, upload quirks — undifferentiated heavy lifting           |

### What actually differentiates this SDK

1. **A version honesty model instead of best-effort compatibility.** 9.x only,
   hard floor at construction, named per-minor gates that fail _before_ the
   request with `ErrUnsupported`. Consumers get "this node can't do that" at the
   call site, not a mystery 501 from an old node.
2. **The testing story ships with the SDK.** No other Go PVE client gives
   consumers an importable fake PVE. `pegaprox-go` (and anyone else) can
   integration-test VM lifecycles in CI with zero infrastructure — that alone
   changes what a consumer's own test suite can be.
3. **Verified, not asserted.** Live-recorded cassettes replay in CI;
   mock-vs-real divergences are hunted and logged per PVE version; the SDK
   provisions its own live test environment. "Works against real PVE" is backed
   by artifacts in the repo.
4. **Errors you can program against** (`errors.Is(err, pverr.ErrNotFound)`) and
   **reads that cannot lose data** (lossless `Extra`).
5. **The whole job, not just the JSON API**: streaming uploads, WebSocket
   console streams, an SSH/SFTP side-channel for non-REST ops, task waiters with
   correct WARNINGS semantics — the parts every real integration hits and most
   clients skip.
6. **Fleet alignment.** It targets exactly the PVE the homelab runs, ships under
   the same CI/release discipline as the rest of the fleet (signed releases,
   SBOM, automatic semver-label-driven tags, Renovate), and its design history
   is fully recorded in-repo.

### The honest flip side

- **Youth.** v0.6.x; the public API is stable-in-practice but not frozen (v1.0.0
  gates on core+compute+storage stability per DESIGN-0001).
- **One known consumer.** The community options have years of diverse production
  exposure this SDK does not.
- **9.x-only is a feature and a limitation** — anyone on PVE 8.x needs a
  different client, full stop.
- **Bus factor of one** — mitigated by unusually thorough design records and CI,
  but real.

## Risks and open items

1. **Provisional endpoint shapes** (DLB, SDN fabrics, DEB822, SMART, ACME) need
   re-verification as PVE minors ship; the schema-drift guard and the pvelab
   version matrix are the mechanism, but someone has to run them.
2. **PVE tracking burden.** Each new minor means: capability-gate review, a
   matrix run (one config file + one template build), possibly new cassette
   batches. On-demand cadence was the deliberate choice (INV-0001), so this is a
   known, bounded cost — not automated away.
3. **Repo split** (lifting `proxmox/` into its own module) is designed-for but
   not yet exercised; go.mod path churn for consumers when it happens.
4. **The recorded-corpus → fuzzed-mockpve pipeline** (IMPL-0001 OQ-4/5/10) is
   deferred; mockpve fidelity currently advances via the certification process
   rather than generative fuzzing.
5. **v1.0.0 criteria** should be made explicit soon — the surface has been
   stable across Phases 3–6; the longer v0.x persists, the more implicit
   compatibility promises accumulate.

## Bottom line

This is a production-grade SDK by the standards that matter for one: a uniform,
idiomatic public surface; an error and versioning model consumers can program
against; refusal to fabricate API surface; and a defense-in-depth verification
story (mock → certified cassettes → CI replay → self-provisioned live clusters)
that none of the existing Go options approach. The trade-offs are the deliberate
ones — 9.x-only scope and the maintenance cadence of tracking PVE minors — plus
the ordinary risks of a young, single-maintainer library. For the homelab fleet
(and for any consumer on PVE 9.x who values testability), it is the strongest
option available; the fastest way to de-risk its youth is exactly what is
planned: let `pegaprox-go` consume it hard, and cut v1.0.0 when the surface
survives that contact.
