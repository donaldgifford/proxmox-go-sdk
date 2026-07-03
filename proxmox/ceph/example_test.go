package ceph_test

import (
	"context"
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
)

// Example lists the Ceph pools and reads the cluster health — the two most
// common Ceph reads. Ceph is reached through a MON node ("pve" here).
func Example() {
	// mockpve stands in for a live cluster so the example is self-contained;
	// against a real node, pass its URL and a real token to proxmox.NewClient.
	mock := mockpve.New()
	mock.AddCephPool("rbd")
	mock.AddCephOSD(0, "pve")
	ts := mock.Serve()
	defer ts.Close()

	ctx := context.Background()
	c, err := proxmox.NewClient(ctx, ts.URL, api.TokenCredentials("root@pam!sdk", "secret"))
	if err != nil {
		fmt.Println("connect:", err)
		return
	}
	cph := c.Ceph()

	pools, err := cph.ListPools(ctx, "pve")
	if err != nil {
		fmt.Println("list pools:", err)
		return
	}
	status, err := cph.GetStatus(ctx, "pve")
	if err != nil {
		fmt.Println("status:", err)
		return
	}
	fmt.Printf("%d pool(s); health %s\n", len(pools), status.Health.Status)
	// Output:
	// 1 pool(s); health HEALTH_OK
}
