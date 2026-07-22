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
//   - HA status — the live per-row manager view (HAStatusCurrent: quorum,
//     master, lrm, and service rows; lossless) and the CRM master's internal
//     state (GetManagerStatus; shape provisional, documented on ManagerStatus).
//   - Migrate/relocate — synchronous CRM placement requests
//     (MigrateResource/RelocateResource) returning a typed MigrateResult with
//     affinity conflicts (BlockingResources) and co-migrations; convergence is
//     observed via HAStatusCurrent.
//   - Arm/Disarm (9.2+, gated on HAClusterSwitch) — the cluster-wide HA switch
//     (ArmHA/DisarmHA over /cluster/ha/status/{arm,disarm}-ha); the disarm
//     resource-mode (freeze/ignore) is required.
//   - CRS settings — the Cluster Resource Scheduler mode and rebalance-on-start
//     flag, stored inside the datacenter options (GetCRSSettings/SetCRSSettings).
//   - Storage/ZFS replication jobs — CRUD over /cluster/replication; requires the
//     9.x VM.Replicate privilege.
//
// PVE HA and replication config writes are synchronous — the methods return an
// error, never a tasks.Ref (migrate/relocate included: the response is a typed
// result, not a task). The Dynamic Load Balancer ops
// (GetDLBStatus/SetDLBConfig) always return pverr.ErrUnsupported — PVE exposes
// no DLB REST endpoint (INV-0004); continuous rebalancing is configured through
// the CRS settings instead.
//
// See docs/design/0001-proxmox-sdk-package-layout.md and
// docs/impl/0001-proxmox-ve-9x-sdk-coverage.md.
package ha
