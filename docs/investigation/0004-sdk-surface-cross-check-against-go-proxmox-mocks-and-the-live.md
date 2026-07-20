---
id: INV-0004
title:
  "SDK surface cross-check against go-proxmox mocks and the live 9.2 apidoc"
status: Concluded
author: Donald Gifford
created: 2026-07-19
---

<!-- markdownlint-disable-file MD025 MD041 -->

# INV 0004: SDK surface cross-check against go-proxmox mocks and the live 9.2 apidoc

**Status:** Concluded **Author:** Donald Gifford **Date:** 2026-07-19

<!--toc:start-->

- [Question](#question)
- [Hypothesis](#hypothesis)
- [Context](#context)
- [Approach](#approach)
- [Environment](#environment)
- [Findings](#findings)
  - [1. There are no captures to lift — the mocks are authored fixtures](#1-there-are-no-captures-to-lift--the-mocks-are-authored-fixtures)
  - [2. First contact with a real apidoc.js found a pve-schemadiff parser bug](#2-first-contact-with-a-real-apidocjs-found-a-pve-schemadiff-parser-bug)
  - [3. Our SDN fabrics paths are wrong](#3-our-sdn-fabrics-paths-are-wrong)
  - [4. Our HA Dynamic Load Balancer path does not exist](#4-our-ha-dynamic-load-balancer-path-does-not-exist)
  - [5. ArmHA/DisarmHA endpoints exist — the stub is too pessimistic](#5-armhadisarmha-endpoints-exist--the-stub-is-too-pessimistic)
  - [6. SDN live status exists — the stub is too pessimistic](#6-sdn-live-status-exists--the-stub-is-too-pessimistic)
  - [7. Every honesty stub that should stay a stub, stays](#7-every-honesty-stub-that-should-stay-a-stub-stays)
  - [8. Coverage gaps: endpoint families we do not model at all](#8-coverage-gaps-endpoint-families-we-do-not-model-at-all)
- [Open questions](#open-questions)
- [Conclusion](#conclusion)
- [Recommendation](#recommendation)
- [References](#references)
<!--toc:end-->

## Question

Can we reuse the mock corpus in
[`luthermonson/go-proxmox`](https://github.com/luthermonson/go-proxmox)
(`tests/mocks/pve{6,7,8,9}x/`) as captured PVE responses — filling gaps in our
own cassette corpus without having to record them ourselves? And secondarily:
does cross-checking his PVE 9.x fixture corpus against our SDK surface reveal
anything our own verification missed?

## Hypothesis

Probably partial reuse at best. His fixtures may be real captures we could adapt
(license permitting), but our certification model (`certification.yaml` =
"recorded by us against real PVE X.Y, replayed in CI") requires provenance that
third-party fixtures cannot carry. Expected value: shape evidence for our
REST-with-caveat surfaces and a coverage checklist, rather than importable
cassettes.

## Context

The project review (`docs/REVIEW.md`, 2026-07-15) compared this SDK against the
community alternatives and flagged `luthermonson/go-proxmox` as the most
polished of them. Our cassette corpus covers 11 tests across three PVE versions,
but several surfaces shipped as REST-with-caveat (provisional shapes: DLB, SDN
fabrics, DEB822, SMART, ACME) or as `ErrUnsupported` stubs justified by "no
confirmed REST endpoint" (SDN status, HA arm/disarm). Those classifications were
made without an in-repo apidoc; a cheap cross-check against both his corpus and
a real schema dump was overdue.

**Triggered by:** `docs/REVIEW.md` (alternatives comparison); IMPL-0001's
REST-with-caveat / `ErrUnsupported` classifications.

## Approach

1. Shallow-clone `luthermonson/go-proxmox`; inspect `tests/mocks/` structure,
   fixture format, provenance signals, and license.
2. Extract the full endpoint list his `pve9x` fixtures register (gock
   interceptor paths).
3. Fetch the authoritative schema from our own hardware: `apidoc.js` from
   `r740a` (PVE 9.2), parse it into a (method, path) set with
   `cmd/pve-schemadiff` — the tool built for exactly this (IMPL-0001 OQ-7).
4. Cross-check three things against the real set: (a) every surface where our
   shapes are provisional, (b) every `ErrUnsupported` stub's justification, (c)
   endpoint families he covers that we do not.

## Environment

| Component                 | Version / Value                                                |
| ------------------------- | -------------------------------------------------------------- |
| `luthermonson/go-proxmox` | `061e2e6` (shallow clone, 2026-07-19), Apache-2.0              |
| His pve9x corpus          | 14 Go files, ~23.2k lines of gock fixtures (landed 2026-05-30) |
| Real schema               | `apidoc.js` fetched from `r740a` (PVE 9.2), 4.3 MB             |
| Parsed endpoint set       | **675 (method, path) endpoints** via `pve-schemadiff -update`  |
| Parser                    | `cmd/pve-schemadiff/schema` **with the fix from Finding 2**    |

## Findings

### 1. There are no captures to lift — the mocks are authored fixtures

His mocks are **hand-authored gock interceptors with inline JSON literals**, not
wire recordings. Their `AGENTS.md` mandates a hand-written fixture for every
endpoint PR; the `tests/mocks/capture/` package is a multipart-body matcher for
upload tests, not a recorder. Provenance tells: `version.go` returns a
fabricated repoid, and "404" negative fixtures actually `Reply(500)`. The
`pve9x` batch landed 2026-05-30 in a burst of AI-assisted "test: cover X" PRs.
Epistemically these sit at the same tier as our `mockpve` — authored emulation —
so importing them as cassettes would poison `certification.yaml`'s provenance
model ("recorded by us against real PVE X.Y") while adding nothing verified.
License reuse would be legal (Apache-2.0, with attribution); it is the wrong
move anyway.

**The corpus is valuable as a lead generator instead** — and its leads were
good: Findings 3–6 all started as places his fixtures disagreed with our code,
then were settled authoritatively by the real apidoc.

### 2. First contact with a real apidoc.js found a pve-schemadiff parser bug

`schema.Parse` extracted the JSON array between the first `[` and the **last**
`]` in the file. A real `apidoc.js` ships the entire ExtJS API-viewer
application after the schema array, so the last `]` in the file belongs to
viewer code and parsing fails (`invalid character ';' after top-level value`).
The committed synthetic fixture ends at the array, so CI never caught it — the
tool had simply never been fed a real dump. Fixed (working tree, this session):
decode the first complete JSON value with `json.Decoder` and ignore the trailing
app code. Unit tests pass; the real 4.3 MB dump now parses to 675 endpoints.

### 3. Our SDN fabrics paths are wrong

Our provisional flat CRUD on `/cluster/sdn/fabrics[/{id}]` (Phase 5, task 3,
REST-with-caveat) does not match reality. The real 9.2 surface:

```text
GET    /cluster/sdn/fabrics            (index)
GET    /cluster/sdn/fabrics/all
GET    /cluster/sdn/fabrics/fabric
POST   /cluster/sdn/fabrics/fabric
GET/PUT/DELETE /cluster/sdn/fabrics/fabric/{id}
GET    /cluster/sdn/fabrics/node
GET/POST /cluster/sdn/fabrics/node/{fabric_id}
GET/PUT/DELETE /cluster/sdn/fabrics/node/{fabric_id}/{node_id}
```

Every fabric write we ship would 404 live. There is also a second sub-collection
we do not model at all: per-fabric **node membership**. His mocks had the
correct nested shape; the apidoc confirms it.

### 4. Our HA Dynamic Load Balancer path does not exist

`/cluster/ha/lbalancer` (Phase 4, task 4, REST-with-caveat, gated 9.2) is absent
from the real surface — zero hits for `lbalancer` or any balancer-like path.
`GetDLBStatus`/`SetDLBConfig` are fabricated-path ops and must be reclassified
to documented `pverr.ErrUnsupported` per the honesty rule (the
`storage.ExpandRAIDZ` precedent). The real scheduler knobs on 9.2 remain the
`crs` datacenter options we already model (`GetCRSSettings`/`SetCRSSettings`).

### 5. ArmHA/DisarmHA endpoints exist — the stub is too pessimistic

`POST /cluster/ha/status/arm-ha` and `POST /cluster/ha/status/disarm-ha` are
real on 9.2. Our `ArmHA`/`DisarmHA` stubs (justified as "no confirmed REST
endpoint — a GUI/pvecm action") can be upgraded to real ops behind the existing
`HAClusterSwitch` (9.2) gate. Bonus unmodelled reads found next to them:
`GET /cluster/ha/status/current` and `GET /cluster/ha/status/manager_status`.

### 6. SDN live status exists — the stub is too pessimistic

A node-scoped SDN status surface is real on 9.2:

```text
GET /nodes/{node}/sdn
GET /nodes/{node}/sdn/zones[/{zone}]
GET /nodes/{node}/sdn/zones/{zone}/{content,bridges,ip-vrf}
GET /nodes/{node}/sdn/vnets/{vnet}[/mac-vrf]
GET /nodes/{node}/sdn/fabrics/{fabric}/{interfaces,neighbors,routes}
```

Our `SDNStatus`/`VNetStatus` `ErrUnsupported` stubs ("no confirmed REST
endpoint") can become real reads — with the caveat that the stubs are
cluster-shaped while the real surface is node-scoped, so the method signatures
need a node parameter (breaking change to the `sdn.API` interface; acceptable
pre-v1).

### 7. Every honesty stub that should stay a stub, stays

Confirmed **absent** from the real 675-endpoint surface, so these remain
correctly `ErrUnsupported`: storage-level volume-chain snapshots (content stops
at `/{volume}`), ZFS RAIDZ expansion (only create/get/delete under `disks/zfs`),
RBD mirroring (zero hits), OTel metrics config, standalone VNC-ticket verify.
And every REST-with-caveat **path** we shipped is real (`apt/repositories`,
`disks/smart`, `certificates/acme`, ACME accounts, `rrddata`, `download-url`) —
only their payload shapes remain provisional. Small adjacent finds:
`POST`/`PUT /nodes/{node}/storage/{storage}/content/{volume}` (volume copy +
attribute update) exist and are unmodelled.

### 8. Coverage gaps: endpoint families we do not model at all

Present on real 9.2, absent from the SDK (counts are endpoints):

| Family                           | Count | Note                                                                                |
| -------------------------------- | ----- | ----------------------------------------------------------------------------------- |
| `/pools` (resource pools)        | 7     | Full CRUD; distinct from Ceph pools                                                 |
| `/cluster/notifications`         | 31    | Endpoints, targets, matchers                                                        |
| `/cluster/mapping` (PCI/USB/dir) | 16    | Hardware/dir mappings for guests                                                    |
| `/cluster/jobs`                  | 7     | Realm-sync etc.                                                                     |
| `/cluster/bulk-action`           | 6     | 9.x-new bulk guest actions                                                          |
| `GET /cluster/metrics/export`    | 1     | Pull-style full metric export                                                       |
| misc node ops                    | —     | `nextid`, services, dns/hosts/time, `startall`/`stopall`/`migrateall`, subscription |

These are triage candidates against what `pegaprox-go` actually needs, not
obligations.

## Open questions

- Commit the real 9.2 endpoint set as the CI baseline: baseline-only (small,
  needs a documented regeneration step) or baseline + the 4.3 MB `apidoc.js` as
  testdata (self-contained, heavy)? Either way the schema-drift guard finally
  guards the real surface.
- Which of the Finding-8 families does `pegaprox-go` need first? (`/pools` looks
  like the best value-to-effort.)
- Per-minor apidoc dumps (9.1 vs 9.2) would let the drift guard track minor
  deltas — worth folding into the pvelab version-matrix runs?

## Conclusion

**Answer: No to reusing his captures — there are none; yes, emphatically, to the
cross-check.** The mocks are authored fixtures that cannot carry our
certification provenance, but using them as a lead generator — settled against
the authoritative apidoc from our own node — found: **two shipped surfaces that
would fail live** (SDN fabrics paths, DLB path), **two stubs that can be
upgraded to real implementations** (arm/disarm HA, SDN status), one real parser
bug in our drift-guard tooling, confirmation that every honesty-rule stub was
honest, and a concrete map of unmodelled endpoint families. The comparison cost
an afternoon and used only tooling we already had (`pve-schemadiff`) plus one
`curl` from the user.

## Recommendation

In order:

1. **PR: `pve-schemadiff` parser fix + adopt the real 9.2 baseline** so the
   schema-drift job guards the live surface (fix already in the working tree;
   baseline decision per Open questions).
2. **PR (`minor`): fix SDN fabrics** to the real nested `fabric`/`node` paths,
   add the fabric-node membership sub-resource; verify on a pvelab clone-up and
   record cassettes.
3. **PR (`minor`): upgrade `ArmHA`/`DisarmHA` + SDN status to real ops;
   reclassify DLB to `ErrUnsupported`.** Same pvelab run verifies all of it —
   arm/disarm needs the quorate nested cluster, which is exactly what pvelab
   provides.
4. **Triage Finding 8** against `pegaprox-go`'s needs; `/pools` first if any.
   Track as new IMPL tasks rather than growing this INV.

## References

- `docs/REVIEW.md` — the alternatives comparison that triggered this
- IMPL-0001 — REST-with-caveat / `ErrUnsupported` classifications and the
  honesty rule (`storage.ExpandRAIDZ` precedent)
- `cmd/pve-schemadiff` — schema parse/diff tool (IMPL-0001 OQ-7)
- [`luthermonson/go-proxmox`](https://github.com/luthermonson/go-proxmox) @
  `061e2e6` — `tests/mocks/pve9x/`, `AGENTS.md` mock policy, Apache-2.0
- `apidoc.js` from `r740a` (PVE 9.2) — the authoritative 675-endpoint set
  (`/tmp/apidoc-9.2.js`, not committed; see Open questions)
