// Package ha wraps Proxmox VE high availability. It is cluster-scoped: unlike
// the compute services it binds no node — every endpoint lives under /cluster.
// Obtain it from the root client's HA accessor:
//
//	h := client.HA()
//	err := h.AddResource(ctx, &ha.HAResourceSpec{SID: "vm:100"})
//
// It covers:
//
//   - HA resources — place a guest ("vm:100"/"ct:101") under HA management and
//     manage its requested state (AddResource/UpdateResource/RemoveResource).
//   - HA rules — the 9.x replacement for the deprecated HA groups: node-affinity
//     (pin resources to nodes) and resource-affinity (co-locate or anti-locate
//     resources), with enable/disable (CreateRule/UpdateRule/DeleteRule).
//   - CRS settings — the Cluster Resource Scheduler mode and rebalance-on-start
//     flag, stored inside the datacenter options (GetCRSSettings/SetCRSSettings).
//   - Dynamic Load Balancer (9.2+) — continuous CRS rebalancing
//     (GetDLBStatus/SetDLBConfig), gated on the DynamicLoadBalancer capability.
//   - Storage/ZFS replication jobs — CRUD over /cluster/replication; requires the
//     9.x VM.Replicate privilege.
//
// PVE HA and replication config writes are synchronous — the methods return an
// error, not a tasks.Ref. Two 9.2 surfaces are not verifiable without a live
// node: the Dynamic Load Balancer uses a provisional REST path (documented on
// GetDLBStatus), and the cluster-wide Arm/Disarm switch (ArmHA/DisarmHA) has no
// confirmed REST endpoint and returns pverr.ErrUnsupported.
//
// See docs/design/0001-proxmox-sdk-package-layout.md and
// docs/impl/0001-proxmox-ve-9x-sdk-coverage.md.
package ha
