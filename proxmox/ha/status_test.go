package ha_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/ha"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
)

// fencingEntry returns the fencing row of a status read — the row that
// carries armed-state on live 9.2.2 — failing the test when it is absent.
func fencingEntry(t *testing.T, entries []ha.HAStatusEntry) ha.HAStatusEntry {
	t.Helper()
	for i := range entries {
		if entries[i].Type == "fencing" {
			return entries[i]
		}
	}
	t.Fatal("status current: no fencing row")
	return ha.HAStatusEntry{}
}

// serviceEntry returns the service row for sid, failing the test when absent.
func serviceEntry(t *testing.T, entries []ha.HAStatusEntry, sid string) ha.HAStatusEntry {
	t.Helper()
	for i := range entries {
		if entries[i].Type == "service" && entries[i].SID == sid {
			return entries[i]
		}
	}
	t.Fatalf("status current: no service row for %s", sid)
	return ha.HAStatusEntry{}
}

// The arm -> disarm -> arm cycle is observable end-to-end through the mock's
// /status/current: the fencing row's armed-state transitions, and the disarm
// resource-mode is mirrored on the service rows while disarmed.
func TestArmDisarmCycleObservable(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddHAResource("vm:100", "started")
	svc := newCappedService(t, mock, "9.2")
	ctx := context.Background()

	entries, err := svc.HAStatusCurrent(ctx)
	if err != nil {
		t.Fatalf("HAStatusCurrent: %v", err)
	}
	if got := fencingEntry(t, entries).ArmedState; got != ha.ArmedStateArmed {
		t.Fatalf("baseline armed-state = %q, want %q", got, ha.ArmedStateArmed)
	}

	if err := svc.DisarmHA(ctx, ha.ResourceModeFreeze); err != nil {
		t.Fatalf("DisarmHA: %v", err)
	}
	entries, err = svc.HAStatusCurrent(ctx)
	if err != nil {
		t.Fatalf("HAStatusCurrent after disarm: %v", err)
	}
	if got := fencingEntry(t, entries).ArmedState; got != ha.ArmedStateDisarmed {
		t.Errorf("disarmed armed-state = %q, want %q", got, ha.ArmedStateDisarmed)
	}
	if got := serviceEntry(t, entries, "vm:100").ResourceMode; got != ha.ResourceModeFreeze {
		t.Errorf("disarmed resource_mode = %q, want %q", got, ha.ResourceModeFreeze)
	}

	if err := svc.ArmHA(ctx); err != nil {
		t.Fatalf("ArmHA: %v", err)
	}
	entries, err = svc.HAStatusCurrent(ctx)
	if err != nil {
		t.Fatalf("HAStatusCurrent after re-arm: %v", err)
	}
	if got := fencingEntry(t, entries).ArmedState; got != ha.ArmedStateArmed {
		t.Errorf("re-armed armed-state = %q, want %q", got, ha.ArmedStateArmed)
	}
	if got := serviceEntry(t, entries, "vm:100").ResourceMode; got != "" {
		t.Errorf("re-armed resource_mode = %q, want empty", got)
	}
}

// The disarm wire form carries resource-mode; the ignore mode is mirrored
// back by the status read.
func TestDisarmResourceModeWire(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddHAResource("ct:101", "started")
	svc := newCappedService(t, mock, "9.2")
	ctx := context.Background()

	if err := svc.DisarmHA(ctx, ha.ResourceModeIgnore); err != nil {
		t.Fatalf("DisarmHA(ignore): %v", err)
	}
	entries, err := svc.HAStatusCurrent(ctx)
	if err != nil {
		t.Fatalf("HAStatusCurrent: %v", err)
	}
	if got := serviceEntry(t, entries, "ct:101").ResourceMode; got != ha.ResourceModeIgnore {
		t.Errorf("resource_mode = %q, want %q", got, ha.ResourceModeIgnore)
	}
}

// GetManagerStatus decodes the live-confirmed nested envelope: the CRM state
// blob under manager_status and the quorum summary alongside.
func TestManagerStatusRead(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddHAResource("vm:100", "started")
	svc := newCappedService(t, mock, "9.2")

	ms, err := svc.GetManagerStatus(context.Background())
	if err != nil {
		t.Fatalf("GetManagerStatus: %v", err)
	}
	if ms.Manager.MasterNode == "" {
		t.Error("Manager.MasterNode empty, want the mock node")
	}
	if got := ms.Manager.NodeStatus[ms.Manager.MasterNode]; got != "online" {
		t.Errorf("NodeStatus[%s] = %q, want online", ms.Manager.MasterNode, got)
	}
	entry, found := ms.Manager.ServiceStatus["vm:100"]
	if !found {
		t.Fatal("ServiceStatus missing vm:100")
	}
	if entry.State != "started" {
		t.Errorf("service state = %q, want started", entry.State)
	}
	if ms.Manager.Timestamp == 0 {
		t.Error("Manager.Timestamp = 0, want set")
	}
	if !bool(ms.Quorum.Quorate) {
		t.Error("Quorum.Quorate = false, want true (string \"1\" on the wire)")
	}
	if ms.Quorum.Node == "" {
		t.Error("Quorum.Node empty, want the answering node")
	}
}

// HAStatusEntry models the 16 apidoc-confirmed fields plus the live-observed
// comment, and routes unknown keys into Extra (hyphenated wire keys included).
func TestHAStatusEntryLossless(t *testing.T) {
	t.Parallel()
	raw := `{
		"id": "service:vm:100", "sid": "vm:100", "node": "pve2",
		"type": "service", "state": "started", "status": "running",
		"crm_state": "started", "request_state": "started", "quorate": 1,
		"armed-state": "standby", "auto-rebalance": 1, "failback": 0,
		"max_relocate": 2, "max_restart": 3, "resource_mode": "freeze",
		"timestamp": 1752000000, "comment": "dogfood", "future-key": 42
	}`
	var e ha.HAStatusEntry
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if e.SID != "vm:100" || e.Node != "pve2" || e.CRMState != "started" ||
		e.RequestState != "started" || !bool(e.Quorate) ||
		e.ArmedState != ha.ArmedStateStandby || !bool(e.AutoRebalance) ||
		bool(e.Failback) || e.MaxRelocate != 2 || e.MaxRestart != 3 ||
		e.ResourceMode != ha.ResourceModeFreeze || e.Timestamp != 1752000000 ||
		e.Comment != "dogfood" {
		t.Errorf("typed fields mis-decoded: %+v", e)
	}
	if e.Extra["future-key"] != "42" {
		t.Errorf("Extra[future-key] = %q, want 42", e.Extra["future-key"])
	}
}

// ManagerStatus decodes the nested live envelope and keeps unmodelled keys —
// at every level — in the respective Extra. The raw body mirrors the
// 2026-07-23 cassette shape (quorate as string "1") plus injected unknowns.
func TestManagerStatusLossless(t *testing.T) {
	t.Parallel()
	raw := `{
		"manager_status": {
			"master_node": "pve", "timestamp": 1752000000,
			"node_status": {"pve": "online"},
			"service_status": {"vm:100": {"node": "pve", "state": "started", "uid": "u1", "flags": "x"}},
			"queue": {"depth": 0}
		},
		"quorum": {"node": "pve", "quorate": "1", "epoch": 3},
		"future-top": true
	}`
	var ms ha.ManagerStatus
	if err := json.Unmarshal([]byte(raw), &ms); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if ms.Manager.MasterNode != "pve" || ms.Manager.Timestamp != 1752000000 {
		t.Errorf("manager typed fields mis-decoded: %+v", ms.Manager)
	}
	if ms.Manager.ServiceStatus["vm:100"].UID != "u1" {
		t.Errorf("service uid = %q, want u1", ms.Manager.ServiceStatus["vm:100"].UID)
	}
	if ms.Manager.ServiceStatus["vm:100"].Extra["flags"] != "x" {
		t.Errorf("service Extra[flags] = %q, want x", ms.Manager.ServiceStatus["vm:100"].Extra["flags"])
	}
	if _, found := ms.Manager.Extra["queue"]; !found {
		t.Error("Manager.Extra missing unmodelled queue key")
	}
	if !bool(ms.Quorum.Quorate) || ms.Quorum.Node != "pve" {
		t.Errorf("quorum mis-decoded: %+v", ms.Quorum)
	}
	if ms.Quorum.Extra["epoch"] != "3" {
		t.Errorf("Quorum.Extra[epoch] = %q, want 3", ms.Quorum.Extra["epoch"])
	}
	if _, found := ms.Extra["future-top"]; !found {
		t.Error("Extra missing unmodelled future-top key")
	}
}
