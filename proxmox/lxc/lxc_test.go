package lxc_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/lxc"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/version"
)

const (
	testNode = "pve"
	powerCT  = 200
)

func newServices(t *testing.T, mock *mockpve.Server) (*lxc.Service, *tasks.Service) {
	t.Helper()
	c, cleanup := mock.NewClient()
	t.Cleanup(cleanup)
	return lxc.NewService(c, testNode, version.Capabilities{}), tasks.NewService(c)
}

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

func wantStatus(t *testing.T, svc *lxc.Service, want types.PowerState) {
	t.Helper()
	st, err := svc.Get(context.Background(), powerCT)
	if err != nil {
		t.Fatalf("Get(%d): %v", powerCT, err)
	}
	if st.Status != want {
		t.Errorf("container %d status = %q, want %q", powerCT, st.Status, want)
	}
}

func TestList(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddContainer(testNode, 200, "web", "running")
	mock.AddContainer(testNode, 201, "db", "stopped")
	svc, _ := newServices(t, mock)

	cts, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(cts) != 2 {
		t.Fatalf("List returned %d containers, want 2", len(cts))
	}
}

func TestGetNotFound(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc, _ := newServices(t, mock)

	if _, err := svc.Get(context.Background(), 999); !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("Get(999) = %v, want ErrNotFound", err)
	}
}

func TestConfig(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddContainer(testNode, 200, "web", "stopped")
	mock.SetCTConfig(testNode, 200, map[string]any{
		"hostname": "web",
		"cores":    2,
		"memory":   512,
		"mp0":      "local-lvm:8,mp=/data", // unmodelled -> Extra.
	})
	svc, _ := newServices(t, mock)

	cfg, err := svc.Config(context.Background(), 200)
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	if cfg.Hostname != "web" || cfg.Cores != 2 || cfg.Memory != 512 {
		t.Errorf("config = %+v, want hostname=web cores=2 memory=512", cfg)
	}
	if cfg.Extra["mp0"] != "local-lvm:8,mp=/data" {
		t.Errorf("Extra[mp0] = %q, want the mount-point spec", cfg.Extra["mp0"])
	}
}

func TestCreate(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc, ts := newServices(t, mock)
	ctx := context.Background()

	unpriv := types.PVEBool(true)
	ref, err := svc.Create(ctx, &lxc.CreateSpec{
		VMID:         210,
		OSTemplate:   "local:vztmpl/debian-12-standard_12.2-1_amd64.tar.zst",
		Hostname:     "fresh",
		Storage:      "local-lvm",
		RootFS:       "local-lvm:8",
		Cores:        2,
		Memory:       512,
		Unprivileged: &unpriv,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	awaitOK(t, ts, ref)

	st, err := svc.Get(ctx, 210)
	if err != nil {
		t.Fatalf("Get after Create: %v", err)
	}
	if st.Status != types.PowerStateStopped {
		t.Errorf("new container Status = %q, want stopped", st.Status)
	}
}

func TestCreateValidation(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc, _ := newServices(t, mock)
	ctx := context.Background()

	if _, err := svc.Create(ctx, nil); err == nil {
		t.Error("Create(nil) error = nil, want non-nil")
	}
	if _, err := svc.Create(ctx, &lxc.CreateSpec{VMID: 210}); err == nil {
		t.Error("Create(no ostemplate) error = nil, want non-nil")
	}
}

func TestClone(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddContainer(testNode, 9000, "template", "stopped")
	svc, ts := newServices(t, mock)
	ctx := context.Background()

	ref, err := svc.Clone(ctx, 9000, &lxc.CloneSpec{NewID: 231, Hostname: "clone"})
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	awaitOK(t, ts, ref)

	if _, err := svc.Get(ctx, 231); err != nil {
		t.Fatalf("Get cloned container: %v", err)
	}
}

func TestCloneSourceNotFound(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc, _ := newServices(t, mock)

	_, err := svc.Clone(context.Background(), 9000, &lxc.CloneSpec{NewID: 231})
	if !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("Clone of missing source = %v, want ErrNotFound", err)
	}
}

func TestDelete(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddContainer(testNode, 200, "doomed", "stopped")
	svc, ts := newServices(t, mock)
	ctx := context.Background()

	ref, err := svc.Delete(ctx, 200)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	awaitOK(t, ts, ref)

	if _, err := svc.Get(ctx, 200); !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("Get after Delete = %v, want ErrNotFound", err)
	}
}

func TestSetConfig(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddContainer(testNode, 200, "web", "stopped")
	svc, _ := newServices(t, mock)
	ctx := context.Background()

	ref, err := svc.SetConfig(ctx, 200, &lxc.ConfigUpdate{Hostname: "renamed", Cores: 4})
	if err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	if ref.UPID != "" {
		t.Errorf("SetConfig returned task %q, want zero ref (sync)", ref.UPID)
	}

	cfg, err := svc.Config(ctx, 200)
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	if cfg.Hostname != "renamed" || cfg.Cores != 4 {
		t.Errorf("config = %+v, want hostname=renamed cores=4", cfg)
	}
}

func TestSetConfigNilSpec(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddContainer(testNode, 200, "web", "stopped")
	svc, _ := newServices(t, mock)

	if _, err := svc.SetConfig(context.Background(), 200, nil); err == nil {
		t.Fatal("SetConfig(nil) error = nil, want non-nil")
	}
}

func TestPowerLifecycle(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddContainer(testNode, powerCT, "box", "stopped")
	svc, ts := newServices(t, mock)
	ctx := context.Background()

	steps := []struct {
		name string
		run  func() (tasks.Ref, error)
		want types.PowerState
	}{
		{"Start", func() (tasks.Ref, error) { return svc.Start(ctx, powerCT) }, types.PowerStateRunning},
		{"Suspend", func() (tasks.Ref, error) { return svc.Suspend(ctx, powerCT) }, types.PowerStateSuspended},
		{"Resume", func() (tasks.Ref, error) { return svc.Resume(ctx, powerCT) }, types.PowerStateRunning},
		{"Reboot", func() (tasks.Ref, error) { return svc.Reboot(ctx, powerCT) }, types.PowerStateRunning},
		{"Shutdown", func() (tasks.Ref, error) { return svc.Shutdown(ctx, powerCT) }, types.PowerStateStopped},
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
	mock.AddContainer(testNode, powerCT, "box", "running")
	svc, ts := newServices(t, mock)

	ref, err := svc.Stop(context.Background(), powerCT, lxc.WithStopTimeout(20*time.Second))
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	awaitOK(t, ts, ref)
	wantStatus(t, svc, types.PowerStateStopped)
}

func TestShutdownForceStop(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddContainer(testNode, powerCT, "box", "running")
	svc, ts := newServices(t, mock)

	ref, err := svc.Shutdown(context.Background(), powerCT, lxc.WithShutdownTimeout(10*time.Second), lxc.WithForceStop())
	if err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	awaitOK(t, ts, ref)
	wantStatus(t, svc, types.PowerStateStopped)
}

func TestStartNotFound(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc, _ := newServices(t, mock)

	if _, err := svc.Start(context.Background(), 999); !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("Start(999) = %v, want ErrNotFound", err)
	}
}
