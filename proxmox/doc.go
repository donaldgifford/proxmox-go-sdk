// Package proxmox is the unified Proxmox VE 9.x SDK client. It holds only the
// [Client] and its construction options; the typed request/response shapes live
// in the per-domain service packages (proxmox/qemu, proxmox/storage, …), and
// the shared primitives and error taxonomy live in proxmox/types and
// proxmox/pverr, imported directly rather than re-exported (DESIGN-0001 / OQ-1).
//
// # Construction
//
// [NewClient] targets one cluster, authenticates with an api.Credentials
// strategy, and fetches /version once to seed [version.Capabilities], rejecting
// any release below the 9.0 floor:
//
//	c, err := proxmox.NewClient(ctx, "pve1.example.com",
//		api.TokenCredentials("root@pam!sdk", secret),
//		proxmox.WithInsecureSkipVerify(true),
//		proxmox.WithClusterEndpoints(api.Endpoint{Name: "pve2", Address: "pve2.example.com", Priority: 1}),
//	)
//	if errors.Is(err, pverr.ErrUnsupported) {
//		// the cluster is older than PVE 9.0
//	}
//
// # Accessors
//
// A Client exposes typed services. [Client.Version], [Client.Capabilities], and
// [Client.Tasks] are available now; [Client.API] is the raw transport escape
// hatch. The per-domain accessors (QEMU, Storage, HA, …) land as those services
// are implemented in later phases.
//
// One Client is safe for concurrent use.
package proxmox
