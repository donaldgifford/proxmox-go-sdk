package console_test

import (
	"context"
	"fmt"
	"io"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/console"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// Example mints a VNC console ticket for a VM and reports whether it is ready to
// dial.
func Example() {
	// mockpve stands in for a live cluster so the example is self-contained;
	// against a real node, pass its URL and a real token to proxmox.NewClient.
	mock := mockpve.New()
	ts := mock.Serve()
	defer ts.Close()

	ctx := context.Background()
	c, err := proxmox.NewClient(ctx, ts.URL, api.TokenCredentials("root@pam!sdk", "secret"))
	if err != nil {
		fmt.Println("connect:", err)
		return
	}

	ticket, err := c.Console().MintVNCTicket(ctx, "pve", console.KindQEMU, types.VMID(100))
	if err != nil {
		fmt.Println("mint ticket:", err)
		return
	}
	fmt.Printf("minted VNC ticket for port %s\n", ticket.Port)
	// Output:
	// minted VNC ticket for port 5900
}

// ExampleService_Connect dials a VNC console and copies its byte stream. The
// bytes are the live PVE VNC (RFB) protocol, WebSocket-framed; a real VNC client
// belongs on top. It is compile-only (no deterministic output) because the wire
// exchange needs a live node.
func ExampleService_Connect() {
	c, err := proxmox.NewClient(context.Background(), "https://pve.example:8006",
		api.TokenCredentials("root@pam!sdk", "secret"))
	if err != nil {
		return
	}
	svc := c.Console()

	ctx := context.Background()
	ticket, err := svc.MintVNCTicket(ctx, "pve", console.KindQEMU, types.VMID(100))
	if err != nil {
		return
	}
	stream, err := svc.Connect(ctx, "pve", ticket)
	if err != nil {
		return
	}
	defer stream.Close()

	// Hand stream to a VNC/RFB client; here we just drain it.
	_, _ = io.Copy(io.Discard, stream)
}
