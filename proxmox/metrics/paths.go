package metrics

import (
	"net/url"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// Node metrics.

func nodeRRDPath(node string) string    { return "/nodes/" + node + "/rrddata" }
func nodeStatusPath(node string) string { return "/nodes/" + node + "/status" }

// vmRRDPath returns /nodes/{node}/{qemu|lxc}/{vmid}/rrddata.
func vmRRDPath(node string, kind VMKind, vmid types.VMID) string {
	return "/nodes/" + node + "/" + string(kind) + "/" + vmid.String() + "/rrddata"
}

// Cluster-scoped metric servers.

func metricsServersPath() string { return "/cluster/metrics/server" }

func metricsServerPath(id string) string {
	return metricsServersPath() + "/" + url.PathEscape(id)
}
