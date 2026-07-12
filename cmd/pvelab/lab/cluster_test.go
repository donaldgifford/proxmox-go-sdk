package lab

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/cluster"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
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

// quorumWorld scripts the real-PVE settling window the per-join quorum gate
// exists for (found live 2026-07-12): a join puts the member in the config
// nodelist immediately, but the member only reports online — and the cluster
// quorate — two status polls later. The event journal proves the next join
// waits for quorum instead of firing on config presence.
type quorumWorld struct {
	mu         sync.Mutex
	events     []string
	members    []string
	lag        map[string]int    // status polls left before a member is online.
	byEndpoint map[string]string // endpoint → node name (who a dial reaches).
}

// quorumView is quorumWorld seen through one endpoint's dial. The embedded
// nil API panics on any call the formation flow is not expected to make.
type quorumView struct {
	cluster.API

	w        *quorumWorld
	endpoint string
}

func (v *quorumView) CreateCluster(_ context.Context, _ *cluster.ClusterCreateSpec) error {
	v.w.mu.Lock()
	defer v.w.mu.Unlock()
	v.w.members = []string{v.w.byEndpoint[v.endpoint]}
	return nil
}

func (*quorumView) JoinInfo(_ context.Context) (*cluster.JoinInfo, error) {
	return &cluster.JoinInfo{
		PreferredNode: "pve1-dogfood",
		Nodelist:      []cluster.JoinNode{{Name: "pve1-dogfood", PVEFingerprint: "AA:BB"}},
	}, nil
}

// JoinCluster is served by the JOINING node: the view's endpoint says who
// joins. Config membership grows now; runtime health lags two polls.
func (v *quorumView) JoinCluster(_ context.Context, _ *cluster.JoinSpec) error {
	v.w.mu.Lock()
	defer v.w.mu.Unlock()
	name := v.w.byEndpoint[v.endpoint]
	v.w.members = append(v.w.members, name)
	v.w.lag[name] = 2
	v.w.events = append(v.w.events, "join:"+name)
	return nil
}

func (v *quorumView) ListConfigNodes(_ context.Context) ([]cluster.ConfigNode, error) {
	v.w.mu.Lock()
	defer v.w.mu.Unlock()
	out := make([]cluster.ConfigNode, 0, len(v.w.members))
	for _, m := range v.w.members {
		out = append(out, cluster.ConfigNode{Name: m})
	}
	return out, nil
}

// GetStatus ages the world one poll: lagging members come online only after
// their countdown, and the cluster is quorate only once nobody lags.
func (v *quorumView) GetStatus(_ context.Context) ([]cluster.StatusEntry, error) {
	v.w.mu.Lock()
	defer v.w.mu.Unlock()
	quorumOK := true
	entries := make([]cluster.StatusEntry, 0, len(v.w.members)+1)
	for _, m := range v.w.members {
		online := v.w.lag[m] == 0
		if !online {
			quorumOK = false
			v.w.lag[m]--
		}
		entries = append(entries, cluster.StatusEntry{
			Type: "node", Name: m, Online: types.PVEBool(online),
		})
	}
	entries = append(entries, cluster.StatusEntry{
		Type: "cluster", Name: "pvelab",
		Nodes: len(v.w.members), Quorate: types.PVEBool(quorumOK),
	})
	v.w.events = append(v.w.events,
		fmt.Sprintf("status:n=%d,q=%t", len(v.w.members), quorumOK))
	return entries, nil
}

// TestFormClusterGatesNextJoinOnQuorum pins the fix for the live 2026-07-12
// failure: pve3's join must not be issued while the cluster is still settling
// from pve2's — the gate has to observe non-quorate status for the 2-member
// cluster, then quorate, before join:pve3 appears.
func TestFormClusterGatesNextJoinOnQuorum(t *testing.T) {
	t.Parallel()
	cfg := clusterTestConfig()
	w := &quorumWorld{lag: map[string]int{}, byEndpoint: map[string]string{}}
	for _, n := range cfg.Nested.Nodes {
		endpoint, err := nodeEndpoint(n)
		if err != nil {
			t.Fatalf("nodeEndpoint(%s): %v", n.Name, err)
		}
		w.byEndpoint[endpoint] = n.Name
	}
	dial := func(_ context.Context, endpoint string) (cluster.API, error) {
		return &quorumView{w: w, endpoint: endpoint}, nil
	}

	err := formCluster(context.Background(), cfg, "hunter2", dial,
		time.Millisecond, time.Second, time.Second, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("formCluster: %v", err)
	}

	join3 := -1
	for i, e := range w.events {
		if e == "join:pve3-dogfood" {
			join3 = i
			break
		}
	}
	if join3 == -1 {
		t.Fatalf("join:pve3-dogfood never happened; events = %v", w.events)
	}
	sawSettling, sawQuorate := false, false
	for _, e := range w.events[:join3] {
		switch e {
		case "status:n=2,q=false":
			sawSettling = true
		case "status:n=2,q=true":
			sawQuorate = true
		}
	}
	if !sawSettling || !sawQuorate {
		t.Errorf("join:pve3 fired without the gate observing the 2-member settle "+
			"(saw non-quorate=%t, quorate=%t); events before it = %v",
			sawSettling, sawQuorate, w.events[:join3])
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
