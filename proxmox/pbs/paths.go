package pbs

import "net/url"

// Cluster-scoped scheduled backup jobs.

func clusterBackupPath() string { return "/cluster/backup" }

func clusterBackupJobPath(id string) string {
	return clusterBackupPath() + "/" + url.PathEscape(id)
}

// Node-scoped backup operations.

func nodeVzdumpPath(node string) string { return "/nodes/" + node + "/vzdump" }

func nodeStorageContentPath(node, storage string) string {
	return "/nodes/" + node + "/storage/" + url.PathEscape(storage) + "/content"
}

// Restore reuses the guest-create endpoints (restore is create-with-archive).
func nodeQEMUPath(node string) string { return "/nodes/" + node + "/qemu" }
func nodeLXCPath(node string) string  { return "/nodes/" + node + "/lxc" }
