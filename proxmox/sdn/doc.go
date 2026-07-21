// Package sdn wraps Proxmox VE software-defined networking: zones, VNets and
// their subnets, fabrics and their node membership, the cluster-wide apply
// that activates staged changes, and the node-scoped live-status reads.
// Configuration is cluster-scoped — every config endpoint lives under
// /cluster/sdn and binds no node; live status lives under /nodes/{node}/sdn
// and takes the node per call. Construct the [Service] with [NewService] or
// via the root client's SDN accessor; one *Service is safe for concurrent use.
//
// SDN configuration is transactional. Creates, updates, and deletes stage
// changes into a pending config; [Service.ApplySDN] commits them cluster-wide.
// All config writes are synchronous (they return an error, not a task
// reference). Reads are lossless — keys the SDK does not model are preserved in
// each type's Extra map, since zone and fabric configs are type/protocol
// dependent.
//
//   - Zones ([Service.CreateZone], …): the backing technology — simple, VLAN,
//     QinQ, VXLAN, or EVPN ([ZoneType]).
//   - VNets ([Service.CreateVNet], …) and their nested subnets
//     ([Service.CreateSubnet], …).
//   - Fabrics ([Service.CreateFabric], …): the OpenFabric/OSPF routing layer
//     (9.0+) at /cluster/sdn/fabrics/fabric, with node membership as its own
//     sub-collection ([Service.CreateFabricNode], …) at
//     /cluster/sdn/fabrics/node/{fabric} — paths confirmed against the real
//     9.2 apidoc (INV-0004). The 9.2 BGP protocol is gated via
//     version.Capabilities.SDNAdvancedFabrics.
//   - Live status ([Service.SDNStatus], [Service.ZoneContent],
//     [Service.ZoneBridges], [Service.ZoneIPVRF], [Service.VNetMACVRF], and
//     the fabric runtime reads [Service.FabricInterfaces],
//     [Service.FabricNeighbors], [Service.FabricRoutes]): node-scoped GETs.
//     There is no per-VNet status endpoint — ZoneContent is the per-VNet
//     health read and VNetMACVRF the EVPN view.
package sdn
