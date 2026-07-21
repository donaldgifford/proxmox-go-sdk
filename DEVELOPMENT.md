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
| `just release vX.Y.Z` | manual tag (recovery only — releases are automatic)   |

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
fails CI on drift from a committed baseline. Since IMPL-0003 it guards the
**real PVE 9.2 REST surface**: `just schemadiff` parses the committed
`cmd/pve-schemadiff/testdata/apidoc-9.2.js.gz` (a genuine dump, gzipped — the
tool gunzips by magic bytes) against `testdata/baseline.json` (675 endpoints;
fetched from the homelab 9.2 node, 2026-07-19).

To refresh the baseline for a new PVE version:

```bash
curl -sk https://<node>:8006/pve-docs/api-viewer/apidoc.js | gzip -9 \
  > cmd/pve-schemadiff/testdata/apidoc-9.2.js.gz
just schemadiff -update   # rewrites baseline.json; review the diff in the PR
```

The endpoint diff in the resulting PR **is** the minor-release API delta —
review it against the capability gates before merging.

## Changelog and releases

`CHANGELOG.md` is **generated** from Conventional Commit messages by `git-cliff`
(`cliff.toml`) and guarded by a drift check — do not edit it by hand. **Releases
are automatic**: on every merge to main, `release.yml` reads the merged PR's
semver label (`major`/`minor`/`patch`/`dont-release`), mints the next `v*` tag,
and runs `goreleaser`, which builds and publishes the `mockpve` helper binary.
Do not tag manually in normal flow (`just release` is recovery-only). The SDK
itself is "released" by the tag — consumers pin
`github.com/donaldgifford/proxmox-go-sdk@vX.Y.Z`.

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
against real hardware.

**→ [TESTING.md](TESTING.md) is the full step-by-step walkthrough** — creating
an API token, configuring the environment, running each lifecycle test one at a
time, and recording `go-vcr` cassettes. This section is only the summary and the
"what's unverified" context.

### What is written-but-unverified

These behaviors have SDK code + `mockpve` coverage but have **not** been run
against real PVE:

- The wire round-trip of every operation against a real cluster (auth, envelope,
  UPID parsing, `Extra` field pass-through).
- Any operation on an **unconfirmed / provisional endpoint** (marked
  "REST-with-caveat" in the code and `docs/impl/0001-*`): several storage
  volume-chain-snapshot paths, DEB822 repos, SMART, ACME, Ceph paths, and the
  metric-server shapes. (SDN fabrics + node-scoped SDN live status graduated:
  their paths and shapes are confirmed against the real 9.2 apidoc, INV-0004;
  runtime semantics await the pvelab live run per DESIGN-0003.)
- Operations that return `pverr.ErrUnsupported` because **no PVE REST endpoint
  is confirmed**: `ha.ArmHA`/`DisarmHA` (upgrading via DESIGN-0004 — the real
  `/cluster/ha/status/{arm,disarm}-ha` endpoints exist in the 9.2 apidoc), RBD
  mirroring, RAIDZ expansion, OTel config, PBS-native verify. These need a live
  node to discover the real endpoint (if any).
- The live **VNC (RFB) wire payload** from `console.Connect` (the WebSocket
  upgrade + duplex plumbing is mock-verified; the RFB bytes are not).

### Running it

The full procedure — token creation, environment setup, the read-only smoke
test, each destructive lifecycle test, the acceptance-criteria checklist, and
recording cassettes — lives in **[TESTING.md](TESTING.md)**. The short version:

```sh
export PVE_ENDPOINT="https://pve.example:8006"
export PVE_TOKEN_ID="root@pam!sdk" PVE_TOKEN_SECRET="…"
export PVE_INSECURE_TLS=1   # if self-signed

# read-only smoke test (safe on any cluster)
go test -tags=integration ./proxmox/integration/... -run 'Reads|Version' -v

# record cassettes while running (secrets are redacted before write)
PVE_RECORD=1 go test -tags=integration ./proxmox/integration/... -run … -v
```

Compile-check the harness without a node:

```sh
go vet -tags=integration ./proxmox/integration/...
```

### Recording (go-vcr)

`PVE_RECORD=1` records each exchange into
`proxmox/integration/testdata/cassettes/` with credentials redacted before
write. The redaction is guarded by `TestRedactInteraction` /
`TestRecorderRecordReplay`, which run in the normal suite (no node needed):

```sh
go test ./proxmox/integration/... -run 'Redact|RecordReplay' -v
```

Cassettes are git-ignored until reviewed; wiring committed cassettes into CI
replay is a planned follow-up. See [TESTING.md](TESTING.md#recording-cassettes).
