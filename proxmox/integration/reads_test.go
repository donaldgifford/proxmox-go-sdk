//go:build integration

package integration

import "testing"

// TestVersionRoundTrip is the Phase 1 live criterion: auth + GET /version
// round-trips against a real 9.x node and reports a >= 9.0 release.
func TestVersionRoundTrip(t *testing.T) {
	c := newClient(t)
	caps, err := c.Version().Capabilities(testCtx(t))
	if err != nil {
		t.Fatalf("Version().Capabilities: %v", err)
	}
	if !caps.AtLeast(9, 0) {
		t.Fatalf("reported version %s is below the 9.0 floor", caps.String())
	}
	t.Logf("connected to PVE %s", caps.String())
}

// TestComputeReads exercises the read side of Phase 2 against a live node:
// listing QEMU VMs and LXC containers on the node must succeed.
func TestComputeReads(t *testing.T) {
	c := newClient(t)
	ctx := testCtx(t)
	node := testNode()

	vms, err := c.QEMU(node).List(ctx)
	if err != nil {
		t.Fatalf("QEMU(%s).List: %v", node, err)
	}
	cts, err := c.LXC(node).List(ctx)
	if err != nil {
		t.Fatalf("LXC(%s).List: %v", node, err)
	}
	t.Logf("node %s: %d VM(s), %d container(s)", node, len(vms), len(cts))
}

// TestStorageReads covers Phase 3's read criterion: datastore listing.
func TestStorageReads(t *testing.T) {
	c := newClient(t)
	stores, err := c.Storage().ListDatastores(testCtx(t))
	if err != nil {
		t.Fatalf("Storage().ListDatastores: %v", err)
	}
	if len(stores) == 0 {
		t.Error("ListDatastores returned no stores; a live node always has at least one")
	}
}

// TestClusterAndHAReads covers Phase 4's read side: cluster resource inventory
// and HA resource listing (both empty-but-nil-error on a single node).
func TestClusterAndHAReads(t *testing.T) {
	c := newClient(t)
	ctx := testCtx(t)

	if _, err := c.Cluster().ListResources(ctx); err != nil {
		t.Fatalf("Cluster().ListResources: %v", err)
	}
	if _, err := c.HA().ListResources(ctx); err != nil {
		t.Fatalf("HA().ListResources: %v", err)
	}
}

// TestNetworkReads covers Phase 5's read criterion: enumerating zones, VNets,
// and fabrics without error. (The live-status half is pverr.ErrUnsupported in
// 9.x, so it is not exercised here.)
func TestNetworkReads(t *testing.T) {
	c := newClient(t)
	ctx := testCtx(t)

	if _, err := c.SDN().ListZones(ctx); err != nil {
		t.Fatalf("SDN().ListZones: %v", err)
	}
	if _, err := c.SDN().ListVNets(ctx); err != nil {
		t.Fatalf("SDN().ListVNets: %v", err)
	}
	if _, err := c.SDN().ListFabrics(ctx); err != nil {
		t.Fatalf("SDN().ListFabrics: %v", err)
	}
}
