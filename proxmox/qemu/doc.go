// Package qemu wraps the PVE QEMU/VM endpoints under /nodes/{node}/qemu.
//
// A Service is bound to one cluster node. Reads (List, Get, Config) return data
// directly; operations that start a PVE worker (Create, Clone, Delete,
// SetConfig when it schedules work) return a tasks.Ref the caller awaits with
// the client's task service:
//
//	q := client.QEMU("pve")
//	ref, err := q.Clone(ctx, 9000, &qemu.CloneSpec{NewID: 131})
//	if err != nil {
//		// ...
//	}
//	if _, err := client.Tasks().Wait(ctx, ref); err != nil {
//		// ...
//	}
//
// Write specs (CreateSpec, CloneSpec, ConfigUpdate) model the common fields as
// typed struct fields and carry an Extra map for PVE parameters the SDK does not
// model yet. Config reads are lossless: keys outside the typed set land in
// Config.Extra.
//
// Power, migration, snapshot, and guest-agent operations land in later tasks of
// the same phase. See docs/design/0001-proxmox-sdk-package-layout.md and
// docs/impl/0001-proxmox-ve-9x-sdk-coverage.md.
package qemu
