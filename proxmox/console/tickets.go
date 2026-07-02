package console

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// MintVNCTicket requests a VNC console ticket for a guest (POST
// /nodes/{node}/{qemu|lxc}/{vmid}/vncproxy). The returned VNCTicket carries the
// one-time ticket and the proxy port that Connect dials. websocket=1 is sent so
// PVE prepares the port for a vncwebsocket connection.
func (s *Service) MintVNCTicket(ctx context.Context, node string, kind GuestKind, vmid types.VMID) (*VNCTicket, error) {
	if !validKind(kind) {
		return nil, fmt.Errorf("console.MintVNCTicket: %w", errBadKind)
	}
	var t VNCTicket
	body := url.Values{"websocket": {"1"}}
	if err := s.c.DoRequest(ctx, http.MethodPost, guestConsolePath(node, kind, vmid, "vncproxy"), body, &t); err != nil {
		return nil, fmt.Errorf("console.MintVNCTicket: %w", err)
	}
	return &t, nil
}

// MintSPICETicket requests a SPICE console ticket for a guest (POST
// /nodes/{node}/{qemu|lxc}/{vmid}/spiceproxy). The returned SPICETicket carries
// the connection parameters a SPICE client (remote-viewer) needs; there is no
// SDK-side dial for SPICE — hand the parameters to the client.
func (s *Service) MintSPICETicket(ctx context.Context, node string, kind GuestKind, vmid types.VMID) (*SPICETicket, error) {
	if !validKind(kind) {
		return nil, fmt.Errorf("console.MintSPICETicket: %w", errBadKind)
	}
	var t SPICETicket
	if err := s.c.DoRequest(ctx, http.MethodPost, guestConsolePath(node, kind, vmid, "spiceproxy"), nil, &t); err != nil {
		return nil, fmt.Errorf("console.MintSPICETicket: %w", err)
	}
	return &t, nil
}

// MintTermProxy requests a terminal (xterm.js) console ticket for a guest (POST
// /nodes/{node}/{qemu|lxc}/{vmid}/termproxy). The returned TermTicket carries
// the one-time ticket and the proxy port for a terminal WebSocket.
func (s *Service) MintTermProxy(ctx context.Context, node string, kind GuestKind, vmid types.VMID) (*TermTicket, error) {
	if !validKind(kind) {
		return nil, fmt.Errorf("console.MintTermProxy: %w", errBadKind)
	}
	var t TermTicket
	if err := s.c.DoRequest(ctx, http.MethodPost, guestConsolePath(node, kind, vmid, "termproxy"), nil, &t); err != nil {
		return nil, fmt.Errorf("console.MintTermProxy: %w", err)
	}
	return &t, nil
}

// MintNodeVNC requests a VNC console ticket for the node's shell (POST
// /nodes/{node}/vncshell). The returned VNCTicket is dialled by Connect exactly
// like a guest VNC ticket.
func (s *Service) MintNodeVNC(ctx context.Context, node string) (*VNCTicket, error) {
	var t VNCTicket
	body := url.Values{"websocket": {"1"}}
	if err := s.c.DoRequest(ctx, http.MethodPost, nodeVNCShellPath(node), body, &t); err != nil {
		return nil, fmt.Errorf("console.MintNodeVNC: %w", err)
	}
	return &t, nil
}

// MintNodeTerm requests a terminal console ticket for the node's shell (POST
// /nodes/{node}/termproxy). The returned TermTicket carries the one-time ticket
// and the proxy port for a terminal WebSocket.
func (s *Service) MintNodeTerm(ctx context.Context, node string) (*TermTicket, error) {
	var t TermTicket
	if err := s.c.DoRequest(ctx, http.MethodPost, nodeTermProxyPath(node), nil, &t); err != nil {
		return nil, fmt.Errorf("console.MintNodeTerm: %w", err)
	}
	return &t, nil
}
