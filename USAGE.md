# Usage

`proxmox-go-sdk` is an idiomatic Go client for the Proxmox VE 9.x REST API. It
exposes a unified `proxmox.Client` plus typed, per-domain service packages
(`qemu`, `lxc`, `storage`, `ha`, `sdn`, …).

> **Requires Proxmox VE 9.0 or newer** (ADR-0002). Earlier releases are not
> supported.

## Install

The SDK is one Go module; pin a released tag.

```sh
go get github.com/donaldgifford/proxmox-go-sdk@latest
```

Then import the root client and whichever service packages you use:

```go
import (
    "github.com/donaldgifford/proxmox-go-sdk/proxmox"
    "github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
    "github.com/donaldgifford/proxmox-go-sdk/proxmox/qemu"
)
```

## Quickstart

Build a client, reach a service, and await the returned task:

```go
ctx := context.Background()

c, err := proxmox.NewClient(
    ctx,
    "https://pve.example:8006",
    api.TokenCredentials("root@pam!sdk", "your-token-secret"),
)
if err != nil {
    return err
}

// Clone a template into VM 101 and wait for the task to finish.
ref, err := c.QEMU("pve").Clone(ctx, 9000, &qemu.CloneSpec{NewID: 101, Name: "web-1"})
if err != nil {
    return err
}
if _, err := c.Tasks().Wait(ctx, ref); err != nil {
    return err
}

// Start it and wait again.
ref, err = c.QEMU("pve").Start(ctx, 101)
if err != nil {
    return err
}
_, err = c.Tasks().Wait(ctx, ref)
return err
```

## Authentication

Pass one of three credential strategies to `NewClient`:

```go
// API token (recommended for automation).
api.TokenCredentials("user@realm!tokenid", "secret")

// A pre-minted ticket + CSRF token.
api.TicketCredentials("PVE:ticket...", "csrf-token")

// Username/password (+ optional OTP); the SDK mints and refreshes the ticket.
api.UserCredentials("root@pam", "password", "")
```

## Client options

`NewClient` takes functional options:

| Option                                | Purpose                                            |
| ------------------------------------- | -------------------------------------------------- |
| `WithInsecureSkipVerify(true)`        | Skip TLS verification (self-signed homelab CA).    |
| `WithLogger(l)`                       | Consumer-supplied debug logger (secrets redacted). |
| `WithClusterEndpoints(eps...)`        | Multiple nodes for sticky failover.                |
| `WithRequestTimeout(d)`               | Per-request timeout.                               |
| `WithRetry(policy)`                   | Custom retry policy for idempotent reads.          |
| `WithHTTPClient(h)`                   | Bring your own pooled `*http.Client`.              |
| `WithMinTLS(v)` / `WithUserAgent(ua)` | Tune the transport.                                |

```go
c, err := proxmox.NewClient(ctx, endpoint, creds,
    proxmox.WithInsecureSkipVerify(true), // self-signed homelab node
    proxmox.WithLogger(myLogger),
)
```

## Services

Reach each domain from the client. Node-scoped services take a node name;
cluster-scoped services do not.

| Accessor                                          | Scope    | Domain                                 |
| ------------------------------------------------- | -------- | -------------------------------------- |
| `c.QEMU(node)`                                    | node     | virtual machines                       |
| `c.LXC(node)`                                     | node     | containers                             |
| `c.Storage()`                                     | cluster  | datastores, volumes, uploads, ZFS      |
| `c.Nodes()`                                       | node/arg | node networking + administration       |
| `c.HA()`                                          | cluster  | HA resources, rules, replication       |
| `c.SDN()`                                         | cluster  | zones, VNets, subnets, fabrics         |
| `c.Cluster()`                                     | cluster  | cluster resources, status, options     |
| `c.Access()`                                      | cluster  | users, groups, roles, ACLs, API tokens |
| `c.Metrics()`                                     | mixed    | RRD, node status, metric servers       |
| `c.Ceph()`                                        | cluster  | pools, OSDs, CRUSH, status             |
| `c.PBS()`                                         | mixed    | PVE-side backup jobs, backup/restore   |
| `c.Console()`                                     | node/arg | VNC/SPICE/term tickets + `Connect`     |
| `c.Firewall()` / `NodeFirewall` / `GuestFirewall` | scoped   | firewall rules, IP sets, options       |
| `c.SSH(opts...)`                                  | node     | SFTP/exec side-channel (non-REST ops)  |

## Working with tasks

Long-running operations return a `tasks.Ref` (a parsed UPID). Await it with the
`Tasks()` service; a failed task surfaces `pverr.ErrTaskFailed` with the log
tail.

```go
ref, err := c.QEMU("pve").Start(ctx, 101)
// ...
status, err := c.Tasks().Wait(ctx, ref) // blocks with capped backoff
```

Synchronous operations (most config writes) return no task — check the error
directly. Some return `(tasks.Ref, error)` where PVE's behavior varies by minor;
use `ref.IsZero()` to tell.

## Error handling

Classify errors with the `pverr` taxonomy rather than string-matching:

```go
_, err := c.QEMU("pve").Get(ctx, 404)
switch {
case errors.Is(err, pverr.ErrNotFound):
    // no such VM
case errors.Is(err, pverr.ErrForbidden):
    // token lacks the privilege
case errors.Is(err, pverr.ErrUnsupported):
    // op requires a newer PVE minor than this node runs
}

var perr *pverr.Error
if errors.As(err, &perr) {
    // perr.Status, perr.Message, ... for detail
}
```

## Capability gating

The client snapshots the node's version at construction. Features introduced in
a later 9.x minor are gated: on an older node the op returns a
`pverr.ErrUnsupported`-wrapped error **before** any request is made.

```go
if c.Capabilities().VolumeChainSnapshots() { // 9.1+
    // safe to call storage volume-chain snapshot ops
}
```

## Testing with mockpve

The SDK ships `proxmox/mockpve`, an in-memory PVE responder, so you can
integration-test your code without a live cluster:

```go
mock := mockpve.New()
mock.AddVM("pve", 9000, "template", "stopped")
ts := mock.Serve()
defer ts.Close()

c, _ := proxmox.NewClient(ctx, ts.URL, api.TokenCredentials("root@pam!sdk", "secret"))
// exercise c against the mock exactly as you would a real node
```

Each service package also carries a runnable `Example` — see `go doc` or the
`example_test.go` files.

## Versioning and stability

The SDK is `v0.x`: the public surface may still change between minor tags. Pin a
specific tag in your `go.mod` and upgrade deliberately. The core, compute, and
storage surfaces stabilize at `v1.0.0`.
