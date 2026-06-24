# proxmox-go-sdk — task runner. Run `just` for the menu.

default:
    @just --list

# Compile every package (library build check) + the mockpve helper
build:
    go build ./...

# Run unit tests with race detector + coverage
test:
    go test -race -coverprofile=coverage.txt -covermode=atomic ./...

# Run the mockpve test-helper server locally
run *args:
    go run ./cmd/mockpve {{args}}

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
