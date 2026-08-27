package storage

import "testing"

// TestStoragePathsReal pins every storage path helper to the string the real
// 9.2 apidoc serves (the TestHAStatusPathsReal / TestCephPathsReal pattern).
// The datastore config writes (DESIGN-0007) reuse the read paths — POST on
// the collection, PUT/DELETE on the entry — so pinning /storage and
// /storage/{id} pins all five config operations. The volid rows pin the
// PathEscape behaviour nodeVolumePath documents: a volid is a single path
// segment where the slash escapes to %2F but the colon stays LITERAL (the
// same url.PathEscape finding as the HA /resources/vm:100 paths — a colon is
// valid inside a path segment). The literal-colon form is what the live ISO
// upload run exercised.
func TestStoragePathsReal(t *testing.T) {
	t.Parallel()
	tests := []struct{ got, want string }{
		{datastoresPath(), "/storage"},
		{datastorePath("local"), "/storage/local"},
		{datastorePath("fast-vm"), "/storage/fast-vm"},
		{nodeStoragesPath("pve"), "/nodes/pve/storage"},
		{nodeStoragePath("pve", "local"), "/nodes/pve/storage/local"},
		{nodeContentPath("pve", "local"), "/nodes/pve/storage/local/content"},
		{
			nodeVolumePath("pve", "local", "local:iso/debian-12.iso"),
			"/nodes/pve/storage/local/content/local:iso%2Fdebian-12.iso",
		},
		{
			nodeVolumePath("pve", "local-lvm", "local-lvm:vm-100-disk-0"),
			"/nodes/pve/storage/local-lvm/content/local-lvm:vm-100-disk-0",
		},
		{nodeZFSPath("pve"), "/nodes/pve/disks/zfs"},
		{nodeZFSPoolPath("pve", "tank"), "/nodes/pve/disks/zfs/tank"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("path = %q, want %q", tt.got, tt.want)
		}
	}
}
