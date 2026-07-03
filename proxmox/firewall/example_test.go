package firewall_test

import (
	"context"
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/firewall"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
)

// Example opens the datacenter (cluster) firewall, allows inbound SSH, and reads
// the rule back. The same calls work at node scope (NodeFirewall) and guest
// scope (GuestFirewall) — only the constructor changes.
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
	fw := c.Firewall() // datacenter scope

	if err := fw.CreateRule(ctx, &firewall.RuleSpec{
		Type:   firewall.RuleIn,
		Action: "ACCEPT",
		Proto:  "tcp",
		Dport:  "22",
	}); err != nil {
		fmt.Println("create rule:", err)
		return
	}

	rule, err := fw.GetRule(ctx, 0)
	if err != nil {
		fmt.Println("get rule:", err)
		return
	}
	fmt.Printf("%s %s proto=%s dport=%s\n", rule.Type, rule.Action, rule.Proto, rule.Dport)
	// Output:
	// in ACCEPT proto=tcp dport=22
}
