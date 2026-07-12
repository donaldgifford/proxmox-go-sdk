# proxmox-go-sdk — task runner. Run `just` for the menu.

default:
    @just --list

# Compile every package (library build check) + the mockpve helper
build:
    go build ./...

# Run unit tests with race detector + coverage
test:
    go test -race -coverprofile=coverage.txt -covermode=atomic ./...

# Replay the committed go-vcr cassettes with NO live node (CI + local). Runs the
# integration suite against the recorded fixtures in
# proxmox/integration/testdata/cassettes, exercising every phase's SDK request
# paths without a cluster. The gate values below match what each cassette was
# recorded with (node pve; QEMU 9101, LXC/console 9102; ISO storage local); the
# host-agnostic replay matcher makes the endpoint a placeholder. Only the tests
# that have a cassette are run — TestConsoleRFB has none by design (a raw
# websocket byte stream cannot replay) and is excluded.
test-replay:
    #!/usr/bin/env bash
    set -euo pipefail
    iso="${TMPDIR:-/tmp}/pve-replay.iso"
    touch "$iso"
    PVE_REPLAY=1 PVE_NODE=pve \
      PVE_TEST_STORAGE=local-zfs PVE_TEST_VMID=9101 \
      PVE_TEST_LXC_VMID=9102 \
      PVE_TEST_LXC_TEMPLATE=local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst \
      PVE_TEST_CONSOLE_VMID=9102 \
      PVE_TEST_ISO_STORAGE=local PVE_TEST_ISO_PATH="$iso" \
      go test -tags=integration ./proxmox/integration/ \
      -run 'TestVersionRoundTrip|TestComputeReads|TestStorageReads|TestClusterAndHAReads|TestNetworkReads|TestAccessReads|TestQEMULifecycle|TestLXCLifecycle|TestISOUpload|TestConsoleMint'

# Run the mockpve test-helper server locally
run *args:
    go run ./cmd/mockpve {{args}}

# --- pvelab: the nested-PVE dogfood lab (IMPL-0002) -------------------------
# pvelab is a `go run`-only dev tool (design OQ-2, never released). It reads
# pvelab.yaml (git-ignored — copy pvelab.example.yaml) and resolves secrets
# from env-var NAMES in that config. All three recipes touch r740a.

# Prepare the auto-install ISO on the outer host (assistant over SSH)
dogfood-iso *args:
    go run ./cmd/pvelab iso {{args}}

# Provision the nested node VMs, wait ready, write .pvelab-state.json + .pvelab.env
dogfood-up *args:
    go run ./cmd/pvelab up {{args}}

# Tear the lab down (add -force / -purge-isos / -no-state as needed)
dogfood-down *args:
    go run ./cmd/pvelab down {{args}}

# Run the inner suite against the nested lab: sources .pvelab.env (written by
# dogfood-up) and records cassettes (PVE_RECORD=1). Default -run targets the
# two IMPL-0001 live-only criteria; pass extra `go test` flags via args.
dogfood-test *args:
    #!/usr/bin/env bash
    set -euo pipefail
    [ -f .pvelab.env ] || { echo "no .pvelab.env — run 'just dogfood-up' first" >&2; exit 1; }
    source .pvelab.env
    PVE_RECORD=1 go test -tags=integration ./proxmox/integration/ -v -timeout 30m \
      -run 'TestResourceAffinityPlacement|TestConsoleRFB' {{args}}

# Full dogfood cycle: up -> inner suite -> down. Tears down even when the
# suite fails (recorded cassettes + .pvelab-state.json survive for review);
# run the three steps individually to keep the lab alive for debugging.
dogfood:
    #!/usr/bin/env bash
    set -euo pipefail
    just dogfood-up
    trap 'just dogfood-down' EXIT
    just dogfood-test

# Guard the API schema against drift (OQ-7). Runs against the committed synthetic
# fixture by default; point --apidoc at a real Proxmox apidoc.js (from a live 9.x
# node) to guard the live REST surface, and re-run with `-update` to rebaseline.
schemadiff *args:
    go run ./cmd/pve-schemadiff \
        -apidoc cmd/pve-schemadiff/testdata/apidoc.sample.js \
        -baseline cmd/pve-schemadiff/testdata/baseline.json {{args}}

# Lint Go + yaml + Actions workflows + markdown + format
lint:
    golangci-lint run
    yamllint .
    actionlint
    # Forgejo workflows are GitHub-compatible; lint them too once that dir exists
    [ ! -d .forgejo/workflows ] || actionlint .forgejo/workflows/*.y*ml
    markdownlint-cli2 '**/*.md'
    prettier --check '**/*.md'

# Auto-fix lint issues
fmt:
    go fmt ./...
    yamlfmt .
    prettier --write '**/*.md'

# Tag + push a release (triggers goreleaser via CI on tag push)
# Example: just release v0.1.0
release version:
    git tag -a {{version}} -m "release {{version}}"
    git push origin {{version}}
