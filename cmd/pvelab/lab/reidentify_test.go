package lab

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox"
)

// fakeSession scripts one clone's SSH exchange, recording every command. The
// reboot always "drops the connection" like a real one.
type fakeSession struct {
	mu    sync.Mutex
	execs []string
}

func (f *fakeSession) Exec(_ context.Context, cmd string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.execs = append(f.execs, cmd)
	if cmd == "reboot" {
		return nil, errors.New("connection dropped")
	}
	return nil, nil
}

func (*fakeSession) Close() error { return nil }

// reidentifyHarness fakes the dialer + probe and journals events so tests can
// assert the pass is serialized: node N+1's dial must come after node N's
// probe succeeded.
type reidentifyHarness struct {
	mu       sync.Mutex
	events   []string
	sessions []*fakeSession
	refuse   int // refuse this many dials first (clone "still booting").
}

func (h *reidentifyHarness) dial(_ context.Context, host string) (cloneSession, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.refuse > 0 {
		h.refuse--
		return nil, errors.New("connection refused")
	}
	s := &fakeSession{}
	h.sessions = append(h.sessions, s)
	h.events = append(h.events, "dial:"+host)
	return s, nil
}

func (h *reidentifyHarness) probe(_ context.Context, endpoint string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, "probe:"+endpoint)
	return nil
}

// seedClones puts the three stopped clones on the mock outer node.
func seedClones(t *testing.T) (*proxmox.Client, *Config) {
	t.Helper()
	c, mock := newMockClient(t)
	cfg := templateTestConfig()
	for _, n := range cfg.Nested.Nodes {
		mock.AddVM("r740a", n.VMID, vmName(n), "stopped")
	}
	return c, cfg
}

func runReidentify(t *testing.T, c *proxmox.Client, cfg *Config, h *reidentifyHarness) ([]NodeReadiness, error) {
	t.Helper()
	return reidentifyClones(context.Background(), c, cfg, h.dial, h.probe,
		time.Millisecond, 250*time.Millisecond, nil)
}

func TestReidentifyClonesHappyPath(t *testing.T) {
	c, cfg := seedClones(t)
	h := &reidentifyHarness{}

	results, err := runReidentify(t, c, cfg, h)
	if err != nil {
		t.Fatalf("reidentifyClones: %v", err)
	}
	for i, r := range results {
		if !r.Ready {
			t.Errorf("node %d (%s) not ready", i, r.Node)
		}
	}

	// Every clone is running afterwards.
	for _, n := range cfg.Nested.Nodes {
		st, err := c.QEMU("r740a").Get(context.Background(), n.VMID)
		if err != nil || string(st.Status) != "running" {
			t.Errorf("clone %d status = %v, %v — want running", n.VMID, st, err)
		}
	}

	// One session per node, each got the rewrite script then the reboot; the
	// reboot's dropped-connection error was tolerated.
	if len(h.sessions) != 3 {
		t.Fatalf("sessions = %d, want 3", len(h.sessions))
	}
	for i, s := range h.sessions {
		if len(s.execs) != 2 || s.execs[1] != "reboot" {
			t.Errorf("session %d execs = %q, want [script, reboot]", i, s.execs)
		}
	}

	// Serialization: every dial happens at the template IP, and node N+1's
	// dial comes only after node N's own endpoint answered.
	want := []string{
		"dial:192.0.2.210",
		"probe:https://192.0.2.201:8006",
		"dial:192.0.2.210",
		"probe:https://192.0.2.202:8006",
		"dial:192.0.2.210",
		"probe:https://192.0.2.203:8006",
	}
	if got := fmt.Sprintf("%v", h.events); got != fmt.Sprintf("%v", want) {
		t.Errorf("event order = %v, want %v", h.events, want)
	}
}

func TestReidentifyScriptContent(t *testing.T) {
	cfg := templateTestConfig()
	n := cfg.Nested.Nodes[0] // pve1-dogfood, 192.0.2.201/24.
	script := reidentifyScript(templateNodeName(cfg), "192.0.2.210", cfg.Nested.Template.CIDR, n, "192.0.2.201")

	for _, want := range []string{
		"set -e",
		"hostnamectl set-hostname pve1-dogfood",
		"sed -i 's/192.0.2.210/192.0.2.201/g; s/tmpl-9-2/pve1-dogfood/g' /etc/hosts",
		"sed -i 's|address 192.0.2.210/24|address 192.0.2.201/24|' /etc/network/interfaces",
		"rm -f /etc/ssh/ssh_host_*",
		"ssh-keygen -A",
		"mv /etc/pve/nodes/tmpl-9-2 /etc/pve/nodes/pve1-dogfood || true",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script missing %q:\n%s", want, script)
		}
	}
}

// TestReidentifyDialRetry tolerates the clone's boot window: early refused
// dials are retried on the cadence.
func TestReidentifyDialRetry(t *testing.T) {
	c, cfg := seedClones(t)
	h := &reidentifyHarness{refuse: 2}

	if _, err := runReidentify(t, c, cfg, h); err != nil {
		t.Fatalf("reidentifyClones with slow first boot: %v", err)
	}
	if len(h.sessions) != 3 {
		t.Errorf("sessions = %d, want 3 after retries", len(h.sessions))
	}
}

// TestReidentifyProbeTimeout fails naming the node and never touches the
// remaining clones.
func TestReidentifyProbeTimeout(t *testing.T) {
	c, cfg := seedClones(t)
	h := &reidentifyHarness{}
	deadProbe := func(_ context.Context, endpoint string) error {
		return errors.New("no answer from " + endpoint)
	}

	_, err := reidentifyClones(context.Background(), c, cfg, h.dial, deadProbe,
		time.Millisecond, 50*time.Millisecond, nil)
	if err == nil || !strings.Contains(err.Error(), "pve1-dogfood") {
		t.Fatalf("err = %v, want node pve1-dogfood named", err)
	}
	if len(h.sessions) != 1 {
		t.Errorf("sessions = %d, want 1 (later clones untouched)", len(h.sessions))
	}
	st, err := c.QEMU("r740a").Get(context.Background(), 9202)
	if err != nil || string(st.Status) != "stopped" {
		t.Errorf("clone 9202 = %v, %v — want still stopped", st, err)
	}
}

func TestReidentifyRequiresTemplateBlock(t *testing.T) {
	c, _ := newMockClient(t)
	cfg := provisionTestConfig() // no template block.
	h := &reidentifyHarness{}

	_, err := runReidentify(t, c, cfg, h)
	if err == nil || !strings.Contains(err.Error(), "nested.template") {
		t.Errorf("err = %v, want nested.template hint", err)
	}
}

func TestTofuHostKeyCallback(t *testing.T) {
	keyA := genHostKey(t)
	keyB := genHostKey(t)
	cb := tofuHostKeyCallback()

	if err := cb("192.0.2.210:22", nil, keyA); err != nil {
		t.Fatalf("first key pinned: %v", err)
	}
	if err := cb("192.0.2.210:22", nil, keyA); err != nil {
		t.Errorf("same key again: %v", err)
	}
	if err := cb("192.0.2.210:22", nil, keyB); err == nil {
		t.Error("different key accepted, want error")
	}
}

func genHostKey(t *testing.T) gossh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	key, err := gossh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("wrap key: %v", err)
	}
	return key
}

func TestCloneNodeVMs(t *testing.T) {
	c, mock := newMockClient(t)
	cfg := templateTestConfig()
	mock.AddVM("r740a", 9210, TemplateName(cfg), "stopped")
	mock.SetVMConfig("r740a", 9210, map[string]any{"template": 1})

	if err := CloneNodeVMs(context.Background(), c, cfg, 9210, nil); err != nil {
		t.Fatalf("CloneNodeVMs: %v", err)
	}
	svc := c.QEMU("r740a")
	for _, n := range cfg.Nested.Nodes {
		st, err := svc.Get(context.Background(), n.VMID)
		if err != nil {
			t.Fatalf("Get %d: %v", n.VMID, err)
		}
		if st.Name != vmName(n) || string(st.Status) != "stopped" {
			t.Errorf("clone %d = %q/%s, want %q/stopped", n.VMID, st.Name, st.Status, vmName(n))
		}
	}
}
