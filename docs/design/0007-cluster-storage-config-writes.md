---
id: DESIGN-0007
title: "Cluster storage config writes"
status: Draft
author: Donald Gifford
created: 2026-08-26
---

<!-- markdownlint-disable-file MD025 MD041 -->

# DESIGN-0007: Cluster storage config writes

**Status:** Draft **Author:** Donald Gifford **Date:** 2026-08-26

<!--toc:start-->

- [Overview](#overview)
- [Goals and Non-Goals](#goals-and-non-goals)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
- [Background](#background)
  - [The consumer](#the-consumer)
  - [What the SDK covers today](#what-the-sdk-covers-today)
  - [Wire facts (from the committed 9.2 apidoc and a live cassette)](#wire-facts-from-the-committed-92-apidoc-and-a-live-cassette)
- [Detailed Design](#detailed-design)
  - [Service methods](#service-methods)
  - [Write specs](#write-specs)
  - [The write result](#the-write-result)
  - [Digest on the read type](#digest-on-the-read-type)
  - [Permissions (doc note only)](#permissions-doc-note-only)
  - [mockpve](#mockpve)
- [API / Interface Changes](#api--interface-changes)
- [Data Model](#data-model)
- [Testing Strategy](#testing-strategy)
- [Migration / Rollout Plan](#migration--rollout-plan)
- [Open Questions](#open-questions)
- [References](#references)
<!--toc:end-->

## Overview

Add the write half of PVE's cluster storage configuration API —
`CreateDatastore` (`POST /storage`), `UpdateDatastore`
(`PUT /storage/{storage}`), `DeleteDatastore` (`DELETE /storage/{storage}`) — to
`storage.Service`, with mockpve serving all three so consumers can test a
config-convergence loop without a cluster. This closes GitHub issue #28: the
`hoomlab` bootstrap CLI's `pve storage` stage (create-if-missing,
update-if-drifted) blocks on it, and the SDK today exposes only the read side.

## Goals and Non-Goals

### Goals

- The three write ops on the existing cluster-scoped `storage.Service`, flipping
  the last three `/storage` coverage `gap` rows to `covered` (258 → 261 of 675).
- Write specs that model the issue's concrete use case first-class: a `zfspool`
  entry over a pre-existing dataset (`pool`, `sparse`, `blocksize`),
  content-typed and node-restricted — plus the `Extra` escape hatch for the
  other ~50 per-type parameters.
- Update semantics that match PVE's: send only what the caller set, unset keys
  via the explicit `delete` list, guard concurrent edits with the optional
  `digest` — the ACME-plugin contract, reused verbatim.
- A typed create/update result: PVE returns `{storage, type, config?}` where
  `config` can carry a server-generated `encryption-key`. Discarding that
  repeats the `UpdateACMEAccount` bug class (a meaningful return thrown away),
  so it is modelled from day one.
- mockpve handlers whose read-backs reflect prior writes **without promising
  byte-exact round-trips** — the hoomlab hardware drill (hoomlab INV-0001) hit
  the consumers-depend-on-mock-order bug class three times; the mock should make
  that dependence impossible to form.

### Non-Goals

- **Typed fields for all 61 `POST /storage` parameters across 14 storage
  types.** The spec models the common set plus the zfspool case; everything else
  rides `Extra` (see OQ-2). Typed fields are added on demand, the same policy as
  `Datastore` reads today.
- **A convergence helper (`EnsureDatastore`).** Create-if-missing /
  update-if-drifted is the consumer's stage logic; the SDK ships primitives (see
  OQ-7).
- **Endpoint-permission enforcement in mockpve.** The issue files that
  separately with the other real-vs-mock parity findings from the drill; this
  design only adds a doc note on the privilege each op needs.
- **Storage-content operations.** Volume CRUD, uploads, and ZFS pool ops shipped
  in Phase 3 and are untouched; this is config only. Deleting a datastore entry
  removes **configuration**, never data — PVE leaves the underlying
  dataset/directory/export alone.
- **The node-scoped `/nodes/{node}/storage/{storage}` PUT** — no such endpoint
  exists; per-node behaviour is expressed through the cluster entry's `nodes`
  list.

## Background

### The consumer

`donaldgifford/hoomlab` `tools/bootstrap` is growing a `pve storage` stage
(hoomlab IMPL-0002 Phase 3): `storage` blocks in `bootstrap.hcl` converge into
PVE cluster storage entries so the `talos` stage's `storage = "<id>"` references
are guaranteed to exist before VM creation. Two concrete shapes:

1. **Create**: `zfspool` entries over pre-existing datasets (`fast/vm`,
   `tank/vm`), `content images,rootdir`, node-restricted to the hosts that carry
   those pools, `sparse` on.
2. **Update**: restrict the stock `local-zfs` entry (content and/or nodes) so VM
   disks cannot land on boot-device pools.

Both are idempotent config writes against `/storage`; neither touches data.

### What the SDK covers today

`storage.Service` (cluster-scoped, no bound node — DESIGN-0001) has the read
side only: `ListDatastores` (`GET /storage`), `GetDatastore`
(`GET /storage/{storage}`), plus the node-scoped status reads. The `Datastore`
read type is lossless (custom `UnmarshalJSON` routes unknown keys into `Extra`;
`datastoreKnownFields` kept in sync) and models `storage`, `type`, `content`,
`path`, `pool`, `server`, `export`, `share`, `nodes`, `shared`, `disable`.
`docs/COVERAGE.md` carries `POST /storage`, `PUT /storage/{}` and
`DELETE /storage/{}` as the family's only `gap` rows.

### Wire facts (from the committed 9.2 apidoc and a live cassette)

Everything below is read from `cmd/pve-schemadiff/testdata/apidoc-9.2.js.gz`
(the r740a capture) or the committed `TestStorageReads` cassette, not guessed:

- `POST /storage`: 61 parameters; `storage` (format `pve-storage-id`) and `type`
  required. `type` is an enum of **14**: `btrfs`, `cephfs`, `cifs`, `dir`,
  `esxi`, `iscsi`, `iscsidirect`, `lvm`, `lvmthin`, `nfs`, `pbs`, `rbd`, `zfs`,
  `zfspool`.
- **`POST` and `PUT` both return an object, not null and not a UPID**:
  `{storage, type, config?}`, where `config` is "Partial, possibly server
  generated, configuration properties" with one schema-named member — the
  "possibly auto-generated" `encryption-key` (PBS storage with
  `encryption-key=autogen`). Both writes are synchronous; there is no task to
  await.
- `PUT /storage/{storage}`: 51 parameters — everything from `POST` **minus
  twelve create-fixed ones** (`type`, `path`, `export`, `share`, `target`,
  `portal`, `vgname`, `thinpool`, `base`, `datastore`, `iscsiprovider`,
  `authsupported`) **plus `delete`** (CSV list of settings to unset, max 4096)
  **and `digest`** (max 64: "prevent changes if current configuration file has a
  different digest"). So identity and backing-location are immutable;
  content/nodes/tuning are not.
- `DELETE /storage/{storage}`: returns `{"type": "null"}` — synchronous, no body
  parameters. It removes the config entry only.
- All three declare the same permission:
  `{"check": ["perm", "/storage", ["Datastore.Allocate"]]}` — a regular
  privilege check, **not** one of the root@pam-reserved identity checks (cluster
  create/join, ACME account ops), so API tokens work.
- The zfspool fields the consumer needs: `pool` (string), `sparse` (boolean),
  `blocksize` (string, format `pve-storage-zfs-blocksize`), `content` (format
  `pve-storage-content-list`), `nodes` (format `pve-node-list`), `disable` and
  `shared` (booleans). `pool` is present in both POST and PUT.
- **Live-confirmed** (`TestStorageReads.yaml`, r740a 9.2): `GET /storage`
  entries carry a `digest` field (the storage.cfg file digest — the same value
  on every entry). The apidoc leaves the GET returns untyped
  (`{"type": "object"}`), so the cassette is the evidence. Today that digest
  lands in `Datastore.Extra`.
- Realism note from the hoomlab drill (hoomlab INV-0001): `nodes` and `content`
  are comma-joined **sets** — submission order is not preserved on read-back,
  and read shapes can differ from submitted shapes generally (cf. the ACME
  plugin `data` field returning decoded plaintext).

## Detailed Design

### Service methods

Three methods on the existing `storage.Service`, named to match the read side
(`ListDatastores`/`GetDatastore`):

```go
// CreateDatastore adds a cluster storage entry (POST /storage). The write is
// synchronous; the returned result echoes the created id and type and, for
// storage types that generate configuration server-side (a PBS datastore with
// encryption-key=autogen), carries the generated properties — capture the
// encryption key from it, PVE does not return it again.
// Requires Datastore.Allocate on /storage (a regular privilege; API tokens
// work).
func (s *Service) CreateDatastore(ctx context.Context, spec *DatastoreSpec) (*DatastoreWriteResult, error)

// UpdateDatastore changes an existing entry (PUT /storage/{storage}). Only
// fields the caller set are sent; unset a key with Delete; pass the Digest
// from the read that informed this update to fail rather than clobber a
// concurrent edit. Identity and backing location (type, path, export, share,
// vgname, …) are fixed at creation — PVE rejects them here.
func (s *Service) UpdateDatastore(ctx context.Context, storage string, update *DatastoreUpdate) (*DatastoreWriteResult, error)

// DeleteDatastore removes a storage entry from the cluster configuration
// (DELETE /storage/{storage}). Synchronous. It never touches the underlying
// data — the dataset, directory, or export remains; only PVE's reference to
// it is removed. Guests still referencing volumes on the removed storage keep
// their config strings and fail on next start/migrate.
func (s *Service) DeleteDatastore(ctx context.Context, storage string) error
```

Nil-spec and empty-id guards mirror every other service (`svcutil.ErrNilSpec` /
`svcutil.ErrMissingField` before any request). No version gate: `/storage` CRUD
is 9.0-baseline (it long predates 9.x), and per ADR-0002 the floor is 9.0.

### Write specs

Two pointer write specs (the repo-wide pattern — see OQ-1), encoded with
`svcutil.EncodeWithExtra` and the established post-encode fix-ups for
list-valued fields:

```go
// DatastoreSpec creates a storage entry. Storage and Type are required;
// which of the remaining fields apply depends on Type (PVE validates
// server-side). Unmodelled per-type parameters ride Extra verbatim.
type DatastoreSpec struct {
    Storage string `json:"storage"` // unique storage ID (pve-storage-id).
    Type    string `json:"type"`    // "dir", "zfspool", "nfs", … — fixed for the entry's lifetime.

    Content []string `json:"-"` // allowed content types; comma-joined ("images", "rootdir", "iso", …).
    Nodes   []string `json:"-"` // node restriction; comma-joined; empty = all nodes.

    Path   string `json:"path,omitempty"`   // dir/btrfs backing path (create-fixed).
    Pool   string `json:"pool,omitempty"`   // zfspool dataset / RBD pool.
    Server string `json:"server,omitempty"` // nfs/cifs server (create-fixed for iscsi portal etc. via Extra).
    Export string `json:"export,omitempty"` // NFS export (create-fixed).
    Share  string `json:"share,omitempty"`  // CIFS share (create-fixed).

    Blocksize string        `json:"blocksize,omitempty"` // zfspool volblocksize, e.g. "16k".
    Sparse    types.PVEBool `json:"sparse,omitempty"`    // zfspool thin provisioning.
    Shared    types.PVEBool `json:"shared,omitempty"`
    Disable   types.PVEBool `json:"disable,omitempty"`

    // Extra carries parameters the SDK does not model ("preallocation",
    // "krbd", "monhost", "username", …); keys here win over typed fields.
    Extra map[string]string `json:"-"`
}

// DatastoreUpdate changes an entry. The zero value sends nothing; only set
// fields go on the wire, so an update cannot accidentally reset a key it
// did not name. Booleans are pointers because false-and-set and unset must
// differ on a partial write.
type DatastoreUpdate struct {
    Content []string `json:"-"`
    Nodes   []string `json:"-"`

    Pool      string         `json:"pool,omitempty"`
    Blocksize string         `json:"blocksize,omitempty"`
    Sparse    *types.PVEBool `json:"sparse,omitempty"`
    Shared    *types.PVEBool `json:"shared,omitempty"`
    Disable   *types.PVEBool `json:"disable,omitempty"`

    // Delete lists settings to unset (PVE's delete param, comma-joined) —
    // the explicit-delete contract shared with ACMEPluginUpdate: clearing a
    // key is a named action, never an empty-string side effect. Clearing
    // "nodes" here is how a node restriction is lifted.
    Delete []string `json:"-"`
    // Digest is the config digest from the read that informed this update.
    // When set, PVE refuses the write if the storage config changed since —
    // pass Datastore.Digest to make read-modify-write safe.
    Digest string `json:"digest,omitempty"`

    Extra map[string]string `json:"-"`
}
```

`Content`, `Nodes` and `Delete` are `json:"-"` and comma-joined after
`EncodeWithExtra` — the same mechanics as ZFS `Devices`, HA rule
`Nodes`/`Resources`, and ACME plugin `Nodes`/`Delete`. A nil slice sends
nothing; lifting a restriction goes through `Delete`, not an empty value (see
the update-semantics bullet in Goals).

The slices also encode the drill's set-semantics finding in the type system: a
caller handing the SDK a `[]string` has no order to depend on once the
join+normalize round-trip is documented (see mockpve below).

### The write result

```go
// DatastoreWriteResult is the response of a datastore create or update:
// the entry's id and type, plus any configuration PVE generated server-side.
type DatastoreWriteResult struct {
    Storage string `json:"storage"`
    Type    string `json:"type"`
    // Config carries server-generated properties. The schema names one:
    // "encryption-key", auto-generated when a PBS datastore is created with
    // encryption-key=autogen. It is returned HERE and not again — a caller
    // that discards it has lost the key material.
    Config map[string]string `json:"config,omitempty"`
}
```

Typing this is the `UpdateACMEAccount` lesson applied up front: the IMPL-0007
schema audit found that op discarding a UPID the schema declared, an SDK bug
invisible to the coverage guard (right path, right verb, wrong return handling).
Here the schema declares a payload both on create **and** update, so both
methods return it. The decode is tolerant by construction: if a hypothetical
node answered `null`, `json.Unmarshal` leaves the zero result — no error
manufactured from a write that succeeded (the `ApplyNetworkConfig` posture,
without needing a special case).

### Digest on the read type

`Datastore` gains `Digest string` (`json:"digest,omitempty"`), and
`datastoreKnownFields` gains `"digest"` — additive, live-confirmed by the
committed cassette, and it is what feeds `DatastoreUpdate.Digest`. This mirrors
`ACMEPlugin.Digest`, whose doc comment already teaches the
read-then-guarded-write idiom.

### Permissions (doc note only)

Each method's doc comment states the requirement — `Datastore.Allocate` on
`/storage` — and that it is an ordinary privilege (tokens work), since the
neighbouring cluster-create/join ops being root@pam-reserved makes this worth
saying explicitly. mockpve does not enforce it (out of scope here; tracked with
the drill's other parity findings).

### mockpve

Three new routes in `registerStorageRoutes`, against the existing cluster-scoped
`stores` map:

```go
s.handle("POST /api2/json/storage", s.handleDatastoreCreate)
s.handle("PUT /api2/json/storage/{storage}", s.handleDatastoreUpdate)
s.handle("DELETE /api2/json/storage/{storage}", s.handleDatastoreDelete)
```

`storeRecord` grows `Nodes`, `Disable`, `Digest`, and an
`Extra map[string]string` for submitted-but-unmodelled keys (`sparse`,
`blocksize`, … live there in the mock — the mock does not need typed fields for
them, only faithful read-back); `datastoreToPayload` emits them, building the
same map-shaped JSON real PVE serves. `AddStorage` stays source-compatible and
seeds a digest.

Handler semantics, each mirroring observed/declared PVE behaviour:

- **Create**: reject a duplicate id (PVE: "storage ID '…' already defined") and
  a missing `storage`/`type`; store the submitted form; answer `{storage, type}`
  — no `config` member, because the mock supports no auto-generating type, and
  fabricating one would teach consumers to expect it (the ACME issuer-name rule:
  the mock must not be mistakable for the real thing where it matters).
- **Update**: unknown id → 404 (matches `handleDatastoreGet`); reject
  create-fixed keys (`type`, `path`, `export`, `share`, …) the way PVE's schema
  does; apply `delete` before set-keys (the `applyConfigForm` contract);
  **enforce the digest guard** — a stale digest is refused, a fresh one
  accepted, both directions unit-covered like the ACME plugin guard.
- **Delete**: unknown id → 404; removes the record; `null` data.
- **Every write bumps the stored digest**, so read → update(digest) →
  update(same digest) fails the second time — the guard is testable without a
  race.
- **Set normalization** (the drill's realism note, see OQ-5): `nodes` and
  `content` are parsed to sets on write and emitted **sorted**, so read-back
  equals submission order only by coincidence and a consumer diffing
  read-vs-submitted strings breaks in the mock the same way it would live. The
  mock's doc comment states the rule: list-valued options are sets; compare them
  as sets.

## API / Interface Changes

All additive (a `minor` release):

| Surface                           | Change                                                              |
| --------------------------------- | ------------------------------------------------------------------- |
| `storage.Service` / `storage.API` | + `CreateDatastore`, `UpdateDatastore`, `DeleteDatastore`           |
| `storage` types                   | + `DatastoreSpec`, `DatastoreUpdate`, `DatastoreWriteResult`        |
| `storage.Datastore`               | + `Digest` field (was falling into `Extra` — additive but see note) |
| `mockpve`                         | + 3 routes, `storeRecord` fields; `AddStorage` unchanged            |
| `docs/COVERAGE.md`                | 3 gap rows flip; 258 → 261 of 675 (38.7%)                           |

Note on `Datastore.Digest`: a consumer that today reads the digest out of
`Extra["digest"]` will find it gone from the map (it moves to the typed field).
Pre-v1 and the lossless-read contract has always said typed fields absorb
`Extra` keys as they are modelled; called out in the changelog.

## Data Model

No new wire shapes beyond the three above. Encoding reuses
`svcutil.EncodeWithExtra` + post-encode comma-joins; decoding reuses the
lossless-read tail. `types.PVEBool` handles PVE's 0/1 booleans in both
directions, and the update's pointer-booleans reuse the `HARuleUpdate.Disable`
precedent.

## Testing Strategy

- **Unit (mockpve), beside the code**: create → list/get reflects the write;
  duplicate create rejected; update applies set-keys, honours `delete`, refuses
  stale digest and accepts fresh; create-fixed key on update rejected; delete →
  get 404s; set-normalization (submit `b,a` → read `a,b`); `Extra` round-trip
  for an unmodelled key (`preallocation`); `DatastoreWriteResult` decode with
  and without `config`.
- **The consumer shapes as tests**: a `TestDatastoreConvergeShape` that walks
  hoomlab's exact sequence against the mock — get(miss) → create zfspool (pool,
  sparse, content, nodes) → get(hit, compare as sets) → update content
  restriction with digest → delete. This is the seeding-not-stubbing point of
  mockpve: the consumer's stage logic can run against it unmodified.
- **`TestStoragePathsReal`** extends the existing literal-path pinning to the
  three write paths.
- **Coverage guard**: the three rows flip only because mock routes serve them; a
  typo'd path is a fabrication failure in CI, not a live 404.
- **Integration (env-gated, live-only)**: `TestDatastoreLifecycle` behind the
  existing env plus a new opt-in gate (see OQ-6) — create a scratch zfspool
  entry over an existing pool with a nodes restriction, read back digest, drift
  it, converge it, stale-digest negative check, delete, verify gone. Recorded
  with `PVE_RECORD=1`, leak-reviewed (node names and any server/export values
  are topology; storage configs can carry `password` for CIFS/PBS — the scratch
  entry uses none), committed, wired into `just test-replay`.

## Migration / Rollout Plan

1. One `minor` PR: methods + specs + result + `Datastore.Digest` + mockpve +
   unit tests + `TestStoragePathsReal` + coverage regen + docs (`storage` doc.go
   gains the config-write story; the runnable `Example` grows a create/delete
   leg or a second `Example_datastoreConfig`).
2. Live verification (Donald-run, pvelab per OQ-6): the integration test above;
   cassette leak-reviewed, committed, replay wired in. Findings fold back as
   fixes before the ledger closes.
3. hoomlab bootstrap bumps the SDK tag and builds the `pve storage` stage
   against it (out of this repo; issue #28's "Consumer" section).

## Open Questions

1. **Write-spec shape** — how do the writes take their parameters?
   - **a (recommended): separate `DatastoreSpec` (create) and `DatastoreUpdate`
     (update) pointer specs.** The two parameter sets genuinely differ — twelve
     create-fixed params exist only on POST, `delete`/`digest` only on PUT — so
     one type per op makes the illegal states unrepresentable (an update cannot
     even express `Type`), and it is the pattern every other service uses
     (`qemu.CreateSpec`/`ConfigUpdate`, `ACMEPluginSpec`/`ACMEPluginUpdate`,
     `HARuleSpec`/`HARuleUpdate`).
   - b: reuse `Datastore` as the write payload (the issue's first option). Fewer
     types, but read fields (`Digest`, mock-normalized `Content`) would silently
     ride writes, update couldn't express `delete`, and the read-vs-write
     asymmetry PVE actually has would be invisible.
   - c: one shared `DatastoreSpec` for create and update with the update method
     ignoring create-fixed fields. Fewer types than (a) but the ignoring is
     exactly the silent-no-op class the IMPL-0007 review caught.

2. **Typed-field extent on the specs** — which of the 61 params get fields?
   - **a (recommended): the read type's modelled set plus the zfspool consumer
     case (`Sparse`, `Blocksize`) plus `Digest` on the read type; everything
     else via `Extra`.** Matches the add-on-demand policy the `Datastore` read
     type already follows, and every field shipped is one the issue's consumer
     exercises — nothing speculative.
   - b: strictly the read type's current set; `sparse`/`blocksize` ride `Extra`.
     Smallest diff, but the very first consumer immediately needs the escape
     hatch for its main use case, which is the signal the issue itself flags
     ("worth considering modeling it ... while in here").
   - c: model a broader tranche now (`format`, `preallocation`, `krbd`,
     `monhost`, `username`, `snapshot-as-volume-chain`, …). More surface to
     document and keep honest with zero consumers exercising it.

3. **`Content`/`Nodes` representation on the write specs** —
   - **a (recommended): `[]string`, comma-joined after encode; the read type
     keeps its strings.** Set semantics become structural on the write side (the
     drill's finding), and the join mechanics are the established pattern (ZFS
     `Devices`, HA rules, ACME `Nodes`). The read side stays lossless-verbatim,
     which is its contract.
   - b: plain comma-strings mirroring the read type. Symmetric, but hands
     consumers a stringly set to get wrong, and the drill showed they do.
   - c: `[]string` on the read type too. Cleanest end state but a breaking
     read-type change bundled into an otherwise-additive PR, and it would force
     order/normalization decisions onto every existing read consumer.

4. **Create/update return handling** —
   - **a (recommended): both return `*DatastoreWriteResult`.** The schema
     declares the same object on both verbs, and the `encryption-key` member is
     one-shot key material — the `UpdateACMEAccount` audit is the precedent for
     not discarding a declared return. Null-tolerant decode means a node that
     answers null costs nothing.
   - b: `error`-only on both, revisit if anyone needs the result. Smaller
     surface, but it re-creates the exact bug class the last ledger fixed, and
     adding the return later is a signature break.
   - c: result on create, `error`-only on update. Matches the likeliest usage,
     but the schema says update returns it too, and asymmetry here is a trap for
     the config-regenerating types.

5. **mockpve list-value normalization** —
   - **a (recommended): parse `nodes`/`content` to sets, store and emit
     sorted.** Read-back ≠ submission order (unless coincidentally sorted),
     which is precisely real PVE's behaviour class, and it makes
     order-dependence break in unit tests instead of live — the drill hit this
     three times.
   - b: store and emit verbatim. Byte-exact round-trips are friendlier to naive
     assertions, which is exactly the problem: the mock would promise something
     real PVE does not.

6. **Live-verification target and gate** —
   - **a (recommended): the pvelab nested cluster, zfspool shape.** Create the
     scratch entry over the nested nodes' existing ZFS pool (`rpool/data`; fall
     back to a `dir`-type entry if the lab's install turns out not to be
     ZFS-backed), nodes-restricted — the exact hoomlab shape — behind a new
     `PVE_TEST_DATASTORE=1` gate, env in the lab's env file. Follows the
     move-destructive-tests-off-r740a direction (IMPL-0007 follow-up) even
     though this write is config-only and reversible.
   - b: r740a. The write is reversible and data-untouching, so the blast radius
     is small — but it edits the production cluster's storage.cfg, and the
     follow-up exists because "small" has been wrong before.
   - c: mock-verified only; let hoomlab's first live converge be the
     verification. Cheapest, but it outsources the SDK's evidence to a consumer
     and the repo's honesty rule ("written-but-unverified" labels) exists to
     avoid exactly that.

7. **Convergence helper in the SDK** —
   - **a (recommended): none — primitives only.** Create-if-missing /
     update-if-drifted needs a diff policy (which fields count as drift? is an
     `Extra` key authoritative or inherited?) that is consumer business logic;
     hoomlab's stage owns it. The SDK guarantees the primitives compose: get →
     compare → create/update, with the digest making the read-modify-write safe.
   - b: ship
     `EnsureDatastore(ctx, *DatastoreSpec) (*DatastoreWriteResult, error)` (get
     → create on 404 → else diff+update). Convenient, but the SDK would be
     baking in one drift policy, and no second consumer exists to tell us
     whether it generalizes.

## References

- [GitHub issue #28](https://github.com/donaldgifford/proxmox-go-sdk/issues/28)
  — motivation, consumer, and the proposal this design details.
- `docs/design/0001-package-layout-and-public-contract.md` — service-package
  pattern; storage's cluster-scoped shape.
- `docs/adr/0002-target-proxmox-ve-9x-only.md` — the 9.0 floor (no gate needed
  here).
- `docs/design/0006-…-provider-generic.md` + IMPL-0007 — the
  digest/explicit-delete contract this reuses, and the `UpdateACMEAccount`
  discarded-return precedent behind OQ-4a.
- `cmd/pve-schemadiff/testdata/apidoc-9.2.js.gz` — every wire fact above.
- `proxmox/integration/testdata/cassettes/TestStorageReads.yaml` — the live
  evidence for `digest` on datastore reads.
- hoomlab INV-0001 (hardware drill) — the set-normalization and
  read-shape-vs-write-shape findings behind the mock realism rules.
- `docs/COVERAGE.md` — the three `/storage` gap rows this closes.
