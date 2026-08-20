package lab

import (
	"errors"
	"fmt"
	"maps"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v4"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/nodes"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/ssh"
)

// The reserved pvelab VMID block (DESIGN-0002 OQ-10: nodes 9201–9203, test
// scratch 93xx). Config validation refuses node VMIDs outside it, so no lab
// operation — provision or teardown — can ever reach a real guest on the
// outer host (the Phase 0 blast-radius guard, enforced at the front door).
const (
	vmidRangeLo = 9200
	vmidRangeHi = 9399
)

// The template sub-range inside the reserved block (IMPL-0002 Phase 5): one
// template per PVE minor, so the version matrix can keep several on the outer
// host at once. Each per-version config file picks a distinct VMID in here.
const (
	templateVMIDLo = 9210
	templateVMIDHi = 9219
)

// Sizing defaults applied when the nested block leaves them zero (the Phase 0
// spike values).
const (
	defaultCores    = 4
	defaultMemoryMB = 8192
	defaultDiskGB   = 32

	// defaultAnswerListen is the answer server's default bind address; the
	// routable URL the installer calls back on is always explicit config
	// (nested.answer_url) because the workstation's reachable address cannot
	// be derived reliably.
	defaultAnswerListen = ":8442"
)

// ACME challenge kinds. Only dns-01 is implementable here: the nested nodes
// sit on the lab's private addressing, and a standalone (http-01) challenge
// requires the CA to reach the node on port 80 from the internet. The constant
// exists so the config rejects it by name with that reason, rather than by
// looking like a typo.
const (
	ChallengeDNS01      = "dns-01"
	ChallengeStandalone = "standalone"
)

// ACME directories. Staging is the default and has to be: a lab that
// re-provisions in a loop would burn Let's Encrypt's production rate limits
// (50 certificates per registered domain per week) in an afternoon, and a
// config typo must not be what discovers that.
const (
	DirectoryStaging    = "staging"
	DirectoryProduction = "production"

	stagingDirectoryURL    = "https://acme-staging-v02.api.letsencrypt.org/directory"
	productionDirectoryURL = "https://acme-v02.api.letsencrypt.org/directory"
)

// Config is the pvelab YAML schema (settings only — anything secret is an
// env-var NAME resolved at runtime, never a value in the file).
type Config struct {
	Outer  Outer  `yaml:"outer"`
	Nested Nested `yaml:"nested"`
	// EnvPath and StatePath are where `up` writes its handoff files and
	// `down` removes them. Both default to the config's own basename
	// (pvelab.yaml -> .pvelab.env and .pvelab-state.json; pvelab-acme.yaml ->
	// .pvelab-acme.env and .pvelab-acme-state.json), which is what keeps two
	// labs from overwriting each other's answer to "what is currently up?".
	//
	// They were fixed names until the ACME variant arrived and made a second
	// config normal. A shared state file is the worse half of that: it records
	// what `up` created, so the wrong one silently describes the other lab.
	EnvPath   string `yaml:"env_path"`
	StatePath string `yaml:"state_path"`
}

// resolveHandoffPaths fills EnvPath/StatePath from the config's filename when
// the operator has not set them. Deriving from the config rather than from a
// flag means the pairing cannot be got wrong by forgetting an argument, and
// the default config keeps the names it has always had.
func (c *Config) resolveHandoffPaths(configPath string) {
	base := filepath.Base(configPath)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	if base == "" || base == "." {
		base = "pvelab"
	}
	if c.EnvPath == "" {
		c.EnvPath = "." + base + ".env"
	}
	if c.StatePath == "" {
		c.StatePath = "." + base + "-state.json"
	}
}

// Outer locates and authenticates the physical PVE host the lab runs on.
type Outer struct {
	Endpoint string `yaml:"endpoint"`
	// Node is the outer host's PVE node name (e.g. "r740a") — required by
	// every node-scoped SDK call (storage content listing, VM create). Not in
	// DESIGN-0002's sample (a gap found wiring `pvelab iso`; the Phase 0 spike
	// hardcoded it).
	Node           string   `yaml:"node"`
	TokenIDEnv     string   `yaml:"token_id_env"`
	TokenSecretEnv string   `yaml:"token_secret_env"`
	InsecureTLS    bool     `yaml:"insecure_tls"`
	Storage        string   `yaml:"storage"`     // node VM disks.
	ISOStorage     string   `yaml:"iso_storage"` // prepared installer ISOs.
	Bridge         string   `yaml:"bridge"`
	SSH            OuterSSH `yaml:"ssh"`
}

// OuterSSH configures the proxmox/ssh side-channel `pvelab iso` uses on the
// outer host. Host-key verification is mandatory (known_hosts); auth is a key
// file and/or a password env var — at least one, key preferred (IQ-3 = a).
type OuterSSH struct {
	User        string `yaml:"user"`
	KnownHosts  string `yaml:"known_hosts"`
	KeyFile     string `yaml:"key_file"`
	PasswordEnv string `yaml:"password_env"`
}

// Nested describes the lab topology installed inside the outer host.
type Nested struct {
	PVEVersion      string `yaml:"pve_version"` // selects the ISO (later: template).
	BaseISO         string `yaml:"base_iso"`    // path on the outer host.
	ClusterName     string `yaml:"cluster_name"`
	Domain          string `yaml:"domain"` // fqdn = <node name>.<domain>; the hostname part IS the PVE node name.
	RootPasswordEnv string `yaml:"root_password_env"`
	Gateway         string `yaml:"gateway"`
	DNS             string `yaml:"dns"`
	Cores           int    `yaml:"cores"`
	MemoryMB        int    `yaml:"memory_mb"`
	DiskGB          int    `yaml:"disk_gb"`
	AnswerURL       string `yaml:"answer_url"`    // baked into the http-mode ISO; must be reachable from the nested VMs.
	AnswerListen    string `yaml:"answer_listen"` // answer server bind address (default ":8442").
	// Template reserves the VMID/CIDR `pvelab template build` uses for this
	// version's template (IMPL-0002 Phase 5). Optional: without it `template
	// build` refuses to run and `up` always takes the ISO-install path.
	Template *TemplateSpec `yaml:"template,omitempty"`
	// ACME requests a real TLS certificate for every node after the cluster
	// forms. Optional, and absent is the original behaviour: the lab installs
	// with PVE's self-signed certificate and never talks to a CA. Keeping the
	// two paths separately configurable is what makes a failed run
	// attributable — a cluster that will not form is a different bug from a
	// certificate that will not issue, and one config per shape says which was
	// under test.
	ACME  *ACMESpec `yaml:"acme,omitempty"`
	Nodes []Node    `yaml:"nodes"`
}

// ACMESpec configures certificate issuance for the nested cluster. Every node
// gets its own certificate for its own FQDN (<node name>.<nested domain>),
// which is why no domain is configured here: the DNS records the lab needs
// already have to match the FQDNs the answer files install, so a second place
// to spell the domain could only ever disagree with the first.
//
// Providers are data, not code. Provider is acme.sh's plugin name — whatever
// PVE's api parameter accepts, 160 of them on 9.2 — and Credentials maps that
// provider's acme.sh variable names to the env vars holding their values. A
// new provider is therefore a config change and nothing else; pvelab hands the
// pair to the SDK's [nodes.ACMEPluginData] interface via [nodes.ACMERawPluginData]
// and never learns what a Cloudflare token is. Ask the node itself for a
// provider's field names with Service.GetACMEChallengeSchema rather than
// guessing them.
//
// The values are live provider credentials: pvelab passes them straight to the
// SDK, never logs them, and the committed example names variables that do not
// exist.
type ACMESpec struct {
	// Directory selects the CA endpoint: "staging" (default) or "production".
	Directory string `yaml:"directory"`
	// Account is the PVE ACME account name. It defaults per directory
	// ("pvelab-staging" / "pvelab-production") because an account is
	// registered against one CA: reusing a staging account's name against
	// production would hand PVE a key the production CA has never seen.
	Account string `yaml:"account"`
	// Contact is the registration email. Not a secret, and it lives in the
	// git-ignored config beside the domain and the addresses, but the recorder
	// scrubs it out of any cassette.
	Contact string `yaml:"contact"`
	// Challenge is the challenge kind — "dns-01" (default).
	Challenge string `yaml:"challenge"`
	// Provider is the acme.sh plugin name PVE stores in the plugin's api
	// field, e.g. "cf".
	Provider string `yaml:"provider"`
	// PluginID is the PVE ACME plugin id to create and reference. It defaults
	// to "pvelab-<provider>". Cluster-scoped on the nested cluster, so it is
	// created once and every node's config points at it.
	PluginID string `yaml:"plugin_id"`
	// Credentials maps acme.sh variable name -> env var NAME holding its
	// value, e.g. {CF_Token: PVELAB_ACME_CF_TOKEN}.
	Credentials map[string]string `yaml:"credentials"`
}

// PluginData resolves the configured env vars and returns the SDK plugin data
// for this provider. It reads the environment on every call and retains
// nothing, so a caller decides how long the credentials live.
//
// A named-but-unset variable is an error rather than an omitted field: config
// load already refuses that case, and silently registering a plugin with a
// missing credential would fail minutes later inside a CA exchange, where the
// cause is much harder to see.
func (a *ACMESpec) PluginData() (nodes.ACMEPluginData, error) {
	values := make(map[string]string, len(a.Credentials))
	var missing []string
	for _, key := range slices.Sorted(maps.Keys(a.Credentials)) {
		env := a.Credentials[key]
		v := os.Getenv(env)
		if v == "" {
			missing = append(missing, fmt.Sprintf("%s (env %s)", key, env))
			continue
		}
		values[key] = v
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("acme credentials unset: %s", strings.Join(missing, ", "))
	}
	return nodes.ACMERawPluginData{Provider: a.Provider, Values: values}, nil
}

// DirectoryURL resolves the configured directory keyword to its CA URL.
func (a *ACMESpec) DirectoryURL() string {
	if a.Directory == DirectoryProduction {
		return productionDirectoryURL
	}
	return stagingDirectoryURL
}

// TemplateSpec reserves the outer-host VMID (in the 9210–9219 template
// sub-range) and install-time CIDR for this version's nested-PVE template. The
// template's NAME is not configured — it is always pvelab-tmpl-<pve_version>,
// computed, so it cannot drift from the version it represents.
type TemplateSpec struct {
	VMID int    `yaml:"vmid"`
	CIDR string `yaml:"cidr"`
}

// Node is one nested PVE node. Name is used verbatim as the hostname (and so
// as the PVE node name — the Phase 0 spike's convention, e.g. "pve1-dogfood").
type Node struct {
	Name string `yaml:"name"`
	VMID int    `yaml:"vmid"`
	CIDR string `yaml:"cidr"`
}

// FQDN renders the node's answer-file fqdn under the lab domain.
func (n Node) FQDN(domain string) string { return n.Name + "." + domain }

// IP returns the address part of the node's CIDR.
func (n Node) IP() (string, error) {
	p, err := netip.ParsePrefix(n.CIDR)
	if err != nil {
		return "", fmt.Errorf("node %s: parse cidr %q: %w", n.Name, n.CIDR, err)
	}
	return p.Addr().String(), nil
}

// LoadConfig reads, strictly decodes (unknown keys are errors), defaults, and
// validates the config at path. Validation is fail-fast and includes the
// presence of every referenced env var, so a bad run dies before touching
// the outer host.
func LoadConfig(path string) (*Config, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- path is the operator's own -config flag.
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true)
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	cfg.resolveHandoffPaths(path)
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config %s: %w", path, err)
	}
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Nested.Cores == 0 {
		c.Nested.Cores = defaultCores
	}
	if c.Nested.MemoryMB == 0 {
		c.Nested.MemoryMB = defaultMemoryMB
	}
	if c.Nested.DiskGB == 0 {
		c.Nested.DiskGB = defaultDiskGB
	}
	if c.Nested.AnswerListen == "" {
		c.Nested.AnswerListen = defaultAnswerListen
	}
	c.Nested.ACME.applyDefaults()
}

// applyDefaults fills the ACME block's optional fields. The nil check lives
// here rather than at the call site because the block itself is optional.
func (a *ACMESpec) applyDefaults() {
	if a == nil {
		return
	}
	if a.Directory == "" {
		a.Directory = DirectoryStaging
	}
	if a.Challenge == "" {
		a.Challenge = ChallengeDNS01
	}
	if a.Account == "" {
		a.Account = "pvelab-" + a.Directory
	}
	if a.PluginID == "" && a.Provider != "" {
		a.PluginID = "pvelab-" + a.Provider
	}
}

// Validate enforces the schema contract: required fields, ≥3 unique nodes
// inside the reserved VMID block, parseable addresses, and every referenced
// env var present in the environment.
func (c *Config) Validate() error {
	var errs []error

	req := func(val, field string) {
		if val == "" {
			errs = append(errs, fmt.Errorf("%s is required", field))
		}
	}
	req(c.Outer.Endpoint, "outer.endpoint")
	req(c.Outer.Node, "outer.node")
	req(c.Outer.TokenIDEnv, "outer.token_id_env")
	req(c.Outer.TokenSecretEnv, "outer.token_secret_env")
	req(c.Outer.Storage, "outer.storage")
	req(c.Outer.ISOStorage, "outer.iso_storage")
	req(c.Outer.Bridge, "outer.bridge")
	req(c.Outer.SSH.User, "outer.ssh.user")
	req(c.Outer.SSH.KnownHosts, "outer.ssh.known_hosts")
	if c.Outer.SSH.KeyFile == "" && c.Outer.SSH.PasswordEnv == "" {
		errs = append(errs, errors.New("outer.ssh needs key_file and/or password_env (key preferred)"))
	}
	if c.Outer.Endpoint != "" {
		if _, err := c.OuterHost(); err != nil {
			errs = append(errs, err)
		}
	}

	req(c.Nested.PVEVersion, "nested.pve_version")
	req(c.Nested.BaseISO, "nested.base_iso")
	req(c.Nested.ClusterName, "nested.cluster_name")
	req(c.Nested.Domain, "nested.domain")
	req(c.Nested.RootPasswordEnv, "nested.root_password_env")
	req(c.Nested.AnswerURL, "nested.answer_url")

	for _, a := range []struct{ field, addr string }{
		{"nested.gateway", c.Nested.Gateway},
		{"nested.dns", c.Nested.DNS},
	} {
		field, addr := a.field, a.addr
		if addr == "" {
			errs = append(errs, fmt.Errorf("%s is required", field))
			continue
		}
		if _, err := netip.ParseAddr(addr); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", field, err))
		}
	}

	errs = append(errs, c.validateNodes()...)
	errs = append(errs, c.validateTemplate()...)
	errs = append(errs, c.validateACME()...)
	errs = append(errs, c.validateEnvRefs()...)
	return errors.Join(errs...)
}

// validateTemplate checks the optional nested.template block: VMID inside the
// template sub-range (so it can never collide with a node VMID), CIDR
// parseable and distinct from every node's.
func (c *Config) validateTemplate() []error {
	t := c.Nested.Template
	if t == nil {
		return nil
	}
	var errs []error
	if t.VMID < templateVMIDLo || t.VMID > templateVMIDHi {
		errs = append(errs, fmt.Errorf("nested.template.vmid %d outside the reserved template sub-range %d-%d",
			t.VMID, templateVMIDLo, templateVMIDHi))
	}
	if _, err := netip.ParsePrefix(t.CIDR); err != nil {
		errs = append(errs, fmt.Errorf("nested.template.cidr: %w", err))
	}
	for _, n := range c.Nested.Nodes {
		if n.VMID == t.VMID {
			errs = append(errs, fmt.Errorf("nested.template.vmid %d collides with node %s", t.VMID, n.Name))
		}
		if n.CIDR == t.CIDR {
			errs = append(errs, fmt.Errorf("nested.template.cidr %q collides with node %s", t.CIDR, n.Name))
		}
	}
	return errs
}

// validateACME checks the optional nested.acme block. It deliberately knows
// nothing about individual providers — the credential keys are acme.sh's
// vocabulary, and the node's own challenge schema is the only authority on
// them — so it checks the two things that fail late and expensively: a
// challenge kind that cannot work here, and a credential the environment does
// not actually hold.
func (c *Config) validateACME() []error {
	a := c.Nested.ACME
	if a == nil {
		return nil
	}
	var errs []error
	if a.Contact == "" {
		errs = append(errs, errors.New("nested.acme.contact is required"))
	}
	if a.Provider == "" {
		errs = append(errs, errors.New("nested.acme.provider is required (an acme.sh plugin name, e.g. cf)"))
	}
	if len(a.Credentials) == 0 {
		errs = append(errs, errors.New("nested.acme.credentials is required (acme.sh variable name -> env var name)"))
	}
	for key, env := range a.Credentials {
		if env == "" {
			errs = append(errs, fmt.Errorf("nested.acme.credentials[%s] names no env var", key))
		}
	}
	switch a.Directory {
	case DirectoryStaging, DirectoryProduction:
	default:
		errs = append(errs, fmt.Errorf("nested.acme.directory %q: want %q or %q",
			a.Directory, DirectoryStaging, DirectoryProduction))
	}
	if a.Challenge == ChallengeStandalone {
		errs = append(errs, fmt.Errorf(
			"nested.acme.challenge %q needs the CA to reach the node on port 80; pvelab's nodes sit on the lab's private addressing, so only %q can work here",
			ChallengeStandalone,
			ChallengeDNS01,
		))
	} else if a.Challenge != ChallengeDNS01 {
		errs = append(errs, fmt.Errorf("nested.acme.challenge %q: want %q", a.Challenge, ChallengeDNS01))
	}
	return errs
}

// acmeEnvRefs lists the env vars the configured credentials read, so
// validateEnvRefs fails at load rather than mid-provision.
func (a *ACMESpec) acmeEnvRefs() []string {
	return slices.Sorted(maps.Values(a.Credentials))
}

func (c *Config) validateNodes() []error {
	var errs []error
	if len(c.Nested.Nodes) < 3 {
		errs = append(errs, fmt.Errorf("nested.nodes needs at least 3 nodes for quorum, got %d", len(c.Nested.Nodes)))
	}
	names := make(map[string]bool, len(c.Nested.Nodes))
	vmids := make(map[int]bool, len(c.Nested.Nodes))
	// The template sub-range is reserved whether or not THIS config declares a
	// template: it holds one template per PVE minor, so a node parked there
	// collides with a version this config has never heard of. Checking only
	// against the configured template VMID (validateTemplate) misses that —
	// the collision arrives later, from another config, as a template build
	// that cannot have its VMID.
	cidrs := make(map[string]bool, len(c.Nested.Nodes))
	for _, n := range c.Nested.Nodes {
		if n.Name == "" {
			errs = append(errs, errors.New("every node needs a name"))
			continue
		}
		if names[n.Name] {
			errs = append(errs, fmt.Errorf("duplicate node name %q", n.Name))
		}
		names[n.Name] = true

		if n.VMID < vmidRangeLo || n.VMID > vmidRangeHi {
			errs = append(errs, fmt.Errorf("node %s: vmid %d outside the reserved pvelab block %d-%d",
				n.Name, n.VMID, vmidRangeLo, vmidRangeHi))
		} else if n.VMID >= templateVMIDLo && n.VMID <= templateVMIDHi {
			errs = append(errs, fmt.Errorf("node %s: vmid %d is inside the template sub-range %d-%d; pick one outside it",
				n.Name, n.VMID, templateVMIDLo, templateVMIDHi))
		}
		if vmids[n.VMID] {
			errs = append(errs, fmt.Errorf("duplicate vmid %d", n.VMID))
		}
		vmids[n.VMID] = true

		if _, err := netip.ParsePrefix(n.CIDR); err != nil {
			errs = append(errs, fmt.Errorf("node %s: cidr: %w", n.Name, err))
		}
		if cidrs[n.CIDR] {
			errs = append(errs, fmt.Errorf("duplicate cidr %q", n.CIDR))
		}
		cidrs[n.CIDR] = true
	}
	return errs
}

// OuterHost extracts the hostname pvelab dials for SSH from outer.endpoint,
// which NewClient accepts as a bare host, host:port, or URL.
func (c *Config) OuterHost() (string, error) {
	e := c.Outer.Endpoint
	if strings.Contains(e, "://") {
		u, err := url.Parse(e)
		if err != nil {
			return "", fmt.Errorf("outer.endpoint: %w", err)
		}
		if u.Hostname() == "" {
			return "", fmt.Errorf("outer.endpoint %q has no host", e)
		}
		return u.Hostname(), nil
	}
	if host, _, err := net.SplitHostPort(e); err == nil {
		return host, nil
	}
	return e, nil
}

// OuterCredentials resolves the outer token env refs into api.Credentials.
func (c *Config) OuterCredentials() (api.Credentials, error) {
	id, secret := os.Getenv(c.Outer.TokenIDEnv), os.Getenv(c.Outer.TokenSecretEnv)
	if id == "" || secret == "" {
		return nil, fmt.Errorf("env vars %s and %s must both be set", c.Outer.TokenIDEnv, c.Outer.TokenSecretEnv)
	}
	return api.TokenCredentials(id, secret), nil
}

// SSHOptions builds the proxmox/ssh options from outer.ssh: user + mandatory
// known-hosts verification, then key auth (preferred) and/or a password from
// the environment.
func (c *Config) SSHOptions() ([]ssh.Option, error) {
	s := c.Outer.SSH
	opts := []ssh.Option{ssh.WithUser(s.User), ssh.WithKnownHostsFile(expandHome(s.KnownHosts))}
	if s.KeyFile != "" {
		pem, err := os.ReadFile(expandHome(s.KeyFile)) // #nosec G304 -- path is the operator's own config value.
		if err != nil {
			return nil, fmt.Errorf("read outer.ssh.key_file: %w", err)
		}
		opts = append(opts, ssh.WithPrivateKey(pem))
	}
	if s.PasswordEnv != "" {
		opts = append(opts, ssh.WithPassword(os.Getenv(s.PasswordEnv)))
	}
	return opts, nil
}

// expandHome resolves a leading "~/" — YAML values never see shell expansion.
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// validateEnvRefs checks that every env var the config names is actually set,
// so credential problems surface at load time rather than mid-provision.
func (c *Config) validateEnvRefs() []error {
	refs := []string{c.Outer.TokenIDEnv, c.Outer.TokenSecretEnv, c.Nested.RootPasswordEnv}
	if c.Outer.SSH.PasswordEnv != "" {
		refs = append(refs, c.Outer.SSH.PasswordEnv)
	}
	if c.Nested.ACME != nil {
		refs = append(refs, c.Nested.ACME.acmeEnvRefs()...)
	}
	var errs []error
	for _, name := range refs {
		if name == "" {
			continue // the missing-field error is already recorded.
		}
		if os.Getenv(name) == "" {
			errs = append(errs, fmt.Errorf("env var %s (referenced by config) is not set", name))
		}
	}
	return errs
}
