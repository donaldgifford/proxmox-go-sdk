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
	// mountpoint is server-generated: PVE materializes it into a zfspool
	// entry on create (the mock mirrors that), so the read carries a key the
	// writer never sent.
	for key, want := range map[string]string{
		"sparse": "1", "blocksize": "16k", "preallocation": "metadata",
		"mountpoint": "/fast/vm",
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

// TestGetDatastoreMissing500 pins the real-PVE wart the hoomlab consumer
// found live (2026-08-27): GET /storage/{id} for a missing entry is HTTP 500
// "storage '<id>' does not exist", NOT 404 — so the error must not resolve to
// ErrNotFound, and existence checks must scan ListDatastores instead.
func TestGetDatastoreMissing500(t *testing.T) {
	t.Parallel()
	svc := newService(t, mockpve.New())

	_, err := svc.GetDatastore(context.Background(), "ghost")
	if err == nil {
		t.Fatal("GetDatastore(ghost): want error, got nil")
	}
	if errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("GetDatastore(ghost) = %v; real PVE answers 500, this must NOT be ErrNotFound", err)
	}
	var pe *pverr.Error
	if !errors.As(err, &pe) || pe.Status != 500 {
		t.Fatalf("GetDatastore(ghost) = %v, want *pverr.Error with status 500", err)
	}
	if !strings.Contains(pe.Message, "does not exist") {
		t.Errorf("message = %q, want the does-not-exist wording", pe.Message)
	}
}

// TestCreateDatastoreExplicitMountpoint pins that materialization never
// overrides a submitted value: a zfspool create carrying its own mountpoint
// reads back exactly that.
func TestCreateDatastoreExplicitMountpoint(t *testing.T) {
	t.Parallel()
	svc := newService(t, mockpve.New())
	ctx := context.Background()

	if _, err := svc.CreateDatastore(ctx, &storage.DatastoreSpec{
		Storage: "tank-vm", Type: "zfspool", Pool: "tank/vm",
		Extra: map[string]string{"mountpoint": "/mnt/custom"},
	}); err != nil {
		t.Fatalf("CreateDatastore: %v", err)
	}
	d, err := svc.GetDatastore(ctx, "tank-vm")
	if err != nil {
		t.Fatalf("GetDatastore: %v", err)
	}
	if got := d.Extra["mountpoint"]; got != "/mnt/custom" {
		t.Errorf("Extra[mountpoint] = %q, want the submitted /mnt/custom", got)
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

// TestUpdateDatastoreErrors covers the update guards and the unknown-id 404
// (the mock's shape — the real PUT missing-id status is unobserved; only the
// GET wart is live-confirmed).
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

// TestDatastoreConvergeShape runs the consumer's converge sequence — the
// exact loop hoomlab's `pve storage` stage runs per configured entry —
// against the mock unmodified: probe existence by scanning the index (the
// by-id GET cannot distinguish missing from server error on real PVE — the
// 500 wart), create the zfspool entry, read it back comparing list-valued
// options AS SETS, correct drift via an update guarded by that read's
// digest, and tear down. Passing here is the seeding-not-stubbing proof: the
// consumer's logic can run against mockpve before the consumer exists.
func TestDatastoreConvergeShape(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	// Probe: absent means create. Existence comes from the index scan —
	// hoomlab's actual approach after finding the by-id 500 wart live.
	if datastoreInList(t, svc, "fast-vm") {
		t.Fatal("probe before create: fast-vm already listed")
	}

	if _, err := svc.CreateDatastore(ctx, &storage.DatastoreSpec{
		Storage:   "fast-vm",
		Type:      "zfspool",
		Pool:      "fast/vm",
		Sparse:    true,
		Blocksize: "16k",
		Content:   []string{"images", "rootdir"},
		Nodes:     []string{"pve2", "pve1"},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// The read is the converge comparison input; field-level reflection is
	// TestCreateDatastoreReflected's job — here only the set-compare step of
	// the sequence is asserted.
	got, err := svc.GetDatastore(ctx, "fast-vm")
	if err != nil {
		t.Fatalf("read after create: %v", err)
	}
	wantSet(t, "content", got.Content, "images", "rootdir")
	wantSet(t, "nodes", got.Nodes, "pve1", "pve2")

	// Drift-correct: narrow the content restriction, guarded by the digest
	// of the read the decision came from.
	if _, err := svc.UpdateDatastore(ctx, "fast-vm", &storage.DatastoreUpdate{
		Content: []string{"images"},
		Digest:  got.Digest,
	}); err != nil {
		t.Fatalf("drift-correct update: %v", err)
	}
	corrected, err := svc.GetDatastore(ctx, "fast-vm")
	if err != nil {
		t.Fatalf("read after update: %v", err)
	}
	wantSet(t, "content", corrected.Content, "images")

	if err := svc.DeleteDatastore(ctx, "fast-vm"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if datastoreInList(t, svc, "fast-vm") {
		t.Fatal("probe after delete: fast-vm still listed")
	}
}

// datastoreInList reports whether id appears in ListDatastores — the
// existence probe consumers must use, since the by-id GET answers a missing
// entry with the 500 wart rather than a 404.
func datastoreInList(t *testing.T, svc *storage.Service, id string) bool {
	t.Helper()
	ds, err := svc.ListDatastores(context.Background())
	if err != nil {
		t.Fatalf("ListDatastores: %v", err)
	}
	for i := range ds {
		if ds[i].Storage == id {
			return true
		}
	}
	return false
}

// TestDeleteDatastore pins delete-then-gone. A by-id get afterwards errors
// with the real-PVE 500 wart (never ErrNotFound — see
// TestGetDatastoreMissing500), so gone-ness is confirmed by the index scan;
// a repeat delete resolves to ErrNotFound (the mock's shape; the real
// missing-id DELETE shape is unobserved), and the empty-id guard holds.
func TestDeleteDatastore(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddStorage("local", "dir", "iso", 0, 0)
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.DeleteDatastore(ctx, "local"); err != nil {
		t.Fatalf("DeleteDatastore: %v", err)
	}
	ds, err := svc.ListDatastores(ctx)
	if err != nil {
		t.Fatalf("ListDatastores after delete: %v", err)
	}
	if len(ds) != 0 {
		t.Errorf("list after delete = %+v, want empty", ds)
	}
	if _, err := svc.GetDatastore(ctx, "local"); err == nil || errors.Is(err, pverr.ErrNotFound) {
		t.Errorf("get after delete = %v, want the non-ErrNotFound 500 wart", err)
	}
	if err := svc.DeleteDatastore(ctx, "local"); !errors.Is(err, pverr.ErrNotFound) {
		t.Errorf("repeat delete = %v, want ErrNotFound", err)
	}
	if err := svc.DeleteDatastore(ctx, ""); !errors.Is(err, svcutil.ErrMissingField) {
		t.Errorf("empty id = %v, want ErrMissingField", err)
	}
}
