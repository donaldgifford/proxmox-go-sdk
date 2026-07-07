package storage_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/storage"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/version"
)

const testNode = "pve"

func newService(t *testing.T, mock *mockpve.Server) *storage.Service {
	t.Helper()
	svc, _ := newServiceAndTasks(t, mock)
	return svc
}

func newServiceAndTasks(t *testing.T, mock *mockpve.Server) (*storage.Service, *tasks.Service) {
	t.Helper()
	c, cleanup := mock.NewClient()
	t.Cleanup(cleanup)
	return storage.NewService(c, version.Capabilities{}), tasks.NewService(c)
}

func newCappedService(t *testing.T, mock *mockpve.Server, ver string) *storage.Service {
	t.Helper()
	c, cleanup := mock.NewClient()
	t.Cleanup(cleanup)
	caps, err := version.Parse(ver)
	if err != nil {
		t.Fatalf("version.Parse(%q): %v", ver, err)
	}
	return storage.NewService(c, caps)
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

func TestListDatastores(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddStorage("local", "dir", "iso,vztmpl,backup", 100<<30, 20<<30)
	mock.AddStorage("local-lvm", "lvmthin", "images,rootdir", 500<<30, 100<<30)
	svc := newService(t, mock)

	ds, err := svc.ListDatastores(context.Background())
	if err != nil {
		t.Fatalf("ListDatastores: %v", err)
	}
	if len(ds) != 2 {
		t.Fatalf("ListDatastores returned %d, want 2", len(ds))
	}
}

func TestGetDatastore(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddStorage("local", "dir", "iso,vztmpl,backup", 100<<30, 20<<30)
	svc := newService(t, mock)

	d, err := svc.GetDatastore(context.Background(), "local")
	if err != nil {
		t.Fatalf("GetDatastore: %v", err)
	}
	if d.Storage != "local" || d.Type != "dir" {
		t.Errorf("datastore = %+v, want storage=local type=dir", d)
	}
	if d.Content != "iso,vztmpl,backup" {
		t.Errorf("content = %q, want iso,vztmpl,backup", d.Content)
	}
}

func TestGetDatastoreNotFound(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)

	if _, err := svc.GetDatastore(context.Background(), "ghost"); !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("GetDatastore(ghost) = %v, want ErrNotFound", err)
	}
}

func TestListNodeStorage(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddStorage("local", "dir", "iso", 100<<30, 20<<30)
	svc := newService(t, mock)

	st, err := svc.ListNodeStorage(context.Background(), testNode)
	if err != nil {
		t.Fatalf("ListNodeStorage: %v", err)
	}
	if len(st) != 1 {
		t.Fatalf("ListNodeStorage returned %d, want 1", len(st))
	}
}

func TestNodeStorageStatus(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddStorage("local", "dir", "iso", 100<<30, 20<<30)
	svc := newService(t, mock)

	st, err := svc.NodeStorageStatus(context.Background(), testNode, "local")
	if err != nil {
		t.Fatalf("NodeStorageStatus: %v", err)
	}
	if st.Total != 100<<30 || st.Used != 20<<30 {
		t.Errorf("status total/used = %d/%d, want %d/%d", st.Total, st.Used, int64(100<<30), int64(20<<30))
	}
	if st.Avail != (100<<30)-(20<<30) {
		t.Errorf("avail = %d, want %d", st.Avail, int64((100<<30)-(20<<30)))
	}
}

func TestListContent(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddVolume(testNode, "local", "local:iso/debian-12.iso", "iso", "iso", 600<<20)
	mock.AddVolume(testNode, "local", "local:vztmpl/alpine.tar.zst", "vztmpl", "tzst", 3<<20)
	mock.AddVolume(testNode, "local", "local:backup/vzdump-qemu-100.vma.zst", "backup", "vma.zst", 2<<30)
	svc := newService(t, mock)

	all, err := svc.ListContent(context.Background(), testNode, "local")
	if err != nil {
		t.Fatalf("ListContent: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("ListContent returned %d, want 3", len(all))
	}

	isos, err := svc.ListContent(context.Background(), testNode, "local", storage.WithContentType("iso"))
	if err != nil {
		t.Fatalf("ListContent(iso): %v", err)
	}
	if len(isos) != 1 || isos[0].Volid != "local:iso/debian-12.iso" {
		t.Fatalf("ListContent(iso) = %+v, want the one ISO", isos)
	}
}

func TestGetVolume(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddVolume(testNode, "local", "local:iso/debian-12.iso", "iso", "iso", 600<<20)
	svc := newService(t, mock)

	v, err := svc.GetVolume(context.Background(), testNode, "local", "local:iso/debian-12.iso")
	if err != nil {
		t.Fatalf("GetVolume: %v", err)
	}
	if v.Volid != "local:iso/debian-12.iso" || v.Size != 600<<20 {
		t.Errorf("volume = %+v, want the debian ISO at 600MiB", v)
	}
}

func TestGetVolumeNotFound(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)

	_, err := svc.GetVolume(context.Background(), testNode, "local", "local:iso/ghost.iso")
	if !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("GetVolume(ghost) = %v, want ErrNotFound", err)
	}
}

func TestCreateVolume(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	volid, err := svc.CreateVolume(ctx, testNode, "local-lvm", &storage.VolumeCreateSpec{
		Filename: "vm-100-disk-1",
		Size:     "10G",
		Format:   "raw",
		VMID:     100,
	})
	if err != nil {
		t.Fatalf("CreateVolume: %v", err)
	}
	if volid != "local-lvm:vm-100-disk-1" {
		t.Errorf("CreateVolume volid = %q, want local-lvm:vm-100-disk-1", volid)
	}

	all, err := svc.ListContent(ctx, testNode, "local-lvm")
	if err != nil {
		t.Fatalf("ListContent: %v", err)
	}
	if len(all) != 1 || all[0].Volid != volid {
		t.Fatalf("ListContent after create = %+v, want the new volume", all)
	}
}

func TestCreateVolumeValidation(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	if _, err := svc.CreateVolume(ctx, testNode, "local-lvm", nil); err == nil {
		t.Error("CreateVolume(nil) error = nil, want non-nil")
	}
	if _, err := svc.CreateVolume(ctx, testNode, "local-lvm", &storage.VolumeCreateSpec{Size: "10G"}); err == nil {
		t.Error("CreateVolume(no filename) error = nil, want non-nil")
	}
	if _, err := svc.CreateVolume(ctx, testNode, "local-lvm", &storage.VolumeCreateSpec{Filename: "x"}); err == nil {
		t.Error("CreateVolume(no size) error = nil, want non-nil")
	}
}

func TestDeleteVolume(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddVolume(testNode, "local-lvm", "local-lvm:vm-100-disk-0", "images", "raw", 8<<30)
	svc, ts := newServiceAndTasks(t, mock)
	ctx := context.Background()

	ref, err := svc.DeleteVolume(ctx, testNode, "local-lvm", "local-lvm:vm-100-disk-0")
	if err != nil {
		t.Fatalf("DeleteVolume: %v", err)
	}
	awaitOK(t, ts, ref)

	if _, err := svc.GetVolume(ctx, testNode, "local-lvm", "local-lvm:vm-100-disk-0"); !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("GetVolume after delete = %v, want ErrNotFound", err)
	}
}

func TestDeleteVolumeNotFound(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)

	_, err := svc.DeleteVolume(context.Background(), testNode, "local-lvm", "local-lvm:ghost")
	if !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("DeleteVolume(ghost) = %v, want ErrNotFound", err)
	}
}

// TestVolumeSnapshotsUnsupported pins the reclassification: PVE exposes no
// storage-level volume-snapshot REST endpoint (verified against a live 9.2 node,
// where the content API stops at .../content/{volume}), so all three ops return
// pverr.ErrUnsupported on every version — including 9.2, and regardless of the
// spec — directing callers at the guest snapshot API. See storage.VolumeSnapshots.
func TestVolumeSnapshotsUnsupported(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddVolume(testNode, "dir", "dir:vm-100-disk-0.qcow2", "images", "qcow2", 8<<30)
	svc := newCappedService(t, mock, "9.2") // newest supported minor.
	ctx := context.Background()
	const volid = "dir:vm-100-disk-0.qcow2"

	_, listErr := svc.VolumeSnapshots(ctx, testNode, "dir", volid)
	if !errors.Is(listErr, pverr.ErrUnsupported) {
		t.Errorf("VolumeSnapshots = %v, want ErrUnsupported", listErr)
	}
	_, createErr := svc.CreateVolumeSnapshot(ctx, testNode, "dir", volid, &storage.VolumeSnapshotSpec{Name: "s1"})
	if !errors.Is(createErr, pverr.ErrUnsupported) {
		t.Errorf("CreateVolumeSnapshot = %v, want ErrUnsupported", createErr)
	}
	// Even a nil spec resolves to ErrUnsupported (no endpoint to validate against).
	if _, err := svc.CreateVolumeSnapshot(ctx, testNode, "dir", volid, nil); !errors.Is(err, pverr.ErrUnsupported) {
		t.Errorf("CreateVolumeSnapshot(nil) = %v, want ErrUnsupported", err)
	}
	_, delErr := svc.DeleteVolumeSnapshot(ctx, testNode, "dir", volid, "s1")
	if !errors.Is(delErr, pverr.ErrUnsupported) {
		t.Errorf("DeleteVolumeSnapshot = %v, want ErrUnsupported", delErr)
	}
}

func TestUploadISO(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc, ts := newServiceAndTasks(t, mock)
	ctx := context.Background()

	payload := "FAKE-ISO-BYTES-0123456789"
	ref, err := svc.UploadISO(ctx, testNode, "local", &storage.UploadSpec{
		Filename: "debian-12.iso",
		Reader:   strings.NewReader(payload),
	})
	if err != nil {
		t.Fatalf("UploadISO: %v", err)
	}
	awaitOK(t, ts, ref)

	all, err := svc.ListContent(ctx, testNode, "local", storage.WithContentType("iso"))
	if err != nil {
		t.Fatalf("ListContent: %v", err)
	}
	if len(all) != 1 || all[0].Volid != "local:iso/debian-12.iso" {
		t.Fatalf("ListContent after upload = %+v, want the uploaded ISO", all)
	}
	if all[0].Size != int64(len(payload)) {
		t.Errorf("uploaded size = %d, want %d (the streamed byte count)", all[0].Size, len(payload))
	}
}

func TestUploadDiskImage(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc, ts := newServiceAndTasks(t, mock)
	ctx := context.Background()

	ref, err := svc.UploadDiskImage(ctx, testNode, "local", &storage.UploadSpec{
		Filename: "cloudimg.qcow2",
		Reader:   strings.NewReader("QCOW2-FAKE"),
	})
	if err != nil {
		t.Fatalf("UploadDiskImage: %v", err)
	}
	awaitOK(t, ts, ref)

	all, err := svc.ListContent(ctx, testNode, "local", storage.WithContentType("import"))
	if err != nil {
		t.Fatalf("ListContent: %v", err)
	}
	if len(all) != 1 || all[0].Volid != "local:import/cloudimg.qcow2" {
		t.Fatalf("ListContent after upload = %+v, want the uploaded image", all)
	}
}

func TestUploadValidation(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	if _, err := svc.UploadISO(ctx, testNode, "local", nil); err == nil {
		t.Error("UploadISO(nil) error = nil, want non-nil")
	}
	if _, err := svc.UploadISO(ctx, testNode, "local", &storage.UploadSpec{Reader: strings.NewReader("x")}); err == nil {
		t.Error("UploadISO(no filename) error = nil, want non-nil")
	}
	if _, err := svc.UploadISO(ctx, testNode, "local", &storage.UploadSpec{Filename: "a.iso"}); err == nil {
		t.Error("UploadISO(no reader) error = nil, want non-nil")
	}
}

func TestListZFSPools(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddZFSPool(testNode, "tank", 4<<40, 3<<40)
	mock.AddZFSPool(testNode, "rpool", 200<<30, 50<<30)
	svc := newService(t, mock)

	pools, err := svc.ListZFSPools(context.Background(), testNode)
	if err != nil {
		t.Fatalf("ListZFSPools: %v", err)
	}
	if len(pools) != 2 {
		t.Fatalf("ListZFSPools returned %d, want 2", len(pools))
	}
}

func TestGetZFSPool(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddZFSPool(testNode, "tank", 4<<40, 3<<40)
	svc := newService(t, mock)

	pool, err := svc.GetZFSPool(context.Background(), testNode, "tank")
	if err != nil {
		t.Fatalf("GetZFSPool: %v", err)
	}
	if pool.Name != "tank" || pool.State != "ONLINE" {
		t.Errorf("pool = %+v, want name=tank state=ONLINE", pool)
	}
}

func TestGetZFSPoolNotFound(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)

	if _, err := svc.GetZFSPool(context.Background(), testNode, "ghost"); !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("GetZFSPool(ghost) = %v, want ErrNotFound", err)
	}
}

func TestCreateZFSPool(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc, ts := newServiceAndTasks(t, mock)
	ctx := context.Background()

	ref, err := svc.CreateZFSPool(ctx, testNode, &storage.ZFSPoolSpec{
		Name:      "tank",
		RAIDLevel: "raidz",
		Devices:   []string{"/dev/sdb", "/dev/sdc", "/dev/sdd"},
	})
	if err != nil {
		t.Fatalf("CreateZFSPool: %v", err)
	}
	awaitOK(t, ts, ref)

	pools, err := svc.ListZFSPools(ctx, testNode)
	if err != nil {
		t.Fatalf("ListZFSPools: %v", err)
	}
	if len(pools) != 1 || pools[0].Name != "tank" {
		t.Fatalf("ListZFSPools after create = %+v, want the new pool", pools)
	}
}

func TestCreateZFSPoolValidation(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	if _, err := svc.CreateZFSPool(ctx, testNode, nil); err == nil {
		t.Error("CreateZFSPool(nil) error = nil, want non-nil")
	}
	if _, err := svc.CreateZFSPool(ctx, testNode, &storage.ZFSPoolSpec{RAIDLevel: "raidz", Devices: []string{"/dev/sdb"}}); err == nil {
		t.Error("CreateZFSPool(no name) error = nil, want non-nil")
	}
	if _, err := svc.CreateZFSPool(ctx, testNode, &storage.ZFSPoolSpec{Name: "tank", Devices: []string{"/dev/sdb"}}); err == nil {
		t.Error("CreateZFSPool(no raidlevel) error = nil, want non-nil")
	}
	if _, err := svc.CreateZFSPool(ctx, testNode, &storage.ZFSPoolSpec{Name: "tank", RAIDLevel: "raidz"}); err == nil {
		t.Error("CreateZFSPool(no devices) error = nil, want non-nil")
	}
}

func TestExpandRAIDZGatedPre92(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newCappedService(t, mock, "9.1") // below the 9.2 gate.
	spec := &storage.RAIDZExpandSpec{Pool: "tank", Device: "/dev/sde"}

	_, err := svc.ExpandRAIDZ(context.Background(), testNode, spec)
	if !errors.Is(err, pverr.ErrUnsupported) {
		t.Fatalf("ExpandRAIDZ on 9.1 = %v, want ErrUnsupported", err)
	}
}

func TestExpandRAIDZNoRESTEndpoint(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newCappedService(t, mock, "9.2") // gate satisfied.
	spec := &storage.RAIDZExpandSpec{Pool: "tank", Device: "/dev/sde"}

	// Even on 9.2 there is no PVE REST endpoint for RAIDZ expansion: the op
	// reports ErrUnsupported and points at the ssh side-channel.
	_, err := svc.ExpandRAIDZ(context.Background(), testNode, spec)
	if !errors.Is(err, pverr.ErrUnsupported) {
		t.Fatalf("ExpandRAIDZ on 9.2 = %v, want ErrUnsupported (no REST endpoint)", err)
	}
	if _, err := svc.ExpandRAIDZ(context.Background(), testNode, nil); err == nil {
		t.Error("ExpandRAIDZ(nil) error = nil, want non-nil")
	}
}
