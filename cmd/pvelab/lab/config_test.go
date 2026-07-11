package lab

import (
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
