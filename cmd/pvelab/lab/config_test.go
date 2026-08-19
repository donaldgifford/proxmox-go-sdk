package lab

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// validYAML is a minimal complete config; tests mutate it per case.
const validYAML = `
outer:
  endpoint: https://outer.example:8006
  node: r740a
  token_id_env: PVELAB_TEST_TOKEN_ID
  token_secret_env: PVELAB_TEST_TOKEN_SECRET
  insecure_tls: true
  storage: local-zfs
  iso_storage: local
  bridge: vmbr0
  ssh:
    user: root
    known_hosts: ~/.ssh/known_hosts
    key_file: ~/.ssh/id_ed25519
nested:
  pve_version: "9.2"
  base_iso: /var/lib/vz/template/iso/proxmox-ve_9.2-1.iso
  cluster_name: dogfood
  domain: lab.example
  root_password_env: PVELAB_TEST_ROOT_PW
  gateway: 192.0.2.1
  dns: 192.0.2.1
  answer_url: http://192.0.2.10:8442
  nodes:
    - { name: pve1-dogfood, vmid: 9201, cidr: 192.0.2.201/24 }
    - { name: pve2-dogfood, vmid: 9202, cidr: 192.0.2.202/24 }
    - { name: pve3-dogfood, vmid: 9203, cidr: 192.0.2.203/24 }
`

// setTestEnv provides every env var validYAML references.
func setTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("PVELAB_TEST_TOKEN_ID", "root@pam!lab")
	t.Setenv("PVELAB_TEST_TOKEN_SECRET", "secret")
	t.Setenv("PVELAB_TEST_ROOT_PW", "throwaway")
}

// loadYAML writes the YAML to a temp file and runs Load.
func loadYAML(t *testing.T, doc string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pvelab.yaml")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return LoadConfig(path)
}

func TestLoadValid(t *testing.T) {
	setTestEnv(t)
	cfg, err := loadYAML(t, validYAML)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Outer.Endpoint != "https://outer.example:8006" {
		t.Errorf("Endpoint = %q", cfg.Outer.Endpoint)
	}
	if len(cfg.Nested.Nodes) != 3 {
		t.Fatalf("nodes = %d, want 3", len(cfg.Nested.Nodes))
	}
	// Defaults applied.
	if cfg.Nested.Cores != defaultCores || cfg.Nested.MemoryMB != defaultMemoryMB || cfg.Nested.DiskGB != defaultDiskGB {
		t.Errorf("sizing defaults not applied: %+v", cfg.Nested)
	}
	if cfg.Nested.AnswerListen != defaultAnswerListen {
		t.Errorf("AnswerListen = %q, want %q", cfg.Nested.AnswerListen, defaultAnswerListen)
	}
}

// TestLoadTemplateBlock accepts a well-formed optional template block and
// leaves it nil when absent.
func TestLoadTemplateBlock(t *testing.T) {
	setTestEnv(t)
	cfg, err := loadYAML(t, validYAML)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Nested.Template != nil {
		t.Errorf("Template = %+v, want nil when absent", cfg.Nested.Template)
	}

	doc := strings.Replace(validYAML, "  nodes:",
		"  template: { vmid: 9210, cidr: 192.0.2.210/24 }\n  nodes:", 1)
	cfg, err = loadYAML(t, doc)
	if err != nil {
		t.Fatalf("Load with template: %v", err)
	}
	if cfg.Nested.Template == nil || cfg.Nested.Template.VMID != 9210 || cfg.Nested.Template.CIDR != "192.0.2.210/24" {
		t.Errorf("Template = %+v, want vmid 9210 cidr 192.0.2.210/24", cfg.Nested.Template)
	}
}

func TestNodeHelpers(t *testing.T) {
	n := Node{Name: "pve1-dogfood", VMID: 9201, CIDR: "192.0.2.201/24"}
	if got := n.FQDN("lab.example"); got != "pve1-dogfood.lab.example" {
		t.Errorf("FQDN = %q", got)
	}
	ip, err := n.IP()
	if err != nil || ip != "192.0.2.201" {
		t.Errorf("IP = %q, %v", ip, err)
	}
	if _, err := (Node{Name: "x", CIDR: "not-a-cidr"}).IP(); err == nil {
		t.Error("IP on bad cidr = nil error, want error")
	}
}

func TestLoadRejects(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(doc string) string
		wantErr string
	}{
		{
			name:    "missing endpoint",
			mutate:  func(d string) string { return strings.Replace(d, "endpoint: https://outer.example:8006", "", 1) },
			wantErr: "outer.endpoint is required",
		},
		{
			name:    "missing node",
			mutate:  func(d string) string { return strings.Replace(d, "  node: r740a\n", "", 1) },
			wantErr: "outer.node is required",
		},
		{
			name:    "unknown key",
			mutate:  func(d string) string { return d + "\nsurprise: true\n" },
			wantErr: "field surprise not found",
		},
		{
			name: "two nodes only",
			mutate: func(d string) string {
				return strings.Replace(d, "    - { name: pve3-dogfood, vmid: 9203, cidr: 192.0.2.203/24 }\n", "", 1)
			},
			wantErr: "at least 3 nodes",
		},
		{
			name:    "duplicate vmid",
			mutate:  func(d string) string { return strings.Replace(d, "vmid: 9202", "vmid: 9201", 1) },
			wantErr: "duplicate vmid 9201",
		},
		{
			name:    "duplicate name",
			mutate:  func(d string) string { return strings.Replace(d, "name: pve2-dogfood", "name: pve1-dogfood", 1) },
			wantErr: `duplicate node name "pve1-dogfood"`,
		},
		{
			name:    "duplicate cidr",
			mutate:  func(d string) string { return strings.Replace(d, "192.0.2.202/24", "192.0.2.201/24", 1) },
			wantErr: `duplicate cidr "192.0.2.201/24"`,
		},
		{
			name:    "vmid outside reserved block",
			mutate:  func(d string) string { return strings.Replace(d, "vmid: 9203", "vmid: 100", 1) },
			wantErr: "outside the reserved pvelab block 9200-9399",
		},
		{
			name:    "bad node cidr",
			mutate:  func(d string) string { return strings.Replace(d, "192.0.2.203/24", "banana", 1) },
			wantErr: "node pve3-dogfood: cidr",
		},
		{
			name:    "bad gateway",
			mutate:  func(d string) string { return strings.Replace(d, "gateway: 192.0.2.1", "gateway: nope", 1) },
			wantErr: "nested.gateway",
		},
		{
			name: "no ssh auth",
			mutate: func(d string) string {
				return strings.Replace(d, "    key_file: ~/.ssh/id_ed25519\n", "", 1)
			},
			wantErr: "outer.ssh needs key_file and/or password_env",
		},
		{
			name:    "missing answer_url",
			mutate:  func(d string) string { return strings.Replace(d, "answer_url: http://192.0.2.10:8442", "", 1) },
			wantErr: "nested.answer_url is required",
		},
		{
			name:    "missing domain",
			mutate:  func(d string) string { return strings.Replace(d, "domain: lab.example", "", 1) },
			wantErr: "nested.domain is required",
		},
		{
			name: "template vmid outside sub-range",
			mutate: func(d string) string {
				return strings.Replace(d, "  nodes:", "  template: { vmid: 9299, cidr: 192.0.2.210/24 }\n  nodes:", 1)
			},
			wantErr: "outside the reserved template sub-range 9210-9219",
		},
		{
			name: "template cidr collides with node",
			mutate: func(d string) string {
				return strings.Replace(d, "  nodes:", "  template: { vmid: 9210, cidr: 192.0.2.201/24 }\n  nodes:", 1)
			},
			wantErr: `nested.template.cidr "192.0.2.201/24" collides with node pve1-dogfood`,
		},
		{
			name: "template cidr unparseable",
			mutate: func(d string) string {
				return strings.Replace(d, "  nodes:", "  template: { vmid: 9210, cidr: banana }\n  nodes:", 1)
			},
			wantErr: "nested.template.cidr",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setTestEnv(t)
			_, err := loadYAML(t, tt.mutate(validYAML))
			if err == nil {
				t.Fatalf("Load = nil error, want %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Load error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// TestExampleConfigValid pins the committed pvelab.example.yaml to the
// schema: if a config field changes, the example must change with it.
func TestExampleConfigValid(t *testing.T) {
	t.Setenv("PVE_TOKEN_ID", "root@pam!lab")
	t.Setenv("PVE_TOKEN_SECRET", "secret")
	t.Setenv("PVELAB_ROOT_PW", "throwaway")
	cfg, err := LoadConfig("../../../pvelab.example.yaml")
	if err != nil {
		t.Fatalf("pvelab.example.yaml does not validate: %v", err)
	}
	if len(cfg.Nested.Nodes) != 3 || cfg.Outer.Node == "" {
		t.Errorf("example config shape unexpected: %+v", cfg)
	}
}

func TestOuterHost(t *testing.T) {
	tests := []struct {
		endpoint string
		want     string
		wantErr  bool
	}{
		{endpoint: "https://outer.example:8006", want: "outer.example"},
		{endpoint: "outer.example:8006", want: "outer.example"},
		{endpoint: "outer.example", want: "outer.example"},
		{endpoint: "https://", wantErr: true},
	}
	for _, tt := range tests {
		c := &Config{Outer: Outer{Endpoint: tt.endpoint}}
		got, err := c.OuterHost()
		if tt.wantErr {
			if err == nil {
				t.Errorf("OuterHost(%q) = %q, want error", tt.endpoint, got)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("OuterHost(%q) = %q, %v; want %q", tt.endpoint, got, err, tt.want)
		}
	}
}

func TestOuterCredentials(t *testing.T) {
	c := &Config{Outer: Outer{TokenIDEnv: "PVELAB_TEST_TOKEN_ID", TokenSecretEnv: "PVELAB_TEST_TOKEN_SECRET"}}

	t.Setenv("PVELAB_TEST_TOKEN_ID", "root@pam!lab")
	t.Setenv("PVELAB_TEST_TOKEN_SECRET", "")
	if _, err := c.OuterCredentials(); err == nil {
		t.Error("OuterCredentials with empty secret = nil error, want error")
	}

	t.Setenv("PVELAB_TEST_TOKEN_SECRET", "secret")
	if _, err := c.OuterCredentials(); err != nil {
		t.Errorf("OuterCredentials = %v, want nil", err)
	}
}

func TestSSHOptions(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "id_test")
	if err := os.WriteFile(keyPath, []byte("fake-pem"), 0o600); err != nil {
		t.Fatal(err)
	}

	c := &Config{Outer: Outer{SSH: OuterSSH{User: "root", KnownHosts: "/tmp/kh", KeyFile: keyPath}}}
	opts, err := c.SSHOptions()
	if err != nil {
		t.Fatalf("SSHOptions: %v", err)
	}
	// user + known-hosts + key.
	if len(opts) != 3 {
		t.Errorf("options = %d, want 3", len(opts))
	}

	c.Outer.SSH.KeyFile = filepath.Join(t.TempDir(), "missing")
	if _, err := c.SSHOptions(); err == nil || !strings.Contains(err.Error(), "outer.ssh.key_file") {
		t.Errorf("SSHOptions with missing key = %v, want read error", err)
	}
}

// TestLoadMissingEnvVar covers the env-presence check separately since it
// depends on process state, not the document.
func TestLoadMissingEnvVar(t *testing.T) {
	setTestEnv(t)
	t.Setenv("PVELAB_TEST_ROOT_PW", "")
	_, err := loadYAML(t, validYAML)
	if err == nil || !strings.Contains(err.Error(), "PVELAB_TEST_ROOT_PW (referenced by config) is not set") {
		t.Errorf("Load = %v, want missing-env error", err)
	}
}

// TestLoadSSHPasswordEnvChecked verifies an ssh password env ref joins the
// presence check when configured.
func TestLoadSSHPasswordEnvChecked(t *testing.T) {
	setTestEnv(t)
	doc := strings.Replace(validYAML,
		"    key_file: ~/.ssh/id_ed25519",
		"    password_env: PVELAB_TEST_SSH_PW", 1)
	_, err := loadYAML(t, doc)
	if err == nil || !strings.Contains(err.Error(), "PVELAB_TEST_SSH_PW (referenced by config) is not set") {
		t.Errorf("Load = %v, want missing ssh password env error", err)
	}
	t.Setenv("PVELAB_TEST_SSH_PW", "hunter2")
	if _, err := loadYAML(t, doc); err != nil {
		t.Errorf("Load with ssh password env set = %v, want nil", err)
	}
}

// exampleEnv sets the variables the committed example configs reference. The
// values are throwaway: LoadConfig only checks that the names resolve.
func exampleEnv(t *testing.T) {
	t.Helper()
	t.Setenv("PVE_TOKEN_ID", "root@pam!lab")
	t.Setenv("PVE_TOKEN_SECRET", "secret")
	t.Setenv("PVELAB_ROOT_PW", "throwaway")
	t.Setenv("PVELAB_ACME_CF_TOKEN", "throwaway-token")
	t.Setenv("PVELAB_ACME_CF_ACCOUNT_ID", "throwaway-account")
}

// TestExampleACMEConfigValid pins pvelab-acme.example.yaml to the schema, the
// same way TestExampleConfigValid pins the plain one. Two configs are the point
// of the pair — the plain lab must keep validating with no acme block at all,
// which is what makes a failure attributable to the cluster or to the
// certificate path rather than to "the lab".
func TestExampleACMEConfigValid(t *testing.T) {
	exampleEnv(t)
	cfg, err := LoadConfig("../../../pvelab-acme.example.yaml")
	if err != nil {
		t.Fatalf("pvelab-acme.example.yaml does not validate: %v", err)
	}
	a := cfg.Nested.ACME
	if a == nil {
		t.Fatal("example has no nested.acme block")
	}
	if a.Directory != DirectoryStaging {
		t.Errorf("directory = %q, want the committed example to stay on staging", a.Directory)
	}
	if a.DirectoryURL() != stagingDirectoryURL {
		t.Errorf("DirectoryURL = %q, want staging", a.DirectoryURL())
	}
	if a.Account != "pvelab-staging" || a.PluginID != "pvelab-cf" {
		t.Errorf("defaults not applied: account=%q plugin=%q", a.Account, a.PluginID)
	}
	if len(cfg.Nested.Nodes) != 3 {
		t.Errorf("node count = %d, want the same 3-node lab as the plain example", len(cfg.Nested.Nodes))
	}
}

// TestPlainExampleHasNoACME is the other half of the pair: the baseline config
// must stay free of the ACME block, or the "provision without certificates
// first" bisect step quietly stops being a different test.
func TestPlainExampleHasNoACME(t *testing.T) {
	exampleEnv(t)
	cfg, err := LoadConfig("../../../pvelab.example.yaml")
	if err != nil {
		t.Fatalf("pvelab.example.yaml does not validate: %v", err)
	}
	if cfg.Nested.ACME != nil {
		t.Error("the baseline example grew an acme block; it must stay the no-CA path")
	}
}

// validACMEYAML is the block appended to validYAML by the cases below.
const validACMEYAML = `  acme:
    contact: ops@lab.example
    provider: cf
    credentials:
      CF_Token: PVELAB_TEST_CF_TOKEN
`

func TestACMEValidation(t *testing.T) {
	tests := []struct {
		name    string
		acme    string
		wantErr string
	}{
		{name: "minimal block is valid", acme: validACMEYAML},
		{
			name: "standalone is refused with the reason",
			acme: validACMEYAML + "    challenge: standalone\n",
			// The message has to say why, not just "invalid": http-01 is a
			// perfectly good challenge, just not reachable in this lab.
			wantErr: "port 80",
		},
		{
			name:    "unknown challenge",
			acme:    validACMEYAML + "    challenge: tls-alpn-01\n",
			wantErr: `want "dns-01"`,
		},
		{
			name:    "unknown directory",
			acme:    validACMEYAML + "    directory: letsencrypt\n",
			wantErr: "nested.acme.directory",
		},
		{
			name: "contact required",
			acme: "  acme:\n    provider: cf\n    credentials:\n" +
				"      CF_Token: PVELAB_TEST_CF_TOKEN\n",
			wantErr: "contact is required",
		},
		{
			name:    "provider required",
			acme:    "  acme:\n    contact: ops@lab.example\n    credentials:\n      CF_Token: PVELAB_TEST_CF_TOKEN\n",
			wantErr: "nested.acme.provider is required",
		},
		{
			name:    "credentials required",
			acme:    "  acme:\n    contact: ops@lab.example\n    provider: cf\n",
			wantErr: "nested.acme.credentials is required",
		},
		{
			name:    "referenced env var must be set",
			acme:    "  acme:\n    contact: ops@lab.example\n    provider: cf\n    credentials:\n      CF_Token: PVELAB_TEST_CF_UNSET\n",
			wantErr: "PVELAB_TEST_CF_UNSET",
		},
		{
			name:    "unknown key is rejected by strict decoding",
			acme:    validACMEYAML + "    dns_vendor: cloudflare\n",
			wantErr: "dns_vendor",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setTestEnv(t)
			t.Setenv("PVELAB_TEST_CF_TOKEN", "throwaway-token")
			_, err := loadYAML(t, strings.Replace(validYAML, "  nodes:", tt.acme+"  nodes:", 1))
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("LoadConfig() = %v, want nil", err)
			case tt.wantErr != "" && err == nil:
				t.Fatalf("LoadConfig() = nil, want an error mentioning %q", tt.wantErr)
			case tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr):
				t.Errorf("LoadConfig() = %v, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}

// TestACMEPluginDataIsProviderAgnostic is the point of the generic credential
// map: an unrelated provider the config has never heard of round-trips to the
// SDK with no code change here.
func TestACMEPluginDataIsProviderAgnostic(t *testing.T) {
	t.Setenv("PVELAB_TEST_R53_KEY", "key-value")
	t.Setenv("PVELAB_TEST_R53_SECRET", "secret-value")
	a := &ACMESpec{
		Provider: "aws",
		Credentials: map[string]string{
			"AWS_ACCESS_KEY_ID":     "PVELAB_TEST_R53_KEY",
			"AWS_SECRET_ACCESS_KEY": "PVELAB_TEST_R53_SECRET",
		},
	}
	data, err := a.PluginData()
	if err != nil {
		t.Fatalf("PluginData: %v", err)
	}
	if data.API() != "aws" {
		t.Errorf("API() = %q, want aws", data.API())
	}
	want := map[string]string{"AWS_ACCESS_KEY_ID": "key-value", "AWS_SECRET_ACCESS_KEY": "secret-value"}
	if got := data.Data(); !maps.Equal(got, want) {
		t.Errorf("Data() = %v, want %v", got, want)
	}
	// The credentials must not be recoverable from a formatted value: pvelab
	// logs config structs, and this one now carries live secrets.
	if s := fmt.Sprintf("%v", data); strings.Contains(s, "key-value") || strings.Contains(s, "secret-value") {
		t.Errorf("plugin data leaked a credential under %%v: %s", s)
	}
}

// TestACMEPluginDataMissingEnv names every unset variable at once rather than
// failing on the first: an operator fixing a .pvelab.env wants the whole list.
func TestACMEPluginDataMissingEnv(t *testing.T) {
	a := &ACMESpec{
		Provider:    "cf",
		Credentials: map[string]string{"CF_Token": "PVELAB_TEST_UNSET_A", "CF_Zone_ID": "PVELAB_TEST_UNSET_B"},
	}
	_, err := a.PluginData()
	if err == nil {
		t.Fatal("PluginData() = nil error, want one naming the unset variables")
	}
	for _, want := range []string{"PVELAB_TEST_UNSET_A", "PVELAB_TEST_UNSET_B"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %v does not mention %s", err, want)
		}
	}
}

// TestACMEDirectoryURL pins the mapping, including that an empty directory
// resolves to staging rather than to the empty string.
func TestACMEDirectoryURL(t *testing.T) {
	for _, tt := range []struct{ dir, want string }{
		{DirectoryStaging, stagingDirectoryURL},
		{DirectoryProduction, productionDirectoryURL},
		{"", stagingDirectoryURL},
	} {
		if got := (&ACMESpec{Directory: tt.dir}).DirectoryURL(); got != tt.want {
			t.Errorf("DirectoryURL(%q) = %q, want %q", tt.dir, got, tt.want)
		}
	}
}

// TestHandoffPathsDeriveFromConfig pins the pairing: each config gets its own
// handoff files, and the default config keeps the names it has always had, so
// an existing lab is not orphaned by this becoming configurable.
func TestHandoffPathsDeriveFromConfig(t *testing.T) {
	tests := []struct{ config, env, state string }{
		{"pvelab.yaml", ".pvelab.env", ".pvelab-state.json"},
		{"pvelab-acme.yaml", ".pvelab-acme.env", ".pvelab-acme-state.json"},
		{"/somewhere/else/pvelab-9.1.yml", ".pvelab-9.1.env", ".pvelab-9.1-state.json"},
	}
	for _, tt := range tests {
		var c Config
		c.resolveHandoffPaths(tt.config)
		if c.EnvPath != tt.env || c.StatePath != tt.state {
			t.Errorf("%s -> env %q state %q, want %q and %q",
				tt.config, c.EnvPath, c.StatePath, tt.env, tt.state)
		}
	}
}

// TestHandoffPathsExplicitWins covers the escape hatch: an operator who wants
// a specific filename is not overridden by the derivation.
func TestHandoffPathsExplicitWins(t *testing.T) {
	c := Config{EnvPath: "/tmp/custom.env", StatePath: "/tmp/custom.json"}
	c.resolveHandoffPaths("pvelab-acme.yaml")
	if c.EnvPath != "/tmp/custom.env" || c.StatePath != "/tmp/custom.json" {
		t.Errorf("explicit paths overwritten: env=%q state=%q", c.EnvPath, c.StatePath)
	}
}

// TestLoadResolvesHandoffPaths proves the resolution happens during load, so
// every command reads it off the config rather than each deciding for itself.
func TestLoadResolvesHandoffPaths(t *testing.T) {
	setTestEnv(t)
	path := filepath.Join(t.TempDir(), "pvelab-acme.yaml")
	if err := os.WriteFile(path, []byte(validYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.EnvPath != ".pvelab-acme.env" || cfg.StatePath != ".pvelab-acme-state.json" {
		t.Errorf("env=%q state=%q, want the acme-derived pair", cfg.EnvPath, cfg.StatePath)
	}
}
