package lab

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// provisionTestConfig covers every field the provision flow reads.
func provisionTestConfig() *Config {
	return &Config{
		Outer: Outer{
			Node:       "r740a",
			Storage:    "local-zfs",
			ISOStorage: "local",
			Bridge:     "vmbr0",
		},
		Nested: Nested{
			PVEVersion: "9.2",
			Cores:      4,
			MemoryMB:   8192,
			DiskGB:     32,
			Nodes: []Node{
				{Name: "pve1-dogfood", VMID: 9201, CIDR: "192.0.2.201/24"},
				{Name: "pve2-dogfood", VMID: 9202, CIDR: "192.0.2.202/24"},
				{Name: "pve3-dogfood", VMID: 9203, CIDR: "192.0.2.203/24"},
			},
		},
	}
}

func TestSmbiosSerial(t *testing.T) {
	got := smbiosSerial(Node{Name: "pve1-dogfood"})
	want := "serial=" + base64.StdEncoding.EncodeToString([]byte("pve1-dogfood")) + ",base64=1"
	if got != want {
		t.Errorf("smbiosSerial = %q, want %q", got, want)
	}
}

func TestNodeEndpoint(t *testing.T) {
	got, err := nodeEndpoint(Node{Name: "pve1-dogfood", CIDR: "192.0.2.201/24"})
	if err != nil || got != "https://192.0.2.201:8006" {
		t.Errorf("nodeEndpoint = %q, %v", got, err)
	}
	if _, err := nodeEndpoint(Node{Name: "x", CIDR: "junk"}); err == nil {
		t.Error("nodeEndpoint on bad cidr = nil error, want error")
	}
}

func TestEnsureISOPrepared(t *testing.T) {
	c, mock := newMockClient(t)
	cfg := provisionTestConfig()
	ctx := context.Background()

	err := EnsureISOPrepared(ctx, c, cfg)
	if err == nil || !strings.Contains(err.Error(), "pvelab iso") {
		t.Errorf("EnsureISOPrepared without ISO = %v, want pvelab-iso hint", err)
	}

	mock.AddVolume("r740a", "local", PreparedISOVolid("local", "9.2"), "iso", "iso", 1<<30)
	if err := EnsureISOPrepared(ctx, c, cfg); err != nil {
		t.Errorf("EnsureISOPrepared with ISO = %v, want nil", err)
	}
}

func TestEnsureVMIDsFree(t *testing.T) {
	c, mock := newMockClient(t)
	cfg := provisionTestConfig()
	ctx := context.Background()

	if err := EnsureVMIDsFree(ctx, c, cfg); err != nil {
		t.Errorf("EnsureVMIDsFree on clean node = %v, want nil", err)
	}

	mock.AddVM("r740a", 9202, "someone-elses-vm", "stopped")
	err := EnsureVMIDsFree(ctx, c, cfg)
	if err == nil || !strings.Contains(err.Error(), "9202") || !strings.Contains(err.Error(), "pvelab down") {
		t.Errorf("EnsureVMIDsFree with collision = %v, want vmid 9202 named + pvelab-down hint", err)
	}
}

func TestCreateAndStartNodeVMs(t *testing.T) {
	c, _ := newMockClient(t)
	cfg := provisionTestConfig()
	ctx := context.Background()
	iso := PreparedISOVolid("local", "9.2")

	if err := CreateNodeVMs(ctx, c, cfg, iso, nil); err != nil {
		t.Fatalf("CreateNodeVMs: %v", err)
	}

	svc := c.QEMU("r740a")
	vms, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	names := make(map[int]string, len(vms))
	for _, vm := range vms {
		names[int(vm.VMID)] = vm.Name
	}
	for _, n := range cfg.Nested.Nodes {
		if names[n.VMID] != "pvelab-"+n.Name {
			t.Errorf("vm %d name = %q, want %q", n.VMID, names[n.VMID], "pvelab-"+n.Name)
		}
	}

	// The created config must carry the answer-server match key and the
	// prepared ISO on ide2.
	vmCfg, err := svc.Config(ctx, 9201)
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	if got := vmCfg.Extra["smbios1"]; got != smbiosSerial(cfg.Nested.Nodes[0]) {
		t.Errorf("smbios1 = %q, want %q", got, smbiosSerial(cfg.Nested.Nodes[0]))
	}
	if got := vmCfg.Extra["ide2"]; !strings.Contains(got, iso) {
		t.Errorf("ide2 = %q, want it to carry %q", got, iso)
	}

	if err := StartNodeVMs(ctx, c, cfg, nil); err != nil {
		t.Fatalf("StartNodeVMs: %v", err)
	}
	st, err := svc.Get(ctx, 9203)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(st.Status) != "running" {
		t.Errorf("vm 9203 status = %q after start, want running", st.Status)
	}
}

func TestWaitReady(t *testing.T) {
	cfg := provisionTestConfig()

	var mu sync.Mutex
	attempts := map[string]int{}
	probe := func(_ context.Context, endpoint string) error {
		mu.Lock()
		defer mu.Unlock()
		attempts[endpoint]++
		switch {
		case strings.Contains(endpoint, "192.0.2.201"):
			return nil // ready immediately.
		case strings.Contains(endpoint, "192.0.2.202") && attempts[endpoint] >= 3:
			return nil // ready on the third poll.
		default:
			return errors.New("connection refused") // never ready.
		}
	}

	results, err := waitReady(context.Background(), cfg, probe, 2*time.Millisecond, 100*time.Millisecond, nil)
	if err == nil || !strings.Contains(err.Error(), "pve3-dogfood") {
		t.Errorf("waitReady = %v, want pve3-dogfood ceiling error", err)
	}
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3", len(results))
	}
	if results[0].Node != "pve1-dogfood" || !results[0].Ready {
		t.Errorf("pve1 readiness = %+v, want ready", results[0])
	}
	if !results[1].Ready {
		t.Errorf("pve2 readiness = %+v, want ready after retries", results[1])
	}
	if results[2].Ready {
		t.Errorf("pve3 readiness = %+v, want not ready", results[2])
	}
}

// TestVersionProbe exercises the real readiness probe against mockpve's
// password-ticket flow (the nested nodes are probed with root@pam creds).
func TestVersionProbe(t *testing.T) {
	_, mock := newMockClient(t)
	mock.AddUser("root@pam", "hunter2")
	ts := mock.Serve()
	t.Cleanup(ts.Close)

	probe := versionProbe("hunter2")
	if err := probe(context.Background(), ts.URL); err != nil {
		t.Errorf("versionProbe against live mock = %v, want nil", err)
	}

	tsDead := mock.Serve()
	deadURL := tsDead.URL
	tsDead.Close()
	if err := probe(context.Background(), deadURL); err == nil {
		t.Error("versionProbe against closed server = nil, want error")
	}
}
