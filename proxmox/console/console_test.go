package console_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/console"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/version"
)

const testNode = "pve"

func newService(t *testing.T, mock *mockpve.Server) *console.Service {
	t.Helper()
	c, cleanup := mock.NewClient()
	t.Cleanup(cleanup)
	return console.NewService(c, version.Capabilities{})
}

func TestMintGuestVNCTicket(t *testing.T) {
	t.Parallel()
	svc := newService(t, mockpve.New())
	ctx := context.Background()

	for _, kind := range []console.GuestKind{console.KindQEMU, console.KindLXC} {
		tk, err := svc.MintVNCTicket(ctx, testNode, kind, types.VMID(100))
		if err != nil {
			t.Fatalf("MintVNCTicket(%s): %v", kind, err)
		}
		if tk.Ticket == "" || tk.Port == "" {
			t.Errorf("MintVNCTicket(%s) = %+v, want ticket and port set", kind, tk)
		}
		if tk.User != "root@pam" {
			t.Errorf("MintVNCTicket(%s) user = %q, want root@pam", kind, tk.User)
		}
	}
}

func TestMintVNCTicketBadKind(t *testing.T) {
	t.Parallel()
	svc := newService(t, mockpve.New())

	if _, err := svc.MintVNCTicket(context.Background(), testNode, console.GuestKind("bogus"), types.VMID(1)); err == nil {
		t.Error("MintVNCTicket(bad kind) error = nil, want non-nil")
	}
	if _, err := svc.MintTermProxy(context.Background(), testNode, console.GuestKind("bogus"), types.VMID(1)); err == nil {
		t.Error("MintTermProxy(bad kind) error = nil, want non-nil")
	}
}

func TestMintNodeConsoles(t *testing.T) {
	t.Parallel()
	svc := newService(t, mockpve.New())
	ctx := context.Background()

	vnc, err := svc.MintNodeVNC(ctx, testNode)
	if err != nil {
		t.Fatalf("MintNodeVNC: %v", err)
	}
	if vnc.Ticket == "" || vnc.Port == "" {
		t.Errorf("MintNodeVNC = %+v, want ticket and port set", vnc)
	}

	term, err := svc.MintNodeTerm(ctx, testNode)
	if err != nil {
		t.Fatalf("MintNodeTerm: %v", err)
	}
	if term.Ticket == "" || term.Port == "" {
		t.Errorf("MintNodeTerm = %+v, want ticket and port set", term)
	}
}

func TestMintTermProxy(t *testing.T) {
	t.Parallel()
	svc := newService(t, mockpve.New())

	term, err := svc.MintTermProxy(context.Background(), testNode, console.KindLXC, types.VMID(101))
	if err != nil {
		t.Fatalf("MintTermProxy: %v", err)
	}
	if term.Ticket == "" || term.Port == "" {
		t.Errorf("MintTermProxy = %+v, want ticket and port set", term)
	}
}

func TestMintSPICETicketLossless(t *testing.T) {
	t.Parallel()
	svc := newService(t, mockpve.New())

	sp, err := svc.MintSPICETicket(context.Background(), testNode, console.KindQEMU, types.VMID(100))
	if err != nil {
		t.Fatalf("MintSPICETicket: %v", err)
	}
	if sp.Type != "spice" || sp.TLSPort != 61000 {
		t.Errorf("MintSPICETicket = %+v, want type=spice tls-port=61000", sp)
	}
	// The unmodelled "release-cursor" key must survive in Extra.
	if sp.Extra["release-cursor"] != "1" {
		t.Errorf("SPICE Extra[release-cursor] = %q, want 1 (lossless decode)", sp.Extra["release-cursor"])
	}
}

func TestConnectEcho(t *testing.T) {
	t.Parallel()
	svc := newService(t, mockpve.New())
	ctx := context.Background()

	tk, err := svc.MintVNCTicket(ctx, testNode, console.KindQEMU, types.VMID(100))
	if err != nil {
		t.Fatalf("MintVNCTicket: %v", err)
	}
	stream, err := svc.Connect(ctx, testNode, tk)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer stream.Close()

	want := []byte("RFB 003.008\n")
	if _, err := stream.Write(want); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := make([]byte, len(want))
	if _, err := io.ReadFull(stream, got); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("console echo = %q, want %q", got, want)
	}
}

// TestConnectNodeShell pins the node-shell dial: a MintNodeVNC ticket connects
// at /nodes/{node}/vncwebsocket (guest tickets dial their guest's own path —
// TestConnectEcho — and the mock enforces the binding like real PVE).
func TestConnectNodeShell(t *testing.T) {
	t.Parallel()
	svc := newService(t, mockpve.New())
	ctx := context.Background()

	tk, err := svc.MintNodeVNC(ctx, testNode)
	if err != nil {
		t.Fatalf("MintNodeVNC: %v", err)
	}
	stream, err := svc.Connect(ctx, testNode, tk)
	if err != nil {
		t.Fatalf("Connect(node shell): %v", err)
	}
	defer stream.Close()

	want := []byte("shell\n")
	if _, err := stream.Write(want); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := make([]byte, len(want))
	if _, err := io.ReadFull(stream, got); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("node shell echo = %q, want %q", got, want)
	}
}

// TestConnectGuestTicketBoundToGuestPath pins the live 2026-07-12 finding: a
// guest-minted ticket VALUE presented at the node-shell path (which is where a
// hand-constructed ticket dials — it carries no guest provenance) must be
// rejected, exactly like real PVE's 401 "invalid PVEVNC ticket".
func TestConnectGuestTicketBoundToGuestPath(t *testing.T) {
	t.Parallel()
	svc := newService(t, mockpve.New())
	ctx := context.Background()

	tk, err := svc.MintVNCTicket(ctx, testNode, console.KindQEMU, types.VMID(100))
	if err != nil {
		t.Fatalf("MintVNCTicket: %v", err)
	}
	stray := &console.VNCTicket{Ticket: tk.Ticket, Port: tk.Port}
	if _, err := svc.Connect(ctx, testNode, stray); err == nil {
		t.Error("Connect(guest ticket at node-shell path) error = nil, want 401")
	}
}

func TestConnectRejectsUnknownTicket(t *testing.T) {
	t.Parallel()
	svc := newService(t, mockpve.New())

	// A ticket the mock never minted: the vncwebsocket upgrade must fail.
	bad := &console.VNCTicket{Ticket: "never-minted", Port: "5900"}
	if _, err := svc.Connect(context.Background(), testNode, bad); err == nil {
		t.Error("Connect(unknown ticket) error = nil, want non-nil")
	}
}

func TestConnectValidation(t *testing.T) {
	t.Parallel()
	svc := newService(t, mockpve.New())
	ctx := context.Background()

	if _, err := svc.Connect(ctx, testNode, nil); err == nil {
		t.Error("Connect(nil) error = nil, want non-nil")
	}
	if _, err := svc.Connect(ctx, testNode, &console.VNCTicket{Port: "5900"}); err == nil {
		t.Error("Connect(no ticket) error = nil, want non-nil")
	}
	if _, err := svc.Connect(ctx, testNode, &console.VNCTicket{Ticket: "t"}); err == nil {
		t.Error("Connect(no port) error = nil, want non-nil")
	}
}

func TestVerifyVNCTicketUnsupported(t *testing.T) {
	t.Parallel()
	svc := newService(t, mockpve.New())

	if err := svc.VerifyVNCTicket(context.Background(), testNode, "some-ticket"); !errors.Is(err, pverr.ErrUnsupported) {
		t.Errorf("VerifyVNCTicket = %v, want ErrUnsupported", err)
	}
}
