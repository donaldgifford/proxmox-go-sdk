//go:build integration

package integration

import (
	"context"
	"os"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/storage"
)

// TestISOUpload covers the upload half of the Phase 3 criterion: streaming an
// ISO to a storage. It is gated on PVE_TEST_STORAGE + PVE_TEST_ISO_PATH (a local
// ISO file) and skips otherwise.
func TestISOUpload(t *testing.T) {
	c := newClient(t)
	node := testNode()

	store := os.Getenv(envTestStorage)
	isoPath := os.Getenv(envTestISOPath)
	if store == "" || isoPath == "" {
		t.Skipf("ISO upload disabled (set %s and %s)", envTestStorage, envTestISOPath)
	}

	f, err := os.Open(isoPath) //nolint:gosec // path is an operator-supplied test fixture.
	if err != nil {
		t.Fatalf("open ISO %q: %v", isoPath, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			t.Logf("close ISO: %v", cerr)
		}
	}()

	info, err := f.Stat()
	if err != nil {
		t.Fatalf("stat ISO: %v", err)
	}
	ref, err := c.Storage().UploadISO(testCtx(t), node, store, &storage.UploadSpec{
		Filename: info.Name(),
		Reader:   f,
	})
	if err != nil {
		t.Fatalf("UploadISO: %v", err)
	}
	mustSucceed(t, c.Tasks(), ref, "upload iso")
}

// TestVolumeSnapshotLifecycle covers the volume-chain snapshot half of the
// Phase 3 criterion: snapshot an existing volume where supported, then clean up.
// Gated on PVE_TEST_STORAGE + PVE_TEST_VOLID and skips otherwise. The snapshot
// is deleted in cleanup even if the create step's wait fails.
func TestVolumeSnapshotLifecycle(t *testing.T) {
	c := newClient(t)
	node := testNode()

	store := os.Getenv(envTestStorage)
	volid := os.Getenv(envTestVolID)
	if store == "" || volid == "" {
		t.Skipf("volume snapshot disabled (set %s and %s)", envTestStorage, envTestVolID)
	}

	s := c.Storage()
	ts := c.Tasks()
	const snap = "itest0"

	t.Cleanup(func() {
		dref, derr := s.DeleteVolumeSnapshot(context.Background(), node, store, volid, snap)
		if derr != nil {
			t.Logf("cleanup DeleteVolumeSnapshot: %v", derr)
			return
		}
		if _, werr := ts.Wait(context.Background(), dref); werr != nil {
			t.Logf("cleanup Wait(delete snapshot): %v", werr)
		}
	})

	ref, err := s.CreateVolumeSnapshot(testCtx(t), node, store, volid, &storage.VolumeSnapshotSpec{Name: snap})
	if err != nil {
		t.Fatalf("CreateVolumeSnapshot: %v", err)
	}
	mustSucceed(t, ts, ref, "create volume snapshot")
}
