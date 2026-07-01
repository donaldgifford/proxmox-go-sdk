package ha

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// RuleType distinguishes the two 9.x HA rule variants. HA rules replace the
// deprecated HA groups: instead of assigning resources to a group, you express
// placement as node-affinity (pin resources to nodes) or resource-affinity
// (keep resources together or apart).
type RuleType string

const (
	// RuleTypeNodeAffinity pins the rule's resources to specific nodes (the
	// 9.x replacement for HA groups). Set HARuleSpec.Nodes.
	RuleTypeNodeAffinity RuleType = "node-affinity"
	// RuleTypeResourceAffinity co-locates or anti-locates the rule's resources
	// relative to each other. Set HARuleSpec.Resources.
	RuleTypeResourceAffinity RuleType = "resource-affinity"
)

// HARule is one entry from GET /cluster/ha/rules or /cluster/ha/rules/{rule}.
// Reads are lossless: keys outside the typed set are preserved in Extra.
type HARule struct {
	Rule      string        `json:"rule"`                // unique rule name.
	Type      RuleType      `json:"type,omitempty"`      // node-affinity or resource-affinity.
	Nodes     string        `json:"nodes,omitempty"`     // CSV of nodes; node-affinity rules.
	Resources string        `json:"resources,omitempty"` // CSV of SIDs; resource-affinity rules.
	Affinity  string        `json:"affinity,omitempty"`  // "positive" or "negative"; resource-affinity only.
	Disable   types.PVEBool `json:"disable,omitempty"`   // PVE stores the disabled flag, not enabled.
	Comment   string        `json:"comment,omitempty"`
	// Extra carries HA rule keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

// haRuleKnownFields lists the JSON keys HARule models directly; keep it in sync
// with the struct so UnmarshalJSON routes only the rest into Extra.
var haRuleKnownFields = map[string]bool{
	"rule": true, "type": true, "nodes": true, "resources": true,
	"affinity": true, "disable": true, "comment": true,
}

// UnmarshalJSON decodes the modelled fields and routes any unknown keys into
// Extra so a rule read round-trips losslessly.
func (r *HARule) UnmarshalJSON(data []byte) error {
	type alias HARule
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode ha rule: %w", err)
	}
	*r = HARule(a)

	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return fmt.Errorf("decode ha rule map: %w", err)
	}
	for key, raw := range all {
		if haRuleKnownFields[key] {
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

// HARuleSpec is the body of POST /cluster/ha/rules. Rule (the name) and Type are
// required. For RuleTypeNodeAffinity set Nodes; for RuleTypeResourceAffinity set
// Resources (and optionally Affinity "positive"/"negative"). Pass it by pointer.
//
// API-shape caveat: the 9.x HA rule endpoint is confirmed in the PVE apidoc, but
// the exact per-variant parameter names have not been verified against a live
// 9.x node; the SDK uses the apidoc names (nodes, resources, affinity)
// provisionally.
type HARuleSpec struct {
	Rule string   `json:"rule"`
	Type RuleType `json:"type"`
	// Nodes is a CSV of node names (node-affinity). It is json:"-" and joined
	// into the "nodes" form param manually, since the flat encoder cannot render
	// a slice.
	Nodes []string `json:"-"`
	// Resources is a CSV of SIDs (resource-affinity), joined the same way.
	Resources []string       `json:"-"`
	Affinity  string         `json:"affinity,omitempty"`
	Disable   *types.PVEBool `json:"disable,omitempty"`
	Comment   string         `json:"comment,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// HARuleUpdate is the body of PUT /cluster/ha/rules/{rule}. All fields are
// optional; only the set ones are sent. Use Delete to unset keys. Pass it by
// pointer.
type HARuleUpdate struct {
	Nodes     []string       `json:"-"`
	Resources []string       `json:"-"`
	Affinity  string         `json:"affinity,omitempty"`
	Disable   *types.PVEBool `json:"disable,omitempty"`
	Comment   string         `json:"comment,omitempty"`
	Delete    string         `json:"delete,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// ListRules returns every HA rule.
func (s *Service) ListRules(ctx context.Context) ([]HARule, error) {
	var rules []HARule
	if err := s.c.DoRequest(ctx, http.MethodGet, haRulesPath(), nil, &rules); err != nil {
		return nil, fmt.Errorf("ha.ListRules: %w", err)
	}
	return rules, nil
}

// GetRule returns one HA rule by name.
func (s *Service) GetRule(ctx context.Context, rule string) (*HARule, error) {
	var r HARule
	if err := s.c.DoRequest(ctx, http.MethodGet, haRulePath(rule), nil, &r); err != nil {
		return nil, fmt.Errorf("ha.GetRule: %w", err)
	}
	return &r, nil
}

// CreateRule defines a new HA rule. The write is synchronous (no task).
func (s *Service) CreateRule(ctx context.Context, spec *HARuleSpec) error {
	if spec == nil {
		return fmt.Errorf("ha.CreateRule: %w", svcutil.ErrNilSpec)
	}
	switch {
	case spec.Rule == "":
		return fmt.Errorf("ha.CreateRule: rule: %w", svcutil.ErrMissingField)
	case spec.Type == "":
		return fmt.Errorf("ha.CreateRule: type: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return fmt.Errorf("ha.CreateRule: %w", err)
	}
	if len(spec.Nodes) > 0 {
		body.Set("nodes", strings.Join(spec.Nodes, ","))
	}
	if len(spec.Resources) > 0 {
		body.Set("resources", strings.Join(spec.Resources, ","))
	}
	if err := s.c.DoRequest(ctx, http.MethodPost, haRulesPath(), body, nil); err != nil {
		return fmt.Errorf("ha.CreateRule: %w", err)
	}
	return nil
}

// UpdateRule changes an HA rule, including enabling or disabling it (set Disable).
// The write is synchronous (no task).
func (s *Service) UpdateRule(ctx context.Context, rule string, update *HARuleUpdate) error {
	if update == nil {
		return fmt.Errorf("ha.UpdateRule: %w", svcutil.ErrNilSpec)
	}
	if rule == "" {
		return fmt.Errorf("ha.UpdateRule: rule: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(update, update.Extra)
	if err != nil {
		return fmt.Errorf("ha.UpdateRule: %w", err)
	}
	if len(update.Nodes) > 0 {
		body.Set("nodes", strings.Join(update.Nodes, ","))
	}
	if len(update.Resources) > 0 {
		body.Set("resources", strings.Join(update.Resources, ","))
	}
	if err := s.c.DoRequest(ctx, http.MethodPut, haRulePath(rule), body, nil); err != nil {
		return fmt.Errorf("ha.UpdateRule: %w", err)
	}
	return nil
}

// DeleteRule removes an HA rule. The write is synchronous (no task).
func (s *Service) DeleteRule(ctx context.Context, rule string) error {
	if rule == "" {
		return fmt.Errorf("ha.DeleteRule: rule: %w", svcutil.ErrMissingField)
	}
	if err := s.c.DoRequest(ctx, http.MethodDelete, haRulePath(rule), nil, nil); err != nil {
		return fmt.Errorf("ha.DeleteRule: %w", err)
	}
	return nil
}
