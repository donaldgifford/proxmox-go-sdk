package storage_test

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/storage"
)

// wantSet fails the test unless the comma-joined csv holds exactly want as a
// set. List-valued datastore options (content, nodes) do not preserve
// submission order on read-back, so this is how a consumer must compare them.
func wantSet(t *testing.T, field, csv string, want ...string) {
	t.Helper()
	got := strings.Split(csv, ",")
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("%s = %q, want set %v", field, csv, want)
	}
}

// TestCreateDatastoreReflected drives the full write shape through the mock:
// the result echoes {storage, type}, and a subsequent get reflects typed
// fields, set-normalized lists (submitted unsorted), and Extra keys — both
// the SDK-typed-but-mock-untyped ones (sparse, blocksize) and a genuinely
// unmodelled one (preallocation) — plus a non-empty digest.
func TestCreateDatastoreReflected(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	res, err := svc.CreateDatastore(ctx, &storage.DatastoreSpec{
		Storage:   "fast-vm",
		Type:      "zfspool",
		Pool:      "fast/vm",
		Sparse:    true,
		Blocksize: "16k",
		Content:   []string{"rootdir", "images"}, // unsorted on purpose.
		Nodes:     []string{"pve2", "pve1"},      // unsorted on purpose.
		Extra:     map[string]string{"preallocation": "metadata"},
	})
	if err != nil {
		t.Fatalf("CreateDatastore: %v", err)
	}
	if res.Storage != "fast-vm" || res.Type != "zfspool" {
		t.Errorf("result = %+v, want storage=fast-vm type=zfspool", res)
	}
	if res.Config != nil {
		t.Errorf("result.Config = %v, want nil (the mock fabricates no config)", res.Config)
	}

	d, err := svc.GetDatastore(ctx, "fast-vm")
	if err != nil {
		t.Fatalf("GetDatastore after create: %v", err)
	}
	if d.Storage != "fast-vm" || d.Type != "zfspool" || d.Pool != "fast/vm" {
		t.Errorf("read = %+v, want the created identity and pool", d)
	}
	wantSet(t, "content", d.Content, "images", "rootdir")
	wantSet(t, "nodes", d.Nodes, "pve1", "pve2")
	// The mock emits list options sorted; pin that the unsorted submission
	// reads back normalized (the set-comparison rule made visible).
	if d.Content != "images,rootdir" || d.Nodes != "pve1,pve2" {
		t.Errorf("content/nodes = %q/%q, want sorted images,rootdir / pve1,pve2",
			d.Content, d.Nodes)
	}
	for key, want := range map[string]string{
		"sparse": "1", "blocksize": "16k", "preallocation": "metadata",
	} {
		if got := d.Extra[key]; got != want {
			t.Errorf("Extra[%q] = %q, want %q", key, got, want)
		}
	}
	if d.Digest == "" {
		t.Error("read after create carries no digest")
	}

	ds, err := svc.ListDatastores(ctx)
	if err != nil {
		t.Fatalf("ListDatastores after create: %v", err)
	}
	if len(ds) != 1 || ds[0].Storage != "fast-vm" {
		t.Errorf("list = %+v, want the one created entry", ds)
	}
}

// TestCreateDatastoreDuplicate pins the duplicate-id rejection: a 400 carrying
// PVE's "already defined" message, not a silent overwrite.
func TestCreateDatastoreDuplicate(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddStorage("local", "dir", "iso", 0, 0)
	svc := newService(t, mock)

	_, err := svc.CreateDatastore(context.Background(),
		&storage.DatastoreSpec{Storage: "local", Type: "dir", Path: "/var/lib/vz"})
	if err == nil {
		t.Fatal("duplicate CreateDatastore: want error, got nil")
	}
	var pe *pverr.Error
	if !errors.As(err, &pe) || pe.Status != 400 {
		t.Fatalf("duplicate create error = %v, want *pverr.Error with status 400", err)
	}
	if !strings.Contains(pe.Message, "already defined") {
		t.Errorf("message = %q, want the already-defined wording", pe.Message)
	}
}

// TestCreateDatastoreGuards pins the client-side guards: nil spec and missing
// required fields fail before any request.
func TestCreateDatastoreGuards(t *testing.T) {
	t.Parallel()
	svc := newService(t, mockpve.New())
	ctx := context.Background()

	if _, err := svc.CreateDatastore(ctx, nil); !errors.Is(err, svcutil.ErrNilSpec) {
		t.Errorf("nil spec = %v, want ErrNilSpec", err)
	}
	if _, err := svc.CreateDatastore(ctx, &storage.DatastoreSpec{Type: "dir"}); !errors.Is(err, svcutil.ErrMissingField) {
		t.Errorf("missing storage = %v, want ErrMissingField", err)
	}
	if _, err := svc.CreateDatastore(ctx, &storage.DatastoreSpec{Storage: "x"}); !errors.Is(err, svcutil.ErrMissingField) {
		t.Errorf("missing type = %v, want ErrMissingField", err)
	}
}

// TestUpdateDatastore covers the partial-write semantics without a digest:
// set-keys apply, delete clears named keys (before set-keys), and everything
// the update did not name survives.
func TestUpdateDatastore(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	if _, err := svc.CreateDatastore(ctx, &storage.DatastoreSpec{
		Storage: "scratch", Type: "dir", Path: "/srv/scratch",
		Content: []string{"iso"}, Nodes: []string{"pve1"},
	}); err != nil {
		t.Fatalf("seed create: %v", err)
	}

	res, err := svc.UpdateDatastore(ctx, "scratch", &storage.DatastoreUpdate{
		Content: []string{"iso", "vztmpl"},
		Delete:  []string{"nodes"},
	})
	if err != nil {
		t.Fatalf("UpdateDatastore: %v", err)
	}
	if res.Storage != "scratch" || res.Type != "dir" {
		t.Errorf("result = %+v, want storage=scratch type=dir", res)
	}

	d, err := svc.GetDatastore(ctx, "scratch")
	if err != nil {
		t.Fatalf("GetDatastore after update: %v", err)
	}
	wantSet(t, "content", d.Content, "iso", "vztmpl")
	if d.Nodes != "" {
		t.Errorf("nodes = %q, want cleared by delete", d.Nodes)
	}
	if d.Path != "/srv/scratch" {
		t.Errorf("path = %q, want untouched /srv/scratch", d.Path)
	}
}

// TestUpdateDatastoreDigestGuard exercises the guard in both directions on
// one storage: an update carrying the digest of the read that informed it is
// accepted, the write changes the digest, and replaying the now-stale digest
// is refused with PVE's modified-configuration 400.
func TestUpdateDatastoreDigestGuard(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddStorage("local", "dir", "iso", 0, 0)
	svc := newService(t, mock)
	ctx := context.Background()

	before, err := svc.GetDatastore(ctx, "local")
	if err != nil {
		t.Fatalf("GetDatastore: %v", err)
	}
	if before.Digest == "" {
		t.Fatal("seeded read carries no digest")
	}

	if _, err := svc.UpdateDatastore(ctx, "local", &storage.DatastoreUpdate{
		Content: []string{"iso", "backup"},
		Digest:  before.Digest,
	}); err != nil {
		t.Fatalf("update with fresh digest: %v", err)
	}

	after, err := svc.GetDatastore(ctx, "local")
	if err != nil {
		t.Fatalf("GetDatastore after update: %v", err)
	}
	if after.Digest == before.Digest {
		t.Error("digest unchanged across a write; the guard would never trip")
	}

	_, err = svc.UpdateDatastore(ctx, "local", &storage.DatastoreUpdate{
		Content: []string{"iso"},
		Digest:  before.Digest, // stale: a write happened since that read.
	})
	if err == nil {
		t.Fatal("update with stale digest: want error, got nil")
	}
	var pe *pverr.Error
	if !errors.As(err, &pe) || pe.Status != 400 {
		t.Fatalf("stale digest error = %v, want *pverr.Error with status 400", err)
	}
	if !strings.Contains(pe.Message, "modified configuration") {
		t.Errorf("message = %q, want the modified-configuration wording", pe.Message)
	}
}

// TestUpdateDatastoreCreateFixed pins that a create-fixed parameter riding
// Extra on an update is refused: PVE's update schema has no path/type/… — the
// mock rejects them the way real PVE's schema validation would.
func TestUpdateDatastoreCreateFixed(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddStorage("local", "dir", "iso", 0, 0)
	svc := newService(t, mock)

	_, err := svc.UpdateDatastore(context.Background(), "local",
		&storage.DatastoreUpdate{Extra: map[string]string{"path": "/elsewhere"}})
	if err == nil {
		t.Fatal("update with create-fixed key: want error, got nil")
	}
	var pe *pverr.Error
	if !errors.As(err, &pe) || pe.Status != 400 {
		t.Fatalf("create-fixed key error = %v, want *pverr.Error with status 400", err)
	}
}

// TestUpdateDatastoreErrors covers the update guards and the unknown-id 404.
func TestUpdateDatastoreErrors(t *testing.T) {
	t.Parallel()
	svc := newService(t, mockpve.New())
	ctx := context.Background()

	if _, err := svc.UpdateDatastore(ctx, "", &storage.DatastoreUpdate{}); !errors.Is(err, svcutil.ErrMissingField) {
		t.Errorf("empty id = %v, want ErrMissingField", err)
	}
	if _, err := svc.UpdateDatastore(ctx, "local", nil); !errors.Is(err, svcutil.ErrNilSpec) {
		t.Errorf("nil update = %v, want ErrNilSpec", err)
	}
	if _, err := svc.UpdateDatastore(ctx, "ghost", &storage.DatastoreUpdate{
		Content: []string{"iso"},
	}); !errors.Is(err, pverr.ErrNotFound) {
		t.Errorf("unknown id = %v, want ErrNotFound", err)
	}
}

// TestDeleteDatastore pins delete-then-gone: the entry 404s afterwards and
// the error resolves to pverr.ErrNotFound via errors.Is, as do a repeat
// delete and the empty-id guard.
func TestDeleteDatastore(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddStorage("local", "dir", "iso", 0, 0)
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.DeleteDatastore(ctx, "local"); err != nil {
		t.Fatalf("DeleteDatastore: %v", err)
	}
	if _, err := svc.GetDatastore(ctx, "local"); !errors.Is(err, pverr.ErrNotFound) {
		t.Errorf("get after delete = %v, want ErrNotFound", err)
	}
	if err := svc.DeleteDatastore(ctx, "local"); !errors.Is(err, pverr.ErrNotFound) {
		t.Errorf("repeat delete = %v, want ErrNotFound", err)
	}
	if err := svc.DeleteDatastore(ctx, ""); !errors.Is(err, svcutil.ErrMissingField) {
		t.Errorf("empty id = %v, want ErrMissingField", err)
	}
}
