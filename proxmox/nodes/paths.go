package nodes

import "net/url"

// Node networking. Interface names ("vmbr0", "eth0", "bond0") are safe ASCII;
// url.PathEscape is applied for consistency and future safety.

func nodeNetworkPath(node string) string { return "/nodes/" + node + "/network" }

func nodeIfacePath(node, iface string) string {
	return nodeNetworkPath(node) + "/" + url.PathEscape(iface)
}
