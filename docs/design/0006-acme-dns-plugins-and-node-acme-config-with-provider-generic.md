---
id: DESIGN-0006
title: "ACME DNS plugins and node ACME config with provider-generic credentials"
status: Implemented
author: Donald Gifford
created: 2026-08-16
---

<!-- markdownlint-disable-file MD025 MD041 -->

# DESIGN 0006: ACME DNS plugins and node ACME config with provider-generic credentials

**Status:** Implemented **Author:** Donald Gifford **Date:** 2026-08-16 (OQs
decided 2026-08-17: all a; OQ-6 amended — one shared domain for both providers,
sequential runs)

<!--toc:start-->

- [Overview](#overview)
- [Goals and Non-Goals](#goals-and-non-goals)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
- [Background](#background)
  - [The PVE ACME model](#the-pve-acme-model)
  - [What the SDK covers today](#what-the-sdk-covers-today)
  - [Wire facts (from the committed 9.2 apidoc)](#wire-facts-from-the-committed-92-apidoc)
- [Detailed Design](#detailed-design)
  - [Provider-generic credentials: the ACMEPluginData interface](#provider-generic-credentials-the-acmeplugindata-interface)
  - [Plugin CRUD](#plugin-crud)
  - [Challenge schema and directories (discovery reads)](#challenge-schema-and-directories-discovery-reads)
  - [Node ACME config](#node-acme-config)
  - [Secret handling](#secret-handling)
  - [mockpve](#mockpve)
- [API / Interface Changes](#api--interface-changes)
- [Data Model](#data-model)
- [Testing Strategy](#testing-strategy)
- [Migration / Rollout Plan](#migration--rollout-plan)
- [Open Questions](#open-questions)
- [References](#references)
<!--toc:end-->

## Overview

Close the two SDK gaps that block hands-off ACME DNS-01 certificate issuance on
a PVE cluster: **ACME plugin CRUD** (`/cluster/acme/plugins`, where the DNS
provider credentials live) and **node ACME config**
(`GET`/`PUT /nodes/{node}/config`, where a node is pointed at a domain and a
plugin). The credentials surface is **provider-generic** — a small interface
with typed per-provider implementations — so adding a provider is a new struct,
not a new API. Cloudflare is the first typed provider (it is what we will test
and run first); Namecheap is the second, added to prove the generic approach
holds beyond one provider.

## Goals and Non-Goals

### Goals

- Full CRUD for ACME plugins: list, get, create, update, delete over
  `/cluster/acme/plugins`, flipping five coverage `gap` rows to `covered`.
- A provider-generic credential model: one interface (`ACMEPluginData`), typed
  `Cloudflare` and `Namecheap` implementations, and a raw escape hatch that
  reaches all 160 providers PVE's enum admits without SDK changes.
- Node ACME wiring: lossless `GetNodeConfig` + `SetNodeConfig` with the `acme`
  and `acmedomain[n]` property strings modelled as typed fields, flipping the
  two `/nodes/{}/config` gap rows.
- The ACME discovery reads that make the generic model usable at runtime:
  `/cluster/acme/challenge-schema` (per-provider credential field schema) and
  `/cluster/acme/directories` (named CA endpoints).
- End-to-end story: with these ops plus the existing account +
  order/renew/revoke ops, a consumer can take three joined nodes to
  DNS-01-issued certificates entirely through the SDK.

### Non-Goals

- **Typed structs for all 160 providers.** The interface plus the raw escape
  hatch covers them; typed structs are added on demand (each is ~20 lines).
- **Client-side re-validation of provider credentials.** PVE validates plugin
  data server-side against its own challenge schema; the SDK does not duplicate
  that (see OQ-4).
- **ACME account changes.** `RegisterACMEAccount` and friends shipped in Phase 6
  and are untouched.
- **The `standalone` (HTTP-01) challenge type beyond pass-through.** The spec
  models it (it is one of two enum values) but the design's focus is DNS-01;
  standalone needs no plugin data.
- **General node-config semantics beyond ACME.** `GetNodeConfig` is lossless so
  nothing is dropped, but only the ACME keys get typed fields (see OQ-5 for the
  alternative).

## Background

### The PVE ACME model

PVE's ACME flow has four steps, each with its own endpoint family:

1. **Account** — register with the CA (`/cluster/acme/account`, cluster-scoped).
2. **Plugin** — a named credential bundle for a DNS provider
   (`/cluster/acme/plugins`, cluster-scoped). A plugin has an `id`, a `type`
   (`dns` or `standalone`), an `api` (the acme.sh plugin name — `cf` for
   Cloudflare, `namecheap` for Namecheap), and `data`: the provider's credential
   environment variables as base64-encoded `KEY=value` lines.
3. **Node config** — each node's `/nodes/{node}/config` carries `acme`
   (`account=<name>`) and `acmedomain[n]` (`<domain>[,plugin=<id>][,alias=…]`)
   property strings binding that node's certificate domains to a plugin.
   Omitting `plugin` means the standalone HTTP-01 challenge, so the plugin
   reference is exactly the DNS-01 switch.
4. **Order** — `POST /nodes/{node}/certificates/acme/certificate` issues; PUT
   renews; DELETE revokes.

### What the SDK covers today

Steps 1 and 4 shipped in Phase 6 (`proxmox/nodes/certificates.go`):
`ListACMEAccounts`/`GetACMEAccount`/`RegisterACMEAccount`/`UpdateACMEAccount`/
`DeactivateACMEAccount` and `OrderNodeCertificate`/`RenewNodeCertificate`/
`RevokeNodeCertificate` (all task-returning, REST-with-caveat on task-vs-sync).
Steps 2 and 3 are explicit `gap` rows in `docs/COVERAGE.md`: the five
`/cluster/acme/plugins` verbs, `GET /cluster/acme/challenge-schema`,
`GET /cluster/acme/directories`, `GET /cluster/acme/meta`, and
`GET`/`PUT /nodes/{}/config`.

### Wire facts (from the committed 9.2 apidoc)

Everything below is read from `cmd/pve-schemadiff/testdata/apidoc-9.2.js.gz`
(the r740a capture), not guessed:

- `POST /cluster/acme/plugins` params: `id` (pve-configid), `type` (enum `dns`,
  `standalone`), `api` (enum of **160** acme.sh plugin names; `cf` and
  `namecheap` both present), `data` ("DNS plugin data. (base64 encoded)"),
  `validation-delay` (seconds; exists to cope with long DNS TTLs), `nodes` (list
  of cluster node names the plugin applies to), `disable`.
- `PUT /cluster/acme/plugins/{id}` adds `delete` (unset keys) and `digest`
  (concurrent-write guard).
- `GET /cluster/acme/challenge-schema` returns `[{id, name, type, schema}]` —
  PVE publishes the per-provider credential field schema itself. The concrete
  field names (`CF_Token`, `CF_Account_ID`, `NAMECHEAP_API_KEY`, …) live in this
  runtime response, **not** in the apidoc, so the typed provider structs below
  are written from acme.sh's documented variables and confirmed against a live
  node's challenge-schema during verification.
- `PUT /nodes/{node}/config` params: `acme`
  (`[account=<name>] [,domains=<domain[;domain;…]>]`), `acmedomain[n]`
  (`[domain=]<domain> [,alias=<domain>] [,plugin=<id>]`), `digest`, `delete`,
  plus non-ACME keys (`description`, `wakeonlan`, `ballooning-target`,
  `startall-onboot-delay`, `location`).
- `GET /nodes/{node}/config` takes an optional `property` filter.

## Detailed Design

### Provider-generic credentials: the `ACMEPluginData` interface

The generic seam is deliberately tiny — two methods, no registration, no
reflection:

```go
// ACMEPluginData supplies a DNS provider's acme.sh credentials for an ACME
// plugin. Implementations render to the KEY=value lines PVE expects in the
// plugin's base64 data field. The SDK ships typed implementations for the
// providers it has verified (Cloudflare, Namecheap) and RawPluginData for
// every other provider PVE's api enum admits.
type ACMEPluginData interface {
	// API returns the acme.sh plugin name PVE's api parameter expects,
	// e.g. "cf" or "namecheap".
	API() string
	// Data returns the provider's credential environment variables. Empty
	// values are omitted from the rendered payload.
	Data() map[string]string
}
```

The SDK owns the wire mechanics: it renders `Data()` to sorted newline-separated
`KEY=value` lines (sorted for deterministic tests), base64s the result, and sets
both `api` and `data` on the request. A caller never touches base64.

Typed providers are plain structs — adding one is additive and mechanical:

```go
// Cloudflare holds Cloudflare DNS credentials for the acme.sh "cf" plugin.
// Prefer the scoped API Token (Zone.DNS edit) over the legacy Key+Email
// global key. Field names follow acme.sh's environment variables.
type Cloudflare struct {
	Token     string // CF_Token (scoped API token, recommended)
	AccountID string // CF_Account_ID
	ZoneID    string // CF_Zone_ID (optional; skips zone lookup)
	Key       string // CF_Key (legacy global API key)
	Email     string // CF_Email (legacy, with Key)
}

func (c Cloudflare) API() string { return "cf" }
func (c Cloudflare) Data() map[string]string { … }

// Namecheap holds Namecheap DNS credentials for the acme.sh "namecheap"
// plugin. All three fields are required by the provider (the API is
// IP-allowlisted).
type Namecheap struct {
	Username string // NAMECHEAP_USERNAME
	APIKey   string // NAMECHEAP_API_KEY
	SourceIP string // NAMECHEAP_SOURCEIP
}

func (n Namecheap) API() string { return "namecheap" }
func (n Namecheap) Data() map[string]string { … }

// RawPluginData reaches any of PVE's other supported providers without a
// typed struct: Provider is the acme.sh plugin name, Values the credential
// variables. Use GetChallengeSchema to discover a provider's field names.
type RawPluginData struct {
	Provider string
	Values   map[string]string
}
```

Two providers in the first PR is a design requirement, not scope creep: one
implementation proves nothing about an interface. Namecheap is also deliberately
shaped differently from Cloudflare (all-required fields, IP-allowlisted API) so
the interface is exercised by a provider that is not Cloudflare-shaped.

### Plugin CRUD

Five ops beside the ACME account ops (placement per OQ-1), following the Phase 6
patterns exactly — lossless read, pointer specs, sync writes:

```go
func (s *Service) ListACMEPlugins(ctx context.Context) ([]ACMEPlugin, error)
func (s *Service) GetACMEPlugin(ctx context.Context, id string) (*ACMEPlugin, error)
func (s *Service) CreateACMEPlugin(ctx context.Context, spec *ACMEPluginSpec) error
func (s *Service) UpdateACMEPlugin(ctx context.Context, id string, update *ACMEPluginUpdate) error
func (s *Service) DeleteACMEPlugin(ctx context.Context, id string) error
```

All writes are synchronous (`error`, never `tasks.Ref`) — these are
`pmxcfs`-backed config writes like HA/SDN/firewall, and the apidoc shows no UPID
return.

### Challenge schema and directories (discovery reads)

```go
// ChallengeSchemaEntry is one provider from GET /cluster/acme/challenge-schema:
// the provider id (= the api parameter value), its human-readable name, and
// the raw per-field schema PVE publishes for it.
type ChallengeSchemaEntry struct {
	ID     string          `json:"id"`
	Name   string          `json:"name,omitempty"`
	Type   string          `json:"type"`
	Schema json.RawMessage `json:"schema"`
}

func (s *Service) GetChallengeSchema(ctx context.Context) ([]ChallengeSchemaEntry, error)
func (s *Service) ListACMEDirectories(ctx context.Context) ([]ACMEDirectory, error)
```

`Schema` stays raw (`json.RawMessage`): its shape is provider-defined and
unverified in-repo, and the honest move is verbatim preservation — the same call
the SDK made with `ACMEAccount.Account`. `GetChallengeSchema` is what makes
`RawPluginData` self-service: a consumer can enumerate providers and their field
names at runtime instead of reading acme.sh source. `ListACMEDirectories`
returns the named CA endpoints (Let's Encrypt production/staging), which the
verification plan uses to point the lab at staging. `GET /cluster/acme/meta`
rides along only if trivially decodable; `/cluster/acme/tos` is deprecated
upstream in favour of `meta` and is NOT added (honesty rule: no surface for a
deprecated endpoint).

### Node ACME config

Two ops in `proxmox/nodes` (node-per-call, matching the package's existing
shape):

```go
// NodeConfig is the lossless read of GET /nodes/{node}/config. The ACME keys
// are typed; every other key is preserved in Extra.
type NodeConfig struct {
	ACME        *NodeACME    // parsed "acme" property string
	ACMEDomains []ACMEDomain // parsed "acmedomain0".."acmedomainN", index-ordered
	Digest      string       // concurrent-write guard for SetNodeConfig
	Extra       map[string]string
}

// NodeACME is the "acme" property string: account=<name>[,domains=…].
type NodeACME struct {
	Account string
	Domains []string // legacy semicolon list; prefer ACMEDomains
}

// ACMEDomain is one "acmedomain[n]" property string.
type ACMEDomain struct {
	Domain string // required
	Plugin string // ACME plugin ID; empty means the standalone HTTP-01 challenge
	Alias  string // optional CNAME alias domain for the DNS challenge
}

func (s *Service) GetNodeConfig(ctx context.Context, node string) (*NodeConfig, error)
func (s *Service) SetNodeConfig(ctx context.Context, node string, update *NodeConfigUpdate) error
```

`NodeConfigUpdate` mirrors the read type with pointer/omit-empty semantics plus
`Delete []string` and `Digest string` (pass the read's digest to guard against
concurrent edits), and the standard `Extra` escape hatch for the non-ACME keys.
The property-string parse/render pairs (`acme`, `acmedomain[n]`) follow the
CRS-settings precedent in `ha` — typed both ways, no `Extra` inside a compound
property.

The index-slot mapping (`ACMEDomains[i]` ↔ `acmedomain<i>`) is the one subtle
contract: `SetNodeConfig` writes the slots it is given and deletes nothing
implicitly; clearing a slot is an explicit `Delete: ["acmedomain1"]`.
Symmetric-but-explicit beats magical diffing.

### Secret handling

The plugin `data` field carries live provider credentials (a Cloudflare API
token). Three consequences, all in scope:

1. **Transport logging is already safe** — `PVE_DEBUG` logs method+URL only,
   never bodies. The spec doc comments state that `Data()` values are
   credentials and that `ACMEPluginSpec` must not be logged by consumers.
2. **The go-vcr recorder must scrub it.** The `BeforeSaveHook` in
   `proxmox/integration/recorder_test.go` gains the `data` form field (and the
   base64 blob in response bodies, since `GET …/plugins/{id}` returns the stored
   config) → `REDACTED`. This lands WITH the recording harness change, before
   any live capture — the 2026-07-23 `Request.Form` leak-review gap is the
   cautionary precedent.
3. **Reads do not decode.** `ACMEPlugin.Data` (the read type) keeps the base64
   string verbatim; the SDK offers no decode helper, so a consumer printing the
   struct dumps base64, not plaintext. (Deliberate speed bump, not security —
   base64 is not encryption; it just keeps casual logs clean.)

### mockpve

`mockpve/nodesadmin.go` already holds the cluster `acmeAccounts` state; it gains
`acmePlugins` (map by id, with digest), the challenge-schema/directories static
responses (two hardcoded providers: `cf`, `namecheap` — enough for the Example
and the RawPluginData test), and per-node `nodeConfig` maps. Routes are
registered through `handle` as always, and — the IMPL-0006 dividend — the
fabrication guard structurally guarantees every new route matches a real
baseline endpoint. All the paths are in the baseline, so this PR flips nine
`gap` rows to `covered` (ten if `meta` rides along) with zero annotation
changes.

## API / Interface Changes

All additive (a `minor` PR):

- `ACMEPluginData` interface + `Cloudflare`, `Namecheap`, `RawPluginData`.
- `ACMEPlugin`, `ACMEPluginSpec`, `ACMEPluginUpdate`, five plugin CRUD ops.
- `ChallengeSchemaEntry`, `ACMEDirectory`, `GetChallengeSchema`,
  `ListACMEDirectories`.
- `NodeConfig`, `NodeACME`, `ACMEDomain`, `NodeConfigUpdate`, `GetNodeConfig`,
  `SetNodeConfig`.
- mockpve seeders: `AddACMEPlugin`, `SetNodeConfig` (mock-side), plus the static
  schema/directories.

No changes to existing types, no transport changes, no new gates (every endpoint
here is 9.0 baseline).

## Data Model

- `ACMEPlugin` (read): `Plugin` (id), `Type`, `API`, `Data` (base64, verbatim),
  `ValidationDelay`, `Nodes []string` (CSV on the wire), `Disable`, `Digest`,
  `Extra` — lossless via the standard `UnmarshalJSON` + `svcutil.DecodeExtra`
  pattern.
- `ACMEPluginSpec` (write): `ID` (required), `Type` (default `dns`),
  `Data ACMEPluginData` (required for dns), `ValidationDelay *int`,
  `Nodes []string` (`json:"-"`, CSV-joined post-encode — the ZFS `Devices`
  precedent), `Disable *types.PVEBool`, `Extra map[string]string`.
- `ACMEPluginUpdate`: same optional fields plus `Delete []string` and
  `Digest string`.
- Rendered `data` wire form: `base64(join(sort(k+"="+v), "\n"))`, empty values
  omitted.

## Testing Strategy

- **Unit (mockpve)**: plugin CRUD round-trip; `Cloudflare.Data()` /
  `Namecheap.Data()` rendering (sorted, empties omitted, base64 exact);
  `RawPluginData` against the mock's second provider; node-config
  property-string parse/render round-trips including multi-slot `acmedomain[n]`
  and the explicit-delete contract; digest-mismatch conflict.
- **Path pinning**: `TestACMEPathsReal` pins every literal path (the
  `TestCephPathsReal` pattern) — plugins, challenge-schema, directories, node
  config.
- **Coverage**: `just coverage` regenerated in the same PR; nine-or-ten
  gap→covered flips, no annotation edits; the fabrication guard is the proof the
  paths are real.
- **Integration (env-gated, live-only)**: `TestACMEDNSCloudflare` behind
  `PVE_TEST_ACME_DOMAIN` + `PVE_TEST_ACME_CF_TOKEN_ENV`: register a staging
  account (Let's Encrypt staging directory via `ListACMEDirectories`), create
  the cf plugin, set `acmedomain0` on one node, order, await the task, verify
  the served certificate's SAN, then revoke + clean up. Recorded with the
  extended redaction; the cassette's `data` and certificate-order responses
  scrubbed before commit. A `TestACMEDNSNamecheap` variant runs against the same
  domain after its nameservers switch to Namecheap DNS (OQ-6: one shared domain,
  sequential runs).
- The order/renew/revoke task-vs-sync REST-with-caveat from Phase 6 gets
  resolved by this live run and the caveat comments updated.

## Migration / Rollout Plan

1. One `minor` PR: interface + providers + plugin CRUD + discovery reads + node
   config + mockpve + recorder redaction + docs (`nodes` doc.go grows an ACME
   section; a runnable `Example` wiring plugin→domain→order against mockpve).
2. Live verification (Donald-run, needs a real domain in a Cloudflare zone + LE
   staging): the integration test above, cassettes leak-reviewed and committed,
   replay wired into `just test-replay`.
3. Namecheap live verification on the same domain after its nameservers switch
   to Namecheap DNS (OQ-6: one shared domain, sequential provider runs); until
   that run lands, Namecheap is unit-verified only and its doc comment says so
   (the honesty rule).

   **Amended 2026-08-20: step 3 was descoped** (IMPL-0007 Phase 4 task 4). The
   nameserver switch would take the now-verified Cloudflare path offline for
   hours to prove a second provider, and what it was evidence for — that
   `ACMEPluginData` is provider-generic — is carried by the interface shape and
   by `RawPluginData` reaching all ~160 providers with no Go code. Namecheap
   stays unit-verified with its caveat in place, which is exactly where step 3
   said it would sit until a run lands. No run is scheduled.

## Open Questions

1. **Where does the ACME surface live?** **Decision (2026-08-17): a.**
   - **a (recommended):** `proxmox/nodes`, beside the ACME account ops in
     `certificates.go` (new files `acmeplugins.go`, `nodeconfig.go`). The whole
     ACME story stays in one package — accounts, plugins, node wiring, ordering
     — matching where Phase 6 already put accounts and orders;
     cluster-scoped-ACME-in-nodes has that precedent (the account paths are
     `/cluster/acme/…` today). No new accessor, no consumer relearning.
   - b: New `proxmox/acme` package with its own `ACME()` accessor. Cleaner
     scoping (the plugin ops ARE cluster-scoped), but it splits ACME across two
     packages unless the account ops move too — and moving them is a pre-v1
     public-surface break for tidiness with no behaviour win.
   - c: `proxmox/cluster`. Matches the URL prefix but nothing else — cluster is
     options/status/resources today, and the node-config half wouldn't belong
     there either.

2. **Shape of the provider-generic mechanism?** **Decision (2026-08-17): a.**
   - **a (recommended):** The two-method `ACMEPluginData` interface + typed
     provider structs + `RawPluginData`. Smallest possible seam; adding a
     provider is one struct with two trivial methods; the raw type keeps all 160
     providers reachable day one; compile-time safety for the typed pair.
   - b: No interface — `ACMEPluginSpec.API string` + `Data map[string]string`,
     with optional helper constructors
     (`CloudflareData(token, …) map[string]string`). One less concept, but
     nothing ties `api` to its field names, and typos in either are runtime-only
     failures; "generic" collapses to "stringly".
   - c: Code-generate all 160 typed structs from a captured challenge-schema
     response. Maximal typing, but the schema is a runtime payload we'd be
     vendoring, the generated surface is enormous relative to demand, and
     unverified providers would look as trustworthy as verified ones — against
     the honesty rule.

3. **Ship Namecheap typed in the first PR, or Cloudflare only?** **Decision
   (2026-08-17): a.**
   - **a (recommended):** Both. Two implementations are the minimum proof the
     interface generalizes, Namecheap's all-required/IP-allowlisted shape is
     usefully un-Cloudflare-like, and you named it as the second provider you'd
     test. Its doc comment carries "unit-verified only" until a live run.
   - b: Cloudflare only; add Namecheap when live credentials exist. Smaller PR,
     but the interface ships proven against one implementation, and the
     second-provider ergonomics (the thing this design exists for) go
     unexercised.

4. **Client-side validation against the challenge schema?** **Decision
   (2026-08-17): a.**
   - **a (recommended):** None. The SDK validates only its own contract
     (non-empty `ID`, `Data` present when `Type` is dns) and lets PVE validate
     provider fields — PVE owns that schema and the SDK duplicating it would
     drift. `GetChallengeSchema` is exposed so consumers who want pre-flight
     validation can do it themselves.
   - b: `CreateACMEPlugin` fetches the challenge schema and validates `Data()`
     keys before POSTing. Friendlier errors, but adds a hidden request to every
     create, a cache-staleness question, and a failure mode where the SDK
     rejects what the server would accept.

5. **How much of node config gets typed?** **Decision (2026-08-17): a.**
   - **a (recommended):** Generic lossless `GetNodeConfig`/`SetNodeConfig` with
     only the ACME keys typed (plus `Digest`); everything else through `Extra`.
     Covers both endpoints completely (two gap rows close, and
     `wakeonlan`/`description` become writable via `Extra` without another PR),
     while typing only what this design needs.
   - b: Narrow ACME-only ops (`GetNodeACME`/`SetNodeACME`) that read/write just
     the acme keys via the `property` filter and targeted PUTs. Smaller surface,
     but `GET /nodes/{}/config` stays a partial gap, a second node-config need
     later means new ops beside these, and it breaks the package's lossless-read
     convention.

6. **Live verification needs a real domain + Cloudflare zone — what is the
   plan?** **Decision (2026-08-17): a, amended — Donald provides ONE domain
   usable for both the Cloudflare and Namecheap live runs.** Operational
   consequence: a zone resolves through one DNS host at a time, so the two
   provider verifications are **sequential** — the domain's nameservers point at
   Cloudflare for the `cf` run, then switch to Namecheap DNS (with propagation
   wait) for the `namecheap` run. The integration tests already take the domain
   via `PVE_TEST_ACME_DOMAIN`, so nothing in the harness changes; only the run
   order does.
   - **a (recommended):** A dedicated subdomain (e.g. `lab.<your-domain>`) in
     your existing Cloudflare account, a scoped API token (Zone.DNS edit on that
     zone only), Let's Encrypt **staging** as the directory, run against r740a
     or a pvelab node. Cassettes recorded with the extended redaction; hostnames
     in cassettes rewritten to `pve.example` by the existing topology scrub.
   - b: Mock-only for now; land the surface REST-with-caveat (shapes from
     apidoc, no live confirmation) and live-verify when a domain is available.
     Honest but leaves the flagship flow of this design unproven, and the Phase
     6 order/renew caveat unresolved.
   - c: A throwaway domain bought for the lab (~$10/yr) with its own Cloudflare
     zone, so nothing production-adjacent ever holds lab credentials.

## References

- `docs/COVERAGE.md` — the `gap` rows this design closes (`/cluster/acme/*`
  plugins + discovery, `/nodes/{}/config`).
- `cmd/pve-schemadiff/testdata/apidoc-9.2.js.gz` — wire-shape source for every
  endpoint above (r740a capture, 2026-07-19).
- Phase 6 ACME accounts + certificate ordering: `proxmox/nodes/certificates.go`
  (the patterns this design extends).
- DESIGN-0005 / IMPL-0006 — the coverage tracker whose fabrication guard
  verifies this PR's mock routes.
- acme.sh dnsapi documentation — source of the `CF_*` / `NAMECHEAP_*` variable
  names (to be confirmed live via `GetChallengeSchema`).
- ADR-0002 — 9.x-only; every endpoint here is 9.0 baseline, no gates.
