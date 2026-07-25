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
// # Shape of a report
//
// [Build] classifies every baseline endpoint into one of four [State] values —
// covered, stub, out of scope, gap — groups them by service with [ServiceFor],
// and totals them; [Report.Markdown] renders that. The service map is a static
// path-prefix table with no catch-alls, so an API family no service claims lands
// in [Unassigned] and a PVE minor adding one surfaces instead of quietly joining
// somebody's table.
//
// The only hand-written input is [Annotations], a small exceptions file: the
// deliberate stubs, the capabilities covered over ssh rather than REST, the
// decided non-goals, and the fabrication guard's allowlist. Everything else is
// derived, because a hand-maintained map of the PVE API is exactly what drifted
// in the first place (INV-0004).
//
// # Checks
//
// Two checks give the report teeth, and both run whether the report is being
// generated or verified:
//
//   - Drift ([CheckDrift]): a regenerated report must byte-match the committed
//     one, so a PR that changes the surface must carry the regenerated doc.
//   - Fabrication guard ([Report.Check]): no mock route may reference an endpoint
//     absent from the baseline. The mock mirrors real PVE; a route with no real
//     endpoint behind it means the SDK is testing against an API that does not
//     exist, which is exactly how the SDN-fabrics and HA Dynamic-Load-Balancer
//     paths shipped wrong (INV-0004). Genuine exceptions are allowlisted, with a
//     reason, in the annotations file. The same check rejects annotations that
//     have gone stale, so the exceptions file cannot rot.
//
// The package is deliberately transport-free and side-effect-free: it takes
// bytes and slices, and returns a report plus findings. Reading files, writing
// docs/COVERAGE.md, and setting exit codes are the command's job.
package coverage
