//go:build integration

package integration

import (
	"context"
	"os"
	"strconv"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/qemu"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// TestQEMULifecycle is the Phase 2 destructive criterion end-to-end against a
// live node: create -> start -> snapshot -> rollback -> stop -> delete. It is
// gated on PVE_TEST_STORAGE and PVE_TEST_VMID (a VMID it is free to create and
// destroy) and skips otherwise, so it never touches an existing guest. The VM is
// deleted in cleanup even if a step fails.
func TestQEMULifecycle(t *testing.T) {
	c := newClient(t)
	node := testNode()

	storage := os.Getenv(envTestStorage)
	rawVMID := os.Getenv(envTestVMID)
	if storage == "" || rawVMID == "" {
		t.Skipf("destructive lifecycle disabled (set %s and %s)", envTestStorage, envTestVMID)
	}
	vmid, err := strconv.Atoi(rawVMID)
	if err != nil {
		t.Fatalf("%s=%q is not an integer: %v", envTestVMID, rawVMID, err)
	}

	q := c.QEMU(node)
	ts := c.Tasks()

	// Create.
	ref, err := q.Create(testCtx(t), &qemu.CreateSpec{
		VMID:    types.VMID(vmid),
		Name:    "sdk-itest",
		Memory:  512,
		Cores:   1,
		Net0:    "virtio,bridge=vmbr0",
		SCSI0:   storage + ":8",
		Storage: storage,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Always attempt teardown, even on later failure.
	t.Cleanup(func() {
		dref, derr := q.Delete(context.Background(), vmid)
		if derr != nil {
			t.Logf("cleanup Delete(%d): %v", vmid, derr)
			return
		}
		if _, werr := ts.Wait(context.Background(), dref); werr != nil {
			t.Logf("cleanup Wait(delete): %v", werr)
		}
	})
	mustSucceed(t, ts, ref, "create")

	// Start.
	ref, err = q.Start(testCtx(t), vmid)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	mustSucceed(t, ts, ref, "start")

	// Snapshot, then roll back to it.
	ref, err = q.CreateSnapshot(testCtx(t), vmid, &qemu.SnapshotSpec{Name: "itest0"})
	if err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}
	mustSucceed(t, ts, ref, "snapshot")

	ref, err = q.RollbackSnapshot(testCtx(t), vmid, "itest0")
	if err != nil {
		t.Fatalf("RollbackSnapshot: %v", err)
	}
	mustSucceed(t, ts, ref, "rollback")

	// Stop (delete happens in cleanup).
	ref, err = q.Stop(testCtx(t), vmid)
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	mustSucceed(t, ts, ref, "stop")
}

// mustSucceed waits for a task and fails the test unless it ends OK.
func mustSucceed(t *testing.T, ts *tasks.Service, ref tasks.Ref, step string) {
	t.Helper()
	st, err := ts.Wait(testCtx(t), ref)
	if err != nil {
		t.Fatalf("Wait(%s): %v", step, err)
	}
	if !st.OK() {
		t.Fatalf("%s task exited %q, want OK", step, st.ExitStatus)
	}
}
