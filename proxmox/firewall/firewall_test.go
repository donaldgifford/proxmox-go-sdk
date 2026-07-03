package firewall_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/firewall"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/version"
)

const testNode = "pve"

// serviceForScope builds a firewall Service for one of the three scopes, with
// caps pinned to ver. All scopes share the same surface, so the tests run the
// same CRUD against each to prove scope.path routes correctly.
func serviceForScope(t *testing.T, mock *mockpve.Server, scope, ver string) *firewall.Service {
	t.Helper()
	caps, err := version.Parse(ver)
	if err != nil {
		t.Fatalf("version.Parse(%q): %v", ver, err)
	}
	c, cleanup := mock.NewClient()
	t.Cleanup(cleanup)
	switch scope {
	case "node":
		return firewall.NewNodeScope(c, caps, testNode)
	case "guest":
		return firewall.NewGuestScope(c, caps, firewall.GuestQEMU, testNode, 100)
	default:
		return firewall.NewClusterScope(c, caps)
	}
}

var scopes = []string{"cluster", "node", "guest"}

func TestRuleCRUDPerScope(t *testing.T) {
	t.Parallel()
	for _, scope := range scopes {
		t.Run(scope, func(t *testing.T) {
			t.Parallel()
			mock := mockpve.New()
			svc := serviceForScope(t, mock, scope, "9.1")
			ctx := context.Background()

			if err := svc.CreateRule(ctx, &firewall.RuleSpec{
				Type:   firewall.RuleIn,
				Action: "ACCEPT",
				Proto:  "tcp",
				Dport:  "22",
			}); err != nil {
				t.Fatalf("CreateRule: %v", err)
			}

			rules, err := svc.ListRules(ctx)
			if err != nil {
				t.Fatalf("ListRules: %v", err)
			}
			if len(rules) != 1 {
				t.Fatalf("ListRules returned %d, want 1", len(rules))
			}

			r, err := svc.GetRule(ctx, 0)
			if err != nil {
				t.Fatalf("GetRule: %v", err)
			}
			if r.Type != firewall.RuleIn || r.Action != "ACCEPT" || r.Dport != "22" {
				t.Errorf("rule = %+v, want in/ACCEPT/dport 22", r)
			}

			if err := svc.UpdateRule(ctx, 0, &firewall.RuleUpdate{Comment: "ssh"}); err != nil {
				t.Fatalf("UpdateRule: %v", err)
			}
			r, err = svc.GetRule(ctx, 0)
			if err != nil {
				t.Fatalf("GetRule after update: %v", err)
			}
			if r.Comment != "ssh" {
				t.Errorf("comment after update = %q, want ssh", r.Comment)
			}

			if err := svc.DeleteRule(ctx, 0); err != nil {
				t.Fatalf("DeleteRule: %v", err)
			}
			rules, err = svc.ListRules(ctx)
			if err != nil {
				t.Fatalf("ListRules after delete: %v", err)
			}
			if len(rules) != 0 {
				t.Fatalf("ListRules after delete returned %d, want 0", len(rules))
			}
		})
	}
}

func TestGetRuleNotFound(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := serviceForScope(t, mock, "cluster", "9.1")

	if _, err := svc.GetRule(context.Background(), 5); !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("GetRule(5) = %v, want ErrNotFound", err)
	}
}

func TestCreateRuleValidation(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := serviceForScope(t, mock, "cluster", "9.1")
	ctx := context.Background()

	if err := svc.CreateRule(ctx, nil); err == nil {
		t.Error("CreateRule(nil) error = nil, want non-nil")
	}
	if err := svc.CreateRule(ctx, &firewall.RuleSpec{Action: "ACCEPT"}); err == nil {
		t.Error("CreateRule(no type) error = nil, want non-nil")
	}
	if err := svc.CreateRule(ctx, &firewall.RuleSpec{Type: firewall.RuleIn}); err == nil {
		t.Error("CreateRule(no action) error = nil, want non-nil")
	}
}

func TestIPSetCRUDPerScope(t *testing.T) {
	t.Parallel()
	for _, scope := range scopes {
		t.Run(scope, func(t *testing.T) {
			t.Parallel()
			mock := mockpve.New()
			svc := serviceForScope(t, mock, scope, "9.1")
			ctx := context.Background()

			if err := svc.CreateIPSet(ctx, &firewall.IPSetSpec{Name: "trusted", Comment: "office"}); err != nil {
				t.Fatalf("CreateIPSet: %v", err)
			}
			sets, err := svc.ListIPSets(ctx)
			if err != nil {
				t.Fatalf("ListIPSets: %v", err)
			}
			if len(sets) != 1 || sets[0].Name != "trusted" {
				t.Fatalf("ListIPSets = %+v, want one named trusted", sets)
			}

			if err := svc.AddIPSetEntry(ctx, "trusted", &firewall.IPSetEntrySpec{
				CIDR:    "10.0.0.0/24",
				Comment: "lan",
			}); err != nil {
				t.Fatalf("AddIPSetEntry: %v", err)
			}
			entries, err := svc.ListIPSetEntries(ctx, "trusted")
			if err != nil {
				t.Fatalf("ListIPSetEntries: %v", err)
			}
			if len(entries) != 1 || entries[0].CIDR != "10.0.0.0/24" {
				t.Fatalf("entries = %+v, want one 10.0.0.0/24", entries)
			}

			if err := svc.DeleteIPSetEntry(ctx, "trusted", "10.0.0.0/24"); err != nil {
				t.Fatalf("DeleteIPSetEntry: %v", err)
			}
			if err := svc.DeleteIPSet(ctx, "trusted"); err != nil {
				t.Fatalf("DeleteIPSet: %v", err)
			}
		})
	}
}

// TestRenameIPSetGate covers the OverlappingIPSets gate: rename requires 9.1.
func TestRenameIPSetGate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mock90 := mockpve.New()
	svc90 := serviceForScope(t, mock90, "cluster", "9.0") // below the 9.1 gate.
	if err := svc90.RenameIPSet(ctx, "old", "new"); !errors.Is(err, pverr.ErrUnsupported) {
		t.Fatalf("RenameIPSet on 9.0 = %v, want ErrUnsupported", err)
	}

	mock91 := mockpve.New()
	svc91 := serviceForScope(t, mock91, "cluster", "9.1") // gate satisfied.
	if err := svc91.CreateIPSet(ctx, &firewall.IPSetSpec{Name: "old"}); err != nil {
		t.Fatalf("CreateIPSet: %v", err)
	}
	if err := svc91.RenameIPSet(ctx, "old", "new"); err != nil {
		t.Fatalf("RenameIPSet on 9.1 = %v, want nil", err)
	}
	sets, err := svc91.ListIPSets(ctx)
	if err != nil {
		t.Fatalf("ListIPSets after rename: %v", err)
	}
	if len(sets) != 1 || sets[0].Name != "new" {
		t.Fatalf("ListIPSets after rename = %+v, want one named new", sets)
	}
}

func TestOptions(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := serviceForScope(t, mock, "cluster", "9.1")
	ctx := context.Background()

	enable := types.PVEBool(true)
	if err := svc.SetOptions(ctx, &firewall.OptionsUpdate{
		Enable:   &enable,
		PolicyIn: "DROP",
	}); err != nil {
		t.Fatalf("SetOptions: %v", err)
	}
	o, err := svc.GetOptions(ctx)
	if err != nil {
		t.Fatalf("GetOptions: %v", err)
	}
	if !bool(o.Enable) || o.PolicyIn != "DROP" {
		t.Errorf("options = %+v, want enable=true policy_in=DROP", o)
	}
}

// TestGuestLXCScope confirms the LXC guest kind routes to /lxc/ (not /qemu/).
func TestGuestLXCScope(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	caps, err := version.Parse("9.1")
	if err != nil {
		t.Fatalf("version.Parse: %v", err)
	}
	c, cleanup := mock.NewClient()
	t.Cleanup(cleanup)
	svc := firewall.NewGuestScope(c, caps, firewall.GuestLXC, testNode, 200)
	ctx := context.Background()

	if err := svc.CreateRule(ctx, &firewall.RuleSpec{Type: firewall.RuleIn, Action: "ACCEPT"}); err != nil {
		t.Fatalf("CreateRule (lxc guest): %v", err)
	}
	rules, err := svc.ListRules(ctx)
	if err != nil {
		t.Fatalf("ListRules (lxc guest): %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("ListRules (lxc guest) returned %d, want 1", len(rules))
	}
}

func TestRuleUnmarshalExtra(t *testing.T) {
	t.Parallel()
	// A key outside the modelled set ("icmp-type") must land in Extra.
	const blob = `{"pos":3,"type":"in","action":"ACCEPT","enable":1,"icmp-type":"echo-request"}`
	var r firewall.Rule
	if err := json.Unmarshal([]byte(blob), &r); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if r.Pos != 3 || r.Type != firewall.RuleIn || !bool(r.Enable) {
		t.Errorf("modelled fields = %+v, want pos=3 type=in enable=true", r)
	}
	if r.Extra["icmp-type"] != "echo-request" {
		t.Errorf("Extra = %v, want icmp-type=echo-request", r.Extra)
	}
}
