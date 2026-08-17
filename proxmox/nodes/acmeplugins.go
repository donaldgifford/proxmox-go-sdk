package nodes

import (
	"encoding/json"
	"fmt"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// ACME challenge types. PVE admits exactly these two: a DNS-01 challenge driven
// by an acme.sh provider plugin, or the built-in standalone HTTP-01 challenge.
const (
	// ChallengeTypeDNS is the DNS-01 challenge. A plugin of this type carries
	// provider credentials (see ACMEPluginData).
	ChallengeTypeDNS = "dns"
	// ChallengeTypeStandalone is PVE's built-in HTTP-01 challenge. It needs no
	// credentials, so a standalone plugin has no data.
	ChallengeTypeStandalone = "standalone"
)

// ACMEPlugin is one ACME challenge plugin from GET /cluster/acme/plugins or
// /cluster/acme/plugins/{id}. Reads are lossless: keys the SDK does not model
// are preserved in Extra.
//
// Data is the stored credential payload exactly as PVE returns it — base64 of
// newline-separated KEY=value lines. The SDK deliberately offers no decode
// helper, so printing an ACMEPlugin cannot spill plaintext credentials.
type ACMEPlugin struct {
	Plugin          string        `json:"plugin,omitempty"` // the plugin ID.
	Type            string        `json:"type,omitempty"`   // ChallengeTypeDNS or ChallengeTypeStandalone.
	API             string        `json:"api,omitempty"`    // acme.sh plugin name, e.g. "cf".
	Data            string        `json:"data,omitempty"`   // base64 credential payload, verbatim.
	ValidationDelay int           `json:"validation-delay,omitempty"`
	Nodes           string        `json:"nodes,omitempty"` // comma-separated node names; empty means all.
	Disable         types.PVEBool `json:"disable,omitempty"`
	Digest          string        `json:"digest,omitempty"` // config digest; pass to an update to guard it.
	// Extra holds plugin keys the SDK does not model, as their raw PVE string
	// values. It is populated on reads and ignored on writes.
	Extra map[string]string `json:"-"`
}

// acmePluginKnownFields lists the JSON keys that map to typed ACMEPlugin
// fields. Keep it in sync when adding a field.
var acmePluginKnownFields = map[string]bool{
	"plugin": true, "type": true, "api": true, "data": true,
	"validation-delay": true, "nodes": true, "disable": true, "digest": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (p *ACMEPlugin) UnmarshalJSON(data []byte) error {
	type alias ACMEPlugin
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("decode acme plugin: %w", err)
	}
	*p = ACMEPlugin(raw)

	extra, err := svcutil.DecodeExtra(data, acmePluginKnownFields)
	if err != nil {
		return fmt.Errorf("decode acme plugin: %w", err)
	}
	p.Extra = extra
	return nil
}

// ACMEPluginSpec is the body of POST /cluster/acme/plugins, registering a
// challenge plugin. ID is required. Data is required for ChallengeTypeDNS and
// supplies both the api and data parameters; leave it nil for
// ChallengeTypeStandalone. Type defaults to ChallengeTypeDNS when empty.
//
// Data carries live provider credentials — do not log this spec.
//
// Pass it to Service.CreateACMEPlugin by pointer.
type ACMEPluginSpec struct {
	ID   string `json:"id"`             // required; a PVE config ID.
	Type string `json:"type,omitempty"` // defaults to ChallengeTypeDNS.
	// Data supplies the provider credentials. The SDK derives the api and
	// (base64) data parameters from it.
	Data ACMEPluginData `json:"-"`
	// ValidationDelay is the extra wait before asking the CA to validate, in
	// seconds. It exists to outlast a slow DNS TTL; nil takes PVE's default.
	ValidationDelay *int `json:"validation-delay,omitempty"`
	// Nodes restricts the plugin to these cluster nodes; empty means all. It is
	// CSV-joined into PVE's nodes parameter.
	Nodes   []string       `json:"-"`
	Disable *types.PVEBool `json:"disable,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// ACMEPluginUpdate is the body of PUT /cluster/acme/plugins/{id}. Only the set
// fields are sent. Set Data to rotate credentials; leave it nil to keep the
// stored payload.
//
// Pass it to Service.UpdateACMEPlugin by pointer.
type ACMEPluginUpdate struct {
	// Data replaces the stored credentials (and the api parameter with it).
	// Leave nil to keep what is stored.
	Data            ACMEPluginData `json:"-"`
	ValidationDelay *int           `json:"validation-delay,omitempty"`
	Nodes           []string       `json:"-"`
	Disable         *types.PVEBool `json:"disable,omitempty"`
	// Delete names plugin keys to unset. It is CSV-joined into PVE's delete
	// parameter.
	Delete []string `json:"-"`
	// Digest is the config digest from the read that informed this update. When
	// set, PVE refuses the write if the config changed meanwhile.
	Digest string `json:"digest,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// ChallengeSchemaEntry is one provider from GET /cluster/acme/challenge-schema:
// the id PVE expects in a plugin's api field, a human-readable name, the
// challenge type it serves, and the provider's own credential-field schema.
//
// Schema is kept as raw JSON: its shape is provider-defined, so the SDK
// preserves it verbatim rather than modelling 160 variations. Read it to
// discover the field names a [RawPluginData] needs.
type ChallengeSchemaEntry struct {
	ID     string          `json:"id"`
	Name   string          `json:"name,omitempty"`
	Type   string          `json:"type,omitempty"`
	Schema json.RawMessage `json:"schema,omitempty"`
}

// ACMEDirectory is one named CA endpoint from GET /cluster/acme/directories —
// the well-known ACME directory URLs PVE ships, including Let's Encrypt
// production and staging. Use a staging URL as ACMEAccountSpec.Directory when
// testing, so failed orders do not burn production rate limits.
type ACMEDirectory struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// ACMEMeta is the CA's directory metadata from GET /cluster/acme/meta. Reads
// are lossless; the CA may publish fields beyond those modelled here.
//
// TermsOfService is the URL to pass as ACMEAccountSpec.TOSURL when registering
// an account, which is how a caller accepts the CA's terms honestly rather than
// hardcoding a URL.
type ACMEMeta struct {
	TermsOfService          string   `json:"termsOfService,omitempty"`
	Website                 string   `json:"website,omitempty"`
	CAAIdentities           []string `json:"caaIdentities,omitempty"`
	ExternalAccountRequired bool     `json:"externalAccountRequired,omitempty"`
	// Extra holds metadata keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

var acmeMetaKnownFields = map[string]bool{
	"termsOfService": true, "website": true,
	"caaIdentities": true, "externalAccountRequired": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (m *ACMEMeta) UnmarshalJSON(data []byte) error {
	type alias ACMEMeta
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("decode acme meta: %w", err)
	}
	*m = ACMEMeta(raw)

	extra, err := svcutil.DecodeExtra(data, acmeMetaKnownFields)
	if err != nil {
		return fmt.Errorf("decode acme meta: %w", err)
	}
	m.Extra = extra
	return nil
}
