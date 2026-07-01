package nodes

import "net/url"

// Node networking. Interface names ("vmbr0", "eth0", "bond0") are safe ASCII;
// url.PathEscape is applied for consistency and future safety.

func nodeNetworkPath(node string) string { return "/nodes/" + node + "/network" }

func nodeIfacePath(node, iface string) string {
	return nodeNetworkPath(node) + "/" + url.PathEscape(iface)
}

// Node package management (apt). The DEB822 repositories path is a real 9.x
// endpoint whose field shapes are provisional (see apt.go).

func nodeAptUpdatePath(node string) string { return "/nodes/" + node + "/apt/update" }
func nodeAptRepoPath(node string) string   { return "/nodes/" + node + "/apt/repositories" }

// Node disk management. Disk devices are passed as query/form params
// ("/dev/sda"), not path segments, so no per-disk path builder is needed.

func nodeDisksListPath(node string) string    { return "/nodes/" + node + "/disks/list" }
func nodeDisksSMARTPath(node string) string   { return "/nodes/" + node + "/disks/smart" }
func nodeDisksInitGPTPath(node string) string { return "/nodes/" + node + "/disks/initgpt" }

// Node certificates + cluster-scoped ACME accounts.

func nodeCertInfoPath(node string) string   { return "/nodes/" + node + "/certificates/info" }
func nodeCertCustomPath(node string) string { return "/nodes/" + node + "/certificates/custom" }
func nodeCertACMEPath(node string) string {
	return "/nodes/" + node + "/certificates/acme/certificate"
}

func acmeAccountsPath() string { return "/cluster/acme/account" }
func acmeAccountPath(name string) string {
	return acmeAccountsPath() + "/" + url.PathEscape(name)
}
