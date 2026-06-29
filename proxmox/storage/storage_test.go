package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/storage"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/version"
)

const testNode = "pve"

func newService(t *testing.T, mock *mockpve.Server) *storage.Service {
	t.Helper()
	c, cleanup := mock.NewClient()
	t.Cleanup(cleanup)
	return storage.NewService(c, version.Capabilities{})
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
