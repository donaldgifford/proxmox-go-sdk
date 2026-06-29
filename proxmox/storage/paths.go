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
