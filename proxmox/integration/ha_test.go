//go:build integration

package integration

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/cluster"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/ha"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/qemu"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// TestHAStatusReads exercises the two /cluster/ha/status reads (IMPL-0005):
// the per-row current view and the CRM master's manager_status blob. Reads
// only — safe anywhere, no gate. On a cluster with HA activity the rows are
// asserted structurally; an idle single node may legitimately return only the
// quorum row.
func TestHAStatusReads(t *testing.T) {
	c := newClient(t)
	h := c.HA()

	entries, err := h.HAStatusCurrent(testCtx(t))
	if err != nil {
		t.Fatalf("HAStatusCurrent: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("HAStatusCurrent returned no rows (expected at least the quorum row)")
	}
	for i := range entries {
		if entries[i].ID == "" && entries[i].Type == "" {
			t.Errorf("row %d has neither id nor type: %+v", i, entries[i])
		}
	}
	t.Logf("status/current: %d rows; armed-state=%q", len(entries), liveArmedState(entries))

	ms, err := h.GetManagerStatus(testCtx(t))
	if err != nil {
		t.Fatalf("GetManagerStatus: %v", err)
	}
	// The typed fields are provisional (the apidoc pins a bare object) — the
	// live run reconciles them (IMPL-0005 Phase 3). Log rather than assert so
	// a shape divergence is visible without failing the read itself.
	t.Logf("manager_status: master_node=%q nodes=%d services=%d extra-keys=%d",
		ms.MasterNode, len(ms.NodeStatus), len(ms.ServiceStatus), len(ms.Extra))
}

// TestHAArmDisarmCycle flips the cluster-wide HA switch (9.2+):
// baseline armed -> DisarmHA(freeze) -> observe armed-state -> ArmHA ->
// observe again, verifying the resource-mode semantics live. A cluster-wide
// switch has real blast radius, so it is gated on the explicit opt-in
// PVE_TEST_HA_ARM=1 (set it only on a disposable cluster — the pvelab lab; it
// must never ride along on a real-node session). Cleanup re-arms
// best-effort even mid-failure.
func TestHAArmDisarmCycle(t *testing.T) {
	if os.Getenv("PVE_TEST_HA_ARM") != "1" {
		t.Skip("HA arm/disarm cycle disabled (set PVE_TEST_HA_ARM=1 on a DISPOSABLE cluster only)")
	}
	c := newClient(t)
	if !c.Capabilities().HAClusterSwitch() {
		t.Skipf("cluster %s is below 9.2 — no HA arm/disarm switch", c.Capabilities())
	}
	h := c.HA()

	// Re-arm no matter how the test exits — never leave a cluster disarmed.
	t.Cleanup(func() {
		ctx, cancel := cleanupCtx()
		defer cancel()
		if err := h.ArmHA(ctx); err != nil {
			t.Logf("cleanup ArmHA: %v", err)
		}
	})

	entries, err := h.HAStatusCurrent(testCtx(t))
	if err != nil {
		t.Fatalf("HAStatusCurrent(baseline): %v", err)
	}
	if got := liveArmedState(entries); got != ha.ArmedStateArmed {
		// A prior run killed before its cleanup (no t.Cleanup on SIGKILL)
		// leaves the lab disarmed — recover rather than fail permanently.
		t.Logf("baseline armed-state = %q — recovering a wedged lab with ArmHA", got)
		if err := h.ArmHA(testCtx(t)); err != nil {
			t.Fatalf("recovery ArmHA: %v", err)
		}
		waitArmedState(t, h, ha.ArmedStateArmed)
	}

	if err := h.DisarmHA(testCtx(t), ha.ResourceModeFreeze); err != nil {
		t.Fatalf("DisarmHA(freeze): %v", err)
	}
	waitArmedState(t, h, ha.ArmedStateDisarmed)
	t.Log("disarm observed: armed-state=disarmed")

	if err := h.ArmHA(testCtx(t)); err != nil {
		t.Fatalf("ArmHA: %v", err)
	}
	waitArmedState(t, h, ha.ArmedStateArmed)
	t.Log("re-arm observed: armed-state=armed")
}

// TestHAResourceMigrate is the IMPL-0005 migrate criterion (its OQ-3a):
// a self-contained scratch-VM pair under a NEGATIVE resource-affinity rule —
// a migrate onto the partner's node must come back with blocking-resources
// (cause resource-affinity), and a migrate to a free third node must be
// accepted and converge, observed via HAStatusCurrent. Reuses the placement
// test's gates and helpers (quorate multi-node cluster — the pvelab lab).
func TestHAResourceMigrate(t *testing.T) {
	c := newClient(t)
	vmid1 := placementVMID(t, "PVE_TEST_PLACEMENT_VMID_1")
	vmid2 := placementVMID(t, "PVE_TEST_PLACEMENT_VMID_2")

	node := testNode()
	q := c.QEMU(node)
	ts := c.Tasks()
	h := c.HA()
	const rule = "sdk-itest-migrate"

	// Register cleanup before the first create: placementCleanup tolerates
	// missing entities, so a Fatal on the second create cannot strand the
	// first VM.
	t.Cleanup(func() { placementCleanup(t, c, rule, vmid1, vmid2) })
	for i, vmid := range []int{vmid1, vmid2} {
		ref, err := q.Create(testCtx(t), &qemu.CreateSpec{
			VMID:   types.VMID(vmid),
			Name:   "sdk-itest-migrate-" + strconv.Itoa(i+1),
			Memory: 256,
			Cores:  1,
		})
		if err != nil {
			t.Fatalf("Create(%d): %v", vmid, err)
		}
		mustSucceed(t, ts, ref, "create")
	}

	for _, vmid := range []int{vmid1, vmid2} {
		if err := h.AddResource(testCtx(t), &ha.HAResourceSpec{
			SID:     sid(vmid),
			State:   ha.StateStarted,
			Comment: "created by the SDK integration suite",
		}); err != nil {
			t.Fatalf("AddResource(%s): %v", sid(vmid), err)
		}
	}
	createRuleSettled(t, h, &ha.HARuleSpec{
		Rule:      rule,
		Type:      ha.RuleTypeResourceAffinity,
		Resources: []string{sid(vmid1), sid(vmid2)},
		Affinity:  "negative",
		Comment:   "created by the SDK integration suite",
	})
	n1, n2 := waitPlacement(t, c, vmid1, vmid2, func(a, b string) bool { return a != b })
	t.Logf("separated: vm:%d on %s, vm:%d on %s", vmid1, n1, vmid2, n2)

	// Blocked: migrating vm1 onto vm2's node conflicts with the negative rule.
	res, err := h.MigrateResource(testCtx(t), sid(vmid1), n2)
	if err != nil {
		t.Fatalf("MigrateResource(%s -> %s) errored (expected a blocking-resources result): %v",
			sid(vmid1), n2, err)
	}
	if len(res.BlockingResources) == 0 {
		t.Fatalf("conflicting migrate returned no blocking-resources: %+v", res)
	}
	if got := res.BlockingResources[0].Cause; got != ha.BlockingCauseResourceAffinity {
		t.Errorf("blocking cause = %q, want %q", got, ha.BlockingCauseResourceAffinity)
	}
	t.Logf("blocked migrate reported: %+v", res.BlockingResources)

	// Accepted: a third node conflicts with nothing.
	target := freeNode(t, c, n1, n2)
	if target == "" {
		t.Skip("no third node available — the accepted-migrate half needs >= 3 nodes")
	}
	res, err = h.MigrateResource(testCtx(t), sid(vmid1), target)
	if err != nil {
		t.Fatalf("MigrateResource(%s -> %s): %v", sid(vmid1), target, err)
	}
	if res.RequestedNode != target {
		t.Errorf("requested-node = %q, want %q", res.RequestedNode, target)
	}
	waitServiceNode(t, h, sid(vmid1), target)
	t.Logf("migrate converged: %s on %s", sid(vmid1), target)
}

// liveArmedState extracts the cluster's armed-state from a status read —
// the master row's value, falling back to any row that carries one (the
// field's row placement is a live-verify item).
func liveArmedState(entries []ha.HAStatusEntry) ha.ArmedState {
	for i := range entries {
		if entries[i].Type == "master" && entries[i].ArmedState != "" {
			return entries[i].ArmedState
		}
	}
	for i := range entries {
		if entries[i].ArmedState != "" {
			return entries[i].ArmedState
		}
	}
	return ""
}

// waitArmedState polls status/current until the cluster reports want,
// bounded by placementPollCeiling.
func waitArmedState(t *testing.T, h ha.API, want ha.ArmedState) {
	t.Helper()
	interval := placementPollLive
	if os.Getenv(envReplay) == "1" {
		interval = placementPollReplay
	}
	ctx, cancel := context.WithTimeout(context.Background(), placementPollCeiling)
	defer cancel()
	var last ha.ArmedState
	for {
		entries, err := h.HAStatusCurrent(ctx)
		if err == nil {
			if last = liveArmedState(entries); last == want {
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("armed-state never reached %q within %s (last %q, err %v)",
				want, placementPollCeiling, last, err)
		case <-time.After(interval):
		}
	}
}

// waitServiceNode polls status/current until the service row for sid reports
// node — the documented convergence observable for migrate/relocate.
func waitServiceNode(t *testing.T, h ha.API, sid, node string) {
	t.Helper()
	interval := placementPollLive
	if os.Getenv(envReplay) == "1" {
		interval = placementPollReplay
	}
	ctx, cancel := context.WithTimeout(context.Background(), placementPollCeiling)
	defer cancel()
	var last string
	for {
		entries, err := h.HAStatusCurrent(ctx)
		if err == nil {
			last = ""
			for i := range entries {
				if entries[i].Type == "service" && entries[i].SID == sid {
					last = entries[i].Node
				}
			}
			if last == node {
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("%s never converged on %s within %s (last on %q, err %v)",
				sid, node, placementPollCeiling, last, err)
		case <-time.After(interval):
		}
	}
}

// freeNode returns an ONLINE cluster node that is neither n1 nor n2, or ""
// when the cluster has no such third node (a downed node would burn the full
// convergence ceiling before failing).
func freeNode(t *testing.T, c *proxmox.Client, n1, n2 string) string {
	t.Helper()
	resources, err := c.Cluster().ListResources(testCtx(t), cluster.WithResourceType(cluster.ResourceTypeNode))
	if err != nil {
		t.Fatalf("ListResources(nodes): %v", err)
	}
	for i := range resources {
		name := resources[i].Node
		if name != "" && name != n1 && name != n2 && resources[i].Status == "online" {
			return name
		}
	}
	return ""
}
