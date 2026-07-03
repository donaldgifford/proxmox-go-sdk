// Package api is the low-level Proxmox VE transport that every service package
// is built on. It owns the HTTP round-trip, the PVE JSON envelope, cluster
// failover, retry/backoff, and authentication; it knows nothing about specific
// endpoints (qemu, storage, …) beyond the /api2/json prefix.
//
// # Client
//
// [New] returns a [Client] targeting one cluster:
//
//	c, err := api.New("pve1.example.com", api.TokenCredentials("root@pam!sdk", secret),
//		api.WithClusterEndpoints(api.Endpoint{Name: "pve2", Address: "pve2.example.com", Priority: 1}),
//		api.WithInsecureSkipVerify(true), // self-signed / IP hosts
//	)
//
// A single Client is safe for concurrent use. [Client.DoRequest] performs one
// call: it expands the path, authenticates, sends, and decodes the PVE
// envelope ({"data":…,"errors":…,"message":…}) into out. Writes (POST/PUT/
// DELETE) are form-encoded (application/x-www-form-urlencoded) and carry the
// CSRF token; reads send no body. [Client.ExpandPath] only prepends /api2/json
// — services compose the node/vmid segments themselves.
//
// # Authentication
//
// Three strategies implement the unexported Credentials interface; the caller
// picks exactly one (precedence ticket > API token > user/pass is resolved at
// the call site):
//
//   - [TokenCredentials] — a PVE API token; never expires, no CSRF needed.
//   - [TicketCredentials] — a pre-minted ticket + CSRF token whose lifetime the
//     caller owns; the SDK will not refresh it.
//   - [UserCredentials] — username/password (+ optional TOTP); the transport
//     mints a ticket on first use and refreshes it before the 2h expiry.
//
// Refresh is lazy (checked before each request) and reactive: when the server
// rejects a ticket as expired, the transport re-mints and replays the call
// once.
//
// # Failover and retry
//
// Failover is sticky and passive (see OQ-2): a per-endpoint [RetryPolicy]
// retries transient errors with jittered exponential backoff, and only on
// exhaustion does the active endpoint advance to the next by priority — it
// never drifts back. Non-transient errors (4xx, task failures) return
// immediately without retry or rotation.
//
// # Errors
//
// Every failure resolves to the SDK taxonomy in
// [github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr]: callers branch with
// errors.Is / errors.As, not by string-matching.
package api
