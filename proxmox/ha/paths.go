package ha

import "net/url"

// All HA endpoints are cluster-scoped under /cluster/ha. SIDs such as "vm:100"
// carry a colon, so they are percent-escaped as path segments (url.PathEscape
// encodes ":" as %3A); Go's ServeMux {sid} + PathValue round-trips them.

func haResourcesPath() string { return "/cluster/ha/resources" }

func haResourcePath(sid string) string {
	return haResourcesPath() + "/" + url.PathEscape(sid)
}

func haRulesPath() string { return "/cluster/ha/rules" }

func haRulePath(rule string) string {
	return haRulesPath() + "/" + url.PathEscape(rule)
}

// clusterOptionsPath is the datacenter options endpoint. It is not under
// /cluster/ha, but the CRS scheduler config lives here (the "crs" key).
func clusterOptionsPath() string { return "/cluster/options" }

// The /cluster/ha/status surface: the current-status read plus the 9.2
// cluster-wide arm/disarm switch (both writes are synchronous, no task).

func haStatusCurrentPath() string { return "/cluster/ha/status/current" }

func haStatusManagerPath() string { return "/cluster/ha/status/manager_status" }

func haStatusArmPath() string { return "/cluster/ha/status/arm-ha" }

func haStatusDisarmPath() string { return "/cluster/ha/status/disarm-ha" }

// dlbPath is the Dynamic Load Balancer endpoint (9.2+). The path is provisional
// — it mirrors PVE's ha-manager "lbalancer" naming and is unconfirmed without a
// live 9.2 node (see ha.GetDLBStatus).
func dlbPath() string { return "/cluster/ha/lbalancer" }

// Storage/ZFS replication jobs. Job IDs are "<vmid>-<jobnum>" (e.g. "100-0") —
// a hyphen, not a colon, so url.PathEscape is effectively a no-op, but it is
// applied for consistency and safety.

func replJobsPath() string { return "/cluster/replication" }

func replJobPath(id string) string {
	return replJobsPath() + "/" + url.PathEscape(id)
}
