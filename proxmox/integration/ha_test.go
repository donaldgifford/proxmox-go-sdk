//go:build integration

package integration

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/ha"
)

// TestResourceAffinityRule covers the testable half of the Phase 4 criterion:
// defining a resource-affinity rule via the SDK and reading it back. It is gated
// on PVE_TEST_HA_SIDS (a CSV of >=2 HA-managed SIDs) and skips otherwise; the
// rule is deleted in cleanup. Observing the scheduler honour the placement is
// live-only and not asserted here.
func TestResourceAffinityRule(t *testing.T) {
	c := newClient(t)

	raw := os.Getenv(envTestHASIDs)
	if raw == "" {
		t.Skipf("resource-affinity rule disabled (set %s to a CSV of >=2 HA SIDs)", envTestHASIDs)
	}
	sids := strings.Split(raw, ",")
	if len(sids) < 2 {
		t.Fatalf("%s needs >=2 SIDs, got %q", envTestHASIDs, raw)
	}

	h := c.HA()
	const rule = "sdk-itest-affinity"

	t.Cleanup(func() {
		if derr := h.DeleteRule(context.Background(), rule); derr != nil {
			t.Logf("cleanup DeleteRule(%s): %v", rule, derr)
		}
	})

	if err := h.CreateRule(testCtx(t), &ha.HARuleSpec{
		Rule:      rule,
		Type:      ha.RuleTypeResourceAffinity,
		Resources: sids,
		Affinity:  "negative", // keep the resources on separate nodes.
		Comment:   "created by the SDK integration suite",
	}); err != nil {
		t.Fatalf("CreateRule: %v", err)
	}

	got, err := h.GetRule(testCtx(t), rule)
	if err != nil {
		t.Fatalf("GetRule(%s): %v", rule, err)
	}
	if got.Type != ha.RuleTypeResourceAffinity {
		t.Errorf("rule type = %q, want resource-affinity", got.Type)
	}
	if got.Affinity != "negative" {
		t.Errorf("rule affinity = %q, want negative", got.Affinity)
	}
}
