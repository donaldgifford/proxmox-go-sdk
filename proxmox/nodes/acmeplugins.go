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

// ACMEChallengeType is a plugin's challenge mechanism. PVE admits exactly the
// two values below; the SDK refuses any other on a write rather than letting a
// typo (say "DNS") reach the cluster config.
type ACMEChallengeType string

const (
	// ACMEChallengeTypeDNS is the DNS-01 challenge. A plugin of this type
	// carries provider credentials (see [ACMEPluginData]).
	ACMEChallengeTypeDNS ACMEChallengeType = "dns"
	// ACMEChallengeTypeStandalone is PVE's built-in HTTP-01 challenge. It needs
	// no credentials, so a standalone plugin has no data.
	ACMEChallengeTypeStandalone ACMEChallengeType = "standalone"
)

// ACMEPlugin is one ACME challenge plugin from GET /cluster/acme/plugins or
// /cluster/acme/plugins/{id}. Reads are lossless: keys the SDK does not model
// are preserved in Extra.
//
// Data is the stored credential payload exactly as PVE returns it — base64 of
// newline-separated KEY=value lines. The SDK offers no decode helper, but
// base64 is an encoding and not protection: redact Data before logging it. The
// String method does that for you.
type ACMEPlugin struct {
	Plugin          string            `json:"plugin,omitempty"` // the plugin ID.
	Type            ACMEChallengeType `json:"type,omitempty"`
	API             string            `json:"api,omitempty"`  // acme.sh plugin name, e.g. "cf".
	Data            string            `json:"data,omitempty"` // base64 credential payload, verbatim.
	ValidationDelay int               `json:"validation-delay,omitempty"`
	Nodes           string            `json:"nodes,omitempty"` // comma-separated node names; empty means all.
	Disable         types.PVEBool     `json:"disable,omitempty"`
	Digest          string            `json:"digest,omitempty"` // config digest; pass to an update to guard it.
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

// String renders the plugin with its credential payload elided, so logging a
// read is safe. Reach for [ACMEPlugin.Data] explicitly when you genuinely need
// the stored blob.
func (p ACMEPlugin) String() string {
	data := "<empty>"
	if p.Data != "" {
		data = "<redacted>"
	}
	return fmt.Sprintf("nodes.ACMEPlugin{Plugin:%s Type:%s API:%s Data:%s Nodes:%s Disable:%v}",
		p.Plugin, p.Type, p.API, data, p.Nodes, bool(p.Disable))
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
	ID string `json:"id"` // required; a PVE config ID.
	// Type defaults to ACMEChallengeTypeDNS when empty. Any value other than
	// the two defined constants is refused before the request is sent.
	Type ACMEChallengeType `json:"type,omitempty"`
	// Data supplies the provider credentials. The SDK derives the api and
	// (base64) data parameters from it.
	Data ACMEPluginData `json:"-"`
	// ValidationDelay is the extra wait before asking the CA to validate, in
	// seconds. It exists to outlast a slow DNS TTL; nil takes PVE's default.
	ValidationDelay *int `json:"validation-delay,omitempty"`
	// Nodes restricts the plugin to these cluster nodes; empty means all. It is
	// CSV-joined into PVE's nodes parameter.
	Nodes []string `json:"-"`
	// Disable registers the plugin without putting it in service; nil leaves it
	// enabled.
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
	// Nodes REPLACES the node restriction. Empty leaves the stored restriction
	// unchanged — note this differs from [ACMEPluginSpec.Nodes], where empty
	// means every node. To widen a restricted plugin back to the whole cluster,
	// name "nodes" in Delete.
	Nodes []string `json:"-"`
	// Disable takes the plugin out of (or back into) service; nil leaves the
	// stored setting unchanged.
	Disable *types.PVEBool `json:"disable,omitempty"`
	// Delete names plugin keys to unset. It is CSV-joined into PVE's delete
	// parameter.
	Delete []string `json:"-"`
	// Digest is the config digest from the read that informed this update. When
	// set, PVE refuses the write if the config changed meanwhile.
	Digest string `json:"digest,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// ACMEChallengeSchemaEntry is one provider from GET /cluster/acme/challenge-schema:
// the id PVE expects in a plugin's api field, a human-readable name, the
// challenge type it serves, and the provider's own credential-field schema.
//
// Schema is kept as raw JSON: its shape is provider-defined, so the SDK
// preserves it verbatim rather than modelling 160 variations. Read it to
// discover the field names an [ACMERawPluginData] needs.
type ACMEChallengeSchemaEntry struct {
	ID     string            `json:"id"`
	Name   string            `json:"name,omitempty"`
	Type   ACMEChallengeType `json:"type,omitempty"`
	Schema json.RawMessage   `json:"schema,omitempty"`
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
		challenge = ACMEChallengeTypeDNS
	}
	switch challenge {
	case ACMEChallengeTypeDNS, ACMEChallengeTypeStandalone:
	default:
		return fmt.Errorf("nodes.CreateACMEPlugin: type %q: %w",
			challenge, svcutil.ErrInvalidValue)
	}
	if challenge == ACMEChallengeTypeDNS && spec.Data == nil {
		return fmt.Errorf("nodes.CreateACMEPlugin: data (required for a %s plugin): %w",
			ACMEChallengeTypeDNS, svcutil.ErrMissingField)
	}
	body, err := svcutil.EncodeWithExtra(spec, spec.Extra)
	if err != nil {
		return fmt.Errorf("nodes.CreateACMEPlugin: %w", err)
	}
	body.Set("type", string(challenge))
	if err := applyPluginData(body, spec.Data); err != nil {
		return fmt.Errorf("nodes.CreateACMEPlugin: %w", err)
	}
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
	if err := applyPluginData(body, update.Data); err != nil {
		return fmt.Errorf("nodes.UpdateACMEPlugin: %w", err)
	}
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
//
// A non-nil d that renders nothing is an error, not a no-op. A provider struct
// built from an environment variable that turned out to be unset is non-nil but
// empty, and PVE accepts the resulting credential-less plugin happily — the
// failure then surfaces days later as a certificate order that cannot answer its
// challenge. On an update it is worse: the caller believes a rotation happened
// while the old credentials stay live.
func applyPluginData(body url.Values, d ACMEPluginData) error {
	if d == nil {
		return nil
	}
	api := d.API()
	if api == "" {
		return fmt.Errorf("data: provider name: %w", svcutil.ErrMissingField)
	}
	encoded := encodePluginData(d)
	if encoded == "" {
		return fmt.Errorf("data: %s credentials are all empty: %w", api, svcutil.ErrMissingField)
	}
	body.Set("api", api)
	body.Set("data", encoded)
	return nil
}

// GetACMEChallengeSchema returns the challenge providers this node supports, each
// with the raw credential-field schema PVE publishes for it. Cluster-scoped.
//
// This is the authoritative source for a provider's id and field names: read it
// to build an [ACMERawPluginData] for a provider the SDK does not type, or to
// confirm a typed provider's fields against the node itself.
func (s *Service) GetACMEChallengeSchema(ctx context.Context) ([]ACMEChallengeSchemaEntry, error) {
	var out []ACMEChallengeSchemaEntry
	if err := s.c.DoRequest(ctx, http.MethodGet, acmeChallengeSchemaPath(), nil, &out); err != nil {
		return nil, fmt.Errorf("nodes.GetACMEChallengeSchema: %w", err)
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
	// GET bodies are not form-encoded, so options ride in the query string (the
	// storage.ListContent precedent). Built through url.Values so a second
	// option cannot introduce a "?" vs "&" bug.
	query := url.Values{}
	if cfg.directory != "" {
		query.Set("directory", cfg.directory)
	}
	if enc := query.Encode(); enc != "" {
		path += "?" + enc
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
