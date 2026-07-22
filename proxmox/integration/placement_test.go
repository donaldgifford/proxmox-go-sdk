//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/cluster"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/ha"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/qemu"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// Placement-poll cadence: the HA manager reacts to a rule change within a few
// LRM/CRM cycles (~10 s each), so placement lands well inside the 5-minute
// bound on a healthy cluster (DESIGN-0002). Replay serves recorded polls
// instantly, so the interval shrinks to keep CI fast.
const (
	placementPollCeiling = 5 * time.Minute
	placementPollLive    = 10 * time.Second
	placementPollReplay  = 50 * time.Millisecond
)

// TestResourceAffinityPlacement is the IMPL-0001 Phase 4 Success Criterion,
// scheduler-observed (design OQ-9, replacing the retired rule-only
// TestResourceAffinityRule): two diskless dummy VMs are placed under HA
// management, a NEGATIVE resource-affinity rule must drive them onto different
// nodes, and flipping the rule to POSITIVE must co-locate them. It needs a
// quorate multi-node cluster (the pvelab nested lab) and is gated on
// PVE_TEST_PLACEMENT_VMID_1/2 — .pvelab.env sets both. Cleanup removes the
// rule, the HA resources, and the VMs, in that order (a rule referencing a
// vanished resource wedges deletes).
func TestResourceAffinityPlacement(t *testing.T) {
	c := newClient(t)

	vmid1 := placementVMID(t, "PVE_TEST_PLACEMENT_VMID_1")
	vmid2 := placementVMID(t, "PVE_TEST_PLACEMENT_VMID_2")

	node := testNode()
	q := c.QEMU(node)
	ts := c.Tasks()
	h := c.HA()
	const rule = "sdk-itest-placement"

	// Register cleanup before the first create: placementCleanup tolerates
	// missing entities, so a Fatal on the second create cannot strand the
	// first VM.
	t.Cleanup(func() { placementCleanup(t, c, rule, vmid1, vmid2) })

	// Two diskless dummies: nothing to boot is fine — QEMU still runs and the
	// HA manager still places them.
	for i, vmid := range []int{vmid1, vmid2} {
		ref, err := q.Create(testCtx(t), &qemu.CreateSpec{
			VMID:   types.VMID(vmid),
			Name:   "sdk-itest-placement-" + strconv.Itoa(i+1),
			Memory: 256,
			Cores:  1,
		})
		if err != nil {
			t.Fatalf("Create(%d): %v", vmid, err)
		}
		mustSucceed(t, ts, ref, "create")
	}

	// HA-manage both (state=started: the HA manager starts and places them).
	for _, vmid := range []int{vmid1, vmid2} {
		if err := h.AddResource(testCtx(t), &ha.HAResourceSpec{
			SID:     sid(vmid),
			State:   ha.StateStarted,
			Comment: "created by the SDK integration suite",
		}); err != nil {
			t.Fatalf("AddResource(%s): %v", sid(vmid), err)
		}
	}

	// NEGATIVE affinity: the scheduler must separate them.
	createRuleSettled(t, h, &ha.HARuleSpec{
		Rule:      rule,
		Type:      ha.RuleTypeResourceAffinity,
		Resources: []string{sid(vmid1), sid(vmid2)},
		Affinity:  "negative",
		Comment:   "created by the SDK integration suite",
	})
	n1, n2 := waitPlacement(t, c, vmid1, vmid2, func(a, b string) bool { return a != b })
	t.Logf("negative affinity honoured: vm:%d on %s, vm:%d on %s", vmid1, n1, vmid2, n2)

	// Flip to POSITIVE: the scheduler must co-locate them. The update carries
	// the type and the type's required properties — PVE's plugin schema keeps
	// them required on update, and a bare affinity-only PUT was rejected with
	// "Parameter verification failed." live (2026-07-12).
	if err := h.UpdateRule(testCtx(t), rule, &ha.HARuleUpdate{
		Type:      ha.RuleTypeResourceAffinity,
		Resources: []string{sid(vmid1), sid(vmid2)},
		Affinity:  "positive",
	}); err != nil {
		t.Fatalf("UpdateRule(positive): %v", err)
	}
	n1, _ = waitPlacement(t, c, vmid1, vmid2, func(a, b string) bool { return a == b })
	t.Logf("positive affinity honoured: vm:%d and vm:%d co-located on %s", vmid1, vmid2, n1)
}

// createRuleSettled issues CreateRule, retrying while the HA stack is still
// activating. On a cluster whose FIRST HA resources were just added (the
// fresh pvelab lab), the rule feasibility check counts HA-ACTIVE nodes — the
// LRMs, which take a few 10 s cycles to come up after AddResource — not
// cluster members, so an immediate create fails with "rule defines more
// resources than available nodes" (observed live 2026-07-12, 3.9 s into a
// quorate 3-node cluster). Any other error is fatal immediately.
func createRuleSettled(t *testing.T, h ha.API, spec *ha.HARuleSpec) {
	t.Helper()
	interval := placementPollLive
	if os.Getenv(envReplay) == "1" {
		interval = placementPollReplay
	}
	ctx, cancel := context.WithTimeout(context.Background(), placementPollCeiling)
	defer cancel()
	for {
		err := h.CreateRule(testCtx(t), spec)
		if err == nil {
			return
		}
		if !strings.Contains(err.Error(), "more resources than available nodes") {
			t.Fatalf("CreateRule(%s): %v", spec.Rule, err)
		}
		t.Logf("HA stack still activating (LRMs not up yet) — retrying rule create: %v", err)
		select {
		case <-ctx.Done():
			t.Fatalf("CreateRule(%s): HA stack never settled within %s (last: %v)",
				spec.Rule, placementPollCeiling, err)
		case <-time.After(interval):
		}
	}
}

// placementVMID reads and parses one placement-VM gate, skipping the test when
// it is unset.
func placementVMID(t *testing.T, env string) int {
	t.Helper()
	raw := os.Getenv(env)
	if raw == "" {
		t.Skipf("placement test disabled (set %s; needs a quorate multi-node cluster)", env)
	}
	vmid, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatalf("%s=%q is not an integer: %v", env, raw, err)
	}
	return vmid
}

func sid(vmid int) string { return "vm:" + strconv.Itoa(vmid) }

// waitPlacement polls the cluster resource inventory until both VMs report a
// node and the relation holds, bounded by placementPollCeiling.
func waitPlacement(t *testing.T, c *proxmox.Client, vmid1, vmid2 int,
	relation func(node1, node2 string) bool,
) (node1, node2 string) {
	t.Helper()
	interval := placementPollLive
	if os.Getenv(envReplay) == "1" {
		interval = placementPollReplay
	}
	ctx, cancel := context.WithTimeout(context.Background(), placementPollCeiling)
	defer cancel()
	for {
		var err error
		node1, node2, err = placementNodes(ctx, c, vmid1, vmid2)
		if err == nil && node1 != "" && node2 != "" && relation(node1, node2) {
			return node1, node2
		}
		select {
		case <-ctx.Done():
			t.Fatalf("placement not honoured within %s: vm:%d on %q, vm:%d on %q (last err %v)",
				placementPollCeiling, vmid1, node1, vmid2, node2, err)
		case <-time.After(interval):
		}
	}
}

// placementNodes reads both VMs' current nodes from /cluster/resources.
func placementNodes(ctx context.Context, c *proxmox.Client, vmid1, vmid2 int) (node1, node2 string, err error) {
	resources, err := c.Cluster().ListResources(ctx, cluster.WithResourceType(cluster.ResourceTypeVM))
	if err != nil {
		return "", "", fmt.Errorf("ListResources: %w", err)
	}
	for i := range resources {
		switch resources[i].VMID {
		case vmid1:
			node1 = resources[i].Node
		case vmid2:
			node2 = resources[i].Node
		}
	}
	return node1, node2, nil
}

// placementCleanup tears down in dependency order: rule → HA resources →
// (stop +) delete VMs, each best-effort under its own bounded context so a
// wedged step cannot hang the suite. The scheduler may have MIGRATED a VM off
// the node that created it, so each VM's current node is resolved from the
// cluster inventory before the node-scoped stop/delete.
func placementCleanup(t *testing.T, c *proxmox.Client, rule string, vmids ...int) {
	t.Helper()
	ctx, cancel := cleanupCtx()
	defer cancel()

	if err := c.HA().DeleteRule(ctx, rule); err != nil {
		t.Logf("cleanup DeleteRule(%s): %v", rule, err)
	}
	for _, vmid := range vmids {
		if err := c.HA().RemoveResource(ctx, sid(vmid)); err != nil {
			t.Logf("cleanup RemoveResource(%s): %v", sid(vmid), err)
		}
	}
	for _, vmid := range vmids {
		deleteSettled(ctx, t, c, vmid)
	}
}

// deleteSettled stops (best-effort) then deletes one VM, retrying the round
// while an in-flight HA action holds the guest — found live 2026-07-12: the
// manager was still migrating when cleanup ran, the stop task failed with
// "VM is locked (migrate)" and the delete with "VM 9301 is running". Each
// retry re-resolves the node (the blocking action may BE a migration).
// Bounded by ctx (the cleanup context).
func deleteSettled(ctx context.Context, t *testing.T, c *proxmox.Client, vmid int) {
	t.Helper()
	ts := c.Tasks()
	interval := placementPollLive
	if os.Getenv(envReplay) == "1" {
		interval = placementPollReplay
	}
	for {
		q := c.QEMU(currentNode(ctx, c, vmid))
		// HA started them; stop before delete (best-effort — HA removal can
		// already have stopped them).
		if sref, serr := q.Stop(ctx, vmid); serr != nil {
			t.Logf("cleanup Stop(%d): %v", vmid, serr)
		} else if _, werr := ts.Wait(ctx, sref); werr != nil {
			t.Logf("cleanup Wait(stop %d): %v", vmid, werr)
		}
		dref, derr := q.Delete(ctx, vmid)
		if derr == nil {
			if _, werr := ts.Wait(ctx, dref); werr != nil {
				t.Logf("cleanup Wait(delete %d): %v", vmid, werr)
			}
			return
		}
		msg := derr.Error()
		if !strings.Contains(msg, "is running") && !strings.Contains(msg, "lock") {
			t.Logf("cleanup Delete(%d): %v", vmid, derr)
			return
		}
		t.Logf("cleanup Delete(%d) blocked by an in-flight HA action — retrying: %v", vmid, derr)
		select {
		case <-ctx.Done():
			t.Logf("cleanup Delete(%d): gave up: %v", vmid, ctx.Err())
			return
		case <-time.After(interval):
		}
	}
}

// currentNode resolves where the cluster currently places vmid, falling back
// to the configured test node when the inventory read fails or misses it.
func currentNode(ctx context.Context, c *proxmox.Client, vmid int) string {
	resources, err := c.Cluster().ListResources(ctx, cluster.WithResourceType(cluster.ResourceTypeVM))
	if err != nil {
		return testNode()
	}
	for i := range resources {
		if resources[i].VMID == vmid && resources[i].Node != "" {
			return resources[i].Node
		}
	}
	return testNode()
}
