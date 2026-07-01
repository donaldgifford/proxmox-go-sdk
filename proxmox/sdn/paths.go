package sdn

import "net/url"

// SDN REST paths, all under /cluster/sdn. Zone, VNet, and subnet identifiers are
// short ASCII tokens; url.PathEscape is applied for consistency and future
// safety.

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
