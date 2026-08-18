package nodes

import "testing"

// TestACMEPathsReal pins every ACME path to the string the real 9.2 apidoc
// serves (the TestCephPathsReal / TestHAStatusPathsReal pattern). A wrong path
// is the failure mode that shipped the SDN fabrics and HA DLB surfaces broken
// (INV-0004), so these are asserted in-repo rather than discovered live.
func TestACMEPathsReal(t *testing.T) {
	t.Parallel()
	tests := []struct{ got, want string }{
		{acmeAccountsPath(), "/cluster/acme/account"},
		{acmeAccountPath("default"), "/cluster/acme/account/default"},
		{acmePluginsPath(), "/cluster/acme/plugins"},
		{acmePluginPath("cf-lab"), "/cluster/acme/plugins/cf-lab"},
		{acmeChallengeSchemaPath(), "/cluster/acme/challenge-schema"},
		{acmeDirectoriesPath(), "/cluster/acme/directories"},
		{acmeMetaPath(), "/cluster/acme/meta"},
		{nodeCertACMEPath("pve"), "/nodes/pve/certificates/acme/certificate"},
		{nodeConfigPath("pve"), "/nodes/pve/config"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("path = %q, want %q", tt.got, tt.want)
		}
	}
}
