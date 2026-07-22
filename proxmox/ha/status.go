package ha

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// ArmedState is the cluster-wide HA arm switch position reported by the
// armed-state field of HAStatusEntry (9.2+) — the observable for
// ArmHA/DisarmHA transitions.
type ArmedState string

const (
	// ArmedStateArmed — HA is active; the CRM applies state changes.
	ArmedStateArmed ArmedState = "armed"
	// ArmedStateStandby — the reporting CRM is idle (no manager lock).
	ArmedStateStandby ArmedState = "standby"
	// ArmedStateDisarming — a disarm was requested and is settling.
	ArmedStateDisarming ArmedState = "disarming"
	// ArmedStateDisarmed — HA is disarmed; resources follow the ResourceMode
	// chosen at disarm time.
	ArmedStateDisarmed ArmedState = "disarmed"
)

// HAStatusEntry is one row of the HA manager status read
// (GET /cluster/ha/status/current). The endpoint returns a heterogeneous
// array — quorum, master, per-node lrm, and per-resource service rows share
// this shape with most fields optional; Type discriminates. Reads are
// lossless: keys outside the typed set are preserved in Extra.
type HAStatusEntry struct {
	ID           string `json:"id,omitempty"`
	SID          string `json:"sid,omitempty"`  // service rows: the resource SID, e.g. "vm:100".
	Node         string `json:"node,omitempty"` // the node the row reports on/for.
	Type         string `json:"type,omitempty"` // "quorum" | "master" | "lrm" | "service".
	State        string `json:"state,omitempty"`
	Status       string `json:"status,omitempty"`
	CRMState     string `json:"crm_state,omitempty"`
	RequestState string `json:"request_state,omitempty"`
	// Quorate is set on the quorum row; PVE encodes it 0/1.
	Quorate types.PVEBool `json:"quorate,omitempty"`
	// ArmedState is the 9.2 cluster-wide arm switch position.
	ArmedState    ArmedState    `json:"armed-state,omitempty"`
	AutoRebalance types.PVEBool `json:"auto-rebalance,omitempty"`
	Failback      types.PVEBool `json:"failback,omitempty"`
	MaxRelocate   int           `json:"max_relocate,omitempty"`
	MaxRestart    int           `json:"max_restart,omitempty"`
	// ResourceMode mirrors the mode chosen at disarm time (see DisarmHA).
	ResourceMode ResourceMode `json:"resource_mode,omitempty"`
	Timestamp    int64        `json:"timestamp,omitempty"` // unix seconds.
	// Extra carries fields the SDK does not model.
	Extra map[string]string `json:"-"`
}

// haStatusEntryKnownFields lists the JSON keys HAStatusEntry models directly;
// keep it in sync with the struct so UnmarshalJSON routes only the rest into
// Extra.
var haStatusEntryKnownFields = map[string]bool{
	"id": true, "sid": true, "node": true, "type": true, "state": true,
	"status": true, "crm_state": true, "request_state": true, "quorate": true,
	"armed-state": true, "auto-rebalance": true, "failback": true,
	"max_relocate": true, "max_restart": true, "resource_mode": true,
	"timestamp": true,
}

// UnmarshalJSON decodes the modelled fields and routes any unknown keys into
// Extra so the read round-trips losslessly.
func (e *HAStatusEntry) UnmarshalJSON(data []byte) error {
	type alias HAStatusEntry
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode ha status entry: %w", err)
	}
	*e = HAStatusEntry(a)
	extra, err := svcutil.DecodeExtra(data, haStatusEntryKnownFields)
	if err != nil {
		return fmt.Errorf("decode ha status entry: %w", err)
	}
	e.Extra = extra
	return nil
}

// ManagerServiceStatus is one resource's entry in the manager's service_status
// map. Lossless: unknown keys are preserved in Extra.
type ManagerServiceStatus struct {
	Node  string `json:"node,omitempty"`
	State string `json:"state,omitempty"`
	UID   string `json:"uid,omitempty"`
	// Extra carries fields the SDK does not model.
	Extra map[string]string `json:"-"`
}

// managerServiceKnownFields lists the JSON keys ManagerServiceStatus models
// directly; keep it in sync with the struct.
var managerServiceKnownFields = map[string]bool{"node": true, "state": true, "uid": true}

// UnmarshalJSON decodes the modelled fields and routes any unknown keys into
// Extra so the read round-trips losslessly.
func (m *ManagerServiceStatus) UnmarshalJSON(data []byte) error {
	type alias ManagerServiceStatus
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode manager service status: %w", err)
	}
	*m = ManagerServiceStatus(a)
	extra, err := svcutil.DecodeExtra(data, managerServiceKnownFields)
	if err != nil {
		return fmt.Errorf("decode manager service status: %w", err)
	}
	m.Extra = extra
	return nil
}

// ManagerStatus is the CRM master's internal state
// (GET /cluster/ha/status/manager_status). The PVE apidoc pins no shape for
// this endpoint (a bare object), so the typed fields are provisional — they
// mirror the pve-ha-manager state file as observed — and everything else
// round-trips losslessly through Extra. Treat the typed fields as best-effort
// until confirmed live (IMPL-0005 Phase 3).
type ManagerStatus struct {
	MasterNode string `json:"master_node,omitempty"`
	// NodeStatus maps node name to its CRM node state (e.g. "online").
	NodeStatus map[string]string `json:"node_status,omitempty"`
	// ServiceStatus maps resource SID to its manager-side state.
	ServiceStatus map[string]ManagerServiceStatus `json:"service_status,omitempty"`
	Timestamp     int64                           `json:"timestamp,omitempty"` // unix seconds.
	// Extra carries fields the SDK does not model.
	Extra map[string]string `json:"-"`
}

// managerStatusKnownFields lists the JSON keys ManagerStatus models directly;
// keep it in sync with the struct.
var managerStatusKnownFields = map[string]bool{
	"master_node": true, "node_status": true, "service_status": true, "timestamp": true,
}

// UnmarshalJSON decodes the modelled fields and routes any unknown keys into
// Extra so the read round-trips losslessly.
func (m *ManagerStatus) UnmarshalJSON(data []byte) error {
	type alias ManagerStatus
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode manager status: %w", err)
	}
	*m = ManagerStatus(a)
	extra, err := svcutil.DecodeExtra(data, managerStatusKnownFields)
	if err != nil {
		return fmt.Errorf("decode manager status: %w", err)
	}
	m.Extra = extra
	return nil
}

// GetManagerStatus reads the CRM master's internal state
// (GET /cluster/ha/status/manager_status). No version gate (baseline
// endpoint). The shape is provisional — see ManagerStatus.
func (s *Service) GetManagerStatus(ctx context.Context) (*ManagerStatus, error) {
	var ms ManagerStatus
	if err := s.c.DoRequest(ctx, http.MethodGet, haStatusManagerPath(), nil, &ms); err != nil {
		return nil, fmt.Errorf("ha.GetManagerStatus: %w", err)
	}
	return &ms, nil
}

// HAStatusCurrent reads the live HA manager status
// (GET /cluster/ha/status/current): quorum, master, per-node lrm, and
// per-resource service rows. It is the observable for ArmHA/DisarmHA (the
// armed-state field) and for MigrateResource/RelocateResource convergence
// (service rows' Node). The endpoint is 9.0 baseline — no version gate; the
// 9.2-only fields (armed-state, resource_mode) are simply absent on older
// clusters.
func (s *Service) HAStatusCurrent(ctx context.Context) ([]HAStatusEntry, error) {
	var entries []HAStatusEntry
	if err := s.c.DoRequest(ctx, http.MethodGet, haStatusCurrentPath(), nil, &entries); err != nil {
		return nil, fmt.Errorf("ha.HAStatusCurrent: %w", err)
	}
	return entries, nil
}
