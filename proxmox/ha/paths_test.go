package ha

import "testing"

// TestHAStatusPathsReal pins the literal /cluster/ha/status and
// /cluster/ha/resources/{sid} action paths to the strings confirmed in the
// real 9.2 apidoc (the IMPL-0004 TestFabricPathsReal pattern) — a refactor
// that drifts a path away from the real API must fail here, in-repo.
func TestHAStatusPathsReal(t *testing.T) {
	t.Parallel()
	tests := []struct{ got, want string }{
		{haStatusCurrentPath(), "/cluster/ha/status/current"},
		{haStatusManagerPath(), "/cluster/ha/status/manager_status"},
		{haStatusArmPath(), "/cluster/ha/status/arm-ha"},
		{haStatusDisarmPath(), "/cluster/ha/status/disarm-ha"},
		// url.PathEscape leaves ':' intact — it is legal in a path segment,
		// and the unescaped form is what real PVE serves.
		{haResourceMigratePath("vm:100"), "/cluster/ha/resources/vm:100/migrate"},
		{haResourceRelocatePath("ct:101"), "/cluster/ha/resources/ct:101/relocate"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("path = %q, want %q", tt.got, tt.want)
		}
	}
}
