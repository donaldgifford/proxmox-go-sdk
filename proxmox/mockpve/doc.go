// Package mockpve is an in-memory Proxmox VE responder for tests. It serves the
// PVE JSON envelope over an [net/http.Handler] so the SDK (and SDK consumers)
// can exercise real request/response paths without a live cluster. The same
// responder runs standalone via cmd/mockpve.
//
// # Usage
//
// Seed a Server, get a wired api.Client, and call the SDK against it:
//
//	mock := mockpve.New()
//	mock.SeedVersion("9.2.1", "9.2", "abc")
//	c, cleanup := mock.NewClient()
//	defer cleanup()
//	caps, _ := version.NewService(c).Capabilities(ctx) // talks to the mock
//
// [Server.NewClient] starts an httptest server and returns an api.Client plus a
// cleanup func; [Server.Serve] exposes the raw httptest.Server for cases that
// need a custom client (e.g. UserCredentials against POST /access/ticket).
//
// # Seeding
//
// The Add/Seed/Finish methods drive the model: [Server.SeedVersion],
// [Server.AddNode], [Server.AddUser], and [Server.AddTask] /
// [Server.FinishTask] (a task added running can be transitioned to stopped,
// OK or failed, so the SDK's task waiter can be tested end to end).
//
// # Extending
//
// [Server.RegisterHandler] mounts additional routes (Go 1.22 ServeMux
// patterns); [WithCache] is the seam through which a recorded 9.x response
// corpus will later seed the mock (OQ-10) without redesign.
//
// # Route surface
//
// A Server serves the foundation endpoints (/version, /access/ticket, and the
// task status/log reads) plus the per-service routes of every SDK service —
// compute, storage, HA, node networking and administration, SDN, firewall,
// cluster, access, metrics, Ceph, PBS, and console.
//
// [Server.Routes] enumerates them: the raw registered ServeMux patterns, method
// and /api2/json prefix included. That list is the numerator of the SDK's
// API-coverage report (docs/COVERAGE.md), which measures it against the real
// PVE endpoint baseline and fails CI on any mock route that PVE does not
// actually serve — the mock mirrors real PVE, and the report is what keeps that
// claim honest.
package mockpve
