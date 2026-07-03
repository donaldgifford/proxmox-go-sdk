package sdn_test

import (
	"context"
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/sdn"
)

// Example builds a minimal SDN topology — a simple zone, a VNet in it, and a
// subnet under that VNet — then commits it cluster-wide with ApplySDN. SDN
// changes are staged until applied, so ApplySDN is the step that makes the new
// network live on every node.
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
	s := c.SDN()

	if err := s.CreateZone(ctx, &sdn.ZoneSpec{Zone: "lab", Type: sdn.ZoneTypeSimple}); err != nil {
		fmt.Println("create zone:", err)
		return
	}
	if err := s.CreateVNet(ctx, &sdn.VNetSpec{VNet: "lab0", Zone: "lab"}); err != nil {
		fmt.Println("create vnet:", err)
		return
	}
	if err := s.CreateSubnet(ctx, "lab0", &sdn.SubnetSpec{
		Subnet:  "10.10.0.0/24",
		Gateway: "10.10.0.1",
	}); err != nil {
		fmt.Println("create subnet:", err)
		return
	}
	if err := s.ApplySDN(ctx); err != nil {
		fmt.Println("apply:", err)
		return
	}

	sn, err := s.GetSubnet(ctx, "lab0", "10.10.0.0/24")
	if err != nil {
		fmt.Println("get subnet:", err)
		return
	}
	fmt.Printf("%s in vnet %s, gw %s\n", sn.Subnet, sn.VNet, sn.Gateway)
	// Output:
	// 10.10.0.0/24 in vnet lab0, gw 10.10.0.1
}
