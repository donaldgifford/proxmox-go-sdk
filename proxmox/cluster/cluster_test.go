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

// TestClusterFormationFlow drives the happy path against one mock playing
// every role: create → join-info → join → membership visible.
func TestClusterFormationFlow(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.SetClusterSelfNode("pve1")
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.CreateCluster(ctx, &cluster.ClusterCreateSpec{Name: "pvelab"}); err != nil {
		t.Fatalf("CreateCluster: %v", err)
	}

	info, err := svc.JoinInfo(ctx)
	if err != nil {
		t.Fatalf("JoinInfo: %v", err)
	}
	if info.PreferredNode != "pve1" {
		t.Errorf("PreferredNode = %q, want pve1", info.PreferredNode)
	}
	if got := info.Fingerprint(); got != mockpve.ClusterFingerprint {
		t.Errorf("Fingerprint() = %q, want the mock's issued fingerprint", got)
	}
	if len(info.Nodelist) != 1 || info.Nodelist[0].Name != "pve1" {
		t.Fatalf("Nodelist = %+v, want one entry named pve1", info.Nodelist)
	}
	if info.Nodelist[0].Extra["nodeid"] == "" {
		t.Error("Nodelist[0].Extra has no nodeid — lossless read lost unmodelled keys")
	}
	if info.Extra["totem"] == "" {
		t.Error("Extra has no totem — lossless read lost unmodelled keys")
	}

	mock.QueueClusterJoin("pve2")
	err = svc.JoinCluster(ctx, &cluster.JoinSpec{
		Hostname:    "192.0.2.11",
		Password:    "hunter2",
		Fingerprint: info.Fingerprint(),
	})
	if err != nil {
		t.Fatalf("JoinCluster: %v", err)
	}

	members, err := svc.ListConfigNodes(ctx)
	if err != nil {
		t.Fatalf("ListConfigNodes: %v", err)
	}
	names := make([]string, 0, len(members))
	for i := range members {
		names = append(names, members[i].NodeName())
	}
	if len(names) != 2 || names[0] != "pve1" || names[1] != "pve2" {
		t.Errorf("membership = %v, want [pve1 pve2]", names)
	}
}

func TestJoinClusterBadFingerprintRejected(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.SetClusterSelfNode("pve1")
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.CreateCluster(ctx, &cluster.ClusterCreateSpec{Name: "pvelab"}); err != nil {
		t.Fatalf("CreateCluster: %v", err)
	}
	mock.QueueClusterJoin("pve2")
	err := svc.JoinCluster(ctx, &cluster.JoinSpec{
		Hostname:    "192.0.2.11",
		Password:    "hunter2",
		Fingerprint: "00:11:22:33",
	})
	if err == nil {
		t.Fatal("JoinCluster with a wrong fingerprint succeeded, want error")
	}
	members, err := svc.ListConfigNodes(ctx)
	if err != nil {
		t.Fatalf("ListConfigNodes: %v", err)
	}
	if len(members) != 1 {
		t.Errorf("membership grew to %d after a rejected join, want 1", len(members))
	}
}

func TestCreateClusterTwiceErrors(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.CreateCluster(ctx, &cluster.ClusterCreateSpec{Name: "pvelab"}); err != nil {
		t.Fatalf("first CreateCluster: %v", err)
	}
	if err := svc.CreateCluster(ctx, &cluster.ClusterCreateSpec{Name: "again"}); err == nil {
		t.Error("second CreateCluster succeeded, want error")
	}
}

func TestJoinInfoStandaloneErrors(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)

	if _, err := svc.JoinInfo(context.Background()); err == nil {
		t.Error("JoinInfo on a standalone node succeeded, want error")
	}
}

func TestJoinClusterWithoutClusterErrors(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.QueueClusterJoin("pve2")
	svc := newService(t, mock)

	err := svc.JoinCluster(context.Background(), &cluster.JoinSpec{
		Hostname: "192.0.2.11", Password: "hunter2", Fingerprint: mockpve.ClusterFingerprint,
	})
	if err == nil {
		t.Error("JoinCluster with no cluster to join succeeded, want error")
	}
}

func TestListConfigNodesStandaloneEmpty(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)

	members, err := svc.ListConfigNodes(context.Background())
	if err != nil {
		t.Fatalf("ListConfigNodes: %v", err)
	}
	if len(members) != 0 {
		t.Errorf("standalone membership = %d entries, want 0", len(members))
	}
}

func TestClusterConfigWriteValidation(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.CreateCluster(ctx, nil); err == nil {
		t.Error("CreateCluster(nil) error = nil, want non-nil")
	}
	if err := svc.CreateCluster(ctx, &cluster.ClusterCreateSpec{}); err == nil {
		t.Error("CreateCluster with no Name error = nil, want non-nil")
	}
	if err := svc.JoinCluster(ctx, nil); err == nil {
		t.Error("JoinCluster(nil) error = nil, want non-nil")
	}
	for _, spec := range []*cluster.JoinSpec{
		{Password: "x", Fingerprint: "y"},
		{Hostname: "h", Fingerprint: "y"},
		{Hostname: "h", Password: "x"},
	} {
		if err := svc.JoinCluster(ctx, spec); err == nil {
			t.Errorf("JoinCluster(%+v) error = nil, want non-nil", spec)
		}
	}
}

// TestJoinInfoFingerprintFallback covers Fingerprint's selection order without
// a server: preferred node's entry first, then the first nodelist entry.
func TestJoinInfoFingerprintFallback(t *testing.T) {
	t.Parallel()
	info := cluster.JoinInfo{
		PreferredNode: "pve2",
		Nodelist: []cluster.JoinNode{
			{Name: "pve1", PVEFingerprint: "fp-1"},
			{Name: "pve2", PVEFingerprint: "fp-2"},
		},
	}
	if got := info.Fingerprint(); got != "fp-2" {
		t.Errorf("Fingerprint() = %q, want the preferred node's fp-2", got)
	}
	info.PreferredNode = "absent"
	if got := info.Fingerprint(); got != "fp-1" {
		t.Errorf("Fingerprint() fallback = %q, want the first entry's fp-1", got)
	}
	if got := (&cluster.JoinInfo{}).Fingerprint(); got != "" {
		t.Errorf("empty JoinInfo Fingerprint() = %q, want empty", got)
	}
}
