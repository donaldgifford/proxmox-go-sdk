package mockpve_test

import (
	"context"
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/version"
)

// Example seeds an in-memory PVE responder, wires an api.Client to it, and reads
// the cluster capabilities through the real SDK request path — no live node.
func Example() {
	mock := mockpve.New()
	mock.SeedVersion("9.2.1", "9.2", "demo")

	c, cleanup := mock.NewClient()
	defer cleanup()

	caps, err := version.NewService(c).Capabilities(context.Background())
	if err != nil {
		fmt.Println("capabilities:", err)
		return
	}
	fmt.Println(caps.String())
	fmt.Println(caps.AtLeast(9, 0))
	// Output:
	// 9.2.1
	// true
}
