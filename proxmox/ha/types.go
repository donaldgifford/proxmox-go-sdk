package ha

import (
	"encoding/json"
	"fmt"
)

// ResourceState is the requested HA state for a managed resource.
type ResourceState string

const (
	// StateStarted keeps the resource running and relocates it on node failure
	// (PVE's "started"; "requested" is a synonym the SDK does not emit).
	StateStarted ResourceState = "started"
	// StateStopped keeps the resource stopped but under HA management.
	StateStopped ResourceState = "stopped"
	// StateDisabled stops the resource and takes it out of active management.
	StateDisabled ResourceState = "disabled"
	// StateIgnored leaves the resource entirely untouched by the HA stack.
	StateIgnored ResourceState = "ignored"
)

// HAResource is one entry from GET /cluster/ha/resources or
// GET /cluster/ha/resources/{sid}. Reads are lossless: keys outside the typed
// set are preserved in Extra.
type HAResource struct {
	SID         string        `json:"sid"`                    // e.g. "vm:100" or "ct:101".
	Type        string        `json:"type,omitempty"`         // "vm" or "ct".
	State       ResourceState `json:"state,omitempty"`        // requested HA state.
	Group       string        `json:"group,omitempty"`        // deprecated in 9.x; read-only compat.
	MaxRestart  int           `json:"max_restart,omitempty"`  // restart attempts before relocate.
	MaxRelocate int           `json:"max_relocate,omitempty"` // relocate attempts before giving up.
	Comment     string        `json:"comment,omitempty"`
	// Extra carries HA resource keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

// haResourceKnownFields lists the JSON keys HAResource models directly; keep it
// in sync with the struct so UnmarshalJSON routes only the rest into Extra.
var haResourceKnownFields = map[string]bool{
	"sid": true, "type": true, "state": true, "group": true,
	"max_restart": true, "max_relocate": true, "comment": true,
}

// UnmarshalJSON decodes the modelled fields and routes any unknown keys into
// Extra so a resource read round-trips losslessly.
func (r *HAResource) UnmarshalJSON(data []byte) error {
	type alias HAResource
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode ha resource: %w", err)
	}
	*r = HAResource(a)

	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return fmt.Errorf("decode ha resource map: %w", err)
	}
	for key, raw := range all {
		if haResourceKnownFields[key] {
			continue
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			s = string(raw) // non-string field: keep the raw token.
		}
		if r.Extra == nil {
			r.Extra = make(map[string]string)
		}
		r.Extra[key] = s
	}
	return nil
}

// HAResourceSpec is the body of POST /cluster/ha/resources, which places a
// guest under HA management. SID is required (e.g. "vm:100"); State defaults to
// "started" on the PVE side when empty. Pass it by pointer.
type HAResourceSpec struct {
	SID         string        `json:"sid"`
	State       ResourceState `json:"state,omitempty"`
	MaxRestart  int           `json:"max_restart,omitempty"`
	MaxRelocate int           `json:"max_relocate,omitempty"`
	Comment     string        `json:"comment,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// HAResourceUpdate is the body of PUT /cluster/ha/resources/{sid}. All fields
// are optional; only the set ones are sent. Use Delete to unset keys (a
// comma-separated list of PVE field names). Pass it by pointer.
type HAResourceUpdate struct {
	State       ResourceState `json:"state,omitempty"`
	MaxRestart  *int          `json:"max_restart,omitempty"`
	MaxRelocate *int          `json:"max_relocate,omitempty"`
	Comment     string        `json:"comment,omitempty"`
	Delete      string        `json:"delete,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}
