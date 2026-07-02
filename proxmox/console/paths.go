package console

import (
	"net/url"
	"strconv"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// Guest console endpoints: POST /nodes/{node}/{qemu|lxc}/{vmid}/{verb}.

func guestConsolePath(node string, kind GuestKind, vmid types.VMID, verb string) string {
	return "/nodes/" + node + "/" + string(kind) + "/" + strconv.Itoa(int(vmid)) + "/" + verb
}

// Node shell console endpoints (no guest).

func nodeVNCShellPath(node string) string  { return "/nodes/" + node + "/vncshell" }
func nodeTermProxyPath(node string) string { return "/nodes/" + node + "/termproxy" }

// vncWebSocketPath builds the dial path for Connect: the vncwebsocket endpoint
// carrying the proxy port and the one-time ticket as query parameters.
func vncWebSocketPath(node, port, ticket string) string {
	q := url.Values{"port": {port}, "vncticket": {ticket}}
	return "/nodes/" + node + "/vncwebsocket?" + q.Encode()
}
