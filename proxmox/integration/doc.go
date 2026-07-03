// Package integration holds the opt-in, live-node integration suite for the
// SDK (OQ-5 / IMPL-0001 Testing Plan). Every test is guarded by the
// `integration` build tag and runs only when a live Proxmox VE 9.x node is
// configured through the environment, so the default `go test ./...` never
// touches it:
//
//	PVE_ENDPOINT=https://pve.example:8006 \
//	PVE_TOKEN_ID='root@pam!sdk' PVE_TOKEN_SECRET=… \
//	go test -tags=integration ./proxmox/integration/...
//
// Without those variables each test t.Skip()s. Read-only tests (version,
// listings, ticket mint) are safe against any cluster; destructive lifecycle
// tests additionally require PVE_TEST_* variables (storage, template) and skip
// otherwise, so a careless run cannot mutate a real cluster. See the package
// test files and CLAUDE.md for the full variable list.
//
// The recorded-cassette replay path (go-vcr for CI, the second half of OQ-5)
// is deferred until a live node is reachable to capture a corpus; this suite is
// the build-tag half of that resolution.
//
// This file is untagged on purpose: it keeps the package non-empty for the
// default `go build ./...`, since every other file here is behind the build tag.
package integration
