---
id: IMPL-0003
title: "Apidoc tooling and docs: real schemadiff baseline"
status: In Progress
author: Donald Gifford
created: 2026-07-19
---

<!-- markdownlint-disable-file MD025 MD041 -->

# IMPL 0003: Apidoc tooling and docs: real schemadiff baseline

**Status:** In Progress **Author:** Donald Gifford **Date:** 2026-07-19 (OQs
decided 2026-07-20)

<!--toc:start-->

- [Objective](#objective)
- [Scope](#scope)
  - [In Scope](#in-scope)
  - [Out of Scope](#out-of-scope)
- [Implementation Phases](#implementation-phases)
  - [Phase 1: Parser hardening](#phase-1-parser-hardening)
    - [Tasks](#tasks)
    - [Success Criteria](#success-criteria)
  - [Phase 2: Real-baseline adoption](#phase-2-real-baseline-adoption)
    - [Tasks](#tasks-1)
    - [Success Criteria](#success-criteria-1)
  - [Phase 3: Docs and release](#phase-3-docs-and-release)
    - [Tasks](#tasks-2)
    - [Success Criteria](#success-criteria-2)
- [Open Questions](#open-questions)
- [References](#references)
<!--toc:end-->

## Objective

Land INV-0004's PR 1: make `cmd/pve-schemadiff` able to parse a **real**
`apidoc.js` (the parser bug is INV-0004 Finding 2), switch the CI schema-drift
guard from the synthetic fixture to the **real 675-endpoint PVE 9.2 surface**,
and commit the INV-0004 / review documents. After this PR, "the REST surface
drifted" is something CI can actually detect — the foundation the SDN/HA
remediation designs (DESIGN-0003/0004) and the coverage tracker (DESIGN-0005)
all build on.

## Scope

### In Scope

- The `schema.Parse` fix (decode the first JSON value; already in the working
  tree) plus a regression fixture pinning the real-world file shape (trailing
  viewer app code).
- Committing a real 9.2 apidoc artifact and regenerating
  `cmd/pve-schemadiff/testdata/baseline.json` from it (675 endpoints).
- Wiring `just schemadiff` / CI to guard the real surface.
- A documented regeneration workflow for future PVE minors.
- Committing INV-0004, `docs/REVIEW.md` (per OQ-2), and the ADR index status
  sync already in the working tree.

### Out of Scope

- Any SDK surface change — the fabrics/HA/DLB findings are DESIGN-0003/0004.
- The coverage tracker (DESIGN-0005).
- Per-minor baselines (a 9.1 dump alongside 9.2) — see OQ-3.

## Implementation Phases

### Phase 1: Parser hardening

#### Tasks

- [x] 1. Land the `schema.Parse` fix: decode the first complete JSON value with
     `json.Decoder` starting at the first `[`; ignore the trailing viewer app
     code. (Done 2026-07-20; implemented 2026-07-19.)
- [x] 2. Add a regression fixture reproducing the real file shape — the schema
     array followed by JavaScript containing `]` / `;` / brackets — and a unit
     test asserting it parses to the same endpoint set as the clean sample.
     (Done 2026-07-20 as `TestParseTrailingAppCode`, an inline fixture in
     `schema_test.go` rather than a testdata file — testdata stays CLI-only. The
     trailing JS ends in `data[0]`, so the old implementation's
     first-`[`-to-last-`]` slice fails it.)
- [x] 3. Re-verify the real dump parses to 675 endpoints via
     `pve-schemadiff -update`. (Done 2026-07-20 against the committed gzipped
     artifact: "baseline updated: 675 endpoint(s)".)

#### Success Criteria

- `just lint` + `just test` green; reverting the parser fix fails the new
  regression test.

### Phase 2: Real-baseline adoption

#### Tasks

- [x] 1. Commit the real 9.2 apidoc as
     `cmd/pve-schemadiff/testdata/apidoc-9.2.js.gz` (290 KB gzipped, per OQ-1a)
     and teach the CLI to transparently gunzip `-apidoc` inputs (sniff the gzip
     magic bytes). (Done 2026-07-20; `readApidoc` + `TestRunGzippedApidoc`
     covering the gz round-trip and the truncated-stream error.)
- [x] 2. Regenerate `testdata/baseline.json` from the committed real dump
     (`-update`): 675 endpoints replace the synthetic set. (Done 2026-07-20.)
- [x] 3. Point the `just schemadiff` recipe at the committed real dump; keep
     `apidoc.sample.js` for parser unit tests only. (Done 2026-07-20;
     `just schemadiff` reports "no drift: 675 endpoint(s)".)
- [x] 4. Document the regeneration workflow in `DEVELOPMENT.md`: fetch
     `https://<node>:8006/pve-docs/api-viewer/apidoc.js` from a 9.x node
     (placeholder host — site topology stays out of the repo), gzip into
     testdata, run `just schemadiff -update`, review the endpoint diff in the
     PR. (Done 2026-07-20 — Schema-drift guard section rewritten; the diff in a
     rebaseline PR is the minor-release API delta.)
- [x] 5. Confirm the CI schema-drift step (`just schemadiff` in the test-go job)
     passes against the real baseline with no workflow edits beyond the justfile
     change. (Done 2026-07-20 locally — recipe passes; tamper check verified:
     deleting one endpoint from baseline.json → exit 1 with the drift line. CI
     confirmation lands with the PR run.)
- [x] 6. (Found in PR #18 CI.) TruffleHog fails on PVE's own doc-example URI
     (the `http_proxy` option's placeholder proxy URL with embedded example credentials)
     inside the committed dump — the URI detector reports it as an unknown
     result and `--results=verified,unknown` treats that as failure. Fixed by
     excluding `cmd/pve-schemadiff/testdata/` via a new `.trufflehog-exclude`
     file; the dump stays verbatim per OQ-1a (upstream PVE documentation
     content, not repo secrets). (Done 2026-07-20.)

#### Success Criteria

- CI's schema-drift step guards the real 9.2 surface: removing one endpoint from
  `baseline.json` locally makes `just schemadiff` exit non-zero.
- The committed artifact's provenance (node, PVE version, fetch date) is
  recorded in `DEVELOPMENT.md` and the PR description.

### Phase 3: Docs and release

#### Tasks

- [x] 1. Commit INV-0004 (+ regenerated docz indexes / mkdocs nav) and the ADR
     README status sync (`Proposed` → `Accepted` columns). (Done 2026-07-20; the
     Draft DESIGN-0003/0004/0005 set created alongside rides the same commit —
     their OQ decisions land in their own implementation PRs.)
- [x] 2. Commit `docs/REVIEW.md` (per OQ-2a). (Done 2026-07-20.)
- [x] 3. Changelog as the branch's final commit (`git-cliff -o CHANGELOG.md` +
     `chore(changelog): Auto-sync`).
- [x] 4. Open the PR with the `patch` label — PR #18, 2026-07-20 (auto-release
     mints the next patch tag).

#### Success Criteria

- PR merged; all CI jobs green including the now-real schema-drift guard; patch
  tag auto-minted by the release workflow.

## Open Questions

1. **What apidoc artifact do we commit for CI to parse?** **Decision
   (2026-07-20): a** — gz is fine; the generated coverage doc (DESIGN-0005) will
   be the human-readable view of the file's contents.
   - **a (recommended):** Commit the full real dump **gzipped**
     (`apidoc-9.2.js.gz`, 290 KB) and teach the CLI to gunzip transparently. CI
     parses genuinely real input end-to-end; the module zip grows only 290 KB;
     regeneration is "fetch, gzip, commit" with no manual transformation of the
     evidence.
   - b: Commit a **trimmed** artifact — the schema array only, viewer code
     stripped (~4 MB raw; no gzip handling needed). Avoids the gunzip code path
     but bloats the module zip ~14x more than (a) and inserts a manual trim step
     between the node and the repo.
   - c: Commit **baseline.json only**, no in-repo apidoc. Smallest repo, but the
     CI drift step degrades to "runs only when someone supplies a fresh dump
     locally" — losing the always-on guard is how the fabrics/DLB drift survived
     this long.

2. **Does `docs/REVIEW.md` get committed?** **Decision (2026-07-20): a** —
   commit it in this PR.
   - **a (recommended):** Yes, in this PR. INV-0004 cites it as its trigger, it
     is already lint-clean, and a dated point-in-time project review is normal
     `docs/` material.
   - b: No — keep it a local working file and reword INV-0004's trigger to
     describe the review without linking it.

3. **Do we add a 9.1 baseline now?** **Decision (2026-07-20): a** — 9.2 only for
   now; revisit with DESIGN-0005 / the next 9.1 matrix run.
   - **a (recommended):** Not in this PR. Single 9.2 baseline (the fleet's
     current target); capture a 9.1 dump opportunistically on the next pvelab
     9.1 matrix run and decide then whether multi-minor baselines earn their
     upkeep — DESIGN-0005 (per-minor coverage) is where they would actually be
     consumed.
   - b: Add 9.1 now — one `pvelab up -config pvelab-9.1.yaml` cycle to fetch the
     dump. Gives minor-delta diffing immediately, at the cost of a second
     artifact plus the naming/selection scheme this PR would then have to
     design.

## References

- INV-0004 — Finding 2 (parser bug) and the 675-endpoint extraction
- `cmd/pve-schemadiff` — schema parse/diff tool (IMPL-0001 OQ-7)
- DESIGN-0003 / DESIGN-0004 / DESIGN-0005 — the work this PR unblocks
- `docs/REVIEW.md` — the project review that triggered INV-0004
