---
id: IMPL-0008
title: "Cluster storage config writes delivery"
status: Draft
author: Donald Gifford
created: 2026-08-26
---

<!-- markdownlint-disable-file MD025 MD041 -->

# IMPL-0008: Cluster storage config writes delivery

**Status:** Draft **Author:** Donald Gifford **Date:** 2026-08-26 (OQs decided
2026-08-26: 1a with the release posture stated, 2a, 3b amended — the
consumer-verification phase is split out as IMPL-0009)

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
  - [Phase 3: Paths, coverage, prepared harness, docs, PR, closure](#phase-3-paths-coverage-prepared-harness-docs-pr-closure)
    - [Tasks](#tasks-2)
    - [Success Criteria](#success-criteria-2)
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
Everything ships **mock-verified**, and — stated up front per OQ-1's decision —
**the release is cut without live verification**: the `minor` tag is minted on
merge with mock-verified evidence only, and verification arrives afterwards as a
**patch release** once hoomlab can import and test/verify. That
consumer-verification work is deliberately not a phase of this ledger — it is
**IMPL-0009**, split out (OQ-3's decision) so this ledger can honestly complete
when everything this repo can build and ship has shipped.

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
- **Consumer verification** — moved wholesale to **IMPL-0009** (OQ-3b):
  hoomlab's import, the converge runs against the production cluster, the
  findings-to-patch-release loop, and the mock-verified → consumer-exercised
  label flip all live there.
- **hoomlab's own stage code** — building `bootstrap.hcl` storage blocks,
  cross-validation, and the converge loop is hoomlab work in hoomlab's repo
  (IMPL-0009 tracks only the SDK side of what it finds).

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

- [x] 1. `Datastore.Digest` in `proxmox/storage/types.go`: add the field
     (`json:"digest,omitempty"`), add `"digest"` to `datastoreKnownFields`. Unit
     tests: a read carrying `digest` lands it in the typed field and **not** in
     `Extra` (assert both), and a read without one leaves it empty. Doc comment
     teaches the read-then-guarded-write idiom (mirror `ACMEPlugin.Digest`'s
     wording). Note for Phase 3's changelog task: a consumer reading
     `Extra["digest"]` today loses it to the typed field. _(Done 2026-08-27:
     `TestDatastoreDigestTyped` unmarshals the committed cassette's literal
     shape — digest in the field, absent from Extra, `thinpool` still routed to
     Extra as the lossless-read regression guard, and the digest-less read
     empty. The field comment names DatastoreUpdate.Digest as the destination so
     the idiom is discoverable from the read side.)_
- [x] 2. Write types in `proxmox/storage/datastore.go`: `DatastoreSpec`,
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
     prints). _(Done 2026-08-27: all three types land per the design, plus a
     custom `DatastoreWriteResult.UnmarshalJSON` — the config member is declared
     additionalProperties, so a non-string value keeps its raw token instead of
     failing the write that succeeded, and a null payload decodes to the zero
     result (the ApplyNetworkConfig posture without a special case).
     `TestDatastoreWriteResultDecode` pins all four shapes — string config,
     raw-token config, configless, null — front-loading that row of Phase 2 task
     4's matrix.)_
- [x] 3. Encoding: `svcutil.EncodeWithExtra` + post-encode comma-joins for
     `Content`/`Nodes`/`Delete` (the ZFS-`Devices`/HA-rules/ACME-`Nodes`
     mechanics). Table-driven wire-form tests, no HTTP: the hoomlab zfspool spec
     renders exactly (`storage`, `type`, `pool`, `sparse=1`, `blocksize`,
     `content` joined, `nodes` joined); a zero `DatastoreUpdate` encodes to an
     empty body; only-set-fields on update (unset pointer booleans absent,
     `false`-and-set present as `0`); `delete`+`digest` render; an `Extra` key
     wins over its typed field (the EncodeWithExtra contract, asserted here so
     the spec docs stay honest). _(Done 2026-08-27: unexported
     `encodeDatastoreSpec`/`encodeDatastoreUpdate` + a shared `joinCSV` in
     `datastore.go`; Phase 2's methods call these. Wire forms pinned
     byte-for-byte via `url.Values.Encode` (canonical sorted keys) in the
     internal `datastore_encode_test.go` — the package's first internal test
     file, needed because the helpers are unexported: the hoomlab zfspool shape
     verbatim, false-PVEBool omission on create, Extra-beats-typed,
     zero-update-empty-body, disable=0-vs-absent tri-state, and delete+digest.)_

#### Success Criteria

- `go build ./...`, `just lint`, `just test` (race) green. **Met 2026-08-27**
  (verified after each task).
- The zfspool wire form is pinned by a table test byte-for-byte, and the
  zero-update-sends-nothing property holds — Phase 2 debugging starts from
  known-good encoding. **Met 2026-08-27** — `TestEncodeDatastoreSpec` /
  `TestEncodeDatastoreUpdate` in `datastore_encode_test.go`.
- `digest` demonstrably moved: one test proves typed-field presence AND `Extra`
  absence on the same read. **Met 2026-08-27** — `TestDatastoreDigestTyped`,
  which also guards the lossless-read tail (`thinpool` still routes to Extra).

**Phase 1 complete 2026-08-27.** All three tasks done; the new public surface
(`DatastoreSpec`, `DatastoreUpdate`, `DatastoreWriteResult`, `Datastore.Digest`)
is doc-commented and renders under `go doc` — the package-level story stays a
Phase 3 task by design. No go-development review agents exist in this
environment; the phase's verification is the pinned wire forms + the full suite,
with a review pass scheduled after Phase 2 (the functional core).

---

### Phase 2: mockpve write support + service methods

The functional core: the three routes on the mock, the three methods on the
service, and the unit matrix that runs every op against the mock. Mock and SDK
land in one phase because the repo convention is service tests against mockpve —
neither half is testable alone.

#### Tasks

- [x] 1. mockpve state (`proxmox/mockpve/storage.go`): `storeRecord` grows
     `Nodes`, `Disable`, `Digest`, and `Extra map[string]string` for
     submitted-but-unmodelled keys (`sparse`, `blocksize`, … — the mock needs
     faithful read-back, not typed fields); `datastoreToPayload` emits them
     (map-shaped JSON, matching real PVE's flat object); `AddStorage` keeps its
     signature and seeds a digest so existing seeded tests read one. **Done
     2026-08-27** — one refinement: the digest lives on `storageState`
     (`cfgVersion`/`cfgDigest` + `bumpStorageDigest`), NOT per-record, because
     the live cassette shows the storage.cfg FILE digest — one value shared by
     every entry of a read. `datastoreToPayload(rec, digest)` takes it as an
     argument. `AddStorage` also normalizes seeded content (`TestGetDatastore`'s
     expectation updated to the sorted set).
- [x] 2. mockpve handlers: `POST /storage` (missing `storage`/`type` → 400;
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
     compare them as sets. **Done 2026-08-27** —
     `handleDatastoreCreate`/`Update`/`Delete` +
     `normalizeSet`/`applyStorageForm`/`clearStorageKey`/`createFixedKeys`,
     routes registered in `registerStorageRoutes`. Form reads use
     `r.PostForm.Get` after `s.parseForm` (gosec G120) and the repeated wire
     keys are consts (goconst). The stale-digest 400 reuses real PVE's "detected
     modified configuration" message.
- [x] 3. Service methods in `proxmox/storage/datastore.go`:
     `CreateDatastore`/`UpdateDatastore` → `*DatastoreWriteResult`,
     `DeleteDatastore` → `error`, per DESIGN-0007's signatures — nil-spec /
     empty-id guards first (`svcutil.ErrNilSpec`/`ErrMissingField`), no version
     gate (9.0 baseline). Doc comments carry the `Datastore.Allocate` permission
     note (ordinary privilege, tokens work) and delete's config-not-data
     semantics. `storage.API` grows the three methods with a changelog note
     (pre-v1 break for external doubles only). **Done 2026-08-27** — the three
     methods follow the sdn.CreateZone guard idiom; `storage.API` gained a
     "Datastore configuration writes (DESIGN-0007)" block. The changelog note is
     Phase 3 task 6's PR body.
- [x] 4. Unit matrix (beside the code, against the mock): create → list/get
     reflects the write including `Extra` keys; duplicate create rejected;
     update applies set-keys, honours `delete`, refuses stale digest AND accepts
     fresh (both directions); create-fixed key on update rejected; delete →
     subsequent get 404s → `pverr.ErrNotFound` via `errors.Is`;
     set-normalization (submit `nodes=b,a` → read `a,b`); `Extra` round-trip for
     an unmodelled key (`preallocation`); result decode with and without
     `config`; digest changes across writes (guard-testable-without-race
     property). **Done 2026-08-27** — `datastore_write_test.go`: eight tests
     covering every row (`wantSet` is the compare-as-sets helper); the
     result-decode row was front-loaded in Phase 1
     (`TestDatastoreWriteResultDecode`). Race suite green.
- [x] 5. `TestDatastoreConvergeShape`: hoomlab's exact sequence against the mock
     — get(miss → `ErrNotFound`) → create zfspool (pool, sparse, content, nodes)
     → get(hit; compare `Content`/`Nodes` **as sets**) → drift-correct via
     update (content restriction) with the read's digest → delete. This is the
     seeding-not-stubbing proof: the consumer's converge logic can run against
     `mockpve` unmodified. **Done 2026-08-27** — in `datastore_write_test.go`;
     the probe branches on `errors.Is(err, pverr.ErrNotFound)` (never
     message-matching), the delete is verified by a second probe.

#### Success Criteria

- `go build ./...`, `just lint`, `just test` (race) green. **Met 2026-08-27.**
- The full unit matrix passes; the digest guard is covered in both directions;
  set-normalization is asserted with an unsorted submission. **Met 2026-08-27**
  — `TestUpdateDatastoreDigestGuard` (fresh accepted → digest bumped → stale
  replay 400), `TestCreateDatastoreReflected` (unsorted submission reads back
  sorted).
- `TestDatastoreConvergeShape` passes end-to-end — the mock can host the
  consumer's loop before the consumer exists. **Met 2026-08-27.**

**Phase 2 complete 2026-08-27.** All five tasks done. The scheduled post-phase
review ran (grug-brain reviewer — no go-development agents exist in this
environment): no complexity findings in production code; its one actionable
finding (duplicated create/read assertions between `TestConvergeShape` and
`TestCreateDatastoreReflected`) was applied — the converge test now asserts only
the set-compare step of its sequence and defers field-level reflection to the
matrix test.

---

### Phase 3: Paths, coverage, prepared harness, docs, PR, closure

Hardening and shipping: the guards that keep the paths honest, the harness that
ends the deferral cheaply later, the PR, and — per OQ-3b — the ledger's own
closure: this phase is the last one, and completing it completes IMPL-0008.

#### Tasks

- [x] 1. Create `proxmox/storage/paths_test.go` with `TestStoragePathsReal` (the
     `ceph`/`ha`/`nodes`/`sdn` pattern): pin the three write paths and — since
     the file is being created anyway — the existing read/content/ upload/zfs
     paths, including the volid-escaping cases `nodeVolumePath` already
     documents. **Done 2026-08-27** — writing the pins immediately caught a
     stale doc comment: `paths.go` claimed `url.PathEscape` renders the volid
     colon as `%3A`, but it leaves the colon LITERAL (the same finding as the HA
     `/resources/vm:100` paths; the literal-colon form is what the live ISO
     upload run drove). Comment corrected, actual wire form pinned.
- [x] 2. Coverage: `just coverage` regen — exactly the three `/storage` rows
     flip, 258 → 261 of 675, **zero unmatched routes** (the fabrication guard is
     the proof the mock's new paths are real PVE paths); no annotation edits.
     Commit the regenerated `docs/COVERAGE.md`. **Done 2026-08-27** — exactly
     `POST /storage`, `PUT /storage/{}`, `DELETE /storage/{}` gap→covered,
     storage family 16→19 of 55, `just coverage-check` clean.
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
- [ ] 6. PR + release: one `minor` PR (exactly one semver label), changelog as
     the branch's final commit before push. The PR body and changelog state the
     OQ-1 release posture explicitly: **this release ships mock-verified,
     without live verification; verification follows via hoomlab (IMPL-0009) and
     any resulting fixes arrive as a patch release.** They also carry the two
     compat notes: `Digest` leaves `Extra`, and `storage.API` grew three
     methods.
- [ ] 7. Ledger closure (after merge + tag): tick this ledger with dated
     evidence, flip DESIGN-0007 → Implemented, flip IMPL-0008 → Completed with
     the surface labelled **mock-verified** (the consumer-exercised flip is
     IMPL-0009's, not this ledger's), and confirm IMPL-0009 is In Progress-able
     the moment hoomlab starts (closure docs ride a `dont-release` PR or the
     next release PR).

#### Success Criteria

- All CI jobs green on the PR — including `Test Replay (cassettes)` untouched
  (nothing added to replay) and `just coverage-check` (stale report / fabricated
  route / stale annotation all clean).
- `docs/COVERAGE.md` shows 261/675 with the three rows flipped and nothing else
  changed.
- The harness compiles under the integration tag and skips cleanly without its
  gate; it appears in no CI job.
- `go doc ./...` renders the new surface; the Example runs deterministically.
- The merge mints the `minor` tag with the mock-verified posture stated in the
  release notes; IMPL-0008 flips to Completed — everything this repo can build
  and ship without a live target has shipped, and what remains is tracked with
  checkboxes in IMPL-0009, not silently here.

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
| `docs/design/0007-…-config-writes.md`   | Modify | Status → Implemented (Phase 3 task 7)                             |

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
- Live behaviour is consumer-verified through hoomlab — tracked in
  **IMPL-0009**, not here; no cassette, no replay, per the deferral.

## Dependencies

- DESIGN-0007 approved (this branch) — the design this implements.
- No new Go module dependencies.
- Nothing here blocks on hoomlab: its stage, the production cluster, and the
  converge runs are IMPL-0009's dependencies, picked up after this ledger's
  release ships.

## Open Questions

1. **Phase-4 import path: when does hoomlab pick the surface up?** **Decision
   (2026-08-26): a — with the posture stated, not implied.** The `minor` release
   is cut **without live verification** (mock-verified only; the PR body,
   changelog, and release notes say so), and verification comes afterwards **in
   the form of a patch release** once hoomlab can import and test/verify.
   Executed as Phase 3 task 6; the verification loop itself is IMPL-0009.
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
   does the SDK do about it? **Decision (2026-08-26): a.**
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
   to Completed? **Decision (2026-08-26): b, amended into a split rather than a
   demotion.** IMPL-0008 completes after Phase 3 — "complete in regards to what
   we can implement", so a work loop breaks once everything
   buildable-and-shippable here has shipped — but the former Phase 4 is **not**
   parked in a follow-up section nobody's checklist forces (option b's stated
   weakness): it moved wholesale into **IMPL-0009**, its own ledger with its own
   checkboxes and success criteria, started once hoomlab can begin testing. The
   consumer-exercised flip therefore still has a checklist that forces it; this
   ledger's Completed just stops claiming it.
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
  destructive integration tests off r740a" section behind the deferral.
- IMPL-0009 — the consumer-verification ledger split out of this one (OQ-3
  decision): hoomlab's converge runs, the findings-to-patch-release loop, and
  the consumer-exercised label flip.
- hoomlab INV-0001 (hardware drill) — the set-normalization findings behind the
  mock's realism rules.
- `docs/COVERAGE.md` + `cmd/pve-schemadiff` — the coverage and fabrication
  guards Phase 3 leans on.
- TESTING.md "Posture change (2026-08-26)" — the deferral note Phase 3's harness
  section points at.
