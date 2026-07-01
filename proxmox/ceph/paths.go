package ceph

import (
	"net/url"
	"strconv"
)

// Ceph endpoints are node-scoped in the REST tree (/nodes/{node}/ceph/…) even
// though the cluster is a single entity — any Ceph MON node answers. The paths
// are provisional (see doc.go): the features are baseline 9.0 (Squid), but the
// exact segments are unconfirmed against a live cluster.

func nodeCephPoolsPath(node string) string { return "/nodes/" + node + "/ceph/pools" }

func nodeCephPoolPath(node, name string) string {
	return nodeCephPoolsPath(node) + "/" + url.PathEscape(name)
}

func nodeCephOSDsPath(node string) string { return "/nodes/" + node + "/ceph/osd" }
func nodeCephOSDPath(node string, osdID int) string {
	return nodeCephOSDsPath(node) + "/" + strconv.Itoa(osdID)
}

func nodeCephStatusPath(node string) string { return "/nodes/" + node + "/ceph/status" }
func nodeCephConfigPath(node string) string { return "/nodes/" + node + "/ceph/config" }
