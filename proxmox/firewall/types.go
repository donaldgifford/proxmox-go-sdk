package firewall

import (
	"encoding/json"
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// RuleDirection is a firewall rule's direction/kind.
type RuleDirection string

// The firewall rule directions. "group" attaches a security group; "forward"
// is the 9.x forward chain.
const (
	RuleIn      RuleDirection = "in"
	RuleOut     RuleDirection = "out"
	RuleGroup   RuleDirection = "group"
	RuleForward RuleDirection = "forward"
)

// Rule is one entry from GET {scope}/firewall/rules[/{pos}]. Reads are lossless:
// keys outside the typed set are preserved in Extra. Pos is the rule's position
// in the scoped table (0 is evaluated first) and is set by PVE on read.
type Rule struct {
	Pos     int           `json:"pos"`
	Type    RuleDirection `json:"type,omitempty"`
	Action  string        `json:"action,omitempty"` // ACCEPT/DROP/REJECT or a security-group name.
	Enable  types.PVEBool `json:"enable,omitempty"`
	Macro   string        `json:"macro,omitempty"`
	Proto   string        `json:"proto,omitempty"`
	Source  string        `json:"source,omitempty"`
	Dest    string        `json:"dest,omitempty"`
	Sport   string        `json:"sport,omitempty"`
	Dport   string        `json:"dport,omitempty"`
	Iface   string        `json:"iface,omitempty"`
	Log     string        `json:"log,omitempty"` // log level: nolog/err/warning/info/debug.
	Comment string        `json:"comment,omitempty"`
	// Extra carries rule keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

var ruleKnownFields = map[string]bool{
	"pos": true, "type": true, "action": true, "enable": true, "macro": true,
	"proto": true, "source": true, "dest": true, "sport": true, "dport": true,
	"iface": true, "log": true, "comment": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (r *Rule) UnmarshalJSON(data []byte) error {
	type alias Rule
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode firewall rule: %w", err)
	}
	*r = Rule(a)
	extra, err := svcutil.DecodeExtra(data, ruleKnownFields)
	if err != nil {
		return fmt.Errorf("decode firewall rule: %w", err)
	}
	r.Extra = extra
	return nil
}

// RuleSpec is the body of POST {scope}/firewall/rules. Type and Action are
// required. New rules are inserted at the top of the table unless Pos is set.
// Pass it by pointer.
type RuleSpec struct {
	Type    RuleDirection  `json:"type"`
	Action  string         `json:"action"`
	Pos     *int           `json:"pos,omitempty"`
	Enable  *types.PVEBool `json:"enable,omitempty"`
	Macro   string         `json:"macro,omitempty"`
	Proto   string         `json:"proto,omitempty"`
	Source  string         `json:"source,omitempty"`
	Dest    string         `json:"dest,omitempty"`
	Sport   string         `json:"sport,omitempty"`
	Dport   string         `json:"dport,omitempty"`
	Iface   string         `json:"iface,omitempty"`
	Log     string         `json:"log,omitempty"`
	Comment string         `json:"comment,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// RuleUpdate is the body of PUT {scope}/firewall/rules/{pos}. All fields are
// optional; use Delete to unset keys. Pass it by pointer.
type RuleUpdate struct {
	Type    RuleDirection  `json:"type,omitempty"`
	Action  string         `json:"action,omitempty"`
	Enable  *types.PVEBool `json:"enable,omitempty"`
	Macro   string         `json:"macro,omitempty"`
	Proto   string         `json:"proto,omitempty"`
	Source  string         `json:"source,omitempty"`
	Dest    string         `json:"dest,omitempty"`
	Sport   string         `json:"sport,omitempty"`
	Dport   string         `json:"dport,omitempty"`
	Iface   string         `json:"iface,omitempty"`
	Log     string         `json:"log,omitempty"`
	Comment string         `json:"comment,omitempty"`
	Delete  string         `json:"delete,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// IPSet is one entry from GET {scope}/firewall/ipset — a named set of CIDRs a
// rule can reference.
type IPSet struct {
	Name    string `json:"name"`
	Comment string `json:"comment,omitempty"`
	// Extra carries IPSet keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

var ipsetKnownFields = map[string]bool{"name": true, "comment": true}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (s *IPSet) UnmarshalJSON(data []byte) error {
	type alias IPSet
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode firewall ipset: %w", err)
	}
	*s = IPSet(a)
	extra, err := svcutil.DecodeExtra(data, ipsetKnownFields)
	if err != nil {
		return fmt.Errorf("decode firewall ipset: %w", err)
	}
	s.Extra = extra
	return nil
}

// IPSetSpec is the body of POST {scope}/firewall/ipset. Name is required. Pass
// it by pointer.
type IPSetSpec struct {
	Name    string `json:"name"`
	Comment string `json:"comment,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// IPSetEntry is one CIDR in an IPSet, from GET {scope}/firewall/ipset/{name}.
// Reads are lossless. To add an entry, use IPSetEntrySpec.
type IPSetEntry struct {
	CIDR    string        `json:"cidr"`
	NoMatch types.PVEBool `json:"nomatch,omitempty"`
	Comment string        `json:"comment,omitempty"`
	// Extra carries entry keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

var ipsetEntryKnownFields = map[string]bool{
	"cidr": true, "nomatch": true, "comment": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (e *IPSetEntry) UnmarshalJSON(data []byte) error {
	type alias IPSetEntry
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode firewall ipset entry: %w", err)
	}
	*e = IPSetEntry(a)
	extra, err := svcutil.DecodeExtra(data, ipsetEntryKnownFields)
	if err != nil {
		return fmt.Errorf("decode firewall ipset entry: %w", err)
	}
	e.Extra = extra
	return nil
}

// IPSetEntrySpec is the body of POST {scope}/firewall/ipset/{name} — adding a
// CIDR to an IPSet. CIDR is required; NoMatch is a pointer so an unset value is
// distinct from an explicit false. Pass it by pointer.
type IPSetEntrySpec struct {
	CIDR    string         `json:"cidr"`
	NoMatch *types.PVEBool `json:"nomatch,omitempty"`
	Comment string         `json:"comment,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// Options is the scoped firewall options block from GET {scope}/firewall/options
// (the enable flag, default policies, logging, and scope-specific toggles). The
// set of keys differs by scope — cluster carries policy_in/policy_out, guests
// carry per-NIC toggles — so reads are lossless via Extra.
type Options struct {
	Enable      types.PVEBool `json:"enable,omitempty"`
	PolicyIn    string        `json:"policy_in,omitempty"`  // cluster: ACCEPT/DROP/REJECT.
	PolicyOut   string        `json:"policy_out,omitempty"` // cluster.
	LogLevelIn  string        `json:"log_level_in,omitempty"`
	LogLevelOut string        `json:"log_level_out,omitempty"`
	DHCP        types.PVEBool `json:"dhcp,omitempty"`      // guest.
	MACFilter   types.PVEBool `json:"macfilter,omitempty"` // guest.
	NDP         types.PVEBool `json:"ndp,omitempty"`
	// Extra carries options keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

var optionsKnownFields = map[string]bool{
	"enable": true, "policy_in": true, "policy_out": true,
	"log_level_in": true, "log_level_out": true, "dhcp": true,
	"macfilter": true, "ndp": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (o *Options) UnmarshalJSON(data []byte) error {
	type alias Options
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode firewall options: %w", err)
	}
	*o = Options(a)
	extra, err := svcutil.DecodeExtra(data, optionsKnownFields)
	if err != nil {
		return fmt.Errorf("decode firewall options: %w", err)
	}
	o.Extra = extra
	return nil
}

// OptionsUpdate is the body of PUT {scope}/firewall/options. All fields are
// optional; use Delete to unset keys. Pass it by pointer.
type OptionsUpdate struct {
	Enable      *types.PVEBool `json:"enable,omitempty"`
	PolicyIn    string         `json:"policy_in,omitempty"`
	PolicyOut   string         `json:"policy_out,omitempty"`
	LogLevelIn  string         `json:"log_level_in,omitempty"`
	LogLevelOut string         `json:"log_level_out,omitempty"`
	DHCP        *types.PVEBool `json:"dhcp,omitempty"`
	MACFilter   *types.PVEBool `json:"macfilter,omitempty"`
	NDP         *types.PVEBool `json:"ndp,omitempty"`
	Delete      string         `json:"delete,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}
