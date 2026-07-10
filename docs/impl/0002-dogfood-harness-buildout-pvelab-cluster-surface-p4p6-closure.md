---
id: IMPL-0002
title: "Dogfood harness buildout: pvelab, cluster surface, P4/P6 closure"
status: Draft
author: Donald Gifford
created: 2026-07-09
---

<!-- markdownlint-disable-file MD025 MD041 -->

# IMPL 0002: Dogfood harness buildout: pvelab, cluster surface, P4/P6 closure

**Status:** Draft **Author:** Donald Gifford **Date:** 2026-07-09

<!--toc:start-->

- [Objective](#objective)
- [Scope](#scope)
  - [In Scope](#in-scope)
  - [Out of Scope](#out-of-scope)
- [Coverage legend](#coverage-legend)
- [Implementation Phases](#implementation-phases)
  - [Phase 0: Substrate check + naive single-node spike](#phase-0-substrate-check--naive-single-node-spike)
    - [Tasks](#tasks)
    - [Success Criteria](#success-criteria)
  - [Phase 1: pvelab CLI skeleton — iso/up/down, no cluster](#phase-1-pvelab-cli-skeleton--isoupdown-no-cluster)
    - [Tasks](#tasks-1)
    - [Success Criteria](#success-criteria-1)
  - [Phase 2: Cluster surface + formation](#phase-2-cluster-surface--formation)
    - [Tasks](#tasks-2)
    - [Success Criteria](#success-criteria-2)
  - [Phase 3: Inner suite — P4 placement + P6 RFB, recordings](#phase-3-inner-suite--p4-placement--p6-rfb-recordings)
    - [Tasks](#tasks-3)
    - [Success Criteria](#success-criteria-3)
  - [Phase 4: Ship + pin — the steady state](#phase-4-ship--pin--the-steady-state)
    - [Tasks](#tasks-4)
    - [Success Criteria](#success-criteria-4)
  - [Phase 5: Evolution — templates, version matrix, certification](#phase-5-evolution--templates-version-matrix-certification)
    - [Tasks](#tasks-5)
    - [Success Criteria](#success-criteria-5)
- [File Changes](#file-changes)
- [Testing Plan](#testing-plan)
- [Dependencies](#dependencies)
- [Open Questions](#open-questions)
- [References](#references)
<!--toc:end-->

## Objective

Track the buildout of DESIGN-0002's dogfood harness as a checkbox ledger: the
`pvelab` CLI (`iso`/`up`/`down`/`status`/`env`), the new `cluster`
create/join-info/join SDK surface with mockpve emulation, the inner-suite
additions (password credentials, `TestResourceAffinityPlacement`,
`TestConsoleRFB`, multi-pair scrub), the `just dogfood` orchestration, and the
recording → `certification.yaml` pipeline. Phases 0–5 mirror DESIGN-0002's
phases; this document is where tasks get checked and per-phase status notes
accrue.

Working definition of "done" per task (this repo's convention): code exists,
`go build ./...` is clean, it is unit-tested against `mockpve` (or the ssh
package's in-process server), and `just lint` + `just test` are green. Steps
that require r740a are marked **(live)** and get a dated note when verified —
never checked on mock evidence alone.

**Implements:** DESIGN-0002 (all 10 design OQs resolved 2026-07-09).
**Investigations:** INV-0002 (direction + research), INV-0001 (nested-node
findings). **Closes:** IMPL-0001 → Testing Plan → Outstanding live verification
(P4 placement, P6 VNC/RFB) via Phase 3.

> **Execution gate (INV-0002, standing):** no phase starts without Donald's
> explicit go-ahead. This ledger existing does not authorize work.

## Scope

### In Scope

- `cmd/pvelab` + `cmd/pvelab/lab` (config, iso, provision, cluster, teardown,
  state) and its `just` recipes (`dogfood-iso`/`-up`/`-test`/`-down`, composite
  `dogfood`).
- `proxmox/cluster` config surface (`CreateCluster`/`JoinInfo`/`JoinCluster`)
  plus mockpve cluster-config emulation.
- Integration-suite changes: `PVE_USERNAME`/`PVE_PASSWORD` credentials, the two
  new tests, multi-pair `topologyScrub`, retirement of
  `TestResourceAffinityRule`.
- Recording-pipeline artifacts: the P4 cassette in CI replay,
  `certification.yaml`, TESTING.md/CLAUDE.md/README doc updates.
- Phase 5 evolution: `pvelab template build`, linked-clone `up`, the 9.0/9.1/9.2
  matrix.

### Out of Scope

- HA failover/fencing testing (P4 is placement only — DESIGN-0002 non-goal).
- A general-purpose PVE provisioner or any release artifact for pvelab (design
  OQ-2 = `go run`-only).
- CI-per-PR dogfood runs (on-demand only; per-PR CI stays `just test` +
  `just test-replay`).
- PBS-native testing, or SDK service surface beyond the cluster config ops.

## Coverage legend

- `[ ]` not started · `[~]` partial · `[x]` done (per the "done" definition
  above)
- **(live)** — requires r740a (or the nested cluster); checked only after a real
  run, with a dated status note
- `OQ-n` references DESIGN-0002's resolved questions; `IQ-n` references this
  document's [Open Questions](#open-questions)

## Implementation Phases

Each phase builds on the previous one. A phase is complete when all its tasks
are checked off and its success criteria are met. Each phase lands as its own PR
(IQ-4 = a) with the changelog regenerated per repo convention.

---

### Phase 0: Substrate check + naive single-node spike

Manual, evidence-gathering. The throwaway driver is **committed under
`hack/pvelab-spike/` for the record** (IQ-5 = b, superseding DESIGN-0002's "no
committed harness code" line) — it is spike evidence, not harness code, and
Phase 1's CLI supersedes it. Everything here is **(live)** by nature — the
phase's whole point is replacing desk estimates with measured reality.

#### Tasks

- [ ] **(live)** Substrate check on r740a:
      `cat /sys/module/kvm_intel/parameters/nested` → `Y`; memory headroom for
      3× 8 GiB VMs; confirm `proxmox-auto-install-assistant` + `xorriso`
      versions and the base 9.2 ISO path (the future `nested.base_iso`)
- [ ] Draft `answer.toml` for pve1 (static IP/gateway/DNS from the reserved
      pool, `root-password` from `PVELAB_ROOT_PW`, ext4/LVM defaults) and check
      it with `proxmox-auto-install-assistant validate-answer` on the node
- [ ] **(live)** Run
      `proxmox-auto-install-assistant prepare-iso <base_iso> --fetch-from iso --answer-file answer-pve1.toml`
      manually over SSH; note the exact command line + output path that
      `lab/iso.go` must reproduce
- [ ] **(live)** Create + start the pve1 VM from the prepared ISO via a
      throwaway SDK driver (CPU `host`, 4 vCPU, 8 GiB RAM, 32 GiB on
      `local-zfs`, `vmbr0`, VMID 9201)
- [ ] **(live)** Measure install wall-clock (VM start → nested `GET /version`
      answering); confirm login with
      `api.UserCredentials("root@pam", $PVELAB_ROOT_PW, "")` + insecure TLS —
      the first live proof of the user/password ticket-mint path
- [ ] **(live)** Tear down via the SDK (stop → delete); verify r740a shows no VM
      9201 and only the intended ISO artifacts remain
- [ ] Commit the throwaway driver under `hack/pvelab-spike/` (IQ-5 = b) with a
      header comment marking it superseded by `cmd/pvelab` from Phase 1; it
      lives in the module, so `go build ./...` + `just lint` must stay green
- [ ] Record measured timings + gotchas in INV-0002's Findings (replace the 5–10
      min/node and 25-min-ceiling desk estimates; tighten DESIGN-0002's
      readiness numbers if reality differs)

#### Success Criteria

- One nested PVE node installs **unattended** from a prepared ISO inside a VM on
  r740a, answers `GET /version` with password credentials, and tears down
  leaving the host clean — with the measured install wall-clock recorded in
  INV-0002. **(live)**

---

### Phase 1: pvelab CLI skeleton — iso/up/down, no cluster

The committed CLI + `lab` package, driving everything the SDK can already do
today (ISO prep over `proxmox/ssh`, node-VM provisioning, readiness, teardown,
state). Cluster formation is deliberately absent until Phase 2.

#### Tasks

- [ ] `go.mod`: YAML dependency (IQ-1 = a): promote `go.yaml.in/yaml/v4` —
      already in the module graph via go-vcr — to a direct dependency; zero new
      modules
- [ ] `cmd/pvelab/main.go`: stdlib-`flag` subcommand dispatch (`iso`, `up`,
      `down`, `status`, `env`), `slog` to stderr, version via
      `runtime/debug.ReadBuildInfo` (no ldflags — pvelab is `go run`-only per
      design OQ-2)
- [ ] `cmd/pvelab/lab/config.go`: YAML schema (DESIGN-0002 shape, plus the IQ-3
      = a auth fields `outer.ssh.key_file` / `outer.ssh.password_env` — at least
      one required, key preferred) + strict fail-fast validation (≥3 nodes,
      unique VMIDs/names/IPs, referenced env vars set); table-driven tests
- [ ] `cmd/pvelab/lab/iso.go`: render per-node `answer.toml` from a `go:embed`ed
      `text/template` (IQ-2 = a); connect with `proxmox/ssh` (known-hosts
      mandatory; auth per IQ-3 = a); SFTP the answer files, run
      `validate-answer` then `prepare-iso` per node via `Exec`; verify the
      prepared volids via `Storage().ListContent` — unit-tested against the ssh
      package's in-process SSH/SFTP server + mockpve
- [ ] `cmd/pvelab/lab/provision.go`: prepared-ISO presence check (error message
      points at `pvelab iso`), VMID-collision check, node-VM create (CPU `host`,
      sizing from config), start, per-node `/version` readiness poll (interval +
      ceiling from Phase 0 measurements); unit tests against mockpve
- [ ] `cmd/pvelab/lab/teardown.go`: stop + delete with bounded per-op contexts
      (the `cleanupCtx` pattern); `--force` tolerates missing/half-created
      objects; optional `--purge-isos`; unit tests
- [ ] `cmd/pvelab/lab/state.go`: `.pvelab-state.json` (schema-versioned)
      write/read + `.pvelab.env` emission
      (`PVE_ENDPOINT`/`PVE_USERNAME`/`PVE_PASSWORD`/`PVE_INSECURE_TLS`/
      `PVE_NODE` + test-gate vars); `down --no-state` recovery path from config
      alone; round-trip tests
- [ ] `justfile`: `dogfood-iso` / `dogfood-up` / `dogfood-down` recipes
      (branch-run `go run ./cmd/pvelab` — the designed Phase 0/buildout state);
      `.gitignore`: `pvelab.yaml`, `.pvelab-state.json`, `.pvelab.env`; commit
      `pvelab.example.yaml` (design OQ-4)
- [ ] Docs: CLAUDE.md layout + workflow notes; amend the "mockpve is the only
      binary" statements (CLAUDE.md, README) to "only _shipped_ binary — pvelab
      is a `go run` dev tool"
- [ ] `just lint` + `just test` green; changelog regenerated
- [ ] **(live)** Acceptance run: `just dogfood-iso && just dogfood-up` → 3
      nested nodes answering `/version` → `just dogfood-down` → r740a clean;
      repeat back-to-back to prove repeatability

#### Success Criteria

- `just dogfood-up` provisions 3 booted, API-answering nested PVE nodes on r740a
  and `just dogfood-down` removes them completely — repeatable back-to-back with
  no manual cleanup. **(live)**
- The `lab` package (config/iso/provision/teardown/state) is unit-tested against
  `mockpve` + the in-process SSH server in default CI — `just test` keeps zero
  live dependencies.

---

### Phase 2: Cluster surface + formation

The one new SDK surface this design needs: PVE cluster-config REST ops, plus
`pvelab` orchestration that turns 3 standalone nodes into a quorate cluster.

#### Tasks

- [ ] `proxmox/cluster/config.go`: `ClusterCreateSpec` / `JoinInfo` (lossless
      read: known fields + `Extra`) / `JoinSpec`;
      `CreateCluster`/`JoinInfo`/`JoinCluster` implemented as **fire-and-poll**
      writes (response body ignored beyond error status — upstream return shapes
      are unverified); `JoinCluster` docs state it is fresh-node-only, wipes the
      joining node's pmxcfs users/tokens, and restarts the API daemons mid-call
- [ ] `proxmox/cluster/doc.go`: promote the package overview to cover the config
      ops
- [ ] mockpve: cluster-config handlers (`POST /cluster/config`,
      `GET`/`POST /cluster/config/join`, membership visible via
      `GET /cluster/config/nodes`); the join handler validates the fingerprint
      issued by the mock's own join-info; reuse the shared `parseForm` helper
- [ ] Unit tests: happy-path create → join-info → join → membership; bad
      fingerprint rejected; double create errors
- [ ] `cmd/pvelab/lab/cluster.go`: create on pve1 → `JoinInfo` → **serialized**
      joins for pve2/pve3 (one SDK client per nested node), each tolerating the
      mid-join connection drop and converging via `GET /cluster/config/nodes`
      polls (~3 min/join bound) → final quorum check via `GET /cluster/status`
      (3 online + quorate); wired into `up`; unit tests against mockpve
- [ ] **(live)** Verify REST create/join end-to-end on the nested nodes; record
      the actual return shapes (UPID vs null) in a dated status note here +
      INV-0002, and tighten the SDK docs if warranted
- [ ] Fallback posture: an `ssh.Exec pvecm` path behind a config flag **only
      if** the live run shows REST unreliable; otherwise a doc note recording
      why no fallback exists
- [ ] `just lint` + `just test` green; changelog regenerated

#### Success Criteria

- `just dogfood-up` ends with a **3-node quorate cluster** — `/cluster/status`
  reports 3 nodes online + quorate — reproducibly from scratch. **(live)**
- The new cluster surface is mock-tested in default CI, with the join
  fingerprint/membership flow emulated in mockpve.

---

### Phase 3: Inner suite — P4 placement + P6 RFB, recordings

The payoff phase: branch-built tests run against the pvelab cluster, the two
IMPL-0001 outstanding criteria get verified live, and P4 gains a cassette that
regression-guards it in CI forever after.

#### Tasks

- [ ] Harness: `PVE_USERNAME`/`PVE_PASSWORD` support in the integration
      `newClient` (`api.UserCredentials`, used when `PVE_TOKEN_*` is absent);
      TESTING.md env-table rows; redaction already covers `password=` form
      fields + `ticket` bodies — re-verify against a recorded password-auth
      exchange before committing any cassette
- [ ] `topologyScrub` → multi-pair (outer endpoint + the three nested IPs;
      hostnames `pve1/2/3` + cluster name `dogfood` are placeholder-safe by
      construction); extend `TestScrubTopology`
- [ ] `TestResourceAffinityPlacement` (IQ-6 = a: gated on
      `PVE_TEST_PLACEMENT_VMID_1/2`, e.g. 9301/9302): two diskless dummy VMs →
      HA resources (`state=started`) → **negative** resource-affinity rule →
      poll `Cluster().ListResources` until the VMs land on different nodes (~5
      min bound) → flip to the **positive** variant → observe co-location →
      cleanup rule → resources → VMs with `cleanupCtx`
- [ ] Retire `TestResourceAffinityRule` + the `PVE_TEST_HA_SIDS` gate (harness
      const, TESTING.md, IMPL-0001 references) per design OQ-9
- [ ] `TestConsoleRFB`: scratch VM (the console-mint pattern) → `MintVNCTicket`
      → `console.Connect` → read exactly 12 bytes → assert the `"RFB 003.00x\n"`
      greeting; skipped under `PVE_REPLAY=1` (no cassette possible — design
      OQ-6)
- [ ] `justfile`: `dogfood-test` (sources `.pvelab.env`, sets `PVE_RECORD=1`,
      `-run`s the targeted tests) + composite `dogfood` (up → test → down)
- [ ] **(live)** Full `just dogfood` run: capture the P4 cassette (+ refresh any
      suite cassettes worth re-recording against the nested cluster); review
      every cassette for leaks (secrets + topology) before force-adding
- [ ] Wire the P4 cassette into `just test-replay` (`-run` list + recorded gate
      values) and confirm the `Test Replay (cassettes)` CI job replays it green
- [ ] Check **both** IMPL-0001 Outstanding-live-verification boxes with dated
      notes (P4: placement observed + cassette + CI replay; P6: live RFB
      assertion, dated, cassette-less by design); update INV-0002
      Findings/Conclusion
- [ ] TESTING.md dogfood section (prereqs, `pvelab.yaml`, `just dogfood`,
      recording flow) + CLAUDE.md testing-reality update
- [ ] `just lint` + `just test` + `just test-replay` green; changelog
      regenerated

#### Success Criteria

- IMPL-0001's P4 and P6 boxes are checked from a single `just dogfood` run:
  negative **and** positive affinity placements observed on the nested cluster,
  and the RFB greeting read over `console.Connect` from a real QEMU VNC server.
  **(live)**
- The P4 cassette replays green in the `Test Replay (cassettes)` CI job — the
  placement criterion is regression-guarded, not once-observed.

---

### Phase 4: Ship + pin — the steady state

Ends the designed chicken-and-egg state: pvelab + the cluster surface land in a
stable tag, and the recipes switch to running the pinned CLI while branch code
stays what's under test.

#### Tasks

- [ ] Confirm `.goreleaser.yml` builds only `cmd/mockpve` (pvelab must not ship
      — design OQ-2); add an explanatory comment if the config could silently
      drift
- [ ] Land pvelab + the cluster surface in a stable tag (target `v0.2.0`,
      `just release`)
- [ ] Switch the `dogfood-*` recipes to
      `go run github.com/donaldgifford/proxmox-go-sdk/cmd/pvelab@v0.2.0` (exact
      pin, bumped intentionally); keep branch-run available behind
      `PVELAB_DEV=1`
- [ ] **(live)** Post-tag smoke: from a clean checkout with only `pvelab.yaml` +
      env configured, `just dogfood` end-to-end with the pinned CLI
- [ ] Final doc sweep (README / CLAUDE.md / TESTING.md consistent on pvelab's
      `go run`-only, never-released status); changelog regenerated

#### Success Criteria

- From a clean checkout, `just dogfood` runs the **stable-pinned** CLI
  end-to-end green — released code provisions, branch code is what gets tested.
  **(live)**

---

### Phase 5: Evolution — templates, version matrix, certification

On-demand from here: faster spin-up via templates, multiple PVE minors, and the
machine-readable certification record that says which mockpve behaviour was
verified against which real PVE version.

#### Tasks

- [ ] `pvelab template build`: run the unattended install once per
      `nested.pve_version` → convert the result to an outer-node template;
      template VMID/naming convention recorded in `pvelab.example.yaml`
- [ ] `up` via **linked clones** when the version's template exists (ISO install
      as fallback when it doesn't); **(live)** measure clone-boot vs ISO-install
      wall-clock
- [ ] Version matrix: base ISOs/templates for the supported minors
      (9.0/9.1/9.2); `nested.pve_version` selects; **(live)** run the
      capability-gate tests against at least one real non-9.2 minor
- [ ] `proxmox/integration/testdata/cassettes/certification.yaml` (design OQ-8
      schema: `pve_version`, `recorded`, `commit`, `harness`, `cassettes`,
      `notes`): first entry for the 9.2 batch, one entry per matrix run
      thereafter; mock divergences reconciled (fixed in mockpve or recorded in
      `notes`) before an entry lands
- [ ] Runbook: `pve-schemadiff` drift → dogfood run → refresh recordings →
      re-certify (a TESTING.md section or docs page)
- [ ] Conclude INV-0001 + INV-0002 (→ Concluded, final findings); DESIGN-0002 →
      Implemented; this IMPL → Completed

#### Success Criteria

- A dogfood run against a **second PVE minor** completes via linked clones,
  measurably faster than the ISO-install path. **(live)**
- `certification.yaml` exists with at least one entry per tested PVE version,
  and `mockpve`'s behaviour has been reconciled against those recordings.

---

## File Changes

| File                                                      | Action | Phase | Description                                          |
| --------------------------------------------------------- | ------ | ----- | ---------------------------------------------------- |
| `hack/pvelab-spike/`                                      | Create | 0     | committed throwaway spike driver (IQ-5 = b)          |
| `cmd/pvelab/main.go`                                      | Create | 1     | subcommand dispatch, slog, buildinfo version         |
| `cmd/pvelab/lab/{config,iso,provision,teardown,state}.go` | Create | 1     | importable harness logic + tests                     |
| `cmd/pvelab/lab/answer.toml.tmpl`                         | Create | 1     | embedded answer-file template (IQ-2)                 |
| `pvelab.example.yaml` + `.gitignore` entries              | Create | 1     | committed schema example; real files git-ignored     |
| `justfile` (`dogfood-*` recipes)                          | Modify | 1–4   | iso/up/down in P1, test + composite in P3, pin in P4 |
| `proxmox/cluster/config.go`                               | Create | 2     | `CreateCluster` / `JoinInfo` / `JoinCluster`         |
| `proxmox/mockpve/cluster.go` (or extend existing)         | Modify | 2     | cluster-config emulation                             |
| `cmd/pvelab/lab/cluster.go`                               | Create | 2     | create + serialized joins + quorum poll              |
| `proxmox/integration/harness_test.go`                     | Modify | 3     | `PVE_USERNAME`/`PVE_PASSWORD` creds; gate consts     |
| `proxmox/integration/recorder_test.go`                    | Modify | 3     | multi-pair `topologyScrub`                           |
| `proxmox/integration/ha_test.go`                          | Modify | 3     | `TestResourceAffinityPlacement`; old rule test gone  |
| `proxmox/integration/console_test.go`                     | Modify | 3     | `TestConsoleRFB`                                     |
| `testdata/cassettes/TestResourceAffinityPlacement.yaml`   | Create | 3     | the P4 cassette (live-recorded, scrubbed)            |
| `testdata/cassettes/certification.yaml`                   | Create | 5     | per-version certification record                     |
| `TESTING.md` / `CLAUDE.md` / `README.md`                  | Modify | 1–5   | dogfood docs, binary-statement amendments            |
| `docs/impl/0001-proxmox-ve-9x-sdk-coverage.md`            | Modify | 3     | Outstanding-live-verification boxes checked, dated   |

## Testing Plan

- **Unit (default CI):** the `lab` package against `mockpve` + the ssh package's
  in-process SSH/SFTP server; the cluster surface against mockpve;
  config-validation table tests; state-file round-trips. `just test` stays
  live-free.
- **Replay (default CI):** the P4 cassette joins `just test-replay`;
  `TestConsoleRFB` is excluded (no cassette possible for a 101-upgrade duplex
  stream).
- **Live (on-demand):** `just dogfood` **is** the live test; the phase Success
  Criteria are its gates. Destructive scope is confined to config-declared VMIDs
  (9201–9203 for nodes; the 9301/9302 placement pair + the console scratch VMID,
  IQ-6 = a) and prepared ISOs on r740a; every created object is removed by
  `down` / test cleanup (`cleanupCtx`-bounded).

## Dependencies

- DESIGN-0002 (all OQs resolved) — the design this ledger tracks; INV-0002 /
  INV-0001 behind it
- IMPL-0001 — Outstanding live verification (P4 + P6), closed by Phase 3
- r740a (PVE 9.2-1): API token for the outer client, root SSH access for
  `pvelab iso`, the base 9.2 ISO + `proxmox-auto-install-assistant` + `xorriso`
  already on-node (design OQ-5), three reserved static IPs + VMIDs 9201–9203
- A stable tag containing pvelab + the cluster surface (target `v0.2.0`) — Phase
  4's pin prerequisite, produced by Phases 1–3 landing
- Additional base ISOs (9.0/9.1) on r740a for Phase 5's matrix

## Open Questions

Implementation-level questions surfaced while turning DESIGN-0002 into this
ledger — none re-ask the design's resolved OQs. **All six resolved by Donald
2026-07-09 (`1a 2a 3a 4a 5b 6a`)**; each heading below carries its decision, and
the resolutions are baked into the phase tasks above. Option lists are kept
as-written for the record (each led with **(a) = my recommendation**).

**IQ-1 — YAML library for `lab/config.go` — RESOLVED (a: `go.yaml.in/yaml/v4`,
promoted to direct).** The module has no direct YAML dependency today, but
go-vcr v4 already pulls `go.yaml.in/yaml/v4` (indirect).

- **a. `go.yaml.in/yaml/v4`, promoted to direct (recommended)** — already in the
  module graph, so consumers see zero new modules; it is the maintained
  successor line of `yaml.v3`.
- b. `gopkg.in/yaml.v3` — the classic import path, but it adds a second YAML
  module alongside the v4 one go-vcr already brings.
- c. a separate nested `go.mod` for `cmd/pvelab` — keeps the SDK module's
  dependency set untouched, but nested modules version independently
  (`cmd/pvelab/vX.Y.Z` tags), complicating the `@v0.2.0` pin story Phase 4
  relies on.

**IQ-2 — `answer.toml` rendering — RESOLVED (a: `go:embed`ed `text/template`).**

- **a. `go:embed`ed `text/template` (recommended)** — the answer file is small
  and scalar-only; the template visibly mirrors the upstream format, and
  `validate-answer` runs on the node before use, catching malformed output. No
  TOML-encoder dependency.
- b. TOML marshalling via `github.com/BurntSushi/toml` — schema-safe encoding,
  but a new dependency for one small write-only file.
- c. `fmt.Sprintf` string-building in code — no files or deps, least
  readable/auditable.

**IQ-3 — SSH auth for `pvelab iso` — RESOLVED (a: config supports both
`key_file` + `password_env`, key preferred).** `proxmox/ssh` supports exactly
`WithPassword` and `WithPrivateKey` (PEM bytes) — no ssh-agent support.

- **a. support both in config, prefer the key (recommended)** —
  `outer.ssh.key_file` (path, read at runtime) and/or `outer.ssh.password_env`;
  validation requires at least one. Covers a lab key file today and
  password-only setups, with zero new SDK surface.
- b. key file only — cleanest, but requires an on-disk private key (agent users,
  e.g. 1Password's SSH agent, would need to export one).
- c. password env only — zero key management, but r740a's root password lands in
  the runner env.
- d. add ssh-agent support to `proxmox/ssh` (`golang.org/x/crypto/ssh/agent`) —
  nicest for agent users, but grows public SDK surface + a dependency for the
  harness's convenience.

**IQ-4 — PR/branch strategy for the phases — RESOLVED (a: one PR per phase).**

- **a. one PR per phase (recommended)** — each phase is a coherent review unit
  with green CI, matching how this repo has landed everything so far; Phase 0
  produces only doc updates (INV-0002 findings) and rides with Phase 1's PR or a
  small docs PR.
- b. one long-running `feat/pvelab` branch merged after Phase 3 — fewer merges,
  but weeks of drift against main and a monster review.
- c. per-task PRs — maximal granularity, disproportionate CI/review overhead.

**IQ-5 — home for the Phase 0 throwaway driver — RESOLVED (b: commit under
`hack/`).**

- a. keep it uncommitted (my recommendation) — DESIGN-0002 says Phase 0 commits
  no harness code; the evidence (timings, exact commands, gotchas) lands in
  INV-0002's Findings, which is the durable artifact.
- **b. commit it under `hack/` (chosen)** — preserves the exact script, at the
  cost of maintaining throwaway code the CLI immediately supersedes.
- c. skip the script — drive Phase 0 through a temporary env-gated integration
  test; slightly less honest (it would look like suite code).

Resolution note: b supersedes DESIGN-0002's Phase 0 "no committed harness code"
sentence (amended there to match). The driver lands as `hack/pvelab-spike/` with
a header comment marking it superseded by `cmd/pvelab`; INV-0002's Findings
remain the durable record of the measurements.

**IQ-6 — env-var gates + VMIDs for the new Phase 3 tests — RESOLVED (a:
`PVE_TEST_PLACEMENT_VMID_1/2` at 9301/9302; `TestConsoleRFB` reuses
`PVE_TEST_CONSOLE_VMID`).**

- **a. `PVE_TEST_PLACEMENT_VMID_1/2` (e.g. 9301/9302) for
  `TestResourceAffinityPlacement`; `TestConsoleRFB` reuses
  `PVE_TEST_CONSOLE_VMID` (recommended)** — placement gets its own pair
  (diskless VMs, distinct from the 9101/9102 lifecycle range so the full suite
  can run in one pass); the RFB test shares the console scratch-VM pattern and
  VMID since package tests run serially.
- b. a single `PVE_TEST_PLACEMENT=1` flag with auto-picked free VMIDs — fewer
  vars, but auto-picking IDs on a shared cluster risks collisions the explicit
  convention avoids.
- c. reuse the lifecycle VMIDs (9101/9102) for placement — no new reservations,
  but the placement test then can't run in the same pass as the lifecycle tests.

## References

- DESIGN-0002 — the design this ledger implements
  (`docs/design/0002-dogfood-harness-pvelab-cli-nested-cluster-provisioning-and.md`)
- INV-0002 — direction, research, execution gate
  (`docs/investigation/0002-dogfood-harness-sdk-provisioned-nested-pve-clusters-for-p4p6.md`)
- INV-0001 — nested-node desk findings
- IMPL-0001 — Outstanding live verification (P4 + P6)
  (`docs/impl/0001-proxmox-ve-9x-sdk-coverage.md`)
- DESIGN-0001 — service-pattern contract the `cluster` surface follows
- `TESTING.md` — the recording/replay harness being extended
