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

Phase 1 (foundation) is underway — track progress in IMPL-0001's checkboxes.
Build order is dependency-driven: `types`+`pverr` → `api` → `version`+`tasks` →
`mockpve` → root `proxmox` → doc.go promotion. **Landed so far:** `types`
(`PVEBool`, ID/ref primitives), `pverr` (error taxonomy + `Classify`), and `api`
(transport `DoRequest`/`ExpandPath`, sticky cluster failover, the three
`Credentials` strategies). Next: `version`, `tasks`.

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
