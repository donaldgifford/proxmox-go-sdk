package console

import (
	"context"
	"errors"
	"io"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/version"
)

// errBadKind flags a GuestKind that is neither qemu nor lxc.
var errBadKind = errors.New("unknown guest kind (want qemu or lxc)")

// Service mints console tickets and opens console sessions. Every operation is
// node-scoped and takes the node as a per-call argument, so the service binds no
// node; one *Service is safe for concurrent use. Construct it with NewService or
// via the root client's Console accessor.
//
// The design splits cleanly in two: minting a ticket is a plain REST call
// (POST vncproxy/spiceproxy/termproxy) that is fully exercised against mockpve,
// while Connect dials the resulting vncwebsocket and hands back a raw duplex
// byte stream. The bytes on that stream are the live PVE console protocol
// (WebSocket-framed VNC/terminal); the SDK carries them verbatim and does not
// interpret them.
type Service struct {
	c    api.Client
	caps version.Capabilities
}

// NewService returns a console Service. caps is accepted for parity with the
// other services; the console endpoints are baseline 9.0 and gate nothing.
func NewService(c api.Client, caps version.Capabilities) *Service {
	return &Service{c: c, caps: caps}
}

// API is the console service contract, published so consumers can stand in a
// test double for *Service. The Mint* calls are synchronous REST reads that
// return a ticket; Connect opens a WebSocket and returns its byte stream.
// VerifyVNCTicket has no standalone PVE REST endpoint and returns
// pverr.ErrUnsupported (see verify.go) — a ticket is verified implicitly when
// Connect dials it.
type API interface {
	// Guest console tickets (node scope).
	MintVNCTicket(ctx context.Context, node string, kind GuestKind, vmid types.VMID) (*VNCTicket, error)
	MintSPICETicket(ctx context.Context, node string, kind GuestKind, vmid types.VMID) (*SPICETicket, error)
	MintTermProxy(ctx context.Context, node string, kind GuestKind, vmid types.VMID) (*TermTicket, error)

	// Node shell tickets (node scope).
	MintNodeVNC(ctx context.Context, node string) (*VNCTicket, error)
	MintNodeTerm(ctx context.Context, node string) (*TermTicket, error)

	// Connect opens the VNC console byte stream for a minted ticket.
	Connect(ctx context.Context, node string, ticket *VNCTicket) (io.ReadWriteCloser, error)

	// VerifyVNCTicket has no standalone PVE REST endpoint and returns
	// pverr.ErrUnsupported.
	VerifyVNCTicket(ctx context.Context, node, ticket string) error
}

// Compile-time assertion that *Service implements the published contract.
var _ API = (*Service)(nil)

// validKind reports whether kind is one the guest console endpoints accept.
func validKind(kind GuestKind) bool {
	return kind == KindQEMU || kind == KindLXC
}
