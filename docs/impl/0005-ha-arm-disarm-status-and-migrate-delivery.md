---
id: IMPL-0005
title: "HA arm disarm status and migrate delivery"
status: In Progress
author: Donald Gifford
created: 2026-07-21
---

<!-- markdownlint-disable-file MD025 MD041 -->

# IMPL 0005: HA arm disarm status and migrate delivery

**Status:** In Progress **Author:** Donald Gifford **Date:** 2026-07-21 (OQs
decided 2026-07-21: all a)

<!--toc:start-->

- [Objective](#objective)
- [Scope](#scope)
  - [In Scope](#in-scope)
  - [Out of Scope](#out-of-scope)
- [Confirmed wire facts](#confirmed-wire-facts)
- [Implementation Phases](#implementation-phases)
  - [Phase 1: SDK surface, mockpve, unit tests](#phase-1-sdk-surface-mockpve-unit-tests)
    - [Tasks](#tasks)
    - [Success Criteria](#success-criteria)
  - [Phase 2: PR and merge](#phase-2-pr-and-merge)
    - [Tasks](#tasks-1)
    - [Success Criteria](#success-criteria-1)
  - [Phase 3: Live verification and closure](#phase-3-live-verification-and-closure)
    - [Tasks](#tasks-2)
    - [Success Criteria](#success-criteria-2)
- [Open Questions](#open-questions)
- [References](#references)
<!--toc:end-->

## Objective

Implement DESIGN-0004 — bring the `ha` package in line with the real 9.2 HA
surface: `ArmHA`/`DisarmHA` upgraded from `ErrUnsupported` stubs to the real
`POST /cluster/ha/status/{arm,disarm}-ha` endpoints, the `/cluster/ha/status`
reads (`current` + `manager_status`) and resource `migrate`/`relocate` actions
added, and the Dynamic Load Balancer ops reclassified to documented
`ErrUnsupported` (the guessed `/cluster/ha/lbalancer` does not exist). Unlike
DESIGN-0003, no code exists yet — this is a fresh implementation.

**Implements:** DESIGN-0004 (OQ decisions 2026-07-21: 1a, 2b-as-lossless, 3b).

## Scope

### In Scope

- The `ha` package changes, mockpve emulation, unit tests, and doc surfaces for
  every op in DESIGN-0004.
- Removal of `version.Capabilities.DynamicLoadBalancer()` (OQ-3 decision b —
  pre-v1 break, nothing gates on it).
- One `minor` PR, then live verification on the shared pvelab run (with
  IMPL-0004 Phase 3), cassettes, certification entry, and doc closure
  (DESIGN-0004 → Implemented; IMPL-0001 Phase-4 margin note).

### Out of Scope

- HA rules, resource-config CRUD, CRS, and replication — untouched (DESIGN-0004
  non-goal).
- DESIGN-0005 (coverage tracker) — lands after this merges.
- Any `tasks.Ref` in the HA package — the Phase-4 structural rule (all HA writes
  synchronous) holds; migrate/relocate return a typed result, not a task.

## Confirmed wire facts

Mined from the committed real 9.2 apidoc (2026-07-21) — firmer than the design
had them, and they shape the tasks below:

- `POST …/arm-ha`: **no parameters**, returns null (sync, optionless).
- `POST …/disarm-ha`: `resource-mode` is an enum **`freeze` | `ignore`** and is
  **required** (no `optional` flag in the apidoc) — the design assumed one
  _optional_ parameter, which changes the `DisarmHA` signature question (OQ-1).
  Semantics per the apidoc: `freeze` = new commands/state changes not applied;
  `ignore` = resources removed from HA tracking while disarmed.
- `GET …/status/current`: **16 item fields**, not the design's twelve — `id`,
  `sid`, `node`, `type`, `state`, `status`, `crm_state`, `request_state`,
  `quorate` (boolean), `armed-state`, `auto-rebalance`, `failback`,
  `max_relocate`, `max_restart`, `resource_mode`, `timestamp`. `armed-state` is
  an enum **`armed` | `standby` | `disarming` | `disarmed`**; `resource_mode`
  mirrors the disarm enum.
- `GET …/status/manager_status`: returns a bare `{"type":"object"}` — no
  contract; the lossless pattern (OQ-2 decision) is the only honest model.
- `POST …/resources/{sid}/{migrate,relocate}`: synchronous, typed result —
  `sid`, `requested-node`, optional `blocking-resources` (`{sid, cause}`, cause
  enum `node-affinity` | `resource-affinity`), `comigrated-resources`.

## Implementation Phases

Each phase builds on the previous one. A phase is complete when all its tasks
are checked off and its success criteria are met.

---

### Phase 1: SDK surface, mockpve, unit tests

The whole code change, mock-verified — the working definition of "done" in this
environment.

#### Tasks

- [x] 1. `ArmHA(ctx) error` → real `POST /cluster/ha/status/arm-ha` (sync),
     gated on `s.caps.Require("HA arm/disarm", "9.2")` (the existing
     `HAClusterSwitch` capability, now load-bearing; gate fires before any
     request). The `ErrUnsupported` stub body is replaced; the signature is
     unchanged. _(Done 2026-07-22.)_
- [x] 2. `DisarmHA` → real `POST /cluster/ha/status/disarm-ha` (sync), same
     gate, with `resource-mode` per the OQ-1 signature decision. New
     `ResourceMode` string type + `ResourceModeFreeze`/`ResourceModeIgnore`
     constants (apidoc-confirmed enum). _(Done 2026-07-22: OQ-1a signature
     `DisarmHA(ctx, mode ResourceMode)`; empty mode refused client-side with
     `ErrMissingField`.)_
- [x] 3. `HAStatusCurrent(ctx) ([]HAStatusEntry, error)` — lossless read
     modelling all 16 confirmed fields (supersedes the design's twelve);
     `ArmedState` typed with the four-value enum constants (the observable for
     arm/disarm). Wire keys with hyphens (`armed-state`, `auto-rebalance`)
     handled in the JSON tags; `haStatusEntryKnownFields` kept in sync. _(Done
     2026-07-22.)_
- [x] 4. `GetManagerStatus(ctx) (*ManagerStatus, error)` — the house lossless
     pattern per OQ-2's decision: typed `MasterNode`/`Timestamp`/`NodeStatus`
     (node → state map)/`ServiceStatus` (sid → typed lossless entry) + the
     standard `Extra` tail. The apidoc pins nothing, so the typed fields are
     provisional until the Phase-3 live run confirms them (documented on the
     type). _(Done 2026-07-22: `ManagerServiceStatus` is itself lossless.)_
- [x] 5. `MigrateResource(ctx, sid, node)` / `RelocateResource(ctx, sid, node)`
     → `(*MigrateResult, error)` — synchronous CRM requests to
     `POST /cluster/ha/resources/{sid}/{migrate,relocate}` (`url.PathEscape` on
     sids); `MigrateResult` lossless with `SID`, `RequestedNode`,
     `BlockingResources []BlockingResource` (`Cause` typed to the
     `node-affinity`/`resource-affinity` enum), `ComigratedResources []string`.
     No version gate (baseline endpoints; the affinity-aware body is
     9.2-observed and hedged by lossless decode). Docs state convergence is
     observed via `HAStatusCurrent`, and the migrate-vs-relocate distinction.
     _(Done 2026-07-22: shared unexported `resourceAction` helper;
     `BlockingCause` enum constants.)_
- [x] 6. DLB reclassification: `GetDLBStatus`/`SetDLBConfig` keep their
     signatures but always return documented `pverr.ErrUnsupported` with docs
     pointing at `GetCRSSettings`/`SetCRSSettings`; no request is ever issued;
     the `DLBStatus`/`DLBConfig` types are retained (the `VolumeSnapshot`
     precedent). Remove `version.Capabilities.DynamicLoadBalancer()` (OQ-3
     decision b) and its tests; note the break in the changelog. _(Done
     2026-07-22: examples/docs that demonstrated the removed gate now use
     `HAClusterSwitch`.)_
- [x] 7. mockpve (`ha.go`): `haState` gains `armed` (default true) +
     `resourceMode`; arm/disarm handlers flip them (disarm rejects a missing
     `resource-mode` to mirror the required param); `/status/current`
     synthesizes one entry per seeded HA resource plus the manager row,
     reporting `armed-state` from the flag; `manager_status` returns a static
     plausible blob; migrate/relocate handlers echo `sid`/`requested-node` and
     move the resource's `current` entry to the target node (no affinity
     evaluation — the mock does not schedule). The fabricated `lbalancer` routes
     are **removed**. _(Done 2026-07-22: `New()` seeds
     `ha: haState{armed: true}`; disarm 400s on a missing/unknown resource-mode;
     one `handleHAResourceMove` serves migrate + relocate.)_
- [x] 8. Unit tests: gate refusal below 9.2 (arm + disarm); arm→disarm→arm
     observable through the mock's `current` (`armed-state` transitions); the
     disarm `resource-mode` wire form; migrate/relocate wire form + the
     `current`-entry node move; `TestDLBUnsupported` asserting
     `pverr.ErrUnsupported` **and zero HTTP traffic** (the
     `TestVolumeSnapshotsUnsupported` pattern); an in-package paths test pinning
     the literal `/cluster/ha/status/*` and `/cluster/ha/resources/{sid}/*`
     strings (the IMPL-0004 `TestFabricPathsReal` pattern). _(Done 2026-07-22:
     zero-HTTP proven with a nil transport — any request would panic. The paths
     pin surfaced a stale claim: `url.PathEscape` leaves `:` intact, so the wire
     path is `/resources/vm:100`, now pinned + comment corrected.)_
- [x] 9. Doc surfaces: `ha/doc.go` + interface comments reflect the upgraded ops
     and the DLB reclassification; `version` docs drop the removed gate;
     DEVELOPMENT.md's unverified list moves arm/disarm out of the "no confirmed
     endpoint" bucket; the integration live tests for Phase 3 are written now
     (env-gated, compile-verified via `go vet -tags=integration`) so the pvelab
     run needs no code changes. _(Done 2026-07-22: `TestHAStatusReads` +
     `TestHAArmDisarmCycle` (re-arms in cleanup) + `TestHAResourceMigrate`;
     TESTING.md/CLAUDE.md document the new `PVE_TEST_HA_ARM` opt-in.)_

#### Success Criteria

- `go build ./...`, `just lint` (0 issues), `just test` (race) green.
- The arm→disarm→arm cycle is observable end-to-end against mockpve, and the DLB
  ops provably issue no HTTP.
- `go doc ./proxmox/ha` renders the promoted docs cleanly.

---

### Phase 2: PR and merge

#### Tasks

- [x] 1. Branch per the OQ-4 sequencing decision; changelog regenerated as the
     branch's final commit. _(Done 2026-07-22:
     `feat/impl-0005-ha-remediation-delivery`, cut from post-#22 main — serial
     after the SDN merge per OQ-4a.)_
- [x] 2. Open the PR: `minor` label; changelog/description note the new surface,
     the DLB behavior reclassification (a would-be live 404 becomes a typed
     error), the `ha.API` interface growth (implementor-breaking,
     source-compatible for callers), and the removed
     `Capabilities.DynamicLoadBalancer()` (pre-v1 break). _(Done 2026-07-22: PR
     #23, `minor`, all breaks noted up front; a pre-PR review pass returned
     MERGEABLE and its findings were folded in.)_
- [ ] 3. CI fully green — no existing cassette touches this surface
     (`TestClusterAndHAReads` only lists resources), so
     `Test Replay (cassettes)` stays green; merge → the label auto-mints the
     next tag.

#### Success Criteria

- PR merged; next `v0.x` minor tag auto-minted and published.
- `just test-replay` green on post-merge `main`.

---

### Phase 3: Live verification and closure

On the shared pvelab run (IMPL-0004 Phase 3 / its OQ-3) — the ephemeral nested
cluster exists precisely so cluster-wide switches can be flipped with zero blast
radius (DESIGN-0004 OQ-1 decision a). All lab-touching steps are Donald's.

#### Tasks

- [ ] 1. `TestHAArmDisarmCycle` (gated per OQ-2): `HAStatusCurrent` baseline →
     `DisarmHA` → observe `armed-state` transition → `ArmHA` → observe again;
     verifies the `resource-mode` semantics live.
- [ ] 2. `TestHAStatusReads`: `current` + `manager_status` against the live
     cluster; **reconcile `ManagerStatus`'s provisional typed fields against the
     real blob before committing cassettes** (the design's stated source of
     truth for OQ-2's typed shape).
- [ ] 3. `TestHAResourceMigrate` (shape per OQ-3): migrate an HA resource
     between nested nodes, watch convergence via `HAStatusCurrent`; with a
     negative-affinity pair in place, assert a conflicting migrate returns
     `blocking-resources` with cause `resource-affinity`.
- [ ] 4. Scrub + commit the three cassettes; wire them into the
     `just test-replay` `-run` list; changelog-final; PR (label `patch` unless
     reconciliation changed public surface).
- [ ] 5. Closure: cassette `certification.yaml` batch entry (shared with
     IMPL-0004's run); IMPL-0001 Phase-4 margin note (arm/disarm + status +
     migrate live-verified); DESIGN-0004 status → Implemented.

#### Success Criteria

- The full disarm→observe→arm cycle passes live, and a blocked migrate returns
  `blocking-resources` with cause `resource-affinity`.
- All three cassettes replay green in CI.
- DESIGN-0004 status is Implemented with dated evidence in IMPL-0001.

## Open Questions

1. **`DisarmHA` signature — the apidoc marks `resource-mode` REQUIRED, but the
   design (written before the param mining) specified an optional variadic
   `DisarmHAOption`. Which wins?** **Decision (2026-07-21): a** — record the
   deviation in DESIGN-0004 as an implementation correction when it lands.
   - **a (recommended):** `DisarmHA(ctx, mode ResourceMode) error` — a required
     argument mirroring the wire contract. Freeze-vs-ignore is a real semantic
     choice (state preserved vs HA tracking dropped) the caller must make;
     recording the deviation in DESIGN-0004 follows the Implementation
     Corrections precedent. Live run double-checks whether PVE actually rejects
     the param's absence.
   - b: Keep the design's variadic option and default to `ResourceModeFreeze`
     when unset — conservative and source-stable with the design text, but it
     silently chooses semantics for the caller and papers over a required
     parameter.
   - c: Keep the variadic option, send nothing when unset, and let PVE reject it
     server-side — matches the design literally but ships a guaranteed footgun
     as the zero-argument call.

2. **Env gate for the live arm/disarm cycle (a cluster-wide switch)?**
   **Decision (2026-07-21): a.**
   - **a (recommended):** A new explicit opt-in, `PVE_TEST_HA_ARM=1` — the cycle
     only ever runs where someone deliberately set it (the pvelab env), and it
     can never ride along on an `r740a` read-only session that happens to have
     the other gates set.
   - b: Infer it from the placement gates (`PVE_TEST_PLACEMENT_VMID_1/2` present
     ⇒ quorate lab ⇒ cycle allowed). One less variable, but implicit
     blast-radius coupling between unrelated tests is how surprises happen.

3. **Shape of the live migrate test?** **Decision (2026-07-21): a.**
   - **a (recommended):** A standalone `TestHAResourceMigrate` that creates its
     own scratch VM pair + negative-affinity rule (reusing the placement test's
     helpers), asserts the blocked migrate and a successful one, and cleans up —
     self-contained, independently replayable cassette.
   - b: Extend `TestResourceAffinityPlacement` with the migrate assertions — one
     setup serves two criteria, but it conflates two ledger items in one
     cassette and re-records a test that is already certified.

4. **PR sequencing against IMPL-0004?** **Decision (2026-07-21): a.**
   - **a (recommended):** Serial — branch after the SDN PR merges. The two PRs
     share no code, but serial merging avoids the known changelog-conflict round
     on whichever lands second, and the shared pvelab run wants both merged
     anyway.
   - b: Parallel branches from `main`, fix the `CHANGELOG.md` conflict on the
     second merge (the documented stack-merge procedure). Saves days only if the
     SDN PR stalls.

## References

- DESIGN-0004 — the design this delivers (OQs decided 2026-07-21)
- INV-0004 — Findings 4 (no lbalancer path) and 5 (arm/disarm exist)
- Apidoc mining 2026-07-21 (this doc's Confirmed wire facts) —
  `cmd/pve-schemadiff/testdata/apidoc-9.2.js.gz`
- IMPL-0004 — the SDN remediation sharing the Phase-3 pvelab run
- `storage.VolumeSnapshots` reclassification — the honesty-rule precedent for
  the DLB change
- `proxmox/integration/testdata/cassettes/certification.yaml` — the per-run
  certification ledger
