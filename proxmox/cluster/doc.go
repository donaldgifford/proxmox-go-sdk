// Package cluster wraps cluster-wide reads and the datacenter options. It is
// cluster-scoped — every endpoint lives under /cluster and binds no node.
// Construct the [Service] with [NewService] or via the root client's Cluster
// accessor; one *Service is safe for concurrent use.
//
//   - [Service.ListResources] enumerates the resource inventory (VMs,
//     containers, storage, nodes, SDN), optionally filtered to one kind with
//     [WithResourceType].
//   - [Service.GetStatus] returns the /cluster/status entries — one "cluster"
//     entry (name, member count, quorum) plus a "node" entry per member.
//   - [Service.GetOptions] and [Service.SetOptions] read and write the
//     datacenter options block. SetOptions is synchronous (returns an error, not
//     a task). Options reads are lossless; note the HA "crs" property-string
//     surfaces in [Options].Extra, since the ha package owns it.
//
// All cluster endpoints are baseline PVE 9.0 and gate nothing.
package cluster
