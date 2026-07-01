package nodes_test

import (
	"context"
	"errors"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/nodes"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/version"
)

const testNode = "pve"

func newService(t *testing.T, mock *mockpve.Server) *nodes.Service {
	t.Helper()
	c, cleanup := mock.NewClient()
	t.Cleanup(cleanup)
	return nodes.NewService(c, version.Capabilities{})
}

func newServiceAndTasks(t *testing.T, mock *mockpve.Server) (*nodes.Service, *tasks.Service) {
	t.Helper()
	c, cleanup := mock.NewClient()
	t.Cleanup(cleanup)
	return nodes.NewService(c, version.Capabilities{}), tasks.NewService(c)
}

func TestListInterfaces(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddInterface(testNode, "vmbr0", "bridge")
	mock.AddInterface(testNode, "eno1", "eth")
	svc := newService(t, mock)

	ifaces, err := svc.ListInterfaces(context.Background(), testNode)
	if err != nil {
		t.Fatalf("ListInterfaces: %v", err)
	}
	if len(ifaces) != 2 {
		t.Fatalf("ListInterfaces returned %d, want 2", len(ifaces))
	}
}

func TestGetInterface(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddInterface(testNode, "vmbr0", "bridge")
	svc := newService(t, mock)

	i, err := svc.GetInterface(context.Background(), testNode, "vmbr0")
	if err != nil {
		t.Fatalf("GetInterface: %v", err)
	}
	if i.Iface != "vmbr0" || i.Type != nodes.InterfaceTypeBridge {
		t.Errorf("interface = %+v, want iface=vmbr0 type=bridge", i)
	}
}

func TestGetInterfaceNotFound(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)

	if _, err := svc.GetInterface(context.Background(), testNode, "ghost0"); !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("GetInterface(ghost) = %v, want ErrNotFound", err)
	}
}

func TestCreateInterface(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	err := svc.CreateInterface(ctx, testNode, &nodes.InterfaceSpec{
		Iface:       "vmbr1",
		Type:        nodes.InterfaceTypeBridge,
		Address:     "10.0.0.1/24",
		BridgePorts: "eno2",
		VLANAware:   true,
		Autostart:   true,
	})
	if err != nil {
		t.Fatalf("CreateInterface: %v", err)
	}

	i, err := svc.GetInterface(ctx, testNode, "vmbr1")
	if err != nil {
		t.Fatalf("GetInterface after create: %v", err)
	}
	if i.Address != "10.0.0.1/24" || i.BridgePorts != "eno2" || !bool(i.VLANAware) {
		t.Errorf("created interface = %+v, want address/ports/vlan-aware set", i)
	}
}

func TestCreateInterfaceValidation(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.CreateInterface(ctx, testNode, nil); err == nil {
		t.Error("CreateInterface(nil) error = nil, want non-nil")
	}
	if err := svc.CreateInterface(ctx, testNode, &nodes.InterfaceSpec{Type: nodes.InterfaceTypeBridge}); err == nil {
		t.Error("CreateInterface(no iface) error = nil, want non-nil")
	}
	if err := svc.CreateInterface(ctx, testNode, &nodes.InterfaceSpec{Iface: "vmbr1"}); err == nil {
		t.Error("CreateInterface(no type) error = nil, want non-nil")
	}
}

func TestUpdateInterface(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddInterface(testNode, "vmbr0", "bridge")
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.UpdateInterface(ctx, testNode, "vmbr0", &nodes.InterfaceUpdate{
		Address: "192.168.1.1/24",
	}); err != nil {
		t.Fatalf("UpdateInterface: %v", err)
	}

	i, err := svc.GetInterface(ctx, testNode, "vmbr0")
	if err != nil {
		t.Fatalf("GetInterface after update: %v", err)
	}
	if i.Address != "192.168.1.1/24" {
		t.Errorf("address after update = %q, want 192.168.1.1/24", i.Address)
	}
}

func TestDeleteInterface(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddInterface(testNode, "vmbr9", "bridge")
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.DeleteInterface(ctx, testNode, "vmbr9"); err != nil {
		t.Fatalf("DeleteInterface: %v", err)
	}
	if _, err := svc.GetInterface(ctx, testNode, "vmbr9"); !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("GetInterface after delete = %v, want ErrNotFound", err)
	}
}

func TestApplyNetworkConfig(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc, ts := newServiceAndTasks(t, mock)
	ctx := context.Background()

	ref, err := svc.ApplyNetworkConfig(ctx, testNode)
	if err != nil {
		t.Fatalf("ApplyNetworkConfig: %v", err)
	}
	// The mock returns a reload task; the ref must be non-zero and awaitable.
	if ref.IsZero() {
		t.Fatal("ApplyNetworkConfig returned a zero Ref, want a reload task")
	}
	st, err := ts.Wait(ctx, ref)
	if err != nil {
		t.Fatalf("Wait(apply): %v", err)
	}
	if !st.OK() {
		t.Errorf("apply task exit = %q, want OK", st.ExitStatus)
	}
}
