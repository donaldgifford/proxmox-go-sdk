# CLAUDE.md

Per-repo orientation for `donaldgifford/proxmox-go-sdk`. This file is a
Go-shaped overlay on top of the universal homelab `CLAUDE.md` (see
[homelab/docs](https://github.com/donaldgifford/docs)); the universals apply
here too — only Go-specific guidance is captured below.

## What this is

`proxmox-go-sdk` is a **Go library** (SDK) for Proxmox VE 9.x, maintained as
part of the homelab fleet. It is the standalone, provider-specific Proxmox SDK
decided in ADR-0001; the VM service (`pegaprox-go`, a separate repo) is its
first consumer.

- **It is a library, not a service.** There is no long-running binary to deploy;
  consumers `go get` a pinned tag and import the packages.
- **The public API lives under `proxmox/`** — the unified client
  (`proxmox.NewClient`, `proxmox.Client`) plus typed per-domain service packages
  (`proxmox/qemu`, `proxmox/lxc`, `proxmox/storage`, `proxmox/ha`, …). The repo
  root is a doc-only `package sdk`.
- **It ships `mockpve`** — an in-memory PVE responder (`proxmox/mockpve`,
  importable) so consumers integration-test without a live cluster. It is also
  runnable as a standalone server via `cmd/mockpve`, which is the only _shipped_
  binary/container this repo produces (a test helper, not the SDK). `cmd/pvelab`
  also exists but is a `go run`-only dev tool (DESIGN-0002 OQ-2) — never
  released, never in goreleaser.
- **Targets Proxmox VE 9.x only** (ADR-0002): 9.0 floor, per-minor capability
  gating. No 8.x.
- Lives on Forgejo (`github.com/donaldgifford/proxmox-go-sdk`); a
  `.github/workflows/` mirror exists so the repo can also build on GitHub.

The design is settled in `docs/`: ADR-0001 (SDK split), ADR-0002 (9.x-only),
DESIGN-0001 (package layout / public contract), IMPL-0001 (capability ledger).
Read those before changing the public surface.

## Layout

```text
doc.go                  # module root — doc-only `package sdk`, points at proxmox/
proxmox/                # the SDK (its own module on the eventual repo split)
├── proxmox.go          # unified Client, NewClient, accessors
├── options.go errors.go
├── api/                # low-level transport: DoRequest, ExpandPath, conn, auth, retry
├── version/ tasks/     # capability gating; UPID waiters
├── qemu/ lxc/ storage/ nodes/ cluster/ access/
├── ha/ sdn/ ceph/ pbs/ console/ metrics/ firewall/   # remaining services
├── ssh/                # SFTP/exec side-channel for non-REST ops
├── mockpve/            # in-memory PVE responder for consumer tests (public)
└── internal/           # unexported helpers ONLY (0/1-bool, marshalling, redaction)
cmd/mockpve/            # runnable mockpve server (the only SHIPPED binary; a test helper)
cmd/pvelab/             # nested-PVE dogfood lab CLI (go run-only dev tool, IMPL-0002)
├── main.go             # subcommand dispatch: iso / up / down / status / env
└── lab/                # importable logic (config/iso/answers/provision/teardown/state)
cmd/pve-schemadiff/     # apidoc.js schema-drift guard (CI)
hack/pvelab-spike/      # Phase 0 spike driver (superseded record, do not grow)
Dockerfile              # builds the mockpve image (NOT an SDK service image)
.goreleaser.yml         # ships the mockpve helper binary + checksums/SBOM/sigs
mise.toml               # pinned go + golangci-lint + goreleaser + universal tools
justfile                # `just` task runner — `just` for the menu
.forgejo/workflows/     # CI (Forgejo Actions) — primary
.github/workflows/      # CI (GitHub Actions) — mirror
```

The whole SDK lives under `proxmox/` so it lifts cleanly into its own module
when the repos split (DESIGN-0001). New public packages are admitted under
`proxmox/` per the DESIGN-0001 layout — keep the root doc-only.

## Workflows

### Build + run

- `just build` — `go build ./...` (compiles every package + the mockpve helper)
- `just run -- <args>` — runs the mockpve server via `go run ./cmd/mockpve`
- `just test` — race detector + coverage to `coverage.txt`

### Lint + format

- `just lint` — `golangci-lint run` + yamllint + actionlint + markdownlint +
  prettier (covers the universal linters too). `yamllint` ignores the workflow
  dirs (`actionlint` owns those) and `mkdocs.yml`.
- `just fmt` — `go fmt ./...` + yamlfmt + prettier `--write`. yamlfmt excludes
  the go-vcr cassettes (committed as-recorded) and `mkdocs.yml`.

### Dogfood lab (pvelab, IMPL-0002)

- `pvelab` provisions an ephemeral nested-PVE cluster on the outer host so the
  live-only IMPL-0001 criteria (P4 HA placement, P6 VNC/RFB) can be verified
  without touching real guests. Run via `just dogfood-iso` / `dogfood-up` /
  `dogfood-down` (all touch r740a — Donald runs these). The recipes run the
  **stable-pinned** pvelab (`justfile` var `pvelab_pin`, IMPL-0002 Phase 4:
  released code provisions, branch code is what gets tested); set `PVELAB_DEV=1`
  to run the branch's `./cmd/pvelab` when developing the harness itself.
- Config is `pvelab.yaml` (git-ignored; copy `pvelab.example.yaml`). Secrets are
  env-var NAMES in the config, resolved+validated at load; site topology stays
  out of the repo. `TestExampleConfigValid` pins the example to the schema.
- **Blast-radius guards**: config validation refuses node VMIDs outside the
  reserved 9200–9399 block; teardown re-checks the range AND refuses any VM
  whose live name lacks the `pvelab-` prefix (`ErrNotOurs`, never skippable by
  `-force` — Force forgives "already gone", never "not ours"); `up` never adopts
  leftover VMIDs.
- `up` writes `.pvelab-state.json` (schema-versioned, updated after every stage)
  and `.pvelab.env` (the inner suite's `PVE_*` env, 0600 — carries the nested
  root password); `down` removes both unless `-no-state`, and always deletes
  from **config**, so a lost state file never strands VMs.
- The install flow is the 2026-07-10 amendment: ONE http-mode ISO per PVE
  version (`pvelab iso`, assistant over the ssh side-channel) + an embedded
  answer server during `up` that matches installer requests by the
  `smbios1: serial=<node>` stamped at VM create. The POST payload shape,
  HTTP-vs-HTTPS, and nested→workstation reachability are live-verify items for
  the Phase 1 acceptance run.
- **Cluster formation (Phase 2)**: after readiness, `up` forms a quorate cluster
  via the new `cluster` config surface —
  `CreateCluster`/`JoinInfo`/`JoinCluster`/`ListConfigNodes`, both writes
  **fire-and-poll** (response body ignored beyond error status; upstream return
  shapes unverified). Joins run **serialized**; each join's request error is
  swallowed by design (the join restarts the joining node's API daemons
  mid-call) and convergence is decided in two stages per join: the corosync
  nodelist poll, then a `/cluster/status` quorum gate (quorate + members-so-far
  online) before the next join fires — config presence precedes runtime health,
  and a join issued into that settling window fails server-side (found live
  2026-07-12); the last gate doubles as the final quorum check. The lab dials a
  fresh root@pam-password SDK client per poll (tokens don't survive a join).
  mockpve emulates formation with one wire-forced seam: the joining node's
  identity is implicit on real PVE, so tests seed it via `QueueClusterJoin`
  (plus `SetClusterSelfNode`); join-info issues the exported
  `mockpve.ClusterFingerprint`, which the join handler requires.
- **Templates + linked clones (Phase 5)**: `pvelab template build` installs once
  and converts to an outer-host template (`pvelab-tmpl-<version>`, dots dashed;
  VMID from `nested.template`, reserved sub-range 9210–9219 — the new SDK op
  `qemu.ConvertToTemplate` is a maybe-UPID hedge). When the template exists,
  `up` clones instead of ISO-installing (building it IS the opt-in; discovery is
  per-run by name, never state-tracked — templates outlive labs). Clones boot
  the template's baked-in identity, so `up` starts them ONE at a time and
  re-identifies each over SSH at the template's IP (TOFU-pinned for the run; new
  hostname/IP, `ssh-keygen -A`, best-effort pmxcfs node-dir move, reboot) before
  the next. **PVE tolerating that rename is the clone path's load-bearing
  live-verify unknown** — tests pin command sequence and serialization only.
  Teardown safety is structural: `down` only enumerates `cfg.Nested.Nodes`,
  which never contains the template VMID.

### Release

- The **SDK is released by the git tag itself** — consumers pin
  `github.com/donaldgifford/proxmox-go-sdk/proxmox@vX.Y.Z`. There is no binary
  to publish for the library.
- **Releases are automatic, driven by PR semver labels** (found 2026-07-12):
  `release.yml` runs on every merge to main — `pr-semver-bump` reads the merged
  PR's `major`/`minor`/`patch`/`dont-release` label, mints the next `v*` tag,
  and runs `goreleaser release --clean`, which builds and publishes the
  **mockpve** helper (multi-arch archives + checksums + SBOM + GPG signature;
  the workflow imports `GPG_PRIVATE_KEY` and passes `GPG_FINGERPRINT`). Do
  **not** tag manually in normal flow — `just release` is exceptional-recovery
  only; a manual tag desyncs the label-driven bump.
- Version metadata is injected into `cmd/mockpve` via `-ldflags`:
  `main.version`, `main.commit`, `main.date`.
- Cut `v1.0.0` only once the core + compute + storage surfaces are stable
  (DESIGN-0001 rollout). During early co-evolution with the service, label
  `v0.x` bumps per PR and let the service pin a known-good tag (use a local
  `go.mod replace` for in-flight changes).

### Container build

The image packages the **mockpve** test-helper server, not the SDK. Built
locally with:

```bash
docker build -t mockpve:dev \
  --build-arg VERSION=$(git describe --tags --always) \
  --build-arg COMMIT=$(git rev-parse --short HEAD) \
  --build-arg DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ) .
```

The Dockerfile uses BuildKit `--mount=type=cache` for `/go/pkg/mod` and
`/root/.cache/go-build` — first build is cold, subsequent builds reuse the cache
layers.

## Go-specific conventions

- **`internal/` is for unexported helpers, NOT the SDK surface.** This is a
  library: the per-domain services (`proxmox/qemu`, `proxmox/storage`, …) are
  **public** so consumers can import them. `proxmox/internal/` holds only things
  consumers must not depend on (marshalling helpers, log redaction). The
  `0/1`→bool type is **public** — `types.PVEBool` — since consumers embed it in
  config structs. Do not bury service code in `internal/` — that would wall off
  the entire SDK. (This reverses the binary-template default.)
- **`go.mod` go directive matches `mise.toml`** (currently `go 1.26.4`). Bump
  both together — Renovate's Go updater handles `go.mod`; bump `mise.toml` in
  the same commit.
- **No `vendor/`**. Modules are resolved at build time; the Docker cache mount
  handles offline-ish builds of the mockpve image.
- **`slog` for structured logs** — but a _library_ must not configure the global
  logger or log on its own initiative. The SDK takes a consumer-supplied
  `Logger` via `WithLogger` (no-op default) and prescribes nothing; only
  `cmd/mockpve` (`main`) calls `slog.SetDefault`.
- **No `init()` for behavior.** It runs at import time, breaks test isolation,
  and surprises consumers of a library. Wire everything explicitly through
  `NewClient` + functional options.
- **Tests live next to the code** (`foo_test.go` alongside `foo.go`). Unit tests
  run every exported op against `mockpve`. Integration tests that need a live
  9.x node go under `//go:build integration` and run via
  `go test -tags=integration ./...`.
- **Errors wrap with `%w`** and resolve to the SDK's error taxonomy in
  `proxmox/pverr` (`*pverr.Error` + sentinels like `pverr.ErrNotFound`,
  `pverr.ErrTaskFailed`, `pverr.ErrUnsupported`). Classification (HTTP status →
  sentinel) lives in `pverr.Classify`; the transport calls it. Consumers branch
  with `errors.Is` / `errors.As` (DESIGN-0001 / OQ-1).
- **`context.Context` is the first arg of every operation.** No background work
  the caller can't cancel; one `*Client` is safe for concurrent use.

## Implementation status & testing reality

**Phase 1 (foundation) is implementation-complete** — all 9 tasks checked in
IMPL-0001. The build order was `types`+`pverr` → `api` → `version`+`tasks` →
`mockpve` → root `proxmox` → doc.go promotion + runnable Examples. `go build`,
`just lint`, and `just test` (race) are green; every package is doc-promoted and
`go doc ./...` renders cleanly. The phase's two **live-only** Success Criteria
(version round-trip + waiters against a real 9.x node) are mock-verified only —
written-but-unverified here. **Phase 2 (compute) is implementation-complete** —
all 9 tasks done: QEMU CRUD (list/status/config-get-set/create/clone/delete),
power (start/stop/shutdown/reboot/suspend/resume), migrate + disk/NIC
add/resize/remove, snapshots (list/create/rollback/delete; `SnapshotSpec` with
`VMState`, `WithStartAfterRollback`; mockpve appends PVE's synthetic `current`
entry), and guest agent (`AgentPing`, `AgentExec` →pid, `AgentExecStatus`, and
the `AgentExecWait` convenience that polls exec-status with capped backoff). The
9.1+ TPM-state-snapshot capability is surfaced as
`version.Capabilities.TPMStateSnapshots()` (documented on `SnapshotSpec`, not
force-gated since normal snapshots don't need it). The 9.x fine-grained
guest-agent privileges (`VM.GuestAgent.*`) are server-enforced and surface as
`pverr.ErrForbidden` — documented on the agent ops, not gated. Power ops POST to
`/status/{verb}` via a shared `statusAction` helper and use the **action-option
pattern**: per-op functional options (`StopOption`, `ShutdownOption`,
`SuspendOption`) write to an unexported `powerConfig` so the `url.Values` wire
form never leaks into public signatures (the template for every later service's
action-style ops; distinct option types keep an op-irrelevant flag from
compiling). Disk/NIC add+remove are **typed wrappers over `SetConfig`** —
`DiskSpec`/`NICSpec` render the PVE volume/NIC string (`appendOptions` sorts
option keys) and delegate; remove uses `ConfigUpdate{Delete: slot}` (the mockpve
config handler honors `delete`). `ResizeDisk` PUTs `/resize` (synchronous → zero
Ref); `Migrate` POSTs `/migrate` (task runs on the source node; mockpve
relocates the VM record to the target). **Landed so far:** `types` (`PVEBool`,
ID/ref primitives), `pverr` (error taxonomy + `Classify`), and `api` (transport
`DoRequest`/`ExpandPath`, sticky cluster failover, the three `Credentials`
strategies), and `version` (`Capabilities` snapshot, `AtLeast` + named per-minor
gates, `Service` that fetches `/version` and enforces the 9.0 floor), and
`tasks` (UPID parse, `Status`/`Log` reads, `Wait`/`WaitFor` backoff waiters that
surface `pverr.ErrTaskFailed` with the log tail), and `mockpve` (in-memory PVE
responder: an `http.Handler` serving the envelope for `/version`,
`/access/ticket`, and task status/log; `New` + `Seed`/`Add`/`Finish` methods +
the four options; `mock.NewClient()` returns a wired `api.Client` + cleanup;
`RegisterHandler`/`WithCache` are the seams for later services + the recorded
corpus), and the root `proxmox` client (`NewClient` builds the transport, seeds
`Capabilities` from `/version` rejecting < 9.0, exposes
`API`/`Version`/`Capabilities`/`Tasks` accessors + functional options that adapt
to `api.TransportOption`s; per-service accessors land with their services), and
`qemu` — the first **service** package and the template every later service
follows (go-architect-designed). The **service-package pattern**: a
`Service{c api.Client; node string; caps version.Capabilities}` built by
`NewService(c, node, caps)` and reached from the root via `c.QEMU("pve")`; an
exported `API` interface + `var _ API = (*Service)(nil)` as the mockability
seam; unexported `qemuPath()`/`vmPath(vmid)` helpers (path segments past
`/api2/json` are the service's job); reads decode directly, task-returning ops
read the UPID string and return a `tasks.Ref` via a small `toRef` helper. Write
specs (`CreateSpec`/`CloneSpec`/`ConfigUpdate`) model the common fields and
carry an `Extra map[string]string` (`json:"-"`) escape hatch for unmodelled PVE
params. `Config` reads are **lossless** — a custom `UnmarshalJSON` routes
unknown keys into `Config.Extra` (keep `configKnownFields` in sync when adding a
typed field). `mockpve` grows one **built-in per-service file** per service
(`mockpve/qemu.go`, `mockpve/lxc.go`: a per-service slice of `state`, `AddVM`/
`AddContainer` + `SetVMConfig`/`SetCTConfig` seeders, `registerQEMURoutes`/
`registerLXCRoutes` called from `registerRoutes`, handlers that emit a
synthetic-but-parseable UPID via `finishedTask`); this scales to all 12 services
and keeps consumer tests seeding-not-stubbing.

**`lxc` is the second service** (task 6), and adding it triggered the planned
extraction of genuinely-shared logic into **`proxmox/internal/svcutil`** (an
unexported helper package, _not_ public surface): `ErrNilSpec`/`ErrMissingField`
sentinels, `TaskRef(op, upid)` (UPID string → `tasks.Ref`, formerly `qemu`'s
`toRef`), and `EncodeWithExtra(spec, extra)` (JSON-flatten the typed spec to
`url.Values` and merge the `Extra` map on top, formerly `qemu`'s
`encodeWithExtra`). Both `qemu` and `lxc` now call `svcutil.*`; later services
do the same. In the mock, the qemu/lxc config handlers share one
`applyConfigForm(rec, form)` (honors PVE's `delete` param). `lxc` mirrors `qemu`
exactly — same `Service`/`API`/`var _` shape, pointer write specs, lossless
`Config`, and the action-option pattern for `Stop`/`Shutdown`
(`Suspend`/`Resume` are plain). Tiny per-service scaffolding (the power
`*Option` types + `powerConfig`) stays duplicated per service — only non-trivial
shared logic moves to `svcutil`.

**LXC snapshots** (task 7) mirror qemu's surface with two API-driven
differences: a container snapshot has **no `vmstate`** (no live RAM/CPU state to
capture — it is purely a rootfs/mount-point copy), and `DeleteSnapshot` takes a
variadic `DeleteSnapshotOption` (`WithForceDeleteSnapshot` → `force=1`, to drop
a snapshot whose backing volume is already gone). `RollbackSnapshot` keeps the
shared `WithStartAfterRollback`. The **ZFS/btrfs/LVM-thin backing-store
requirement is a server-side constraint** — the SDK cannot pre-validate the
container's storage backend, so an unsupported backend surfaces through the
`pverr` taxonomy (documented on `SnapshotSpec`). In the mock, the lxc snapshot
handlers reuse the shared `snapRecord`/`qemuSnapshotPayload`/`finishedTask`
plumbing (task types `vzsnapshot`/`vzrollback`/`vzdelsnapshot`).

**LXC OCI templates** (task 8) are the SDK's first **version-gated** op:
`PullOCITemplate(ctx, *OCITemplateSpec)` pulls an OCI image
(`Reference`/`Storage`/`Filename`) into a storage's `vztmpl` content and returns
the download task; the resulting volume id is usable as `CreateSpec.OSTemplate`.
The op gates on `s.caps.Require("LXC OCI templates", "9.1")` — a pre-9.1 node
returns a `pverr.ErrUnsupported`-wrapped error _before_ any request (the gate
fires after the nil-spec guard, before field validation). It drives the node's
storage `download-url` endpoint
(`POST /nodes/{node}/storage/{storage}/download-url`, `content=vztmpl`); that
generic storage surface — and its mock handler, parked in `mockpve/lxc.go` for
now — moves to the storage service in Phase 3. This is the template for every
later minor-gated op: gate with `caps.Require`, surface `ErrUnsupported`,
document the tech-preview status on the spec.

Deliberate deviations from DESIGN-0001 (each documented at its call site): (1)
`WithCache` is deferred (nothing consumes a cache yet); (2) `Tasks()` takes no
node (the `tasks.Ref` carries it); (3) **write specs are passed by pointer**
(`Create(ctx, *CreateSpec)`, etc.) not by value as the design's illustrative
signature shows — the structs exceed gocritic's `hugeParam` 80-byte threshold,
and pointer-passing is exactly Uber's "pass large structs by pointer". The
`API`-interface-in-the-implementing-package choice is intentional (DESIGN-0001
pins it as the test-double seam, mirroring `api.Client`), not a stutter to fix.
**Doc Examples** (task 9): each service package carries a runnable package-level
`Example` in `example_test.go`, wired through `proxmox.NewClient` against a
`mockpve.Serve()` URL with an `// Output:` block (the Phase 1 convention) — qemu
shows clone → start, lxc shows create → start. They are documentation _and_
tests. The qemu/lxc `doc.go` overviews are promoted to describe the full landed
surface (no more "lands in a later task" stubs).

**Phase 2 (compute) is implementation-complete** — all 9 tasks checked. The
phase Success Criterion (create → start → snapshot → rollback → stop → delete
end-to-end for both QEMU and LXC) is covered by `TestLifecycleEndToEnd` in each
service's test file — **mock-verified** (the chain runs against `mockpve`,
awaiting every task); a live 9.x node is not reachable here, so the end-to-end
behaviour against real PVE is written-but-unverified.

**Phase 3 (storage) is implementation-complete** — all 7 tasks checked in
IMPL-0001. The Success Criterion narrowed after live verification: **PVE exposes
no storage-level volume-snapshot REST endpoint** (confirmed on live 9.2 node
`r740a` — `/nodes/{node}/storage/{storage}/content` stops at `/{volume}`, no
`/snapshot` child), so the "volume-chain snapshot" half was reclassified to
documented `ErrUnsupported` (see the `storage.VolumeSnapshots` reclassification
note below). What's live-verified is the **ISO upload** half (`TestISOUpload`
passes end-to-end on `r740a`); the runnable `storage` `Example` now shows upload
→ volume create → delete. The `storage` service was **go-architect designed**
and differs from compute in one structural way: `c.Storage()` is **not
node-scoped** (DESIGN-0001) — `storage.Service{c, caps}` holds no node;
cluster-scoped datastore config reads (`/storage`, `/storage/{id}`) take no
node, while per-node status/content/volume/upload/zfs ops take `node` as a
per-call arg. `Datastore` reads are lossless (custom `UnmarshalJSON` → `Extra`;
`datastoreKnownFields` kept in sync). `ListContent` filters via functional
options (`WithContentType`/`WithVMID`) that build a `?content=…&vmid=…` query
appended to the GET path (GET bodies aren't form-encoded, so query goes in the
path; the mock reads `r.URL.Query()`). Volids are single path segments escaped
with `url.PathEscape` (colon→`%3A`, slash→`%2F`); Go's ServeMux `{volid}` +
`PathValue` round-trips them (verified). `mockpve/storage.go` adds
`storageState` (cluster `stores` + per-node `content` + per-node `zfsPools`),
`AddStorage`/`AddVolume`/`AddZFSPool` seeders, and `registerStorageRoutes`. The
later tasks (volume CRUD, volume-chain snapshots gated on 9.1, **streaming
upload via a new `api.Client.DoUpload`**, ssh/SFTP side-channel, ZFS pools)
followed the design captured in the storage-module-architecture memory; several
PVE 9.x endpoints there were unconfirmed (no apidoc in-repo) and are kept
minimal / stubbed with `ErrUnsupported` + documented (volume resize/move,
volume-chain-snapshot paths, RAIDZ expansion).

Task 2 (volume create/resize/delete/move) clarified a PVE-API reality: **PVE has
no storage-level resize or move endpoint** — only volume _allocate_
(`POST .../content`, synchronous → returns the new volid) and _free_
(`DELETE .../content/{volid}` → task) live on the storage API. So
`storage.CreateVolume` returns a `string` volid (not a task) and
`storage.DeleteVolume` returns a `tasks.Ref`; resize/move are **guest-scoped** —
`qemu.ResizeDisk` (Phase 2) and the new **`qemu.MoveDisk`**
(`POST /qemu/{vmid}/move_disk`, `MoveDiskSpec`). Don't add fake storage
resize/move endpoints.

Task 3 (volume-chain snapshots) was **reclassified to `ErrUnsupported` after
live verification**. It originally shipped
`VolumeSnapshots`/`CreateVolumeSnapshot`/ `DeleteVolumeSnapshot` gated on
`caps.Require("volume-chain snapshots", "9.1")` against a **guessed** path
`…/content/{volid}/snapshot`. That path does not exist: on live 9.2 node
`r740a`, `grep …/content` in the node's own `apidoc.js` shows the storage
content API stops at `/nodes/{node}/storage/{storage}/content/{volume}` — no
`/snapshot` child. Per the honesty rule (the `ExpandRAIDZ`/`ArmHA`/RBD-mirroring
precedent), the three ops now **always return `pverr.ErrUnsupported`** (no
version gate, no request), with docs redirecting callers to
`qemu.CreateSnapshot`/`lxc.CreateSnapshot` — the guest snapshot API is what
actually drives the 9.1 volume-chain mechanism underneath on supported storage;
raw storage-plugin ops go via the ssh side-channel. The
`VolumeSnapshot`/`VolumeSnapshotSpec` types are retained (for a future PVE
release that may add the endpoint).
`version.Capabilities. VolumeChainSnapshots()` **stays** but is now documented
as an _informational_ capability (does the storage support guest snapshots at
all), not a gate for a storage endpoint. The mock's `…/content/{volid}/snapshot`
routes/handlers + `volSnapRecord`/`volSnapshotPayload` types were removed
(mockpve mirrors real PVE); the `volumeSnapshotsPath` helper is gone. Unit
guard: `TestVolumeSnapshotsUnsupported`. No live volume-snapshot integration
test (nothing hits the node); `PVE_TEST_VOLID` retired.

Task 4 (ISO/disk-image streaming upload) extended the **transport**: the
`api.Client` interface gained
`DoUpload(ctx, path, body io.Reader, contentType, out)` — a multipart POST that
applies the same auth+CSRF as a write but **does not retry** (an upload body is
a single-use stream; a failed upload must be restarted by the caller). Only
`*transport` implements `api.Client`, so the interface grew safely.
`storage.UploadISO`/`UploadDiskImage` build the multipart body with an
`io.Pipe` + `multipart.Writer` in a goroutine so the file is **never buffered
whole**; on a DoUpload error the read end is closed to unblock the writer
goroutine (closed through an `io.Closer`-typed value to satisfy errcheck's
`check-blank`, since `(*io.PipeReader).Close` isn't matched by the
`(io.Closer).Close` exclude). The mock's `NewClient` returns the real transport,
so DoUpload is exercised end-to-end; `TestDoUpload` in the api package covers
the auth/Content-Type/stream path directly.

Task 5 (snippet/backup upload) added the **`proxmox/ssh` side-channel** — the
non-REST path for the handful of ops the API can't do. It is deliberately
separate from the REST transport: `golang.org/x/crypto/ssh` (aliased `gossh`) +
`github.com/pkg/sftp`, with `UploadSnippet`/`UploadBackup` (SFTP create + ctx-
aware `io.Copy`) and `Exec` (one-shot command). Reached via `Client.SSH(opts…)`,
which returns a **fresh, single-connection `*ssh.Client`** (NOT concurrency-safe
like the REST services — Connect, use, Close from one goroutine). **Host-key
verification is mandatory**: `Connect` errors unless one of
`WithKnownHostsFile`/ `WithHostKey`/`WithHostKeyCallback` is set (no
`InsecureIgnoreHostKey` — dodges gosec G106). Fire-and-forget cleanup closes go
through a `closeQuietly(io.Closer)` helper (concrete `Close()` calls aren't
matched by errcheck's `(io.Closer).Close` exclude); the `Exec`/`upload`
cancellation goroutines now wait for their watcher to exit before returning (no
goroutine outlives the call). Tested against an **in-process SSH+SFTP server
over a loopback TCP listener** — `net.Pipe` deadlocks pkg/sftp's concurrent
request packets; the listener must be created with
`(&net.ListenConfig{}).Listen(ctx, …)` (noctx rejects bare `net.Listen`). Live
PAM auth + writes under `/var/lib/vz` are unverifiable here.

Task 6 (ZFS pool ops) added node-scoped `ListZFSPools`/`GetZFSPool`/
`CreateZFSPool` over `/nodes/{node}/disks/zfs` (`CreateZFSPool` returns a
`tasks.Ref`; its `Devices []string` is `json:"-"` and joined into PVE's
comma-separated `devices` form param after `EncodeWithExtra`, since the flat
encoder can't render a slice). `GetZFSPool` returns a `ZFSPoolStatus` with a
recursive `ZFSVdev` tree (the parsed `zpool status`). **RAIDZ expansion has no
PVE REST endpoint** — it's a `zpool attach`, so `ExpandRAIDZ` is gated on the
new `version.Capabilities.ZFSRAIDZExpansion()` (9.2) but **always** returns a
documented `pverr.ErrUnsupported` directing callers to the ssh side-channel,
rather than fabricating a path that would 404. Its signature still returns a
`tasks.Ref` so a real REST impl can land later non-breaking. mockpve gained
`AddZFSPool` + the three disk/zfs routes.

**Phase 4 (HA/scheduling/replication) is implementation-complete** — all 7 tasks
checked in IMPL-0001; `ha` is doc-promoted with a runnable resource-affinity
`Example` (`go doc`-verified). The Success Criterion (define a resource-affinity
rule + read it back) is mock-verified; the _placement-honored_ observation is
live-only (the mock does not schedule) and stays written-but-unverified.
go-architect designed (see the ha-module-architecture memory). The `ha` service
is **cluster-scoped** (like storage but every endpoint is under `/cluster/ha`):
`c.HA()` takes no node. Structural rule for this phase: **all `/cluster/ha` +
`/cluster/replication` config writes are synchronous** (PVE returns 200 with
null data, no UPID), so those ops return `error`, **never `tasks.Ref`** — do not
thread tasks into HA. Model the **9.x HA rules, never the deprecated
`/cluster/ha/groups`**. Task 1 (HA resources) landed the cluster-scoped
`Service`/`API`, `HAResource` (lossless read via `UnmarshalJSON` +
`haResourceKnownFields`), `HAResourceSpec`/`Update`, and CRUD over
`/cluster/ha/resources`; SIDs (`vm:100`) carry a colon so path segments use
`url.PathEscape`. mockpve gained `haState` + `AddHAResource` +
`registerHARoutes`. The two unconfirmed 9.2 endpoints are decided up front:
**Dynamic Load Balancer = REST-with-caveat** (provisional
`/cluster/ha/lbalancer` path, gated + documented), **Arm/Disarm = documented
`ErrUnsupported` stub** (no known REST path). Task 2 (HA rules) added `RuleType`
(node-affinity / resource-affinity — the 9.x replacement for the deprecated
groups, never modelled) + lossless `HARule` + `HARuleSpec`/`Update` (the
`Nodes`/`Resources` `[]string` fields are `json:"-"` and CSV-joined after
`EncodeWithExtra`, same as storage ZFS `Devices`); disable via
`HARuleUpdate.Disable` (`*types.PVEBool`; PVE stores the disable flag, not
enable). Task 3 (CRS settings) is not under `/cluster/ha` at all — the CRS
scheduler config lives inside datacenter options (`GET`/`PUT /cluster/options`,
the `crs` **compound property-string** `ha=static,ha-rebalance-on-start=1`).
`GetCRSSettings`/`SetCRSSettings` parse/encode that string to typed `Mode` +
`HARebalanceOnStart` (no `Extra` — the body is one `crs=` param, not a flat
form). Task 4 originally landed a REST-with-caveat Dynamic Load Balancer on the
provisional `/cluster/ha/lbalancer` path and Task 5 landed `ArmHA`/ `DisarmHA`
as `ErrUnsupported` stubs — **both were reversed by IMPL-0005 (DESIGN-0004,
2026-07-22) after apidoc mining**: the DLB path does not exist on real 9.2
(INV-0004 F4), so `GetDLBStatus`/`SetDLBConfig` are now documented
`ErrUnsupported` (types retained; `version.Capabilities.DynamicLoadBalancer()`
**removed** — pre-v1 break) pointing at the CRS settings, while arm/disarm
**graduated to real ops**: `ArmHA(ctx)` / `DisarmHA(ctx, mode)` drive the
confirmed `POST /cluster/ha/status/{arm,disarm}-ha` (sync, gated on
`HAClusterSwitch` 9.2; disarm's `ResourceMode` freeze/ignore is REQUIRED on the
wire — the design's optional-param sketch was corrected). IMPL-0005 also added
the status reads — `HAStatusCurrent` (17-field lossless `HAStatusEntry`;
`ArmedState` enum, the arm/disarm observable, rides the **`fencing` row** —
never master, and an idle cluster has no master row at all, reporting `standby`
as its NORMAL fresh state; both live-confirmed 2026-07-23) and
`GetManagerStatus` (the response NESTS: `ManagerStatus` is the live-confirmed
envelope `Manager ManagerState` + `Quorum ManagerQuorum` — quorate arrives as
string "1"; only the inner active-master fields stay provisional) — and
synchronous `MigrateResource`/`RelocateResource` → `*MigrateResult` (lossless;
`BlockingResource.Cause` typed `node-affinity`/`resource-affinity`; never a
`tasks.Ref`). mockpve emulates the armed switch (fencing row) + status rows +
node moves (no affinity evaluation) and dropped the fabricated lbalancer routes;
`TestHAStatusPathsReal` pins the literal paths (which also exposed that
`url.PathEscape` leaves `:` intact — the wire path is `/resources/vm:100`). Task
6 (replication jobs) added CRUD over `/cluster/replication`
(`ListReplicationJobs`/`Get`/`Create`/`Update`/`DeleteReplicationJob`), lossless
`ReplicationJob` (IDs `<vmid>-<jobnum>`), requires the 9.x `VM.Replicate`
privilege (noted in docs). Task 7 promoted `ha/doc.go` (full overview) and added
the runnable resource-affinity `Example` that satisfies the phase Success
Criterion (mock-verified).

**Phase 5 (network + SDN) is underway** — go-architect designed (see the
network-sdn-module-architecture memory). Three packages: `sdn` (cluster-scoped),
`firewall` (a single `Service` carrying a `Scope` — cluster/node/guest
constructors, one set of methods, `scopePath` switches the prefix; avoids 3x
duplication), and node networking in the existing `nodes` package (OQ-8: node
networking lives in `nodes`, node-scoped). As in HA, SDN/firewall/node-network
config writes are **synchronous** (return `error`, not `tasks.Ref`) — the one
exception is `nodes.ApplyNetworkConfig` (PUT `/nodes/{node}/network`), which PVE
may answer with a reload UPID (9.1+) or null (9.0), so it returns
`(tasks.Ref, error)` and callers check the new `tasks.Ref.IsZero()`. Two
unconfirmed surfaces are decided up front: **SDN Fabrics = REST-with-caveat**
(provisional `/cluster/sdn/fabrics`, 9.2 protocol gate), **SDN status =
`ErrUnsupported` stub**; overlapping-ipset rename is gated 9.1. Task 1 landed
node networking (`Interface` lossless read + CRUD + `ApplyNetworkConfig`) in
`nodes`, plus `tasks.Ref.IsZero()` and the `Nodes()` accessor. Task 2 landed the
cluster-scoped `sdn` package — `Zone`/`VNet`/`Subnet` lossless reads with full
CRUD and a cluster-wide `ApplySDN` (all config writes synchronous), the `SDN()`
accessor, and `svcutil.DecodeExtra` (the shared lossless-read tail, extracted so
the fabrics read type in task 3 reuses it). Task 3 landed SDN **fabrics**
(`Fabric` lossless read + CRUD) with `SDNFabrics`/`SDNAdvancedFabrics` gates —
openfabric/ospf are 9.0 baseline, `FabricProtocolBGP` is refused below 9.2.
**DESIGN-0003 (2026-07-21) remediated the fabric surface**: the original flat
`/cluster/sdn/fabrics` path was a guess that would 404 live — CRUD now targets
the real nested `/cluster/sdn/fabrics/fabric[/{id}]` (INV-0004), `Fabric`
dropped the nonexistent `Nodes`/`Comment` fields and gained
`IPPrefix`/`IP6Prefix`/`RouteFilter` (`redistribute` deliberately unmodelled —
array wire form unverified → `Extra`), and node membership landed as its own
sub-collection (`FabricNode` + `ListFabricNodes`/`Get`/`Create`/`Update`/
`DeleteFabricNode` over `/cluster/sdn/fabrics/node/{fabric}`;
`FabricNodeSpec.Interfaces` sent as repeated form values). Task 4's
`ErrUnsupported` SDN-status stubs were **replaced by real node-scoped reads**
(the runtime surface lives under `/nodes/{node}/sdn`, found via INV-0004):
`SDNStatus(ctx,node)` (zones), `ZoneContent` (per-VNet health — there is NO
per-VNet status endpoint; the `…/{vnet}` path is a subdir index),
`ZoneBridges`/`ZoneIPVRF`/`VNetMACVRF` + fabric runtime
`FabricInterfaces`/`FabricNeighbors`/`FabricRoutes` — all lossless, array-valued
fields (`ports`/`nexthops`/`via`) kept in `Extra` as raw JSON;
`TestFabricPathsReal`/`TestNodeSDNStatusPaths` pin every literal path in-repo.
Task 5 landed the `firewall` package with the **scope model**: ONE
`Service{c,caps,scope}` + three constructors
(`NewClusterScope`/`NewNodeScope`/`NewGuestScope`); rule/IPSet/options methods
written once, `scope.path()` switches the prefix. `RenameIPSet` gated 9.1
(`OverlappingIPSets`); `IPSetEntry` (read) is split from `IPSetEntrySpec`
(write, pointer `NoMatch`). Root accessors `Firewall`/`NodeFirewall`/
`GuestFirewall`; mockpve keys `firewallState` by scope string
("cluster"/"node:pve"/"guest:qemu:100"). Task 6 promoted the `sdn`, `firewall`,
and `nodes` `doc.go` overviews (skeletons → real package docs) and added a
runnable nodes networking `Example`; all render under `go doc ./...`.

**Phase 5 (network + SDN) is COMPLETE** — all 6 tasks checked; enumeration of
zones/VNets/fabrics + full CRUD across every scope is mock-verified. The former
"SDN live status unsupported" caveat is gone: DESIGN-0003 landed the real
node-scoped status reads (paths + shapes confirmed via the 9.2 apidoc), and the
fabric lifecycle is **live-verified** (2026-07-23, pvelab 9.2.2): create +
3-node enrollment (bare-IPv4 `ip`, property-string `interfaces`) + `ApplySDN` +
FRR convergence + teardown. Operational finding: openfabric never binds an
address-less bridge-enslaved port — enroll the addressed bridge (`vmbr0`), not
the raw NIC (the first attempt on `ens18` sat at 0 interfaces for the full poll
ceiling).

**Phase 6 (cluster, access, nodes-admin, Ceph, PBS, console, metrics) is
underway** — go-architect designed it (see the phase6-module-architecture
memory): 8 impl tasks in order cluster → access+tokens → nodes-admin → metrics →
ceph → pbs → console, then doc. Key up-front decisions: `console.Connect` needs
a new `api.Client.DoWebSocket`; PBS wraps the PVE-side only (not the PBS-native
API); new gates `ClearTokenComment`/`OTelExporter` (both 9.1); several
REST-with-caveat surfaces (token-secret rotation, DEB822, SMART, Ceph mirroring)
and one ErrUnsupported stub (metrics OTel config). Task 1 landed the
cluster-scoped `cluster` package (`ListResources`/`GetStatus`/`GetOptions`/
`SetOptions`; `Cluster()` accessor). The mock's `/cluster/options` route is
shared between HA (`crs`) and cluster (other keys) — do NOT double-register it.
Tasks 2+3 landed the cluster-scoped `access` package (`Access()` accessor):
user/group/role CRUD, ACL grant/revoke (`SetACL`, `Delete` revokes), and API
tokens (`CreateToken`/`RegenerateTokenSecret` return the one-time `TokenSecret`;
`ClearTokenComment` gated 9.1, `RegenerateTokenSecret` gated 9.2). `Role`
normalises PVE's two role-read shapes. Added `svcutil`-free mockpve `parseForm`
helper (body-cap + ParseForm) reused by the access handlers. Task 4 extended the
`nodes` package (still node-per-call, no bound node) with node administration —
apt updates + DEB822 repositories (`ListAptUpdates`/`RefreshAptCache`/
`ListRepositories`/`UpdateRepository`), disks + SMART
(`ListDisks`/`GetDiskSMART`/ `InitializeDisk`), node certificates
(`GetNodeCertificates`/ `UploadCustomCertificate`/`DeleteCustomCertificate`) and
cluster-scoped ACME accounts + node ACME certificate order/renew/revoke. DEB822
fields, SMART attribute tables, and the ACME cert task-vs-sync split are
REST-with-caveat (real endpoints, provisional shapes). Custom node scripts have
**no** SDK method — run them over `c.SSH().Exec`. mockpve gained `nodesadmin.go`
(per-node apt/repos/disks/certs + cluster `acmeAccounts`). Task 5 landed the
mixed-scope `metrics` package (`Metrics()` accessor, no bound node): node/guest
RRD (`GetNodeRRD`/`GetVMRRD` with `WithTimeframe`/`WithConsolidation` options) +
`GetNodeStatus` (lossless; pressure-stall + ZFS-ARC in `Extra`,
REST-with-caveat), cluster-scoped metric-server CRUD (`/cluster/metrics/server`,
sync), and OTel `GetOTelConfig`/`SetOTelConfig` that return
`pverr.ErrUnsupported` (9.x OTel is file-configured, no REST; new `OTelExporter`
9.1 gate reserved). mockpve `metrics.go` synthesizes RRD/status statically. Task
6 landed the `ceph` package (`c.Ceph()`, **no** node arg — each op takes the MON
node per-call; flat cluster-wide mock state): pools + OSDs
(create/delete/destroy return `tasks.Ref`), `GetStatus`/`GetClusterConfig`
(lossless; config is verbatim text), recursive CRUSH `OSDTree`. Baseline 9.0, no
gates; paths provisional (centralised in paths.go). **RBD mirroring reclassified
to ErrUnsupported** (no PVE REST endpoint — it's an `rbd`-CLI feature; use SSH),
diverging from the memo's REST-with-caveat guess per the honesty rule. Task 7
landed the `pbs` package (`PBS()` accessor, **PVE-side only** — the PBS-native
datastore API is a future `pbsclient`; mixed scope, no bound node): scheduled
backup jobs (`/cluster/backup`, sync), `ListNodeBackups` (reuses the storage
content route, content=backup), `CreateBackup`→`tasks.Ref`
(`/nodes/{n}/vzdump`), and `RestoreQEMU`/`RestoreLXC`→`tasks.Ref` (reuse the
guest-create endpoints with `archive=`/`ostemplate=`+`restore=1`).
`VerifyBackup` returns ErrUnsupported (PBS-native, no PVE endpoint). Task 8
landed the node-per-call `console` package (`Console()` accessor, no bound
node), split cleanly in two: **ticket mint** is plain sync REST, fully
mock-verified — guest
`MintVNCTicket`/`MintSPICETicket`/`MintTermProxy(node, kind, vmid)` and
node-shell `MintNodeVNC`/`MintNodeTerm(node)` (VNC/term tickets

- SPICE params all lossless); **`Connect(ctx, node, *VNCTicket)`** dials the
  vncwebsocket path the ticket is BOUND to — a guest ticket dials
  `/nodes/{n}/{qemu|lxc}/{vmid}/vncwebsocket` (provenance carried in unexported
  `VNCTicket` fields set at mint), a node-shell or hand-built ticket
  `/nodes/{n}/vncwebsocket`. PVE enforces that binding: presenting a guest
  ticket at the node path is a 401 — found live 2026-07-12 on the pvelab
  cluster; mockpve now binds each minted ticket to its dial path so the
  misrouting can never pass unit tests again. Connect returns the raw duplex
  byte stream. That needed a new
  **`api.Client.DoWebSocket(ctx, path) (io.ReadWriteCloser, error)`** —
  implemented in `api/websocket.go` using Go's **native 101 upgrade** (GET with
  `Connection: Upgrade`/`Upgrade: websocket`/`Sec-WebSocket-*`; on
  `101 Switching Protocols` the `http.Transport` hands back a body that is also
  writable — `resp.Body.(io.ReadWriteCloser)` is the stream). The mock's
  vncwebsocket routes do a real `http.Hijacker` 101 handshake + echo, so
  `Connect` is tested end-to-end through the real transport. The RFB wire
  payload is REST-with-caveat (unverified without a live node);
  `VerifyVNCTicket` returns ErrUnsupported (no standalone verify endpoint — a
  ticket is checked when `Connect` presents it), diverging from the memo's guess
  per the honesty rule. **Breaking-but-safe**: `DoWebSocket` grew the
  `api.Client` interface; only `*transport` implements it, no external doubles.
  Task 9 (doc promotion) closed the phase: every Phase-6 package is doc-promoted
  with a runnable `Example`, `mockpve` gained its own, and the phase success
  flow ships as `proxmox.Example_consoleAndAccess` (mock →
  `Access().ListUsers`/`ListTokens` → `Console().MintVNCTicket`, deterministic
  output). **Phase 6 is implementation-complete** — all 9 tasks checked; the
  only live-only piece is the VNC (RFB) wire payload.

Note the `api.Client` interface now has three verbs beyond `DoRequest`:
`DoUpload` (Phase 3, multipart), `DoWebSocket` (Phase 6, 101 upgrade), plus
`ExpandPath`/`HTTP`. When adding a transport method, add it to the interface
**and** the `websocket_test.go`-style httptest coverage; `gosec`/`gocritic`/
`errcheck check-blank` are all on, and `net.Conn.Close` is NOT covered by the
`(io.Closer).Close` errcheck exclusion — handle hijacked-conn errors explicitly
(log at `s.logger.Debug`), don't `_ =` them.

**No live PVE node and no recorded `go-vcr` cassettes exist in this dev
environment.** This shapes how we test and what "done" means:

- Unit tests run against an in-process `mockpve` responder +
  `net/http/httptest`, not real cassettes. The recorded-corpus →
  fuzzed-`mockpve` pipeline (OQ-4/5/10) is a capture step deferred until a live
  9.x node is reachable; `mockpve` is built so that corpus can seed it later
  without redesign.
- **The go-vcr record/replay harness now EXISTS**
  (`proxmox/integration/ recorder_test.go`, non-tagged so it runs in the default
  suite): a `BeforeSaveHook` redacts secrets (Authorization/Cookie/CSRF headers,
  password/secret/otp form fields, ticket/CSRF/token-value response bodies) to
  `REDACTED` **before** the cassette hits disk; a method+URL matcher
  (`matchMethodURL`) tolerates the redacted headers on replay. `newRecorder`/
  `newRecorderClient` inject a go-vcr `*http.Client` via
  `proxmox.WithHTTPClient` (which bypasses the SDK's TLS opts, so record mode
  applies insecure-TLS to the recorder's real transport instead).
  `TestRedactInteraction` + `TestRecorderRecordReplay` prove
  record→redact→replay end-to-end against `mockpve` (server closed before
  replay) and run in CI — real verification here, no node. **Capturing real
  cassettes is still live-only** (I cannot reach a node); the harness records to
  `proxmox/integration/testdata/cassettes/` under `PVE_RECORD=1`. **Sixteen
  reviewed cassettes are now committed** (force-added past the dir's `*.yaml`
  gitignore): version + per-phase reads, QEMU/LXC lifecycles, ISO upload,
  console mint, HA placement, and the 2026-07-23 pvelab batch (HA status/
  arm-disarm/migrate + SDN status/fabric lifecycle) — each scrubbed of secrets
  (Authorization/token, console VNC ticket+password, LXC password) **and** lab
  topology (endpoint host/IP + node name rewritten to `pve.example`/`pve`; the
  ISO body truncated; the scrub also rewrites go-vcr's parsed `Form` map, a gap
  found in the 2026-07-23 leak review). Batch provenance lives in
  `certification.yaml` (mixed-version corpus by design). **CI replay is now
  wired in**: a `PVE_REPLAY=1` mode in the harness (`newReplayClient`) backs
  each test with its committed cassette in `ModeReplayOnly` against the
  placeholder endpoint, the matcher is host-agnostic (method + path+query,
  renamed `matchMethodURL`→`matchReplayRequest`), and `just test-replay` (the
  `Test Replay (cassettes)` CI job) runs the 16 cassette-backed tests with no
  live node. Recording collapses two identical `/version` fetches into one
  interaction, so `TestVersionRoundTrip` asserts on `NewClient`'s cached caps
  rather than re-fetching (else replay 404s on the second). `TestConsoleRFB` has
  **no** cassette by design (a raw websocket byte stream cannot replay) and is
  excluded from the replay run; the retired `TestResourceAffinityRule` was
  superseded by `TestResourceAffinityPlacement` (IMPL-0002 Phase 3).
  **`TESTING.md`** is the thorough manual walkthrough (token creation → env →
  per-phase lifecycle runs → recording); `DEVELOPMENT.md`'s live-node section
  now points at it. **The recorder must NOT set
  `WithReplayableInteractions(true)`** — a task-status poll loop makes many
  identical GETs to `/tasks/{upid}/status`, and that flag serves the first
  recording ("running") for all of them, so in record mode the task never
  reaches "stopped" and `tasks.Wait` spins to its deadline (found live; guarded
  by `TestRecorderRecordsEachPoll`). Destructive-test teardown uses `cleanupCtx`
  (90s), not `context.Background()`, so a wedged delete fails fast instead of
  hanging to the 10-min binary panic. `PVE_DEBUG=1` streams one `slog` line per
  SDK request (method+URL) — the fastest way to see a silent poll loop.
- **Live verification (user-run, 9.2-1 node `r740a`):** the suite has now run
  end-to-end against real PVE with `PVE_RECORD=1`, and the resulting cassettes
  are committed + replay green in CI (`just test-replay` / the
  `Test Replay (cassettes)` job). **Live-verified:** P1 version round-trip; the
  P2–P6 read criteria (compute/storage/cluster+HA/network/access reads); P2
  **QEMU** lifecycle (create → start → snapshot → rollback-while-running → stop
  → delete) **and P2 LXC** lifecycle; P3 **ISO upload** (drove out the
  chunked-body 501 + redundant-multipart-field 400 bugs); and P6 **VNC ticket
  mint** (`TestConsoleMint`, spins up its own scratch VM). The formerly
  live-only criteria are ALL closed on the pvelab nested cluster: P4
  scheduler-observed resource-affinity placement + the P6 VNC (RFB) wire payload
  (both 2026-07-12, IMPL-0002 Phase 3), and the INV-0004 remediation wave
  (2026-07-23, IMPL-0004/0005 Phase 3): the HA arm/disarm cycle, blocked migrate
  with cause `resource-affinity`, HA/SDN status reads, and the OpenFabric fabric
  lifecycle with FRR convergence. **Nothing on the SDK surface remains
  written-but-unverified.** Volume-chain snapshots are **not** a gap — confirmed
  via `r740a`'s own `apidoc.js` that PVE has no storage-level snapshot endpoint,
  so they were honestly reclassified to `pverr.ErrUnsupported`.
- **Task exit status `WARNINGS: N` is success, not failure.** PVE finishes some
  tasks (routinely an LXC create on a modern-systemd template — e.g. debian-13's
  "Systemd 257 detected. You may need to enable nesting.") with exit status
  `WARNINGS: N`: the operation completed. `tasks.Status.OK()` treats `OK` and
  `WARNINGS: N` as success; `tasks.Status.Warnings()` flags the latter so a
  consumer can log/inspect it. `tasks.Wait` returns **nil** for a warnings task
  (it is a warning, not an error — modelling it as a sentinel error would make a
  naive `if err != nil` fail a benign result). Found live on debian-13 LXC
  create; unit-guarded by `TestWaitWarningsIsSuccess`.
- Integration tests live in `proxmox/integration/` behind
  `//go:build integration`, read the node from `PVE_ENDPOINT` plus one
  credential pair — `PVE_TOKEN_ID`/`PVE_TOKEN_SECRET` (wins) or
  `PVE_USERNAME`/`PVE_PASSWORD` (what `.pvelab.env` uses; tokens don't survive a
  cluster join) — with optional `PVE_NODE` / `PVE_INSECURE_TLS` / `PVE_RECORD` /
  `PVE_SCRUB_EXTRA` (extra live=placeholder recording-scrub pairs). Read-only
  tests cover every phase; destructive tests are env-gated: QEMU lifecycle
  (`PVE_TEST_STORAGE` + `PVE_TEST_VMID`), LXC lifecycle (`+PVE_TEST_LXC_VMID` +
  `PVE_TEST_LXC_TEMPLATE`), ISO upload (`PVE_TEST_ISO_STORAGE` +
  `PVE_TEST_ISO_PATH`), console mint + RFB read (`PVE_TEST_STORAGE` +
  `PVE_TEST_CONSOLE_VMID`, each spins up its own scratch VM), HA placement + HA
  migrate (`PVE_TEST_PLACEMENT_VMID_1/2`, need the quorate pvelab cluster), the
  cluster-wide HA arm/disarm cycle (`PVE_TEST_HA_ARM=1`, an explicit opt-in for
  DISPOSABLE clusters only — never set it on a real node), and the SDN fabric
  lifecycle (`PVE_TEST_FABRIC_NODES` ≥2 + `PVE_TEST_FABRIC_IFACE` — enroll the
  addressed bridge, e.g. `vmbr0`, never an IP-less enslaved NIC). They are **not
  runnable here** — they `t.Skip` without a node. The harness is
  compile-verified (`go vet -tags=integration ./proxmox/integration/`) but its
  execution + the go-vcr cassette capture are live-only. The package keeps an
  untagged `doc.go` so the default `go build ./...` sees a non-empty package.
  Never claim a phase's live-only Success Criteria pass when they cannot be
  verified — mark them written-but-unverified instead.
- Working definition of "done" for a task in this environment: typed op exists,
  `go build ./...` is clean, it is unit-tested against `mockpve`, and
  `just lint`
  - `just test` are green.

## CI matrix

- `.github/workflows/ci.yml` runs on every push/PR — `just test`
  (race+coverage), `just test-replay` (the `Test Replay (cassettes)` job:
  replays the committed go-vcr cassettes through the integration suite with
  `PVE_REPLAY=1`, no live node), `just lint`, schema-drift, security, and a
  goreleaser snapshot. (A `.forgejo/workflows/` mirror is planned but not yet in
  the repo.)
- Release workflows fire only on `v*` tag push; `goreleaser` consumes
  `.goreleaser.yml` and the appropriate token (`GITEA_TOKEN` for Forgejo,
  `GITHUB_TOKEN` for GitHub).
- A schema-drift step (`just schemadiff`, in the test-go job) runs
  `cmd/pve-schemadiff`: it parses a Proxmox `apidoc.js` into a (method, path)
  set and fails CI on drift from the committed baseline
  (`cmd/pve-schemadiff/testdata/baseline.json`). It guards a synthetic fixture
  by default; point `-apidoc` at a real 9.x dump (and `-update` to rebaseline)
  to guard the live REST surface. Parse/diff logic lives in the importable
  `cmd/pve-schemadiff/schema` package and is unit-tested against a fixture.

## Gotchas

- **`go mod tidy` on first scaffold**: the post-create hook runs it
  automatically. If you skip hooks (`--no-hooks`), run it manually before the
  first `just build` or imports will be unresolved.
- **Don't import `proxmox/internal/...` from `cmd/mockpve`.** Go's internal rule
  scopes `proxmox/internal` to `proxmox/...`; `cmd/mockpve` (outside that
  subtree) must depend only on the public `proxmox/mockpve` package.
- **`goreleaser` v2 config**: the v1 → v2 migration moved `archives[].format` to
  `archives[].formats` (slice). Validate with `goreleaser check`.
- **Distroless `nonroot` UID is 65532**. If mockpve needs to write state, mount
  a writable volume — the rootfs is read-only.
- **goreleaser + Forgejo**: the v6 action defaults to GitHub-shaped release
  URLs. The `gitea_urls` block in `.goreleaser.yml` is commented by default —
  uncomment for Forgejo releases, and ensure `GITEA_TOKEN` is set in repo
  Secrets (PAT with `write:repository`).
- **Test-writing lint rules** (lots of `_test.go` files land this phase, so bake
  these in up front): `golangci-lint`'s `noctx` rejects bare
  `httptest.NewRequest` — use
  `httptest.NewRequestWithContext(context.Background(), …)`. `gocritic`'s
  `httpNoBody` rejects a `nil` request body — pass `http.NoBody`. `revive` flags
  unused method receivers and unused func params: drop the receiver name
  (`func (*T) m()`) and rename unused handler params to `_`
  (`func(w http.ResponseWriter, _ *http.Request)`).

## Renovate

- `go.mod` updates are PR'd by Renovate's Go module manager.
- Container base images in `Dockerfile` are PR'd by the Docker manager.
- `mise.toml` versions are handled by a custom regex manager configured upstream
  in `donaldgifford/renovate-config`.
