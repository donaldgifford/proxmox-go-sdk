package lab

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v4"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
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

// Config is the pvelab YAML schema (settings only — anything secret is an
// env-var NAME resolved at runtime, never a value in the file).
type Config struct {
	Outer  Outer  `yaml:"outer"`
	Nested Nested `yaml:"nested"`
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
	Nodes           []Node `yaml:"nodes"`
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
	errs = append(errs, c.validateEnvRefs()...)
	return errors.Join(errs...)
}

func (c *Config) validateNodes() []error {
	var errs []error
	if len(c.Nested.Nodes) < 3 {
		errs = append(errs, fmt.Errorf("nested.nodes needs at least 3 nodes for quorum, got %d", len(c.Nested.Nodes)))
	}
	names := make(map[string]bool, len(c.Nested.Nodes))
	vmids := make(map[int]bool, len(c.Nested.Nodes))
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
