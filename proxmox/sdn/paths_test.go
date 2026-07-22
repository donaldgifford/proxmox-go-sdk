package sdn

import "testing"

// TestFabricPathsReal pins the fabric request paths to the literal strings the
// real 9.2 apidoc exposes (INV-0004). The original flat /cluster/sdn/fabrics
// guess shipped unnoticed because nothing in-repo asserted the wire paths —
// this test makes any future path regression visible without a live node.
func TestFabricPathsReal(t *testing.T) {
	t.Parallel()
	cases := []struct{ got, want string }{
		{sdnFabricsPath(), "/cluster/sdn/fabrics/fabric"},
		{sdnFabricPath("fab0"), "/cluster/sdn/fabrics/fabric/fab0"},
		{sdnFabricNodesPath("fab0"), "/cluster/sdn/fabrics/node/fab0"},
		{sdnFabricNodePath("fab0", "pve1"), "/cluster/sdn/fabrics/node/fab0/pve1"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("fabric path = %q, want %q", c.got, c.want)
		}
	}
}

// TestNodeSDNStatusPaths pins the node-scoped live-status paths to the real
// 9.2 apidoc surface.
func TestNodeSDNStatusPaths(t *testing.T) {
	t.Parallel()
	cases := []struct{ got, want string }{
		{nodeSDNZonesPath("pve1"), "/nodes/pve1/sdn/zones"},
		{nodeSDNZoneContentPath("pve1", "z0"), "/nodes/pve1/sdn/zones/z0/content"},
		{nodeSDNZoneBridgesPath("pve1", "z0"), "/nodes/pve1/sdn/zones/z0/bridges"},
		{nodeSDNZoneIPVRFPath("pve1", "z0"), "/nodes/pve1/sdn/zones/z0/ip-vrf"},
		{nodeSDNVNetMACVRFPath("pve1", "v0"), "/nodes/pve1/sdn/vnets/v0/mac-vrf"},
		{nodeSDNFabricInterfacesPath("pve1", "f0"), "/nodes/pve1/sdn/fabrics/f0/interfaces"},
		{nodeSDNFabricNeighborsPath("pve1", "f0"), "/nodes/pve1/sdn/fabrics/f0/neighbors"},
		{nodeSDNFabricRoutesPath("pve1", "f0"), "/nodes/pve1/sdn/fabrics/f0/routes"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("status path = %q, want %q", c.got, c.want)
		}
	}
}
