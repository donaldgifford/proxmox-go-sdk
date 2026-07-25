package ceph

import "testing"

// TestCephPathsReal pins every Ceph path to the string the real 9.2 apidoc
// serves (the TestHAStatusPathsReal / TestFabricPathsReal pattern). Two of these
// were wrong for a whole phase — the pool collection is singular and there is no
// /ceph/config — so a refactor that drifts them back must fail here, in-repo,
// rather than on a live cluster.
func TestCephPathsReal(t *testing.T) {
	t.Parallel()
	tests := []struct{ got, want string }{
		{nodeCephPoolsPath("pve"), "/nodes/pve/ceph/pool"},
		{nodeCephPoolPath("pve", "rbd"), "/nodes/pve/ceph/pool/rbd"},
		{nodeCephOSDsPath("pve"), "/nodes/pve/ceph/osd"},
		{nodeCephOSDPath("pve", 3), "/nodes/pve/ceph/osd/3"},
		{nodeCephStatusPath("pve"), "/nodes/pve/ceph/status"},
		{nodeCephConfigPath("pve"), "/nodes/pve/ceph/cfg/raw"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("path = %q, want %q", tt.got, tt.want)
		}
	}
}
