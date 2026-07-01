package nodes_test

import (
	"context"
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/nodes"
)

// Example stages a VLAN-aware bridge into a node's pending network config, then
// applies it. Interface writes are staged until ApplyNetworkConfig activates
// them; on a live node that apply may reload networking via a worker task, which
// the caller awaits when the returned Ref is non-zero.
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
	n := c.Nodes()

	if err := n.CreateInterface(ctx, "pve", &nodes.InterfaceSpec{
		Iface:       "vmbr1",
		Type:        nodes.InterfaceTypeBridge,
		Address:     "10.0.0.1/24",
		BridgePorts: "eno2",
		VLANAware:   true,
		Autostart:   true,
	}); err != nil {
		fmt.Println("create interface:", err)
		return
	}

	ref, err := n.ApplyNetworkConfig(ctx, "pve")
	if err != nil {
		fmt.Println("apply:", err)
		return
	}
	if !ref.IsZero() {
		if _, err := c.Tasks().Wait(ctx, ref); err != nil {
			fmt.Println("await reload:", err)
			return
		}
	}

	iface, err := n.GetInterface(ctx, "pve", "vmbr1")
	if err != nil {
		fmt.Println("get interface:", err)
		return
	}
	fmt.Printf("%s (%s) %s vlan-aware=%t\n", iface.Iface, iface.Type, iface.Address, bool(iface.VLANAware))
	// Output:
	// vmbr1 (bridge) 10.0.0.1/24 vlan-aware=true
}
