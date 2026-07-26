---
id: IMPL-0006
title: "API coverage tracker delivery"
status: Completed
author: Donald Gifford
created: 2026-07-21
---

<!-- markdownlint-disable-file MD025 MD041 -->

# IMPL 0006: API coverage tracker delivery

**Status:** Completed **Author:** Donald Gifford **Date:** 2026-07-21 (OQs
decided 2026-07-21: all a; delivered 2026-07-25)

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

- [x] 1. Add the `handle(pattern string, h http.HandlerFunc)` helper on
     `*Server` (records the pattern, then `s.mux.HandleFunc`) and the exported
     `Routes() []string` returning the recorded patterns per the OQ-1 decision,
     with a doc comment stating the format contract (Go 1.22 ServeMux patterns,
     exactly as registered). _(Done 2026-07-24: `routes []string` field +
     `handle` + `Routes()` (returns a copy). `RegisterHandler` records its
     pattern too, so `Routes()` is an honest enumeration of everything
     registered, not just the built-ins — documented on both methods.)_
- [x] 2. Mechanically switch every `s.mux.HandleFunc` call site in
     `proxmox/mockpve` (~185 across the per-service files) to `s.handle`; a grep
     guard in the tests asserts no direct `mux.HandleFunc` registrations remain
     outside `handle` itself. _(Done 2026-07-24: **201** registrations switched
     across the 14 per-service files + `registerRoutes` — above the ledger's
     ~196 estimate. `TestNoDirectMuxRegistrations` reads the package's own
     non-test sources and permits exactly one direct call, the one inside
     `handle`; the needle is assembled at runtime so the guard's own source is
     not a hit.)_
- [x] 3. Unit tests: `Routes()` length equals the registration count and
     contains known samples from several services; the route list is stable
     across two `New()` instances. _(Done 2026-07-24, with one deliberate
     deviation: "length equals the registration count" is asserted at RUNTIME,
     not by counting `s.handle` call sites in source. Source-counting is wrong
     here — `registerFirewallScope` is invoked three times, so 201 call sites
     produce **231** routes. `TestRoutesAreAllServed` instead dials every
     recorded pattern credential-free and requires a handler response (401 from
     checkAuth, or 2xx/4xx from the public ones) rather than the mux's 404,
     which verifies the stronger property the numerator actually rests on: every
     reported route is genuinely served. It also pins uniqueness, the documented
     `"METHOD /api2/json/..."` form, and a ~200 floor. Plus
     `TestRoutesContainsKnownPatterns` (10 samples spanning foundation, qemu,
     lxc, storage, ha, sdn, nodes, cluster, access),
     `TestRoutesStableAcrossInstances` (identical order across two `New()`s +
     the returned slice is a copy), and `TestRoutesIncludesRegisteredHandler`.)_

#### Success Criteria

- `go build ./...`, `just lint`, `just test` (race) green.
- `mockpve.New().Routes()` returns every registered pattern; no registration
  bypasses the helper.

---

### Phase 2: The coverage mode on pve-schemadiff

The arithmetic, the report, and the checks — all unit-tested in an importable
package (the `schema`-package precedent).

#### Tasks

- [x] 1. New importable package `cmd/pve-schemadiff/coverage`: normalization
     (strip the `/api2/json` prefix, rewrite every `{name}` wildcard to `{}` on
     **both** sides, split `"METHOD /path"` patterns into `(method, path)`
     pairs) — table-driven tests including the placeholder-name mismatches
     (`{vmid}` vs `{id}`, `{fabric}` vs `{fabric_id}`). _(Done 2026-07-24:
     `Key`/`NormalizePath`/`ParsePattern`/`ParsePatterns`/`EndpointKey` in
     `normalize.go`; 7 tests including the three real placeholder mismatches, a
     no-collision check, and malformed-pattern rejection (a method-less pattern
     is an error, never silently dropped — dropping it would inflate coverage).
     Verified against the real data: 205 of 231 mock routes match the baseline,
     and the 26 that do not are the pre-triage findings recorded under Phase 3
     task 2 below — i.e. the guard works.)_
- [x] 2. Service mapping: the static prefix → service table (`/cluster/ha` →
     `ha`, `/nodes/{}/qemu` → `qemu`, …); anything unmapped lands in an
     "unassigned" section so new API families surface loudly. A test runs the
     mapper over the real committed baseline and pins the current unassigned set
     (the known gap families), so a future PVE minor adding a family breaks the
     test loudly instead of silently swelling "unassigned". _(Done 2026-07-25:
     `coverage/service.go` — 66 rules over 15 services, longest-prefix +
     whole-segment matching, `Services()` sorted with `unassigned` last. Two
     decisions worth recording. **No catch-all `/cluster` or `/nodes` rule** —
     measured both ways, and with catch-alls `unassigned` collapses from 67
     endpoints to 7 (`/pools*`) and the "new families surface loudly" property
     dies, since a PVE minor adding a family would silently join an existing
     service's table. Both bare paths are `exact: true` rules claiming only
     their own index endpoint, so every family under them is enumerated
     explicitly (44 `/nodes/{}/…` rules); `TestExactRulesDoNotClaimSubtrees`
     fails if either is ever widened. **A family is mapped on domain ownership,
     not on coverage** — `/access/tfa` maps to `access` with zero covered
     endpoints so it reads as an access gap rather than an orphan, which leaves
     `unassigned` meaning exactly "no service claims this". Current split:
     **205/675 (30.4%)** covered, with `unassigned` = 67 endpoints in 5
     untriaged families — `/cluster/notifications` (31), `/cluster/mapping`
     (16), `/cluster/jobs` (7), `/pools` (7), `/cluster/bulk-action` (6) — kept
     visible as gaps per OQ-4a. Four invariants over the real baseline replace a
     golden file: the unassigned set is pinned per family with counts; no
     covered endpoint may land in `unassigned` (implementing an unmapped family
     fails until it is mapped); every rule must match ≥1 baseline endpoint (typo
     guard); `Services()` must cover every rule. Verified by tampering —
     catch-all, dropped rule, and typo'd prefix each fail with the offending
     path named.)_
- [x] 3. Annotations loader (`go.yaml.in/yaml/v4`) for the four sections —
     `stubs` (real endpoints deliberately stubbed, with reason), `side_channel`,
     `out_of_scope` (prefix + deciding doc), and `allow_unmatched_routes` (the
     fabrication-guard escape hatch; empty is the goal). Unknown YAML keys are
     an error (a typoed section must not silently annotate nothing). _(Done
     2026-07-25: `coverage/annotations.go` — `Annotations` + `LoadAnnotations`/
     `ParseAnnotations` (strict `KnownFields(true)`), `Validate`, and the three
     lookups `StubReason`/`AllowsRoute`/`OutOfScopeFor` (segment-aware prefix
     match, reusing `serviceRule.matches`). **Two amendments to the design's
     illustrative YAML**, both recorded here: (1) a fifth **`baseline:`
     section** (`pve_version`/`source`/`captured`, all required) — the report
     header must state which PVE version was measured and `baseline.json` is a
     bare endpoint array carrying no metadata, so the provenance has nowhere
     else to live; (2) `allow_unmatched_routes` entries are **typed
     `{route, reason}`**, not the design example's bare strings, because this
     ledger's task 5 requires a written reason for any surviving allowlist entry
     — the schema now enforces what the process demands. `out_of_scope` likewise
     requires `doc:`, so an untriaged family cannot be relabelled a decided
     non-goal (OQ-4a) without naming the deciding document. Paths are normalized
     on load (`/api2/json` stripped, placeholders erased, method upper-cased),
     so an entry written in PVE's own spelling still matches instead of silently
     annotating nothing. An empty file is `ErrEmptyAnnotations`, not an empty
     exception set — a truncated file must not strip the report's provenance and
     demote every stub to a gap. `Validate` aggregates with `errors.Join` so a
     hand-edited file reports every problem in one pass. 8 tests: normalization,
     unknown-key rejection, empty-file, a 10-case validation table (missing
     fields, unrooted prefix, unparseable path, duplicates in all three keyed
     sections), multi-error aggregation, the lookups (including
     method-sensitivity and the `/cluster/notificationsX` non-match), and disk
     loading. Baseline cross-checks — a stub or allowlist entry naming an
     endpoint the baseline does not hold — are deliberately NOT here: they need
     the baseline, so they land with the checks in task 5.)_
- [x] 4. Report renderer: per-service tables (method, path, state = covered /
     stub-with-reason / gap), header with totals, per-service percentages, and
     the baseline's PVE version + provenance; golden-file test against a small
     fixture baseline + fixture routes + fixture annotations. _(Done 2026-07-25,
     split in two: `report.go` computes (`Build` → `Report` with `Services`,
     `Totals`, `Findings`) and `render.go` formats (`Report.Markdown`). Four
     states, not three — `out of scope` joins covered/stub/gap, since an
     annotated non-goal is neither implemented nor debt. Precedence is
     documented and tested: **covered beats every annotation** (a route that
     exists is a fact; an annotation claiming otherwise is stale), then stub
     beats out-of-scope (per-endpoint beats family-wide). `Build` errors only
     when the arithmetic would be meaningless — a malformed route pattern, or a
     baseline that **collides under normalization** (two endpoints reducing to
     one `{}`-shape, which would mean placeholder erasure is lossy; measured 0
     collisions on the real baseline, and the guard fails loudly if a future
     minor introduces one). Everything else is a `Findings` field so the caller
     picks what is fatal: `UnmatchedRoutes` (the fabrication guard's offenders),
     `AllowedRoutes` (rendered in the report, so exemptions stay uncomfortable),
     and three **stale-annotation** sets — a stub, allowlist entry, or
     out-of-scope prefix that no longer describes anything, so the exceptions
     file cannot rot into the hand-maintained API knowledge this tracker
     replaces. Rendering is byte-stable by construction (every section and row
     from a sorted slice, never a map walk) and pinned two ways: five renders
     compared byte-for-byte, plus a reversed-input render that must match — the
     drift check diffs bytes, so nondeterminism would fail CI at random. Table
     cells are unpadded on purpose (a padded table reflows every row when one
     long path is added, burying the real change in the diff). Header states the
     provenance and that **the percentage is not a target**. Golden fixture
     covers all four states, both extra sections, and the `{vmid}`-vs-`{id}`
     match; it is `testdata/report.golden` — **not** `.md`, because the repo's
     prettier and markdownlint globs cover every `.md` file and a reformatted
     golden would no longer be what the renderer emits (`docs/COVERAGE.md` needs
     the same treatment via ignore lists, Phase 3 task 4). Regenerate with
     `go test ./cmd/pve-schemadiff/coverage -run TestMarkdownGolden -args -update-golden`.
     Sanity-checked at real scale: 835 lines / 36 KB from the committed
     baseline + live mock routes, and `Findings.UnmatchedRoutes` came back as
     exactly the 26 pre-triaged routes below.)_
- [x] 5. The two checks as tool behavior: `-coverage -out docs/COVERAGE.md`
     writes the report; `-coverage -check` regenerates in memory, diffs against
     the committed file, and exits non-zero on drift ("regenerate and commit") —
     and in **both** modes any normalized mockpve route absent from the baseline
     and not allowlisted fails the run, naming the route. Unit test: a fixture
     route set containing a fabricated route (the old flat
     `/cluster/sdn/fabrics` shape) must be named in the error — the guard
     validated against the exact drift it exists to prevent. _(Done 2026-07-25:
     `coverage/checks.go` — `Report.Check()` returns the fatal findings and
     `CheckDrift(path, committed, regenerated)` returns a `*DriftError`; both
     are pure, so the flag plumbing in task 6 just maps them to exit codes. The
     guard's error **separates two failure modes**, which the ledger's own
     pre-triage shows are different bugs with different fixes: a path PVE does
     not serve at all (`no such path on PVE (fabricated path)` — ceph `/pools`,
     the flat fabrics shape) versus a path it serves under other verbs
     (`PVE serves only GET, POST at this path (wrong verb)` — the collapsed
     `status/{action}` wildcard). That needed a richer
     `Unmatched{Key, RealMethods}` finding and a path→verbs index of the
     baseline. **Stale annotations are fatal too** (`StaleAnnotationsError`) —
     not in the ledger's wording, but an exceptions file that silently
     accumulates dead entries is the hand-maintained API knowledge this tracker
     replaces, and the file is small enough that keeping it exact is free. Drift
     comparison is byte-exact and points at the first differing line. 6 tests,
     including the required one: the flat `/cluster/sdn/fabrics` **and**
     `/cluster/ha/lbalancer` — the two paths INV-0004 actually caught live — are
     both named by the error.)_
- [x] 6. Wire `-coverage` into `main.go` (flags: `-coverage`, `-annotations`,
     `-out`, `-check`; the existing `-apidoc`/`-baseline` flags are reused); the
     tool imports `proxmox/mockpve`, constructs a `Server`, and calls `Routes()`
     — no codegen, no source parsing. _(Done 2026-07-25:
     `run(apidoc, baseline, update)` became `run(opts *options)` dispatching to
     `runCoverage`/`runSchemaDiff`; `-apidoc` is no longer required in coverage
     mode, and `readBaseline` is now shared by both. Three decisions worth
     recording. (1) **`options.validate()` rejects nonsense combinations**
     rather than ignoring them — `-out` in schema-diff mode, `-update` in
     coverage mode, `-check` without `-out` — because a typo in a CI invocation
     that silently checks nothing is worse than one that fails. (2) **Exit codes
     are split**: exit 1 means a check said no (fabrication, stale annotations,
     drift), exit 2 means the tool could not run (unreadable file, malformed
     input); the `checkFailed` helper keeps that mapping in one place. (3)
     `options.routes` is the numerator and a **test seam** — `main` fills it
     from `mockpve.New().Routes()`, while the command tests supply fixture
     routes, since measuring the real mock's 231 routes against a 3-endpoint
     fixture baseline would report every one of them as fabricated. 6 new
     command tests (write → verify round-trip, drift detection on a hand-edited
     file, guard-blocks-the-write incl. asserting the file was never created,
     stdout mode, file errors, and the flag-combination table). Verified from
     the CLI against the real baseline + real mock: exit 1, all 26 offending
     routes named, and no file written.)_

#### Success Criteria

- Golden-file test green; the fabrication-guard test names the fabricated
  fixture route; `just lint` + `just test` green. **Met 2026-07-25** (coverage
  package at 97.6% statements).
- `go run ./cmd/pve-schemadiff -coverage …` produces a complete report from the
  real baseline + real mock routes locally. **Blocked on Phase 3 task 2, by
  design**: the run currently exits 1 with the 26 fabricated-route findings and
  writes nothing, which is the guard working. The render itself is verified at
  real scale (835 lines from the real baseline + real mock routes); it reaches
  disk once the mock is fixed.

---

### Phase 3: Annotations, first report, CI teeth

Seed the exceptions honestly, commit the first clean report, and turn on the
checks.

#### Tasks

- [x] 1. Seed the annotations file (location per OQ-2) with today's true
     exceptions only: `side_channel` (snippet/backup upload via `proxmox/ssh`,
     custom node scripts via `Exec`), the untriaged gap families per the OQ-4
     decision, and a `stubs` audit (expected empty or near-empty — see Ground
     facts). `allow_unmatched_routes` starts empty. _(Done 2026-07-25:
     `cmd/pve-schemadiff/coverage-annotations.yaml` per OQ-2a. The **stubs audit
     came back empty, and correctly so**: every `ErrUnsupported` op in the SDK —
     HA DLB, storage volume-chain snapshots, ZFS RAIDZ expansion, Ceph RBD
     mirroring, metrics OTel config, PBS `VerifyBackup`, console
     `VerifyVNCTicket` — refuses a path real PVE does not serve, so it is in
     neither the baseline nor the mock and drops out of the arithmetic entirely.
     The section's comment records that, so a future reader does not mistake
     "empty" for "unaudited"; an entry belongs there only when PVE serves the
     endpoint and the SDK still declines to drive it. `side_channel` has 5
     entries — the two the ledger names plus the three ssh-only capabilities
     those `ErrUnsupported` ops redirect to (RAIDZ expansion, RBD mirroring, raw
     storage-plugin snapshots), which would otherwise read as absent rather than
     deliberate. `out_of_scope` is **empty per OQ-4a** (the untriaged families
     are gaps; the comment says why parking them there would hide the debt) and
     `allow_unmatched_routes` is empty, which is the goal.)_
- [x] 2. Run the fabrication guard against the real mock; triage every hit by
     **fixing the mock path** (the mock mirrors real PVE) rather than
     allowlisting — any surviving allowlist entry needs a written reason in the
     annotations file and a matching note in the PR.

     **Pre-triage (2026-07-24, from the Phase-2 task-1 normalization probe):**
     26 of 231 mock routes do not match the baseline, in four groups — all real
     mock defects, none a tracker artifact:
     1. **Guest-firewall `{kind}` collapse (14 routes).** The mock registers
        `/nodes/{node}/{kind}/{vmid}/firewall/…` with `{kind}` as a wildcard;
        real PVE serves the literal `qemu`/`lxc` prefixes as separate paths. The
        mock therefore answers `…/foo/100/firewall/rules`, which real PVE 404s.
        Fix: call `registerFirewallScope` once per literal kind.
     2. **Node-scope firewall IPSet does not exist on real PVE (7 routes).**
        PVE's node firewall is only `/firewall`, `/log`, `/options`, `/rules`,
        `/rules/{pos}` — IPSet lives at cluster and guest scope only. The scope
        model's write-once route set over-registers it. This is an **SDK-surface
        finding too**: `firewall.NewNodeScope(...).ListIPSets` and friends would
        404 live.
     3. **Ceph pool path is singular (5 routes).** Real PVE:
        `/nodes/{node}/ceph/pool[/{name}]` (+ `/pool/{name}/status`); the mock
        (and the SDK's provisional `ceph/paths.go`) use `/ceph/pools`. Also
        `/nodes/{node}/ceph/config` does not exist. Phase 6 flagged these paths
        provisional — the guard proved them wrong.
     4. **`status/{action}` collapse (2 routes).** The mock registers one
        wildcard route for the power verbs; real PVE has literal
        `/status/{start,stop,shutdown,reboot,suspend,resume,reset}`. Fix:
        register the literals.

     Groups 2 and 3 change SDK behavior, so this task is not mock-only — scope
     note goes in the PR.

     **Triaged 2026-07-25 — all 26 fixed, none allowlisted; the guard now passes
     and `allow_unmatched_routes` stays empty.** Three commits, one per coherent
     change; the pre-triage group counts were slightly off (13/6/5/2, not
     14/7/5/2 — the mock registers 13 routes per firewall scope, of which 6 are
     IPSet).
     1. **Guest-firewall wildcard (13 routes)** — the mock now registers one
        firewall scope per LITERAL guest kind (`fwGuestKinds`), so 13 wildcard
        routes become 26 real ones. Mock-only.
     2. **Node-scope IPSet (6 routes)** — confirmed against the baseline: a
        node's firewall is `/firewall`, `/log`, `/options`, `/rules`,
        `/rules/{pos}` only. Mock routes removed **and** the SDK's seven
        node-scope IPSet ops now return a `pverr.ErrUnsupported`-wrapped error
        naming the scope, before any request (a bare 404 reads like a broken
        node, not an operation that cannot exist). The one-`Service`-plus-scope
        model is untouched; `TestIPSetCRUDPerScope` now iterates the two scopes
        that have IPSets and `TestIPSetsUnsupportedAtNodeScope` pins the
        refusal. **SDK behavior change.**
     3. **Ceph paths (5 routes)** — `/ceph/pools` → `/ceph/pool` (PVE spells the
        collection singular) and `/ceph/config` → `/ceph/cfg/raw` (the config is
        served three ways under `/cfg`; raw is the verbatim text
        `GetClusterConfig` returns). Every `ceph` pool op and `GetClusterConfig`
        would have 404'd live while passing against the mock. Now pinned by
        `TestCephPathsReal` (the `TestHAStatusPathsReal` pattern) and
        `ceph/doc.go` no longer calls the paths provisional — only the response
        shapes are. **SDK behavior change.**
     4. **`status/{action}` wildcard (2 routes)** — the verb is now bound at
        registration and the handler is a closure, so an unknown verb is a mux
        404 exactly as on PVE. Only the **6** verbs the SDK drives are
        registered: qemu's `/status/reset` is deliberately left out, since the
        SDK has no `Reset` op and a route for it would count an unreachable
        endpoint as covered. It shows in the report as a gap, which is what it
        is. Mock-only.

     Arithmetic after the fixes: 231 mock routes → **248**, all matching, so
     **248 of 675 endpoints (36.7%)** covered, up from the 205 (30.4%) measured
     before the triage — the 43 gained are endpoints the SDK always implemented
     but was testing against wrong paths.

- [x] 3. Generate and commit the first `docs/COVERAGE.md`; sanity-review the
     totals (the ~196 covered routes against 675 endpoints — the number is the
     baseline for the group-5 triage conversation, not a target). \_(Done
     2026-07-25: 845 lines, **248 of 675 (36.7%)**. Reviewed per service and
     against the ledger's estimate — the "~196" is superseded twice over: 231
     mock routes existed before the triage and 248 after it, and the covered
     count is per-endpoint, not per-route. Spot-checks look right (`version`
     1/1, `qemu` 42/104 with the unimplemented `/cluster/qemu` CPU-model family
     and the agent file/fsfreeze ops as gaps, `unassigned` 0/67). Two traps hit
     on the way in, both fixed: **`.gitignore`'s `coverage.*` matches
     `docs/COVERAGE.md`** on a case-insensitive filesystem (macOS) but not on
     CI's, so the report was committable on one machine and invisible on the
     other — un-ignored explicitly with a comment; and **`yamlfmt` refolds YAML
     block scalars and injects a `#magic___^_^___line` sentinel**, which then
     rendered into the report's side-channel list, so the annotations file uses
     single-line entries only. Also found `.yamlfmt` had been renamed to
     `.yamlfmt.yml` in the working tree, which silently disabled the whole
     yamlfmt config (its own comment says the name must be `.yamlfmt`);
     restored.)\_
- [x] 4. `just coverage` recipe (regenerate) + `just coverage-check` (or
     `-check` flag invocation) wired into the CI test-go job next to
     `just schemadiff`; confirm a deliberate local tamper (edit one line of
     `COVERAGE.md`; add one fake mock route) fails each check respectively.
     _(Done 2026-07-25: both recipes + the `Check API coverage report` step
     immediately after `Check API schema drift` in `.github/workflows/ci.yml`.
     `docs/COVERAGE.md` added to `.prettierignore` and the markdownlint ignores
     alongside `CHANGELOG.md` — the drift check compares bytes, so a formatter
     rewriting the report would fail CI on a clean tree. **Three deliberate
     tampers, each exit 1**: (1) editing one summary row → `is stale at line 43`
     with the committed and expected lines shown; (2) registering
     `GET /api2/json/cluster/ha/lbalancer` in the mock →
     `no such path on PVE (fabricated path)`, i.e. the guard catching the exact
     route INV-0004 F4 found live; (3) annotating that same path as a stub →
     `stale annotations (they match nothing; delete them)`. Clean tree exits
     0.)_
- [x] 5. Docs: DEVELOPMENT.md gains the regenerate-and-commit workflow (next to
     the schema-drift section it extends); CLAUDE.md's CI matrix mentions the
     coverage step; `mockpve` doc.go notes `Routes()`. _(Done 2026-07-25:
     DEVELOPMENT.md gained an "API coverage report" section right after
     schema-drift plus the two recipes in its task table; it documents the three
     ways `coverage-check` fails and **what to do about each**, since they have
     different fixes — regenerate, fix the mock and check the SDK, or delete the
     dead annotation. CLAUDE.md's CI matrix describes the coverage step in the
     same detail as the schema-drift one. `mockpve/doc.go` documented `Routes()`
     as the coverage numerator back in Phase 1 task 1, and `coverage/doc.go` was
     rewritten at Phase 2 closure.)_
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
   **Decision (2026-07-21): a.**
   - **a (recommended):** Raw — exactly the Go 1.22 ServeMux patterns as
     registered (`"GET /api2/json/nodes/{node}/qemu"`). The public API stays a
     dumb, honest enumeration; normalization is the coverage tool's concern and
     can evolve without touching `mockpve`'s contract. Consumers who want to
     introspect the mock get the real patterns.
   - b: Normalized (`/api2/json` stripped, wildcards rewritten to `{}`) — saves
     the tool a step but bakes a reporting-tool convention into a public testing
     API, and loses the placeholder names, which are useful to humans reading
     the list.

2. **Where does the annotations file live?** **Decision (2026-07-21): a.**
   - **a (recommended):** `cmd/pve-schemadiff/coverage-annotations.yaml` —
     beside the tool and its testdata (baseline + apidoc), keeping all tracker
     inputs in one place and `docs/` free of hand-edited tracker inputs
     (`docs/COVERAGE.md` stays the only tracker artifact there, and it is
     generated-only).
   - b: `docs/coverage-annotations.yaml` — input next to output; discoverable,
     but it puts a hand-edited file in a directory whose tracker content is
     otherwise machine-written.

3. **When does this start, relative to IMPL-0004/0005?** **Decision
   (2026-07-21): a.**
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
   bulk-action, …) appear in the first report?** **Decision (2026-07-21): a** —
   record the amendment to the design's annotation example in DESIGN-0005 when
   this lands.
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
