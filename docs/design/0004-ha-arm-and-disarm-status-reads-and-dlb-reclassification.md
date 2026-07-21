---
id: DESIGN-0004
title: "HA arm and disarm, status reads, and DLB reclassification"
status: Approved
author: Donald Gifford
created: 2026-07-19
---

<!-- markdownlint-disable-file MD025 MD041 -->

# DESIGN 0004: HA arm and disarm, status reads, and DLB reclassification

**Status:** Approved **Author:** Donald Gifford **Date:** 2026-07-19 (OQs
decided 2026-07-21: 1a, 2b-as-lossless, 3b)

<!--toc:start-->

- [Overview](#overview)
- [Goals and Non-Goals](#goals-and-non-goals)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
- [Background](#background)
- [Detailed Design](#detailed-design)
  - [ArmHA / DisarmHA (stub → real)](#armha--disarmha-stub--real)
  - [HA status reads (new)](#ha-status-reads-new)
  - [Resource migrate / relocate (new)](#resource-migrate--relocate-new)
  - [DLB reclassification (real → ErrUnsupported)](#dlb-reclassification-real--errunsupported)
  - [mockpve](#mockpve)
- [API / Interface Changes](#api--interface-changes)
- [Data Model](#data-model)
- [Testing Strategy](#testing-strategy)
- [Migration / Rollout Plan](#migration--rollout-plan)
- [Open Questions](#open-questions)
- [References](#references)
<!--toc:end-->

## Overview

INV-0004 Findings 4 and 5 plus the HA slice of Finding 8, one PR (`minor`):
upgrade `ArmHA`/`DisarmHA` from `ErrUnsupported` stubs to the **real 9.2
endpoints** (`POST /cluster/ha/status/{arm,disarm}-ha`), add the
`/cluster/ha/status` reads and the resource `migrate`/`relocate` actions we
never modelled, and reclassify the Dynamic Load Balancer ops to documented
`ErrUnsupported` — `/cluster/ha/lbalancer` does not exist and every DLB call we
ship today would 404 live. The scope is honest at the package level: **bring
`ha` in line with the real 9.2 HA surface.**

## Goals and Non-Goals

### Goals

- Working `ArmHA`/`DisarmHA` gated on the existing `HAClusterSwitch` (9.2)
  capability.
- `HAStatusCurrent` (rich per-resource/manager state, including `armed-state`)
  and `GetManagerStatus` reads.
- `MigrateResource`/`RelocateResource` — the CRM request actions, with their
  typed result (blocking / comigrated resources from affinity evaluation).
- Honest DLB: `GetDLBStatus`/`SetDLBConfig` → always `pverr.ErrUnsupported`,
  docs pointing at the CRS options (`GetCRSSettings`/`SetCRSSettings`) that
  actually exist.
- mockpve mirrors real PVE: fabricated `lbalancer` routes removed; arm/disarm
  emulated observably.
- Live verification on the shared pvelab run (with DESIGN-0003).

### Non-Goals

- Any change to HA rules, resource config CRUD, CRS, or replication surfaces.

## Background

Phase 4 shipped `ArmHA`/`DisarmHA` as `ErrUnsupported` stubs ("no confirmed REST
endpoint — a GUI/pvecm action") and DLB as REST-with-caveat against the guessed
`/cluster/ha/lbalancer`. The real 9.2 apidoc (INV-0004) inverts both
classifications, and the mined shapes settle the semantics:

- `POST /cluster/ha/status/arm-ha` — **returns null, no parameters** →
  synchronous, optionless (the Phase 4 "HA config writes are synchronous" rule
  extends cleanly).
- `POST /cluster/ha/status/disarm-ha` — returns null, one parameter:
  `resource-mode` → synchronous with one option.
- `GET /cluster/ha/status/current` — array with `id`, `sid`, `node`,
  `crm_state`, `request_state`, `quorate`, **`armed-state`**, `auto-rebalance`,
  `failback`, `max_relocate`, `max_restart`, `resource_mode`.
- `GET /cluster/ha/status/manager_status` — the raw manager blob.
- `POST /cluster/ha/resources/{sid}/{migrate,relocate}` — synchronous CRM
  requests with a **typed result**: required `sid` + `requested-node`, optional
  `blocking-resources` (array of `{sid, cause}`, cause enum `node-affinity` |
  `resource-affinity`) and `comigrated-resources` (resources dragged to the same
  target by positive affinity).
- No `lbalancer` (or any balancer-like) path anywhere in the 675-endpoint set.
  The real 9.2 scheduler knobs remain the `crs` datacenter options the SDK
  already models.

## Detailed Design

### ArmHA / DisarmHA (stub → real)

```go
ArmHA(ctx) error                              // POST …/arm-ha (sync)
DisarmHA(ctx, opts ...DisarmHAOption) error   // POST …/disarm-ha (sync)
WithResourceMode(mode ResourceMode) DisarmHAOption
```

Both gate on `s.caps.Require("HA arm/disarm", "9.2")` (the existing
`HAClusterSwitch` capability, now load-bearing) — gate fires before any request,
matching the OCI-template precedent. `ArmHA` keeps its exact current signature;
`DisarmHA` gains a variadic option (non-breaking) using the per-op action-option
pattern (`DisarmHAOption` → unexported config → `resource-mode` form value).
`ResourceMode` is a string type with constants per the apidoc enum (verified
live before the cassettes commit).

### HA status reads (new)

```go
HAStatusCurrent(ctx) ([]HAStatusEntry, error)  // GET …/status/current
GetManagerStatus(ctx) (*ManagerStatus, error)  // GET …/status/manager_status
```

`HAStatusEntry` is a lossless read modelling the twelve observed fields
(`ArmedState` typed — it is the observable for arm/disarm). `ManagerStatus` (per
OQ-2 decision) is a fully typed **lossless read** of the observed blob —
`MasterNode`, `Timestamp`, `NodeStatus` (node -> state map), `ServiceStatus`
(sid -> typed entry, itself lossless) — with the standard `Extra` tail; the
apidoc pins no shape, so the live run is the source of truth for the typed
fields before cassettes commit.

### Resource migrate / relocate (new)

```go
MigrateResource(ctx, sid, node) (*MigrateResult, error)   // POST …/{sid}/migrate
RelocateResource(ctx, sid, node) (*MigrateResult, error)  // POST …/{sid}/relocate
```

Both are synchronous **requests to the CRM** — no UPID; the manager acts on its
next cycle — returning the apidoc-defined result: `SID`, `RequestedNode`,
optional `BlockingResources []BlockingResource` (`Cause` typed to the
`node-affinity`/`resource-affinity` enum) and `ComigratedResources []string`.
Convergence is observed via `HAStatusCurrent` (`node`/`request_state`) — the
read added above is exactly the observable these actions need. The
migrate/relocate distinction is PVE's: migrate attempts live migration; relocate
stops, moves, restarts. SIDs carry a colon → `url.PathEscape` (existing rule).
No version gate (the endpoints are baseline; the affinity-aware response body is
9.2-observed and hedged by lossless decode).

In mockpve the handlers echo `sid`/`requested-node` and move the resource's
synthesized `current` entry to the target node. The mock does not evaluate
affinity (established: the mock does not schedule), so `blocking-resources`
scenarios are live-verify territory — the placement suite's negative-affinity
pair provides them for free.

### DLB reclassification (real → ErrUnsupported)

`GetDLBStatus`/`SetDLBConfig` keep their signatures (interface-stable for test
doubles, the `ArmHA`-stub precedent in reverse) but always return a documented
`pverr.ErrUnsupported` directing callers to `GetCRSSettings`/`SetCRSSettings`.
No request is ever issued; the 9.2 gate check is removed from the call path. The
`DLBStatus`/`DLBConfig` types are **retained** (the `VolumeSnapshot` precedent —
a future PVE release may add the surface). The
`version.Capabilities.DynamicLoadBalancer()` gate is **removed** (OQ-3 decision
b — a pre-v1 break; nothing gates on it). mockpve's `lbalancer` routes are
**removed** — the mock mirrors real PVE.

### mockpve

`mockpve/ha.go`: `haState` gains `armed bool` (default true, matching a fresh
cluster); the arm/disarm handlers flip it; the new `/cluster/ha/status/current`
handler synthesizes one entry per seeded HA resource plus the manager entry,
reporting `armed-state` from the flag — so unit tests can observe arm/disarm
end-to-end (`Disarm` → `current` shows disarmed → `Arm` → armed).
`manager_status` returns a static plausible blob. This is the observable-effect
emulation pattern (`QueueClusterJoin` precedent, but wire-driven — no test seam
needed).

## API / Interface Changes

- `ha.API` gains `HAStatusCurrent`, `GetManagerStatus`, `MigrateResource`, and
  `RelocateResource`; `DisarmHA` gains variadic options (source-compatible for
  callers; **signature change for implementors** of the interface — acceptable
  pre-v1, noted in the changelog).
- `ArmHA`/`DisarmHA`/`GetDLBStatus`/`SetDLBConfig` keep their shapes; behavior
  changes are documented on the methods.
- Everything stays synchronous — no `tasks.Ref` enters the HA package (Phase 4
  structural rule holds). Migrate/relocate return a typed result + error, not a
  task: the CRM acts asynchronously and emits no UPID.

## Data Model

| Type                    | Kind                | Notes                                     |
| ----------------------- | ------------------- | ----------------------------------------- |
| `HAStatusEntry`         | lossless read (new) | 12 observed fields; `ArmedState` typed    |
| `ManagerStatus`         | lossless read (new) | typed observed blob + Extra (OQ-2: b)     |
| `ResourceMode`          | string const (new)  | disarm `resource-mode` enum, live-checked |
| `MigrateResult`         | lossless read (new) | sid, requested-node, blocking/comigrated  |
| `BlockingResource`      | read (new)          | `{sid, cause}`; `Cause` enum typed        |
| `DLBStatus`/`DLBConfig` | retained            | future-proofing, VolumeSnapshot precedent |

## Testing Strategy

- Unit: gate refusal below 9.2; arm→disarm→arm observable through the mock's
  `current`; `WithResourceMode` wire form; migrate/relocate wire form + the
  `current`-entry node move through the mock; DLB ops assert
  `pverr.ErrUnsupported` and zero HTTP traffic (`TestDLBUnsupported`, the
  `TestVolumeSnapshotsUnsupported` pattern).
- Live (shared pvelab run with DESIGN-0003): on the ephemeral nested cluster —
  `HAStatusCurrent` baseline → `DisarmHA` → observe `armed-state` → `ArmHA` →
  observe again; migrate an HA resource between nested nodes and watch
  convergence via `HAStatusCurrent`; with the placement suite's
  negative-affinity pair, assert a conflicting migrate returns
  `blocking-resources` with cause `resource-affinity`; record
  `TestHAArmDisarmCycle` + `TestHAStatusReads` + `TestHAResourceMigrate`
  cassettes; verify the `resource-mode` enum values; certification batch entry.
  Zero blast radius: the cluster is destroyed minutes later. Depth per OQ-1.
- Replay: new cassettes wired into `just test-replay`.

## Migration / Rollout Plan

1. Implement + mock-verify; PR labelled `minor` (new surface + behavior
   reclassification, changelog notes for both).
2. Live-verify on the shared pvelab run; reconcile divergences (especially the
   `resource-mode` enum and `current`'s exact fields) before committing
   cassettes.
3. Cassettes + certification entry + IMPL-0001 P4 margin note ride the follow-up
   per the established pattern.

Consumers: `pegaprox-go` does not call the affected ops; the DLB behavior change
converts a would-be live 404 into a typed error — strictly less surprising.

## Open Questions

1. **Does the live run actually disarm HA on the nested cluster?** **Decision
   (2026-07-21): a.**
   - **a (recommended):** Yes — full disarm→observe→arm cycle. The ephemeral
     pvelab cluster exists precisely so cluster-wide switches can be flipped
     with zero blast radius, and `armed-state` in `current` makes the effect
     assertable. The suite never targets a real cluster (env-gated like all
     destructive tests).
   - b: Mock-only for the cycle; live run records status reads only.
     Safer-feeling but the safety is already structural, and the cycle is the
     entire point of the upgrade.

2. **How is `ManagerStatus` modelled?** **Decision (2026-07-21): b, implemented
   as the house lossless-read pattern** — a fully typed struct for every field
   the live run observes (testable, per review), with the standard
   `UnmarshalJSON` -> `Extra` tail so a PVE-pushed change lands in `Extra`
   instead of breaking the decode (the apidoc pins nothing —
   `returns: {"type": "object"}` — so plain-b would have no contract to mirror).
   Drift is tracked by cassette re-certification per PVE version and surfaces in
   the changelog when fields get promoted. Option a's mixed RawMessage core is
   withdrawn as confusing/inconsistent.
   - **a (recommended):** Minimal typed core (manager node, quorum flag,
     per-service map as `json.RawMessage`) + lossless `Extra` — the blob is
     internal manager state whose schema PVE does not pin; commit to nothing
     beyond what the live run confirms.
   - b: Fully typed struct mirroring the observed blob. Richer now, brittle
     across minors.
   - c: Return the raw `json.RawMessage` only. Honest but pushes all parsing
     onto consumers.

3. **What happens to `Capabilities.DynamicLoadBalancer()`?** **Decision
   (2026-07-21): b — remove it** (pre-v1 break; nothing gates on it anymore, and
   an informational gate for a surface PVE never shipped invites the next
   guess).
   - **a (recommended):** Keep it, re-documented as informational ("9.2+ ships
     the CRS rebalance-on-start controls") — the `VolumeChainSnapshots()`
     precedent; removing a public method is a needless break.
   - b: Remove it (pre-v1 break) since nothing gates on it anymore.

## References

- INV-0004 — Findings 4 (no lbalancer path) and 5 (arm/disarm exist); mined
  `returns`/params for all four status endpoints (2026-07-19)
- Phase 4 design memo (ha-module-architecture) — the original stub /
  REST-with-caveat decisions this design supersedes
- `storage.VolumeSnapshots` reclassification — the honesty-rule precedent
- DESIGN-0003 — shares the pvelab live-verification run
