# Development

How to set up, build, test, and develop against `proxmox-go-sdk`. This is a Go
**library** (SDK) for Proxmox VE 9.x; the only binary it produces is `mockpve`,
an in-memory PVE responder used as a test helper.

- Using the SDK as a consumer? See [USAGE.md](USAGE.md).
- Contributing changes? See [CONTRIBUTING.md](CONTRIBUTING.md).
- Reporting a vulnerability? See [SECURITY.md](SECURITY.md).

## Prerequisites

- [mise](https://mise.jdx.dev/) — pins and installs the toolchain (Go +
  linters + release tools). This is the only thing you must install by hand.
- `git`.
- Optionally `docker` — only needed to build the `mockpve` helper image.

## Setup

```sh
git clone https://github.com/donaldgifford/proxmox-go-sdk
cd proxmox-go-sdk
mise install     # installs the pinned Go, golangci-lint, git-cliff, syft, ...
just             # show the task menu
```

`mise install` reads `mise.toml`. The Go version there is kept **in lockstep**
with the `go` directive in `go.mod` (currently `1.26.4`) — if you bump one, bump
the other in the same commit.

## Common tasks

Everything is driven by [`just`](https://github.com/casey/just):

| Command               | What it does                                          |
| --------------------- | ----------------------------------------------------- |
| `just build`          | `go build ./...` — compiles every package + `mockpve` |
| `just test`           | `go test -race -coverprofile=coverage.txt ./...`      |
| `just lint`           | golangci-lint + yamllint + actionlint + markdown      |
| `just fmt`            | `go fmt` + yamlfmt + prettier `--write`               |
| `just run -- <args>`  | run the `mockpve` server via `go run ./cmd/mockpve`   |
| `just schemadiff`     | guard the API schema against drift (see below)        |
| `just release vX.Y.Z` | tag + push (CI runs goreleaser)                       |

Run `just fmt` and `just lint` before every commit; run `just test` (race)
before every push.

## Repository layout

```text
doc.go                  # module root — doc-only `package sdk`
proxmox/                # the SDK
├── proxmox.go          # unified Client, NewClient, service accessors
├── options.go errors.go
├── api/                # low-level transport: DoRequest/DoUpload/DoWebSocket, auth, retry
├── version/ tasks/     # capability gating; UPID task waiters
├── qemu/ lxc/ storage/ nodes/ cluster/ access/
├── ha/ sdn/ ceph/ pbs/ console/ metrics/ firewall/
├── ssh/                # SFTP/exec side-channel for non-REST ops
├── pverr/ types/       # error taxonomy; shared primitives (PVEBool, refs)
├── mockpve/            # in-memory PVE responder for tests (public)
├── integration/        # //go:build integration live-node suite
└── internal/           # unexported helpers ONLY (svcutil, redaction)
cmd/mockpve/            # runnable mockpve server (the only binary)
cmd/pve-schemadiff/     # API schema-drift guard
docs/                   # ADRs, DESIGN, IMPL, RFCs
```

Public service packages live directly under `proxmox/`. `proxmox/internal/` is
for helpers consumers must not depend on. See
[docs/design/0001-proxmox-sdk-package-layout.md](docs/design/0001-proxmox-sdk-package-layout.md).

## Testing model

The suite has two tiers:

- **Unit tests** run against the in-process `mockpve` responder +
  `net/http/httptest` — no live cluster, no network. Every exported operation is
  exercised this way. These run in `just test` and in CI.
- **Integration tests** live in `proxmox/integration/` behind a
  `//go:build integration` tag. They read a live node from the environment and
  **skip cleanly when it is not configured**, so they are a no-op by default.

### mockpve

`proxmox/mockpve` is an importable `http.Handler` that speaks enough of the PVE
REST envelope to drive the SDK. It grows one built-in file per service
(`mockpve/qemu.go`, `mockpve/storage.go`, …) with `Add*`/`Set*` seeders. Prefer
**seeding state** over stubbing responses. `mockpve.New()` + `mock.Serve()`
gives you a URL; `mock.NewClient()` returns a wired `api.Client` + cleanup.

### Schema-drift guard

`cmd/pve-schemadiff` parses a Proxmox `apidoc.js` into a (method, path) set and
fails CI on drift from a committed baseline. By default it guards a synthetic
fixture; point `-apidoc` at a real 9.x dump (and `-update` to rebaseline) to
guard the live REST surface.

## Changelog and releases

`CHANGELOG.md` is **generated** from Conventional Commit messages by `git-cliff`
(`cliff.toml`) and guarded by a drift check — do not edit it by hand. Releases
are cut by tagging (`just release vX.Y.Z`); CI runs `goreleaser`, which builds
and publishes the `mockpve` helper binary. The SDK itself is "released" by the
tag — consumers pin `github.com/donaldgifford/proxmox-go-sdk@vX.Y.Z`.

## Continuous integration

- `.forgejo/workflows/` is the primary CI (Forgejo Actions);
  `.github/workflows/` is an identical mirror for GitHub.
- Every push/PR runs `just test` + `just lint`, plus govulncheck, Trivy, CodeQL,
  TruffleHog secret scanning, the schema-drift check, and a goreleaser snapshot.
- A semver label (`major` / `minor` / `patch` / `dont-release`) is required on
  every PR.

---

## Manual verification against a live Proxmox VE 9.x node

> **The SDK's live-only acceptance criteria still require a real PVE 9.x node —
> those remain written-but-unverified, unchanged by this work.**

The SDK was developed without access to a live Proxmox cluster or recorded API
cassettes. Everything is verified against `mockpve`, which is faithful to the
REST envelope but does **not** run a real hypervisor, scheduler, or storage
backend. A set of acceptance criteria can therefore only be confirmed by running
against real hardware. **This section is for you to work through manually on a
reachable 9.x node** (and a 9.2 node for `9.2+` gated operations).

### What is written-but-unverified

These behaviors have SDK code + `mockpve` coverage but have **not** been run
against real PVE:

- The wire round-trip of every operation against a real cluster (auth, envelope,
  UPID parsing, `Extra` field pass-through).
- Any operation on an **unconfirmed / provisional endpoint** (marked
  "REST-with-caveat" in the code and `docs/impl/0001-*`): several storage
  volume-chain-snapshot paths, SDN fabrics, DEB822 repos, SMART, ACME, Ceph
  paths, and the metric-server shapes.
- Operations that return `pverr.ErrUnsupported` because **no PVE REST endpoint
  is confirmed**: SDN live status, `ha.ArmHA`/`DisarmHA`, RBD mirroring, RAIDZ
  expansion, OTel config, PBS-native verify. These need a live node to discover
  the real endpoint (if any).
- The live **VNC (RFB) wire payload** from `console.Connect` (the WebSocket
  upgrade + duplex plumbing is mock-verified; the RFB bytes are not).

### Prerequisites

- A reachable Proxmox VE **9.0+** node (plus a **9.2** node to exercise `9.2+`
  gated ops).
- An API token (`user@realm!tokenid` + secret) with enough privilege for the
  operations you intend to test.
- For self-signed clusters, set `PVE_INSECURE_TLS=1`.

> **Use a scratch cluster / disposable guest IDs.** The destructive tests below
> create and delete VMs, containers, volumes, snapshots, and HA rules. They
> clean up after themselves, but run them somewhere you can afford to break.

### Environment variables

The integration harness (`proxmox/integration/`) is configured entirely through
the environment. Read-only tests need only the first three; each destructive
test is gated behind its own variable and skips when unset.

| Variable                | Required | Purpose                                          |
| ----------------------- | -------- | ------------------------------------------------ |
| `PVE_ENDPOINT`          | yes      | e.g. `https://pve.example:8006`                  |
| `PVE_TOKEN_ID`          | yes      | e.g. `root@pam!sdk`                              |
| `PVE_TOKEN_SECRET`      | yes      | the token's secret                               |
| `PVE_NODE`              | no       | node under test (default `pve`)                  |
| `PVE_INSECURE_TLS`      | no       | `1` to skip TLS verify (self-signed)             |
| `PVE_TEST_STORAGE`      | gate     | target storage for scratch disks / uploads       |
| `PVE_TEST_VMID`         | gate     | scratch QEMU VMID the suite may create/destroy   |
| `PVE_TEST_LXC_VMID`     | gate     | scratch LXC VMID the suite may create/destroy    |
| `PVE_TEST_LXC_TEMPLATE` | gate     | OS template volid for the LXC lifecycle          |
| `PVE_TEST_ISO_PATH`     | gate     | local path to a small ISO to upload (Phase 3)    |
| `PVE_TEST_VOLID`        | gate     | existing volume to snapshot + clean up (Phase 3) |
| `PVE_TEST_HA_SIDS`      | gate     | CSV of ≥2 HA-managed SIDs (Phase 4)              |

### Running the suite

The tests are excluded from the default build; enable them with the
`integration` tag:

```sh
export PVE_ENDPOINT="https://pve.example:8006"
export PVE_TOKEN_ID="root@pam!sdk"
export PVE_TOKEN_SECRET="..."
export PVE_INSECURE_TLS=1          # if self-signed

# read-only tests only (safe on any cluster)
go test -tags=integration ./proxmox/integration/... -run 'Reads|Version' -v

# enable a destructive lifecycle test by setting its gate(s)
export PVE_TEST_STORAGE="local-lvm"
export PVE_TEST_VMID=9101
go test -tags=integration ./proxmox/integration/... -run QEMU -v
```

Compile-check the harness without a node:

```sh
go vet -tags=integration ./proxmox/integration/...
```

### Acceptance-criteria checklist

Work through these against a live node and tick each one off. They map to the
per-phase Success Criteria in `docs/impl/0001-proxmox-ve-9x-sdk-coverage.md`.

- [ ] **Phase 1 — foundation:** auth + `GET /version` round-trips against a live
      9.x node; the task waiters drive a real start/stop task to completion.
- [ ] **Phase 2 — compute:** create → start → snapshot → rollback → stop →
      delete runs end-to-end for **both QEMU and LXC** (`PVE_TEST_STORAGE` +
      `PVE_TEST_VMID`, and `+ PVE_TEST_LXC_VMID` + `PVE_TEST_LXC_TEMPLATE`).
- [ ] **Phase 3 — storage:** upload an ISO (`PVE_TEST_ISO_PATH`), create a
      volume-chain snapshot where supported and clean it up (`PVE_TEST_VOLID`);
      confirm the provisional volume-chain-snapshot endpoint path.
- [ ] **Phase 4 — HA:** define a resource-affinity rule (`PVE_TEST_HA_SIDS`),
      read it back, **and observe the scheduler honor the placement** (the mock
      does not schedule — this half is live-only).
- [ ] **Phase 5 — network/SDN:** enumerate zones / VNets / fabrics without
      error; confirm whether a real **SDN live-status** endpoint exists (the SDK
      currently returns `ErrUnsupported`).
- [ ] **Phase 6 — cluster/access/console:** list users/tokens under the 9.x
      privilege model; mint a VNC ticket and **drive a real RFB session**
      through `console.Connect` (`PVE_TEST_VMID`).
- [ ] **9.2-gated ops:** on a 9.2 node, confirm the real endpoints (or absence)
      behind Dynamic Load Balancer, HA arm/disarm, SDN BGP fabrics, ZFS RAIDZ
      expansion, and token-secret rotation.

### Capturing cassettes for CI (deferred)

Once a live node is reachable, the recorded-corpus → `go-vcr` cassette pipeline
(so CI can replay real responses without a cluster) can be captured and used to
seed `mockpve`. `mockpve` is built to accept that corpus later without redesign.
Until then, `mockpve` remains the source of truth for automated tests.
