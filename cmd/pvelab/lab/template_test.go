package lab

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
)

// templateTestConfig is provisionTestConfig plus the template block.
func templateTestConfig() *Config {
	cfg := provisionTestConfig()
	cfg.Nested.Template = &TemplateSpec{VMID: 9210, CIDR: "192.0.2.210/24"}
	return cfg
}

// readyNow is the probe for template tests: the mock cannot run an install,
// so the synthetic node is "ready" on the first poll.
func readyNow(context.Context, string) error { return nil }

// buildTemplateForTest runs the injectable core with test cadence.
func buildTemplateForTest(t *testing.T, c *proxmox.Client, cfg *Config, force bool) error {
	t.Helper()
	return buildTemplate(context.Background(), c, cfg, force,
		readyNow, 10*time.Millisecond, time.Second, nil)
}

func TestTemplateNaming(t *testing.T) {
	cfg := templateTestConfig()
	if got := TemplateName(cfg); got != "pvelab-tmpl-9-2" {
		t.Errorf("TemplateName = %q, want pvelab-tmpl-9-2", got)
	}
	// The synthetic node name is a valid hostname label: no dots.
	if n := templateNodeName(cfg); strings.Contains(n, ".") {
		t.Errorf("templateNodeName = %q contains a dot — invalid hostname label", n)
	}
}

func TestTemplateConfig(t *testing.T) {
	cfg := templateTestConfig()
	tcfg, err := TemplateConfig(cfg)
	if err != nil {
		t.Fatalf("TemplateConfig: %v", err)
	}
	if len(tcfg.Nested.Nodes) != 1 {
		t.Fatalf("synthetic nodes = %d, want 1", len(tcfg.Nested.Nodes))
	}
	n := tcfg.Nested.Nodes[0]
	if n.VMID != 9210 || n.CIDR != "192.0.2.210/24" || n.Name != "tmpl-9-2" {
		t.Errorf("synthetic node = %+v", n)
	}
	// The original config is untouched (the copy is deep enough).
	if len(cfg.Nested.Nodes) != 3 {
		t.Errorf("original config mutated: nodes = %d", len(cfg.Nested.Nodes))
	}

	cfg.Nested.Template = nil
	if _, err := TemplateConfig(cfg); err == nil || !strings.Contains(err.Error(), "nested.template") {
		t.Errorf("TemplateConfig without block = %v, want config hint", err)
	}
}

// TestBuildTemplateHappyPath drives the full build against mockpve: install
// (probe-ready), shutdown, ISO detach, conversion — then FindTemplate sees it.
func TestBuildTemplateHappyPath(t *testing.T) {
	c, mock := newMockClient(t)
	cfg := templateTestConfig()
	mock.AddVolume("r740a", "local", PreparedISOVolid("local", "9.2"), "iso", "iso", 1<<30)

	if err := buildTemplateForTest(t, c, cfg, false); err != nil {
		t.Fatalf("buildTemplate: %v", err)
	}

	vm, found, err := FindTemplate(context.Background(), c, cfg)
	if err != nil || !found {
		t.Fatalf("FindTemplate after build = found %v, %v", found, err)
	}
	if !vm.Template.Bool() {
		t.Errorf("template flag not set on %q", vm.Name)
	}
	if int(vm.VMID) != 9210 {
		t.Errorf("template vmid = %d, want 9210", vm.VMID)
	}

	// The installer ISO is detached before conversion.
	vmCfg, err := c.QEMU("r740a").Config(context.Background(), 9210)
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	if got, ok := vmCfg.Extra["ide2"]; ok {
		t.Errorf("ide2 still attached after build: %q", got)
	}
}

// TestBuildTemplateExistsWithoutForce refuses to clobber an existing template.
func TestBuildTemplateExistsWithoutForce(t *testing.T) {
	c, mock := newMockClient(t)
	cfg := templateTestConfig()
	mock.AddVM("r740a", 9215, TemplateName(cfg), "stopped")

	err := buildTemplateForTest(t, c, cfg, false)
	if err == nil || !strings.Contains(err.Error(), "-force") {
		t.Errorf("buildTemplate over existing = %v, want -force hint", err)
	}
}

// TestBuildTemplateForceRebuild deletes the leftover (even a running,
// half-built one) and builds fresh at the configured VMID.
func TestBuildTemplateForceRebuild(t *testing.T) {
	c, mock := newMockClient(t)
	cfg := templateTestConfig()
	mock.AddVolume("r740a", "local", PreparedISOVolid("local", "9.2"), "iso", "iso", 1<<30)
	mock.AddVM("r740a", 9215, TemplateName(cfg), "running")

	if err := buildTemplateForTest(t, c, cfg, true); err != nil {
		t.Fatalf("buildTemplate -force: %v", err)
	}

	vm, found, err := FindTemplate(context.Background(), c, cfg)
	if err != nil || !found {
		t.Fatalf("FindTemplate after rebuild = found %v, %v", found, err)
	}
	if int(vm.VMID) != 9210 {
		t.Errorf("rebuilt template vmid = %d, want 9210", vm.VMID)
	}
	// The old squatter is gone.
	if _, err := c.QEMU("r740a").Get(context.Background(), 9215); err == nil {
		t.Error("old template VM 9215 still exists after -force rebuild")
	}
}

// TestBuildTemplateForeignVMIDRefused never touches a guest pvelab does not
// own, even under -force.
func TestBuildTemplateForeignVMIDRefused(t *testing.T) {
	c, mock := newMockClient(t)
	cfg := templateTestConfig()
	mock.AddVolume("r740a", "local", PreparedISOVolid("local", "9.2"), "iso", "iso", 1<<30)
	mock.AddVM("r740a", 9210, "production-db", "running")

	for _, force := range []bool{false, true} {
		err := buildTemplateForTest(t, c, cfg, force)
		if err == nil || !strings.Contains(err.Error(), "refusing to touch it") {
			t.Errorf("buildTemplate(force=%v) over foreign VM = %v, want refusal", force, err)
		}
	}
	if _, err := c.QEMU("r740a").Get(context.Background(), 9210); err != nil {
		t.Errorf("foreign VM 9210 gone after refused build: %v", err)
	}
}

// TestFindTemplateAbsent reports found=false on a clean node.
func TestFindTemplateAbsent(t *testing.T) {
	c, _ := newMockClient(t)
	_, found, err := FindTemplate(context.Background(), c, templateTestConfig())
	if err != nil || found {
		t.Errorf("FindTemplate on clean node = found %v, %v", found, err)
	}
}

func TestBuildTemplateWithoutBlock(t *testing.T) {
	c, _ := newMockClient(t)
	cfg := provisionTestConfig() // no template block.
	err := buildTemplateForTest(t, c, cfg, false)
	if err == nil || !strings.Contains(err.Error(), "nested.template") {
		t.Errorf("buildTemplate without block = %v, want config hint", err)
	}
}
