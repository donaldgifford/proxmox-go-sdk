package ha_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/ha"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
)

// masterEntry returns the master row of a status read, failing the test when
// it is absent.
func masterEntry(t *testing.T, entries []ha.HAStatusEntry) ha.HAStatusEntry {
	t.Helper()
	for i := range entries {
		if entries[i].Type == "master" {
			return entries[i]
		}
	}
	t.Fatal("status current: no master row")
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
// /status/current: the master row's armed-state transitions, and the disarm
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
	if got := masterEntry(t, entries).ArmedState; got != ha.ArmedStateArmed {
		t.Fatalf("baseline armed-state = %q, want %q", got, ha.ArmedStateArmed)
	}

	if err := svc.DisarmHA(ctx, ha.ResourceModeFreeze); err != nil {
		t.Fatalf("DisarmHA: %v", err)
	}
	entries, err = svc.HAStatusCurrent(ctx)
	if err != nil {
		t.Fatalf("HAStatusCurrent after disarm: %v", err)
	}
	if got := masterEntry(t, entries).ArmedState; got != ha.ArmedStateDisarmed {
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
	if got := masterEntry(t, entries).ArmedState; got != ha.ArmedStateArmed {
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

// GetManagerStatus decodes the mock's CRM blob into the provisional typed
// fields.
func TestManagerStatusRead(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddHAResource("vm:100", "started")
	svc := newCappedService(t, mock, "9.2")

	ms, err := svc.GetManagerStatus(context.Background())
	if err != nil {
		t.Fatalf("GetManagerStatus: %v", err)
	}
	if ms.MasterNode == "" {
		t.Error("MasterNode empty, want the mock node")
	}
	if ms.NodeStatus[ms.MasterNode] != "online" {
		t.Errorf("NodeStatus[%s] = %q, want online", ms.MasterNode, ms.NodeStatus[ms.MasterNode])
	}
	entry, found := ms.ServiceStatus["vm:100"]
	if !found {
		t.Fatal("ServiceStatus missing vm:100")
	}
	if entry.State != "started" {
		t.Errorf("service state = %q, want started", entry.State)
	}
	if ms.Timestamp == 0 {
		t.Error("Timestamp = 0, want set")
	}
}

// HAStatusEntry models all 16 apidoc-confirmed fields and routes unknown keys
// into Extra (hyphenated wire keys included).
func TestHAStatusEntryLossless(t *testing.T) {
	t.Parallel()
	raw := `{
		"id": "service:vm:100", "sid": "vm:100", "node": "pve2",
		"type": "service", "state": "started", "status": "running",
		"crm_state": "started", "request_state": "started", "quorate": 1,
		"armed-state": "standby", "auto-rebalance": 1, "failback": 0,
		"max_relocate": 2, "max_restart": 3, "resource_mode": "freeze",
		"timestamp": 1752000000, "future-key": 42
	}`
	var e ha.HAStatusEntry
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if e.SID != "vm:100" || e.Node != "pve2" || e.CRMState != "started" ||
		e.RequestState != "started" || !bool(e.Quorate) ||
		e.ArmedState != ha.ArmedStateStandby || !bool(e.AutoRebalance) ||
		bool(e.Failback) || e.MaxRelocate != 2 || e.MaxRestart != 3 ||
		e.ResourceMode != ha.ResourceModeFreeze || e.Timestamp != 1752000000 {
		t.Errorf("typed fields mis-decoded: %+v", e)
	}
	if e.Extra["future-key"] != "42" {
		t.Errorf("Extra[future-key] = %q, want 42", e.Extra["future-key"])
	}
}

// ManagerStatus keeps unmodelled keys — including nested objects — in Extra.
func TestManagerStatusLossless(t *testing.T) {
	t.Parallel()
	raw := `{
		"master_node": "pve", "timestamp": 1752000000,
		"node_status": {"pve": "online"},
		"service_status": {"vm:100": {"node": "pve", "state": "started", "uid": "u1", "flags": "x"}},
		"queue": {"depth": 0}
	}`
	var ms ha.ManagerStatus
	if err := json.Unmarshal([]byte(raw), &ms); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if ms.MasterNode != "pve" || ms.Timestamp != 1752000000 {
		t.Errorf("typed fields mis-decoded: %+v", ms)
	}
	if ms.ServiceStatus["vm:100"].UID != "u1" {
		t.Errorf("service uid = %q, want u1", ms.ServiceStatus["vm:100"].UID)
	}
	if ms.ServiceStatus["vm:100"].Extra["flags"] != "x" {
		t.Errorf("service Extra[flags] = %q, want x", ms.ServiceStatus["vm:100"].Extra["flags"])
	}
	if _, found := ms.Extra["queue"]; !found {
		t.Error("Extra missing unmodelled queue key")
	}
}
