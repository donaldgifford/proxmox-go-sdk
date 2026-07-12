package lab

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// seedOwnedVMs puts the three configured node VMs on the mock, harness-named.
func seedOwnedVMs(t *testing.T, mock interface {
	AddVM(node string, vmid int, name, status string)
}, cfg *Config, status string,
) {
	t.Helper()
	for _, n := range cfg.Nested.Nodes {
		mock.AddVM(cfg.Outer.Node, n.VMID, vmName(n), status)
	}
}

func TestTeardownDeletesOwnedVMs(t *testing.T) {
	c, mock := newMockClient(t)
	cfg := provisionTestConfig()
	seedOwnedVMs(t, mock, cfg, "running")
	ctx := context.Background()

	if err := Teardown(ctx, c, cfg, TeardownOptions{}, nil); err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	vms, err := c.QEMU("r740a").List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(vms) != 0 {
		t.Errorf("VMs remaining after teardown: %v", vms)
	}
}

func TestTeardownRefusesForeignName(t *testing.T) {
	c, mock := newMockClient(t)
	cfg := provisionTestConfig()
	// 9201 exists but is NOT harness-named; the others are ours.
	mock.AddVM("r740a", 9201, "precious-production-vm", "running")
	mock.AddVM("r740a", 9202, "pvelab-pve2-dogfood", "running")
	mock.AddVM("r740a", 9203, "pvelab-pve3-dogfood", "running")
	ctx := context.Background()

	err := Teardown(ctx, c, cfg, TeardownOptions{Force: true}, nil)
	if !errors.Is(err, ErrNotOurs) {
		t.Fatalf("Teardown = %v, want ErrNotOurs (Force must NOT bypass ownership)", err)
	}

	vms, err := c.QEMU("r740a").List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(vms) != 1 || vms[0].Name != "precious-production-vm" {
		t.Errorf("survivors = %v, want only the foreign VM (owned ones deleted)", vms)
	}
}

func TestTeardownRefusesOutOfRangeVMID(t *testing.T) {
	c, mock := newMockClient(t)
	cfg := provisionTestConfig()
	// A config that (somehow) names a VMID outside the reserved block — the
	// teardown-side guard must hold even if load-time validation is bypassed.
	cfg.Nested.Nodes = []Node{{Name: "rogue", VMID: 100, CIDR: "192.0.2.50/24"}}
	mock.AddVM("r740a", 100, "pvelab-rogue", "running") // even harness-named!
	ctx := context.Background()

	err := Teardown(ctx, c, cfg, TeardownOptions{Force: true}, nil)
	if !errors.Is(err, ErrNotOurs) || !strings.Contains(err.Error(), "outside the reserved pvelab block") {
		t.Fatalf("Teardown = %v, want out-of-range ErrNotOurs", err)
	}
	vms, _ := c.QEMU("r740a").List(ctx)
	if len(vms) != 1 {
		t.Errorf("vm 100 was touched: %v", vms)
	}
}

func TestTeardownMissingVMs(t *testing.T) {
	c, _ := newMockClient(t)
	cfg := provisionTestConfig()
	ctx := context.Background()

	// Nothing seeded: without Force every node errors...
	if err := Teardown(ctx, c, cfg, TeardownOptions{}, nil); err == nil {
		t.Error("Teardown on empty node without Force = nil, want error")
	}
	// ...with Force it is a clean no-op.
	if err := Teardown(ctx, c, cfg, TeardownOptions{Force: true}, nil); err != nil {
		t.Errorf("Teardown on empty node with Force = %v, want nil", err)
	}
}

func TestTeardownPurgeISOs(t *testing.T) {
	c, mock := newMockClient(t)
	cfg := provisionTestConfig()
	seedOwnedVMs(t, mock, cfg, "stopped")
	volid := PreparedISOVolid("local", "9.2")
	mock.AddVolume("r740a", "local", volid, "iso", "iso", 1<<30)
	ctx := context.Background()

	if err := Teardown(ctx, c, cfg, TeardownOptions{PurgeISOs: true}, nil); err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	present, err := isoPresent(ctx, c, cfg, volid)
	if err != nil {
		t.Fatalf("isoPresent: %v", err)
	}
	if present {
		t.Error("prepared ISO still present after -purge-isos")
	}
}

func TestTeardownPurgeISOsMissing(t *testing.T) {
	c, mock := newMockClient(t)
	cfg := provisionTestConfig()
	seedOwnedVMs(t, mock, cfg, "stopped")
	ctx := context.Background()

	// No ISO seeded: purge errors without Force, no-ops with it.
	if err := Teardown(ctx, c, cfg, TeardownOptions{PurgeISOs: true}, nil); err == nil {
		t.Error("purge of missing ISO without Force = nil, want error")
	}

	seedOwnedVMs(t, mock, cfg, "stopped") // the first run deleted them.
	if err := Teardown(ctx, c, cfg, TeardownOptions{PurgeISOs: true, Force: true}, nil); err != nil {
		t.Errorf("purge of missing ISO with Force = %v, want nil", err)
	}
}
