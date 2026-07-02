---
id: IMPL-0001
title: "Proxmox VE 9.x SDK coverage"
status: Draft
author: Donald Gifford
created: 2026-06-22
---

<!-- markdownlint-disable-file MD025 MD041 -->

# IMPL 0001: Proxmox VE 9.x SDK coverage

<!--toc:start-->

- [IMPL 0001: Proxmox VE 9.x SDK coverage](#impl-0001-proxmox-ve-9x-sdk-coverage)
  - [Objective](#objective)
  - [Scope](#scope)
    - [In Scope](#in-scope)
    - [Out of Scope](#out-of-scope)
  - [Coverage legend](#coverage-legend)
  - [Implementation Phases](#implementation-phases)
    - [Phase 1: Core, auth, version, tasks (foundation)](#phase-1-core-auth-version-tasks-foundation)
      - [Tasks](#tasks)
      - [Success Criteria](#success-criteria)
    - [Phase 2: Compute — QEMU + LXC](#phase-2-compute--qemu--lxc)
      - [Tasks](#tasks-1)
      - [Success Criteria](#success-criteria-1)
    - [Phase 3: Storage](#phase-3-storage)
      - [Tasks](#tasks-2)
      - [Success Criteria](#success-criteria-2)
    - [Phase 4: HA, scheduling, replication](#phase-4-ha-scheduling-replication)
      - [Tasks](#tasks-3)
      - [Success Criteria](#success-criteria-3)
    - [Phase 5: Network + SDN](#phase-5-network--sdn)
      - [Tasks](#tasks-4)
      - [Success Criteria](#success-criteria-4)
    - [Phase 6: Cluster, access, nodes-admin, Ceph, backup, console, metrics](#phase-6-cluster-access-nodes-admin-ceph-backup-console-metrics)
      - [Tasks](#tasks-5)
      - [Success Criteria](#success-criteria-5)
  - [File Changes](#file-changes)
  - [Testing Plan](#testing-plan)
  - [Dependencies](#dependencies)
  - [Open Questions](#open-questions)
  - [References](#references)
  <!--toc:end-->

**Status:** Draft **Author:** Donald Gifford **Date:** 2026-06-22

## Objective

Track which Proxmox VE 9.x capabilities the Proxmox SDK wraps, and to what
degree. This is a **living coverage matrix**: each capability is a checkbox the
SDK ticks off as it is implemented and tested. "Done" means a typed operation
exists, is unit-tested against `mockpve`, and has at least one integration test
against a live 9.x node.

**Implements:** ADR-0001 (standalone Proxmox SDK), ADR-0002 (PVE 9.x-only).
**Design:** DESIGN-0001 (Proxmox SDK package layout) — this ledger tracks
coverage against that contract.

**Design alignment (per the resolved DESIGN-0001 questions, 2026-06-22):** the
SDK lives under `proxmox/` (client in package `proxmox`, root is a doc-only
`sdk`); the transport supports optional intra-cluster node failover
(`WithClusterEndpoints`, Phase 1); `console` mints tickets, verifies the 9.x
auth-ticket, and exposes `Connect()` (Phase 6); config codegen is reference +
diff only (hand-written structs); `ssh` is an in-module sub-package; the licence
is Apache-2.0 (`LICENSE`); shared primitives live in `proxmox/types` and the
error taxonomy in `proxmox/pverr` (OQ-1). Remaining build-time decisions are in
[Open Questions](#open-questions).

## Scope

### In Scope

- Capabilities Proxmox VE 9.x exposes server-side (REST `/api2/json` + the
  SSH/SFTP side-channel for the few ops the REST API cannot do).
- Per-minor gating across 9.0 / 9.1 / 9.2 (the API is unversioned within the
  major).

### Out of Scope

- Consumer-side orchestration PVE has no API for (cross-cluster
  balancing/migration, policy we compute ourselves) — that lives in the service,
  not the SDK.
- The frontend, and any future naos provider (its own SDK).

## Coverage legend

Annotate each task as it lands:

- `[ ]` not started · `[~]` partial · `[x]` done (typed + mock-tested +
  live-tested)
- `(9.1+)` / `(9.2+)` — capability requires that minor; SDK must `version`-gate
  it
- `(tp)` — Proxmox tech-preview; wrap behind a capability flag, expect churn
- `(ssh)` — needs the SSH/SFTP side-channel, not pure REST

## Implementation Phases

Phases are sequenced so each builds on the last. A phase is complete when its
tasks are checked and its success criteria are met.

---

### Phase 1: Core, auth, version, tasks (foundation)

The transport and primitives every service hangs off.

#### Tasks

- [x] `DoRequest(ctx, method, path, req, resp)` + `ExpandPath` path templating
- [x] `Connection`: primary endpoint + optional ordered cluster-node failover
      set (`WithClusterEndpoints`), TLS (self-signed/IP, min-TLS), retry/backoff
      that rotates across nodes
- [x] Credentials + precedence: auth-ticket > API token > user/pass; 2 h ticket
      refresh; CSRF on writes
- [x] `version` service: `MinimumProxmoxVersion = 9.0` + `Support*()` per-minor
      gates
- [x] `tasks`: UPID parse, `WaitForTask` / `WaitForStatus` waiters, task-log
      read
- [x] `0`/`1` → bool handling (`types.PVEBool`), typed error taxonomy (`pverr`:
      NotFound/Conflict/AuthExpired/TaskFailed/Transient/…)
- [x] `mockpve` server + mockable interfaces; functional options
      (`WithLogger`/`WithCache`/`WithHTTPClient`/`WithTLS`)
- [x] Root `proxmox` package: `NewClient` (seeds `Capabilities` from `/version`,
      rejects < 9.0) + `Client` accessors + functional options; placement of
      shared primitives & the error taxonomy per **OQ-1**
- [x] Promote the `doc.go` stubs (created in the skeleton commit) for every
      Phase 1 package — `api`, `types`, `pverr`, `version`, `tasks`, root
      `proxmox`: replace the "Skeleton: no implementation yet" placeholder with
      a real package overview + a runnable `Example`; `go doc ./...` renders
      cleanly

#### Success Criteria

- `go build ./...` clean; auth + a trivial `GET /version` round-trips against
  live 9.x
- Waiters drive a real start/stop task to completion

> **Status (all 9 tasks done):** `go build ./...` and `just lint`/`just test`
> (race) are green. Auth (token / pre-minted ticket / user-pass mint+refresh) +
> `GET /version` round-trip and the task waiters (running→stopped OK/failed) are
> **verified against the in-process `mockpve` responder**, not a live 9.x node.
> The two live-only criteria above are therefore **written-but-unverified** in
> this environment (no live node / recorded cassettes — see CLAUDE.md); they
> stand to be confirmed once a 9.x node is reachable.

---

### Phase 2: Compute — QEMU + LXC

#### Tasks

- [x] QEMU: list, status, config get/set, create, clone, delete
- [x] QEMU power: start/stop/shutdown/reboot/suspend/resume
- [x] QEMU migrate (online/offline), disk + NIC add/resize/remove
- [x] QEMU snapshots: list/create/rollback/delete (+ TPM-state snapshots on
      NFS/CIFS/dir `(9.1+)`)
- [x] Guest-agent exec + fine-grained agent privileges (9.x model)
- [x] LXC: list, status, config, create, clone, delete, power
- [x] LXC snapshots (ZFS/btrfs/LVM-thin backing)
- [x] LXC from **OCI image templates** `(9.1+ tp)` — pull/upload OCI as template
- [x] Promote the `doc.go` stubs for `qemu` + `lxc` — real package overview + a
      runnable `Example` (e.g. clone → start)

#### Success Criteria

- Create→start→snapshot→rollback→stop→delete works end-to-end for both QEMU and
  LXC

---

### Phase 3: Storage

#### Tasks

- [x] Datastore list + status; content listing (volumes, ISOs, templates,
      backups)
- [x] Volume create/resize/delete/move (allocate/free in `storage`; resize/move
      are guest-scoped — `qemu.ResizeDisk` + `qemu.MoveDisk`)
- [x] **Snapshots as volume chains** on thick-LVM + Directory/NFS/CIFS
      `(tp → maturing)` — capability-gated (`VolumeChainSnapshots` 9.1+; gate
      mock-verified, endpoint shape unconfirmed without a live node)
- [x] ISO / disk-image upload (large-file streaming) — `UploadISO`/
      `UploadDiskImage` over the new `api.Client.DoUpload` (io.Pipe + multipart,
      no buffering, no retry)
- [x] Snippet + backup upload `(ssh)` — SFTP via PAM account; new `proxmox/ssh`
      side-channel (`UploadSnippet`/`UploadBackup`/`Exec`), mandatory host-key
      verification, single-connection `Client` exposed via `Client.SSH(...)`.
      Unit-tested against an in-process SSH+SFTP server; live PAM auth + writes
      under `/var/lib/vz` unverifiable without a reachable node.
- [x] ZFS pool ops incl. RAIDZ expansion `(9.x)` — `ListZFSPools`/`GetZFSPool`/
      `CreateZFSPool` over `/nodes/{node}/disks/zfs` (mock-verified). RAIDZ
      expansion is gated on the new `ZFSRAIDZExpansion` capability (9.2) but PVE
      exposes **no REST endpoint** for it (`zpool attach`), so `ExpandRAIDZ`
      returns a documented `pverr.ErrUnsupported` pointing at the ssh
      side-channel rather than fabricating an endpoint.
- [x] Promote the `doc.go` stubs for `storage` (and the `ssh` side-channel) —
      real package overview + a runnable `Example`. `storage` has a runnable
      `Example` (upload → snapshot → cleanup, `go doc`-verified); `ssh` has a
      compile-only `Example` (no `Output`) since it needs a live host.

#### Success Criteria

- [x] Upload an ISO, create a volume-chain snapshot where supported, clean up —
      covered by the runnable `storage` `Example` (seeds 9.1 to enable
      volume-chain snapshots) and `TestVolumeSnapshotLifecycle`; mock-verified.

---

### Phase 4: HA, scheduling, replication

The 9.x-reworked area — model rules, never the deprecated groups.

#### Tasks

- [x] HA resources: add/remove (incl. add-after-create/restore), state
      management — new cluster-scoped `proxmox/ha` service
      (`ListResources`/`GetResource`/`AddResource`/`UpdateResource`/
      `RemoveResource`); SIDs (`vm:100`) path-escaped; config writes are
      synchronous (return `error`, no task). Mock-verified.
- [x] **HA rules**: node-affinity + resource-affinity (resource-to-node,
      resource-to-resource); enable/disable —
      `ListRules`/`GetRule`/`CreateRule`/ `UpdateRule`/`DeleteRule` over
      `/cluster/ha/rules` (the 9.x replacement for the deprecated groups, which
      the SDK never models). `RuleType` + `HARuleSpec` (Nodes/Resources
      CSV-joined) + lossless `HARule`; disable via `HARuleUpdate.Disable`.
      Mock-verified; per-variant param names provisional without a live node.
- [x] CRS settings read/write (static-load scheduler) — `GetCRSSettings`/
      `SetCRSSettings`; CRS lives inside datacenter options (`/cluster/options`,
      the `crs` compound property-string), parsed/encoded to typed `Mode` +
      `HARebalanceOnStart`. Mock-verified; sub-key names provisional.
- [x] **Dynamic Load Balancer** controls `(9.2+)` — continuous CRS rebalancing
      toggle/config — `GetDLBStatus`/`SetDLBConfig`, gated on the 9.2
      `DynamicLoadBalancer` capability. REST-with-caveat: provisional path
      `/cluster/ha/lbalancer` (mirrors ha-manager naming), gate mock-verified,
      wire shape unconfirmed without a live 9.2 node.
- [x] Arm/Disarm HA cluster-wide switch `(9.2+)` — `ArmHA`/`DisarmHA` + new
      `HAClusterSwitch` (9.2) capability. No confirmed PVE REST endpoint (a
      GUI/pvecm action), so both return a documented `pverr.ErrUnsupported`
      rather than fabricating a path — like `storage.ExpandRAIDZ`.
- [x] Storage/ZFS replication jobs (respect new `VM.Replicate` privilege) —
      `ListReplicationJobs`/`GetReplicationJob`/`CreateReplicationJob`/
      `UpdateReplicationJob`/`DeleteReplicationJob` over `/cluster/replication`;
      lossless `ReplicationJob`, IDs `<vmid>-<jobnum>`; VM.Replicate noted in
      docs. Synchronous writes. Mock-verified.
- [x] Promote the `doc.go` stub for `ha` — real package overview + a runnable
      `Example` (define a resource-affinity rule). `go doc`-verified; the
      `Example` (add two resources → resource-affinity rule → read back) runs
      against mockpve with `// Output` checked.

#### Success Criteria

- [x] Define a resource-affinity rule via the SDK and observe placement honor it
      — the rule definition + read-back is covered by the runnable `ha`
      `Example` and `TestCreateResourceAffinityRule` (mock-verified). The
      **placement-honored observation is live-only** (the mock does not
      schedule) and remains written-but-unverified without a real cluster.

---

### Phase 5: Network + SDN

#### Tasks

- [x] Node networking: bridges, bonds, VLAN-aware bridges, interface config
      (package placement per **OQ-8** — in `nodes`) — `ListInterfaces`/
      `GetInterface`/`CreateInterface`/`UpdateInterface`/`DeleteInterface` over
      `/nodes/{node}/network`; lossless `Interface`. `ApplyNetworkConfig` (PUT)
      returns a `tasks.Ref` (PVE may reload via a worker; zero Ref when
      synchronous). Mock-verified.
- [x] SDN zones (VLAN/VXLAN/EVPN) + VNets + subnets — cluster-scoped `sdn`
      package: `Zone`/`VNet`/`Subnet` (lossless reads) with full CRUD over
      `/cluster/sdn/{zones,vnets,vnets/{vnet}/subnets}`; all config writes are
      synchronous (return `error`). `ApplySDN` (PUT `/cluster/sdn`) commits the
      staged config cluster-wide. Mock-verified.
- [x] **SDN Fabrics** `(9.0+)` — OpenFabric/OSPF; gate newer protocols
      (WireGuard/BGP route-maps/IPv6 underlay) `(9.2+)`. `Fabric` lossless
      read + CRUD over the **provisional** `/cluster/sdn/fabrics`
      (REST-with-caveat: real 9.0 feature, path/fields unverified against a live
      node). Basic protocols (openfabric/ospf) are baseline; `FabricProtocolBGP`
      is refused below 9.2 via the new `SDNAdvancedFabrics` gate. Mock-verified.
- [x] SDN status reporting (connected guest NICs, EVPN learned IPs/MACs, fabric
      routes/neighbors) — `SDNStatus`/`VNetStatus` with fixed forward-compatible
      return types, but **no confirmed PVE REST endpoint** exists, so both
      return documented `pverr.ErrUnsupported` (like `ha.ArmHA`). No mock
      handlers.
- [x] Firewall: rules, ipsets (incl. overlapping ipset support `(9.1+)`) — new
      `firewall` package with a **scope model**: ONE `Service{c,caps,scope}` and
      three constructors (`NewClusterScope`/`NewNodeScope`/`NewGuestScope`), so
      the rule/IPSet/options surface is written once and `scope.path()` switches
      the prefix (cluster `/cluster/firewall`, node `/nodes/{n}/firewall`, guest
      `/nodes/{n}/{qemu|lxc}/{vmid}/firewall`). `RenameIPSet` gated 9.1
      (`OverlappingIPSets`). Root accessors `Firewall`/`NodeFirewall`/
      `GuestFirewall`. Mock-verified across all three scopes.
- [x] Promote the `doc.go` stubs for `sdn` + `firewall` (and node networking in
      `nodes`) — real package overview + a runnable `Example`. All three render
      cleanly under `go doc ./...` and their Examples pass.

#### Success Criteria

- Enumerate zones/VNets/fabrics and their live status without error

> **Status (all 6 tasks done):** `go build ./...`, `just lint` (0 issues), and
> `just test` (race) are green. Enumerating zones/VNets/fabrics
> (`ListZones`/`ListVNets`/`ListFabrics`) plus full CRUD, `ApplySDN`, node
> networking, and the scoped firewall are **verified against the in-process
> `mockpve` responder** across all three firewall scopes. The **live-status**
> half of the criterion (`SDNStatus`/`VNetStatus`) has **no confirmed PVE REST
> endpoint** and returns documented `pverr.ErrUnsupported` — it is neither mock-
> nor live-verifiable here and is recorded as such (like `ha.ArmHA`).
> Enumeration is satisfied; live status is written-but-unsupported pending a
> reachable 9.x node to confirm the real endpoint.

---

### Phase 6: Cluster, access, nodes-admin, Ceph, backup, console, metrics

#### Tasks

- [x] Cluster: `/cluster/resources`, status, options — new cluster-scoped
      `cluster` package: `ListResources` (with `WithResourceType` filter),
      `GetStatus` (lossless `StatusEntry` list), `GetOptions`/`SetOptions`
      (lossless; sync write). The mock's `/cluster/options` handler is shared
      with HA (HA owns `crs`; cluster owns description/migration/…).
      Mock-verified.
- [x] Access: users, groups, roles, ACLs using the **9.x privilege model**
      (`VM.Replicate`, granular agent privs; no `VM.Monitor`) — new
      cluster-scoped `access` package: full user/group/role CRUD + ACL
      grant/revoke (`SetACL`, one PUT, `Delete` revokes). `Role` normalises
      PVE's two role-read shapes (CSV list entry vs privilege→1 object).
      Mock-verified.
- [x] API tokens: create/list/revoke, clear comment `(9.1+)`, **regenerate
      secret in place** `(9.2+)` — in the `access` package;
      `CreateToken`/`RegenerateTokenSecret` return the one-time `TokenSecret`.
      `ClearTokenComment` gated 9.1, `RegenerateTokenSecret` gated 9.2
      (REST-with-caveat: provisional rotate path). Gates mock-verified.
- [x] Node admin: package updates (DEB822 sources), disks/SMART,
      certificates/ACME, custom scripts `(ssh)` — extends the `nodes` package
      (node per-call, no bound node): apt (`ListAptUpdates`,
      `RefreshAptCache`→`tasks.Ref`) plus DEB822 repositories
      (`ListRepositories`/`UpdateRepository`, **REST-with-caveat**: real
      endpoint, provisional field shapes); disks (`ListDisks`, `GetDiskSMART`
      **REST-with-caveat** on the attribute table,
      `InitializeDisk`→`tasks.Ref`); certificates (`GetNodeCertificates`,
      `UploadCustomCertificate`/ `DeleteCustomCertificate` sync) +
      cluster-scoped ACME accounts
      (`ListACMEAccounts`/`GetACMEAccount`/`RegisterACMEAccount`→`tasks.Ref`/
      `UpdateACMEAccount`/`DeactivateACMEAccount`→`tasks.Ref`) + node ACME cert
      `Order`/`Renew`/`RevokeNodeCertificate`→`tasks.Ref` (**REST-with-caveat**:
      real endpoint, task-vs-sync unconfirmed). **Custom node scripts have no
      PVE REST endpoint** — the SDK offers no method; run them over the SSH
      side-channel (`c.SSH().Exec`). Mock-verified.
- [x] Ceph: pools, OSDs, RBD mirroring (Squid) — new `ceph` package (`c.Ceph()`,
      **no** node arg; each op takes the MON node per-call, flat cluster-wide
      state). Pools (`ListPools`/`GetPool`/`CreatePool`→`tasks.Ref`/
      `DeletePool`→`tasks.Ref`), OSDs (`ListOSDs` → recursive CRUSH `OSDTree`,
      `CreateOSD`/`DestroyOSD`→`tasks.Ref`), `GetStatus` (lossless health) +
      `GetClusterConfig` (ceph.conf verbatim text). Baseline 9.0, no gates; REST
      **paths provisional** (unconfirmed against a live cluster, centralised in
      paths.go). **RBD mirroring** is an `rbd`-CLI feature with **no confirmed
      PVE REST endpoint**, so `GetMirrorStatus`/`EnableMirroring`/
      `DisableMirroring` return documented `pverr.ErrUnsupported` (drive
      `rbd     mirror` over SSH) — reclassified from the memo's REST-with-caveat
      guess to an honest ErrUnsupported stub. Pools/OSDs/status mock-verified.
- [x] PBS integration: datastores, backups, verify, restore — new `pbs` package
      (**PVE-side only**; the PBS-native datastore API is a future `pbsclient`).
      Mixed scope, no bound node (`PBS()` accessor): scheduled backup jobs
      (cluster `/cluster/backup` — `ListBackupJobs`/`GetBackupJob`/`Create`/
      `Update`/`DeleteBackupJob`, sync), node backups (`ListNodeBackups` via the
      storage content listing, `CreateBackup`→`tasks.Ref` via
      `/nodes/{n}/vzdump`), and restore (`RestoreQEMU`/`RestoreLXC`→`tasks.Ref`,
      reusing the guest-create endpoints with
      `archive=`/`ostemplate=`+`restore=1`). **Backup verification is PBS-native
      with no PVE REST endpoint**, so `VerifyBackup` returns documented
      `pverr.ErrUnsupported` (honest stub, diverging from the memo's
      REST-with-caveat guess). Mock-verified.
- [x] Console: mint VNC/SPICE/term tickets, verify the **token-owned VNC
      auth-ticket** `(9.x)`, and `Connect()` a duplex byte stream to the
      console; the browser bridge is the consumer's — new node-per-call
      `console` package (`Console()` accessor, no bound node). Ticket mint is a
      plain sync REST call, fully mock-verified: guest
      `MintVNCTicket`/`MintSPICETicket`/`MintTermProxy(node, kind, vmid)` (POST
      `/nodes/{n}/{qemu|lxc}/{vmid}/{vncproxy|spiceproxy|termproxy}`) and node
      shell `MintNodeVNC`/`MintNodeTerm(node)`
      (`/nodes/{n}/{vncshell|termproxy}`); VNC/term tickets are lossless, SPICE
      params lossless. `Connect(ctx, node,     *VNCTicket)` dials
      `/nodes/{n}/vncwebsocket` over a **new `api.Client.DoWebSocket`** (native
      101 upgrade → `resp.Body` duplex stream) and returns the raw byte stream —
      the WebSocket-framed RFB payload is the caller's concern
      (**REST-with-caveat**: wire format unverified without a live node;
      plumbing verified against a mockpve hijack+echo upgrade).
      **VerifyVNCTicket** has no standalone PVE REST endpoint (a ticket is
      verified when `Connect` presents it to the upgrade), so it returns
      documented `pverr.ErrUnsupported` — honest stub, diverging from the memo's
      REST-with-caveat guess. Ticket mint + Connect echo mock-verified.
- [x] Metrics: extended metrics (CPU/mem/IO pressure stall, ZFS ARC);
      OpenTelemetry exporter `(9.1+)` — new mixed-scope `metrics` package (no
      bound node; `Metrics()` accessor). Node/guest RRD (`GetNodeRRD`/`GetVMRRD`
      with `WithTimeframe`/`WithConsolidation` options) + `GetNodeStatus` are
      lossless reads — pressure-stall and ZFS-ARC counters are
      **REST-with-caveat** and land in `Extra`. Cluster-scoped external metric
      servers
      (`ListMetricServers`/`GetMetricServer`/`Create`/`Update`/`DeleteMetricServer`,
      InfluxDB/Graphite, sync writes). The 9.1 OpenTelemetry exporter is
      file-configured with **no REST endpoint**, so
      `GetOTelConfig`/`SetOTelConfig` return documented `pverr.ErrUnsupported`
      (new `OTelExporter` 9.1 gate reserved for the future). Mock-verified
      (RRD/status synthesized static).
- [x] Promote the `doc.go` stubs for `cluster`, `access`, `nodes`, `ceph`,
      `pbs`, `console`, `metrics`, `mockpve` — real package overview + a
      runnable `Example`. All Phase-6 packages carry a promoted `doc.go` (no
      `Skeleton` placeholders remain) and a runnable `Example`; `mockpve` gained
      its own (`New` → `NewClient` → read capabilities). The Phase-6 success
      flow ships as `proxmox.Example_consoleAndAccess`: mock → `NewClient` →
      `Access().ListUsers`/`ListTokens` → `Console().MintVNCTicket`, with
      deterministic seeded output.

#### Success Criteria

- Mint a VNC console session through the SDK; list users/tokens under the 9.x
  privilege model

> **Status (all 9 tasks done):** `go build ./...`, `just lint` (0 issues), and
> `just test` (race) are green; every Phase-6 package is doc-promoted with a
> runnable `Example`. The success flow is **mock-verified** end-to-end
> (`proxmox.Example_consoleAndAccess`): `Access().ListUsers`/`ListTokens` under
> the 9.x privilege model, and `Console().MintVNCTicket` mints a session ticket.
> `Console().Connect` dials the ticket over `api.DoWebSocket` and is exercised
> against a `mockpve` hijack+echo `/vncwebsocket` upgrade, so the SDK plumbing
> is verified; the **live VNC (RFB) wire payload** is the one **live-only**
> piece, written-but-unverified here (no live 9.x node / recorded cassettes —
> see CLAUDE.md). Several Phase-6 surfaces are REST-with-caveat (DEB822, SMART,
> ACME task-vs-sync, Ceph/PBS paths, RRD pressure-stall/ZFS-ARC) or documented
> `pverr.ErrUnsupported` stubs (Ceph RBD mirroring, PBS verify, metrics OTel,
> console VerifyVNCTicket) where no PVE 9.x REST endpoint is confirmed.

---

## File Changes

Package skeletons already exist as `doc.go` stubs (the reconcile commit), each a
one-line summary plus a "Skeleton: no implementation yet" placeholder — so the
per-phase godoc tasks above _promote_ an existing stub, they do not create the
file. This table maps the real code to phases. Column widths are re-aligned by
`just fmt`.

| File                                                        | Action | Description                                               |
| ----------------------------------------------------------- | ------ | --------------------------------------------------------- |
| `proxmox/api/{client,connection,credentials,auth,retry}.go` | Create | Phase 1 — low-level client + transport                    |
| `proxmox/types/`                                            | Create | Phase 1 — primitives: VMID, GuestRef, … (OQ-1)            |
| `proxmox/pverr/`                                            | Create | Phase 1 — error taxonomy: `*Error` + sentinels (OQ-1)     |
| `proxmox/{version,tasks}/`                                  | Create | Phase 1 services                                          |
| `proxmox/{qemu,lxc}/`                                       | Create | Phase 2 compute                                           |
| `proxmox/storage/`                                          | Create | Phase 3                                                   |
| `proxmox/{ha,cluster}/`                                     | Create | Phase 4                                                   |
| `proxmox/{sdn,firewall}/` (node net in `nodes`; OQ-8)       | Create | Phase 5                                                   |
| `proxmox/{access,nodes,ceph,pbs,console,metrics}/`          | Create | Phase 6                                                   |
| `proxmox/mockpve/`                                          | Create | mock server (all phases) + `cmd/mockpve/` runnable server |
| `proxmox/{proxmox,options}.go`                              | Create | Phase 1 — root: client + options, no aliases (OQ-1)       |
| `proxmox/ssh/`                                              | Create | SFTP/exec side-channel (Phase 3/6 ops)                    |
| `cmd/pve-schemadiff/`                                       | Done   | CI schema-drift tool (OQ-7) — parse+diff, CI-wired        |
| `LICENSE`                                                   | Done   | Apache-2.0                                                |

## Testing Plan

- [x] Unit tests for every exported operation against `mockpve` (model per
      **OQ-4**) — every service package unit-tests its exported ops against the
      in-process `mockpve` responder; `just test` (race + coverage) is green
      module-wide.
- [ ] Integration tests against a live 9.x node (and a 9.2 node for `(9.2+)`
      rows); harness per **OQ-5** — **written-but-unverified**: not runnable in
      this environment (no live 9.x node / recorded cassettes). The
      `//go:build     integration` harness reads `PVE_ENDPOINT`/`PVE_TOKEN_*`;
      this is the sole cross-cutting item that is genuinely blocked here.
- [x] Table-driven tests for the `0/1`→bool + config-struct (un)marshalling —
      `proxmox/types/types_test.go` covers `PVEBool` both directions; the
      per-service lossless-decode tests cover config-struct (un)marshalling +
      `Extra` round-trips.
- [x] CI `version`-diff step: regenerate from `apidoc.js`, flag drift across 9.x
      minors — `cmd/pve-schemadiff` (OQ-7) parses an `apidoc.js` dump into a
      (method, path) set and diffs it against a committed baseline (`-update`
      rebaselines); unit-tested against a synthetic fixture, wired into CI via
      `just schemadiff` (test-go job). It runs against a committed synthetic
      fixture here; pointing `-apidoc` at a real 9.x dump to guard the live REST
      surface is the live-only remainder.
- [x] `Example` functions compile + run under `go test`; `go doc ./...` renders
      every package's overview (godoc coverage gate) — every service + `mockpve`
      package ships a runnable `Example`; no `Skeleton` doc stubs remain.

## Dependencies

- ADR-0001 — standalone SDK split (this is the SDK's coverage ledger)
- ADR-0002 — PVE 9.x-only floor (defines the gating baseline)
- A live PVE 9.x cluster (ideally one 9.0/9.1 and one 9.2) for integration tests

## Open Questions

Build-time decisions surfaced while planning Phase 1. **OQ-1–OQ-10 are
resolved** — the chosen letter is in each heading; the lettered options are kept
as record. OQ-1 amended DESIGN-0001's "primitives live in the root package"
line.

### OQ-1. Package layering for shared primitives + the error taxonomy — RESOLVED (a)

The unified `proxmox` package imports the service subpackages (its accessors
return `*qemu.Service`, etc.), so whatever the services depend on must sit
_below_ them — they cannot live in the root `proxmox` package.

**Decision (a):** dedicated leaf packages, no re-export.

- `proxmox/types` — primitives: `VMID`, `NodeName`, `GuestRef`, `PowerState`,
  `PVEBool`.
- `proxmox/pverr` — error taxonomy: the `*Error` type + sentinels
  (`ErrNotFound`, …) + classification. Named `pverr` (not `errors`) to avoid
  shadowing stdlib `errors`, so no call-site alias is forced.
- `proxmox/api` stays the low-level client and imports `pverr` to classify.
- Services import `api`, `types`, `pverr`; the root `proxmox` package holds only
  the client + options.

Consumers import the package they need (`types.VMID`, `pverr.ErrNotFound`) — the
same shape as AWS (`service/<svc>/types`, `smithy`) and k8s (`apierrors`). No
alias-façade in the root (the un-idiomatic part we rejected). This amends
DESIGN-0001's earlier "primitives live in the root package" line.

Alternatives (not chosen): **b** errors co-located in `types` (AWS-style, one
fewer package); **c** primitives + errors both in `api`; **d** move the client
to `proxmox/client` with primitives in the root.

### OQ-2. Node-failover behavior (`WithClusterEndpoints`) — RESOLVED (a)

How the transport picks among a cluster's node addresses.

- **a (recommended):** Sticky + passive — use the primary until a transport
  error, then rotate by priority and stay there. No background health checks (a
  library shouldn't spawn goroutines the caller can't see).
- **b:** Sticky + periodic re-probe — as above, but occasionally retry
  priority-0 so it drifts back to the preferred node after recovery.
- **c:** Per-request priority walk — every request starts at priority-0. Simple,
  but more dial churn while a node is down.
- **other:** \_\_\_\_\_

### OQ-3. Ticket-refresh strategy (user/pass auth) — RESOLVED (a)

API tokens need no refresh; tickets expire at 2h.

- **a (recommended):** Lazy + reactive — check the expiry timestamp (minus skew)
  before each request and re-auth if due; also re-auth once and replay on
  `ErrTicketExpired`. No background timer.
- **b:** Proactive background timer that refreshes ahead of expiry — smoother
  under load, but a library-owned goroutine to manage.
- **c:** Purely reactive — re-auth only on `ErrTicketExpired` / 401. Simplest;
  every long-idle client burns one failed request first.
- **other:** \_\_\_\_\_

### OQ-4. How `mockpve` models PVE — RESOLVED (c)

Drives Phase 1 and every unit test.

**Decision (c):** the SDK's own unit + integration tests replay recorded
`go-vcr` cassettes of real PVE exchanges — one ground-truth corpus, shared with
OQ-5. Caveat: go-vcr is **client-side** replay, so it cannot power the _shipped_
`proxmox/mockpve` / `cmd/mockpve` **server** consumers run against — that
substrate is a separate call, now **OQ-10**.

- **a (recommended):** Stateful in-memory model for resources under test (create
  → appears in list → delete), seeded from golden fixtures for read-heavy
  endpoints. Lets unit tests exercise waiters and lifecycle flows.
- **b:** Pure golden-fixture replay (request → canned response). Simple, but
  can't represent state transitions (start → running, task → OK).
- **c:** Recorded cassettes (`go-vcr`) from a live node, replayed. Highest
  fidelity; tests depend on a capture step.
- **other:** \_\_\_\_\_

### OQ-5. Integration-test harness — RESOLVED (a)

DESIGN-0001 wants live 9.0/9.1 + 9.2 coverage; CI has no PVE.

- **a (recommended):** Build-tag + env-configured live nodes for opt-in/local
  runs, plus committed `go-vcr` cassettes for CI replay.
- **b:** Live-only via env vars; CI runs unit/mock only.
- **c:** Cassettes-only in CI; live optional and undocumented.
- **other:** \_\_\_\_\_

### OQ-6. Modeling the sprawly config objects (`net0=virtio,bridge=vmbr0`, …) — RESOLVED (a)

The `ConfigQemu`-class surface (Phase 2/3).

**Decision (a):** typed common path + `Extra map[string]string` fallback.
**Under discussion (your cassettes idea):** use the recorded cassettes
(OQ-4/OQ-5) as ground truth — a CI test unmarshals each into the typed structs
and flags any field that lands in `Extra`, giving a data-driven worklist to
_promote_ fields toward fuller typing (b) incrementally rather than big-bang.
apidoc.js (OQ-7) still covers the declared superset for completeness.

- **a (recommended):** Typed common path + fallback — typed fields/parsers for
  the stable, common keys; a `map[string]string` (`Extra`) for the long tail.
  Matches Telmate's split and the reference-only codegen stance.
- **b:** Fully typed — model every key. Most ergonomic, but large and brittle
  against the unversioned API.
- **c:** Raw strings — expose PVE's `key=val,…` verbatim; caller parses. Minimal
  SDK code, worst consumer experience.
- **other:** \_\_\_\_\_

### OQ-7. Home for the `apidoc.js` schema-drift tool — RESOLVED (a)

Codegen is reference + diff only (resolved); the diff tool still needs a place.

- **a (recommended):** A `cmd/pve-schemadiff` helper, outside the library
  surface, run in CI (built like `cmd/mockpve`); defer the build to late Phase 1
  / Phase 2.
- **b:** `internal/tools/` behind a `tools` build tag.
- **c:** Defer entirely — add it only when drift actually bites.
- **other:** \_\_\_\_\_

### OQ-8. Node-networking package placement — RESOLVED (a)

Phase 5 covers per-node interface config (`/nodes/{node}/network`). The skeleton
has `nodes`, `sdn`, `firewall` — no `network`.

- **a (recommended):** Put node networking under `nodes` (it is node-scoped);
  keep `sdn` and `firewall` separate. Matches the skeleton.
- **b:** A dedicated `network` package for node interface config.
- **other:** \_\_\_\_\_

### OQ-9. Delivery shape — strict phases or a vertical slice first? — RESOLVED (a)

The overview doc suggested proving the architecture end-to-end early.

- **a (recommended):** Finish Phase 1, then a thin vertical slice (qemu
  start/stop + task wait against a live node) before completing Phase 2 — proves
  auth/transport/waiters end-to-end early.
- **b:** Strictly sequential — complete each phase before the next.
- **c:** Full vertical slice across all phases first (auth → qemu → console),
  then backfill breadth.
- **other:** \_\_\_\_\_

### OQ-10. What powers the shipped `mockpve` server? (surfaced by OQ-4) — RESOLVED (a)

OQ-4 chose `go-vcr` cassettes for the SDK's own tests, but go-vcr is client-side
replay — it can't be the `proxmox/mockpve` package / `cmd/mockpve` server
consumers run against, so that server needs its own substrate.

**Decision (a):** one ground-truth corpus, two consumers. Real PVE exchanges are
recorded once; those recordings back the SDK's own `go-vcr` cassettes (OQ-4),
and a **fuzzed** copy of the same recordings becomes the `proxmox/mockpve` /
`cmd/mockpve` server's response set — so the client-side tests and the shipped
mock never drift from real responses. This is the `wiz-go-gen` pattern: tests
built from real recorded responses, then those responses fuzzed to serve the
mock server (see References).

- **a (recommended):** A fixture-backed responder seeded from the **same
  recorded corpus** the cassettes use — one source of truth (real PVE responses)
  powers both go-vcr (our client-side tests) and the mockpve server (consumers).
  Statefulness limited to scripted sequences.
- **b:** A hand-written **stateful in-memory** model (create → list → delete),
  independent of cassettes — more flexible for arbitrary consumer scenarios,
  more to maintain (this was OQ-4's option a).
- **c:** **Defer** the server — ship the recorded corpus + the importable
  transport so consumers wire their own go-vcr; build `cmd/mockpve` later.
- **other:** \_\_\_\_\_

## References

- DESIGN-0001 — Proxmox SDK package layout (the public contract this ledger
  tracks)
- Proxmox VE Roadmap + 9.0/9.1/9.2 release notes
- `devnullvoid/pvetui` `pkg/api` + `pve-openapi-gen` + `mockpve` (structural
  reference)
- `donaldgifford/wiz-go-gen` `test/mock/` — reference for the
  recordings→fuzzed-mock approach (OQ-4 / OQ-5 / OQ-10):
  <https://github.com/donaldgifford/wiz-go-gen/blob/main/test/mock/>
- `bpg/terraform-provider-proxmox` (client + version-gating reference; note its
  9.x HA-API support was still pending — that area is greenfield against the 9.x
  docs)
