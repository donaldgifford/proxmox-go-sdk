---
id: IMPL-0007
title: "ACME DNS plugins and node config delivery"
status: Draft
author: Donald Gifford
created: 2026-08-17
---

<!-- markdownlint-disable-file MD025 MD041 -->

# IMPL 0007: ACME DNS plugins and node config delivery

**Status:** Draft **Author:** Donald Gifford **Date:** 2026-08-17 (OQs decided
2026-08-17: all a)

<!--toc:start-->

- [Objective](#objective)
- [Scope](#scope)
  - [In Scope](#in-scope)
  - [Out of Scope](#out-of-scope)
- [Ground facts](#ground-facts)
- [Implementation Phases](#implementation-phases)
  - [Phase 1: ACME plugins — provider model, CRUD, discovery](#phase-1-acme-plugins--provider-model-crud-discovery)
    - [Tasks](#tasks)
    - [Success Criteria](#success-criteria)
  - [Phase 2: Node ACME config](#phase-2-node-acme-config)
    - [Tasks](#tasks-1)
    - [Success Criteria](#success-criteria-1)
  - [Phase 3: Harness, docs, PR](#phase-3-harness-docs-pr)
    - [Tasks](#tasks-2)
    - [Success Criteria](#success-criteria-2)
  - [Phase 4: Live verification (Donald-run)](#phase-4-live-verification-donald-run)
    - [Tasks](#tasks-3)
    - [Success Criteria](#success-criteria-3)
- [File Changes](#file-changes)
- [Testing Plan](#testing-plan)
- [Dependencies](#dependencies)
- [Open Questions](#open-questions)
- [References](#references)
<!--toc:end-->

## Objective

Implement DESIGN-0006: the ACME plugin CRUD surface with the provider-generic
`ACMEPluginData` credential model (typed Cloudflare + Namecheap, `RawPluginData`
for the rest), the ACME discovery reads, and lossless node config get/set with
the `acme`/`acmedomain[n]` property strings typed — then live-verify DNS-01
issuance end-to-end with both providers on one shared domain. Phases 1–3 are one
`minor` PR; Phase 4 is the Donald-run live verification that closes the
REST-with-caveat items.

**Implements:** DESIGN-0006 (OQ decisions 2026-08-17: all a; OQ-6 amended — one
shared domain for both providers, sequential nameserver switch).

## Scope

### In Scope

- `ACMEPluginData` interface + `Cloudflare`, `Namecheap`, `RawPluginData` and
  the KEY=value/base64 render helper.
- Plugin CRUD over `/cluster/acme/plugins` (5 ops) in `proxmox/nodes`.
- Discovery reads: `GetChallengeSchema`, `ListACMEDirectories` (+ `GetACMEMeta`
  per OQ-1).
- `GetNodeConfig`/`SetNodeConfig` over `/nodes/{node}/config`, ACME keys typed,
  digest guard, explicit-delete slot contract.
- mockpve state/routes/seeders for all of the above; coverage report regen (ten
  gap→covered flips; the one annotation edit is the `tos` `out_of_scope` entry,
  OQ-2a).
- Recorder redaction for the plugin `data` field, landing **before** any live
  capture.
- Env-gated integration tests for both providers; TESTING.md walkthrough;
  `nodes` doc promotion + runnable Example.

### Out of Scope

- Typed structs for the other ~158 providers (`RawPluginData` reaches them; add
  on demand).
- Client-side validation against the challenge schema (DESIGN-0006 OQ-4a).
- Any non-ACME typed fields on node config (`wakeonlan` etc. ride in `Extra`).
- The deprecated `/cluster/acme/tos` endpoint (no SDK surface, per the design;
  its coverage-report handling is OQ-2 here).
- ACME account op changes (Phase 6 surface, untouched).

## Ground facts

Checked against the tree and the committed 9.2 apidoc, 2026-08-17:

- The gap rows this closes: 5× `/cluster/acme/plugins` verbs,
  `GET /cluster/acme/challenge-schema`, `GET /cluster/acme/directories`,
  `GET /cluster/acme/meta` (OQ-1), and `GET`/`PUT /nodes/{}/config` — today's
  coverage is 248/675; this lands at 258.
- Discovery return shapes are confirmed in the apidoc and are simple:
  `directories` → `[{name, url}]`; `meta` → an object with
  `caaIdentities`/`externalAccountRequired`/`termsOfService`/`website` and
  `additionalProperties:1` (so the lossless-read pattern applies), taking an
  optional `directory` query param. `challenge-schema` →
  `[{id, name, type, schema}]` with `schema` provider-defined (kept as
  `json.RawMessage`).
- The per-provider credential field names (`CF_Token`, `NAMECHEAP_API_KEY`, …)
  are **runtime** challenge-schema facts, not in the apidoc — the typed structs
  are written from acme.sh's documented variables and confirmed live in Phase 4.
- `acmedomain[n]`'s maximum index is **not stated in the apidoc** (the
  properties key is the wildcard `acmedomain[n]`); the SDK models slots without
  a hard cap and lets the server enforce its limit.
- The plugin `data` wire form is base64 of newline-joined `KEY=value` pairs;
  `GET …/plugins/{id}` returns the stored config, so response bodies carry the
  secret too — both directions need recorder redaction.
- All endpoints are 9.0 baseline: **no version gates anywhere in this
  delivery**.
- mockpve's `nodesadmin.go` already holds cluster `acmeAccounts` state and the
  account routes; plugins/node-config state slots in beside it.

## Implementation Phases

Each phase builds on the previous one. A phase is complete when all its tasks
are checked off and its success criteria are met.

---

### Phase 1: ACME plugins — provider model, CRUD, discovery

The cluster-side surface: the credential model the design exists for, the plugin
CRUD that carries it, and the discovery reads that make the raw escape hatch
self-service.

#### Tasks

- [ ] 1. Provider data model in `proxmox/nodes/acmeproviders.go`: the two-method
     `ACMEPluginData` interface, `Cloudflare` (Token/AccountID/ ZoneID/Key/Email
     → `CF_*`), `Namecheap` (Username/APIKey/SourceIP → `NAMECHEAP_*`),
     `RawPluginData{Provider, Values}`, and the unexported render helper (sorted
     `KEY=value` lines, empty values omitted, base64). Doc comments flag the
     values as credentials (never log), Namecheap as unit-verified-only until
     Phase 4, and the field names as confirmed-live-via-challenge-schema. Unit
     tests: exact base64 output for both typed providers and raw;
     empty-omission; sort stability.
- [ ] 2. Plugin types + paths in `proxmox/nodes/acmeplugins.go`: lossless
     `ACMEPlugin` read (`Plugin`, `Type`, `API`, `Data` verbatim base64,
     `ValidationDelay`, `Nodes` CSV, `Disable`, `Digest`, `Extra` via
     `svcutil.DecodeExtra`), `ACMEPluginSpec` (ID required;
     `Data ACMEPluginData` required when Type is dns; `Nodes []string`
     `json:"-"` CSV-joined post-encode), `ACMEPluginUpdate` (+
     `Delete []string`, `Digest`), path helpers, and `TestACMEPathsReal` pinning
     the plugin paths (the `TestCephPathsReal` pattern).
- [ ] 3. mockpve plugin state in `mockpve/nodesadmin.go`: `acmePlugins` map (id
     → record with digest), routes for the five verbs through `handle`,
     `AddACMEPlugin` seeder, digest-mismatch conflict on update (mirror real
     PVE's refusal), `delete` param honored. The mock stores `data` verbatim and
     returns it on get/list, same as real PVE.
- [ ] 4. The five CRUD ops (`ListACMEPlugins`/`GetACMEPlugin`/
     `CreateACMEPlugin`/`UpdateACMEPlugin`/`DeleteACMEPlugin`), all synchronous
     (`error`, never `tasks.Ref`). Unit round-trips against mockpve: create with
     typed `Cloudflare` data → get returns the exact base64; create with
     `RawPluginData`; update with `Delete` + digest-mismatch rejection;
     standalone type with no data; nil-spec and missing-ID guards.
- [ ] 5. Discovery reads: `GetChallengeSchema` (`ChallengeSchemaEntry`, `Schema`
     as `json.RawMessage`), `ListACMEDirectories` (`ACMEDirectory{Name, URL}`),
     and `GetACMEMeta` per OQ-1 (lossless `ACMEMeta`, optional
     `WithACMEDirectory` query option). mockpve serves static payloads: two
     schema entries (`cf`, `namecheap` with plausible field schemas), the two
     Let's Encrypt directories, a static meta. Unit tests decode all three.

#### Success Criteria

- `go build ./...`, `just lint`, `just test` (race) green.
- Plugin CRUD round-trips against mockpve, including the digest guard and the
  verbatim-base64 property (what was rendered at create is what get returns).
- `just coverage-check` green with the eight cluster-ACME rows flipped to
  covered and zero unmatched routes — the fabrication guard is the proof the
  paths are real.

---

### Phase 2: Node ACME config

The node-side wiring: lossless config get/set with the ACME property strings
typed both ways.

#### Tasks

- [ ] 1. Property-string parse/render in `proxmox/nodes/nodeconfig.go`:
     `NodeACME` ↔ `acme` (`account=…[,domains=…;…]`) and `ACMEDomain` ↔
     `acmedomain[n]` (`[domain=]<domain>[,plugin=…][,alias=…]`), following the
     CRS-settings precedent (typed both ways, no Extra inside a compound
     property). Table-driven tests: default-key domain form, all-options form,
     round-trip stability, malformed-string rejection.
- [ ] 2. `NodeConfig` lossless read (`ACME`, `ACMEDomains` index-ordered,
     `Digest`, `Extra`) + `GetNodeConfig`; `NodeConfigUpdate` + `SetNodeConfig`
     with the explicit-delete contract (writes only the slots given; clearing is
     `Delete: ["acmedomain1"]`, never implicit diffing) and the `Digest`
     concurrent-write guard. Extend `TestACMEPathsReal` with the node-config
     path.
- [ ] 3. mockpve node-config state: per-node config map + digest,
     `GET`/`PUT /nodes/{node}/config` routes through `handle`, digest-mismatch
     conflict, `delete` param honored, seeder. Unit round-trips: set two
     acmedomain slots + account → get parses both; delete one slot; digest
     rejection; non-ACME keys survive via `Extra` untouched.
- [ ] 4. Regenerate `docs/COVERAGE.md` (`just coverage`) — the two node-config
     rows flip; total lands at 258 (OQ-1a) — and add the `/cluster/acme/tos`
     `out_of_scope` annotation with its deprecation reason (OQ-2a, the section's
     first non-empty entry).

#### Success Criteria

- Property-string round-trips pass, including multi-slot `acmedomain[n]` and the
  explicit-delete contract.
- `just coverage-check` green; the committed report is current with all intended
  flips and no annotation drift.
- `just lint` + `just test` (race) green.

---

### Phase 3: Harness, docs, PR

Recorder safety, the runnable documentation, the integration tests that Phase 4
will execute, and the release PR.

#### Tasks

- [ ] 1. Recorder redaction FIRST: the integration harness's `BeforeSaveHook`
     scrubs the `data` form field (request side) and the base64 `data` values in
     plugin response bodies to `REDACTED`, plus the go-vcr parsed-`Form` map
     (the 2026-07-23 leak-review precedent). Extend `TestRedactInteraction` with
     a plugin-create interaction so the scrub is pinned before any live capture
     exists.
- [ ] 2. Env-gated integration tests in `proxmox/integration/`
     (`//go:build integration`): `TestACMEDNSCloudflare` and
     `TestACMEDNSNamecheap`, gated per OQ-3's env set, both skipping cleanly
     without a node/domain. Flow per the design: staging directory via
     `ListACMEDirectories` → register account → create plugin → `acmedomain0` +
     account on the target node → order → await task → verify the served
     certificate's SAN → revoke → restore node config + delete plugin/account.
     Teardown uses `cleanupCtx`, never `context.Background()`. Compile-verified
     via `go vet -tags=integration ./proxmox/integration/`.
- [ ] 3. TESTING.md: an ACME DNS section — domain + scoped-token prep, the env
     set, Let's Encrypt staging, the sequential provider dance (Cloudflare run →
     nameserver switch + propagation wait → Namecheap run), and the cassette
     leak-review checklist addition (`data` must read REDACTED).
- [ ] 4. Docs promotion: `nodes/doc.go` gains the ACME story (accounts → plugins
     → node config → order, with the provider-generic model); a runnable
     `Example` (named, e.g. `Example_acmeDNS`) wiring plugin-create → node
     config → order against mockpve with an `// Output:` block; `go doc ./...`
     renders cleanly.
- [ ] 5. PR: exactly one `minor` label (new public API on `proxmox/nodes`);
     changelog as the branch's final commit; DESIGN-0006 status → Implemented in
     the same PR; merge → auto-release.

#### Success Criteria

- All CI jobs green on the PR: `just test` (race), `just test-replay`,
  `just lint`, schema-drift, `just coverage-check`, goreleaser snapshot.
- `TestRedactInteraction` proves a plugin-create interaction is scrubbed before
  any cassette exists.
- The integration tests compile under the tag and skip without env; the Example
  runs deterministically under `go test`.
- PR merged and the tag minted (Donald fires the merge).

---

### Phase 4: Live verification (Donald-run)

Executes on real PVE with the shared domain (DESIGN-0006 OQ-6). Everything here
is Donald-run; SDK-side findings fold back as fixes + cassette commits. Until
this phase completes, the ACME surface is mock-verified and says so.

#### Tasks

- [ ] 1. Environment prep: the shared domain's zone on Cloudflare DNS with a
     scoped API token (Zone.DNS edit on that zone only); env per OQ-3 exported
     alongside the existing `PVE_*` set; target node per OQ-4 (pvelab nested
     node recommended — verify its outbound reachability to the Let's Encrypt
     staging directory and the Cloudflare API early, before burning an order
     attempt).
- [ ] 2. Cloudflare run: `TestACMEDNSCloudflare` with `PVE_RECORD=1` — staging
     account, cf plugin, order, SAN verify, revoke, cleanup. Confirm the typed
     `Cloudflare` field names against the node's live `GetChallengeSchema`
     output; fix the struct if drift is found (this is the design's confirm-live
     item).
- [ ] 3. Cassette leak review + commit: `data` REDACTED in both directions,
     token absent, topology scrubbed (domain rewritten by the existing scrub;
     add a `PVE_SCRUB_EXTRA` pair if the real domain leaks anywhere unexpected);
     wire the cassettes into `just test-replay`; replay green in CI.
- [ ] 4. Namecheap run: switch the domain's nameservers to Namecheap DNS, wait
     out propagation, run `TestACMEDNSNamecheap` with `PVE_RECORD=1` (Namecheap
     API allowlist needs the node's egress IP); confirm the `Namecheap` field
     names live; leak-review + commit + replay as above. Drop the "unit-verified
     only" caveat from the Namecheap doc comment.
- [ ] 5. Caveat closure: update the Phase-6 order/renew/revoke REST-with-caveat
     comments with the observed task-vs-sync behaviour; record the run in
     `certification.yaml`; tick this ledger; flip IMPL-0007 status → Completed
     (docs ride a `dont-release` PR or the next release PR).

#### Success Criteria

- Both provider runs issue (staging), verify, and revoke live; cassettes
  committed, leak-reviewed, and replaying green in CI.
- The typed provider field names are live-confirmed against
  `GetChallengeSchema`; no REST-with-caveat remains anywhere on the ACME surface
  (including the Phase-6 order/renew/revoke task-vs-sync note).
- Zero credential or topology leaks in the committed cassettes.

---

## File Changes

| File                                           | Action | Description                                                      |
| ---------------------------------------------- | ------ | ---------------------------------------------------------------- |
| `proxmox/nodes/acmeproviders.go`               | Create | `ACMEPluginData` + Cloudflare/Namecheap/Raw + render helper      |
| `proxmox/nodes/acmeplugins.go`                 | Create | Plugin types + five CRUD ops + discovery reads                   |
| `proxmox/nodes/nodeconfig.go`                  | Create | `NodeConfig` + property-string codecs + get/set                  |
| `proxmox/nodes/paths.go`                       | Modify | Plugin/schema/directories/meta/node-config path helpers          |
| `proxmox/nodes/doc.go`                         | Modify | ACME story promotion                                             |
| `proxmox/nodes/example_test.go`                | Modify | `Example_acmeDNS`                                                |
| `proxmox/mockpve/nodesadmin.go`                | Modify | `acmePlugins` + node-config state, routes, seeders, digest guard |
| `proxmox/integration/recorder_test.go`         | Modify | `data` redaction both directions + test                          |
| `proxmox/integration/acme_test.go`             | Create | The two env-gated live tests                                     |
| `docs/COVERAGE.md`                             | Modify | Regenerated: ten flips                                           |
| `cmd/pve-schemadiff/coverage-annotations.yaml` | Modify | tos out_of_scope entry (OQ-2a)                                   |
| `TESTING.md`                                   | Modify | ACME DNS walkthrough                                             |
| `docs/design/0006-acme-dns-plugins-…md`        | Modify | Status → Implemented (Phase 3 task 5)                            |

## Testing Plan

- Unit tests beside the code against mockpve for every exported op (repo
  convention); table-driven for the render helper and property-string codecs.
- `TestACMEPathsReal` pins every literal path in-repo.
- Coverage drift + fabrication guard run in CI on every push (IMPL-0006).
- Redaction pinned by `TestRedactInteraction` before any live capture.
- Live behaviour via the two env-gated integration tests (Phase 4), recorded and
  replayed in CI thereafter.

## Dependencies

- DESIGN-0006 merged (PR #26) — the approved design this implements.
- Phase 4 needs: the shared domain, a scoped Cloudflare token, Namecheap API
  credentials + egress-IP allowlist entry, and a reachable PVE node (per OQ-4).
  None of these block Phases 1–3.
- No new Go module dependencies.

## Open Questions

1. **Does `GetACMEMeta` ride along in Phase 1?** **Decision (2026-08-17): a.**
   - **a (recommended):** Yes. The apidoc confirms a simple lossless object
     (`termsOfService`, `website`, `caaIdentities`, `externalAccountRequired`,
     `additionalProperties:1`) with an optional `directory` query param — one
     small read op closes the whole non-deprecated cluster-ACME family (10th
     flip), and it is the modern replacement for the deprecated `tos` the design
     already excludes.
   - b: Skip it; land plugins + schema + directories only (9 flips). Smaller PR
     by ~40 lines, but the family stays one row short for no real saving, and a
     consumer wanting the CA's ToS URL (needed to set `ACMEAccountSpec.TOSURL`
     honestly) has no SDK path to it.

2. **How does the coverage report treat the deprecated `/cluster/acme/tos`?**
   **Decision (2026-08-17): a.**
   - **a (recommended):** Add an `out_of_scope` annotation with reason
     "deprecated upstream in favour of /cluster/acme/meta; no SDK surface by
     design (DESIGN-0006)". This is exactly what the annotations file exists for
     — a triaged, reasoned exclusion — and it stops the row reading as future
     work. First non-empty entry in the `out_of_scope` section.
   - b: Leave it as a `gap`. Zero annotation churn and "the percentage is not a
     target", but the report then permanently shows one ACME row that looks
     undone when it is actually a deliberate exclusion recorded only in a design
     doc.

3. **Integration-test env shape (amends the design's sketch)?** **Decision
   (2026-08-17): a.**
   - **a (recommended):** Direct-value envs, matching the suite's existing
     convention (`PVE_TOKEN_SECRET`, `PVE_TEST_STORAGE`, …):
     `PVE_TEST_ACME_DOMAIN`, `PVE_TEST_ACME_CF_TOKEN` (+ optional
     `PVE_TEST_ACME_CF_ACCOUNT_ID`), and `PVE_TEST_ACME_NC_USERNAME`/
     `PVE_TEST_ACME_NC_API_KEY`/`PVE_TEST_ACME_NC_SOURCE_IP`. The design's
     `PVE_TEST_ACME_CF_TOKEN_ENV` name implied pvelab-style env-var-NAME
     indirection, which is the config-file convention, not the integration-suite
     one — this OQ records the correction.
   - b: Keep the `*_ENV` indirection as sketched in DESIGN-0006. Consistent with
     pvelab.yaml's secrets pattern, but the integration suite has no config file
     to hold the names — the indirection adds a hop with nothing to justify it.

4. **Which node hosts the Phase-4 live runs?** **Decision (2026-08-17): a.**
   - **a (recommended):** A pvelab nested node. Ordering an ACME cert REPLACES
     the node's pveproxy certificate — on a disposable clone that is free; the
     nested nodes already have outbound gateway+DNS from the install, DNS-01
     needs no inbound reachability, and the staging-cert-on-real-UI problem
     never arises. Task 1 verifies outbound reach to LE staging + the provider
     APIs before the first order.
   - b: r740a directly. Simplest network path and no lab spin-up, but the
     staging (untrusted) certificate lands on the real node's UI until a
     revoke/restore, and a cleanup failure leaves the homelab's actual PVE web
     UI on a broken cert.
   - c: Both — first run on pvelab, then a confirmation order on r740a with the
     production directory once trusted certs are actually wanted there. That
     second half is homelab ops, not SDK verification; it can happen any time
     after this ledger closes.

## References

- DESIGN-0006 — the approved design (OQs all a, 2026-08-17); PR #26.
- `docs/COVERAGE.md` — the gap rows being closed.
- `cmd/pve-schemadiff/testdata/apidoc-9.2.js.gz` — wire-shape source (r740a
  capture, 2026-07-19).
- `proxmox/nodes/certificates.go` — the Phase-6 ACME account/order ops this
  extends, including the task-vs-sync REST-with-caveat Phase 4 resolves.
- IMPL-0006 / DESIGN-0005 — the coverage tracker guarding the new routes.
- TESTING.md — the live-run walkthrough this delivery extends.
