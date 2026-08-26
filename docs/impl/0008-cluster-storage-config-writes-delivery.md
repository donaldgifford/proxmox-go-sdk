---
id: IMPL-0008
title: "Cluster storage config writes delivery"
status: Draft
author: Donald Gifford
created: 2026-08-26
---

<!-- markdownlint-disable-file MD025 MD041 -->

# IMPL-0008: Cluster storage config writes delivery

**Status:** Draft **Author:** Donald Gifford **Date:** 2026-08-26

<!--toc:start-->

- [Objective](#objective)
- [Scope](#scope)
  - [In Scope](#in-scope)
  - [Out of Scope](#out-of-scope)
- [Ground facts](#ground-facts)
- [Implementation Phases](#implementation-phases)
  - [Phase 1: Write types, digest on the read type, wire encoding](#phase-1-write-types-digest-on-the-read-type-wire-encoding)
    - [Tasks](#tasks)
    - [Success Criteria](#success-criteria)
  - [Phase 2: mockpve write support + service methods](#phase-2-mockpve-write-support--service-methods)
    - [Tasks](#tasks-1)
    - [Success Criteria](#success-criteria-1)
  - [Phase 3: Paths, coverage, prepared harness, docs, PR](#phase-3-paths-coverage-prepared-harness-docs-pr)
    - [Tasks](#tasks-2)
    - [Success Criteria](#success-criteria-2)
  - [Phase 4: Consumer verification (hoomlab, Donald-run)](#phase-4-consumer-verification-hoomlab-donald-run)
    - [Tasks](#tasks-3)
    - [Success Criteria](#success-criteria-3)
- [File Changes](#file-changes)
- [Testing Plan](#testing-plan)
- [Dependencies](#dependencies)
- [Open Questions](#open-questions)
- [References](#references)
<!--toc:end-->

## Objective

**Implements:** DESIGN-0007 (Approved 2026-08-26; OQs 1–5 and 7 decided `a`,
OQ-6 amended to the live-verification deferral). Closes GitHub issue #28.

Deliver the write half of PVE's cluster storage configuration API on
`storage.Service` — `CreateDatastore` (`POST /storage`), `UpdateDatastore`
(`PUT /storage/{storage}`), `DeleteDatastore` (`DELETE /storage/{storage}`) —
with mockpve serving all three, so `hoomlab`'s `pve storage` converge stage
(create-if-missing, update-if-drifted) can be built and tested against the SDK.
Everything ships **mock-verified**; live evidence arrives through hoomlab as the
consumer (DESIGN-0007 OQ-6), never through this repo's own suite while the
deferral holds.

## Scope

### In Scope

- `DatastoreSpec` / `DatastoreUpdate` pointer write specs (OQ-1a) with
  `Content`/`Nodes`/`Delete` as `[]string` comma-joined post-encode (OQ-3a),
  typed `Sparse`/`Blocksize` for the zfspool consumer case (OQ-2a), and the
  `Extra` escape hatch.
- `DatastoreWriteResult` returned by **both** create and update (OQ-4a) —
  `{storage, type, config?}`, `config` carrying server-generated properties (the
  schema-named `encryption-key`).
- `Datastore.Digest` on the read type (+ `datastoreKnownFields`), feeding the
  update's digest guard.
- The three service methods + `storage.API` interface growth.
- mockpve: three routes, `storeRecord` growth, digest guard (both directions),
  create-fixed-key rejection, **set-normalized** `nodes`/ `content` (OQ-5a),
  digest bump per write.
- Coverage regen (3 flips, 258 → 261 of 675), `TestStoragePathsReal` (new file —
  storage has no `paths_test.go` yet), doc promotion +
  `Example_datastoreConfig`.
- The **prepared-but-never-run** `TestDatastoreLifecycle` integration harness
  behind `PVE_TEST_DATASTORE=1` (the `TestACMEDNSNamecheap` mould), and its
  TESTING.md section.

### Out of Scope

- **Any live run from this repo** — no cassette, no `just test-replay` wiring,
  no recording (DESIGN-0007 OQ-6: r740a is production, pvelab rides it). Ending
  the deferral is a future env-file change, not part of this ledger.
- **A convergence helper** (`EnsureDatastore`) — OQ-7a: primitives only; drift
  policy is hoomlab's stage logic.
- **Typed fields beyond the OQ-2a set** — the other ~50 per-type params ride
  `Extra`; added on demand.
- **mockpve permission enforcement** — filed separately with the drill's parity
  findings (issue #28).
- **hoomlab's own stage code** — Phase 4 exercises it, but building
  `bootstrap.hcl` storage blocks, cross-validation, and the converge loop is
  hoomlab work in hoomlab's repo.

## Ground facts

Checked against the tree at branch time (2026-08-26):

- `proxmox/storage/datastore.go` (45 lines) holds the four read methods;
  `types.go` holds `Datastore` + `datastoreKnownFields` + the lossless
  `UnmarshalJSON`; `paths.go` has `datastoresPath()`/`datastorePath(storage)`
  (no escaping — a `pve-storage-id` is colon-free, unlike volids/SIDs).
- `storage.API` is published with `var _ API = (*Service)(nil)`
  (`service.go:69`); growing it is a pre-v1 break for external doubles only,
  same class as the `DoUpload`/`DoWebSocket`/`UpdateACMEAccount` precedents.
- `mockpve/storage.go`:
  `storeRecord{Storage, Type, Content, Path, Pool, Shared, Total, Used}` — no
  `Nodes`, `Disable`, `Digest`, or extras;
  `AddStorage(id, type, content, total, used)` seeds it; `handleDatastoreGet`
  404s on a missing id (`msgNoSuchStorage`), which the new update/delete
  handlers reuse.
- `docs/COVERAGE.md`: 258/675 (38.2%); `POST /storage`, `PUT /storage/{}`,
  `DELETE /storage/{}` are the family's only `gap` rows. No annotation changes
  needed (no stubs, nothing out of scope).
- The wire facts (61/51-param asymmetry, twelve create-fixed keys, object
  returns on POST **and** PUT, sync-null DELETE, `Datastore.Allocate`
  everywhere, live-confirmed `digest` on reads) are pinned in DESIGN-0007's
  "Wire facts" section — the implementation cites, not re-derives, them.
- `proxmox/storage` has **no** `paths_test.go`; the `TestXxxPathsReal` pattern
  to copy lives in `ceph`/`ha`/`nodes`/`sdn`.
- Verification posture: live runs are deferred repo-wide. The working definition
  of "done" for a task here is: typed op exists, `go build ./...` clean,
  unit-tested against mockpve, `just lint` + `just test` green.

## Implementation Phases

Each phase builds on the previous one. A phase is complete when all its tasks
are checked off and its success criteria are met.

---

### Phase 1: Write types, digest on the read type, wire encoding

The pure-SDK half that needs no mock: the two specs, the result type, the digest
field, and the encoding that turns a spec into PVE's form body. Pinning the wire
form first means Phase 2's handler tests exercise handlers, not encoding bugs.

#### Tasks

- [ ] 1. `Datastore.Digest` in `proxmox/storage/types.go`: add the field
     (`json:"digest,omitempty"`), add `"digest"` to `datastoreKnownFields`. Unit
     tests: a read carrying `digest` lands it in the typed field and **not** in
     `Extra` (assert both), and a read without one leaves it empty. Doc comment
     teaches the read-then-guarded-write idiom (mirror `ACMEPlugin.Digest`'s
     wording). Note for Phase 3's changelog task: a consumer reading
     `Extra["digest"]` today loses it to the typed field.
- [ ] 2. Write types in `proxmox/storage/datastore.go`: `DatastoreSpec`,
     `DatastoreUpdate`, `DatastoreWriteResult` exactly as DESIGN-0007's "Write
     specs" / "The write result" sections define them (required
     `Storage`+`Type`; `[]string` list fields `json:"-"`; update's pointer
     booleans; `Delete`/`Digest` on the update only; `Config map[string]string`
     decoding tolerant of non-string values the way `Extra` reads do). Doc
     comments carry: the create-fixed key list on the update type ("PVE rejects
     these here — recreate instead"), the explicit-delete contract, the digest
     idiom, the one-shot `encryption-key` warning on `Config`, and the
     `Extra`-sensitivity note per OQ-2 of this ledger (a CIFS/PBS `password`
     rides `Extra`; the SDK never logs bodies, but the consumer owns what it
     prints).
- [ ] 3. Encoding: `svcutil.EncodeWithExtra` + post-encode comma-joins for
     `Content`/`Nodes`/`Delete` (the ZFS-`Devices`/HA-rules/ACME-`Nodes`
     mechanics). Table-driven wire-form tests, no HTTP: the hoomlab zfspool spec
     renders exactly (`storage`, `type`, `pool`, `sparse=1`, `blocksize`,
     `content` joined, `nodes` joined); a zero `DatastoreUpdate` encodes to an
     empty body; only-set-fields on update (unset pointer booleans absent,
     `false`-and-set present as `0`); `delete`+`digest` render; an `Extra` key
     wins over its typed field (the EncodeWithExtra contract, asserted here so
     the spec docs stay honest).

#### Success Criteria

- `go build ./...`, `just lint`, `just test` (race) green.
- The zfspool wire form is pinned by a table test byte-for-byte, and the
  zero-update-sends-nothing property holds — Phase 2 debugging starts from
  known-good encoding.
- `digest` demonstrably moved: one test proves typed-field presence AND `Extra`
  absence on the same read.

---

### Phase 2: mockpve write support + service methods

The functional core: the three routes on the mock, the three methods on the
service, and the unit matrix that runs every op against the mock. Mock and SDK
land in one phase because the repo convention is service tests against mockpve —
neither half is testable alone.

#### Tasks

- [ ] 1. mockpve state (`proxmox/mockpve/storage.go`): `storeRecord` grows
     `Nodes`, `Disable`, `Digest`, and `Extra map[string]string` for
     submitted-but-unmodelled keys (`sparse`, `blocksize`, … — the mock needs
     faithful read-back, not typed fields); `datastoreToPayload` emits them
     (map-shaped JSON, matching real PVE's flat object); `AddStorage` keeps its
     signature and seeds a digest so existing seeded tests read one.
- [ ] 2. mockpve handlers: `POST /storage` (missing `storage`/`type` → 400;
     duplicate id → 400 "storage ID '…' already defined"; store the form; answer
     `{storage, type}` — **no fabricated `config`**, the mock supports no
     auto-generating type and must not teach consumers to expect one),
     `PUT /storage/{storage}` (unknown id → 404 reusing `msgNoSuchStorage`; the
     twelve create-fixed keys → 400; `delete` applied before set-keys — the
     `applyConfigForm` contract; **digest guard**: stale → 400, fresh or absent
     → accepted), `DELETE /storage/{storage}` (unknown id → 404; removes the
     record; null data). Every write **bumps the stored digest**;
     `nodes`/`content` are parsed to sets and emitted **sorted** (OQ-5a), with
     the mock's doc comment stating the rule: list-valued options are sets,
     compare them as sets.
- [ ] 3. Service methods in `proxmox/storage/datastore.go`:
     `CreateDatastore`/`UpdateDatastore` → `*DatastoreWriteResult`,
     `DeleteDatastore` → `error`, per DESIGN-0007's signatures — nil-spec /
     empty-id guards first (`svcutil.ErrNilSpec`/`ErrMissingField`), no version
     gate (9.0 baseline). Doc comments carry the `Datastore.Allocate` permission
     note (ordinary privilege, tokens work) and delete's config-not-data
     semantics. `storage.API` grows the three methods with a changelog note
     (pre-v1 break for external doubles only).
- [ ] 4. Unit matrix (beside the code, against the mock): create → list/get
     reflects the write including `Extra` keys; duplicate create rejected;
     update applies set-keys, honours `delete`, refuses stale digest AND accepts
     fresh (both directions); create-fixed key on update rejected; delete →
     subsequent get 404s → `pverr.ErrNotFound` via `errors.Is`;
     set-normalization (submit `nodes=b,a` → read `a,b`); `Extra` round-trip for
     an unmodelled key (`preallocation`); result decode with and without
     `config`; digest changes across writes (guard-testable-without-race
     property).
- [ ] 5. `TestDatastoreConvergeShape`: hoomlab's exact sequence against the mock
     — get(miss → `ErrNotFound`) → create zfspool (pool, sparse, content, nodes)
     → get(hit; compare `Content`/`Nodes` **as sets**) → drift-correct via
     update (content restriction) with the read's digest → delete. This is the
     seeding-not-stubbing proof: the consumer's converge logic can run against
     `mockpve` unmodified.

#### Success Criteria

- `go build ./...`, `just lint`, `just test` (race) green.
- The full unit matrix passes; the digest guard is covered in both directions;
  set-normalization is asserted with an unsorted submission.
- `TestDatastoreConvergeShape` passes end-to-end — the mock can host the
  consumer's loop before the consumer exists.

---

### Phase 3: Paths, coverage, prepared harness, docs, PR

Hardening and shipping: the guards that keep the paths honest, the harness that
ends the deferral cheaply later, and the PR.

#### Tasks

- [ ] 1. Create `proxmox/storage/paths_test.go` with `TestStoragePathsReal` (the
     `ceph`/`ha`/`nodes`/`sdn` pattern): pin the three write paths and — since
     the file is being created anyway — the existing read/content/ upload/zfs
     paths, including the volid-escaping cases `nodeVolumePath` already
     documents.
- [ ] 2. Coverage: `just coverage` regen — exactly the three `/storage` rows
     flip, 258 → 261 of 675, **zero unmatched routes** (the fabrication guard is
     the proof the mock's new paths are real PVE paths); no annotation edits.
     Commit the regenerated `docs/COVERAGE.md`.
- [ ] 3. Prepared integration harness (`proxmox/integration/datastore_test.go`,
     `//go:build integration`): `TestDatastoreLifecycle` behind
     `PVE_TEST_DATASTORE=1` + the standard env — create scratch zfspool entry
     over an existing pool (dir-type fallback per DESIGN-0007 OQ-6), nodes
     restriction, read-back digest, drift, converge, stale-digest negative
     check, delete, verify gone. Labelled **never-run, no cassette** (the
     `TestACMEDNSNamecheap` mould); NOT added to `just test-replay`. Verify
     compile via `go vet -tags=integration ./proxmox/integration/` and the skip
     path via
     `env -u PVE_ENDPOINT go test -tags=integration -run TestDatastoreLifecycle`
     (never run the tagged suite with a live env — it can rewrite committed
     cassettes).
- [ ] 4. TESTING.md: a short "Storage config writes" subsection — the gate, the
     harness's shape, and a pointer to the posture-change note (this is a
     deferred harness; running it needs a disposable target that does not
     currently exist).
- [ ] 5. Docs: `storage/doc.go` gains the config-write story (create/update/
     delete, digest idiom, delete-is-config-only, the permission note);
     `Example_datastoreConfig` in `example_test.go` (create zfspool → get →
     update → delete against `mockpve.Serve()`, `// Output:` block —
     documentation and test). `go doc ./...` renders cleanly.
- [ ] 6. PR: one `minor` PR (exactly one semver label), changelog as the
     branch's final commit before push. Changelog/PR body carries the two compat
     notes: `Digest` leaves `Extra`, and `storage.API` grew three methods.

#### Success Criteria

- All CI jobs green on the PR — including `Test Replay (cassettes)` untouched
  (nothing added to replay) and `just coverage-check` (stale report / fabricated
  route / stale annotation all clean).
- `docs/COVERAGE.md` shows 261/675 with the three rows flipped and nothing else
  changed.
- The harness compiles under the integration tag and skips cleanly without its
  gate; it appears in no CI job.
- `go doc ./...` renders the new surface; the Example runs deterministically.

---

### Phase 4: Consumer verification (hoomlab, Donald-run)

The DESIGN-0007 OQ-6 posture executed: hoomlab is the live-verification vehicle,
in parallel and in its own repo. This phase is Donald-run; SDK-side findings
fold back as fixes. Until it completes, the write surface is **mock-verified**
and every doc that mentions it says so.

#### Tasks

- [ ] 1. hoomlab imports the surface (per OQ-1 of this ledger: released tag or
     `go.mod replace` on main) and builds its `pve storage` stage against it:
     `bootstrap.hcl` storage blocks → converge loop over
     `ListDatastores`/`GetDatastore` + the three writes, comparing
     `Content`/`Nodes` as sets and threading `Datastore.Digest` into
     `DatastoreUpdate.Digest`.
- [ ] 2. First converge against the production cluster (hoomlab-run,
     hoomlab-attributed): create the `fast/vm` + `tank/vm` zfspool entries
     (node-restricted, sparse) and restrict the stock `local-zfs` entry — the
     two shapes issue #28 names. hoomlab knows which entry it expected to create
     or drift-correct, so a breakage names itself.
- [ ] 3. Findings fold back here as issues → fixes, each pinned by a mock test
     so mock and PVE cannot silently disagree again. Watch the classes the mock
     cannot prove: the PUT return shape actually populated, digest semantics on
     concurrent edits, PVE's real normalization of `nodes`/`content` versus the
     mock's sorted-set model, and the create-fixed rejection's real
     status/message.
- [ ] 4. Ledger closure: record the converge evidence with dates; flip the
     surface's label from mock-verified to **consumer-exercised** in this ledger
     and anywhere else that states verification status; per OQ-3, flip IMPL-0008
     → Completed. The repo-side cassette + replay wiring stays deferred (out of
     scope) and is NOT resurrected here.

#### Success Criteria

- hoomlab's `pve storage` converge stage runs green against the production
  cluster: both issue-#28 shapes exist/hold on re-run (idempotent — a second
  converge is a no-op).
- Every SDK-vs-PVE discrepancy hoomlab surfaced is fixed on this repo's side
  with a mock test pinning it — zero known mock-and-SDK-agree-but-PVE- disagrees
  gaps on the storage write surface.
- The ledger and docs say **consumer-exercised**, not live-verified — the
  honesty rule under the deferral.

---

## File Changes

| File                                    | Action | Description                                                       |
| --------------------------------------- | ------ | ----------------------------------------------------------------- |
| `proxmox/storage/types.go`              | Modify | `Datastore.Digest` + `datastoreKnownFields`                       |
| `proxmox/storage/datastore.go`          | Modify | Specs, `DatastoreWriteResult`, three write methods                |
| `proxmox/storage/service.go`            | Modify | `API` interface grows the three methods                           |
| `proxmox/storage/storage_test.go`       | Modify | Wire-form tables, unit matrix, `TestDatastoreConvergeShape`       |
| `proxmox/storage/paths_test.go`         | Create | `TestStoragePathsReal` (write + existing paths)                   |
| `proxmox/storage/doc.go`                | Modify | Config-write story                                                |
| `proxmox/storage/example_test.go`       | Modify | `Example_datastoreConfig`                                         |
| `proxmox/mockpve/storage.go`            | Modify | `storeRecord` growth, 3 routes, digest guard, set normalization   |
| `proxmox/integration/datastore_test.go` | Create | Prepared `TestDatastoreLifecycle` (gated, never-run, no cassette) |
| `TESTING.md`                            | Modify | Harness section + gate row, posture cross-reference               |
| `docs/COVERAGE.md`                      | Modify | Regenerated: three flips → 261/675                                |
| `docs/design/0007-…-config-writes.md`   | Modify | Status → Implemented (Phase 3/4 closure)                          |

## Testing Plan

- Wire-form table tests with no HTTP pin the encoding before any handler exists
  (Phase 1).
- Unit tests beside the code run every exported op against mockpve (repo
  convention); the digest guard, create-fixed rejection, and set-normalization
  are asserted in both directions (Phase 2).
- `TestDatastoreConvergeShape` proves the consumer's loop runs against the mock
  unmodified (Phase 2).
- `TestStoragePathsReal` pins every literal path in-repo; the coverage
  fabrication guard proves the mock's paths are real PVE paths in CI (Phase 3).
- Live behaviour is consumer-verified through hoomlab (Phase 4) — no cassette,
  no replay, per the deferral.

## Dependencies

- DESIGN-0007 approved (this branch) — the design this implements.
- No new Go module dependencies.
- Phase 4 needs: hoomlab's `pve storage` stage built against this surface
  (hoomlab repo work), and the production cluster hoomlab already manages.
  Nothing in Phases 1–3 blocks on it.

## Open Questions

1. **Phase-4 import path: when does hoomlab pick the surface up?**
   - **a (recommended): merge and release first; hoomlab verifies against the
     minted tag (or a `replace` on main).** The repo's working definition of
     done is mock-verified + CI green — features have always merged on that
     basis, with live evidence following and findings folding back as fixes.
     Holding this PR open while another repo builds a whole converge stage
     couples two repos' timelines; and Donald's decision framed the loop as "add
     the features as we normally would," with hoomlab testing in parallel.
     Follow-up fixes ride normal patch/minor releases.
   - b: hold the PR open; hoomlab `replace`s onto the branch and its findings
     fold into the same PR, so the first minted tag is already
     consumer-exercised. One clean release, but the PR's lifetime becomes
     hoomlab's development timeline, and the repo's own CI green + mock
     verification gets demoted from "done" to "waiting".
   - c: merge with `dont-release`; hoomlab verifies main via `replace`; a later
     trivial PR carries the `minor` label to mint the tag. Avoids a
     possibly-wrong tag without holding the PR, but decouples the release from
     the change it releases and adds a ceremony merge.

2. **`Extra`-carried secrets (a CIFS/PBS `password` rides `Extra`)** — how much
   does the SDK do about it?
   - **a (recommended): doc-note only.** The spec doc comments state that
     `Extra` may carry provider secrets and that the consumer owns what it
     prints; the SDK's transport logging (`PVE_DEBUG`) logs method+URL, never
     bodies, so the SDK itself cannot spill it. The first consumer's shapes
     (zfspool) carry no secret at all — redaction machinery for a field nobody
     ships yet is speculative surface.
   - b: give `DatastoreSpec`/`DatastoreUpdate` a redacting `String()` (the
     `ACMECloudflare` pattern) that prints field names but masks `Extra` values.
     Cheap and makes `%v` in consumer logs safe, but unlike the ACME types these
     specs are mostly non-secret, so masking all of `Extra` hides useful
     debugging state to protect a key nobody sends yet.
   - c: model `Password string` as a typed field with redaction now. Most honest
     about the hazard, but it types a field for storage types (CIFS/PBS) with no
     consumer and reopens the OQ-2 typed-extent decision that was just settled
     as minimal.

3. **Ledger completion semantics under the deferral** — when does IMPL-0008 flip
   to Completed?
   - **a (recommended): after Phase 4** — hoomlab's converge green and the label
     flipped to consumer-exercised. That keeps this ledger's completion meaning
     what every earlier ledger's meant: the surface was exercised against real
     PVE by _something_, with the deferral-era substitute (consumer) standing in
     for the retired live suite. If hoomlab's stage takes a while, the ledger
     honestly stays In Progress — the SDK release itself is not delayed (see
     OQ-1a).
   - b: after Phase 3 (SDK shipped, CI green); Phase 4 becomes a follow-up
     section like IMPL-0007's r740a note. Closes faster, but "Completed" would
     then mean less than it has meant in every prior ledger, and the
     consumer-exercised flip would live in a section nobody's checklist forces.

## References

- DESIGN-0007 — Cluster storage config writes (Approved; the decided OQs this
  ledger executes).
- [GitHub issue #28](https://github.com/donaldgifford/proxmox-go-sdk/issues/28)
  — motivation and consumer.
- IMPL-0007 — the digest/explicit-delete precedent (ACME plugins), the
  `UpdateACMEAccount` discarded-return lesson, and the "Follow-up: move the
  destructive integration tests off r740a" section this ledger's Phase 4
  operates under.
- hoomlab INV-0001 (hardware drill) — the set-normalization findings behind the
  mock's realism rules.
- `docs/COVERAGE.md` + `cmd/pve-schemadiff` — the coverage and fabrication
  guards Phase 3 leans on.
- TESTING.md "Posture change (2026-08-26)" — the deferral note Phase 3's harness
  section points at.
