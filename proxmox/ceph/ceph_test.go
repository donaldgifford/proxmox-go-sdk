package ceph_test

import (
	"context"
	"errors"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/ceph"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/version"
)

const testNode = "pve"

func newService(t *testing.T, mock *mockpve.Server) *ceph.Service {
	t.Helper()
	c, cleanup := mock.NewClient()
	t.Cleanup(cleanup)
	return ceph.NewService(c, version.Capabilities{})
}

func newServiceAndTasks(t *testing.T, mock *mockpve.Server) (*ceph.Service, *tasks.Service) {
	t.Helper()
	c, cleanup := mock.NewClient()
	t.Cleanup(cleanup)
	return ceph.NewService(c, version.Capabilities{}), tasks.NewService(c)
}

func TestListPools(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddCephPool("rbd")
	mock.AddCephPool("cephfs_data")
	svc := newService(t, mock)

	pools, err := svc.ListPools(context.Background(), testNode)
	if err != nil {
		t.Fatalf("ListPools: %v", err)
	}
	if len(pools) != 2 {
		t.Fatalf("ListPools returned %d, want 2", len(pools))
	}
}

func TestGetPool(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddCephPool("rbd")
	svc := newService(t, mock)

	p, err := svc.GetPool(context.Background(), testNode, "rbd")
	if err != nil {
		t.Fatalf("GetPool: %v", err)
	}
	if p.Name != "rbd" || p.Size != 3 {
		t.Errorf("pool = %+v, want name=rbd size=3", p)
	}
}

func TestGetPoolNotFound(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)

	if _, err := svc.GetPool(context.Background(), testNode, "ghost"); !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("GetPool(ghost) = %v, want ErrNotFound", err)
	}
}

func TestCreateAndDeletePool(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc, ts := newServiceAndTasks(t, mock)
	ctx := context.Background()

	ref, err := svc.CreatePool(ctx, testNode, &ceph.PoolSpec{Name: "newpool", Size: 3, Application: "rbd"})
	if err != nil {
		t.Fatalf("CreatePool: %v", err)
	}
	if _, err := ts.Wait(ctx, ref); err != nil {
		t.Fatalf("Wait(create pool): %v", err)
	}
	if _, err := svc.GetPool(ctx, testNode, "newpool"); err != nil {
		t.Fatalf("GetPool after create: %v", err)
	}

	ref, err = svc.DeletePool(ctx, testNode, "newpool")
	if err != nil {
		t.Fatalf("DeletePool: %v", err)
	}
	if _, err := ts.Wait(ctx, ref); err != nil {
		t.Fatalf("Wait(delete pool): %v", err)
	}
	if _, err := svc.GetPool(ctx, testNode, "newpool"); !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("GetPool after delete = %v, want ErrNotFound", err)
	}
}

func TestCreatePoolValidation(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	if _, err := svc.CreatePool(ctx, testNode, nil); err == nil {
		t.Error("CreatePool(nil) error = nil, want non-nil")
	}
	if _, err := svc.CreatePool(ctx, testNode, &ceph.PoolSpec{}); err == nil {
		t.Error("CreatePool(no name) error = nil, want non-nil")
	}
}

func TestListOSDs(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddCephOSD(0, "pve")
	mock.AddCephOSD(1, "pve")
	svc := newService(t, mock)

	tree, err := svc.ListOSDs(context.Background(), testNode)
	if err != nil {
		t.Fatalf("ListOSDs: %v", err)
	}
	if tree.Root == nil || tree.Root.Type != "root" {
		t.Fatalf("OSD tree root = %+v, want a root node", tree.Root)
	}
	if len(tree.Root.Children) != 1 || len(tree.Root.Children[0].Children) != 2 {
		t.Errorf("OSD tree = %+v, want one host with two OSDs", tree.Root)
	}
}

func TestCreateAndDestroyOSD(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc, ts := newServiceAndTasks(t, mock)
	ctx := context.Background()

	ref, err := svc.CreateOSD(ctx, testNode, &ceph.OSDSpec{DevPath: "/dev/sdb"})
	if err != nil {
		t.Fatalf("CreateOSD: %v", err)
	}
	if _, err := ts.Wait(ctx, ref); err != nil {
		t.Fatalf("Wait(create osd): %v", err)
	}

	ref, err = svc.DestroyOSD(ctx, testNode, 0)
	if err != nil {
		t.Fatalf("DestroyOSD: %v", err)
	}
	if _, err := ts.Wait(ctx, ref); err != nil {
		t.Fatalf("Wait(destroy osd): %v", err)
	}
}

func TestCreateOSDValidation(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)

	if _, err := svc.CreateOSD(context.Background(), testNode, &ceph.OSDSpec{}); err == nil {
		t.Error("CreateOSD(no dev) error = nil, want non-nil")
	}
}

func TestGetStatusAndConfig(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	st, err := svc.GetStatus(ctx, testNode)
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if st.Health == nil || st.Health.Status != "HEALTH_OK" {
		t.Errorf("status health = %+v, want HEALTH_OK", st.Health)
	}

	cfg, err := svc.GetClusterConfig(ctx, testNode)
	if err != nil {
		t.Fatalf("GetClusterConfig: %v", err)
	}
	if cfg == "" {
		t.Error("GetClusterConfig returned empty config")
	}
}

func TestMirroringUnsupported(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	if _, err := svc.GetMirrorStatus(ctx, testNode, "rbd"); !errors.Is(err, pverr.ErrUnsupported) {
		t.Errorf("GetMirrorStatus = %v, want ErrUnsupported", err)
	}
	if err := svc.EnableMirroring(ctx, testNode, &ceph.MirrorSpec{Pool: "rbd", Mode: ceph.MirrorModePool}); !errors.Is(err, pverr.ErrUnsupported) {
		t.Errorf("EnableMirroring = %v, want ErrUnsupported", err)
	}
	if err := svc.DisableMirroring(ctx, testNode, "rbd"); !errors.Is(err, pverr.ErrUnsupported) {
		t.Errorf("DisableMirroring = %v, want ErrUnsupported", err)
	}
}
