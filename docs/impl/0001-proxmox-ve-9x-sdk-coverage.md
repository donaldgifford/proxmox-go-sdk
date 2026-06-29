---
id: IMPL-0001
title: "Proxmox VE 9.x SDK coverage"
status: Draft
author: Donald Gifford
created: 2026-06-22
---

<!-- markdownlint-disable-file MD025 MD041 -->

# IMPL 0001: Proxmox VE 9.x SDK coverage

<!--toc:start-->

- [IMPL 0001: Proxmox VE 9.x SDK coverage](#impl-0001-proxmox-ve-9x-sdk-coverage)
  - [Objective](#objective)
  - [Scope](#scope)
    - [In Scope](#in-scope)
    - [Out of Scope](#out-of-scope)
  - [Coverage legend](#coverage-legend)
  - [Implementation Phases](#implementation-phases)
    - [Phase 1: Core, auth, version, tasks (foundation)](#phase-1-core-auth-version-tasks-foundation)
      - [Tasks](#tasks)
      - [Success Criteria](#success-criteria)
    - [Phase 2: Compute — QEMU + LXC](#phase-2-compute--qemu--lxc)
      - [Tasks](#tasks-1)
      - [Success Criteria](#success-criteria-1)
    - [Phase 3: Storage](#phase-3-storage)
      - [Tasks](#tasks-2)
      - [Success Criteria](#success-criteria-2)
    - [Phase 4: HA, scheduling, replication](#phase-4-ha-scheduling-replication)
      - [Tasks](#tasks-3)
      - [Success Criteria](#success-criteria-3)
    - [Phase 5: Network + SDN](#phase-5-network--sdn)
      - [Tasks](#tasks-4)
      - [Success Criteria](#success-criteria-4)
    - [Phase 6: Cluster, access, nodes-admin, Ceph, backup, console, metrics](#phase-6-cluster-access-nodes-admin-ceph-backup-console-metrics)
      - [Tasks](#tasks-5)
      - [Success Criteria](#success-criteria-5)
  - [File Changes](#file-changes)
  - [Testing Plan](#testing-plan)
  - [Dependencies](#dependencies)
  - [Open Questions](#open-questions)
  - [References](#references)
  <!--toc:end-->

**Status:** Draft **Author:** Donald Gifford **Date:** 2026-06-22

## Objective

Track which Proxmox VE 9.x capabilities the Proxmox SDK wraps, and to what
degree. This is a **living coverage matrix**: each capability is a checkbox the
SDK ticks off as it is implemented and tested. "Done" means a typed operation
exists, is unit-tested against `mockpve`, and has at least one integration test
against a live 9.x node.

**Implements:** ADR-0001 (standalone Proxmox SDK), ADR-0002 (PVE 9.x-only).
**Design:** DESIGN-0001 (Proxmox SDK package layout) — this ledger tracks
coverage against that contract.

**Design alignment (per the resolved DESIGN-0001 questions, 2026-06-22):** the
SDK lives under `proxmox/` (client in package `proxmox`, root is a doc-only
`sdk`); the transport supports optional intra-cluster node failover
(`WithClusterEndpoints`, Phase 1); `console` mints tickets, verifies the 9.x
auth-ticket, and exposes `Connect()` (Phase 6); config codegen is reference +
diff only (hand-written structs); `ssh` is an in-module sub-package; the licence
is Apache-2.0 (`LICENSE`); shared primitives live in `proxmox/types` and the
error taxonomy in `proxmox/pverr` (OQ-1). Remaining build-time decisions are in
[Open Questions](#open-questions).

## Scope

### In Scope

- Capabilities Proxmox VE 9.x exposes server-side (REST `/api2/json` + the
  SSH/SFTP side-channel for the few ops the REST API cannot do).
- Per-minor gating across 9.0 / 9.1 / 9.2 (the API is unversioned within the
  major).

### Out of Scope

- Consumer-side orchestration PVE has no API for (cross-cluster
  balancing/migration, policy we compute ourselves) — that lives in the service,
  not the SDK.
- The frontend, and any future naos provider (its own SDK).

## Coverage legend

Annotate each task as it lands:

- `[ ]` not started · `[~]` partial · `[x]` done (typed + mock-tested +
  live-tested)
- `(9.1+)` / `(9.2+)` — capability requires that minor; SDK must `version`-gate
  it
- `(tp)` — Proxmox tech-preview; wrap behind a capability flag, expect churn
- `(ssh)` — needs the SSH/SFTP side-channel, not pure REST

## Implementation Phases

Phases are sequenced so each builds on the last. A phase is complete when its
tasks are checked and its success criteria are met.

---

### Phase 1: Core, auth, version, tasks (foundation)

The transport and primitives every service hangs off.

#### Tasks

- [x] `DoRequest(ctx, method, path, req, resp)` + `ExpandPath` path templating
- [x] `Connection`: primary endpoint + optional ordered cluster-node failover
      set (`WithClusterEndpoints`), TLS (self-signed/IP, min-TLS), retry/backoff
      that rotates across nodes
- [x] Credentials + precedence: auth-ticket > API token > user/pass; 2 h ticket
      refresh; CSRF on writes
- [x] `version` service: `MinimumProxmoxVersion = 9.0` + `Support*()` per-minor
      gates
- [x] `tasks`: UPID parse, `WaitForTask` / `WaitForStatus` waiters, task-log
      read
- [x] `0`/`1` → bool handling (`types.PVEBool`), typed error taxonomy (`pverr`:
      NotFound/Conflict/AuthExpired/TaskFailed/Transient/…)
- [x] `mockpve` server + mockable interfaces; functional options
      (`WithLogger`/`WithCache`/`WithHTTPClient`/`WithTLS`)
- [x] Root `proxmox` package: `NewClient` (seeds `Capabilities` from `/version`,
      rejects < 9.0) + `Client` accessors + functional options; placement of
      shared primitives & the error taxonomy per **OQ-1**
- [x] Promote the `doc.go` stubs (created in the skeleton commit) for every
      Phase 1 package — `api`, `types`, `pverr`, `version`, `tasks`, root
      `proxmox`: replace the "Skeleton: no implementation yet" placeholder with
      a real package overview + a runnable `Example`; `go doc ./...` renders
      cleanly

#### Success Criteria

- `go build ./...` clean; auth + a trivial `GET /version` round-trips against
  live 9.x
- Waiters drive a real start/stop task to completion

> **Status (all 9 tasks done):** `go build ./...` and `just lint`/`just test`
> (race) are green. Auth (token / pre-minted ticket / user-pass mint+refresh) +
> `GET /version` round-trip and the task waiters (running→stopped OK/failed) are
> **verified against the in-process `mockpve` responder**, not a live 9.x node.
> The two live-only criteria above are therefore **written-but-unverified** in
> this environment (no live node / recorded cassettes — see CLAUDE.md); they
> stand to be confirmed once a 9.x node is reachable.

---

### Phase 2: Compute — QEMU + LXC

#### Tasks

- [x] QEMU: list, status, config get/set, create, clone, delete
- [ ] QEMU power: start/stop/shutdown/reboot/suspend/resume
- [ ] QEMU migrate (online/offline), disk + NIC add/resize/remove
- [ ] QEMU snapshots: list/create/rollback/delete (+ TPM-state snapshots on
      NFS/CIFS/dir `(9.1+)`)
- [ ] Guest-agent exec + fine-grained agent privileges (9.x model)
- [ ] LXC: list, status, config, create, clone, delete, power
- [ ] LXC snapshots (ZFS/btrfs/LVM-thin backing)
- [ ] LXC from **OCI image templates** `(9.1+ tp)` — pull/upload OCI as template
- [ ] Promote the `doc.go` stubs for `qemu` + `lxc` — real package overview + a
      runnable `Example` (e.g. clone → start)

#### Success Criteria

- Create→start→snapshot→rollback→stop→delete works end-to-end for both QEMU and
  LXC

---

### Phase 3: Storage

#### Tasks

- [ ] Datastore list + status; content listing (volumes, ISOs, templates,
      backups)
- [ ] Volume create/resize/delete/move
- [ ] **Snapshots as volume chains** on thick-LVM + Directory/NFS/CIFS
      `(tp → maturing)` — capability-gated
- [ ] ISO / disk-image upload (large-file streaming)
- [ ] Snippet + backup upload `(ssh)` — SFTP via PAM account
- [ ] ZFS pool ops incl. RAIDZ expansion `(9.x)`
- [ ] Promote the `doc.go` stubs for `storage` (and the `ssh` side-channel) —
      real package overview + a runnable `Example`

#### Success Criteria

- Upload an ISO, create a volume-chain snapshot where supported, clean up

---

### Phase 4: HA, scheduling, replication

The 9.x-reworked area — model rules, never the deprecated groups.

#### Tasks

- [ ] HA resources: add/remove (incl. add-after-create/restore), state
      management
- [ ] **HA rules**: node-affinity + resource-affinity (resource-to-node,
      resource-to-resource); enable/disable
- [ ] CRS settings read/write (static-load scheduler)
- [ ] **Dynamic Load Balancer** controls `(9.2+)` — continuous CRS rebalancing
      toggle/config
- [ ] Arm/Disarm HA cluster-wide switch `(9.2+)`
- [ ] Storage/ZFS replication jobs (respect new `VM.Replicate` privilege)
- [ ] Promote the `doc.go` stub for `ha` — real package overview + a runnable
      `Example` (define a resource-affinity rule)

#### Success Criteria

- Define a resource-affinity rule via the SDK and observe placement honor it

---

### Phase 5: Network + SDN

#### Tasks

- [ ] Node networking: bridges, bonds, VLAN-aware bridges, interface config
      (package placement per **OQ-8**)
- [ ] SDN zones (VLAN/VXLAN/EVPN) + VNets + subnets
- [ ] **SDN Fabrics** `(9.0+)` — OpenFabric/OSPF; gate newer protocols
      (WireGuard/BGP route-maps/IPv6 underlay) `(9.2+)`
- [ ] SDN status reporting (connected guest NICs, EVPN learned IPs/MACs, fabric
      routes/neighbors)
- [ ] Firewall: rules, ipsets (incl. overlapping ipset support `(9.1+)`)
- [ ] Promote the `doc.go` stubs for `sdn` + `firewall` (and node networking in
      `nodes`) — real package overview + a runnable `Example`

#### Success Criteria

- Enumerate zones/VNets/fabrics and their live status without error

---

### Phase 6: Cluster, access, nodes-admin, Ceph, backup, console, metrics

#### Tasks

- [ ] Cluster: `/cluster/resources`, status, options
- [ ] Access: users, groups, roles, ACLs using the **9.x privilege model**
      (`VM.Replicate`, granular agent privs; no `VM.Monitor`)
- [ ] API tokens: create/list/revoke, clear comment `(9.1+)`, **regenerate
      secret in place** `(9.2+)`
- [ ] Node admin: package updates (DEB822 sources), disks/SMART,
      certificates/ACME, custom scripts `(ssh)`
- [ ] Ceph: pools, OSDs, RBD mirroring (Squid)
- [ ] PBS integration: datastores, backups, verify, restore
- [ ] Console: mint VNC/SPICE/term tickets, verify the **token-owned VNC
      auth-ticket** `(9.x)`, and `Connect()` a duplex byte stream to the
      console; the browser bridge is the consumer's
- [ ] Metrics: extended metrics (CPU/mem/IO pressure stall, ZFS ARC);
      OpenTelemetry exporter `(9.1+)`
- [ ] Promote the `doc.go` stubs for `cluster`, `access`, `nodes`, `ceph`,
      `pbs`, `console`, `metrics`, `mockpve` — real package overview + a
      runnable `Example`

#### Success Criteria

- Mint a VNC console session through the SDK; list users/tokens under the 9.x
  privilege model

---

## File Changes

Package skeletons already exist as `doc.go` stubs (the reconcile commit), each a
one-line summary plus a "Skeleton: no implementation yet" placeholder — so the
per-phase godoc tasks above _promote_ an existing stub, they do not create the
file. This table maps the real code to phases. Column widths are re-aligned by
`just fmt`.

| File                                                        | Action | Description                                               |
| ----------------------------------------------------------- | ------ | --------------------------------------------------------- |
| `proxmox/api/{client,connection,credentials,auth,retry}.go` | Create | Phase 1 — low-level client + transport                    |
| `proxmox/types/`                                            | Create | Phase 1 — primitives: VMID, GuestRef, … (OQ-1)            |
| `proxmox/pverr/`                                            | Create | Phase 1 — error taxonomy: `*Error` + sentinels (OQ-1)     |
| `proxmox/{version,tasks}/`                                  | Create | Phase 1 services                                          |
| `proxmox/{qemu,lxc}/`                                       | Create | Phase 2 compute                                           |
| `proxmox/storage/`                                          | Create | Phase 3                                                   |
| `proxmox/{ha,cluster}/`                                     | Create | Phase 4                                                   |
| `proxmox/{sdn,firewall}/` (node net in `nodes`; OQ-8)       | Create | Phase 5                                                   |
| `proxmox/{access,nodes,ceph,pbs,console,metrics}/`          | Create | Phase 6                                                   |
| `proxmox/mockpve/`                                          | Create | mock server (all phases) + `cmd/mockpve/` runnable server |
| `proxmox/{proxmox,options}.go`                              | Create | Phase 1 — root: client + options, no aliases (OQ-1)       |
| `proxmox/ssh/`                                              | Create | SFTP/exec side-channel (Phase 3/6 ops)                    |
| `cmd/pve-schemadiff/`                                       | Create | CI schema-drift tool (OQ-7)                               |
| `LICENSE`                                                   | Done   | Apache-2.0                                                |

## Testing Plan

- [ ] Unit tests for every exported operation against `mockpve` (model per
      **OQ-4**)
- [ ] Integration tests against a live 9.x node (and a 9.2 node for `(9.2+)`
      rows); harness per **OQ-5**
- [ ] Table-driven tests for the `0/1`→bool + config-struct (un)marshalling
- [ ] CI `version`-diff step: regenerate from `apidoc.js`, flag drift across 9.x
      minors
- [ ] `Example` functions compile + run under `go test`; `go doc ./...` renders
      every package's overview (godoc coverage gate)

## Dependencies

- ADR-0001 — standalone SDK split (this is the SDK's coverage ledger)
- ADR-0002 — PVE 9.x-only floor (defines the gating baseline)
- A live PVE 9.x cluster (ideally one 9.0/9.1 and one 9.2) for integration tests

## Open Questions

Build-time decisions surfaced while planning Phase 1. **OQ-1–OQ-10 are
resolved** — the chosen letter is in each heading; the lettered options are kept
as record. OQ-1 amended DESIGN-0001's "primitives live in the root package"
line.

### OQ-1. Package layering for shared primitives + the error taxonomy — RESOLVED (a)

The unified `proxmox` package imports the service subpackages (its accessors
return `*qemu.Service`, etc.), so whatever the services depend on must sit
_below_ them — they cannot live in the root `proxmox` package.

**Decision (a):** dedicated leaf packages, no re-export.

- `proxmox/types` — primitives: `VMID`, `NodeName`, `GuestRef`, `PowerState`,
  `PVEBool`.
- `proxmox/pverr` — error taxonomy: the `*Error` type + sentinels
  (`ErrNotFound`, …) + classification. Named `pverr` (not `errors`) to avoid
  shadowing stdlib `errors`, so no call-site alias is forced.
- `proxmox/api` stays the low-level client and imports `pverr` to classify.
- Services import `api`, `types`, `pverr`; the root `proxmox` package holds only
  the client + options.

Consumers import the package they need (`types.VMID`, `pverr.ErrNotFound`) — the
same shape as AWS (`service/<svc>/types`, `smithy`) and k8s (`apierrors`). No
alias-façade in the root (the un-idiomatic part we rejected). This amends
DESIGN-0001's earlier "primitives live in the root package" line.

Alternatives (not chosen): **b** errors co-located in `types` (AWS-style, one
fewer package); **c** primitives + errors both in `api`; **d** move the client
to `proxmox/client` with primitives in the root.

### OQ-2. Node-failover behavior (`WithClusterEndpoints`) — RESOLVED (a)

How the transport picks among a cluster's node addresses.

- **a (recommended):** Sticky + passive — use the primary until a transport
  error, then rotate by priority and stay there. No background health checks (a
  library shouldn't spawn goroutines the caller can't see).
- **b:** Sticky + periodic re-probe — as above, but occasionally retry
  priority-0 so it drifts back to the preferred node after recovery.
- **c:** Per-request priority walk — every request starts at priority-0. Simple,
  but more dial churn while a node is down.
- **other:** \_\_\_\_\_

### OQ-3. Ticket-refresh strategy (user/pass auth) — RESOLVED (a)

API tokens need no refresh; tickets expire at 2h.

- **a (recommended):** Lazy + reactive — check the expiry timestamp (minus skew)
  before each request and re-auth if due; also re-auth once and replay on
  `ErrTicketExpired`. No background timer.
- **b:** Proactive background timer that refreshes ahead of expiry — smoother
  under load, but a library-owned goroutine to manage.
- **c:** Purely reactive — re-auth only on `ErrTicketExpired` / 401. Simplest;
  every long-idle client burns one failed request first.
- **other:** \_\_\_\_\_

### OQ-4. How `mockpve` models PVE — RESOLVED (c)

Drives Phase 1 and every unit test.

**Decision (c):** the SDK's own unit + integration tests replay recorded
`go-vcr` cassettes of real PVE exchanges — one ground-truth corpus, shared with
OQ-5. Caveat: go-vcr is **client-side** replay, so it cannot power the _shipped_
`proxmox/mockpve` / `cmd/mockpve` **server** consumers run against — that
substrate is a separate call, now **OQ-10**.

- **a (recommended):** Stateful in-memory model for resources under test (create
  → appears in list → delete), seeded from golden fixtures for read-heavy
  endpoints. Lets unit tests exercise waiters and lifecycle flows.
- **b:** Pure golden-fixture replay (request → canned response). Simple, but
  can't represent state transitions (start → running, task → OK).
- **c:** Recorded cassettes (`go-vcr`) from a live node, replayed. Highest
  fidelity; tests depend on a capture step.
- **other:** \_\_\_\_\_

### OQ-5. Integration-test harness — RESOLVED (a)

DESIGN-0001 wants live 9.0/9.1 + 9.2 coverage; CI has no PVE.

- **a (recommended):** Build-tag + env-configured live nodes for opt-in/local
  runs, plus committed `go-vcr` cassettes for CI replay.
- **b:** Live-only via env vars; CI runs unit/mock only.
- **c:** Cassettes-only in CI; live optional and undocumented.
- **other:** \_\_\_\_\_

### OQ-6. Modeling the sprawly config objects (`net0=virtio,bridge=vmbr0`, …) — RESOLVED (a)

The `ConfigQemu`-class surface (Phase 2/3).

**Decision (a):** typed common path + `Extra map[string]string` fallback.
**Under discussion (your cassettes idea):** use the recorded cassettes
(OQ-4/OQ-5) as ground truth — a CI test unmarshals each into the typed structs
and flags any field that lands in `Extra`, giving a data-driven worklist to
_promote_ fields toward fuller typing (b) incrementally rather than big-bang.
apidoc.js (OQ-7) still covers the declared superset for completeness.

- **a (recommended):** Typed common path + fallback — typed fields/parsers for
  the stable, common keys; a `map[string]string` (`Extra`) for the long tail.
  Matches Telmate's split and the reference-only codegen stance.
- **b:** Fully typed — model every key. Most ergonomic, but large and brittle
  against the unversioned API.
- **c:** Raw strings — expose PVE's `key=val,…` verbatim; caller parses. Minimal
  SDK code, worst consumer experience.
- **other:** \_\_\_\_\_

### OQ-7. Home for the `apidoc.js` schema-drift tool — RESOLVED (a)

Codegen is reference + diff only (resolved); the diff tool still needs a place.

- **a (recommended):** A `cmd/pve-schemadiff` helper, outside the library
  surface, run in CI (built like `cmd/mockpve`); defer the build to late Phase 1
  / Phase 2.
- **b:** `internal/tools/` behind a `tools` build tag.
- **c:** Defer entirely — add it only when drift actually bites.
- **other:** \_\_\_\_\_

### OQ-8. Node-networking package placement — RESOLVED (a)

Phase 5 covers per-node interface config (`/nodes/{node}/network`). The skeleton
has `nodes`, `sdn`, `firewall` — no `network`.

- **a (recommended):** Put node networking under `nodes` (it is node-scoped);
  keep `sdn` and `firewall` separate. Matches the skeleton.
- **b:** A dedicated `network` package for node interface config.
- **other:** \_\_\_\_\_

### OQ-9. Delivery shape — strict phases or a vertical slice first? — RESOLVED (a)

The overview doc suggested proving the architecture end-to-end early.

- **a (recommended):** Finish Phase 1, then a thin vertical slice (qemu
  start/stop + task wait against a live node) before completing Phase 2 — proves
  auth/transport/waiters end-to-end early.
- **b:** Strictly sequential — complete each phase before the next.
- **c:** Full vertical slice across all phases first (auth → qemu → console),
  then backfill breadth.
- **other:** \_\_\_\_\_

### OQ-10. What powers the shipped `mockpve` server? (surfaced by OQ-4) — RESOLVED (a)

OQ-4 chose `go-vcr` cassettes for the SDK's own tests, but go-vcr is client-side
replay — it can't be the `proxmox/mockpve` package / `cmd/mockpve` server
consumers run against, so that server needs its own substrate.

**Decision (a):** one ground-truth corpus, two consumers. Real PVE exchanges are
recorded once; those recordings back the SDK's own `go-vcr` cassettes (OQ-4),
and a **fuzzed** copy of the same recordings becomes the `proxmox/mockpve` /
`cmd/mockpve` server's response set — so the client-side tests and the shipped
mock never drift from real responses. This is the `wiz-go-gen` pattern: tests
built from real recorded responses, then those responses fuzzed to serve the
mock server (see References).

- **a (recommended):** A fixture-backed responder seeded from the **same
  recorded corpus** the cassettes use — one source of truth (real PVE responses)
  powers both go-vcr (our client-side tests) and the mockpve server (consumers).
  Statefulness limited to scripted sequences.
- **b:** A hand-written **stateful in-memory** model (create → list → delete),
  independent of cassettes — more flexible for arbitrary consumer scenarios,
  more to maintain (this was OQ-4's option a).
- **c:** **Defer** the server — ship the recorded corpus + the importable
  transport so consumers wire their own go-vcr; build `cmd/mockpve` later.
- **other:** \_\_\_\_\_

## References

- DESIGN-0001 — Proxmox SDK package layout (the public contract this ledger
  tracks)
- Proxmox VE Roadmap + 9.0/9.1/9.2 release notes
- `devnullvoid/pvetui` `pkg/api` + `pve-openapi-gen` + `mockpve` (structural
  reference)
- `donaldgifford/wiz-go-gen` `test/mock/` — reference for the
  recordings→fuzzed-mock approach (OQ-4 / OQ-5 / OQ-10):
  <https://github.com/donaldgifford/wiz-go-gen/blob/main/test/mock/>
- `bpg/terraform-provider-proxmox` (client + version-gating reference; note its
  9.x HA-API support was still pending — that area is greenfield against the 9.x
  docs)
