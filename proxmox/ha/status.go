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
