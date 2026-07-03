package cluster_test

import (
	"context"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/cluster"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/version"
)

func newService(t *testing.T, mock *mockpve.Server) *cluster.Service {
	t.Helper()
	c, cleanup := mock.NewClient()
	t.Cleanup(cleanup)
	return cluster.NewService(c, version.Capabilities{})
}

func TestListResources(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddClusterResource("qemu", "pve", "running", 100)
	mock.AddClusterResource("lxc", "pve", "running", 101)
	mock.AddClusterResource("node", "pve", "online", 0)
	svc := newService(t, mock)

	all, err := svc.ListResources(context.Background())
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("ListResources returned %d, want 3", len(all))
	}
}

func TestListResourcesFilteredToVM(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddClusterResource("qemu", "pve", "running", 100)
	mock.AddClusterResource("lxc", "pve", "running", 101)
	mock.AddClusterResource("node", "pve", "online", 0)
	svc := newService(t, mock)

	vms, err := svc.ListResources(context.Background(), cluster.WithResourceType(cluster.ResourceTypeVM))
	if err != nil {
		t.Fatalf("ListResources(vm): %v", err)
	}
	// The "vm" filter matches both qemu and lxc guests, not the node.
	if len(vms) != 2 {
		t.Fatalf("ListResources(vm) returned %d, want 2", len(vms))
	}
	for _, r := range vms {
		if r.Type != "qemu" && r.Type != "lxc" {
			t.Errorf("resource type = %q, want qemu or lxc", r.Type)
		}
	}
}

func TestGetStatus(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.SetClusterStatusInfo("lab", 2, true)
	mock.AddClusterStatusNode("pve1", true)
	mock.AddClusterStatusNode("pve2", true)
	svc := newService(t, mock)

	entries, err := svc.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("GetStatus returned %d entries, want 3", len(entries))
	}
	var sawCluster bool
	for _, e := range entries {
		if e.Type == "cluster" {
			sawCluster = true
			if e.Name != "lab" || e.Nodes != 2 || !bool(e.Quorate) {
				t.Errorf("cluster entry = %+v, want name=lab nodes=2 quorate", e)
			}
		}
	}
	if !sawCluster {
		t.Error("no cluster entry in status")
	}
}

func TestGetOptions(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.SetClusterOptions("lab datacenter", "type=secure")
	svc := newService(t, mock)

	o, err := svc.GetOptions(context.Background())
	if err != nil {
		t.Fatalf("GetOptions: %v", err)
	}
	if o.Description != "lab datacenter" || o.Migration != "type=secure" {
		t.Errorf("options = %+v, want description/migration set", o)
	}
}

func TestSetOptions(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.SetOptions(ctx, &cluster.OptionsUpdate{Description: "changed"}); err != nil {
		t.Fatalf("SetOptions: %v", err)
	}
	o, err := svc.GetOptions(ctx)
	if err != nil {
		t.Fatalf("GetOptions after set: %v", err)
	}
	if o.Description != "changed" {
		t.Errorf("description after set = %q, want changed", o.Description)
	}
}

func TestSetOptionsNil(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)

	if err := svc.SetOptions(context.Background(), nil); err == nil {
		t.Error("SetOptions(nil) error = nil, want non-nil")
	}
}
