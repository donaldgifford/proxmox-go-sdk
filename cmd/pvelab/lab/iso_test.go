package lab

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
)

// fakeExec scripts the SSH side-channel: fn decides each command's outcome
// (nil fn = every command succeeds silently) and calls records the exact
// command lines.
type fakeExec struct {
	calls []string
	fn    func(cmd string) ([]byte, error)
}

func (f *fakeExec) Exec(_ context.Context, cmd string) ([]byte, error) {
	f.calls = append(f.calls, cmd)
	if f.fn == nil {
		return nil, nil
	}
	return f.fn(cmd)
}

// newMockClient wires a root SDK client to a fresh mockpve, returning both so
// tests can seed storage state mid-flow.
func newMockClient(t *testing.T) (*proxmox.Client, *mockpve.Server) {
	t.Helper()
	mock := mockpve.New()
	mock.SeedVersion("9.2.4", "9.2", "pvelab-test")
	ts := mock.Serve()
	t.Cleanup(ts.Close)
	c, err := proxmox.NewClient(context.Background(), ts.URL, api.TokenCredentials("root@pam!lab", "secret"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c, mock
}

// isoTestConfig is a minimal valid-enough config for the ISO flow (PrepareISO
// only reads the fields set here; full validation is config_test.go's job).
func isoTestConfig() *Config {
	return &Config{
		Outer: Outer{Node: "r740a", ISOStorage: "local"},
		Nested: Nested{
			PVEVersion: "9.2",
			BaseISO:    "/var/lib/vz/template/iso/proxmox-ve_9.2-1.iso",
			AnswerURL:  "http://192.0.2.10:8442",
		},
	}
}

func TestPreparedISOVolid(t *testing.T) {
	if got := PreparedISOVolid("local", "9.2"); got != "local:iso/pvelab-9.2-auto-http.iso" {
		t.Errorf("PreparedISOVolid = %q", got)
	}
}

func TestPrepareISOSkipsWhenPresent(t *testing.T) {
	c, mock := newMockClient(t)
	cfg := isoTestConfig()
	volid := PreparedISOVolid("local", "9.2")
	mock.AddVolume("r740a", "local", volid, "iso", "iso", 1<<20)

	sc := &fakeExec{}
	got, err := PrepareISO(context.Background(), c, sc, cfg, nil)
	if err != nil {
		t.Fatalf("PrepareISO: %v", err)
	}
	if got != volid {
		t.Errorf("volid = %q, want %q", got, volid)
	}
	if len(sc.calls) != 0 {
		t.Errorf("PrepareISO ran %d ssh commands on an already-prepared ISO, want 0: %q", len(sc.calls), sc.calls)
	}
}

func TestPrepareISOPreparesAndVerifies(t *testing.T) {
	c, mock := newMockClient(t)
	cfg := isoTestConfig()
	volid := PreparedISOVolid("local", "9.2")

	sc := &fakeExec{}
	sc.fn = func(cmd string) ([]byte, error) {
		if strings.HasPrefix(cmd, "proxmox-auto-install-assistant prepare-iso") {
			// The assistant "writes" the ISO into the storage.
			mock.AddVolume("r740a", "local", volid, "iso", "iso", 1<<30)
		}
		return []byte("ok"), nil
	}

	got, err := PrepareISO(context.Background(), c, sc, cfg, nil)
	if err != nil {
		t.Fatalf("PrepareISO: %v", err)
	}
	if got != volid {
		t.Errorf("volid = %q, want %q", got, volid)
	}
	if len(sc.calls) != 2 {
		t.Fatalf("ssh commands = %d, want 2 (check + prepare): %q", len(sc.calls), sc.calls)
	}
	if !strings.Contains(sc.calls[0], "command -v proxmox-auto-install-assistant") {
		t.Errorf("first command should be the assistant check, got %q", sc.calls[0])
	}
	prep := sc.calls[1]
	for _, want := range []string{
		"prepare-iso '/var/lib/vz/template/iso/proxmox-ve_9.2-1.iso'",
		"--fetch-from http",
		"--url 'http://192.0.2.10:8442'",
		"--output '/var/lib/vz/template/iso/pvelab-9.2-auto-http.iso'",
	} {
		if !strings.Contains(prep, want) {
			t.Errorf("prepare command missing %q: %q", want, prep)
		}
	}
}

func TestPrepareISOInstallsMissingAssistant(t *testing.T) {
	c, mock := newMockClient(t)
	cfg := isoTestConfig()
	volid := PreparedISOVolid("local", "9.2")

	sc := &fakeExec{}
	sc.fn = func(cmd string) ([]byte, error) {
		switch {
		case strings.HasPrefix(cmd, "command -v"):
			return nil, errors.New("exit 1")
		case strings.HasPrefix(cmd, "apt-get install"):
			return []byte("installed"), nil
		default:
			mock.AddVolume("r740a", "local", volid, "iso", "iso", 1<<30)
			return []byte("ok"), nil
		}
	}

	if _, err := PrepareISO(context.Background(), c, sc, cfg, nil); err != nil {
		t.Fatalf("PrepareISO: %v", err)
	}
	if len(sc.calls) != 3 || !strings.Contains(sc.calls[1], "apt-get install -y proxmox-auto-install-assistant xorriso") {
		t.Errorf("expected check, install, prepare — got %q", sc.calls)
	}
}

func TestPrepareISOInstallFailure(t *testing.T) {
	c, _ := newMockClient(t)
	cfg := isoTestConfig()

	sc := &fakeExec{fn: func(string) ([]byte, error) { return []byte("E: no candidate"), errors.New("exit 100") }}
	_, err := PrepareISO(context.Background(), c, sc, cfg, nil)
	if err == nil || !strings.Contains(err.Error(), "install manually") {
		t.Errorf("PrepareISO = %v, want install-manually guidance", err)
	}
}

func TestPrepareISONotVisibleAfterPrepare(t *testing.T) {
	c, _ := newMockClient(t)
	cfg := isoTestConfig()

	// Every command "succeeds" but nothing ever lands in the storage.
	sc := &fakeExec{}
	_, err := PrepareISO(context.Background(), c, sc, cfg, nil)
	if err == nil || !strings.Contains(err.Error(), "not visible on storage") {
		t.Errorf("PrepareISO = %v, want not-visible error", err)
	}
}
