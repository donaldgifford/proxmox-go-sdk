package cluster_test

import (
	"context"
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/cluster"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
)

// Example enumerates the cluster's VM resources, then reads the datacenter
// options — the two most common cluster-wide reads.
func Example() {
	// mockpve stands in for a live cluster so the example is self-contained;
	// against a real node, pass its URL and a real token to proxmox.NewClient.
	mock := mockpve.New()
	mock.AddClusterResource("qemu", "pve", "running", 100)
	mock.SetClusterOptions("lab datacenter", "")
	ts := mock.Serve()
	defer ts.Close()

	ctx := context.Background()
	c, err := proxmox.NewClient(ctx, ts.URL, api.TokenCredentials("root@pam!sdk", "secret"))
	if err != nil {
		fmt.Println("connect:", err)
		return
	}
	cl := c.Cluster()

	vms, err := cl.ListResources(ctx, cluster.WithResourceType(cluster.ResourceTypeVM))
	if err != nil {
		fmt.Println("list resources:", err)
		return
	}
	opts, err := cl.GetOptions(ctx)
	if err != nil {
		fmt.Println("get options:", err)
		return
	}
	fmt.Printf("%d vm(s); datacenter %q\n", len(vms), opts.Description)
	// Output:
	// 1 vm(s); datacenter "lab datacenter"
}
