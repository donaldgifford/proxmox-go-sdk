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
// Beyond CRUD the Service covers the container lifecycle: power transitions
// (Start/Stop/Shutdown/Reboot/Suspend/Resume, with per-op options such as
// WithStopTimeout) and snapshots
// (Snapshots/CreateSnapshot/RollbackSnapshot/DeleteSnapshot, which require a
// ZFS/btrfs/LVM-thin backing store). PullOCITemplate pulls an OCI image as a
// container template; it is gated on PVE 9.1 and returns a
// pverr.ErrUnsupported-wrapped error on older nodes. See the package Example
// for a runnable create → start flow.
//
// See docs/design/0001-proxmox-sdk-package-layout.md and
// docs/impl/0001-proxmox-ve-9x-sdk-coverage.md.
package lxc
