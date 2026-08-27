//go:build integration

package integration

import (
	"errors"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/storage"
)

// datastoreTestID is the scratch cluster storage entry the lifecycle test
// creates and removes. The test refuses to adopt a pre-existing entry with
// this id — leftovers mean a previous run failed mid-way and a human should
// look before anything deletes cluster config.
const datastoreTestID = "sdk-datastore-test"

// TestDatastoreLifecycle drives the DESIGN-0007 write surface against a live
// cluster: probe (absent) → create → read back (sets + digest) → drift-correct
// with the read's digest → stale-digest negative check → delete → verify gone.
// With PVE_TEST_DATASTORE_POOL set it creates a zfspool entry over that
// EXISTING dataset; otherwise a dir entry (PVE mkdirs the path). Either way it
// only writes cluster CONFIG — deleting the entry never touches data — but the
// config file is cluster-wide state, so the test is gated on
// PVE_TEST_DATASTORE=1 as an explicit opt-in.
//
// It has never been run. IMPL-0008 shipped mock-verified under the repo's
// live-verification deferral (DESIGN-0007 OQ-6: r740a is production), so this
// is a prepared harness, not a verified one — there is no cassette and CI
// cannot replay it. Consumer verification happens in hoomlab (IMPL-0009);
// whoever runs this first should expect to find something.
func TestDatastoreLifecycle(t *testing.T) {
	if os.Getenv(envReplay) == "1" {
		t.Skip("TestDatastoreLifecycle has no cassette by design (never run live)")
	}
	if os.Getenv(envTestDatastore) != "1" {
		t.Skipf("datastore lifecycle disabled (set %s=1; it edits the cluster "+
			"storage config — optionally set %s to an existing ZFS dataset for a "+
			"zfspool entry, else a dir entry is used)", envTestDatastore, envTestDatastorePool)
	}
	c := newClient(t)
	svc := c.Storage()
	ctx := testCtx(t)
	node := testNode()

	// Probe: the id must be absent, and absent must resolve to ErrNotFound —
	// the same branch condition hoomlab's create-if-missing uses.
	if _, err := svc.GetDatastore(ctx, datastoreTestID); !errors.Is(err, pverr.ErrNotFound) {
		if err == nil {
			t.Fatalf("storage entry %q already exists — refusing to adopt it; "+
				"remove it by hand before rerunning", datastoreTestID)
		}
		t.Fatalf("probe GetDatastore(%s): %v (want ErrNotFound)", datastoreTestID, err)
	}

	spec := &storage.DatastoreSpec{
		Storage: datastoreTestID,
		Content: []string{"images", "rootdir"},
		Nodes:   []string{node},
	}
	if pool := os.Getenv(envTestDatastorePool); pool != "" {
		spec.Type = "zfspool"
		spec.Pool = pool
		spec.Sparse = true
	} else {
		spec.Type = "dir"
		spec.Path = "/var/lib/" + datastoreTestID
		spec.Content = []string{"images", "iso"} // rootdir is LXC-on-dir; keep the dir case simple.
	}
	res, err := svc.CreateDatastore(ctx, spec)
	if err != nil {
		t.Fatalf("CreateDatastore(%s): %v", datastoreTestID, err)
	}
	t.Cleanup(func() {
		cctx, cancel := cleanupCtx()
		defer cancel()
		if err := svc.DeleteDatastore(cctx, datastoreTestID); err != nil && !errors.Is(err, pverr.ErrNotFound) {
			t.Errorf("cleanup DeleteDatastore(%s): %v", datastoreTestID, err)
		}
	})
	if res.Storage != datastoreTestID || res.Type != spec.Type {
		t.Errorf("create result = %+v, want storage=%s type=%s", res, datastoreTestID, spec.Type)
	}

	// Read back. List-valued options are sets: PVE does not preserve
	// submission order, so compare them as sets, never as strings.
	before, err := svc.GetDatastore(ctx, datastoreTestID)
	if err != nil {
		t.Fatalf("GetDatastore after create: %v", err)
	}
	assertCSVSet(t, "content", before.Content, spec.Content)
	assertCSVSet(t, "nodes", before.Nodes, spec.Nodes)
	if before.Digest == "" {
		t.Error("read after create carries no digest — the guarded-write idiom needs one")
	}

	// Drift-correct: narrow the content restriction, guarded by the digest of
	// the read the decision came from.
	if _, err := svc.UpdateDatastore(ctx, datastoreTestID, &storage.DatastoreUpdate{
		Content: []string{"images"},
		Digest:  before.Digest,
	}); err != nil {
		t.Fatalf("UpdateDatastore with fresh digest: %v", err)
	}
	after, err := svc.GetDatastore(ctx, datastoreTestID)
	if err != nil {
		t.Fatalf("GetDatastore after update: %v", err)
	}
	assertCSVSet(t, "content", after.Content, []string{"images"})
	if after.Digest == before.Digest {
		t.Error("digest unchanged across a write — the stale-guard could never trip")
	}

	// Stale-digest negative check: replaying the pre-update digest must be
	// refused, not applied.
	if _, err := svc.UpdateDatastore(ctx, datastoreTestID, &storage.DatastoreUpdate{
		Content: []string{"images", "rootdir"},
		Digest:  before.Digest,
	}); err == nil {
		t.Error("UpdateDatastore with stale digest succeeded — want a refusal")
	}

	if err := svc.DeleteDatastore(ctx, datastoreTestID); err != nil {
		t.Fatalf("DeleteDatastore(%s): %v", datastoreTestID, err)
	}
	if _, err := svc.GetDatastore(ctx, datastoreTestID); !errors.Is(err, pverr.ErrNotFound) {
		t.Errorf("GetDatastore after delete = %v, want ErrNotFound", err)
	}
}

// assertCSVSet fails the test unless the comma-joined csv equals want as a set.
func assertCSVSet(t *testing.T, field, csv string, want []string) {
	t.Helper()
	got := strings.Split(csv, ",")
	sort.Strings(got)
	w := append([]string(nil), want...)
	sort.Strings(w)
	if strings.Join(got, ",") != strings.Join(w, ",") {
		t.Errorf("%s = %q, want set %v", field, csv, want)
	}
}
