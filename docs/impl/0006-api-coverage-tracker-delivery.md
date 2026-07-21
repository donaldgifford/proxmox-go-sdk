---
id: IMPL-0006
title: "API coverage tracker delivery"
status: Draft
author: Donald Gifford
created: 2026-07-21
---

<!-- markdownlint-disable-file MD025 MD041 -->

# IMPL 0006: API coverage tracker delivery

**Status:** Draft **Author:** Donald Gifford **Date:** 2026-07-21

<!--toc:start-->

- [Objective](#objective)
- [Scope](#scope)
  - [In Scope](#in-scope)
  - [Out of Scope](#out-of-scope)
- [Ground facts](#ground-facts)
- [Implementation Phases](#implementation-phases)
  - [Phase 1: Route enumeration in mockpve](#phase-1-route-enumeration-in-mockpve)
    - [Tasks](#tasks)
    - [Success Criteria](#success-criteria)
  - [Phase 2: The coverage mode on pve-schemadiff](#phase-2-the-coverage-mode-on-pve-schemadiff)
    - [Tasks](#tasks-1)
    - [Success Criteria](#success-criteria-1)
  - [Phase 3: Annotations, first report, CI teeth](#phase-3-annotations-first-report-ci-teeth)
    - [Tasks](#tasks-2)
    - [Success Criteria](#success-criteria-2)
- [Open Questions](#open-questions)
- [References](#references)
<!--toc:end-->

## Objective

Implement DESIGN-0005: a **generated** `docs/COVERAGE.md` measuring the SDK
against the real PVE 9.2 API (per service, with totals), plus the two CI checks
that give it teeth — a **drift check** (the committed report must match a
regeneration) and the **fabrication guard** (no mockpve route may reference an
endpoint that does not exist on real PVE — the check that makes the fabrics/DLB
failure mode structurally impossible). One `minor` PR.

**Implements:** DESIGN-0005 (OQ decisions 2026-07-21: all a — `-coverage` mode
on `cmd/pve-schemadiff`, mockpve's route table as the numerator, full
per-endpoint tables, first report after the DESIGN-0003/0004 remediations merge,
`minor` label).

## Scope

### In Scope

- `mockpve.Server.handle()` registration helper + the exported
  `Routes() []string` (the numerator).
- A `-coverage` mode on `cmd/pve-schemadiff` (normalization, service mapping,
  annotations, report rendering, the two checks).
- The hand-curated annotations file seeded with today's true exceptions.
- The first committed `docs/COVERAGE.md`, the `just coverage` recipe, and the CI
  step next to `just schemadiff`.

### Out of Scope

- Per-consumer need tracking, SDK-method-level mapping, response-shape
  conformance, multi-minor coverage matrices (all DESIGN-0005 non-goals; the
  last waits on a second baseline, IMPL-0003 OQ-3).
- Closing any coverage gap the report exposes — the report is the input to the
  group-5 triage, not a license to start it.

## Ground facts

Checked against the tree 2026-07-21 (they update the design's estimates):

- mockpve has **185** route registrations today on `main` (the design's "~167"
  is stale); the DESIGN-0003 remediation adds 13 (5 fabric-node + 8 node-scoped
  status) and DESIGN-0004 removes the 2 `lbalancer` routes — expect **~196**
  when this lands.
- The denominator is `cmd/pve-schemadiff/testdata/baseline.json` — 675 endpoints
  (IMPL-0003).
- The module already depends on `go.yaml.in/yaml/v4` (pvelab config), so the
  annotations loader adds no new dependency.
- `cmd/pve-schemadiff` may import `proxmox/mockpve` (public package, same module
  — the internal-package gotcha does not apply).
- After the remediations, nearly every SDK `ErrUnsupported` stub targets an
  endpoint real PVE does **not** serve (DLB, RAIDZ expansion, volume-chain
  snapshots, OTel, PBS verify) — those are in neither the baseline nor the mock,
  so they drop out of the coverage arithmetic entirely and the `stubs:`
  annotation section will start empty or near-empty. The real annotation load is
  `side_channel` and the untriaged gap families (OQ-4).

## Implementation Phases

Each phase builds on the previous one. A phase is complete when all its tasks
are checked off and its success criteria are met.

---

### Phase 1: Route enumeration in mockpve

The numerator, as an additive public API.

#### Tasks

- [ ] 1. Add the `handle(pattern string, h http.HandlerFunc)` helper on
     `*Server` (records the pattern, then `s.mux.HandleFunc`) and the exported
     `Routes() []string` returning the recorded patterns per the OQ-1 decision,
     with a doc comment stating the format contract (Go 1.22 ServeMux patterns,
     exactly as registered).
- [ ] 2. Mechanically switch every `s.mux.HandleFunc` call site in
     `proxmox/mockpve` (~185 across the per-service files) to `s.handle`; a grep
     guard in the tests asserts no direct `mux.HandleFunc` registrations remain
     outside `handle` itself.
- [ ] 3. Unit tests: `Routes()` length equals the registration count and
     contains known samples from several services; the route list is stable
     across two `New()` instances.

#### Success Criteria

- `go build ./...`, `just lint`, `just test` (race) green.
- `mockpve.New().Routes()` returns every registered pattern; no registration
  bypasses the helper.

---

### Phase 2: The coverage mode on pve-schemadiff

The arithmetic, the report, and the checks — all unit-tested in an importable
package (the `schema`-package precedent).

#### Tasks

- [ ] 1. New importable package `cmd/pve-schemadiff/coverage`: normalization
     (strip the `/api2/json` prefix, rewrite every `{name}` wildcard to `{}` on
     **both** sides, split `"METHOD /path"` patterns into `(method, path)`
     pairs) — table-driven tests including the placeholder-name mismatches
     (`{vmid}` vs `{id}`, `{fabric}` vs `{fabric_id}`).
- [ ] 2. Service mapping: the static prefix → service table (`/cluster/ha` →
     `ha`, `/nodes/{}/qemu` → `qemu`, …); anything unmapped lands in an
     "unassigned" section so new API families surface loudly. A test runs the
     mapper over the real committed baseline and pins the current unassigned set
     (the known gap families), so a future PVE minor adding a family breaks the
     test loudly instead of silently swelling "unassigned".
- [ ] 3. Annotations loader (`go.yaml.in/yaml/v4`) for the four sections —
     `stubs` (real endpoints deliberately stubbed, with reason), `side_channel`,
     `out_of_scope` (prefix + deciding doc), and `allow_unmatched_routes` (the
     fabrication-guard escape hatch; empty is the goal). Unknown YAML keys are
     an error (a typoed section must not silently annotate nothing).
- [ ] 4. Report renderer: per-service tables (method, path, state = covered /
     stub-with-reason / gap), header with totals, per-service percentages, and
     the baseline's PVE version + provenance; golden-file test against a small
     fixture baseline + fixture routes + fixture annotations.
- [ ] 5. The two checks as tool behavior: `-coverage -out docs/COVERAGE.md`
     writes the report; `-coverage -check` regenerates in memory, diffs against
     the committed file, and exits non-zero on drift ("regenerate and commit") —
     and in **both** modes any normalized mockpve route absent from the baseline
     and not allowlisted fails the run, naming the route. Unit test: a fixture
     route set containing a fabricated route (the old flat
     `/cluster/sdn/fabrics` shape) must be named in the error — the guard
     validated against the exact drift it exists to prevent.
- [ ] 6. Wire `-coverage` into `main.go` (flags: `-coverage`, `-annotations`,
     `-out`, `-check`; the existing `-apidoc`/`-baseline` flags are reused); the
     tool imports `proxmox/mockpve`, constructs a `Server`, and calls `Routes()`
     — no codegen, no source parsing.

#### Success Criteria

- Golden-file test green; the fabrication-guard test names the fabricated
  fixture route; `just lint` + `just test` green.
- `go run ./cmd/pve-schemadiff -coverage …` produces a complete report from the
  real baseline + real mock routes locally.

---

### Phase 3: Annotations, first report, CI teeth

Seed the exceptions honestly, commit the first clean report, and turn on the
checks.

#### Tasks

- [ ] 1. Seed the annotations file (location per OQ-2) with today's true
     exceptions only: `side_channel` (snippet/backup upload via `proxmox/ssh`,
     custom node scripts via `Exec`), the untriaged gap families per the OQ-4
     decision, and a `stubs` audit (expected empty or near-empty — see Ground
     facts). `allow_unmatched_routes` starts empty.
- [ ] 2. Run the fabrication guard against the real mock; triage every hit by
     **fixing the mock path** (the mock mirrors real PVE) rather than
     allowlisting — any surviving allowlist entry needs a written reason in the
     annotations file and a matching note in the PR.
- [ ] 3. Generate and commit the first `docs/COVERAGE.md`; sanity-review the
     totals (the ~196 covered routes against 675 endpoints — the number is the
     baseline for the group-5 triage conversation, not a target).
- [ ] 4. `just coverage` recipe (regenerate) + `just coverage-check` (or
     `-check` flag invocation) wired into the CI test-go job next to
     `just schemadiff`; confirm a deliberate local tamper (edit one line of
     `COVERAGE.md`; add one fake mock route) fails each check respectively.
- [ ] 5. Docs: DEVELOPMENT.md gains the regenerate-and-commit workflow (next to
     the schema-drift section it extends); CLAUDE.md's CI matrix mentions the
     coverage step; `mockpve` doc.go notes `Routes()`.
- [ ] 6. PR: `minor` label (`Routes()` is new public API on an importable
     package, DESIGN-0005 OQ-5a); changelog-final; merge → auto-release;
     DESIGN-0005 status → Implemented.

#### Success Criteria

- CI fails on a stale `COVERAGE.md` and on a fabricated mock route; both checks
  green on the real tree with an empty (or reasoned) allowlist.
- `docs/COVERAGE.md` is committed, generated-only, and current; DESIGN-0005 is
  Implemented.

## Open Questions

1. **Does `Routes()` return raw registered patterns or normalized ones?**
   - **a (recommended):** Raw — exactly the Go 1.22 ServeMux patterns as
     registered (`"GET /api2/json/nodes/{node}/qemu"`). The public API stays a
     dumb, honest enumeration; normalization is the coverage tool's concern and
     can evolve without touching `mockpve`'s contract. Consumers who want to
     introspect the mock get the real patterns.
   - b: Normalized (`/api2/json` stripped, wildcards rewritten to `{}`) — saves
     the tool a step but bakes a reporting-tool convention into a public testing
     API, and loses the placeholder names, which are useful to humans reading
     the list.

2. **Where does the annotations file live?**
   - **a (recommended):** `cmd/pve-schemadiff/coverage-annotations.yaml` —
     beside the tool and its testdata (baseline + apidoc), keeping all tracker
     inputs in one place and `docs/` free of hand-edited tracker inputs
     (`docs/COVERAGE.md` stays the only tracker artifact there, and it is
     generated-only).
   - b: `docs/coverage-annotations.yaml` — input next to output; discoverable,
     but it puts a hand-edited file in a directory whose tracker content is
     otherwise machine-written.

3. **When does this start, relative to IMPL-0004/0005?**
   - **a (recommended):** After both remediation PRs **merge** (IMPL-0004 Phase
     2 / IMPL-0005 Phase 2) — that satisfies DESIGN-0005 OQ-4's "first report is
     clean" requirement — running in parallel with their Phase-3
     live-verification work, which this tracker does not touch (no cassettes, no
     lab). The Phase-3 cassette PRs land route-neutral changes, so no regen
     conflict is expected.
   - b: After IMPL-0004/0005 close out entirely (live runs + cassettes +
     ledger). Strictly serial and simplest to reason about, but it idles the
     tracker on lab scheduling it has no dependency on.

4. **How do the untriaged gap families (notifications, mappings, pools, jobs,
   bulk-action, …) appear in the first report?**
   - **a (recommended):** As **gaps** — the honest state; `out_of_scope` is
     reserved for decided non-goals with a deciding doc (pegaprox-go-side
     orchestration, the frontend). This amends DESIGN-0005's illustrative
     annotation example, which parked notifications under `out_of_scope` with
     reason "not yet triaged" — a not-yet-triaged family is precisely what the
     gap count exists to keep visible pressure on, and the group-5 triage then
     moves each family to covered / out-of-scope with a real deciding doc.
   - b: `out_of_scope` with a "not yet triaged (INV-0004 F8)" reason, per the
     design's example — a tidier headline percentage, but it hides exactly the
     debt the tracker was built to expose, behind a label that claims a decision
     that has not been made.

## References

- DESIGN-0005 — the design this delivers (OQs decided 2026-07-21: all a)
- IMPL-0003 — committed the 675-endpoint baseline (the denominator)
- IMPL-0004 / IMPL-0005 — the remediations that must merge first (DESIGN-0005
  OQ-4; see OQ-3)
- INV-0004 — Finding 8 (gap families) and the fabrics/DLB drift this makes
  structurally impossible
- `cmd/pve-schemadiff` — the tool gaining the `-coverage` mode (IMPL-0001 OQ-7
  heritage)
