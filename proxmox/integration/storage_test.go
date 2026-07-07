//go:build integration

package integration

import (
	"os"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/storage"
)

// TestISOUpload covers the upload half of the Phase 3 criterion: streaming an
// ISO to a storage. It is gated on PVE_TEST_ISO_PATH (a local ISO file) plus a
// target storage that allows "iso" content — PVE_TEST_ISO_STORAGE, falling back
// to PVE_TEST_STORAGE (the guest-disk storage is often ZFS/LVM, which does not
// take ISOs, so the two are separable). Skips otherwise.
func TestISOUpload(t *testing.T) {
	c := newClient(t)
	node := testNode()

	store := os.Getenv(envTestISOStorage)
	if store == "" {
		store = os.Getenv(envTestStorage)
	}
	isoPath := os.Getenv(envTestISOPath)
	if store == "" || isoPath == "" {
		t.Skipf("ISO upload disabled (set %s or %s, and %s)", envTestISOStorage, envTestStorage, envTestISOPath)
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

// There is no live volume-snapshot test: PVE exposes no storage-level
// volume-snapshot REST endpoint (verified against a live 9.2 node — the content
// API stops at .../content/{volume}). storage.VolumeSnapshots and friends return
// pverr.ErrUnsupported without touching the node; that behaviour is guarded by
// the unit test TestVolumeSnapshotsUnsupported. A volume is snapshotted through
// its owning guest, which the QEMU/LXC lifecycle tests already exercise.
