// Package storage wraps PVE storage: datastore configuration (reads and
// writes), per-node status, content/volume management, streaming uploads,
// volume snapshots, and ZFS pools.
//
// Unlike the compute services a storage Service is not bound to a node.
// Datastore configuration is cluster-wide, so the datastore reads and writes
// take no node; every node-scoped operation (status, content, volumes,
// uploads, ZFS) takes a node argument:
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
// # Datastore configuration writes
//
// CreateDatastore, UpdateDatastore and DeleteDatastore edit the cluster's
// storage.cfg (POST/PUT/DELETE /storage). All three are synchronous — PVE
// answers immediately, no task — and require the Datastore.Allocate privilege
// on /storage (an ordinary privilege; API tokens work). Creating an entry
// points the cluster at EXISTING backing storage (a ZFS dataset, a directory,
// an export); deleting one removes config, not data — the backing storage and
// every volume on it survive.
//
// The safe update idiom is read-then-guarded-write: a datastore read carries
// the storage.cfg digest (the same value on every entry of one read), and an
// update that passes it in DatastoreUpdate.Digest fails instead of clobbering
// a concurrent edit:
//
//	d, _ := s.GetDatastore(ctx, "fast-vm")
//	_, err := s.UpdateDatastore(ctx, "fast-vm", &storage.DatastoreUpdate{
//		Content: []string{"images"},
//		Digest:  d.Digest,
//	})
//
// List-valued options (Content, Nodes) are sets on the wire: PVE does not
// preserve submission order on read-back, so compare them as sets, never as
// strings. Identity and backing location (type, path, export, …) are fixed at
// creation — changing them means delete and recreate — and clearing a key is
// a named action via DatastoreUpdate.Delete, never an empty-string side
// effect.
//
// Probe existence by scanning ListDatastores, not by calling GetDatastore:
// real PVE answers a missing id with HTTP 500 "storage '<id>' does not
// exist" (never 404, so never pverr.ErrNotFound), and reads can carry
// server-generated keys the writer never sent (a zfspool entry gains
// "mountpoint") — see GetDatastore and Datastore.Extra.
//
// See docs/design/0001-proxmox-sdk-package-layout.md,
// docs/design/0007-cluster-storage-config-writes.md and
// docs/impl/0001-proxmox-ve-9x-sdk-coverage.md.
package storage
