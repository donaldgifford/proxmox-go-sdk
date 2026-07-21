---
id: DESIGN-0005
title: "API coverage tracker with CI drift and fabrication guards"
status: Draft
author: Donald Gifford
created: 2026-07-19
---

<!-- markdownlint-disable-file MD025 MD041 -->

# DESIGN 0005: API coverage tracker with CI drift and fabrication guards

**Status:** Draft **Author:** Donald Gifford **Date:** 2026-07-19

<!--toc:start-->

- [Overview](#overview)
- [Goals and Non-Goals](#goals-and-non-goals)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
- [Background](#background)
- [Detailed Design](#detailed-design)
  - [Numerator: mockpve enumerates its routes](#numerator-mockpve-enumerates-its-routes)
  - [Denominator: the committed real baseline](#denominator-the-committed-real-baseline)
  - [Annotations: the exceptions file](#annotations-the-exceptions-file)
  - [The generator and COVERAGE.md](#the-generator-and-coveragemd)
  - [The CI checks](#the-ci-checks)
- [API / Interface Changes](#api--interface-changes)
- [Data Model](#data-model)
- [Testing Strategy](#testing-strategy)
- [Migration / Rollout Plan](#migration--rollout-plan)
- [Open Questions](#open-questions)
- [References](#references)
<!--toc:end-->

## Overview

A **generated** coverage report measuring the SDK against the real PVE API —
objectively, per service, in CI — plus the guard that makes the fabrics/DLB
failure mode structurally impossible: **no mockpve route may reference an
endpoint that does not exist on real PVE**. Coverage is measured against the API
itself; what any consumer (e.g. `pegaprox-go`) needs stays what it should be — a
prioritization input, never the metric.

Hand-maintained tables are explicitly rejected: 675 endpoints across ~20
services would rot within weeks, and hand-maintenance of API knowledge is
exactly how the fabrics/DLB drift happened.

## Goals and Non-Goals

### Goals

- `docs/COVERAGE.md`: per-service tables (covered / deliberate stub with reason
  / gap) + totals, committed but **generated** — humans review diffs, never
  edit.
- CI fails when the committed doc is stale (drift check).
- CI fails when mockpve registers a route absent from the real baseline
  (fabrication guard).
- Near-zero maintenance: the only hand-curated input is a small exceptions file.

### Non-Goals

- Per-consumer need tracking or prioritization.
- Method-level mapping (SDK method → endpoint) — see OQ-2 for why endpoint-level
  is the right granularity.
- Response-**shape** conformance (that is the cassette/certification pipeline's
  job); this tracks surface, not payloads.
- Multi-minor coverage matrices — deferred until a second baseline exists
  (IMPL-0003 OQ-3).

## Background

Two machine-readable halves already exist. The **denominator**: IMPL-0003
commits the real 9.2 baseline — 675 `(method, path)` endpoints parsed from
`apidoc.js` by `pve-schemadiff`. The **numerator**: every REST op the SDK ships
is unit-tested against a registered mockpve route — **167 unique registrations
today** — and the repo's standing discipline ("mockpve mirrors real PVE",
enforced through cassette certification) makes the mock's route table a
trustworthy map of the covered surface. What is missing is only the arithmetic,
the report, and the teeth.

## Detailed Design

### Numerator: mockpve enumerates its routes

Route registration in `mockpve` goes through a tiny helper that records each
pattern as it registers it:

```go
func (s *Server) handle(pattern string, h http.HandlerFunc) {
    s.routes = append(s.routes, pattern) // "GET /api2/json/nodes/{node}/qemu"
    s.mux.HandleFunc(pattern, h)
}
```

All `registerXRoutes` functions switch from `s.mux.HandleFunc` to `s.handle`
(mechanical, ~167 call sites), and `Server` gains `Routes() []string`.
Normalization for comparison: strip `/api2/json`, rewrite every `{name}`
wildcard to `{}` on **both** sides (mockpve and apidoc placeholder names differ
occasionally: `{vmid}` vs `{id}` vs `{name}`).

### Denominator: the committed real baseline

`cmd/pve-schemadiff/testdata/baseline.json` (IMPL-0003). The tracker consumes it
as-is — one source of truth for "what real PVE serves".

### Annotations: the exceptions file

One small hand-curated YAML holding only the **exceptions** — everything else is
derived:

```yaml
stubs: # ship as documented ErrUnsupported; counted separately, with reason
  - path: "GET /cluster/ha/lbalancer"
    reason: "no PVE REST endpoint (INV-0004 F4); use CRS options"
side_channel: # covered via proxmox/ssh, not REST
  - "snippet/backup upload -> /var/lib/vz (no REST upload for these)"
out_of_scope: # deliberate non-goals, with the deciding doc
  - prefix: "/cluster/notifications"
    reason: "gap-family, not yet triaged (INV-0004 F8)"
allow_unmatched_routes: [] # fabrication-guard escape hatch; empty is the goal
```

### The generator and COVERAGE.md

A `-coverage` mode on `pve-schemadiff` (per OQ-1) reads baseline + routes

- annotations and emits `docs/COVERAGE.md`:

* One table per service, mapped from path prefixes (`/cluster/ha` → `ha`,
  `/nodes/{}/qemu` → `qemu`, …) via a small static table in the tool; unmapped
  prefixes land in an "unassigned" section so new API families surface loudly.
* Row per endpoint: method, path, state — covered (mockpve route exists) / stub
  (annotated, reason shown) / gap.
* Header: totals and per-service percentages, plus the baseline's PVE version
  and provenance.

Getting the numerator out of the library and into the tool: the tool imports
`proxmox/mockpve`, constructs a `Server`, and calls `Routes()` — no codegen, no
source parsing.

### The CI checks

`just coverage` (wired into the test-go job next to `just schemadiff`):

1. **Drift**: regenerate to a temp file, diff against the committed
   `docs/COVERAGE.md`, non-zero on mismatch ("regenerate and commit").
2. **Fabrication guard**: any normalized mockpve route not present in the
   baseline and not in `allow_unmatched_routes` fails the build, naming the
   route. This is the check that would have caught the fabrics paths and the DLB
   route the day they were written.

## API / Interface Changes

- `mockpve.Server` gains the exported `Routes() []string` (additive).
- No SDK service package changes. New justfile recipe + CI step.

## Data Model

| Artifact                    | Kind                        | Maintained by               |
| --------------------------- | --------------------------- | --------------------------- |
| `testdata/baseline.json`    | real endpoint set           | IMPL-0003 regeneration flow |
| `mockpve.Routes()`          | covered-surface enumeration | falls out of registration   |
| `coverage-annotations.yaml` | exceptions only             | hand, small                 |
| `docs/COVERAGE.md`          | the report                  | generated                   |

## Testing Strategy

- Unit: normalization (placeholder rewriting, prefix stripping) — table-driven;
  service mapping (every baseline prefix maps or lands in "unassigned");
  generator golden-file test against a tiny fixture baseline + fixture routes.
- The fabrication guard is validated by construction: point it at today's mock
  with the DESIGN-0003/0004 fixes absent and it must name the flat fabrics +
  lbalancer routes (a good pre-land smoke test of the tool itself).
- CI: the two checks above run on every push.

## Migration / Rollout Plan

1. Land after IMPL-0003 (needs the real baseline) and after DESIGN-0003/0004
   (per OQ-4 — so the first committed report is clean of known-wrong routes
   rather than institutionalizing them).
2. First PR ships: `handle` refactor + `Routes()`, the tool mode, annotations
   seeded with today's true exceptions, the first generated `docs/COVERAGE.md`,
   the justfile recipe + CI step. Label per OQ-5.
3. Thereafter the doc updates whenever a PR adds/removes mockpve routes — the
   drift check forces the regen into the same PR, so coverage history is just
   git log on `docs/COVERAGE.md`.

## Open Questions

1. **Where does the tool live?**
   - **a (recommended):** A `-coverage` mode on `cmd/pve-schemadiff` — it
     already owns apidoc parsing, the baseline, and the CI slot; both features
     are "compare a surface against the baseline". One tool, one `schema`
     package, one testdata dir.
   - b: A new `cmd/pve-coverage` importing the `schema` package. Cleaner
     single-purpose binaries, one more cmd to version/wire/document.

2. **What is the coverage numerator?**
   - **a (recommended):** mockpve's route table. Zero new bookkeeping, kept
     honest by the existing every-op-tests-against-mockpve discipline plus
     cassette certification; drifts only if that discipline drifts (which the
     doc's diff would itself expose).
   - b: A hand-maintained SDK-method → endpoint mapping file. More precise
     (method-level) but rot-prone — reintroduces exactly the hand-maintained API
     knowledge this design rejects.
   - c: Parse the service packages' source for path expressions. No bookkeeping
     and no mock dependency, but a fragile mini-parser over `fmt.Sprintf`-built
     paths.

3. **Report format?**
   - **a (recommended):** Full per-endpoint tables per service (greppable,
     diff-reviewable, self-explanatory in the repo).
   - b: Summary percentages only (short but hides _which_ endpoints gap).
   - c: Summary in-repo + full report as a CI artifact (splits the truth across
     two places).

4. **When does the first report land?**
   - **a (recommended):** After DESIGN-0003/0004 merge — the guard's first run
     is then clean, and the report never memorializes routes we already know are
     wrong.
   - b: Before them — baselines the current (wrong) state and shows the fixes as
     diffs; requires seeding `allow_unmatched_routes` with the known-bad routes
     just to get CI green, which is backwards.

5. **Release label for the tracker PR?**
   - **a (recommended):** `minor` — `mockpve.Routes()` is new public API on an
     importable package.
   - b: `patch` — treat it as tooling; defensible only if `Routes()` stays
     unexported, which would force the tool into the `mockpve` package or an
     internal seam.

## References

- INV-0004 — Finding 8 (gap families) and the 675-endpoint extraction; the
  fabrics/DLB drift this design makes structurally impossible
- IMPL-0003 — commits the baseline this tool consumes
- DESIGN-0003 / DESIGN-0004 — the remediation that should land first (OQ-4)
- `cmd/pve-schemadiff` — existing parse/diff tool and CI slot
