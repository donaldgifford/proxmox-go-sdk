package sdn

import "net/url"

// SDN REST paths. Config lives under /cluster/sdn; live status is node-scoped
// under /nodes/{node}/sdn (verified against the real 9.2 apidoc, INV-0004).
// Zone, VNet, subnet, fabric, and node identifiers are short ASCII tokens;
// url.PathEscape is applied for consistency and future safety.

func sdnPath() string      { return "/cluster/sdn" }
func sdnZonesPath() string { return "/cluster/sdn/zones" }

func sdnZonePath(zone string) string {
	return sdnZonesPath() + "/" + url.PathEscape(zone)
}

func sdnVNetsPath() string { return "/cluster/sdn/vnets" }

func sdnVNetPath(vnet string) string {
	return sdnVNetsPath() + "/" + url.PathEscape(vnet)
}

func sdnSubnetsPath(vnet string) string {
	return sdnVNetPath(vnet) + "/subnets"
}

func sdnSubnetPath(vnet, subnet string) string {
	return sdnSubnetsPath(vnet) + "/" + url.PathEscape(subnet)
}

// Fabrics are two sub-collections under /cluster/sdn/fabrics: the fabric
// definitions at …/fabrics/fabric and per-fabric node membership at
// …/fabrics/node/{fabric}. (The flat /cluster/sdn/fabrics path the SDK
// originally guessed is only a subdir index — INV-0004 Finding 3.)

func sdnFabricsPath() string { return "/cluster/sdn/fabrics/fabric" }

func sdnFabricPath(fabric string) string {
	return sdnFabricsPath() + "/" + url.PathEscape(fabric)
}

func sdnFabricNodesPath(fabric string) string {
	return "/cluster/sdn/fabrics/node/" + url.PathEscape(fabric)
}

func sdnFabricNodePath(fabric, node string) string {
	return sdnFabricNodesPath(fabric) + "/" + url.PathEscape(node)
}

// Node-scoped live-status paths (GET-only). The per-object roots
// (…/zones/{zone}, …/vnets/{vnet}, …/fabrics/{fabric}) are subdir indexes on
// real PVE — only their leaf children return data, so the roots exist here
// solely as path builders.

func nodeSDNZonesPath(node string) string {
	return "/nodes/" + url.PathEscape(node) + "/sdn/zones"
}

func nodeSDNZonePath(node, zone string) string {
	return nodeSDNZonesPath(node) + "/" + url.PathEscape(zone)
}

func nodeSDNZoneContentPath(node, zone string) string {
	return nodeSDNZonePath(node, zone) + "/content"
}

func nodeSDNZoneBridgesPath(node, zone string) string {
	return nodeSDNZonePath(node, zone) + "/bridges"
}

func nodeSDNZoneIPVRFPath(node, zone string) string {
	return nodeSDNZonePath(node, zone) + "/ip-vrf"
}

func nodeSDNVNetPath(node, vnet string) string {
	return "/nodes/" + url.PathEscape(node) + "/sdn/vnets/" + url.PathEscape(vnet)
}

func nodeSDNVNetMACVRFPath(node, vnet string) string {
	return nodeSDNVNetPath(node, vnet) + "/mac-vrf"
}

func nodeSDNFabricPath(node, fabric string) string {
	return "/nodes/" + url.PathEscape(node) + "/sdn/fabrics/" + url.PathEscape(fabric)
}

func nodeSDNFabricInterfacesPath(node, fabric string) string {
	return nodeSDNFabricPath(node, fabric) + "/interfaces"
}

func nodeSDNFabricNeighborsPath(node, fabric string) string {
	return nodeSDNFabricPath(node, fabric) + "/neighbors"
}

func nodeSDNFabricRoutesPath(node, fabric string) string {
	return nodeSDNFabricPath(node, fabric) + "/routes"
}
