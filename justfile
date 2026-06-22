# proxmox-go-sdk — task runner. Run `just` for the menu.

default:
    @just --list

# Build the binary
build:
    go build -o bin/proxmox-go-sdk ./cmd/proxmox-go-sdk

# Run unit tests with race detector + coverage
test:
    go test -race -coverprofile=coverage.txt -covermode=atomic ./...

# Run the binary locally
run *args:
    go run ./cmd/proxmox-go-sdk {{args}}

# Lint Go + yaml + markdown + format
lint:
    golangci-lint run
    yamllint .
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
