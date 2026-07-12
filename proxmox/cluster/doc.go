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
//   - [Service.CreateCluster] forms a new cluster on the responding node and
//     [Service.JoinCluster] joins the responding node to an existing one.
//     Both are fire-and-poll writes: PVE's return shapes are unverified and a
//     join restarts the responding API daemons mid-call, so the response body
//     is ignored beyond error status and convergence is observed by polling
//     [Service.ListConfigNodes] (the corosync nodelist) until the node
//     appears. JoinCluster is only meaningful against a FRESH node — it wipes
//     the joining node's local pmxcfs config (users and API tokens do not
//     survive; root@pam, being a PAM account, does).
//   - [Service.JoinInfo] returns what a joining node needs to know about an
//     existing member's cluster; [JoinInfo.Fingerprint] yields the cluster
//     certificate fingerprint a [JoinSpec] must present.
//
// All cluster endpoints are baseline PVE 9.0 and gate nothing.
package cluster
