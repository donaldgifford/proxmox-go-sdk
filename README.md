# proxmox-go-sdk

[![codecov](https://codecov.io/gh/donaldgifford/proxmox-go-sdk/graph/badge.svg?token=NGC0TRYLZS)](https://codecov.io/gh/donaldgifford/proxmox-go-sdk)
[![Go Reference](https://pkg.go.dev/badge/github.com/donaldgifford/proxmox-go-sdk/proxmox.svg)](https://pkg.go.dev/github.com/donaldgifford/proxmox-go-sdk/proxmox)

An idiomatic Go SDK for Proxmox VE 9.x. The public API lives in the
[`proxmox`](https://pkg.go.dev/github.com/donaldgifford/proxmox-go-sdk/proxmox)
package — a unified client plus typed per-domain services (`qemu`, `lxc`,
`storage`, `ha`, …) — with an importable in-memory mock
([`mockpve`](https://pkg.go.dev/github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve))
so consumers can integration-test without a live cluster.

## Install

```sh
go get github.com/donaldgifford/proxmox-go-sdk/proxmox@latest
```

## Quickstart (development)

```sh
mise install                  # toolchain
just                          # task menu
just build                    # compiles every package + the mockpve helper
just test                     # race + coverage
just run -- --help            # run the mockpve server via `go run`
```

See [DEVELOPMENT.md](DEVELOPMENT.md) for the full workflow and
[TESTING.md](TESTING.md) for live-node testing + cassette recording.

## Release

Releases are **automatic**: merging a PR to main mints the next tag from the
PR's semver label (`major`/`minor`/`patch`/`dont-release`) and CI runs
goreleaser — no manual tagging.

The **SDK is released by the git tag itself** — consumers pin
`github.com/donaldgifford/proxmox-go-sdk/proxmox@vX.Y.Z`; there is no library
binary. The tag also builds the **mockpve** test-helper: multi-arch archives
land on the Forgejo (or GitHub) release page, with version metadata (`version`,
`commit`, `date`) embedded via `-ldflags`.

## Container

The image packages the **mockpve** test-helper server (not the SDK):

```sh
docker build -t mockpve:dev \
  --build-arg VERSION=$(git describe --tags --always) \
  --build-arg COMMIT=$(git rev-parse --short HEAD) \
  --build-arg DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ) .
```

Image is distroless + nonroot; entrypoint is `mockpve`.

## Layout

```text
doc.go                  module root — doc-only package, points at proxmox/
proxmox/                the SDK: unified client + per-domain services
├── api/                low-level transport (auth, retry, failover)
├── qemu/ lxc/ storage/ ha/ sdn/ …   typed per-domain services
├── mockpve/            importable in-memory PVE responder for tests
└── integration/        live-node suite + recorded go-vcr cassettes
cmd/mockpve/            runnable mockpve server (the only SHIPPED binary)
cmd/pvelab/             nested-PVE dogfood lab CLI (go run-only dev tool)
Dockerfile              multi-stage distroless build (mockpve image)
.goreleaser.yml         release config
mise.toml               pinned toolchain
justfile                task runner
```

## Conventions

See `CLAUDE.md` for the full operating notes (Go-specific + homelab universals).

## License

Apache-2.0
