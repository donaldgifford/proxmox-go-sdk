//go:build integration

package integration

import (
	"os"
	"strconv"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/console"
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
// console ticket for a VM. It needs a real VMID (PVE_TEST_VMID) and skips
// otherwise. Minting is non-destructive — it does not dial or alter the guest.
func TestConsoleMint(t *testing.T) {
	c := newClient(t)
	ctx := testCtx(t)

	raw := os.Getenv(envTestVMID)
	if raw == "" {
		t.Skipf("no scratch VM configured (set %s to an existing VMID)", envTestVMID)
	}
	vmid, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatalf("%s=%q is not an integer: %v", envTestVMID, raw, err)
	}

	ticket, err := c.Console().MintVNCTicket(ctx, testNode(), console.KindQEMU, types.VMID(vmid))
	if err != nil {
		t.Fatalf("Console().MintVNCTicket(vmid=%d): %v", vmid, err)
	}
	if ticket.Ticket == "" || ticket.Port == "" {
		t.Errorf("minted ticket = %+v, want ticket and port set", ticket)
	}
}
