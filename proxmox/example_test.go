package proxmox_test

import (
	"context"
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/console"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

func ExampleNewClient() {
	// mockpve stands in for a live cluster so the example is self-contained.
	mock := mockpve.New()
	mock.SeedVersion("9.2.1", "9.2", "demo")
	ts := mock.Serve()
	defer ts.Close()

	c, err := proxmox.NewClient(context.Background(), ts.URL,
		api.TokenCredentials("root@pam!sdk", "secret"))
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(c.Capabilities().String())
	fmt.Println(c.Capabilities().HAClusterSwitch())
	// Output:
	// 9.2.1
	// true
}

// Example_consoleAndAccess is the Phase 6 success flow: list users and their API
// tokens under the 9.x privilege model, then mint a VNC console session for a
// VM. mockpve seeds a deterministic cluster so the example is self-contained.
func Example_consoleAndAccess() {
	mock := mockpve.New()
	mock.AddAccessUser("automation@pve")
	mock.AddToken("automation@pve", "ci")
	ts := mock.Serve()
	defer ts.Close()

	ctx := context.Background()
	c, err := proxmox.NewClient(ctx, ts.URL, api.TokenCredentials("root@pam!sdk", "secret"))
	if err != nil {
		fmt.Println("connect:", err)
		return
	}

	// List users, then the tokens owned by the seeded user.
	users, err := c.Access().ListUsers(ctx)
	if err != nil {
		fmt.Println("list users:", err)
		return
	}
	tokens, err := c.Access().ListTokens(ctx, "automation@pve")
	if err != nil {
		fmt.Println("list tokens:", err)
		return
	}

	// Mint a VNC console ticket for VM 100; Connect would dial it on a live node.
	ticket, err := c.Console().MintVNCTicket(ctx, "pve", console.KindQEMU, types.VMID(100))
	if err != nil {
		fmt.Println("mint ticket:", err)
		return
	}

	fmt.Printf("%d user(s), %d token(s) for automation@pve\n", len(users), len(tokens))
	fmt.Printf("VNC console ready on port %s\n", ticket.Port)
	// Output:
	// 1 user(s), 1 token(s) for automation@pve
	// VNC console ready on port 5900
}
