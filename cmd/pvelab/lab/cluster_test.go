package lab

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/cluster"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/version"
)

// clusterTestConfig is provisionTestConfig with the cluster name formation
// needs.
func clusterTestConfig() *Config {
	cfg := provisionTestConfig()
	cfg.Nested.ClusterName = "pvelab"
	return cfg
}

// newClusterMockDial backs the dialer seam with ONE mock playing every node:
// the mock's cluster-config state is the shared "world", so membership grown
// by a join on any endpoint is visible to the convergence polls. The dialer
// ignores the endpoint — routing per node is real PVE behaviour the mock
// cannot emulate (one Server is one state).
func newClusterMockDial(t *testing.T) (*mockpve.Server, clusterDialer) {
	t.Helper()
	mock := mockpve.New()
	mock.SeedVersion("9.2.4", "9.2", "test")
	c, cleanup := mock.NewClient()
	t.Cleanup(cleanup)
	svc := cluster.NewService(c, version.Capabilities{})
	return mock, func(context.Context, string) (cluster.API, error) { return svc, nil }
}

// seedQuorateStatus makes the mock's /cluster/status report a quorate 3-node
// cluster (the mock does not derive status from formation; it is seeded).
func seedQuorateStatus(mock *mockpve.Server, quorate bool) {
	mock.SetClusterStatusInfo("pvelab", 3, quorate)
	mock.AddClusterStatusNode("pve1-dogfood", true)
	mock.AddClusterStatusNode("pve2-dogfood", true)
	mock.AddClusterStatusNode("pve3-dogfood", true)
}

func TestFormClusterHappyPath(t *testing.T) {
	t.Parallel()
	cfg := clusterTestConfig()
	mock, dial := newClusterMockDial(t)
	mock.SetClusterSelfNode("pve1-dogfood")
	mock.QueueClusterJoin("pve2-dogfood")
	mock.QueueClusterJoin("pve3-dogfood")
	seedQuorateStatus(mock, true)

	err := formCluster(context.Background(), cfg, "hunter2", dial,
		time.Millisecond, time.Second, time.Second, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("formCluster: %v", err)
	}

	svc, err := dial(context.Background(), "")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	members, err := svc.ListConfigNodes(context.Background())
	if err != nil {
		t.Fatalf("ListConfigNodes: %v", err)
	}
	names := make([]string, 0, len(members))
	for i := range members {
		names = append(names, members[i].NodeName())
	}
	want := []string{"pve1-dogfood", "pve2-dogfood", "pve3-dogfood"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("membership = %v, want %v", names, want)
	}
}

// TestFormClusterJoinNeverConverges starves the third node's join (nothing
// queued → the mock rejects it; formCluster swallows the join error by
// design) and asserts the bounded membership poll surfaces it, naming the
// node.
func TestFormClusterJoinNeverConverges(t *testing.T) {
	t.Parallel()
	cfg := clusterTestConfig()
	mock, dial := newClusterMockDial(t)
	mock.SetClusterSelfNode("pve1-dogfood")
	mock.QueueClusterJoin("pve2-dogfood") // pve3's join has nothing queued.
	seedQuorateStatus(mock, true)

	err := formCluster(context.Background(), cfg, "hunter2", dial,
		time.Millisecond, 50*time.Millisecond, time.Second, slog.New(slog.DiscardHandler))
	if err == nil {
		t.Fatal("formCluster with a never-converging join succeeded, want error")
	}
	if !strings.Contains(err.Error(), "pve3-dogfood") {
		t.Errorf("error %q does not name the stuck node pve3-dogfood", err)
	}
}

// TestFormClusterCreateFails pre-forms the cluster so CreateCluster hits the
// double-create rejection — a create failure is fatal, not swallowed.
func TestFormClusterCreateFails(t *testing.T) {
	t.Parallel()
	cfg := clusterTestConfig()
	mock, dial := newClusterMockDial(t)
	mock.SetClusterSelfNode("pve1-dogfood")

	svc, err := dial(context.Background(), "")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if err := svc.CreateCluster(context.Background(), &cluster.ClusterCreateSpec{Name: "leftover"}); err != nil {
		t.Fatalf("pre-create: %v", err)
	}

	err = formCluster(context.Background(), cfg, "hunter2", dial,
		time.Millisecond, time.Second, time.Second, slog.New(slog.DiscardHandler))
	if err == nil || !strings.Contains(err.Error(), "create cluster") {
		t.Errorf("formCluster = %v, want a create-cluster error", err)
	}
}

// TestFormClusterQuorumTimeout forms fine but the seeded status never reports
// quorate, so the final quorum poll must fail within its bound.
func TestFormClusterQuorumTimeout(t *testing.T) {
	t.Parallel()
	cfg := clusterTestConfig()
	mock, dial := newClusterMockDial(t)
	mock.SetClusterSelfNode("pve1-dogfood")
	mock.QueueClusterJoin("pve2-dogfood")
	mock.QueueClusterJoin("pve3-dogfood")
	seedQuorateStatus(mock, false)

	err := formCluster(context.Background(), cfg, "hunter2", dial,
		time.Millisecond, time.Second, 50*time.Millisecond, slog.New(slog.DiscardHandler))
	if err == nil || !strings.Contains(err.Error(), "quorate") {
		t.Errorf("formCluster = %v, want a quorum-timeout error", err)
	}
}

// TestNestedClusterDialer exercises the production dialer against the mock's
// password-ticket flow (the versionProbe test pattern).
func TestNestedClusterDialer(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.SeedVersion("9.2.4", "9.2", "test")
	mock.AddUser("root@pam", "hunter2")
	mock.SetClusterSelfNode("pve1-dogfood")
	ts := mock.Serve()
	t.Cleanup(ts.Close)

	dial := nestedClusterDialer("hunter2")
	svc, err := dial(context.Background(), ts.URL)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if err := svc.CreateCluster(context.Background(), &cluster.ClusterCreateSpec{Name: "pvelab"}); err != nil {
		t.Fatalf("CreateCluster through the production dialer: %v", err)
	}
	members, err := svc.ListConfigNodes(context.Background())
	if err != nil {
		t.Fatalf("ListConfigNodes: %v", err)
	}
	if len(members) != 1 || members[0].NodeName() != "pve1-dogfood" {
		t.Errorf("membership = %+v, want [pve1-dogfood]", members)
	}

	if _, err := dial(context.Background(), "https://127.0.0.1:1"); err == nil {
		t.Error("dial against a dead endpoint succeeded, want error")
	}
}
