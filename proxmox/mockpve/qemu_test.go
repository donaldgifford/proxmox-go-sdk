package mockpve_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
)

func TestQEMUListAndSeed(t *testing.T) {
	mock := mockpve.New()
	mock.AddVM("pve", 100, "debian12", "stopped")
	mock.AddVM("pve", 101, "ubuntu24", "running")
	c, cleanup := mock.NewClient()
	defer cleanup()

	var list []struct {
		VMID   int    `json:"vmid"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := c.DoRequest(context.Background(), http.MethodGet, "/nodes/pve/qemu", nil, &list); err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("list returned %d entries, want 2", len(list))
	}
}

func TestQEMUStatusNotFound(t *testing.T) {
	mock := mockpve.New()
	c, cleanup := mock.NewClient()
	defer cleanup()

	var out any
	err := c.DoRequest(context.Background(), http.MethodGet, "/nodes/pve/qemu/999/status/current", nil, &out)
	if !errors.Is(err, pverr.ErrNotFound) {
		t.Errorf("status of unknown VM = %v, want ErrNotFound", err)
	}
}

func TestQEMUConfigSeed(t *testing.T) {
	mock := mockpve.New()
	mock.AddVM("pve", 100, "debian12", "stopped")
	mock.SetVMConfig("pve", 100, map[string]any{"cores": 2, "memory": 2048, "net0": "virtio,bridge=vmbr0"})
	c, cleanup := mock.NewClient()
	defer cleanup()

	var cfg struct {
		Cores  int    `json:"cores"`
		Memory int    `json:"memory"`
		Net0   string `json:"net0"`
	}
	if err := c.DoRequest(context.Background(), http.MethodGet, "/nodes/pve/qemu/100/config", nil, &cfg); err != nil {
		t.Fatalf("config: %v", err)
	}
	if cfg.Cores != 2 || cfg.Memory != 2048 || cfg.Net0 != "virtio,bridge=vmbr0" {
		t.Errorf("config = %+v, want cores=2 memory=2048 net0=virtio,bridge=vmbr0", cfg)
	}
}

func TestQEMUCreateThenDelete(t *testing.T) {
	mock := mockpve.New()
	c, cleanup := mock.NewClient()
	defer cleanup()
	ctx := context.Background()
	svc := tasks.NewService(c)

	var upid string
	body := url.Values{"vmid": {"130"}, "name": {"created"}}
	if err := c.DoRequest(ctx, http.MethodPost, "/nodes/pve/qemu", body, &upid); err != nil {
		t.Fatalf("create: %v", err)
	}
	ref, err := tasks.NewRef(upid)
	if err != nil {
		t.Fatalf("parse create UPID %q: %v", upid, err)
	}
	st, err := svc.Status(ctx, ref)
	if err != nil {
		t.Fatalf("create task status: %v", err)
	}
	if !st.OK() {
		t.Errorf("create task exit = %q, want OK", st.ExitStatus)
	}

	var delUPID string
	if err := c.DoRequest(ctx, http.MethodDelete, "/nodes/pve/qemu/130", nil, &delUPID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if delUPID == "" {
		t.Fatal("delete returned an empty UPID")
	}

	var out any
	if err := c.DoRequest(ctx, http.MethodGet, "/nodes/pve/qemu/130/status/current", nil, &out); !errors.Is(err, pverr.ErrNotFound) {
		t.Errorf("status after delete = %v, want ErrNotFound", err)
	}
}

func TestQEMUSetConfig(t *testing.T) {
	mock := mockpve.New()
	mock.AddVM("pve", 100, "debian12", "stopped")
	c, cleanup := mock.NewClient()
	defer cleanup()
	ctx := context.Background()

	body := url.Values{"cores": {"4"}, "description": {"managed"}}
	var upid string
	if err := c.DoRequest(ctx, http.MethodPut, "/nodes/pve/qemu/100/config", body, &upid); err != nil {
		t.Fatalf("set config: %v", err)
	}
	if upid != "" {
		t.Errorf("set config returned UPID %q, want empty (synchronous)", upid)
	}

	var cfg struct {
		Cores       int    `json:"cores"`
		Description string `json:"description"`
	}
	if err := c.DoRequest(ctx, http.MethodGet, "/nodes/pve/qemu/100/config", nil, &cfg); err != nil {
		t.Fatalf("config: %v", err)
	}
	if cfg.Cores != 4 || cfg.Description != "managed" {
		t.Errorf("config = %+v, want cores=4 description=managed", cfg)
	}
}

func TestQEMUClone(t *testing.T) {
	mock := mockpve.New()
	mock.AddVM("pve", 9000, "template", "stopped")
	c, cleanup := mock.NewClient()
	defer cleanup()
	ctx := context.Background()

	var upid string
	body := url.Values{"newid": {"131"}, "name": {"clone"}}
	if err := c.DoRequest(ctx, http.MethodPost, "/nodes/pve/qemu/9000/clone", body, &upid); err != nil {
		t.Fatalf("clone: %v", err)
	}
	if upid == "" {
		t.Fatal("clone returned an empty UPID")
	}

	var out any
	if err := c.DoRequest(ctx, http.MethodGet, "/nodes/pve/qemu/131/status/current", nil, &out); err != nil {
		t.Fatalf("status of clone: %v", err)
	}
}

func TestQEMUCloneSourceNotFound(t *testing.T) {
	mock := mockpve.New()
	c, cleanup := mock.NewClient()
	defer cleanup()

	body := url.Values{"newid": {"131"}}
	var upid string
	err := c.DoRequest(context.Background(), http.MethodPost, "/nodes/pve/qemu/9000/clone", body, &upid)
	if !errors.Is(err, pverr.ErrNotFound) {
		t.Errorf("clone of missing source = %v, want ErrNotFound", err)
	}
}
