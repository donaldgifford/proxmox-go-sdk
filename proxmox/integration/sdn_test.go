//go:build integration

package integration

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/sdn"
)

// Fabric-convergence cadence: FRR adjacencies come up within a few hello
// intervals once the SDN apply lands, so convergence sits well inside the
// ceiling on a healthy lab. Replay serves recorded polls instantly.
const (
	fabricPollCeiling = 3 * time.Minute
	fabricPollLive    = 10 * time.Second
	fabricPollReplay  = 50 * time.Millisecond
)

// TestSDNStatusReads covers the node-scoped SDN live-status surface
// (DESIGN-0003) read-only: zone status on the test node must round-trip, and
// each reported zone's content must decode. Safe against any node — an empty
// SDN config is a valid result (empty list, nil error).
func TestSDNStatusReads(t *testing.T) {
	c := newClient(t)
	ctx := testCtx(t)
	node := testNode()

	zones, err := c.SDN().SDNStatus(ctx, node)
	if err != nil {
		t.Fatalf("SDN().SDNStatus(%s): %v", node, err)
	}
	t.Logf("node %s reports %d SDN zone(s)", node, len(zones))
	for _, z := range zones {
		vnets, err := c.SDN().ZoneContent(ctx, node, z.Zone)
		if err != nil {
			t.Fatalf("SDN().ZoneContent(%s, %s): %v", node, z.Zone, err)
		}
		t.Logf("zone %s (%s): %d vnet(s)", z.Zone, z.Status, len(vnets))
	}
}

// TestSDNFabricLifecycle is DESIGN-0003's live criterion: create an OpenFabric
// fabric spanning the lab nodes, enroll each node, apply, watch the fabric
// converge (neighbors appear via FRR), read the runtime tables, and tear it
// all down. It needs the quorate pvelab nested cluster and is gated on
// PVE_TEST_FABRIC_NODES (comma-separated node names, at least two) and
// PVE_TEST_FABRIC_IFACE (the fabric-facing interface name, identical on every
// lab clone). Wire-form notes baked in from the 9.2 apidoc: the per-node `ip`
// is a bare IPv4, and each `interfaces` entry is a property string
// ("name=<iface>").
func TestSDNFabricLifecycle(t *testing.T) {
	c := newClient(t)
	nodes := fabricNodes(t)
	iface := os.Getenv(envFabricIface)
	if iface == "" {
		t.Skipf("fabric test disabled (set %s to the fabric-facing interface name)", envFabricIface)
	}

	const fabric = "sdkfab0" // fits the pve-sdn-fabric-id 2–8 char pattern.
	s := c.SDN()

	if err := s.CreateFabric(testCtx(t), &sdn.FabricSpec{
		Fabric:   fabric,
		Protocol: sdn.FabricProtocolOpenFabric,
		IPPrefix: "10.99.99.0/24",
	}); err != nil {
		t.Fatalf("CreateFabric(%s): %v", fabric, err)
	}
	t.Cleanup(func() { fabricCleanup(t, c, fabric, nodes) })

	for i, node := range nodes {
		if err := s.CreateFabricNode(testCtx(t), fabric, &sdn.FabricNodeSpec{
			NodeID:     node,
			Protocol:   sdn.FabricProtocolOpenFabric,
			IP:         "10.99.99." + strconv.Itoa(i+1),
			Interfaces: []string{"name=" + iface},
		}); err != nil {
			t.Fatalf("CreateFabricNode(%s, %s): %v", fabric, node, err)
		}
	}

	members, err := s.ListFabricNodes(testCtx(t), fabric)
	if err != nil {
		t.Fatalf("ListFabricNodes(%s): %v", fabric, err)
	}
	if len(members) != len(nodes) {
		t.Fatalf("ListFabricNodes(%s) = %d member(s), want %d", fabric, len(members), len(nodes))
	}
	n, err := s.GetFabricNode(testCtx(t), fabric, nodes[0])
	if err != nil {
		t.Fatalf("GetFabricNode(%s, %s): %v", fabric, nodes[0], err)
	}
	if n.IP != "10.99.99.1" {
		t.Errorf("fabric node ip = %q, want 10.99.99.1", n.IP)
	}

	if err := s.ApplySDN(testCtx(t)); err != nil {
		t.Fatalf("ApplySDN: %v", err)
	}

	// Runtime reads on the first node: interfaces immediately, neighbors once
	// FRR converges.
	ifaces, err := s.FabricInterfaces(testCtx(t), nodes[0], fabric)
	if err != nil {
		t.Fatalf("FabricInterfaces(%s, %s): %v", nodes[0], fabric, err)
	}
	t.Logf("node %s: %d fabric interface(s)", nodes[0], len(ifaces))

	neighbors := waitFabricNeighbors(t, s, nodes[0], fabric)
	t.Logf("node %s: fabric converged with %d neighbor(s), first %q (%s, up %s)",
		nodes[0], len(neighbors), neighbors[0].Neighbor, neighbors[0].Status, neighbors[0].Uptime)

	routes, err := s.FabricRoutes(testCtx(t), nodes[0], fabric)
	if err != nil {
		t.Fatalf("FabricRoutes(%s, %s): %v", nodes[0], fabric, err)
	}
	t.Logf("node %s: %d fabric route(s)", nodes[0], len(routes))
}

// fabricNodes reads the PVE_TEST_FABRIC_NODES gate, skipping unless it names
// at least two nodes (one node has no neighbors to converge with).
func fabricNodes(t *testing.T) []string {
	t.Helper()
	raw := os.Getenv(envFabricNodes)
	if raw == "" {
		t.Skipf("fabric test disabled (set %s; needs the quorate pvelab cluster)", envFabricNodes)
	}
	nodes := strings.Split(raw, ",")
	for i := range nodes {
		nodes[i] = strings.TrimSpace(nodes[i])
	}
	if len(nodes) < 2 {
		t.Fatalf("%s=%q names %d node(s); the fabric needs at least two", envFabricNodes, raw, len(nodes))
	}
	return nodes
}

// waitFabricNeighbors polls the first node's neighbor table until at least one
// neighbor appears, bounded by fabricPollCeiling.
func waitFabricNeighbors(t *testing.T, s *sdn.Service, node, fabric string) []sdn.FabricNeighbor {
	t.Helper()
	interval := fabricPollLive
	if os.Getenv(envReplay) == "1" {
		interval = fabricPollReplay
	}
	ctx, cancel := context.WithTimeout(context.Background(), fabricPollCeiling)
	defer cancel()
	for {
		neighbors, err := s.FabricNeighbors(testCtx(t), node, fabric)
		if err == nil && len(neighbors) > 0 {
			return neighbors
		}
		select {
		case <-ctx.Done():
			t.Fatalf("fabric never converged within %s on %s (last: %d neighbor(s), err %v)",
				fabricPollCeiling, node, len(neighbors), err)
		case <-time.After(interval):
		}
	}
}

// fabricCleanup tears down in dependency order — membership, then the fabric,
// then an apply to push the removal — each best-effort under one bounded
// context so a wedged step cannot hang the suite.
func fabricCleanup(t *testing.T, c *proxmox.Client, fabric string, nodes []string) {
	t.Helper()
	ctx, cancel := cleanupCtx()
	defer cancel()

	s := c.SDN()
	for _, node := range nodes {
		if err := s.DeleteFabricNode(ctx, fabric, node); err != nil {
			t.Logf("cleanup DeleteFabricNode(%s, %s): %v", fabric, node, err)
		}
	}
	if err := s.DeleteFabric(ctx, fabric); err != nil {
		t.Logf("cleanup DeleteFabric(%s): %v", fabric, err)
	}
	if err := s.ApplySDN(ctx); err != nil {
		t.Logf("cleanup ApplySDN: %v", err)
	}
}
