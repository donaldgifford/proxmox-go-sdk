package console

import (
	"encoding/json"
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// GuestKind selects the guest type for a guest console ticket: QEMU virtual
// machines or LXC containers. It is the path segment under /nodes/{node}.
type GuestKind string

// The guest kinds a console ticket can target.
const (
	KindQEMU GuestKind = "qemu"
	KindLXC  GuestKind = "lxc"
)

// VNCTicket is the response of a vncproxy/vncshell mint: the one-time ticket and
// the proxy port a Connect must dial. Reads are lossless.
type VNCTicket struct {
	Ticket string `json:"ticket"`
	Port   string `json:"port"` // PVE returns the proxy port as a string.
	User   string `json:"user,omitempty"`
	Cert   string `json:"cert,omitempty"`
	UPID   string `json:"upid,omitempty"` // the proxy worker, informational.
	// Extra carries ticket keys the SDK does not model.
	Extra map[string]string `json:"-"`

	// kind/vmid record which guest the ticket was minted for — zero for a
	// node-shell ticket — so Connect dials the vncwebsocket path the ticket
	// is bound to (real PVE enforces the binding; found live 2026-07-12).
	// Set by MintVNCTicket; a hand-constructed ticket dials the node-shell
	// path.
	kind GuestKind
	vmid types.VMID
}

var vncTicketKnownFields = map[string]bool{
	"ticket": true, "port": true, "user": true, "cert": true, "upid": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (v *VNCTicket) UnmarshalJSON(data []byte) error {
	type alias VNCTicket
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode vnc ticket: %w", err)
	}
	*v = VNCTicket(a)
	extra, err := svcutil.DecodeExtra(data, vncTicketKnownFields)
	if err != nil {
		return fmt.Errorf("decode vnc ticket: %w", err)
	}
	v.Extra = extra
	return nil
}

// TermTicket is the response of a termproxy mint: the one-time ticket and the
// proxy port for a terminal WebSocket. Reads are lossless.
type TermTicket struct {
	Ticket string `json:"ticket"`
	Port   string `json:"port"`
	User   string `json:"user,omitempty"`
	UPID   string `json:"upid,omitempty"`
	// Extra carries ticket keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

var termTicketKnownFields = map[string]bool{
	"ticket": true, "port": true, "user": true, "upid": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (tk *TermTicket) UnmarshalJSON(data []byte) error {
	type alias TermTicket
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode term ticket: %w", err)
	}
	*tk = TermTicket(a)
	extra, err := svcutil.DecodeExtra(data, termTicketKnownFields)
	if err != nil {
		return fmt.Errorf("decode term ticket: %w", err)
	}
	tk.Extra = extra
	return nil
}

// SPICETicket is the response of a spiceproxy mint: the connection parameters a
// SPICE client (remote-viewer) needs. Reads are lossless.
type SPICETicket struct {
	Host     string `json:"host,omitempty"`
	Proxy    string `json:"proxy,omitempty"`
	TLSPort  int    `json:"tls-port,omitempty"`
	Password string `json:"password,omitempty"`
	CA       string `json:"ca,omitempty"`
	Type     string `json:"type,omitempty"`
	Title    string `json:"title,omitempty"`
	// Extra carries SPICE keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

var spiceTicketKnownFields = map[string]bool{
	"host": true, "proxy": true, "tls-port": true, "password": true,
	"ca": true, "type": true, "title": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (sp *SPICETicket) UnmarshalJSON(data []byte) error {
	type alias SPICETicket
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode spice ticket: %w", err)
	}
	*sp = SPICETicket(a)
	extra, err := svcutil.DecodeExtra(data, spiceTicketKnownFields)
	if err != nil {
		return fmt.Errorf("decode spice ticket: %w", err)
	}
	sp.Extra = extra
	return nil
}
