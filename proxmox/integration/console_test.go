//go:build integration

package integration

import (
	"os"
	"strconv"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/console"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/qemu"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// TestAccessReads covers the Phase 6 access criterion: listing users under the
// 9.x privilege model, and the tokens owned by root@pam.
func TestAccessReads(t *testing.T) {
	c := newClient(t)
	ctx := testCtx(t)

	users, err := c.Access().ListUsers(ctx)
	if err != nil {
		t.Fatalf("Access().ListUsers: %v", err)
	}
	if len(users) == 0 {
		t.Error("ListUsers returned none; root@pam always exists on a live node")
	}
	if _, err := c.Access().ListTokens(ctx, "root@pam"); err != nil {
		t.Fatalf("Access().ListTokens(root@pam): %v", err)
	}
}

// TestConsoleMint covers the other half of the Phase 6 criterion: minting a VNC
// console ticket for a VM. It is self-contained — it spins up its own scratch VM
// (create + start), mints against it, then tears it down (stop + delete) in
// cleanup — so it does not depend on a pre-existing guest and never touches one.
// It is gated on PVE_TEST_STORAGE and PVE_TEST_CONSOLE_VMID and skips otherwise.
// The VMID is deliberately its own gate (not the shared PVE_TEST_VMID) so this
// test can run in the same invocation as TestQEMULifecycle without both trying to
// create the same VMID. Minting itself is non-destructive — it does not dial or
// alter the running guest.
func TestConsoleMint(t *testing.T) {
	c := newClient(t)
	node := testNode()

	storage := os.Getenv(envTestStorage)
	raw := os.Getenv(envTestConsoleVMID)
	if storage == "" || raw == "" {
		t.Skipf("console mint disabled (set %s and %s)", envTestStorage, envTestConsoleVMID)
	}
	vmid, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatalf("%s=%q is not an integer: %v", envTestConsoleVMID, raw, err)
	}

	q := c.QEMU(node)
	ts := c.Tasks()

	// Spin up a scratch VM so the VMID exists for the mint.
	ref, err := q.Create(testCtx(t), &qemu.CreateSpec{
		VMID:    types.VMID(vmid),
		Name:    "sdk-itest-console",
		Memory:  512,
		Cores:   1,
		Net0:    "virtio,bridge=vmbr0",
		SCSI0:   storage + ":8",
		Storage: storage,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Tear down even if a later step fails: a running VM cannot be destroyed, so
	// stop first (best-effort — it may already be stopped), then delete.
	t.Cleanup(func() {
		ctx, cancel := cleanupCtx()
		defer cancel()
		if sref, serr := q.Stop(ctx, vmid); serr != nil {
			t.Logf("cleanup Stop(%d): %v", vmid, serr)
		} else if _, werr := ts.Wait(ctx, sref); werr != nil {
			t.Logf("cleanup Wait(stop): %v", werr)
		}
		dref, derr := q.Delete(ctx, vmid)
		if derr != nil {
			t.Logf("cleanup Delete(%d): %v", vmid, derr)
			return
		}
		if _, werr := ts.Wait(ctx, dref); werr != nil {
			t.Logf("cleanup Wait(delete): %v", werr)
		}
	})
	mustSucceed(t, ts, ref, "create")

	// Start it so the mint is against a running guest.
	ref, err = q.Start(testCtx(t), vmid)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	mustSucceed(t, ts, ref, "start")

	ticket, err := c.Console().MintVNCTicket(testCtx(t), node, console.KindQEMU, types.VMID(vmid))
	if err != nil {
		t.Fatalf("Console().MintVNCTicket(vmid=%d): %v", vmid, err)
	}
	if ticket.Ticket == "" || ticket.Port == "" {
		t.Errorf("minted ticket = %+v, want ticket and port set", ticket)
	}
}
