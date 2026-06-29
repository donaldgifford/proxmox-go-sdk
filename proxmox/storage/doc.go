// Package storage wraps PVE storage: datastore configuration, per-node status,
// content/volume management, streaming uploads, volume snapshots, and ZFS pools.
//
// Unlike the compute services a storage Service is not bound to a node.
// Datastore configuration is cluster-wide, so ListDatastores/GetDatastore take
// no node; every node-scoped operation (status, content, volumes, uploads, ZFS)
// takes a node argument:
//
//	s := client.Storage()
//	ds, err := s.ListDatastores(ctx)                    // cluster-scoped.
//	vols, err := s.ListContent(ctx, "pve", "local-lvm") // node-scoped.
//
// Reads (ListDatastores, ListContent, …) return data directly; operations that
// start a PVE worker (CreateVolume, uploads, ZFS pool ops) return a tasks.Ref
// the caller awaits with the client's task service. Datastore reads are
// lossless: keys outside the typed set land in Datastore.Extra.
//
// See docs/design/0001-proxmox-sdk-package-layout.md and
// docs/impl/0001-proxmox-ve-9x-sdk-coverage.md.
package storage
