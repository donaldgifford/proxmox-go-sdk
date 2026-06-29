package qemu_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/qemu"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/version"
)

const (
	testNode = "pve"
	powerVM  = 100 // the VM the power tests drive.
)

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

// wantStatus fails the test unless the power VM reports the expected state.
func wantStatus(t *testing.T, svc *qemu.Service, want types.PowerState) {
	t.Helper()
	st, err := svc.Get(context.Background(), powerVM)
	if err != nil {
		t.Fatalf("Get(%d): %v", powerVM, err)
	}
	if st.Status != want {
		t.Errorf("VM %d status = %q, want %q", powerVM, st.Status, want)
	}
}

func TestPowerLifecycle(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddVM(testNode, powerVM, "box", "stopped")
	svc, ts := newServices(t, mock)
	ctx := context.Background()

	steps := []struct {
		name string
		run  func() (tasks.Ref, error)
		want types.PowerState
	}{
		{"Start", func() (tasks.Ref, error) { return svc.Start(ctx, powerVM) }, types.PowerStateRunning},
		{"Suspend", func() (tasks.Ref, error) { return svc.Suspend(ctx, powerVM) }, types.PowerStateSuspended},
		{"Resume", func() (tasks.Ref, error) { return svc.Resume(ctx, powerVM) }, types.PowerStateRunning},
		{"Reboot", func() (tasks.Ref, error) { return svc.Reboot(ctx, powerVM) }, types.PowerStateRunning},
		{"Shutdown", func() (tasks.Ref, error) { return svc.Shutdown(ctx, powerVM) }, types.PowerStateStopped},
	}
	for _, step := range steps {
		ref, err := step.run()
		if err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		awaitOK(t, ts, ref)
		wantStatus(t, svc, step.want)
	}
}

func TestStopWithTimeout(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddVM(testNode, powerVM, "box", "running")
	svc, ts := newServices(t, mock)
	ctx := context.Background()

	ref, err := svc.Stop(ctx, powerVM, qemu.WithStopTimeout(30*time.Second))
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	awaitOK(t, ts, ref)
	wantStatus(t, svc, types.PowerStateStopped)
}

func TestShutdownForceStop(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddVM(testNode, powerVM, "box", "running")
	svc, ts := newServices(t, mock)
	ctx := context.Background()

	ref, err := svc.Shutdown(ctx, powerVM, qemu.WithShutdownTimeout(10*time.Second), qemu.WithForceStop())
	if err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	awaitOK(t, ts, ref)
	wantStatus(t, svc, types.PowerStateStopped)
}

func TestSuspendToDisk(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddVM(testNode, powerVM, "box", "running")
	svc, ts := newServices(t, mock)
	ctx := context.Background()

	ref, err := svc.Suspend(ctx, powerVM, qemu.WithSuspendToDisk("local-zfs"))
	if err != nil {
		t.Fatalf("Suspend: %v", err)
	}
	awaitOK(t, ts, ref)
	wantStatus(t, svc, types.PowerStateSuspended)
}

func TestStartNotFound(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc, _ := newServices(t, mock)

	_, err := svc.Start(context.Background(), 999)
	if !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("Start(999) error = %v, want ErrNotFound", err)
	}
}

func TestAddDisk(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddVM(testNode, 100, "box", "stopped")
	svc, _ := newServices(t, mock)
	ctx := context.Background()

	if _, err := svc.AddDisk(ctx, 100, &qemu.DiskSpec{
		Slot:    "scsi1",
		Storage: "local-lvm",
		SizeGB:  32,
		Options: map[string]string{"discard": "on", "ssd": "1"},
	}); err != nil {
		t.Fatalf("AddDisk: %v", err)
	}

	cfg, err := svc.Config(ctx, 100)
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	// Options are rendered sorted for determinism.
	if cfg.SCSI1 != "local-lvm:32,discard=on,ssd=1" {
		t.Errorf("scsi1 = %q, want local-lvm:32,discard=on,ssd=1", cfg.SCSI1)
	}
}

func TestAddDiskValidation(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddVM(testNode, 100, "box", "stopped")
	svc, _ := newServices(t, mock)
	ctx := context.Background()

	if _, err := svc.AddDisk(ctx, 100, nil); err == nil {
		t.Error("AddDisk(nil) error = nil, want non-nil")
	}
	if _, err := svc.AddDisk(ctx, 100, &qemu.DiskSpec{Slot: "scsi1"}); err == nil {
		t.Error("AddDisk(no storage/size) error = nil, want non-nil")
	}
}

func TestRemoveDisk(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddVM(testNode, 100, "box", "stopped")
	mock.SetVMConfig(testNode, 100, map[string]any{"scsi1": "local-lvm:vm-100-disk-1,size=32G"})
	svc, _ := newServices(t, mock)
	ctx := context.Background()

	if _, err := svc.RemoveDisk(ctx, 100, "scsi1"); err != nil {
		t.Fatalf("RemoveDisk: %v", err)
	}
	cfg, err := svc.Config(ctx, 100)
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	if cfg.SCSI1 != "" {
		t.Errorf("scsi1 = %q after RemoveDisk, want empty", cfg.SCSI1)
	}
}

func TestResizeDisk(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddVM(testNode, 100, "box", "stopped")
	svc, _ := newServices(t, mock)

	ref, err := svc.ResizeDisk(context.Background(), 100, "scsi0", "+10G")
	if err != nil {
		t.Fatalf("ResizeDisk: %v", err)
	}
	if ref.UPID != "" {
		t.Errorf("ResizeDisk returned task %q, want zero ref (synchronous)", ref.UPID)
	}
}

func TestAddNIC(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddVM(testNode, 100, "box", "stopped")
	svc, _ := newServices(t, mock)
	ctx := context.Background()

	if _, err := svc.AddNIC(ctx, 100, &qemu.NICSpec{
		Slot:   "net1",
		Model:  "virtio",
		Bridge: "vmbr0",
		VLAN:   10,
	}); err != nil {
		t.Fatalf("AddNIC: %v", err)
	}
	cfg, err := svc.Config(ctx, 100)
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	if cfg.Net1 != "virtio,bridge=vmbr0,tag=10" {
		t.Errorf("net1 = %q, want virtio,bridge=vmbr0,tag=10", cfg.Net1)
	}
}

func TestRemoveNIC(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddVM(testNode, 100, "box", "stopped")
	mock.SetVMConfig(testNode, 100, map[string]any{"net1": "virtio,bridge=vmbr0"})
	svc, _ := newServices(t, mock)
	ctx := context.Background()

	if _, err := svc.RemoveNIC(ctx, 100, "net1"); err != nil {
		t.Fatalf("RemoveNIC: %v", err)
	}
	cfg, err := svc.Config(ctx, 100)
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	if cfg.Net1 != "" {
		t.Errorf("net1 = %q after RemoveNIC, want empty", cfg.Net1)
	}
}

func TestMigrate(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddVM(testNode, 100, "mover", "running")
	c, cleanup := mock.NewClient()
	t.Cleanup(cleanup)
	src := qemu.NewService(c, testNode, version.Capabilities{})
	tgt := qemu.NewService(c, "pve2", version.Capabilities{})
	tsvc := tasks.NewService(c)
	ctx := context.Background()

	online := types.PVEBool(true)
	ref, err := src.Migrate(ctx, 100, &qemu.MigrateSpec{Target: "pve2", Online: &online})
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	awaitOK(t, tsvc, ref)

	if _, err := src.Get(ctx, 100); !errors.Is(err, pverr.ErrNotFound) {
		t.Errorf("Get on source after migrate = %v, want ErrNotFound", err)
	}
	if _, err := tgt.Get(ctx, 100); err != nil {
		t.Errorf("Get on target after migrate: %v", err)
	}
}

func TestMigrateValidation(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddVM(testNode, 100, "box", "running")
	svc, _ := newServices(t, mock)
	ctx := context.Background()

	if _, err := svc.Migrate(ctx, 100, nil); err == nil {
		t.Error("Migrate(nil) error = nil, want non-nil")
	}
	if _, err := svc.Migrate(ctx, 100, &qemu.MigrateSpec{}); err == nil {
		t.Error("Migrate(no target) error = nil, want non-nil")
	}
}

// hasSnapshot reports whether the list contains a snapshot named want.
func hasSnapshot(snaps []qemu.Snapshot, want string) bool {
	for _, s := range snaps {
		if s.Name == want {
			return true
		}
	}
	return false
}

func TestSnapshotLifecycle(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddVM(testNode, 100, "box", "running")
	svc, ts := newServices(t, mock)
	ctx := context.Background()

	ref, err := svc.CreateSnapshot(ctx, 100, &qemu.SnapshotSpec{
		Name:        "before-upgrade",
		Description: "pre-change checkpoint",
		VMState:     true,
	})
	if err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}
	awaitOK(t, ts, ref)

	snaps, err := svc.Snapshots(ctx, 100)
	if err != nil {
		t.Fatalf("Snapshots: %v", err)
	}
	if !hasSnapshot(snaps, "before-upgrade") {
		t.Fatalf("snapshots %+v missing before-upgrade", snaps)
	}
	if !hasSnapshot(snaps, "current") {
		t.Errorf("snapshots %+v missing the synthetic current entry", snaps)
	}

	rbRef, err := svc.RollbackSnapshot(ctx, 100, "before-upgrade", qemu.WithStartAfterRollback())
	if err != nil {
		t.Fatalf("RollbackSnapshot: %v", err)
	}
	awaitOK(t, ts, rbRef)

	delRef, err := svc.DeleteSnapshot(ctx, 100, "before-upgrade")
	if err != nil {
		t.Fatalf("DeleteSnapshot: %v", err)
	}
	awaitOK(t, ts, delRef)

	snaps, err = svc.Snapshots(ctx, 100)
	if err != nil {
		t.Fatalf("Snapshots after delete: %v", err)
	}
	if hasSnapshot(snaps, "before-upgrade") {
		t.Errorf("snapshot before-upgrade still present after delete: %+v", snaps)
	}
}

func TestSnapshotValidation(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddVM(testNode, 100, "box", "stopped")
	svc, _ := newServices(t, mock)
	ctx := context.Background()

	if _, err := svc.CreateSnapshot(ctx, 100, nil); err == nil {
		t.Error("CreateSnapshot(nil) error = nil, want non-nil")
	}
	if _, err := svc.CreateSnapshot(ctx, 100, &qemu.SnapshotSpec{}); err == nil {
		t.Error("CreateSnapshot(no name) error = nil, want non-nil")
	}
	if _, err := svc.RollbackSnapshot(ctx, 100, ""); err == nil {
		t.Error("RollbackSnapshot(no name) error = nil, want non-nil")
	}
	if _, err := svc.DeleteSnapshot(ctx, 100, ""); err == nil {
		t.Error("DeleteSnapshot(no name) error = nil, want non-nil")
	}
}

func TestRollbackUnknownSnapshot(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddVM(testNode, 100, "box", "running")
	svc, _ := newServices(t, mock)

	_, err := svc.RollbackSnapshot(context.Background(), 100, "ghost")
	if !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("RollbackSnapshot(ghost) error = %v, want ErrNotFound", err)
	}
}

func TestAgentPing(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddVM(testNode, 100, "box", "running")
	svc, _ := newServices(t, mock)

	if err := svc.AgentPing(context.Background(), 100); err != nil {
		t.Fatalf("AgentPing: %v", err)
	}
}

func TestAgentPingNotFound(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc, _ := newServices(t, mock)

	if err := svc.AgentPing(context.Background(), 999); !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("AgentPing(999) = %v, want ErrNotFound", err)
	}
}

func TestAgentExecWait(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddVM(testNode, 100, "box", "running")
	mock.SetVMAgentResult(testNode, 100, 0, "hello from guest\n", "")
	svc, _ := newServices(t, mock)

	st, err := svc.AgentExecWait(context.Background(), 100, []string{"echo", "hello from guest"})
	if err != nil {
		t.Fatalf("AgentExecWait: %v", err)
	}
	if !st.Exited.Bool() {
		t.Error("exec status Exited = false, want true")
	}
	if st.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", st.ExitCode)
	}
	if st.OutData != "hello from guest\n" {
		t.Errorf("OutData = %q, want the greeting", st.OutData)
	}
}

func TestAgentExecNonZeroExit(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddVM(testNode, 100, "box", "running")
	mock.SetVMAgentResult(testNode, 100, 2, "", "boom\n")
	svc, _ := newServices(t, mock)

	st, err := svc.AgentExecWait(context.Background(), 100, []string{"false"})
	if err != nil {
		t.Fatalf("AgentExecWait: %v", err)
	}
	if st.ExitCode != 2 || st.ErrData != "boom\n" {
		t.Errorf("status = %+v, want exitcode 2 / err boom", st)
	}
}

func TestAgentExecEmptyCommand(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddVM(testNode, 100, "box", "running")
	svc, _ := newServices(t, mock)

	if _, err := svc.AgentExec(context.Background(), 100, nil); err == nil {
		t.Error("AgentExec(nil command) error = nil, want non-nil")
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
