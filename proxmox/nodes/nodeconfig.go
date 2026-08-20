package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
)

// The node config carries two ACME property strings, and the SDK types both
// directions rather than making callers build them (the ha CRS-settings
// precedent). Everything else in the config — description, wakeonlan, location,
// ballooning-target — rides in Extra untouched.

// NodeACME is the node's "acme" property string: which ACME account issues its
// certificate, and (legacy form) which domains that certificate covers.
//
// Prefer the per-domain acmedomain slots ([NodeConfig.ACMEDomains]) over
// Domains: a slot names its own challenge plugin, which is what DNS-01 needs,
// while this list can only use the standalone HTTP-01 challenge.
type NodeACME struct {
	// Account is the ACME account config name from ListACMEAccounts. Empty
	// means PVE's "default" account.
	Account string
	// Domains is the legacy semicolon-separated domain list.
	Domains []string
}

// ACMEDomain is one "acmedomain[n]" property string: a domain on the node's
// certificate together with the plugin that answers its challenge.
type ACMEDomain struct {
	// Index is the slot this domain occupies — acmedomain0 through
	// acmedomain[ACMEDomainMaxIndex]. It is preserved across a read/write
	// round-trip so a sparse config (slots 0 and 3, with 1 and 2 unset) is not
	// silently renumbered.
	Index int
	// Domain is the FQDN to certify. Required.
	Domain string
	// Plugin is the ACME plugin ID that answers this domain's challenge — the
	// ID of an [ACMEPlugin]. Empty means PVE's built-in standalone HTTP-01
	// challenge, so a DNS-01 domain must name its plugin here.
	Plugin string
	// Alias is the domain the DNS-01 challenge is actually written to, when the
	// real domain CNAMEs its _acme-challenge record elsewhere.
	Alias string
}

// NodeConfig is the lossless read of GET /nodes/{node}/config. The ACME keys are
// parsed into typed fields; every other key is preserved verbatim in Extra.
type NodeConfig struct {
	// ACME is the parsed "acme" property string, or nil when the node has none.
	ACME *NodeACME
	// ACMEDomains holds the parsed acmedomain slots, ordered by Index. A slot
	// whose property string names no domain is left here unparsed and kept in
	// Extra under its original key instead.
	ACMEDomains []ACMEDomain
	// Digest guards a concurrent write; pass it to NodeConfigUpdate.
	Digest string
	// Extra holds every config key the SDK does not model, as its raw PVE
	// string value. It is populated on reads and ignored on writes.
	Extra map[string]string
}

// nodeConfigKnownFields names the fixed JSON keys that map to typed NodeConfig
// fields; it sizes the per-read set. The acmedomain slots are matched
// dynamically (their index is part of the key), so they are not listed here.
var nodeConfigKnownFields = map[string]bool{
	"acme": true, "digest": true,
}

// UnmarshalJSON parses the ACME property strings and routes every other key into
// Extra.
//
// A malformed acmedomain slot does not fail the read: the raw string stays in
// Extra under its own key. A node config the SDK cannot fully model is still
// worth returning — the alternative is an unreadable node.
func (c *NodeConfig) UnmarshalJSON(data []byte) error {
	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return fmt.Errorf("decode node config: %w", err)
	}

	*c = NodeConfig{}
	// known starts EMPTY and grows only as a key is successfully claimed by a
	// typed field. Pre-seeding it would mean a key the SDK failed to decode is
	// neither typed nor preserved in Extra — losing exactly what this type
	// promises to keep.
	known := make(map[string]bool, len(nodeConfigKnownFields))

	for key, raw := range all {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			continue // non-string value: leave it for Extra.
		}
		switch key {
		case "acme":
			// A JSON null decodes into a string without error; treat it as
			// absent so ACME stays nil, as documented.
			if string(raw) == "null" {
				continue
			}
			acme := parseNodeACME(value)
			c.ACME = &acme
			known[key] = true
		case "digest":
			c.Digest = value
			known[key] = true
		default:
			index, ok := acmeDomainSlot(key)
			if !ok {
				continue
			}
			domain, err := parseACMEDomain(value)
			if err != nil {
				continue // stays in Extra, raw.
			}
			domain.Index = index
			c.ACMEDomains = append(c.ACMEDomains, domain)
			known[key] = true
		}
	}
	slices.SortFunc(c.ACMEDomains, func(a, b ACMEDomain) int { return a.Index - b.Index })

	extra, err := svcutil.DecodeExtra(data, known)
	if err != nil {
		return fmt.Errorf("decode node config: %w", err)
	}
	c.Extra = extra
	return nil
}

// NodeConfigUpdate is a partial change to a node's config (PUT
// /nodes/{node}/config). Only the set fields are written.
//
// Clearing is always explicit: the update writes the acmedomain slots it is
// given and removes nothing on its own, so dropping a domain means naming its
// key in Delete (for example "acmedomain1"). Symmetric-but-explicit beats
// diffing a read against a write and guessing what the caller meant.
//
// Pass it to Service.SetNodeConfig by pointer.
type NodeConfigUpdate struct {
	// ACME replaces the node's acme property string; nil leaves it unchanged.
	ACME *NodeACME
	// ACMEDomains writes one acmedomain slot per entry, each at its own Index.
	// Slots not named here are left as they are.
	ACMEDomains []ACMEDomain
	// Delete names config keys to unset, CSV-joined into PVE's delete
	// parameter — "acmedomain1", "acme", "description".
	Delete []string
	// Digest is the digest from the read that informed this update. When set,
	// PVE refuses the write if the config changed meanwhile.
	Digest string
	// Extra carries config keys the SDK does not model, as raw PVE values.
	Extra map[string]string
}

// GetNodeConfig reads a node's configuration, including its ACME wiring.
func (s *Service) GetNodeConfig(ctx context.Context, node string) (*NodeConfig, error) {
	if node == "" {
		return nil, fmt.Errorf("nodes.GetNodeConfig: node: %w", svcutil.ErrMissingField)
	}
	var cfg NodeConfig
	if err := s.c.DoRequest(ctx, http.MethodGet, nodeConfigPath(node), nil, &cfg); err != nil {
		return nil, fmt.Errorf("nodes.GetNodeConfig: %w", err)
	}
	return &cfg, nil
}

// SetNodeConfig applies a partial config change. The write is synchronous (no
// task): PVE updates the node config file and answers with null data.
//
// Pointing a node at a DNS-01 certificate is two calls — this one to set the
// account and the domain slots, then OrderNodeCertificate to issue.
func (s *Service) SetNodeConfig(ctx context.Context, node string, update *NodeConfigUpdate) error {
	if update == nil {
		return fmt.Errorf("nodes.SetNodeConfig: %w", svcutil.ErrNilSpec)
	}
	if node == "" {
		return fmt.Errorf("nodes.SetNodeConfig: node: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(&struct{}{}, update.Extra)
	if err != nil {
		return fmt.Errorf("nodes.SetNodeConfig: %w", err)
	}
	if update.ACME != nil {
		acme := encodeNodeACME(*update.ACME)
		// An empty acme= would CLEAR the account and the legacy domain list.
		// This type promises clearing is explicit, so say so rather than doing
		// it silently on what looks like a no-op.
		if acme == "" {
			return fmt.Errorf(
				`nodes.SetNodeConfig: ACME is set but renders empty (use Delete: []string{"acme"} to clear it): %w`,
				svcutil.ErrInvalidValue)
		}
		body.Set("acme", acme)
	}
	seen := make(map[int]bool, len(update.ACMEDomains))
	for _, domain := range update.ACMEDomains {
		if domain.Index < 0 || domain.Index > ACMEDomainMaxIndex {
			return fmt.Errorf("nodes.SetNodeConfig: acmedomain index %d (PVE defines 0-%d): %w",
				domain.Index, ACMEDomainMaxIndex, svcutil.ErrInvalidValue)
		}
		if domain.Domain == "" {
			return fmt.Errorf("nodes.SetNodeConfig: acmedomain%d domain: %w",
				domain.Index, svcutil.ErrMissingField)
		}
		// Two entries at one index would silently drop a write, and the caller
		// would have no way to tell which one landed.
		if seen[domain.Index] {
			return fmt.Errorf("nodes.SetNodeConfig: acmedomain%d given twice: %w",
				domain.Index, svcutil.ErrInvalidValue)
		}
		seen[domain.Index] = true
		body.Set(ACMEDomainKey(domain.Index), encodeACMEDomain(domain))
	}
	if len(update.Delete) > 0 {
		body.Set("delete", strings.Join(update.Delete, ","))
	}
	if update.Digest != "" {
		body.Set("digest", update.Digest)
	}
	if err := s.c.DoRequest(ctx, http.MethodPut, nodeConfigPath(node), body, nil); err != nil {
		return fmt.Errorf("nodes.SetNodeConfig: %w", err)
	}
	return nil
}

// --- property-string codecs ---

// acmeDomainKeyPrefix is the config-key stem the slot index is appended to.
const acmeDomainKeyPrefix = "acmedomain"

// ACMEDomainMaxIndex is the highest acmedomain slot PVE 9.x defines. The PUT
// schema renders the key as the wildcard "acmedomain[n]", but it also sets
// additionalProperties:0, and the GET's property filter enumerates the concrete
// keys — acmedomain0 through acmedomain5. Writing to a higher slot is a
// parameter-verification error, not a stored setting.
const ACMEDomainMaxIndex = 5

// ACMEDomainKey renders the node-config key for a slot: ACMEDomainKey(1) is
// "acmedomain1". Use it to name a slot in [NodeConfigUpdate.Delete], since
// clearing one is explicit.
func ACMEDomainKey(index int) string {
	return acmeDomainKeyPrefix + strconv.Itoa(index)
}

// acmeDomainSlot reports whether key is an acmedomain slot and returns its
// index.
//
// The read path is deliberately more permissive than the write path: it accepts
// any slot number, including one above [ACMEDomainMaxIndex], because PVE returns
// a hand-edited config file verbatim and a key the SDK refuses to write is still
// a key it must not lose. The suffix must be canonical decimal, though —
// "acmedomain00" is not slot 0, or two config keys would parse to one slot and
// collide on write.
func acmeDomainSlot(key string) (int, bool) {
	suffix, ok := strings.CutPrefix(key, acmeDomainKeyPrefix)
	if !ok || suffix == "" {
		return 0, false
	}
	index, err := strconv.Atoi(suffix)
	if err != nil || index < 0 || strconv.Itoa(index) != suffix {
		return 0, false
	}
	return index, true
}

// parseNodeACME decodes the "acme" property string. Unknown sub-keys are
// ignored: PVE may add one, and dropping it is better than failing the read.
func parseNodeACME(s string) NodeACME {
	var out NodeACME
	for _, part := range strings.Split(s, ",") {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "account":
			out.Account = strings.TrimSpace(value)
		case "domains":
			for _, domain := range strings.Split(strings.TrimSpace(value), ";") {
				if domain = strings.TrimSpace(domain); domain != "" {
					out.Domains = append(out.Domains, domain)
				}
			}
		}
	}
	return out
}

// encodeNodeACME renders the "acme" property string from the set fields.
func encodeNodeACME(a NodeACME) string {
	var parts []string
	if a.Account != "" {
		parts = append(parts, "account="+a.Account)
	}
	if len(a.Domains) > 0 {
		parts = append(parts, "domains="+strings.Join(a.Domains, ";"))
	}
	return strings.Join(parts, ",")
}

// parseACMEDomain decodes an "acmedomain[n]" property string. The domain is
// PVE's default key, so it may appear bare ("host.example.com,plugin=cf") or
// keyed ("domain=host.example.com"). A string that yields no domain is an
// error: the sub-key is required, and an ACMEDomain without one is not
// something the SDK can write back.
//
// Index is not set here — it comes from the config key, not the value.
func parseACMEDomain(s string) (ACMEDomain, error) {
	var out ACMEDomain
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			// PVE's property-string parser takes a bare token as the default
			// key wherever it appears, not only in first position — its own
			// writer emits it first, but a hand-edited config is stored
			// verbatim and must still read back into the typed field.
			if out.Domain == "" {
				out.Domain = part
			}
			continue
		}
		switch strings.TrimSpace(key) {
		case "domain":
			out.Domain = strings.TrimSpace(value)
		case "plugin":
			out.Plugin = strings.TrimSpace(value)
		case "alias":
			out.Alias = strings.TrimSpace(value)
		}
	}
	if out.Domain == "" {
		return ACMEDomain{}, fmt.Errorf("acmedomain %q: domain: %w", s, svcutil.ErrMissingField)
	}
	return out, nil
}

// encodeACMEDomain renders an "acmedomain[n]" property string. The domain is
// written with its explicit key rather than bare — both are accepted, and the
// keyed form reads unambiguously in a node config file.
func encodeACMEDomain(d ACMEDomain) string {
	parts := []string{"domain=" + d.Domain}
	if d.Plugin != "" {
		parts = append(parts, "plugin="+d.Plugin)
	}
	if d.Alias != "" {
		parts = append(parts, "alias="+d.Alias)
	}
	return strings.Join(parts, ",")
}
