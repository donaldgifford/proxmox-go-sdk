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

// Example_acmeDNS wires a node for a DNS-01 certificate end to end: register the
// challenge plugin with the provider's credentials, point the node at an ACME
// account and the domain, then order.
//
// DNS-01 is what you want when the node is not reachable from the internet —
// the challenge proves control of the DNS zone, not of a web server. The
// provider's credentials go in via an ACMEPluginData; the SDK renders them to
// PVE's wire format, so nothing here handles base64.
func Example_acmeDNS() {
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

	// 1. The challenge plugin. Type defaults to dns, and the api parameter comes
	// from the provider rather than from the caller.
	if err := n.CreateACMEPlugin(ctx, &nodes.ACMEPluginSpec{
		ID:   "cf-lab",
		Data: nodes.ACMECloudflare{Token: "cf-api-token"},
	}); err != nil {
		fmt.Println("create plugin:", err)
		return
	}

	// 2. Point the node at an account and one domain, answered by that plugin.
	// The write clears nothing it does not name.
	if err := n.SetNodeConfig(ctx, "pve", &nodes.NodeConfigUpdate{
		ACME: &nodes.NodeACME{Account: "default"},
		ACMEDomains: []nodes.ACMEDomain{
			{Index: 0, Domain: "pve.lab.example", Plugin: "cf-lab"},
		},
	}); err != nil {
		fmt.Println("set node config:", err)
		return
	}

	cfg, err := n.GetNodeConfig(ctx, "pve")
	if err != nil {
		fmt.Println("get node config:", err)
		return
	}
	fmt.Printf("account %s certifies %s via plugin %s\n",
		cfg.ACME.Account, cfg.ACMEDomains[0].Domain, cfg.ACMEDomains[0].Plugin)

	// 3. Order. The order runs as a worker and REPLACES the node's serving
	// certificate when it finishes.
	ref, err := n.OrderNodeCertificate(ctx, "pve")
	if err != nil {
		fmt.Println("order certificate:", err)
		return
	}
	if _, err := c.Tasks().Wait(ctx, ref); err != nil {
		fmt.Println("await order:", err)
		return
	}
	fmt.Println("certificate ordered")

	// Output:
	// account default certifies pve.lab.example via plugin cf-lab
	// certificate ordered
}
