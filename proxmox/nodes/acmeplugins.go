package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

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

// ListACMEPlugins returns every configured ACME challenge plugin. ACME is
// cluster-scoped: this and the other plugin methods take no node.
func (s *Service) ListACMEPlugins(ctx context.Context) ([]ACMEPlugin, error) {
	var out []ACMEPlugin
	if err := s.c.DoRequest(ctx, http.MethodGet, acmePluginsPath(), nil, &out); err != nil {
		return nil, fmt.Errorf("nodes.ListACMEPlugins: %w", err)
	}
	return out, nil
}

// GetACMEPlugin returns one challenge plugin by ID. The returned Digest can be
// passed to ACMEPluginUpdate to guard against a concurrent change.
func (s *Service) GetACMEPlugin(ctx context.Context, id string) (*ACMEPlugin, error) {
	if id == "" {
		return nil, fmt.Errorf("nodes.GetACMEPlugin: id: %w", svcutil.ErrMissingField)
	}
	var plugin ACMEPlugin
	if err := s.c.DoRequest(ctx, http.MethodGet, acmePluginPath(id), nil, &plugin); err != nil {
		return nil, fmt.Errorf("nodes.GetACMEPlugin: %w", err)
	}
	return &plugin, nil
}

// CreateACMEPlugin registers a challenge plugin. The write is synchronous (no
// task): PVE stores it in the cluster config and answers with null data.
//
// For a DNS-01 plugin, spec.Data supplies the provider and its credentials; the
// SDK renders them to PVE's api and base64 data parameters. A standalone plugin
// needs no Data.
func (s *Service) CreateACMEPlugin(ctx context.Context, spec *ACMEPluginSpec) error {
	if spec == nil {
		return fmt.Errorf("nodes.CreateACMEPlugin: %w", svcutil.ErrNilSpec)
	}
	if spec.ID == "" {
		return fmt.Errorf("nodes.CreateACMEPlugin: id: %w", svcutil.ErrMissingField)
	}
	challenge := spec.Type
	if challenge == "" {
		challenge = ChallengeTypeDNS
	}
	if challenge == ChallengeTypeDNS && spec.Data == nil {
		return fmt.Errorf("nodes.CreateACMEPlugin: data is required for a %s plugin: %w",
			ChallengeTypeDNS, svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return fmt.Errorf("nodes.CreateACMEPlugin: %w", err)
	}
	body.Set("type", challenge)
	applyPluginData(body, spec.Data)
	if len(spec.Nodes) > 0 {
		body.Set("nodes", strings.Join(spec.Nodes, ","))
	}
	if err := s.c.DoRequest(ctx, http.MethodPost, acmePluginsPath(), body, nil); err != nil {
		return fmt.Errorf("nodes.CreateACMEPlugin: %w", err)
	}
	return nil
}

// UpdateACMEPlugin changes a challenge plugin. The write is synchronous.
//
// Set update.Data to rotate the provider credentials; leave it nil to keep the
// stored payload. Set update.Digest to the digest from the read that informed
// the update and PVE refuses the write if the config changed meanwhile.
func (s *Service) UpdateACMEPlugin(ctx context.Context, id string, update *ACMEPluginUpdate) error {
	if update == nil {
		return fmt.Errorf("nodes.UpdateACMEPlugin: %w", svcutil.ErrNilSpec)
	}
	if id == "" {
		return fmt.Errorf("nodes.UpdateACMEPlugin: id: %w", svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(update, update.Extra)
	if err != nil {
		return fmt.Errorf("nodes.UpdateACMEPlugin: %w", err)
	}
	applyPluginData(body, update.Data)
	if len(update.Nodes) > 0 {
		body.Set("nodes", strings.Join(update.Nodes, ","))
	}
	if len(update.Delete) > 0 {
		body.Set("delete", strings.Join(update.Delete, ","))
	}
	if err := s.c.DoRequest(ctx, http.MethodPut, acmePluginPath(id), body, nil); err != nil {
		return fmt.Errorf("nodes.UpdateACMEPlugin: %w", err)
	}
	return nil
}

// DeleteACMEPlugin removes a challenge plugin. The write is synchronous.
//
// A node config still referencing the plugin is not rewritten: clear the
// reference with SetNodeConfig first, or its next certificate order fails.
func (s *Service) DeleteACMEPlugin(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("nodes.DeleteACMEPlugin: id: %w", svcutil.ErrMissingField)
	}
	if err := s.c.DoRequest(ctx, http.MethodDelete, acmePluginPath(id), nil, nil); err != nil {
		return fmt.Errorf("nodes.DeleteACMEPlugin: %w", err)
	}
	return nil
}

// applyPluginData writes the api and data parameters for d, or leaves the form
// untouched when d is nil (a standalone plugin, or an update that keeps the
// stored credentials).
func applyPluginData(body url.Values, d ACMEPluginData) {
	if d == nil {
		return
	}
	body.Set("api", d.API())
	if encoded := encodePluginData(d); encoded != "" {
		body.Set("data", encoded)
	}
}

// GetChallengeSchema returns the challenge providers this node supports, each
// with the raw credential-field schema PVE publishes for it. Cluster-scoped.
//
// This is the authoritative source for a provider's id and field names: read it
// to build a [RawPluginData] for a provider the SDK does not type, or to confirm
// a typed provider's fields against the node itself.
func (s *Service) GetChallengeSchema(ctx context.Context) ([]ChallengeSchemaEntry, error) {
	var out []ChallengeSchemaEntry
	if err := s.c.DoRequest(ctx, http.MethodGet, acmeChallengeSchemaPath(), nil, &out); err != nil {
		return nil, fmt.Errorf("nodes.GetChallengeSchema: %w", err)
	}
	return out, nil
}

// ListACMEDirectories returns the named ACME directory endpoints PVE ships,
// including Let's Encrypt production and staging. Cluster-scoped.
//
// Pass a staging URL as ACMEAccountSpec.Directory while testing: staging has far
// looser rate limits, so a failed order costs nothing.
func (s *Service) ListACMEDirectories(ctx context.Context) ([]ACMEDirectory, error) {
	var out []ACMEDirectory
	if err := s.c.DoRequest(ctx, http.MethodGet, acmeDirectoriesPath(), nil, &out); err != nil {
		return nil, fmt.Errorf("nodes.ListACMEDirectories: %w", err)
	}
	return out, nil
}

// GetACMEMeta returns a CA's directory metadata — its terms-of-service URL,
// website, CAA identities, and whether external account binding is required.
// Cluster-scoped.
//
// With no options it queries PVE's default CA (Let's Encrypt production); pass
// WithACMEDirectory to ask a different one, e.g. the staging URL from
// ListACMEDirectories. Read TermsOfService here rather than hardcoding a URL
// when registering an account with RegisterACMEAccount.
//
// It supersedes PVE's deprecated /cluster/acme/tos, which the SDK does not
// implement.
func (s *Service) GetACMEMeta(ctx context.Context, opts ...ACMEMetaOption) (*ACMEMeta, error) {
	var cfg acmeMetaConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	path := acmeMetaPath()
	if cfg.directory != "" {
		path += "?directory=" + url.QueryEscape(cfg.directory)
	}
	var meta ACMEMeta
	if err := s.c.DoRequest(ctx, http.MethodGet, path, nil, &meta); err != nil {
		return nil, fmt.Errorf("nodes.GetACMEMeta: %w", err)
	}
	return &meta, nil
}

// acmeMetaConfig collects the optional query parameters for GetACMEMeta.
type acmeMetaConfig struct {
	directory string
}

// ACMEMetaOption configures GetACMEMeta.
type ACMEMetaOption func(*acmeMetaConfig)

// WithACMEDirectory queries the CA at the given ACME directory URL instead of
// PVE's default. Use a URL from ListACMEDirectories.
func WithACMEDirectory(directoryURL string) ACMEMetaOption {
	return func(c *acmeMetaConfig) { c.directory = directoryURL }
}
