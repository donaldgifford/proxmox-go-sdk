package ha_test

import (
	"context"
	"errors"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/ha"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/version"
)

func newService(t *testing.T, mock *mockpve.Server) *ha.Service {
	t.Helper()
	c, cleanup := mock.NewClient()
	t.Cleanup(cleanup)
	return ha.NewService(c, version.Capabilities{})
}

func newCappedService(t *testing.T, mock *mockpve.Server, ver string) *ha.Service {
	t.Helper()
	c, cleanup := mock.NewClient()
	t.Cleanup(cleanup)
	caps, err := version.Parse(ver)
	if err != nil {
		t.Fatalf("version.Parse(%q): %v", ver, err)
	}
	return ha.NewService(c, caps)
}

func TestListResources(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddHAResource("vm:100", "started")
	mock.AddHAResource("ct:101", "stopped")
	svc := newService(t, mock)

	res, err := svc.ListResources(context.Background())
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("ListResources returned %d, want 2", len(res))
	}
}

func TestGetResource(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddHAResource("vm:100", "started")
	svc := newService(t, mock)

	// The SID carries a colon; the path helper escapes it and PVE round-trips it.
	r, err := svc.GetResource(context.Background(), "vm:100")
	if err != nil {
		t.Fatalf("GetResource: %v", err)
	}
	if r.SID != "vm:100" || r.Type != "vm" || r.State != ha.StateStarted {
		t.Errorf("resource = %+v, want sid=vm:100 type=vm state=started", r)
	}
}

func TestGetResourceNotFound(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)

	if _, err := svc.GetResource(context.Background(), "vm:999"); !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("GetResource(ghost) = %v, want ErrNotFound", err)
	}
}

func TestAddResource(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	err := svc.AddResource(ctx, &ha.HAResourceSpec{
		SID:        "vm:100",
		State:      ha.StateStarted,
		MaxRestart: 3,
		Comment:    "web frontend",
	})
	if err != nil {
		t.Fatalf("AddResource: %v", err)
	}

	r, err := svc.GetResource(ctx, "vm:100")
	if err != nil {
		t.Fatalf("GetResource after add: %v", err)
	}
	if r.State != ha.StateStarted || r.MaxRestart != 3 {
		t.Errorf("added resource = %+v, want state=started max_restart=3", r)
	}
}

func TestAddResourceValidation(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.AddResource(ctx, nil); err == nil {
		t.Error("AddResource(nil) error = nil, want non-nil")
	}
	if err := svc.AddResource(ctx, &ha.HAResourceSpec{}); err == nil {
		t.Error("AddResource(no sid) error = nil, want non-nil")
	}
}

func TestUpdateResource(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddHAResource("vm:100", "started")
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.UpdateResource(ctx, "vm:100", &ha.HAResourceUpdate{State: ha.StateStopped}); err != nil {
		t.Fatalf("UpdateResource: %v", err)
	}

	r, err := svc.GetResource(ctx, "vm:100")
	if err != nil {
		t.Fatalf("GetResource after update: %v", err)
	}
	if r.State != ha.StateStopped {
		t.Errorf("state after update = %q, want stopped", r.State)
	}
}

func TestUpdateResourceValidation(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.UpdateResource(ctx, "vm:100", nil); err == nil {
		t.Error("UpdateResource(nil) error = nil, want non-nil")
	}
	if err := svc.UpdateResource(ctx, "", &ha.HAResourceUpdate{State: ha.StateStopped}); err == nil {
		t.Error("UpdateResource(no sid) error = nil, want non-nil")
	}
}

func TestRemoveResource(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddHAResource("vm:100", "started")
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.RemoveResource(ctx, "vm:100"); err != nil {
		t.Fatalf("RemoveResource: %v", err)
	}
	if _, err := svc.GetResource(ctx, "vm:100"); !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("GetResource after remove = %v, want ErrNotFound", err)
	}
}

func TestRemoveResourceNotFound(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)

	if err := svc.RemoveResource(context.Background(), "vm:999"); !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("RemoveResource(ghost) = %v, want ErrNotFound", err)
	}
}

func TestListRules(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddHARule("pin-web", string(ha.RuleTypeNodeAffinity))
	mock.AddHARule("collocate-db", string(ha.RuleTypeResourceAffinity))
	svc := newService(t, mock)

	rules, err := svc.ListRules(context.Background())
	if err != nil {
		t.Fatalf("ListRules: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("ListRules returned %d, want 2", len(rules))
	}
}

func TestCreateNodeAffinityRule(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	err := svc.CreateRule(ctx, &ha.HARuleSpec{
		Rule:  "pin-web",
		Type:  ha.RuleTypeNodeAffinity,
		Nodes: []string{"pve1", "pve2"},
	})
	if err != nil {
		t.Fatalf("CreateRule(node-affinity): %v", err)
	}

	r, err := svc.GetRule(ctx, "pin-web")
	if err != nil {
		t.Fatalf("GetRule: %v", err)
	}
	if r.Type != ha.RuleTypeNodeAffinity || r.Nodes != "pve1,pve2" {
		t.Errorf("rule = %+v, want type=node-affinity nodes=pve1,pve2", r)
	}
}

func TestCreateResourceAffinityRule(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	err := svc.CreateRule(ctx, &ha.HARuleSpec{
		Rule:      "collocate-web",
		Type:      ha.RuleTypeResourceAffinity,
		Resources: []string{"vm:100", "vm:101"},
		Affinity:  "positive",
	})
	if err != nil {
		t.Fatalf("CreateRule(resource-affinity): %v", err)
	}

	r, err := svc.GetRule(ctx, "collocate-web")
	if err != nil {
		t.Fatalf("GetRule: %v", err)
	}
	if r.Type != ha.RuleTypeResourceAffinity || r.Resources != "vm:100,vm:101" {
		t.Errorf("rule = %+v, want type=resource-affinity resources=vm:100,vm:101", r)
	}
	if r.Affinity != "positive" {
		t.Errorf("affinity = %q, want positive", r.Affinity)
	}
}

func TestCreateRuleValidation(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.CreateRule(ctx, nil); err == nil {
		t.Error("CreateRule(nil) error = nil, want non-nil")
	}
	if err := svc.CreateRule(ctx, &ha.HARuleSpec{Type: ha.RuleTypeNodeAffinity}); err == nil {
		t.Error("CreateRule(no name) error = nil, want non-nil")
	}
	if err := svc.CreateRule(ctx, &ha.HARuleSpec{Rule: "x"}); err == nil {
		t.Error("CreateRule(no type) error = nil, want non-nil")
	}
}

func TestUpdateRuleDisable(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddHARule("pin-web", string(ha.RuleTypeNodeAffinity))
	svc := newService(t, mock)
	ctx := context.Background()

	disabled := types.PVEBool(true)
	if err := svc.UpdateRule(ctx, "pin-web", &ha.HARuleUpdate{Disable: &disabled}); err != nil {
		t.Fatalf("UpdateRule: %v", err)
	}

	r, err := svc.GetRule(ctx, "pin-web")
	if err != nil {
		t.Fatalf("GetRule after disable: %v", err)
	}
	if !bool(r.Disable) {
		t.Errorf("disable after update = %v, want true", bool(r.Disable))
	}
}

func TestDeleteRule(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddHARule("pin-web", string(ha.RuleTypeNodeAffinity))
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.DeleteRule(ctx, "pin-web"); err != nil {
		t.Fatalf("DeleteRule: %v", err)
	}
	if _, err := svc.GetRule(ctx, "pin-web"); !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("GetRule after delete = %v, want ErrNotFound", err)
	}
}

func TestDeleteRuleNotFound(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)

	if err := svc.DeleteRule(context.Background(), "ghost"); !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("DeleteRule(ghost) = %v, want ErrNotFound", err)
	}
}
