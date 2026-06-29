package qemu_test

import (
	"context"
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/qemu"
)

// Example clones a template VM into a new VM and starts it, awaiting each PVE
// task before moving on — the canonical clone → start flow.
func Example() {
	// mockpve stands in for a live cluster so the example is self-contained;
	// against a real node, pass its URL and a real token to proxmox.NewClient.
	mock := mockpve.New()
	mock.AddVM("pve", 9000, "template", "stopped") // the template to clone from.
	ts := mock.Serve()
	defer ts.Close()

	ctx := context.Background()
	c, err := proxmox.NewClient(ctx, ts.URL, api.TokenCredentials("root@pam!sdk", "secret"))
	if err != nil {
		fmt.Println("connect:", err)
		return
	}
	vms := c.QEMU("pve")

	// Clone the template into VM 101, then wait for the clone task to finish.
	ref, err := vms.Clone(ctx, 9000, &qemu.CloneSpec{NewID: 101, Name: "web-1"})
	if err != nil {
		fmt.Println("clone:", err)
		return
	}
	if _, err := c.Tasks().Wait(ctx, ref); err != nil {
		fmt.Println("await clone:", err)
		return
	}

	// Start the clone and wait for the start task.
	ref, err = vms.Start(ctx, 101)
	if err != nil {
		fmt.Println("start:", err)
		return
	}
	if _, err := c.Tasks().Wait(ctx, ref); err != nil {
		fmt.Println("await start:", err)
		return
	}

	st, err := vms.Get(ctx, 101)
	if err != nil {
		fmt.Println("status:", err)
		return
	}
	fmt.Println(st.Status)
	// Output:
	// running
}
