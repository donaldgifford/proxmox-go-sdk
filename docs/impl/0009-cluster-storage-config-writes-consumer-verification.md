---
id: IMPL-0009
title: "Cluster storage config writes consumer verification"
status: Draft
author: Donald Gifford
created: 2026-08-26
---

<!-- markdownlint-disable-file MD025 MD041 -->

# IMPL-0009: Cluster storage config writes consumer verification

**Status:** Draft **Author:** Donald Gifford **Date:** 2026-08-26

<!--toc:start-->

- [Objective](#objective)
- [Scope](#scope)
  - [In Scope](#in-scope)
  - [Out of Scope](#out-of-scope)
- [Ground rules](#ground-rules)
- [Implementation Phases](#implementation-phases)
  - [Phase 1: hoomlab imports and converges (Donald-run)](#phase-1-hoomlab-imports-and-converges-donald-run)
    - [Tasks](#tasks)
    - [Success Criteria](#success-criteria)
  - [Phase 2: Findings fold-back and label flip](#phase-2-findings-fold-back-and-label-flip)
    - [Tasks](#tasks-1)
    - [Success Criteria](#success-criteria-1)
- [File Changes](#file-changes)
- [Testing Plan](#testing-plan)
- [Dependencies](#dependencies)
- [Open Questions](#open-questions)
- [References](#references)
<!--toc:end-->

## Objective

**Implements:** the consumer-verification half of DESIGN-0007 (OQ-6's amended
decision), split out of IMPL-0008 by its OQ-3 decision so that ledger could
complete on what this repo can build and ship alone.

Verify the storage config-write surface (`CreateDatastore` / `UpdateDatastore` /
`DeleteDatastore`, shipped mock-verified by IMPL-0008's release) against real
PVE **through hoomlab**, the consumer: hoomlab imports the released tag, builds
its `pve storage` converge stage against it, runs it against the production
cluster it manages, and every discrepancy it surfaces comes back here as a fix,
pinned by a mock test, shipped as a **patch release** (IMPL-0008 OQ-1's stated
posture). Closing this ledger flips the surface's label from **mock-verified**
to **consumer-exercised** — never "live-verified", which stays reserved for this
repo's own retired live suite.

## Scope

### In Scope

- The SDK side of everything hoomlab's converge runs surface: issues, fixes,
  mock tests pinning each fix, and the patch release(s) that carry them.
- The verification-status label flip (mock-verified → consumer-exercised) in
  every doc that states it: IMPL-0008, DESIGN-0007, `storage` doc comments that
  carry a verification caveat, CLAUDE.md's verification paragraph.
- Recording the converge evidence (dates, cluster, SDK tag, what converged) in
  this ledger — the deferral-era substitute for a `certification.yaml` cassette
  entry.

### Out of Scope

- **hoomlab's stage implementation** — `bootstrap.hcl` storage blocks,
  cross-validation, drift policy, and the converge loop are hoomlab-repo work;
  this ledger tracks only what its runs teach the SDK.
- **Any live run from this repo** — the deferral (DESIGN-0007 OQ-6) is not
  reopened here: no cassette, no `just test-replay` wiring, and the prepared
  `TestDatastoreLifecycle` harness stays never-run. Ending the deferral is its
  own future decision.
- **New SDK surface.** Fixes only. If hoomlab needs an op or field the design
  did not model, that is a new issue → design amendment → its own ledger, not
  scope creep here.

## Ground rules

- **This ledger is expected to idle.** It cannot start until hoomlab's stage
  exists, and IMPL-0008's release deliberately does not wait for it (OQ-1a).
  Draft until hoomlab begins; In Progress from the first import; nothing here
  has a deadline coupled to this repo.
- **The fix channel is a patch release** — stated in IMPL-0008's release notes.
  A finding that genuinely needs new surface (not a fix) escalates out of this
  ledger instead of stretching "patch".
- **Every fix lands with a mock test** reproducing what PVE actually did, so
  mock and SDK cannot silently agree with each other against PVE again — the
  `UpdateACMEAccount` failure class this repo already paid for once.
- **Attribution stays with the consumer.** hoomlab knows which entry it expected
  to create or drift-correct; SDK-side triage starts from its report, not from
  re-deriving state on the cluster. Donald runs everything that touches the
  production cluster.

## Implementation Phases

Each phase builds on the previous one. A phase is complete when all its tasks
are checked off and its success criteria are met.

---

### Phase 1: hoomlab imports and converges (Donald-run)

The consumer picks the surface up and runs the two shapes issue #28 was filed
for. This phase lives mostly in hoomlab's repo; the tasks here are the
SDK-facing checkpoints.

> **Head start (2026-08-27):** this loop began early — hoomlab built its stage
> against the IMPL-0008 PR branch (go.work) and ran live converges against the
> production cluster BEFORE the merge, so the first findings landed as PR #30
> fixes instead of a patch release: the missing-id `GET /storage/{id}` 500 wart
> (hoomlab INV-0001 deviation 8; existence checks now scan `ListDatastores`) and
> zfspool `mountpoint` materialization (reads carry server-generated keys;
> compare fields, not maps). Write paths passed live: create zfspool, partial
> update with index-read digest, zero-drift re-run. Phase 1 proper picks up from
> the **v0.12.0** tag import — the tasks below track the released-tag evidence,
> which the pre-release run does not replace.

#### Tasks

- [ ] 1. hoomlab imports the IMPL-0008 release tag (or, while iterating, a local
     `go.mod replace` on this repo's main) and builds its `pve storage` stage
     against the surface: converge loop over `ListDatastores`/`GetDatastore` +
     the three writes, comparing `Content`/`Nodes` **as sets** (the documented
     contract) and threading `Datastore.Digest` into `DatastoreUpdate.Digest`
     for read-modify-write safety.
- [ ] 2. First converge against the production cluster (hoomlab-run): create the
     `fast/vm` + `tank/vm` zfspool entries (node-restricted, `sparse`) and
     restrict the stock `local-zfs` entry — the two shapes issue #28 names.
     Record here: date, SDK tag, cluster PVE version, and which entries were
     created vs drift-corrected.
- [ ] 3. Idempotency check: a second converge immediately after the first is a
     no-op (zero writes issued). This is the strongest single signal that the
     SDK's read-back and the consumer's diff agree about what PVE stored —
     order-dependence, digest churn, or read-shape drift all break it.

#### Success Criteria

- hoomlab's `pve storage` stage converges green against the production cluster:
  both issue-#28 shapes exist and hold.
- The re-run is a no-op — convergence is stable, not oscillating.
- Every anomaly hit along the way is either explained in place or captured as a
  Phase 2 finding; nothing is worked around silently in hoomlab.

---

### Phase 2: Findings fold-back and label flip

What the runs taught the SDK, made permanent — then the honest relabel and
closure.

#### Tasks

- [ ] 1. Each hoomlab-surfaced discrepancy becomes an issue here, fixed on the
     SDK/mock side with a mock test pinning PVE's observed behaviour. The
     classes the mock could not prove, worth explicit checks even if no failure
     forces them: the PUT return shape actually populated
     (`DatastoreWriteResult` on update), digest semantics under concurrent
     edits, PVE's real `nodes`/`content` normalization versus the mock's
     sorted-set model, and the create-fixed rejection's real status and message.
     Confirmations (no fix needed) get a dated note here instead of a change.
- [ ] 2. Fixes ship as **patch release(s)**; hoomlab bumps to the patched tag
     and drops any `replace`. Repeat Phase 1's idempotency check on the final
     tag if any fix touched read-back or encoding.
- [ ] 3. Label flip: mock-verified → **consumer-exercised** everywhere the
     status is stated — IMPL-0008, DESIGN-0007 (status stays Implemented; the
     verification note updates), any `storage` doc-comment caveats, and
     CLAUDE.md's verification paragraph. Never "live-verified".
- [ ] 4. Ledger closure: converge evidence recorded (Phase 1 task 2 + the final
     tag), IMPL-0009 → Completed (docs ride a `dont-release` PR or the next
     release PR).

#### Success Criteria

- Zero known mock-and-SDK-agree-but-PVE-disagrees gaps on the storage write
  surface: every finding is fixed-and-pinned or recorded as confirmed.
- hoomlab runs a released tag (no `replace`), and its converge is green and
  idempotent on that tag.
- The label reads consumer-exercised — not live-verified — in every place that
  states it, and this ledger holds the dated evidence.

---

## File Changes

| File                                  | Action | Description                                             |
| ------------------------------------- | ------ | ------------------------------------------------------- |
| `proxmox/storage/*` + `mockpve`       | Modify | Only as findings demand; each fix carries its mock test |
| `docs/impl/0008-…-delivery.md`        | Modify | Label flip note (mock-verified → consumer-exercised)    |
| `docs/design/0007-…-config-writes.md` | Modify | Verification note update                                |
| `CLAUDE.md`                           | Modify | Verification paragraph: the storage-writes label        |
| this ledger                           | Modify | Converge evidence, findings log, closure                |

## Testing Plan

- The production converge runs are the test — hoomlab-run, hoomlab-attributed
  (Donald executes anything touching the cluster).
- Every SDK-side fix is regression-pinned by a mock test before it ships.
- The idempotent-re-run check is the acceptance test for the surface as a whole,
  on the first tag and again on the final one if fixes intervened.
- No cassettes, no replay, no tagged-suite runs from this repo (the deferral).

## Dependencies

- IMPL-0008 Completed and its `minor` release minted — the tag hoomlab imports.
- hoomlab's `pve storage` stage built (hoomlab IMPL-0002 Phase 3, in hoomlab's
  repo) and the production cluster it already manages.
- Donald's availability to run the converges — nothing here is agent-runnable
  against the cluster.

## Open Questions

None. The shape was decided upstream: DESIGN-0007 OQ-6 (the deferral and the
consumer-as-verifier posture) and IMPL-0008 OQs 1a (release first, verify via
patch release), 2a, and 3b (this split). If hoomlab's runs surface a question
the decisions do not cover — e.g. a needed op the design never modelled — it
becomes an issue and, if big enough, its own design doc rather than an OQ here.

## References

- IMPL-0008 — the delivery ledger this was split from (its OQ-3 decision created
  this doc; its OQ-1 decision defines the patch-release channel).
- DESIGN-0007 — Cluster storage config writes (OQ-6: the deferral and
  consumer-verification posture this executes).
- [GitHub issue #28](https://github.com/donaldgifford/proxmox-go-sdk/issues/28)
  — the consumer and the two converge shapes.
- IMPL-0007 "Follow-up: move the destructive integration tests off r740a" — the
  posture's origin and its 2026-08-26 update.
- TESTING.md "Posture change (2026-08-26)" — why no run in this repo.
