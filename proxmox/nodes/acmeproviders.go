package nodes

import (
	"encoding/base64"
	"slices"
	"strings"
)

// ACMEPluginData supplies a DNS provider's acme.sh credentials for an ACME
// plugin. Implementations return the acme.sh plugin name PVE stores in the
// plugin's api field plus the credential environment variables it stores,
// base64 encoded, in the data field; the SDK owns that encoding so a caller
// never handles base64.
//
// The SDK ships typed implementations for the providers it has verified —
// [ACMECloudflare] and [ACMENamecheap] — and [ACMERawPluginData] for every
// other provider PVE's api enum admits (160 on 9.2). Add a provider by
// implementing these two methods; nothing else in the SDK needs to change.
//
// Values returned by Data are live credentials. Do not log them, and do not log
// an [ACMEPluginSpec] that carries them. The shipped implementations redact
// themselves under fmt's %v and %s, but an implementation of your own does not
// inherit that.
type ACMEPluginData interface {
	// API returns the acme.sh plugin name PVE's api parameter expects,
	// e.g. "cf" or "namecheap". It must not be empty: the SDK refuses a write
	// whose provider is unnamed rather than storing a plugin PVE cannot drive.
	API() string
	// Data returns the provider's credential environment variables keyed by the
	// acme.sh variable name. Empty values are omitted when rendered. The SDK
	// reads the map during the write that carries it and never retains it.
	Data() map[string]string
}

// ACMECloudflare holds Cloudflare DNS credentials for the acme.sh "cf" plugin.
//
// Prefer the scoped API token (Token, a token with Zone.DNS edit on the target
// zone) over the legacy global key (Key plus Email). ZoneID is optional and
// lets acme.sh skip its zone lookup.
//
// The field-to-variable mapping follows acme.sh's documented dnsapi variables.
// PVE publishes the authoritative per-provider field set at runtime — see
// [Service.GetACMEChallengeSchema] — and this mapping is confirmed against it
// during live verification.
type ACMECloudflare struct {
	Token     string // CF_Token — scoped API token (recommended).
	AccountID string // CF_Account_ID — account the zone belongs to.
	ZoneID    string // CF_Zone_ID — optional; skips the zone lookup.
	Key       string // CF_Key — legacy global API key; use with Email.
	Email     string // CF_Email — legacy account email; use with Key.
}

// API returns Cloudflare's acme.sh plugin name.
func (ACMECloudflare) API() string { return "cf" }

// Data returns the CF_* credential variables. Unset fields are omitted.
//
// The value receiver is deliberate even though ACMECloudflare sits exactly at
// gocritic's 80-byte hugeParam threshold: every provider satisfying
// ACMEPluginData by value keeps construction uniform (no "&" on some providers
// but not others) and makes a typed-nil receiver impossible. The copy happens
// once per plugin write, never in a hot path.
//
//nolint:gocritic // hugeParam: see the note above — uniformity over an 80-byte copy.
func (c ACMECloudflare) Data() map[string]string {
	return map[string]string{
		"CF_Token":      c.Token,
		"CF_Account_ID": c.AccountID,
		"CF_Zone_ID":    c.ZoneID,
		"CF_Key":        c.Key,
		"CF_Email":      c.Email,
	}
}

// String redacts the credentials. fmt consults it for %v and %s, including when
// this value is the Data field of an [ACMEPluginSpec], so a consumer logging a
// spec cannot spill a live token by accident.
//
// The receiver must be a value, not a pointer: fmt will not call a pointer
// method on the non-addressable value stored in an ACMEPluginData interface.
func (ACMECloudflare) String() string { return "nodes.ACMECloudflare{<redacted>}" }

// ACMENamecheap holds Namecheap DNS credentials for the acme.sh "namecheap"
// plugin. All three fields are required by the provider: its API authenticates
// the caller by API key AND by source IP, which must be allowlisted in the
// Namecheap account, so SourceIP is the address PVE will call out from.
//
// Field names are read off acme.sh's dns_namecheap.sh, the script PVE actually
// runs, so they are not guesses — but they have never been exercised against a
// live Namecheap zone: that run was descoped from IMPL-0007 (it needs the shared
// domain's nameservers moved to Namecheap and the node's egress IP allowlisted).
// [ACMECloudflare] is the live-verified provider. If a Namecheap challenge
// fails, check the field names against the node's own answer first —
// [Service.GetACMEChallengeSchema] publishes what the installed acme.sh expects.
type ACMENamecheap struct {
	Username string // NAMECHEAP_USERNAME — API username.
	APIKey   string // NAMECHEAP_API_KEY — API key.
	SourceIP string // NAMECHEAP_SOURCEIP — allowlisted caller address.
}

// API returns Namecheap's acme.sh plugin name.
func (ACMENamecheap) API() string { return "namecheap" }

// Data returns the NAMECHEAP_* credential variables.
func (n ACMENamecheap) Data() map[string]string {
	return map[string]string{
		"NAMECHEAP_USERNAME": n.Username,
		"NAMECHEAP_API_KEY":  n.APIKey,
		"NAMECHEAP_SOURCEIP": n.SourceIP,
	}
}

// String redacts the credentials; see [ACMECloudflare.String].
func (ACMENamecheap) String() string { return "nodes.ACMENamecheap{<redacted>}" }

// ACMERawPluginData reaches any provider PVE supports without a typed struct:
// Provider is the acme.sh plugin name (what API returns, and what PVE stores in
// its api parameter) and Values the credential variables it expects.
//
// Use [Service.GetACMEChallengeSchema] to discover a provider's id and its field
// names from the node itself rather than guessing them.
type ACMERawPluginData struct {
	Provider string
	Values   map[string]string
}

// API returns the configured provider name.
func (r ACMERawPluginData) API() string { return r.Provider }

// Data returns the configured credential variables.
func (r ACMERawPluginData) Data() map[string]string { return r.Values }

// String names the provider but redacts its values; see [ACMECloudflare.String].
func (r ACMERawPluginData) String() string {
	return "nodes.ACMERawPluginData{Provider:" + r.Provider + ", Values:<redacted>}"
}

// Compile-time assertions that the shipped providers satisfy the contract.
var (
	_ ACMEPluginData = ACMECloudflare{}
	_ ACMEPluginData = ACMENamecheap{}
	_ ACMEPluginData = ACMERawPluginData{}
)

// encodePluginData renders a provider's credentials to PVE's data parameter:
// newline-separated KEY=value lines, base64 encoded. Keys are sorted so the
// encoding is deterministic, and empty values are omitted so an unset optional
// field is absent rather than blank.
//
// It returns "" when every value is empty, which callers treat as an error
// rather than a write: a credential-less DNS plugin is accepted by PVE and then
// fails every certificate order.
func encodePluginData(d ACMEPluginData) string {
	values := d.Data()
	keys := make([]string, 0, len(values))
	for k, v := range values {
		if v == "" {
			continue
		}
		keys = append(keys, k)
	}
	slices.Sort(keys)
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, k+"="+values[k])
	}
	return base64.StdEncoding.EncodeToString([]byte(strings.Join(lines, "\n")))
}
