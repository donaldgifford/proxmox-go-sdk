---
id: DESIGN-0003
title: "SDN fabrics real paths, node membership, and live status"
status: Implemented
author: Donald Gifford
created: 2026-07-19
---

<!-- markdownlint-disable-file MD025 MD041 -->

# DESIGN 0003: SDN fabrics real paths, node membership, and live status

**Status:** Implemented **Author:** Donald Gifford **Date:** 2026-07-19 (OQs
decided 2026-07-21: all a; implemented 2026-07-23 — merged as v0.7.0 via
IMPL-0004, live-verified on the 9.2.2 pvelab cluster: fabric CRUD + node
enrollment on the nested paths, FRR convergence on the addressed bridge, and the
node-scoped status reads; cassettes replay in CI)

<!--toc:start-->

- [Overview](#overview)
- [Goals and Non-Goals](#goals-and-non-goals)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
- [Background](#background)
- [Detailed Design](#detailed-design)
  - [Fabric CRUD on the real paths](#fabric-crud-on-the-real-paths)
  - [Fabric node membership (new)](#fabric-node-membership-new)
  - [SDN live status (stub → real)](#sdn-live-status-stub--real)
  - [mockpve](#mockpve)
- [API / Interface Changes](#api--interface-changes)
- [Data Model](#data-model)
- [Testing Strategy](#testing-strategy)
- [Migration / Rollout Plan](#migration--rollout-plan)
- [Open Questions](#open-questions)
- [Implementation Corrections (2026-07-21)](#implementation-corrections-2026-07-21)
- [References](#references)
<!--toc:end-->

## Overview

INV-0004 Findings 3 and 6, one PR (`minor`): rewrite SDN fabrics CRUD onto the
**real nested paths** (every fabric write we ship today would 404 live), add the
**fabric node membership** sub-resource we do not model at all, and upgrade the
`SDNStatus`/`VNetStatus` `ErrUnsupported` stubs to the **real node-scoped status
reads** that the 9.2 apidoc confirms exist.

## Goals and Non-Goals

### Goals

- Fabric CRUD that works against real PVE 9.2:
  `/cluster/sdn/fabrics/fabric[/{id}]`.
- Model per-fabric node membership:
  `/cluster/sdn/fabrics/node/{fabric_id}[/{node_id}]`.
- Real SDN status reads: node-scoped zones/content/vnet (plus fabric runtime
  reads per OQ-3).
- mockpve mirrors the real paths (the old fabricated routes are removed).
- Live verification + cassettes on a pvelab clone-up.

### Non-Goals

- The other SDN families the apidoc shows and we do not model (controllers, DNS,
  IPAMs, prefix-lists, route-maps, vnet-firewall) — gap-family work, deferred to
  the group-5 triage.
- First-class SDN config transactions (`lock-token` / `digest` / `rollback`) —
  see OQ-6.
- Modelling `GET /cluster/sdn/fabrics` (the subdir index) or `/all` — see OQ-4.

## Background

Phase 5 task 3 shipped fabrics as REST-with-caveat against a **guessed** flat
path `/cluster/sdn/fabrics[/{id}]`, and task 4 shipped `SDNStatus`/`VNetStatus`
as documented `ErrUnsupported` stubs ("no confirmed REST endpoint"). The real
9.2 apidoc (INV-0004, 675-endpoint set) shows both classifications were wrong:

```text
GET            /cluster/sdn/fabrics            (subdir index)
GET            /cluster/sdn/fabrics/all
GET/POST       /cluster/sdn/fabrics/fabric
GET/PUT/DELETE /cluster/sdn/fabrics/fabric/{id}
GET            /cluster/sdn/fabrics/node
GET/POST       /cluster/sdn/fabrics/node/{fabric_id}
GET/PUT/DELETE /cluster/sdn/fabrics/node/{fabric_id}/{node_id}

GET /nodes/{node}/sdn
GET /nodes/{node}/sdn/zones[/{zone}]
GET /nodes/{node}/sdn/zones/{zone}/{content,bridges,ip-vrf}
GET /nodes/{node}/sdn/vnets/{vnet}[/mac-vrf]
GET /nodes/{node}/sdn/fabrics/{fabric}/{interfaces,neighbors,routes}
```

The apidoc also gives the **field lists** (mined 2026-07-19): fabric carries
`protocol`, `ip_prefix`/`ip6_prefix`, OpenFabric/OSPF timers (`area`,
`csnp_interval`, `hello_interval`), `redistribute`, `route_filter`, plus
transaction fields `lock-token`/`digest`; fabric-node carries `node_id`,
`ip`/`ip6`, `interfaces`, `peers`, and WireGuard-shaped fields (`endpoint`,
`public_key`, `allowed_ips`, `persistent_keepalive`, `role`). Fabric/node writes
return `null` → the Phase 5 "SDN config writes are synchronous" rule holds
unchanged.

## Detailed Design

### Fabric CRUD on the real paths

The five existing methods keep their names and signatures — only `paths.go`
changes: `sdnFabricsPath()` → `/cluster/sdn/fabrics/fabric`, `sdnFabricPath(id)`
→ `/cluster/sdn/fabrics/fabric/{id}`. `ListFabrics` gains nothing; the
`pending`/`running` query filters the apidoc shows are reachable via the
existing functional-option pattern only if we choose to model them (OQ-5).
Version gates unchanged: `SDNFabrics` (9.0 baseline), `FabricProtocolBGP`
refused below 9.2 (`SDNAdvancedFabrics`).

### Fabric node membership (new)

New methods on the same service, following the existing pointer-spec
conventions:

```go
ListFabricNodes(ctx, fabricID) ([]FabricNode, error)
GetFabricNode(ctx, fabricID, nodeID) (*FabricNode, error)
CreateFabricNode(ctx, fabricID, *FabricNodeSpec) error   // sync (null)
UpdateFabricNode(ctx, fabricID, nodeID, *FabricNodeUpdate) error
DeleteFabricNode(ctx, fabricID, nodeID) error
```

`FabricNode` is a lossless read (custom `UnmarshalJSON` +
`fabricNodeKnownFields`, `svcutil.DecodeExtra`); the spec models the observed
fields (`NodeID`, `IP`/`IP6`, `Interfaces []string` — CSV-joined after
`EncodeWithExtra` like ZFS `Devices` — plus the WireGuard fields) with `Extra`
as the escape hatch.

### SDN live status (stub → real)

The stubs are replaced by real reads. Signatures change (breaking, pre-v1; see
[API / Interface Changes](#api--interface-changes)) because the real surface is
**node-scoped** while the stubs were cluster-shaped:

```go
SDNStatus(ctx, node) ([]ZoneStatus, error)          // GET /nodes/{n}/sdn/zones
ZoneContent(ctx, node, zone) ([]VNetStatus, error)  // …/zones/{z}/content
VNetStatus(ctx, node, vnet) (*VNetStatus, error)    // …/vnets/{v}
```

Observed shapes are small — zones return `{zone, status}`, content returns
`{vnet, status, statusmsg}` — modelled as lossless reads anyway
(REST-with-caveat on the remaining fields). Fabric runtime reads
(`interfaces`/`neighbors`/`routes`) are OQ-3; `bridges`/`ip-vrf`/`mac-vrf` ride
the same decision.

### mockpve

`mockpve/sdn.go`: the fabricated flat-fabrics routes are **removed** (the mock
mirrors real PVE — the DLB-route precedent in DESIGN-0004), replaced by the
nested `fabric`/`node` routes; new `AddFabricNode` seeder; new node-scoped
status routes that derive zone/vnet status from seeded SDN state
(`AddZone`/`AddVNet` already exist — status handlers report seeded objects as
`available`).

## API / Interface Changes

- `sdn.API` **breaking changes** (pre-v1, `minor` + changelog BREAKING note):
  `SDNStatus`/`VNetStatus` change signatures (gain `node`; return real types
  instead of erroring); five new fabric-node methods and `ZoneContent` join the
  interface.
- Fabric CRUD signatures are **unchanged** — the path fix is invisible at the
  interface, which is exactly why the old paths shipped unnoticed: nothing but
  real PVE could catch them. (DESIGN-0005's fabrication guard closes that
  class.)
- Root accessor `SDN()` unchanged.

## Data Model

| Type                    | Kind                       | Notes                                |
| ----------------------- | -------------------------- | ------------------------------------ |
| `Fabric`                | lossless read              | exists; new observed fields per OQ-2 |
| `FabricSpec/Update`     | pointer write specs        | exist; fields per OQ-2               |
| `FabricNode`            | lossless read (new)        | `fabricNodeKnownFields` kept in sync |
| `FabricNodeSpec/Update` | pointer write specs (new)  | `Interfaces` CSV-joined              |
| `ZoneStatus`            | lossless read (new)        | `{zone, status}` + Extra             |
| `VNetStatus`            | lossless read (repurposed) | `{vnet, status, statusmsg}` + Extra  |

## Testing Strategy

- Unit: every op against mockpve (happy path + not-found + gate refusal for BGP
  below 9.2); `TestFabricPathsReal` pins the request paths the mock receives (so
  a path regression is visible in-repo).
- Live (pvelab clone-up, one run shared with DESIGN-0004): create an OpenFabric
  fabric spanning the three nested nodes, add/remove a fabric node, read
  node-scoped SDN status, delete the fabric; record cassettes
  (`TestSDNFabricLifecycle`, `TestSDNStatusReads`) and add the certification
  batch entry. Depth per OQ-1.
- Replay: new cassettes wired into `just test-replay`.

## Migration / Rollout Plan

1. Implement + mock-verify (this design), PR labelled `minor` with the BREAKING
   interface note.
2. Live-verify on the shared pvelab run (with DESIGN-0004); reconcile any shape
   divergences into the SDK/mock before committing cassettes.
3. Cassettes + certification entry + ledger notes ride the follow-up commit/PR
   per the established pattern.

Consumers: only `pegaprox-go` (does not use SDN today), so the breaking
interface change is uncontentious.

## Open Questions

1. **How deep does the live verification go?** **Decision (2026-07-21): a.**
   - **a (recommended):** Full: OpenFabric fabric across the three nested
     nodes + node membership + status reads + teardown, recorded. This is the
     only way the _semantics_ (not just paths) get verified, and the nested
     cluster exists precisely for zero-blast-radius experiments.
   - b: CRUD-only — create/read/update/delete a fabric without asserting runtime
     convergence (no interfaces/neighbors checks). Cheaper, but status reads
     would be recorded against an unconverged fabric.

2. **Which observed fabric fields get promoted to typed fields?** **Decision
   (2026-07-21): a.**
   - **a (recommended):** Promote the protocol-neutral core (`Protocol`,
     `IPPrefix`, `IP6Prefix`, `Redistribute`, `RouteFilter`) and leave the
     per-protocol tunables (OpenFabric/OSPF timers, WireGuard keepalive) in
     `Extra` until live verification shows their exact wire forms.
   - b: Promote everything the apidoc lists now. Maximal typing, but each
     guessed wire form is a potential silent mismatch — the exact failure mode
     this PR exists to fix.
   - c: Promote nothing new; `Extra` for all. Safest, least useful.

3. **Are the fabric runtime reads (`interfaces`/`neighbors`/`routes`, plus
   `bridges`/`ip-vrf`/`mac-vrf`) in scope?** **Decision (2026-07-21): a.**
   - **a (recommended):** Yes — they are trivial GETs on the same node-scoped
     surface, land as lossless reads in the same PR, and the live run exercises
     them for free.
   - b: Defer to a follow-up; ship only zones/content/vnet status now.

4. **Do we model `GET /cluster/sdn/fabrics` (index) and `/all`?** **Decision
   (2026-07-21): a.**
   - **a (recommended):** No. The index is a subdir listing (not data) and
     `/all` merges the two collections we already expose; consumers can compose.
     Fewer methods, no information loss.
   - b: Add `ListFabricsAll` for one-call convenience.

5. **Model the `pending`/`running` query filters on fabric reads?** **Decision
   (2026-07-21): a — deferred, revisit post-ship** (see OQ-6).
   - **a (recommended):** Not now — they expose the SDN transaction view; add
     them with transaction support if/when OQ-6 says so. Reads return the
     running config, matching zones/vnets today.
   - b: Add `WithPending`/`WithRunning` functional options now.

6. **SDN config transactions (`lock-token`, `digest`, rollback)?** **Decision
   (2026-07-21): a — deferred, revisit after this design ships and runs live.**
   Deliberately NO placeholder transactions design doc now: the live run will
   teach us how `lock-token`/`digest` actually behave, and a design written then
   is better-informed (a blocked stub today would be speculation — the
   REST-with-caveat failure mode). If a consumer needs transactional SDN, that
   future design also folds in OQ-5's `pending`/`running` filters.
   - **a (recommended):** Out of scope; document that callers needing locked
     applies pass `lock-token` via `Extra`. Revisit as its own design if a
     consumer needs transactional SDN.
   - b: First-class `Lock`/`Apply`/`Rollback` support in this PR.

## Implementation Corrections (2026-07-21)

Recorded while implementing against the mined 9.2 apidoc `returns`/`parameters`
(deeper than the path-level mining this design was written from):

1. **No per-VNet status read.** The planned `VNetStatus(ctx, node, vnet)`
   targeted `GET /nodes/{node}/sdn/vnets/{vnet}` — which is a **subdir index**
   on real PVE (as are `…/zones/{zone}` and `…/fabrics/{fabric}`), not a data
   endpoint. The shipped surface is the leaf reads: `ZoneContent` is the
   per-VNet health read (`{vnet, status?, statusmsg?}` rows) and `VNetMACVRF`
   the EVPN view; `ZoneBridges`/`ZoneIPVRF` and the three fabric runtime reads
   (OQ-3a) complete the eight node-scoped GETs. All field types came from the
   apidoc `returns` blocks (e.g. neighbor `uptime` is an FRR string like
   `8h24m12s`, ip-vrf `metric` is an integer); array-valued fields
   (`ports`/`nexthops`/`via`) stay in `Extra` as raw JSON per the lossless
   pattern.
2. **`Redistribute` is NOT promoted** (amending OQ-2a): the apidoc types
   `redistribute` as an **array** whose wire form is unverified, so promoting it
   would be exactly the guessed-wire-form failure mode OQ-2b was rejected for.
   It stays in `Extra`; promote after the pvelab live run shows the real form. A
   fabric also has **no `nodes`/`comment` fields** at all — membership is solely
   the node sub-collection.
3. **Fabric-node `interfaces` is sent as repeated form values**, not CSV-joined
   (amending the ZFS-`Devices` analogy in the design): the apidoc types it as a
   proper array parameter, and PVE's array params take repeated keys.

## References

- INV-0004 — Findings 3 (fabrics paths) and 6 (SDN status exists)
- Phase 5 design memo (network-sdn-module-architecture) — the original
  REST-with-caveat / stub decisions this design supersedes
- DESIGN-0004 — shares the pvelab live-verification run
- DESIGN-0005 — the fabrication guard preventing this bug class
