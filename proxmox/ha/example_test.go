package ha_test

import (
	"context"
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/ha"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
)

// Example places two guests under HA management and defines a resource-affinity
// rule that co-locates them — the 9.x way to express "keep these together"
// (HA groups are gone). On a live cluster the HA manager then honors the rule
// when it places or relocates the resources.
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
	h := c.HA()

	// Put both guests under HA management.
	for _, sid := range []string{"vm:100", "vm:101"} {
		if aerr := h.AddResource(ctx, &ha.HAResourceSpec{SID: sid}); aerr != nil {
			fmt.Println("add resource:", aerr)
			return
		}
	}

	// Define a positive resource-affinity rule: keep vm:100 and vm:101 together.
	if err := h.CreateRule(ctx, &ha.HARuleSpec{
		Rule:      "collocate-web",
		Type:      ha.RuleTypeResourceAffinity,
		Resources: []string{"vm:100", "vm:101"},
		Affinity:  "positive",
	}); err != nil {
		fmt.Println("create rule:", err)
		return
	}

	rule, err := h.GetRule(ctx, "collocate-web")
	if err != nil {
		fmt.Println("get rule:", err)
		return
	}
	fmt.Printf("%s: %s -> %s\n", rule.Rule, rule.Type, rule.Resources)
	// Output:
	// collocate-web: resource-affinity -> vm:100,vm:101
}
