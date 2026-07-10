---
id: DESIGN-0001
title: "Proxmox SDK package layout"
status: Draft
author: Donald Gifford
created: 2026-06-22
---

<!-- markdownlint-disable-file MD025 MD041 -->

# DESIGN 0001: Proxmox SDK package layout

<!--toc:start-->

- [Overview](#overview)
- [Goals and Non-Goals](#goals-and-non-goals)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
- [Background](#background)
- [Detailed Design](#detailed-design)
  - [Module & package layout](#module--package-layout)
  - [Construction & options](#construction--options)
  - [Unified client & accessors](#unified-client--accessors)
  - [Low-level transport](#low-level-transport)
  - [Connection & node failover](#connection--node-failover)
  - [Credentials & auth precedence](#credentials--auth-precedence)
  - [Representative service (the pattern)](#representative-service-the-pattern)
  - [Tasks & waiters](#tasks--waiters)
  - [Version gating](#version-gating)
  - [Error taxonomy](#error-taxonomy)
  - [Concurrency & context](#concurrency--context)
  - [SSH side-channel](#ssh-side-channel)
- [API / Interface Changes](#api--interface-changes)
- [Data Model](#data-model)
- [Testing Strategy](#testing-strategy)
- [Migration / Rollout Plan](#migration--rollout-plan)
- [Open Questions](#open-questions)
- [References](#references)
<!--toc:end-->

**Status:** Draft **Author:** Donald Gifford **Date:** 2026-06-22

## Overview

This designs the public surface of the standalone Proxmox SDK from ADR-0001: a
provider-specific Go module that wraps the Proxmox VE 9.x API, consumed by the
VM service (and a CLI). It fixes the package layout, the client construction and
service accessors, the version-gating mechanism, and the error taxonomy — the
contract IMPL-0001 ticks its coverage against.

## Goals and Non-Goals

### Goals

- A clean, idiomatic, **provider-specific** Go SDK: core primitives + typed
  per-domain services, modeled on AWS SDK v2, `bpg`, and `pvetui`'s `pkg/api`.
- One `Client` is safe for concurrent use, takes `context.Context` everywhere,
  and is fully mockable (interface-per-service + a `mockpve` server).
- A deterministic **error taxonomy** with `errors.Is`/`As` semantics and a clear
  retry/idempotency story.
- **Version gating** for PVE 9.x minors (per ADR-0002), so callers can ask "is
  this supported here?" rather than fail opaquely.
- The SDK prescribes no logging, caching, transport, persistence, or API/gRPC
  layer — those are injected by or owned by the consumer.

### Non-Goals

- **Multi-cluster** registry/grouping and cross-cluster orchestration — a
  `Client` is **one PVE endpoint**; multi-cluster is the consumer's job
  (ADR-0001 boundary).
- A provider-neutral abstraction (naos gets its own SDK; neutral interface, if
  ever, is consumer-side).
- PVE 8.x compatibility (ADR-0002).
- A reconciler / desired-state engine — the SDK is imperative + observational.

## Background

Prior art and decisions this builds on: ADR-0001 (standalone SDK, the
AWS-SDK-style core/services split, functional options, consumer-injected
cross-cutting, waiters, mock server), ADR-0002 (PVE 9.x-only floor with
per-minor gating), IMPL-0001 (the capability ledger). `pvetui/pkg/api` is the
closest structural reference; `bpg` is the layered-client reference (note its
9.x HA API support was still pending, so that area is greenfield). Proxmox has
**no event push** — everything is request/response or task polling, plus the
console (VNC/term) websocket. So the SDK offers polling primitives + waiters + a
console proxy; realtime fan-out is the consumer's.

## Detailed Design

### Module & package layout

Module is `github.com/donaldgifford/proxmox-go-sdk`; the SDK lives under
`proxmox/` (client in package `proxmox`, repo root is a doc-only `sdk`).
Exported packages:

```text
proxmox/                  # the SDK (its own module on the repo split)
├── proxmox.go            # unified Client, NewClient, accessors
├── options.go            # functional options (WithLogger, WithCache, ...)
├── types/                # primitives: VMID, NodeName, GuestRef, PowerState, PVEBool
├── pverr/                # error taxonomy: *Error + sentinels (import aliased)
├── api/                  # low-level client + transport
│   ├── client.go         # Client iface: DoRequest, ExpandPath, HTTP
│   ├── connection.go     # endpoint(s) + node failover, TLS (self-signed/IP), min-TLS, pooling
│   ├── credentials.go    # Token/Ticket/User creds + precedence
│   ├── auth.go           # ticket refresh (2h), CSRF on writes, token header
│   └── retry.go          # RetryPolicy, transient classification
├── version/              # ProxmoxVersion, Capabilities, Support* gates
├── tasks/                # UPID Ref, waiters, status, log
├── qemu/  lxc/           # compute services
├── storage/ nodes/ cluster/ access/   # ...
├── ha/ sdn/ ceph/ pbs/ console/ metrics/ firewall/   # remaining services
├── ssh/                  # SFTP/exec side-channel (non-REST ops)
├── mockpve/              # in-memory PVE responder for consumer tests
└── internal/             # unexported: 0/1-bool, marshalling, log redaction
```

Cross-cutting primitives live in `proxmox/types` and the error taxonomy in
`proxmox/pverr` — both leaves, imported directly (no re-export); domain types
live in their service package (AWS/k8s convention). The root `proxmox` package
holds only the client + options.

### Construction & options

```go
func NewClient(ctx context.Context, endpoint string,
    creds api.Credentials, opts ...Option) (*Client, error)
```

`NewClient` builds the connection, resolves credentials, and fetches `/version`
**once** to seed `Capabilities` (rejecting < 9.0). Options are functional:

```go
func WithHTTPClient(*http.Client) Option       // bring your own pooled client
func WithLogger(Logger) Option                 // consumer-supplied; default no-op
func WithCache(Cache) Option                   // consumer-supplied; default no-op
func WithRequestTimeout(time.Duration) Option
func WithRetry(api.RetryPolicy) Option
func WithClusterEndpoints(...api.Endpoint) Option // intra-cluster node failover
func WithMinTLS(uint16) Option
func WithInsecureSkipVerify(bool) Option       // self-signed / IP-only hosts
func WithUserAgent(string) Option
```

`Logger` and `Cache` are minimal interfaces the SDK _consumes_; it ships no-op
defaults so it prescribes nothing.

### Unified client & accessors

```go
type Client struct { /* api.Client, caps, opts */ }

func (c *Client) API() api.Client            // raw escape hatch
func (c *Client) Version() *version.Service
func (c *Client) Capabilities() version.Capabilities
func (c *Client) Cluster() *cluster.Service
func (c *Client) Access()  *access.Service
func (c *Client) Nodes()   *nodes.Service
func (c *Client) Tasks(node string)   *tasks.Service
func (c *Client) QEMU(node string)    *qemu.Service
func (c *Client) LXC(node string)     *lxc.Service
func (c *Client) Storage() *storage.Service
func (c *Client) HA()      *ha.Service
func (c *Client) SDN()     *sdn.Service
func (c *Client) Ceph()    *ceph.Service
func (c *Client) PBS()     *pbs.Service
func (c *Client) Console() *console.Service
func (c *Client) Metrics() *metrics.Service
func (c *Client) SSH()     *ssh.Client
```

### Low-level transport

```go
package api
type Client interface {
    // DoRequest performs one PVE call, unmarshalling data into out.
    DoRequest(ctx context.Context, method, path string, body, out any) error
    // ExpandPath templates a relative path, e.g. "status/start" ->
    // "/nodes/<node>/qemu/<vmid>/status/start".
    ExpandPath(path string) string
    HTTP() *http.Client
}
```

`DoRequest` owns CSRF injection on writes, ticket-expiry detection + one re-auth
retry, the `0/1`→bool normalisation, and error classification (below).

### Connection & node failover

A `Client` targets **one PVE cluster**, and by default uses a **single endpoint
address** (one node). Since every node fronts the whole cluster, one address is
correct — but it is a single point of failure if that node is down.

Optionally the caller supplies the cluster's other node addresses so the
transport can fail over between them:

```go
func WithClusterEndpoints(eps ...api.Endpoint) Option

// package api
type Endpoint struct {
    Name     string // node name, informational (e.g. "pve-node1")
    Address  string // host, host:port, or URL
    Priority int    // lower is tried first; ties break on declaration order
}
```

- The address passed to `NewClient` is the **primary** (priority 0); the option
  appends fallbacks. The transport holds the ordered, de-duplicated set.
- **Failover is transport-level only:** it triggers on dial/connection errors
  and `ErrTransient` (connection refused, TLS/handshake, timeout, 5xx/596) —
  never on 4xx or `ErrTaskFailed`. A dial failure means the request never
  reached PVE, so rotating to the next node is safe even for writes; once a
  request is in flight with an unknown outcome, the idempotency rule below
  applies (no auto-replay of non-idempotent writes).
- **Backoff reuses `api.RetryPolicy`** (`WithRetry`): each address gets the
  policy's attempts, then the client rotates to the next by priority.
- Auth is per-cluster, so a ticket/token carries across nodes — a ticket minted
  on node A is valid on node B of the same cluster.

This is **intra-cluster** resilience, not multi-cluster aggregation (a `Client`
is still one cluster). Typical CLI flow: connect once, read the node list
(`cluster`/`nodes` service), persist node→IP locally, then pass them via
`WithClusterEndpoints` on later runs so the client self-heals when the primary
node is unreachable.

### Credentials & auth precedence

```go
type Credentials interface{ /* unexported */ }
func TokenCredentials(tokenID, secret string) Credentials  // "user@realm!tok"
func TicketCredentials(ticket, csrf string) Credentials
func UserCredentials(user, password, otp string) Credentials
```

Precedence (resolved at construction): **ticket > API token > user/password**.
Token auth needs no refresh; user/pass mints a ticket and refreshes before the
2h expiry.

### Representative service (the pattern)

Every service embeds the low-level client + node + caps, and exposes an
interface for mocking. Operations that start a PVE task return a `tasks.Ref`:

```go
package qemu
type Service struct { c api.Client; node string; caps version.Capabilities }

type API interface {
    List(ctx context.Context) ([]VM, error)
    Get(ctx context.Context, vmid int) (*VM, error)
    Config(ctx context.Context, vmid int) (*Config, error)
    Create(ctx context.Context, spec CreateSpec) (tasks.Ref, error)
    Clone(ctx context.Context, vmid int, spec CloneSpec) (tasks.Ref, error)
    Start(ctx context.Context, vmid int) (tasks.Ref, error)
    Stop(ctx context.Context, vmid int, opts ...StopOption) (tasks.Ref, error)
    Delete(ctx context.Context, vmid int) (tasks.Ref, error)
}
var _ API = (*Service)(nil)
```

Caller awaits the task explicitly:

```go
ref, err := c.QEMU("pve").Clone(ctx, 9000, qemu.CloneSpec{NewID: 131})
if err != nil { return err }
st, err := c.Tasks("pve").Wait(ctx, ref)   // blocks on UPID poll
```

### Tasks & waiters

```go
package tasks
type Ref struct { Node, UPID string }
type Service struct { /* api.Client, node */ }
func (s *Service) Status(ctx context.Context, r Ref) (Status, error)
func (s *Service) Wait(ctx context.Context, r Ref) (Status, error)
func (s *Service) WaitFor(ctx context.Context, r Ref, cond func(Status) bool) (Status, error)
func (s *Service) Log(ctx context.Context, r Ref) ([]LogLine, error)
```

`Wait` polls with backoff until the UPID exits; a non-OK exit status returns
`ErrTaskFailed` carrying the task log. `ctx` cancellation stops the poll.

### Version gating

`Capabilities` is the fetched-once snapshot; services consult it before
attempting minor-gated operations:

```go
package version
type Capabilities struct { /* major, minor, patch */ }
func (c Capabilities) AtLeast(major, minor int) bool
func (c Capabilities) DynamicLoadBalancer() bool { return c.AtLeast(9, 2) }
func (c Capabilities) OCITemplates() bool        { return c.AtLeast(9, 1) }
func (c Capabilities) TokenSecretRotation() bool { return c.AtLeast(9, 2) }
func (c Capabilities) Require(feature string, min string) error // -> ErrUnsupported
```

Example use inside a service:

```go
if !s.caps.DynamicLoadBalancer() {
    return fmt.Errorf("dynamic load balancer: %w", ErrUnsupported)
}
```

`MinimumProxmoxVersion = 9.0`; `NewClient` errors on anything lower.

### Error taxonomy

These live in `proxmox/pverr` (imported aliased, e.g. `pverr`); `api.DoRequest`
classifies into them. A single rich `*Error` plus sentinels for `errors.Is`:

```go
type Error struct {
    Op      string            // "qemu.Start"
    Path    string            // "/nodes/pve/qemu/100/status/start"
    Status  int               // HTTP status
    Message string            // PVE message
    Params  map[string]string // per-parameter PVE validation errors
    UPID    string            // set when a task failed
    err     error             // wrapped sentinel/cause
}
func (e *Error) Error() string { /* ... */ }
func (e *Error) Unwrap() error { return e.err }

var (
    ErrNotFound      = errors.New("proxmox: not found")        // 404 / missing
    ErrConflict      = errors.New("proxmox: conflict")          // e.g. VMID in use
    ErrUnauthorized  = errors.New("proxmox: unauthorized")      // 401
    ErrTicketExpired = errors.New("proxmox: ticket expired")    // triggers re-auth
    ErrForbidden     = errors.New("proxmox: forbidden")         // RBAC denial
    ErrTaskFailed    = errors.New("proxmox: task failed")       // UPID exit != OK
    ErrUnsupported   = errors.New("proxmox: unsupported on this PVE version")
    ErrTransient     = errors.New("proxmox: transient")         // retryable
)
```

- **Classification:** `DoRequest` maps HTTP status + PVE body to a sentinel and
  wraps it in `*Error` with `%w`. Callers use `errors.Is(err, ErrNotFound)`
  etc.; `errors.As(err, &apiErr)` exposes `Status`, `Params`, `UPID`.
- **Retry:** only `errors.Is(err, ErrTransient)` (connection errors, 5xx, 596)
  is retried per `RetryPolicy`. `ErrTicketExpired` triggers exactly one
  re-auth + replay. 4xx (except expiry) is never retried.
- **Idempotency:** PVE has no idempotency tokens, so the SDK does **not**
  auto-retry non-idempotent writes (create/clone) when the outcome is unknown.
  It returns the `tasks.Ref`/`*Error` so the caller can poll or reconcile via a
  follow-up `Get`. VMID allocation (`cluster/nextid` + collision handling) is a
  consumer concern; the SDK surfaces `ErrConflict` cleanly.

### Concurrency & context

One `*Client` is safe for concurrent use; ticket refresh is mutex-guarded; a
shared pooled `*http.Client` carries connection reuse. Every operation honors
`ctx` deadline/cancellation — replacing PegaProx's manual stale-ticket and
empty-response heuristics.

### SSH side-channel

`ssh.Client` (in-module) covers the few ops the REST API can't: snippet/backup
SFTP upload (PAM account) and `(ssh)`-tagged tasks in IMPL-0001. Kept in the
same module as a sub-package; the consumer supplies SSH credentials/known-hosts.

## API / Interface Changes

New public module; no existing surface to change. The exported contract is:
`proxmox.NewClient` + options + the `Client` accessors above, the `api.Client`
interface, `Credentials` constructors, per-service `API` interfaces and typed
request/response structs, `tasks.Ref`/waiters, `version.Capabilities`, and the
error taxonomy. Everything else is `internal/`.

## Data Model

The SDK holds **no persistent state** — only in-memory connection state (auth
material, the `Capabilities` snapshot, the HTTP client). All PVE data crosses as
typed request/response structs with `json` tags and `0/1`→bool normalisation.
Persistence (inventory, credentials at rest, audit) is the consumer's, per
ADR-0001.

## Testing Strategy

- **Unit:** every exported op against `mockpve` (in-memory responder); golden
  fixtures from real 9.x responses.
- **Mockability:** consumers depend on the per-service `API` interfaces and mock
  them directly.
- **Table-driven:** `0/1`→bool, config-struct (un)marshalling, error
  classification, credential precedence.
- **Integration:** build-tag/env-gated against a live 9.x node; a 9.2 node for
  `(9.2+)` rows. Target >80% coverage on core + services.
- **Drift CI:** regenerate from `apidoc.js` (à la `pve-openapi-gen`) and fail on
  schema drift across 9.x minors.

## Migration / Rollout Plan

Greenfield. Build in IMPL-0001 phase order (core/auth/tasks → compute → storage
→ HA → network/SDN → cluster/access/console/metrics). During early co-evolution
with the service, use a local `go.mod replace` and tag SDK releases frequently
(`v0.x`); the service pins a known-good tag. Cut `v1.0.0` once the core +
compute

- storage surfaces are stable.

## Open Questions

All six questions raised in this draft are now resolved (recorded 2026-06-22):

- **Module path / repo name — RESOLVED.** Module is
  `github.com/donaldgifford/proxmox-go-sdk`; the client entrypoint is the
  `proxmox` subpackage (`proxmox.NewClient` / `proxmox.Client`), and the repo
  root is a doc-only `package sdk`. The whole SDK lives under `proxmox/` so it
  lifts into its own module on the repo split; the minor
  `proxmox-go-sdk/proxmox` import stutter is accepted and disappears post-split.
- **Read-only aggregation — RESOLVED (stays a Non-Goal).** A `Client` is one PVE
  endpoint (one cluster). Cross-_cluster_ read fan-out is the consumer's job —
  it holds N clients and fans out with an errgroup. If a thin helper is ever
  wanted it can be additive (taking `[]*proxmox.Client`) without bending the
  boundary. Note this is distinct from node-address failover _within_ one
  cluster — an accepted transport feature (`WithClusterEndpoints`; see the
  Connection & node failover section), not aggregation.
- **SSH packaging — RESOLVED.** `proxmox/ssh` stays an in-module sub-package
  (one tag, one version). `x/crypto/ssh` is light and per-package compilation
  keeps it out of REST-only consumers' binaries. Revisit only if SSH grows
  heavier deps.
- **Console primitive shape — RESOLVED.** `proxmox/console` mints the
  vnc/term/spice tickets, verifies the 9.x token-owned VNC auth-ticket, and
  exposes `Connect()` returning an `io.ReadWriteCloser` duplex stream to the PVE
  console (all PVE-native, so SDK). The browser noVNC/xterm bridge and realtime
  fan-out stay in the consumer.
- **Codegen scope — RESOLVED.** Reference + diff only: config structs are
  hand-written (typed common path + `map[string]any` fallback); `apidoc.js`
  feeds docs and a CI schema-drift diff across 9.x minors, never shipped types.
- **License — RESOLVED.** Apache-2.0 (permissive + patent grant, the infra-SDK
  norm). The AGPL service consumes it cleanly; see `LICENSE` at the repo root.

## References

- ADR-0001 — Separate the Proxmox SDK into its own repository
- ADR-0002 — Target Proxmox VE 9.x only
- IMPL-0001 — Proxmox VE 9.x SDK coverage (checks against this contract)
- `devnullvoid/pvetui` `pkg/api` (`options.go`, `interfaces/`, `mockpve`,
  `pve-openapi-gen`)
- `bpg/terraform-provider-proxmox` `proxmox/api` (layered client, version
  gating)
- AWS SDK for Go v2 (core/services split, functional options, waiters)
