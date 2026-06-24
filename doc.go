// Package sdk is the module root for the Proxmox VE 9.x Go SDK. It carries no
// API surface itself — the client lives in package proxmox:
//
//	import "github.com/donaldgifford/proxmox-go-sdk/proxmox"
//
//	client, err := proxmox.NewClient(ctx, endpoint, creds)
//
// Typed per-domain services hang off the client (proxmox/qemu, proxmox/lxc,
// proxmox/storage, proxmox/ha, ...); operations that start a PVE task return a
// tasks.Ref the caller awaits. An in-memory PVE responder for consumer tests
// lives in proxmox/mockpve and is also runnable as a standalone server via
// cmd/mockpve.
//
// See docs/design/0001-proxmox-sdk-package-layout.md for the public contract,
// docs/impl/0001-proxmox-ve-9x-sdk-coverage.md for the capability ledger, and
// docs/adr/ for the decisions behind the SDK split and the PVE 9.x-only floor.
package sdk
