package qemu_test

import (
	"context"
	"errors"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/qemu"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/version"
)

const testNode = "pve"

// newServices wires a qemu and a tasks service onto one mock-backed client so
// task-returning ops can be awaited in the same test.
func newServices(t *testing.T, mock *mockpve.Server) (*qemu.Service, *tasks.Service) {
	t.Helper()
	c, cleanup := mock.NewClient()
	t.Cleanup(cleanup)
	return qemu.NewService(c, testNode, version.Capabilities{}), tasks.NewService(c)
}

// awaitOK waits for a task ref and fails the test unless it ends OK.
func awaitOK(t *testing.T, ts *tasks.Service, ref tasks.Ref) {
	t.Helper()
	st, err := ts.Wait(context.Background(), ref)
	if err != nil {
		t.Fatalf("Wait(%s): %v", ref.UPID, err)
	}
	if !st.OK() {
		t.Fatalf("task %s exit = %q, want OK", ref.UPID, st.ExitStatus)
	}
}

func TestList(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddVM(testNode, 100, "debian12", "stopped")
	mock.AddVM(testNode, 101, "ubuntu24", "running")
	svc, _ := newServices(t, mock)

	vms, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(vms) != 2 {
		t.Fatalf("List returned %d VMs, want 2", len(vms))
	}
	for _, vm := range vms {
		if vm.VMID == 0 || vm.Status == "" {
			t.Errorf("VM %+v missing vmid/status", vm)
		}
	}
}

func TestGet(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddVM(testNode, 100, "debian12", "running")
	svc, _ := newServices(t, mock)

	st, err := svc.Get(context.Background(), 100)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if st.VMID != 100 {
		t.Errorf("Get VMID = %d, want 100", st.VMID)
	}
	if st.Status != types.PowerStateRunning {
		t.Errorf("Get Status = %q, want %q", st.Status, types.PowerStateRunning)
	}
}

func TestGetNotFound(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc, _ := newServices(t, mock)

	_, err := svc.Get(context.Background(), 999)
	if !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("Get(999) error = %v, want ErrNotFound", err)
	}
}

func TestConfig(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddVM(testNode, 100, "debian12", "stopped")
	mock.SetVMConfig(testNode, 100, map[string]any{
		"cores":   2,
		"memory":  2048,
		"net0":    "virtio,bridge=vmbr0",
		"balloon": 0,
		"virtio0": "local-lvm:vm-100-disk-0,size=32G", // unmodelled -> Extra.
	})
	svc, _ := newServices(t, mock)

	cfg, err := svc.Config(context.Background(), 100)
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	if cfg.Cores != 2 {
		t.Errorf("Cores = %d, want 2", cfg.Cores)
	}
	if cfg.Memory != 2048 {
		t.Errorf("Memory = %d, want 2048", cfg.Memory)
	}
	if cfg.Net0 != "virtio,bridge=vmbr0" {
		t.Errorf("Net0 = %q, want virtio,bridge=vmbr0", cfg.Net0)
	}
	if got := cfg.Extra["virtio0"]; got != "local-lvm:vm-100-disk-0,size=32G" {
		t.Errorf("Extra[virtio0] = %q, want the disk spec", got)
	}
}

func TestSetConfig(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddVM(testNode, 100, "debian12", "stopped")
	svc, _ := newServices(t, mock)
	ctx := context.Background()

	ref, err := svc.SetConfig(ctx, 100, &qemu.ConfigUpdate{
		Description: "managed by sdk",
		Cores:       4,
	})
	if err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	if ref.UPID != "" {
		t.Errorf("SetConfig returned a task ref %q, want the zero ref (sync change)", ref.UPID)
	}

	cfg, err := svc.Config(ctx, 100)
	if err != nil {
		t.Fatalf("Config after SetConfig: %v", err)
	}
	if cfg.Description != "managed by sdk" {
		t.Errorf("Description = %q, want \"managed by sdk\"", cfg.Description)
	}
	if cfg.Cores != 4 {
		t.Errorf("Cores = %d, want 4", cfg.Cores)
	}
}

func TestSetConfigNilSpec(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddVM(testNode, 100, "debian12", "stopped")
	svc, _ := newServices(t, mock)

	if _, err := svc.SetConfig(context.Background(), 100, nil); err == nil {
		t.Fatal("SetConfig(nil) error = nil, want non-nil")
	}
}

func TestCreate(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc, ts := newServices(t, mock)
	ctx := context.Background()

	ref, err := svc.Create(ctx, &qemu.CreateSpec{
		VMID:   110,
		Name:   "fresh",
		Memory: 2048,
		Cores:  2,
		Net0:   "virtio,bridge=vmbr0",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	awaitOK(t, ts, ref)

	st, err := svc.Get(ctx, 110)
	if err != nil {
		t.Fatalf("Get after Create: %v", err)
	}
	if st.Status != types.PowerStateStopped {
		t.Errorf("new VM Status = %q, want stopped", st.Status)
	}
}

func TestCreateNilSpec(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc, _ := newServices(t, mock)

	if _, err := svc.Create(context.Background(), nil); err == nil {
		t.Fatal("Create(nil) error = nil, want non-nil")
	}
}

func TestClone(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddVM(testNode, 9000, "template", "stopped")
	svc, ts := newServices(t, mock)
	ctx := context.Background()

	full := types.PVEBool(true)
	ref, err := svc.Clone(ctx, 9000, &qemu.CloneSpec{
		NewID: 131,
		Name:  "clone-of-template",
		Full:  &full,
	})
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	awaitOK(t, ts, ref)

	if _, err := svc.Get(ctx, 131); err != nil {
		t.Fatalf("Get cloned VM: %v", err)
	}
}

func TestCloneSourceNotFound(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc, _ := newServices(t, mock)

	_, err := svc.Clone(context.Background(), 9000, &qemu.CloneSpec{NewID: 131})
	if !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("Clone of missing source error = %v, want ErrNotFound", err)
	}
}

func TestDelete(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddVM(testNode, 100, "doomed", "stopped")
	svc, ts := newServices(t, mock)
	ctx := context.Background()

	ref, err := svc.Delete(ctx, 100)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	awaitOK(t, ts, ref)

	if _, err := svc.Get(ctx, 100); !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("Get after Delete error = %v, want ErrNotFound", err)
	}
}

func TestDeleteNotFound(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc, _ := newServices(t, mock)

	_, err := svc.Delete(context.Background(), 999)
	if !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("Delete(999) error = %v, want ErrNotFound", err)
	}
}

// TestCreateWithExtra exercises the unmodelled-param escape hatch end to end.
func TestCreateWithExtra(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc, ts := newServices(t, mock)
	ctx := context.Background()

	ref, err := svc.Create(ctx, &qemu.CreateSpec{
		VMID: 120,
		Name: "with-extra",
		Extra: map[string]string{
			"scsihw": "virtio-scsi-single",
		},
	})
	if err != nil {
		t.Fatalf("Create with Extra: %v", err)
	}
	awaitOK(t, ts, ref)
}
