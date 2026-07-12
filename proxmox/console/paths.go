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

// vncWebSocketPath builds the dial path for Connect, carrying the proxy port
// and the one-time ticket as query parameters. PVE binds a ticket to the
// surface that minted it — a guest ticket presented at the node-shell path is
// a 401 "invalid PVEVNC ticket" (found live 2026-07-12) — so a guest ticket
// dials the guest's own vncwebsocket and a node-shell ticket the node's.
func vncWebSocketPath(node string, ticket *VNCTicket) string {
	q := url.Values{"port": {ticket.Port}, "vncticket": {ticket.Ticket}}
	if ticket.kind != "" {
		return guestConsolePath(node, ticket.kind, ticket.vmid, "vncwebsocket") + "?" + q.Encode()
	}
	return "/nodes/" + node + "/vncwebsocket?" + q.Encode()
}
