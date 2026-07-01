package version

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
)

// Minimum is the lowest PVE release this SDK supports (ADR-0002): the 9.0
// floor. NewClient rejects anything below it.
const (
	MinimumMajor = 9
	MinimumMinor = 0
)

// MinimumProxmoxVersion is the human-readable form of the supported floor, used
// in error messages.
const MinimumProxmoxVersion = "9.0"

// Capabilities is the fetched-once version snapshot every service consults
// before attempting a minor-gated operation. It is seeded by NewClient from
// GET /version and is safe to copy (value semantics, no pointers). The zero
// value reports version 0.0.0 and gates everything off.
type Capabilities struct {
	major int
	minor int
	patch int
}

// Parse reads a PVE version string ("9.0.3", "9.0", or a packaged form like
// "9.0.0-1") into Capabilities. Trailing non-numeric suffixes on each component
// are tolerated; a missing minor or patch defaults to zero.
func Parse(s string) (Capabilities, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Capabilities{}, errors.New("version: empty version string")
	}

	fields := strings.SplitN(s, ".", 3)
	major, err := leadingInt(fields[0])
	if err != nil {
		return Capabilities{}, fmt.Errorf("version: parse %q: %w", s, err)
	}
	caps := Capabilities{major: major}
	if len(fields) > 1 {
		if caps.minor, err = leadingInt(fields[1]); err != nil {
			return Capabilities{}, fmt.Errorf("version: parse %q: %w", s, err)
		}
	}
	if len(fields) > 2 {
		if caps.patch, err = leadingInt(fields[2]); err != nil {
			return Capabilities{}, fmt.Errorf("version: parse %q: %w", s, err)
		}
	}
	return caps, nil
}

// AtLeast reports whether the running version is >= major.minor. Patch is not
// considered — PVE gates capabilities at the minor level.
func (c Capabilities) AtLeast(major, minor int) bool {
	if c.major != major {
		return c.major > major
	}
	return c.minor >= minor
}

// MeetsMinimum reports whether the version satisfies the SDK's 9.0 floor.
func (c Capabilities) MeetsMinimum() bool {
	return c.AtLeast(MinimumMajor, MinimumMinor)
}

// DynamicLoadBalancer gates the continuous CRS rebalancing controls (9.2+).
func (c Capabilities) DynamicLoadBalancer() bool { return c.AtLeast(9, 2) }

// OCITemplates gates pulling OCI images as LXC templates (9.1+).
func (c Capabilities) OCITemplates() bool { return c.AtLeast(9, 1) }

// VolumeChainSnapshots gates snapshots-as-volume-chains on storage that lacks
// native snapshots — thick LVM and Directory/NFS/CIFS via qcow2 chains (9.1+;
// tech-preview maturing). ZFS/btrfs/LVM-thin have native snapshots and do not
// need this gate.
func (c Capabilities) VolumeChainSnapshots() bool { return c.AtLeast(9, 1) }

// TPMStateSnapshots gates snapshotting a VM's TPM state on file-based storage
// (NFS/CIFS/directory) (9.1+).
func (c Capabilities) TPMStateSnapshots() bool { return c.AtLeast(9, 1) }

// TokenSecretRotation gates regenerating an API token secret in place (9.2+).
func (c Capabilities) TokenSecretRotation() bool { return c.AtLeast(9, 2) }

// ZFSRAIDZExpansion gates online RAIDZ vdev expansion — adding a disk to an
// existing RAIDZ group (OpenZFS 2.3, shipped in PVE 9.2). The minor floor is the
// SDK's best estimate; whether PVE exposes it over REST at all is unconfirmed
// without a live node (see storage.ExpandRAIDZ).
func (c Capabilities) ZFSRAIDZExpansion() bool { return c.AtLeast(9, 2) }

// HAClusterSwitch gates the cluster-wide HA arm/disarm switch (9.2+). Whether
// PVE exposes it over REST is unconfirmed without a live node (see ha.ArmHA).
func (c Capabilities) HAClusterSwitch() bool { return c.AtLeast(9, 2) }

// SDNFabrics gates SDN fabrics — the OpenFabric/OSPF routing layer under the SDN
// stack (9.0+). It is baseline on every supported release; the gate exists for
// symmetry and for callers constructing a Service with an unset version.
func (c Capabilities) SDNFabrics() bool { return c.AtLeast(9, 0) }

// SDNAdvancedFabrics gates the newer fabric protocols and options layered on the
// 9.0 fabric baseline — BGP, WireGuard-encrypted underlays, and IPv6 underlays
// (9.2+). The exact 9.2 fabric surface is unconfirmed without a live node (see
// sdn.CreateFabric).
func (c Capabilities) SDNAdvancedFabrics() bool { return c.AtLeast(9, 2) }

// OverlappingIPSets gates the firewall's overlapping-IPSet support — the 9.1
// rework that also exposes IPSet rename (see firewall.RenameIPSet).
func (c Capabilities) OverlappingIPSets() bool { return c.AtLeast(9, 1) }

// ClearTokenComment gates explicitly clearing an API token's comment (9.1+); on
// older releases an empty comment was ignored (see access.ClearTokenComment).
func (c Capabilities) ClearTokenComment() bool { return c.AtLeast(9, 1) }

// Require returns nil when the version is at least minVersion ("9.2"), and a
// pverr.ErrUnsupported-wrapped error naming the feature otherwise. Services use
// it to gate minor-specific operations with a uniform error.
func (c Capabilities) Require(feature, minVersion string) error {
	want, err := Parse(minVersion)
	if err != nil {
		return fmt.Errorf("version: require %s: %w", feature, err)
	}
	if c.AtLeast(want.major, want.minor) {
		return nil
	}
	return fmt.Errorf("%s requires PVE %s (have %s): %w", feature, minVersion, c, pverr.ErrUnsupported)
}

// String renders the version as "major.minor.patch".
func (c Capabilities) String() string {
	return strconv.Itoa(c.major) + "." + strconv.Itoa(c.minor) + "." + strconv.Itoa(c.patch)
}

// leadingInt parses the leading run of ASCII digits in s, so "0-1" yields 0. It
// errors when s has no leading digit.
func leadingInt(s string) (int, error) {
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, fmt.Errorf("no leading digits in %q", s)
	}
	return strconv.Atoi(s[:end])
}
