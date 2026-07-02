package pbs_test

import (
	"context"
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pbs"
)

// Example lists the scheduled backup jobs, then starts an immediate backup of a
// VM and awaits the worker task.
func Example() {
	// mockpve stands in for a live cluster so the example is self-contained;
	// against a real node, pass its URL and a real token to proxmox.NewClient.
	mock := mockpve.New()
	mock.AddBackupJob("nightly", "pbs-store", "02:00")
	ts := mock.Serve()
	defer ts.Close()

	ctx := context.Background()
	c, err := proxmox.NewClient(ctx, ts.URL, api.TokenCredentials("root@pam!sdk", "secret"))
	if err != nil {
		fmt.Println("connect:", err)
		return
	}
	b := c.PBS()

	jobs, err := b.ListBackupJobs(ctx)
	if err != nil {
		fmt.Println("list jobs:", err)
		return
	}
	ref, err := b.CreateBackup(ctx, "pve", &pbs.VzdumpSpec{VMID: "100", Storage: "pbs-store", Mode: "snapshot"})
	if err != nil {
		fmt.Println("create backup:", err)
		return
	}
	if _, err := c.Tasks().Wait(ctx, ref); err != nil {
		fmt.Println("wait backup:", err)
		return
	}
	fmt.Printf("%d scheduled job(s); ad-hoc backup complete\n", len(jobs))
	// Output:
	// 1 scheduled job(s); ad-hoc backup complete
}
