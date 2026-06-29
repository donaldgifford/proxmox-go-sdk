package proxmox_test

import (
	"context"
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
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
	fmt.Println(c.Capabilities().DynamicLoadBalancer())
	// Output:
	// 9.2.1
	// true
}
