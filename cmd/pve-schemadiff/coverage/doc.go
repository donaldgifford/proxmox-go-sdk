// Package coverage measures the SDK's REST surface against the real Proxmox VE
// API and renders docs/COVERAGE.md, the committed coverage report (DESIGN-0005).
//
// The arithmetic has two halves. The denominator is the committed endpoint
// baseline parsed from a genuine apidoc.js dump (the sibling schema package's
// baseline.json — "what real PVE serves"). The numerator is
// mockpve.Server.Routes(), the mock's own route table: every REST op the SDK
// ships is unit-tested against a registered mock route, so the mock's routes
// are a trustworthy map of the covered surface, and nothing new has to be
// maintained by hand.
//
// Comparing the two requires normalizing both sides — see [Key]: the mock's
// patterns carry the /api2/json prefix and their own wildcard names, which do
// not always match PVE's (`{vmid}` vs `{id}`, `{fabric}` vs `{fabric_id}`).
//
// # Checks
//
// Two checks give the report teeth, and both run whether the report is being
// generated or verified:
//
//   - Drift: a regenerated report must byte-match the committed one, so a PR
//     that changes the surface must carry the regenerated doc.
//   - Fabrication guard: no mock route may reference an endpoint absent from the
//     baseline. The mock mirrors real PVE; a route with no real endpoint behind
//     it means the SDK is testing against an API that does not exist, which is
//     exactly how the SDN-fabrics and HA Dynamic-Load-Balancer paths shipped
//     wrong (INV-0004). Genuine exceptions are allowlisted, with a reason, in
//     the annotations file.
//
// The package is deliberately transport-free and side-effect-free: it takes
// bytes and slices, and returns a report plus findings. Reading files, writing
// docs/COVERAGE.md, and setting exit codes are the command's job.
package coverage
