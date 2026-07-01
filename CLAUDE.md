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
  runnable as a standalone server via `cmd/mockpve`, which is the _only_
  binary/container this repo produces (a test helper, not the SDK).
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
cmd/mockpve/            # runnable mockpve server (the only binary; a test helper)
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
- `just fmt` — `go fmt ./...` + yamlfmt + prettier `--write`.

### Release

- The **SDK is released by the git tag itself** — consumers pin
  `github.com/donaldgifford/proxmox-go-sdk/proxmox@vX.Y.Z`. There is no binary
  to publish for the library.
- `just release v0.1.0` — tag + push. CI picks up the `v*` tag and runs
  `goreleaser release --clean`, which builds and publishes the **mockpve**
  helper (multi-arch archives + checksums + SBOM + signatures) to Forgejo (via
  `GITEA_TOKEN`) or GitHub (via `GITHUB_TOKEN`).
- Version metadata is injected into `cmd/mockpve` via `-ldflags`:
  `main.version`, `main.commit`, `main.date`.
- Cut `v1.0.0` only once the core + compute + storage surfaces are stable
  (DESIGN-0001 rollout). During early co-evolution with the service, tag `v0.x`
  frequently and let the service pin a known-good tag (use a local
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
IMPL-0001 and the Success Criterion (upload ISO → volume-chain snapshot where
supported → clean up) is covered by a runnable `storage` `Example` plus
`TestVolumeSnapshotLifecycle` (mock-verified; live-node behaviour
written-but-unverified). The `storage` service was **go-architect designed** and
differs from compute in one structural way: `c.Storage()` is **not node-scoped**
(DESIGN-0001) — `storage.Service{c, caps}` holds no node; cluster-scoped
datastore config reads (`/storage`, `/storage/{id}`) take no node, while
per-node status/content/volume/upload/zfs ops take `node` as a per-call arg.
`Datastore` reads are lossless (custom `UnmarshalJSON` → `Extra`;
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

Task 3 (volume-chain snapshots) is the SDK's first **storage** version-gated op:
`VolumeSnapshots`/`CreateVolumeSnapshot`/`DeleteVolumeSnapshot` gate on
`caps.Require("volume-chain snapshots", "9.1")` (new
`version.Capabilities.VolumeChainSnapshots()`), bringing snapshots to storage
without native support (thick-LVM, dir/NFS/CIFS via qcow2 chains). **The gate is
the firm, mock-verified part; the storage-level snapshot endpoint shape is
unconfirmed** (no apidoc) — the path `…/content/{volid}/snapshot` mirrors the
guest convention and is documented as needing live-node verification.

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
form). Task 4 (Dynamic Load Balancer, 9.2+) landed `GetDLBStatus`/
`SetDLBConfig` gated on `caps.Require("Dynamic Load Balancer","9.2")` — the
REST-with-caveat approach: provisional path `/cluster/ha/lbalancer`, lossless
`DLBStatus` read (hedges the unconfirmed shape), gate + round-trip
mock-verified. Task 5 (Arm/Disarm) added `ArmHA`/`DisarmHA` + the new
`HAClusterSwitch` (9.2) capability, but there is no confirmed PVE REST endpoint
(a GUI/pvecm action), so both return a documented `pverr.ErrUnsupported` — the
`storage.ExpandRAIDZ` precedent, kept in the interface so test doubles can stub
them. Task 6 (replication jobs) added CRUD over `/cluster/replication`
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
the fabrics read type in task 3 reuses it). Next: task 3 (SDN Fabrics,
REST-with-caveat).

**No live PVE node and no recorded `go-vcr` cassettes exist in this dev
environment.** This shapes how we test and what "done" means:

- Unit tests run against an in-process `mockpve` responder +
  `net/http/httptest`, not real cassettes. The recorded-corpus →
  fuzzed-`mockpve` pipeline (OQ-4/5/10) is a capture step deferred until a live
  9.x node is reachable; `mockpve` is built so that corpus can seed it later
  without redesign.
- Integration tests live behind `//go:build integration`, read the node from
  `PVE_ENDPOINT` / `PVE_TOKEN_ID` / `PVE_TOKEN_SECRET`, and are **not runnable
  here**. Never claim a phase's live-only Success Criteria pass when they cannot
  be verified — mark them written-but-unverified instead.
- Working definition of "done" for a task in this environment: typed op exists,
  `go build ./...` is clean, it is unit-tested against `mockpve`, and
  `just lint`
  - `just test` are green.

## CI matrix

- `.forgejo/workflows/ci.yml` runs on every push/PR — `just test` + `just lint`.
- `.github/workflows/ci.yml` is the mirror; identical jobs, runs on the GitHub
  mirror if/when one exists.
- Release workflows fire only on `v*` tag push; `goreleaser` consumes
  `.goreleaser.yml` and the appropriate token (`GITEA_TOKEN` for Forgejo,
  `GITHUB_TOKEN` for GitHub).
- A `version`-drift step (planned, IMPL-0001) regenerates types from `apidoc.js`
  and fails on schema drift across 9.x minors.

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
