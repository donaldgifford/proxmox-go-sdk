package ceph

import (
	"net/url"
	"strconv"
)

// Ceph endpoints are node-scoped in the REST tree (/nodes/{node}/ceph/…) even
// though the cluster is a single entity — any Ceph MON node answers. Every path
// here is confirmed against the real 9.2 apidoc (IMPL-0006): the pool collection
// is SINGULAR (/ceph/pool, not /pools) and the cluster config is /ceph/cfg/raw
// (there is no /ceph/config) — both were wrong until the coverage fabrication
// guard caught them.

// nodeCephPoolsPath is the pool collection. PVE spells it in the singular; a
// plural /ceph/pools 404s.
func nodeCephPoolsPath(node string) string { return "/nodes/" + node + "/ceph/pool" }

func nodeCephPoolPath(node, name string) string {
	return nodeCephPoolsPath(node) + "/" + url.PathEscape(name)
}

func nodeCephOSDsPath(node string) string { return "/nodes/" + node + "/ceph/osd" }
func nodeCephOSDPath(node string, osdID int) string {
	return nodeCephOSDsPath(node) + "/" + strconv.Itoa(osdID)
}

func nodeCephStatusPath(node string) string { return "/nodes/" + node + "/ceph/status" }

// nodeCephConfigPath is the verbatim ceph.conf. PVE exposes the config three
// ways under /ceph/cfg — raw (the file as text), db, and value — and this is the
// text one, which is what GetClusterConfig returns.
func nodeCephConfigPath(node string) string { return "/nodes/" + node + "/ceph/cfg/raw" }
