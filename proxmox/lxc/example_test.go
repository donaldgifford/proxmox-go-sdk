package lxc_test

import (
	"context"
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/lxc"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
)

// Example provisions a container from an OS template and starts it, awaiting
// each PVE task before moving on — the canonical create → start flow.
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
	cts := c.LXC("pve")

	// Create container 200 from a template, then wait for the create task.
	ref, err := cts.Create(ctx, &lxc.CreateSpec{
		VMID:       200,
		OSTemplate: "local:vztmpl/debian-12-standard_12.2-1_amd64.tar.zst",
		Hostname:   "web",
		Cores:      2,
		Memory:     512,
	})
	if err != nil {
		fmt.Println("create:", err)
		return
	}
	if _, err := c.Tasks().Wait(ctx, ref); err != nil {
		fmt.Println("await create:", err)
		return
	}

	// Start the container and wait for the start task.
	ref, err = cts.Start(ctx, 200)
	if err != nil {
		fmt.Println("start:", err)
		return
	}
	if _, err := c.Tasks().Wait(ctx, ref); err != nil {
		fmt.Println("await start:", err)
		return
	}

	st, err := cts.Get(ctx, 200)
	if err != nil {
		fmt.Println("status:", err)
		return
	}
	fmt.Println(st.Status)
	// Output:
	// running
}
