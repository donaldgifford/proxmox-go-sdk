package storage

import "net/url"

// Cluster-scoped datastore configuration.

func datastoresPath() string { return "/storage" }

func datastorePath(storage string) string { return "/storage/" + storage }

// Node-scoped status, content, and volumes. A volid such as
// "local:iso/debian.iso" is a single path segment, so it is percent-escaped
// (url.PathEscape preserves the colon as %3A and the slash as %2F, which is how
// PVE expects volids inside a path).

func nodeStoragesPath(node string) string { return "/nodes/" + node + "/storage" }

func nodeStoragePath(node, storage string) string {
	return nodeStoragesPath(node) + "/" + storage
}

func nodeContentPath(node, storage string) string {
	return nodeStoragePath(node, storage) + "/content"
}

func nodeVolumePath(node, storage, volid string) string {
	return nodeContentPath(node, storage) + "/" + url.PathEscape(volid)
}

// Note: PVE has no storage-level volume-snapshot endpoint (verified against a
// live 9.2 node — the content API stops at .../content/{volume}). The
// storage.VolumeSnapshots family therefore returns ErrUnsupported and needs no
// path helper here; snapshots are driven through the guest (qemu/lxc).

// Node-scoped ZFS pool management: GET/POST /nodes/{node}/disks/zfs and
// GET /nodes/{node}/disks/zfs/{name}. RAIDZ expansion has no confirmed PVE REST
// endpoint (see zfs.go); these two are the established disk-management paths.

func nodeZFSPath(node string) string { return "/nodes/" + node + "/disks/zfs" }

func nodeZFSPoolPath(node, name string) string {
	return nodeZFSPath(node) + "/" + name
}
