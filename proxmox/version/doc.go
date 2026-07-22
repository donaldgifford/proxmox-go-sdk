// Package version reports the running PVE release and gates per-minor 9.x
// capabilities against the SDK's 9.0 floor (ADR-0002).
//
// # Capabilities
//
// [Capabilities] is the fetched-once version snapshot the unified client seeds
// from GET /version and hands to every service. Services consult it before
// attempting a minor-gated operation:
//
//	if !caps.HAClusterSwitch() {
//		return caps.Require("HA arm/disarm", "9.2") // -> pverr.ErrUnsupported
//	}
//
// [Capabilities.AtLeast] is the primitive (major.minor comparison; patch is not
// considered, matching how PVE gates features). The named gates
// ([Capabilities.OCITemplates] 9.1+, [Capabilities.HAClusterSwitch] and
// [Capabilities.TokenSecretRotation] 9.2+) are thin wrappers over it, and
// [Capabilities.Require] produces a uniform pverr.ErrUnsupported-wrapped error
// for ad-hoc gates.
//
// # Service
//
// [Service] fetches version data over an [github.com/donaldgifford/proxmox-go-sdk/proxmox/api.Client].
// [Service.Capabilities] parses /version and rejects any release below
// [MinimumProxmoxVersion] with pverr.ErrUnsupported — this is the check
// NewClient runs once at construction.
package version
