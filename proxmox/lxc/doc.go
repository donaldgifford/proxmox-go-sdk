// Package lxc wraps the PVE LXC container endpoints under /nodes/{node}/lxc.
//
// A Service is bound to one cluster node. Reads (List, Get, Config) return data
// directly; lifecycle and power operations (Create, Clone, Delete, Start, Stop,
// …) return a tasks.Ref the caller awaits with the client's task service:
//
//	ct := client.LXC("pve")
//	ref, err := ct.Create(ctx, &lxc.CreateSpec{
//		VMID:       200,
//		OSTemplate: "local:vztmpl/debian-12-standard_12.2-1_amd64.tar.zst",
//		Hostname:   "web",
//		Storage:    "local-lvm",
//		RootFS:     "local-lvm:8",
//	})
//	if err != nil {
//		// ...
//	}
//	if _, err := client.Tasks().Wait(ctx, ref); err != nil {
//		// ...
//	}
//
// Write specs (CreateSpec, CloneSpec, ConfigUpdate) model the common fields as
// typed struct fields and carry an Extra map for PVE parameters the SDK does not
// model yet (mount points, raw lxc.* entries, …). Config reads are lossless:
// keys outside the typed set land in Config.Extra.
//
// Snapshots and OCI-template creation land in later tasks of the same phase. See
// docs/design/0001-proxmox-sdk-package-layout.md and
// docs/impl/0001-proxmox-ve-9x-sdk-coverage.md.
package lxc
