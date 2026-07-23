---
id: IMPL-0004
title: "SDN fabrics remediation delivery"
status: In Progress
author: Donald Gifford
created: 2026-07-21
---

<!-- markdownlint-disable-file MD025 MD041 -->

# IMPL 0004: SDN fabrics remediation delivery

**Status:** In Progress **Author:** Donald Gifford **Date:** 2026-07-21 (OQs
decided 2026-07-21: all a)

<!--toc:start-->

- [Objective](#objective)
- [Scope](#scope)
  - [In Scope](#in-scope)
  - [Out of Scope](#out-of-scope)
- [Implementation Phases](#implementation-phases)
  - [Phase 1: Land the SDK implementation](#phase-1-land-the-sdk-implementation)
    - [Tasks](#tasks)
    - [Success Criteria](#success-criteria)
  - [Phase 2: Replay-green and merge](#phase-2-replay-green-and-merge)
    - [Tasks](#tasks-1)
    - [Success Criteria](#success-criteria-1)
  - [Phase 3: Live verification and closure](#phase-3-live-verification-and-closure)
    - [Tasks](#tasks-2)
    - [Success Criteria](#success-criteria-2)
- [Open Questions](#open-questions)
- [References](#references)
<!--toc:end-->

## Objective

Deliver DESIGN-0003 to `main` and close it out: SDN fabric CRUD on the **real
nested paths** (`/cluster/sdn/fabrics/fabric[/{id}]`), the **fabric node
membership** sub-collection, and the **eight node-scoped live-status reads**
replacing the `ErrUnsupported` stubs — mock-verified in the PR, then
live-verified on a pvelab nested cluster with cassettes, certification entry,
and ledger notes.

**Implements:** DESIGN-0003 (including its Implementation Corrections section).

**Starting position (be honest about it):** a complete, `just lint`/`just test`
(race)-green implementation of DESIGN-0003 already exists on the parked branch
`feat/sdn-fabrics-remediation` (PR #19, closed unmerged 2026-07-21 — it jumped
the gun on the go-ahead, not on quality). Every wire shape in it was mined from
the committed real 9.2 apidoc (`cmd/pve-schemadiff/testdata/apidoc-9.2.js.gz`),
not guessed. This document tracks _delivering_ that work, not rewriting it — see
OQ-1.

## Scope

### In Scope

- Reviving the parked branch, rebasing onto current `main`, and re-verifying the
  quality gates.
- The `minor` PR with the BREAKING `sdn.API` note (status-read signatures
  changed; `Fabric` lost `Nodes`/`Comment`).
- Re-recording the `TestNetworkReads` cassette against `r740a` (the committed
  cassette holds the old flat-path fabrics interaction, so the replay CI job is
  red until then — see OQ-2).
- The pvelab live run for `TestSDNFabricLifecycle` + `TestSDNStatusReads`,
  cassette capture/scrub/commit, replay wiring, `certification.yaml` batch
  entry, and doc closure (IMPL-0001 Phase-5 note → live-verified, DESIGN-0003 →
  Implemented).

### Out of Scope

- DESIGN-0004 (HA remediation) — its own IMPL-0005; the two share one pvelab
  live run (see OQ-3).
- DESIGN-0005 (coverage tracker) — blocked until both remediations merge
  (DESIGN-0005 OQ-4).
- SDN transactions and the `pending`/`running` fabric filters — deferred
  post-ship (DESIGN-0003 OQ-5/6).
- The remaining SDN gap families (controllers, DNS, IPAMs, prefix-lists,
  route-maps, vnet-firewall) — group-5 triage.

## Implementation Phases

Each phase builds on the previous one. A phase is complete when all its tasks
are checked off and its success criteria are met.

---

### Phase 1: Land the SDK implementation

Bring the parked implementation back to a mergeable state on top of current
`main` and re-open the PR.

#### Tasks

- [x] 1. Rebase `feat/sdn-fabrics-remediation` onto `main` (per OQ-1). The only
     expected conflict is `CHANGELOG.md` — resolve by regenerating
     (`git fetch --tags origin` first, then `git-cliff -o CHANGELOG.md`; the
     tags fetch is load-bearing, see the PR-CI gotchas), keeping
     `chore(changelog): Auto-sync` as the branch's final commit. _(Done
     2026-07-21: delivered as the OQ-1a revival with 1b's mechanics — Donald
     asked for a fresh branch `feat/impl-0004-sdn-fabrics-delivery`, so the
     three content commits were cherry-picked onto post-#20 `main` (zero
     conflicts; the stale changelog commit was skipped and regenerates fresh as
     the final commit). Identical content, tidier history.)_
- [x] 2. Re-run the gates: `just fmt`, `just lint` (0 issues), `just test`
     (race), `go vet -tags=integration ./proxmox/integration/`. Confirm the
     branch content still matches DESIGN-0003 + its Implementation Corrections
     (nested fabric paths; fabric-node sub-collection with property-string
     `interfaces` and bare-IPv4 `ip`; the eight status reads with
     `ports`/`nexthops`/`via` in `Extra`; mockpve mirroring only real routes;
     `TestFabricPathsReal`/`TestNodeSDNStatusPaths` pinning the literal paths).
     _(Done 2026-07-21: all four gates green on the revived branch; grep
     confirms the nested `fabrics/fabric`/`fabrics/node` paths, both
     path-pinning tests, and zero flat-path remnants.)_
- [x] 3. Open the PR: `minor` label, BREAKING interface note in the description
     and changelog (`SDNStatus`/`VNetStatus` signature changes, the `VNetStatus`
     _method_ replaced by `ZoneContent`/`VNetMACVRF`, `Fabric` field removals),
     and the Phase-2 cassette caveat stated up front. _(Done 2026-07-21: PR #21
     open with `minor`; CI landed exactly as predicted — every job green except
     `Test Replay (cassettes)`, whose log shows the expected stale-cassette 404
     on `…/cluster/sdn/fabrics/fabric`. Phase 1 complete.)_

#### Success Criteria

- PR open with `minor` label; every CI job green **except** the known-red
  `Test Replay (cassettes)` job (the stale `TestNetworkReads` cassette — Phase
  2's job to fix).
- `just lint` + `just test` green locally on the rebased branch.

---

### Phase 2: Replay-green and merge

Fix the one honest casualty of the re-path — the committed `TestNetworkReads`
cassette recorded the old flat fabrics path (its "passing" live read was
actually decoding the subdir index) — then merge.

#### Tasks

- [x] 1. Re-record `TestNetworkReads` against `r740a` (Donald; reads-only, no
     pvelab, no destructive gates needed):
     `PVE_RECORD=1 go test -tags=integration -run 'TestNetworkReads' ./proxmox/integration/`
     with the usual `.env.local` environment. _(Done 2026-07-22: recorded via
     `op run --env-file=.env` — first attempt used the stale `.pvelab.env` and
     dialed a torn-down nested-lab address; the r740a token env is the right
     one. Four interactions; the fabrics read now hits the nested
     `…/fabrics/fabric` path and honestly returns the empty list.)_
- [x] 2. Leak-review the new cassette (credentials redacted to `REDACTED`,
     endpoint/node rewritten to the `pve.example`/`pve` placeholders) and commit
     it on the PR branch, changelog re-synced as the final commit. _(Done
     2026-07-22: scan clean — all hosts `pve.example`, Authorization `REDACTED`,
     no real IPs/hostnames; full `just test-replay` green locally before
     commit.)_
- [x] 3. All CI jobs green including `Test Replay (cassettes)`; merge. The
     `minor` label auto-mints the next tag — no manual tagging. _(Done
     2026-07-22: PR #21 merged with every job green; the release workflow minted
     and published `v0.7.0` (10 assets). `just test-replay` re-verified green on
     post-merge `main`. Superseded branch `feat/sdn-fabrics-remediation`
     deleted. Phase 2 complete — Phase 3 (the shared pvelab live run with
     IMPL-0005) is the remaining work.)_

#### Success Criteria

- PR merged; the release workflow mints and publishes the next `v0.x` minor tag.
- `just test-replay` green on post-merge `main`.

---

### Phase 3: Live verification and closure

The design's live criterion on an ephemeral pvelab nested cluster — shared with
IMPL-0005 Phase 3 (see OQ-3). All lab-touching steps are Donald's.

#### Tasks

- [x] 1. Bring up the pvelab cluster (`just dogfood-up`, clone path) and source
     `.pvelab.env`; set the fabric gates (`PVE_TEST_FABRIC_NODES` = the ≥2 lab
     node names, `PVE_TEST_FABRIC_IFACE` per OQ-4). _(Done 2026-07-23: clone
     path, quorate(3) — after restoring the `template:` block in pvelab.yaml;
     the ISO path is currently unusable because the baked-in answer-server
     address predates the workstation's move off the lab subnet.)_
- [x] 2. Run `TestSDNStatusReads` (read-only zone status) and
     `TestSDNFabricLifecycle` (OpenFabric fabric across the lab nodes → enroll
     each node → `ApplySDN` → poll `FabricNeighbors` until FRR converges → read
     interfaces/routes → teardown) with `PVE_RECORD=1`. _(Done 2026-07-23, both
     PASSED. The first fabric attempt enrolled the raw guest NIC (`ens18`) and
     burned the 3-minute ceiling at 0 interfaces — openfabric never binds an
     address-less bridge-enslaved port. Re-run enrolling the addressed bridge
     (`PVE_TEST_FABRIC_IFACE=vmbr0`) converged in ~10s (neighbor
     `0100.9909.9002`, Initializing) and tore down clean; the failed-run
     cassette was overwritten by the passing re-record. The mgmt-bridge
     enrollment perturbation never materialized.)_
- [x] 3. Reconcile any live divergence into the SDK/mock **before** committing
     cassettes — the known watch-list: fabric-node `ip` bare-IPv4 acceptance,
     the property-string `interfaces` form, whether `redistribute`'s wire form
     justifies promoting it out of `Extra` (DESIGN-0003 Correction 2), and the
     status-read field contents against the mock's synthesis. _(2026-07-23:
     bare-IPv4 `ip` and the property-string `interfaces` form both ACCEPTED live
     — no divergence, no SDK change needed. `redistribute` was not exercised
     (stays in `Extra`). Status-read shapes matched the mock for the zone reads;
     the fabric runtime reads returned empty pending the convergence fix, so
     their field contents remain the only open item.)_
- [x] 4. Scrub + commit the two cassettes; add both tests to the
     `just test-replay` `-run` list; changelog-final; PR (label `patch` unless
     reconciliation changed public surface). _(Done 2026-07-23 on
     `feat/phase3-live-reconcile`, shared with IMPL-0005's Phase-3 closure —
     label `minor` (the HA reconciliation changed public surface). Leak review
     found + fixed a real harness gap: `topologyScrub` rewrote the raw request
     body but not go-vcr's parsed `Form` map (pinned in `TestScrubTopology`).
     Full 16-test `just test-replay` green locally.)_
- [ ] 5. Closure: cassette `certification.yaml` batch entry for the run;
     IMPL-0001 Phase-5 status note updated (fabric lifecycle + status reads →
     live-verified); DESIGN-0003 status → Implemented; tear down the lab
     (`just dogfood-down`), `r740a` clean check. _(Doc closure done 2026-07-23 —
     9.2.2 batch entry (plus a retroactive 9.2.4 entry for the Phase-2
     `TestNetworkReads` re-record), IMPL-0001 Phase-5 live-verified note,
     DESIGN-0003 → Implemented. Remaining: the lab teardown + `r740a` clean
     check (Donald), after the closure PR merges.)_

#### Success Criteria

- `TestSDNFabricLifecycle` green live: fabric created, all lab nodes enrolled,
  FRR neighbors observed (convergence within the 3-minute ceiling), teardown
  leaves no SDN config behind.
- Both new cassettes replay green in CI.
- DESIGN-0003 status is Implemented and IMPL-0001's Phase-5 note records the
  live evidence with dates.

## Open Questions

1. **Where does the implementation come from?** **Decision (2026-07-21): a.**
   - **a (recommended):** Revive the parked `feat/sdn-fabrics-remediation`
     branch (rebase + re-verify). The work is complete, green, and apidoc-mined;
     re-implementing rediscovers nothing and re-risks transcription errors. The
     rebase is cheap (only `CHANGELOG.md` conflicts).
   - b: Cherry-pick the branch's commits selectively onto a fresh branch — same
     content, tidier history, slightly more ceremony for no behavioural
     difference.
   - c: Re-implement from scratch following DESIGN-0003. Maximum process purity,
     zero new information, real regression risk.

2. **When is the `TestNetworkReads` cassette re-recorded?** **Decision
   (2026-07-21): a.**
   - **a (recommended):** Pre-merge, on the PR branch (Phase 2 as written). The
     replay job stays authoritative — the PR that breaks a cassette ships its
     replacement, and `main` is never replay-red.
   - b: Temporarily drop `TestNetworkReads` from the `just test-replay` `-run`
     list in the PR, merge, and re-record it with the Phase-3 cassette batch.
     One less `r740a` touch, but it weakens the guard exactly when the SDN
     surface is changing and leaves a window where fabrics reads have no replay
     coverage.

3. **One pvelab run or two?** **Decision (2026-07-21): a.**
   - **a (recommended):** One shared clone-up after both IMPL-0004 and IMPL-0005
     merge — both designs already assume the shared run, one lab cycle (~3 min
     up via clones) covers fabric + HA + migrate, and one certification batch
     entry describes it.
   - b: Separate runs per IMPL. Cleaner attribution per PR, double the lab
     cycles and `r740a` sessions for no verification gain.

4. **Which interface do fabric nodes enroll (`PVE_TEST_FABRIC_IFACE`)?**
   **Decision (2026-07-21): a.**
   - **a (recommended):** The nested nodes' existing management interface,
     first. OpenFabric runs FRR hellos over the interface without re-addressing
     it, the lab is disposable (worst case: teardown and retry), and it needs
     zero pvelab changes. If the live run shows the management path perturbed,
     fall back to (b).
   - b: Extend pvelab to give the nested clones a second NIC (touches
     `cmd/pvelab` provisioning + the template build from IMPL-0002 Phase 5) and
     enroll that. Cleaner isolation, but real harness work spent hedging a risk
     the ephemeral lab already absorbs.

## References

- DESIGN-0003 — the design this delivers (incl. Implementation Corrections,
  2026-07-21)
- INV-0004 — Findings 3 (fabrics paths) and 6 (node-scoped SDN status)
- Parked branch `feat/sdn-fabrics-remediation` / closed PR #19 — the existing
  implementation (park rationale in the PR close comment)
- PR #20 — the docz decisions this plan executes on
- IMPL-0005 — the HA remediation sharing the Phase-3 pvelab run
- `proxmox/integration/testdata/cassettes/certification.yaml` — the per-run
  certification ledger
