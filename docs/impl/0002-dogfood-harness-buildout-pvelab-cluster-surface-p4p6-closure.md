---
id: IMPL-0002
title: "Dogfood harness buildout: pvelab, cluster surface, P4/P6 closure"
status: Completed
author: Donald Gifford
created: 2026-07-09
---

<!-- markdownlint-disable-file MD025 MD041 -->

# IMPL 0002: Dogfood harness buildout: pvelab, cluster surface, P4/P6 closure

**Status:** Completed **Author:** Donald Gifford **Date:** 2026-07-09 (completed
2026-07-13)

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

- [x] **(live)** Substrate check on r740a:
      `cat /sys/module/kvm_intel/parameters/nested` → `Y`; memory headroom for
      3× 8 GiB VMs; confirm `proxmox-auto-install-assistant` + `xorriso`
      versions and the base 9.2 ISO path (the future `nested.base_iso`) —
      _2026-07-10: nested=Y, 121 GiB free, pve-manager 9.2.4, `local` +
      `local-zfs` active, vmbr0 UP, VMIDs 9201–9203 free. **Finding: the
      assistant, xorriso, and the base 9.2 ISO were NOT present** (design OQ-5's
      "already on node" premise was stale) — remediated as part of this task:
      assistant installed (proxmox-installer-common v9.2.7), xorriso 1.5.6, base
      ISO downloaded to `/var/lib/vz/template/iso/proxmox-ve_9.2-1.iso` (1.6
      GiB)._
- [x] Draft `answer.toml` for pve1 (static IP/gateway/DNS from the reserved
      pool, `root-password` from `PVELAB_ROOT_PW`, ext4/LVM defaults) and check
      it with `proxmox-auto-install-assistant validate-answer` on the node —
      _2026-07-10: rendered from `hack/pvelab-spike/answer-pve1.toml.tmpl` (fqdn
      = site domain, so the node name is `pve1-dogfood`; fqdn is a placeholder
      in the committed template to keep the real domain out of the repo);
      `validate-answer`: "parsed successfully, no errors found" — the
      `filter.ID_NET_NAME_MAC = "*"` NIC matcher and `[disk-setup]` keys are
      valid as written._
- [x] **(live)** Run
      `proxmox-auto-install-assistant prepare-iso <base_iso> --fetch-from iso --answer-file answer-pve1.toml`
      manually over SSH; note the exact command line + output path that
      `lab/iso.go` must reproduce — _2026-07-10: worked first try, ~1 min. The
      exact command:
      `proxmox-auto-install-assistant prepare-iso /var/lib/vz/template/iso/proxmox-ve_9.2-1.iso --fetch-from iso --answer-file /root/answer-pve1.toml --output /var/lib/vz/template/iso/proxmox-ve_9.2-1-auto-pve1.iso`
      (`--output` is a valid flag; without it the assistant writes
      `<name>-auto-from-iso.iso` beside the source). Prepared ISO volid:
      `local:iso/proxmox-ve_9.2-1-auto-pve1.iso` (1.6 GiB)._
- [x] **(live)** Create + start the pve1 VM from the prepared ISO via a
      throwaway SDK driver (CPU `host`, 4 vCPU, 8 GiB RAM, 32 GiB on
      `local-zfs`, `vmbr0`, VMID 9201) — _2026-07-10: `hack/pvelab-spike up`;
      create task ~3 s, start task ~3 s._
- [x] **(live)** Measure install wall-clock (VM start → nested `GET /version`
      answering); confirm login with
      `api.UserCredentials("root@pam", $PVELAB_ROOT_PW, "")` + insecure TLS —
      the first live proof of the user/password ticket-mint path — _2026-07-10:
      **4m04s** from VM start to `/version` answering through a real
      password-credential mint (beats the 5–10 min desk estimate). Observed poll
      cadence: 15 s sleep + ~7 s connection-refused attempt ≈ 22 s effective.
      Readiness numbers for `lab/provision.go`: keep the 15 s interval, set the
      per-node ceiling to 15 min (≈3.7× measured) instead of the design's 25
      min._
- [x] **(live)** Tear down via the SDK (stop → delete); verify r740a shows no VM
      9201 and only the intended ISO artifacts remain — _2026-07-10: first
      `down` attempt **crashed on a real SDK bug** — PVE 9.2.4 returns the guest
      config's `memory` as a quoted string (9.2-1 returned a number; confirmed
      via `pvesh`: only `memory` is stringified, `cores` stays numeric). Fixed
      in this phase: new `types.PVEInt` on all guest-config int fields (qemu +
      lxc), mockpve reconciled to serve `memory` as a string, regression
      unit-guarded (`TestConfigDecodesStringIntegers`, PVEInt table tests),
      9.2-1 cassettes still replay green. Re-run `down` succeeded —
      live-validating the fix through the ownership guard's config read;
      `zfs list` shows no 9201 datasets, ISOs intact._
- [x] Commit the throwaway driver under `hack/pvelab-spike/` (IQ-5 = b) with a
      header comment marking it superseded by `cmd/pvelab` from Phase 1; it
      lives in the module, so `go build ./...` + `just lint` must stay green —
      _2026-07-10: committed on `feat/pvelab-phase0` together with the answer
      template and the PVEInt fix._
- [x] Record measured timings + gotchas in INV-0002's Findings (replace the 5–10
      min/node and 25-min-ceiling desk estimates; tighten DESIGN-0002's
      readiness numbers if reality differs) — _2026-07-10: INV-0002 → Findings →
      "Phase 0 hardware validation": 4m04s install, 15-min ceiling
      recommendation, the memory-string SDK bug, the stale-packages premise, the
      exact assistant pipeline, fqdn→node-name, the answer-server amendment, and
      the blast-radius guards._

#### Success Criteria

- One nested PVE node installs **unattended** from a prepared ISO inside a VM on
  r740a, answers `GET /version` with password credentials, and tears down
  leaving the host clean — with the measured install wall-clock recorded in
  INV-0002. **(live)** — _MET 2026-07-10 (4m04s; see task notes above). **Phase
  0 complete.**_

---

### Phase 1: pvelab CLI skeleton — iso/up/down, no cluster

The committed CLI + `lab` package, driving everything the SDK can already do
today (ISO prep over `proxmox/ssh`, node-VM provisioning, readiness, teardown,
state). Cluster formation is deliberately absent until Phase 2.

#### Tasks

- [x] `go.mod`: YAML dependency (IQ-1 = a): promote `go.yaml.in/yaml/v4` —
      already in the module graph via go-vcr — to a direct dependency; zero new
      modules — _2026-07-10: `go mod tidy` moved it to the direct require block
      when `lab/config.go`'s import landed; no version change (v4.0.0-rc.6, the
      go-vcr pin), zero new modules._
- [x] `cmd/pvelab/main.go`: stdlib-`flag` subcommand dispatch (`iso`, `up`,
      `down`, `status`, `env`), `slog` to stderr, version via
      `runtime/debug.ReadBuildInfo` (no ldflags — pvelab is `go run`-only per
      design OQ-2) — _2026-07-11: dispatch + per-command FlagSets (`-config`,
      down's `-force`/`-no-state`/`-purge-isos`), `PVELAB_DEBUG` log level, exit
      codes 0/1/2; subcommands return a documented not-implemented error until
      their lab tasks land (each later task wires its own)._
- [x] `cmd/pvelab/lab/config.go`: YAML schema (DESIGN-0002 shape, plus the IQ-3
      = a auth fields `outer.ssh.key_file` / `outer.ssh.password_env` — at least
      one required, key preferred) + strict fail-fast validation (≥3 nodes,
      unique VMIDs/names/IPs, referenced env vars set); table-driven tests —
      _2026-07-10: `lab.LoadConfig` = strict decode (`KnownFields`, unknown keys
      error) → defaults (cores 4 / 8192 MB / 32 GB / answer listen `:8442`, the
      Phase 0 spike values) → `errors.Join` validation: required fields, ≥3
      unique nodes inside the reserved 9200–9399 VMID block (the front-door
      blast-radius guard), `netip` parsing for CIDRs/gateway/DNS, and every
      referenced env var present. Schema carries the 2026-07-10 amendments:
      `nested.domain` (fqdn = `<name>.<domain>`), `nested.answer_url` (explicit
      routable URL baked into the http-mode ISO) plus `nested.answer_listen`
      (server bind address). Table-driven tests cover every refusal path._
- [x] `cmd/pvelab/lab/iso.go` (design amended 2026-07-10: **one http-mode ISO
      per PVE version + embedded answer server**, not per-node baked ISOs):
      connect with `proxmox/ssh` (known-hosts mandatory; auth per IQ-3 = a);
      verify/install the assistant + xorriso (Phase 0 found them absent —
      apt-install or error with instructions); run
      `prepare-iso <base_iso> --fetch-from http` once per version via `Exec`;
      verify the prepared volid via `Storage().ListContent` — unit-tested
      against the ssh package's in-process SSH/SFTP server + mockpve —
      _2026-07-10: `PrepareISO` is idempotent (skips when `PreparedISOVolid` =
      `<iso_storage>:iso/pvelab-<version>-auto-http.iso` already lists) →
      `ensureAssistant` (check-first `command -v`, apt-install on miss,
      manual-install guidance on failure — the one mutation outside the VMID
      blast radius, logged before mutating) →
      `prepare-iso     --fetch-from http --url <answer_url> --output <beside base_iso>`
      → re-verify via `ListContent` (catches base_iso outside the storage's iso
      dir). `cmdISO` wired: signal-cancelled ctx, `LoadConfig` → outer client →
      `Client.SSH(cfg.SSHOptions()...)` + `Connect(cfg.OuterHost())`. **Two
      documented deviations:** (1) config gained `outer.node` — the outer PVE
      node name every node-scoped SDK call needs; a gap in DESIGN-0002's YAML
      sample (the spike hardcoded it). (2) SSH tests use an `execer` seam
      (scripted fake; `var _ execer =
      (\*ssh.Client)(nil)`pins the contract)     rather than duplicating ~100 lines of the ssh package's unexported     in-process server — the client's exec plumbing is already covered by     `proxmox/ssh`'s own tests; lab's tests cover lab's logic (command lines,     idempotence, install/verify failure paths) against mockpve. The     `--fetch-from
      http` flag shape is live-verified at the acceptance run.\_
- [x] `cmd/pvelab/lab/answers.go`: render per-node `answer.toml` from a
      `go:embed`ed `text/template` (IQ-2 = a) and serve the answers from an
      embedded HTTP server that `up` runs for the duration of the installs,
      matching each installer's POST by the `smbios1: serial=<node>` stamped at
      VM create; **verify live in this phase**: the POST payload shape (DMI
      serial field name), plain HTTP vs HTTPS + `--cert-fingerprint` (persistent
      self-signed cert in the state dir if required), and nested-VM →
      workstation reachability; the baked `--fetch-from iso` mode stays as the
      documented fallback; unit tests with `httptest` — _2026-07-10:
      `RenderAnswer` renders the `go:embed`ed `answer.toml.tmpl` (mirror of the
      validated spike template); `AnswerServer` implements `http.Handler` —
      `Start` binds `nested.answer_listen` (bind errors synchronous), `Shutdown`
      graceful, `Served()` reports first-answer times (debug only; readiness
      stays the `/version` poll). Matching is **shape-agnostic by design** (the
      POST payload shape is the live-verify unknown): the bounded body is
      substring-scanned for each node's serial — raw and base64 forms,
      longest-name-first so prefix names can't misroute — with a `?serial=` GET
      fallback; every request body logs at Debug as the live-verification
      instrument. httptest covers JSON/form/garbage/base64 bodies, no-match 404,
      longest-match, and the real Start/Shutdown listener path. The three
      live-verify items stay open for the acceptance run; HTTPS +
      `--cert-fingerprint` is deliberately unimplemented until live evidence
      says plain HTTP fails._ — _2026-07-12 (live, first acceptance runs on
      r740a): all three items answered. (1) Serial matching worked on the wire
      first try — all six installer fetches across two runs matched their nodes
      (the shape-agnostic scan does its job; the exact DMI field name stays
      unexamined because the matcher never needs it). (2) Plain HTTP sufficed —
      HTTPS/`--cert-fingerprint` stays unimplemented. (3) Reachability resolved
      by **posture, not networking**: nested→workstation was never opened (lab
      VLAN → workstation inbound blocked); instead pvelab ran ON the outer host
      (linux binary + config with `answer_url` pointing at r740a itself) — now
      the documented recommendation in TESTING.md. Six answers served across two
      runs, ~43 s after VM start each time._
- [x] `cmd/pvelab/lab/provision.go`: prepared-ISO presence check (error message
      points at `pvelab iso`), VMID-collision check, node-VM create (CPU `host`,
      sizing from config, `smbios1: serial=<node>` for answer-server matching),
      start, per-node `/version` readiness poll (interval + ceiling from Phase 0
      measurements); unit tests against mockpve — _2026-07-11:
      `EnsureISOPrepared` (points at `pvelab iso`) + `EnsureVMIDsFree` (up never
      adopts leftovers, OQ-7) + `CreateNodeVMs`/`StartNodeVMs` (the spike's VM
      spec: CPU `host`, ostype l26, scsi0 on `outer.storage`, virtio NIC, boot
      `order=scsi0;ide2`, prepared ISO on ide2, plus
      `smbios1 serial=<base64 name>,base64=1` — the answer-server match key;
      parallel per node via a shared `errors.Join` helper, every task awaited) +
      `WaitReady` (per-node parallel `/version` poll with root@pam password
      creds + insecure TLS; Phase 0 cadence: 15 s interval, 15 min ceiling, 10 s
      per-attempt timeout; `readyProbe` seam keeps the loop testable and
      `versionProbe` is itself tested against mockpve's ticket flow).
      `ownedNamePrefix` (`pvelab-`) lands here as the VM-name guard teardown
      enforces. **Drove a mockpve fidelity fix** (separate commit): the qemu/lxc
      create handlers dropped every create-form key except vmid/name, so
      create-then-Config-read returned empty — they now persist the form via
      `applyConfigForm` like real PVE._
- [x] `cmd/pvelab/lab/teardown.go`: stop + delete with bounded per-op contexts
      (the `cleanupCtx` pattern); `--force` tolerates missing/half-created
      objects; optional `--purge-isos`; unit tests. **Blast-radius guards**
      (Phase 0 spike precedent, Donald-requested 2026-07-10): refuse any VMID
      outside the reserved 9200–9399 block, and refuse to delete a VM whose name
      lacks the harness's `pvelab-` prefix — teardown only deletes what the
      harness created (both guards unit-tested, including the refusal paths) —
      _2026-07-11: `Teardown` fans out per node (errors joined; one stuck node
      hides nothing): `checkOwnership` (VMID-range + live-name-prefix via
      `Config` read; refusals surface `ErrNotOurs` and are **never** skippable
      by `Force` — Force forgives "already gone", never "not ours") →
      best-effort stop → delete, every op on a 3-min bounded context; `purgeISO`
      frees the `pvelab-`-prefixed volid (harness-owned by construction).
      `cmdDown` wired (deletes what the CONFIG says — VMIDs declared, not
      discovered; documented in its help). Refusal-path tests: foreign-named VM
      survives while owned VMs still get deleted; out-of-range VMID refused even
      when harness-named; missing VMs error without `-force`, no-op with it;
      purge-missing-ISO likewise. Also seeder-side mockpve fidelity fix:
      `AddVM`/`AddContainer` now seed `name`/`hostname` into config reads like
      real PVE._
- [x] `cmd/pvelab/lab/state.go`: `.pvelab-state.json` (schema-versioned)
      write/read + `.pvelab.env` emission
      (`PVE_ENDPOINT`/`PVE_USERNAME`/`PVE_PASSWORD`/`PVE_INSECURE_TLS`/
      `PVE_NODE` + test-gate vars); `down --no-state` recovery path from config
      alone; round-trip tests — _2026-07-11: schema-versioned `State`
      (`LoadState` surfaces `ErrNoState` for a missing file — normal before the
      first up — and rejects a newer `schema_version`; older/unknown keys
      tolerated by `encoding/json`); `UpdateState` load-or-init + mutate + 0600
      write, called after **every** up stage (seed → created → started →
      readiness) so a mid-up failure leaves evidence (OQ-7). `.pvelab.env` emits
      the design vars plus the IQ-6 = a gates (`PVE_TEST_PLACEMENT_VMID_1/2` =
      9301/9302, `PVE_TEST_CONSOLE_VMID` = 9303, `PVE_TEST_STORAGE` = local-lvm
      — the ext4 install's default), all values shell-quoted, 0600.
      `cmdUp`/`cmdStatus`/`cmdEnv` wired (all five subcommands now real): up =
      preflight → answer server → staged provision → env write; status = outer
      VM view + state readiness; env = re-derive to stdout. `down` removes the
      two handoff files on success unless `-no-state` (deletion itself is always
      config-driven, so a lost state file never strands VMs). Round-trip +
      newer-schema + perms + env content tests._
- [x] `justfile`: `dogfood-iso` / `dogfood-up` / `dogfood-down` recipes
      (branch-run `go run ./cmd/pvelab` — the designed Phase 0/buildout state);
      `.gitignore`: `pvelab.yaml`, `.pvelab-state.json`, `.pvelab.env`; commit
      `pvelab.example.yaml` (design OQ-4) — _2026-07-11: three pass-through
      recipes (`*args` forwards `-force`/`-purge-isos`/etc.), the three
      git-ignore entries, and the example config (placeholder domain/IPs per the
      topology-scrub rule; every schema field represented).
      `TestExampleConfigValid` pins the committed example to the schema — a
      config-field change now fails tests until the example is updated._
- [x] Docs: CLAUDE.md layout + workflow notes; amend the "mockpve is the only
      binary" statements (CLAUDE.md, README) to "only _shipped_ binary — pvelab
      is a `go run` dev tool" — _2026-07-11: both amendments made; CLAUDE.md
      layout now shows `cmd/pvelab` (+ `cmd/pve-schemadiff`,
      `hack/pvelab-spike`) and a new "Dogfood lab (pvelab)" workflow section
      covers the recipes, config/secrets rules, blast-radius guards, state/env
      handoff, and the http-mode install flow with its live-verify items._
- [x] `just lint` + `just test` green; changelog regenerated — _2026-07-11: both
      green locally (race + coverage; the full linter set);
      `git-cliff -o CHANGELOG.md` regenerated in the phase's changelog commit._
- [x] **(live)** Acceptance run: `just dogfood-iso && just dogfood-up` → 3
      nested nodes answering `/version` → `just dogfood-down` → r740a clean;
      repeat back-to-back to prove repeatability — _2026-07-12, in progress:
      `iso` + two `up` runs completed on r740a (run-on-host posture — the pvelab
      linux binary on the outer node itself). Both runs installed all 3 nodes in
      ~4 min (answers matched by serial, `/version` ready); the first exposed
      the join-quorum race (found + fixed, see Phase 2), the second was green
      end-to-end (`lab is up`, 3-node quorate cluster, 4m41s total). Still open
      for this box: the formal cycle — `down` → r740a-clean check → repeat
      back-to-back._ — _2026-07-12, formal cycle COMPLETE (Donald, on r740a):
      after the Phase 3 dogfood run's `down`, the clean check passed — zero
      VMIDs in the reserved 9200–9399 block (`qm list` shows no stray guests at
      all), `.pvelab-state.json` + `.pvelab.env` removed, prepared ISO preserved
      by design. Then the back-to-back repeat: `./pvelab iso` skipped
      idempotently (volid already present); `./pvelab up` from bare VMIDs →
      quorate 3-node cluster in **4m39s** (vs 4m41s on run two — near-identical
      timings: parallel installs ~4m00s/node, answers served ~43 s after VM
      start, create → pve2 join 14 s → quorate(2) → pve3 join 6 s → quorate(3));
      `./pvelab down` deleted all three VMIDs; the second clean check passed
      identically. No manual cleanup at any point in either cycle._

#### Success Criteria

- `just dogfood-up` provisions 3 booted, API-answering nested PVE nodes on r740a
  and `just dogfood-down` removes them completely — repeatable back-to-back with
  no manual cleanup. **(live)** — _2026-07-12: MET — two full up/down cycles
  (the Phase 3 dogfood lab lifetime, then the formal repeat), both formed
  quorate(3) in under 5 min and both left r740a clean (VMID block empty, state
  files removed, prepared ISO intact). Run-on-host posture note: the recipes
  wrap the same `pvelab` subcommands the acceptance ran directly on r740a._
- The `lab` package (config/iso/provision/teardown/state) is unit-tested against
  `mockpve` + the in-process SSH server in default CI — `just test` keeps zero
  live dependencies.

---

### Phase 2: Cluster surface + formation

The one new SDK surface this design needs: PVE cluster-config REST ops, plus
`pvelab` orchestration that turns 3 standalone nodes into a quorate cluster.

#### Tasks

- [x] `proxmox/cluster/config.go`: `ClusterCreateSpec` / `JoinInfo` (lossless
      read: known fields + `Extra`) / `JoinSpec`;
      `CreateCluster`/`JoinInfo`/`JoinCluster` implemented as **fire-and-poll**
      writes (response body ignored beyond error status — upstream return shapes
      are unverified); `JoinCluster` docs state it is fresh-node-only, wipes the
      joining node's pmxcfs users/tokens, and restarts the API daemons mid-call
      — _2026-07-11: landed per the pinned signatures plus `ListConfigNodes`
      (the convergence read both writes' docs point at) on the `API` interface.
      Two shape-hedges beyond the sketch: `JoinInfo.Fingerprint` is a METHOD
      (the wire carries per-node `pve_fp` inside `nodelist`, no top-level
      fingerprint field — it selects the preferred node's, falling back to the
      first entry's), and `JoinNode`/`ConfigNode` model only string fields
      (`name`/`pve_addr`/`pve_fp`; `node`+`name` with a `NodeName()` accessor)
      routing everything numeric to `Extra`, so an unverified live shape can't
      hard-fail the decode. `JoinSpec.Force` is `*types.PVEBool` (the repo's
      Force precedent); links/nodeid/votes go via `Extra`._
- [x] `proxmox/cluster/doc.go`: promote the package overview to cover the config
      ops — _2026-07-11: fire-and-poll semantics, the fresh-node-only join
      warning, and the Fingerprint flow; `go doc` renders clean._
- [x] mockpve: cluster-config handlers (`POST /cluster/config`,
      `GET`/`POST /cluster/config/join`, membership visible via
      `GET /cluster/config/nodes`); the join handler validates the fingerprint
      issued by the mock's own join-info; reuse the shared `parseForm` helper —
      _2026-07-11: join-info issues the exported `ClusterFingerprint` const for
      every member and the join handler requires exactly it. One emulation seam
      the wire protocol forces: on real PVE the joining node's identity is
      implicit (the request is served BY the joining node), but one mock serves
      every role, so `QueueClusterJoin(name)` seeds the identity each join
      consumes (in order) and `SetClusterSelfNode` names the mock's own node
      (default "pve"). Double create and wrong-fingerprint joins 400 like real
      PVE; a standalone mock's `config/nodes` returns an empty list
      (REST-with-caveat — live shape unverified)._
- [x] Unit tests: happy-path create → join-info → join → membership; bad
      fingerprint rejected; double create errors — _2026-07-11: plus lossless
      Extra assertions (nodelist `nodeid`, top-level `totem`), standalone
      join-info/join errors, spec validation, and `Fingerprint()` selection
      order. All against mockpve._
- [x] `cmd/pvelab/lab/cluster.go`: create on pve1 → `JoinInfo` → **serialized**
      joins for pve2/pve3 (one SDK client per nested node), each tolerating the
      mid-join connection drop and converging via `GET /cluster/config/nodes`
      polls (~3 min/join bound) → final quorum check via `GET /cluster/status`
      (3 online + quorate); wired into `up`; unit tests against mockpve —
      _2026-07-11: `FormCluster` with a `clusterDialer` seam (endpoint →
      `cluster.API`, the `readyProbe` pattern); production dials a fresh SDK
      client per poll attempt (root@pam password creds — tokens don't survive a
      join; insecure TLS — certs churn at join), so a ticket invalidated by the
      formation restarts can never wedge a poll. Join request errors are
      swallowed with a Warn by design; a genuinely rejected join surfaces as the
      bounded membership-poll timeout naming the node. `up` runs formation after
      readiness and records `Clustered` in state (additive schema key); `status`
      prints it. Tests back the dialer with ONE mock playing every node (its
      cluster state is the shared world; join identities via `QueueClusterJoin`,
      quorum via seeded status): happy path, join-never- converges (names the
      stuck node), create-failure fatal, quorum timeout, and the production
      dialer against the mock's password-ticket flow._
- [x] **(live)** Verify REST create/join end-to-end on the nested nodes; record
      the actual return shapes (UPID vs null) in a dated status note here +
      INV-0002, and tighten the SDK docs if warranted — _2026-07-12, first live
      formation run (pvelab binary ON r740a): REST create succeeded and the
      first join (pve2) was accepted and converged in 13s; the second join
      (pve3) was accepted (2xx — no request error logged) but never converged.
      Root cause found in the lab logic, not the SDK ops: convergence polled the
      corosync **config** nodelist, and config presence precedes runtime health
      — pve2 was in corosync.conf (expected votes already 2) while its corosync
      was still starting, so the cluster was momentarily non-quorate and pve3's
      join task failed server-side (pmxcfs read-only). Fixed by a per-join
      `/cluster/status` quorum gate (quorate + members-so-far online) before the
      next join; the last gate doubles as the final quorum check. Pinned by
      `TestFormClusterGatesNextJoinOnQuorum` (scripted settle window)._ —
      _2026-07-12, second run (gate fix in): **fully converged formation** —
      create; pve2 joined in 14 s, quorate members=2 after 5 s more; pve3 joined
      in 6 s, quorate members=3; `cluster formed`; whole `up` 4m41s from bare
      VMIDs. REST create/join verified reliable end-to-end. Return shapes (UPID
      vs null) recorded as **deliberately unobservable through the SDK**: the
      fire-and-poll ops ignore response bodies by design and `PVE_DEBUG` logs
      requests only — the shape is irrelevant to convergence, and the ops' docs
      already say precisely this (no tightening needed). Noted in INV-0002
      Findings._
- [x] Fallback posture: an `ssh.Exec pvecm` path behind a config flag **only
      if** the live run shows REST unreliable; otherwise a doc note recording
      why no fallback exists — _2026-07-12: no fallback. Two live formations
      showed the REST endpoints themselves reliable — the only failure was the
      lab's own config-vs-runtime quorum race (fixed by the per-join gate), not
      a PVE endpoint. Recorded on `FormCluster`'s doc comment._
- [x] `just lint` + `just test` green; changelog regenerated — _2026-07-11: full
      suite (race, 25 packages) + `just test-replay` green; go-style review of
      the change set returned 0 errors (2 advisory notes matching existing repo
      precedent: the design-pinned `ClusterCreateSpec` name, and mockpve's
      naked-bool seeder shape)._

#### Success Criteria

- `just dogfood-up` ends with a **3-node quorate cluster** — `/cluster/status`
  reports 3 nodes online + quorate — reproducibly from scratch. **(live)** —
  _met 2026-07-12 (second formation run, quorum-gate fix in): 3 nodes online +
  quorate from scratch in 4m41s. Formal repeatability evidence (back-to-back
  cycle) accrues with the Phase 1 acceptance box._ — _accrued 2026-07-12: the
  formal repeat formed quorate(3) again in 4m39s (see Phase 1)._
- The new cluster surface is mock-tested in default CI, with the join
  fingerprint/membership flow emulated in mockpve. — _2026-07-11: met (unit
  tests in `proxmox/cluster` + `cmd/pvelab/lab` run in the default suite)._

---

### Phase 3: Inner suite — P4 placement + P6 RFB, recordings

The payoff phase: branch-built tests run against the pvelab cluster, the two
IMPL-0001 outstanding criteria get verified live, and P4 gains a cassette that
regression-guards it in CI forever after.

#### Tasks

- [x] Harness: `PVE_USERNAME`/`PVE_PASSWORD` support in the integration
      `newClient` (`api.UserCredentials`, used when `PVE_TOKEN_*` is absent);
      TESTING.md env-table rows; redaction already covers `password=` form
      fields + `ticket` bodies — re-verify against a recorded password-auth
      exchange before committing any cassette — _2026-07-11: `envCredentials`
      selects token (wins) or user/pass; the dotenv autoload accepts either
      complete pair. The re-verification is a STANDING guard, not a one-off:
      `TestRecorderPasswordAuthRedaction` (non-tagged, default CI) records a
      real password-auth exchange through the recorder against mockpve and
      asserts the password and the actual minted ticket/CSRF values
      (mock-ticket-/mock-csrf- material — not a pattern that could pass
      vacuously) never reach disk._
- [x] `topologyScrub` → multi-pair (outer endpoint + the three nested IPs +
      **the site DNS domain**: Phase 0 set real fqdns like
      `pve<n>-dogfood.<site-domain>`, so the domain must scrub to a placeholder
      — the original "hostnames are placeholder-safe by construction" assumption
      no longer holds; the `pve<n>-dogfood` hostname part and cluster name
      `dogfood` remain safe); extend `TestScrubTopology` — _2026-07-11:
      `topologyScrub` is now an ordered live=placeholder pair list; extra pairs
      ride `PVE_SCRUB_EXTRA` (CSV of `live=placeholder`; malformed entries error
      rather than silently leak). pvelab derives the value into `.pvelab.env`:
      the other nodes' IPs → TEST-NET stand-ins, the domain → `lab.example`, the
      outer host (defensive) → `outer.example`; the first node's IP is already
      scrubbed via `PVE_ENDPOINT`. New `TestScrubTopologyMultiPair` covers the
      corosync-ring/fqdn shape._
- [x] `TestResourceAffinityPlacement` (IQ-6 = a: gated on
      `PVE_TEST_PLACEMENT_VMID_1/2`, e.g. 9301/9302): two diskless dummy VMs →
      HA resources (`state=started`) → **negative** resource-affinity rule →
      poll `Cluster().ListResources` until the VMs land on different nodes (~5
      min bound) → flip to the **positive** variant → observe co-location →
      cleanup rule → resources → VMs with `cleanupCtx` — _2026-07-11: as
      specified, plus two hardenings: cleanup resolves each VM's CURRENT node
      before the node-scoped stop/delete (the scheduler may have migrated it),
      and the poll interval shrinks under `PVE_REPLAY=1` so the future cassette
      replays fast. Compile-verified + skip-gated; execution is the (live) task
      below._
- [x] Retire `TestResourceAffinityRule` + the `PVE_TEST_HA_SIDS` gate (harness
      const, TESTING.md, IMPL-0001 references) per design OQ-9 — _2026-07-11:
      test file deleted, gate const removed, TESTING.md
      runbook/table/cheat-sheet updated, IMPL-0001 annotated with dated notes
      (history kept, not rewritten)._
- [x] `TestConsoleRFB`: scratch VM (the console-mint pattern) → `MintVNCTicket`
      → `console.Connect` → read exactly 12 bytes → assert the `"RFB 003.00x\n"`
      greeting; skipped under `PVE_REPLAY=1` (no cassette possible — design
      OQ-6) — _2026-07-11: asserts the RFC 6143 greeting shape
      (`RFB \d{3}.\d{3}\n`) rather than pinning one minor; the read is bounded
      (30 s) since the stream has no deadline API. One consequence the ledger
      task didn't spell out: under `PVE_RECORD=1` the test uses the new
      `newStreamClient` (record-bypassing live client) because the hijacked
      101-upgrade stream cannot ride go-vcr — the dogfood run records placement
      while RFB stays deliberately cassette-less._
- [x] `justfile`: `dogfood-test` (sources `.pvelab.env`, sets `PVE_RECORD=1`,
      `-run`s the targeted tests) + composite `dogfood` (up → test → down) —
      _2026-07-11: `dogfood-test` -runs placement+RFB with `-timeout 30m` and
      passes extra flags via args; the composite tears down via bash trap even
      when the suite fails (cassettes + state survive for review)._
- [x] **(live)** Full `just dogfood` run: capture the P4 cassette (+ refresh any
      suite cassettes worth re-recording against the nested cluster); review
      every cassette for leaks (secrets + topology) before force-adding —
      _2026-07-12, first inner-suite run against the live pvelab cluster: **both
      tests failed, each a genuine live-only finding** (the harness doing its
      job). (1) `TestConsoleRFB` → 401 "invalid PVEVNC ticket":
      `console.Connect` dialled the node-shell `/nodes/{n}/vncwebsocket` for
      every ticket, but real PVE binds a guest ticket to the guest's own
      `/nodes/{n}/{qemu|lxc}/{vmid}/vncwebsocket`. **SDK fixed** (VNCTicket
      carries unexported mint provenance; Connect routes on it) and **mockpve
      fidelity fixed** — the mock accepted any minted ticket at the node path,
      which is exactly how the bug passed unit tests; it now binds each ticket
      to its dial path (`TestConnectGuestTicketBoundToGuestPath`). (2)
      `TestResourceAffinityPlacement` → 500 "rule defines more resources than
      available nodes" 3.9 s in: PVE's rule feasibility counts **HA-active
      nodes** (LRMs, which lag AddResource by a few 10 s cycles on a
      first-ever-HA cluster), not members; the test now retries rule creation on
      that specific error (`createRuleSettled`) until the stack settles. Re-run
      pending._ — _2026-07-12, second inner-suite run: **negative
      resource-affinity placement OBSERVED LIVE** (the retry worked, the rule
      landed, and the scheduler separated vm:9301 → pve2 / vm:9302 → pve3), and
      the **live RFB greeting bytes arrived** (`0x82 0x0c` + "RFB 003.008\n" —
      WebSocket-framed, exactly Connect's documented contract; the test now
      de-frames instead of expecting raw bytes). Two more fixes fell out: (1)
      the positive-flip `UpdateRule` got 400 "Parameter verification failed."
      with the failing fields invisible — `pverr.Error` now renders the `Params`
      map, `HARuleUpdate` gained `Type`, and the flip sends the type + its
      required properties (PVE's plugin schema keeps resource-affinity's
      `affinity`+`resources` required on update); (2) cleanup raced an in-flight
      HA migration ("VM is locked (migrate)" → "destroy failed") —
      `deleteSettled` now retries the stop+delete round, re-resolving the node,
      until the guest unlocks. Third run pending: the positive flip + full green
      pass + P4 cassette capture._ — _2026-07-12, third inner-suite run: **FULL
      PASS.** `TestConsoleRFB` read `"RFB 003.008\n"` live;
      `TestResourceAffinityPlacement` observed negative (vm:9301 → pve2-dogfood,
      vm:9302 → pve3-dogfood) **and** positive (co-located on pve3-dogfood)
      placement; clean teardown. The P4 cassette was captured and leak-reviewed:
      review found ONE automated-scrub gap — go-vcr stores the request `Host`
      separately from the URL and `topologyScrub.apply` never rewrote it (the
      earlier committed batch was hand-fixed without noticing), so 32 `host:`
      fields carried the outer endpoint. `apply` now scrubs `Request.Host` + all
      request/response header values (`TestScrubTopology` extended to pin it),
      the cassette was hand-fixed to `pve.example:8006`, and a full re-scan
      shows zero topology/secret leaks (48 REDACTED markers intact). Evidence
      note: the run used the run-on-host posture — `./pvelab up` on r740a +
      `just dogfood-test` from the workstation — i.e. the composite's steps
      executed individually, not the single `just dogfood` invocation._
- [x] Wire the P4 cassette into `just test-replay` (`-run` list + recorded gate
      values) and confirm the `Test Replay (cassettes)` CI job replays it green
      — _2026-07-12: `TestResourceAffinityPlacement` added to the `-run` list
      with `PVE_TEST_PLACEMENT_VMID_1=9301`/`_2=9302`; local `just test-replay`
      green (the placement test replays both affinity assertions from the
      cassette in ~2.4 s — `placementPollReplay` doing its job). The CI job runs
      the identical recipe and fires on this branch's push._
- [x] Check **both** IMPL-0001 Outstanding-live-verification boxes with dated
      notes (P4: placement observed + cassette + CI replay; P6: live RFB
      assertion, dated, cassette-less by design); update INV-0002
      Findings/Conclusion — _2026-07-12: both boxes checked with the live
      evidence + fold-back findings; the section now records that every
      IMPL-0001 Success Criterion is verified. INV-0002 Findings/Conclusion
      updated (nested-virt viability CONFIRMED end-to-end). certification.yaml
      gained the 9.2.2 nested-cluster batch entry._
- [x] TESTING.md dogfood section (prereqs, `pvelab.yaml`, `just dogfood`,
      recording flow) + CLAUDE.md testing-reality update — _2026-07-11: "The
      dogfood lab (pvelab)" section after the acceptance checklist; the console
      runbook covers the RFB test; CLAUDE.md testing-reality names the new
      tests, the credential pairs, and `PVE_SCRUB_EXTRA`._
- [x] `just lint` + `just test` + `just test-replay` green; changelog
      regenerated — _2026-07-11: all three green locally (replay unchanged — the
      P4 cassette wiring is the live-gated task above)._

#### Success Criteria

- IMPL-0001's P4 and P6 boxes are checked from a single `just dogfood` run:
  negative **and** positive affinity placements observed on the nested cluster,
  and the RFB greeting read over `console.Connect` from a real QEMU VNC server.
  **(live)** — _2026-07-12: MET, with one posture note: the evidence came from
  one lab lifetime driven as individual steps (`./pvelab up` on r740a, then
  `just dogfood-test` recording, then `down`) rather than the composite
  `just dogfood` recipe — the run-on-host posture splits the steps by machine.
  Both placements and the RFB greeting were observed in that single lab
  lifetime._
- The P4 cassette replays green in the `Test Replay (cassettes)` CI job — the
  placement criterion is regression-guarded, not once-observed. — _2026-07-12:
  wired + green locally via the identical `just test-replay` recipe; the CI leg
  fires on this branch's push._ — _confirmed same day: the job ran green on PR
  #12's full-CI run (the first with the cassette) and on every main push since
  the stack merged. CRITERION MET._

---

### Phase 4: Ship + pin — the steady state

Ends the designed chicken-and-egg state: pvelab + the cluster surface land in a
stable tag, and the recipes switch to running the pinned CLI while branch code
stays what's under test.

#### Tasks

- [x] Confirm `.goreleaser.yml` builds only `cmd/mockpve` (pvelab must not ship
      — design OQ-2); add an explanatory comment if the config could silently
      drift — _2026-07-11: confirmed — the `builds` list has exactly one entry
      (`main: ./cmd/mockpve`); the drift risk is someone appending a second
      entry for a future `cmd/`, so a comment now sits directly on the `builds`
      block naming pvelab as `go run`-only per OQ-2 and pointing at
      DESIGN-0002/IMPL-0002. `goreleaser check` green._
- [x] Land pvelab + the cluster surface in a stable tag (target `v0.2.0`,
      `just release`) — _2026-07-12: landed as **v0.6.0**, and the task's
      premise was stale: releases are AUTOMATIC — `release.yml` runs
      `pr-semver-bump` on every merge to main, minting the next tag from the
      merged PR's semver label (that is what the mandatory PR labels are for).
      `v0.2.0` was auto-minted 2026-07-11 by an unrelated merge; the #8–#12
      stack minted v0.3.0 → v0.6.0, so v0.6.0 is the first tag carrying the full
      pvelab + cluster surface. `just release` never was the flow (recipe +
      README/DEVELOPMENT/CLAUDE.md corrected). Found in the same sweep: every
      v0.2.0–v0.6.0 goreleaser run FAILED at the final signing step
      (`.goreleaser.yml` signs checksums with `{{ .Env.GPG_FINGERPRINT }}` but
      the workflow never imported a key) — tags and module consumption were
      unaffected; only the GitHub Release artifacts (mockpve binaries) never
      published. Fixed in this phase's PR (GPG import step + `GPG_FINGERPRINT`
      env; secrets added by Donald); the fix proves itself on this PR's own
      merge-release._ — _confirmed same day: PR #13's merge minted v0.6.1 and
      its Release run went green — the repo's FIRST successful goreleaser
      publish (mockpve archives ×4 platforms + SBOMs + `checksums.txt` +
      `checksums.txt.sig` on the v0.6.1 release page)._
- [x] Switch the `dogfood-*` recipes to
      `go run github.com/donaldgifford/proxmox-go-sdk/cmd/pvelab@v0.2.0` (exact
      pin, bumped intentionally); keep branch-run available behind
      `PVELAB_DEV=1` — _2026-07-12: done, pinned to **v0.6.0** (see above; the
      task's v0.2.0 was the desk-estimated tag). `justfile` gains `pvelab_pin` +
      a `pvelab_pkg` expression: default is the pinned module path,
      `PVELAB_DEV=1` switches to `./cmd/pvelab`. Verified: the pin resolves and
      self-reports `pvelab v0.6.0` via `go run`, and both recipe variants render
      correctly (`just -n`). TESTING.md's run-on-host section shows the pinned
      cross-compile (`GOOS=linux GOARCH=amd64 go install …@v0.6.0`) alongside
      the dev build._
- [x] **(live)** Post-tag smoke: from a clean checkout with only `pvelab.yaml` +
      env configured, `just dogfood` end-to-end with the pinned CLI —
      _2026-07-12 (Donald, on r740a): PASSED with the pinned CLI built straight
      from the module proxy —
      `GOBIN= GOOS=linux GOARCH=amd64 go install …/cmd/pvelab@v0.6.0` (no
      checkout at all, a stronger form of "clean checkout"; the empty GOBIN
      dodges mise's setting, which blocks cross-compiled installs), scp'd to
      r740a per the run-on-host posture with the same `pvelab.yaml` + env. `up`
      formed quorate(3) in **4m40s** — the third consecutive formation inside a
      2-second spread (4m41s, 4m39s, 4m40s) — `down` deleted all three VMIDs,
      and the clean check passed (VMID block empty, state files removed,
      prepared ISO intact). Smoked at v0.6.0, the recipes' exact pin; v0.6.1
      (minted by this phase's own merge) contains no pvelab changes and the pin
      moves only when pvelab itself does._
- [x] Final doc sweep (README / CLAUDE.md / TESTING.md consistent on pvelab's
      `go run`-only, never-released status); changelog regenerated —
      _2026-07-12: swept together with the auto-release correction — README,
      DEVELOPMENT.md, and CLAUDE.md now describe the label-driven automatic
      release flow (manual `just release` demoted to recovery-only), CLAUDE.md's
      dogfood section names the pin + `PVELAB_DEV=1`, and TESTING.md's
      run-on-host runbook shows both the pinned `go install` and the dev build.
      pvelab's status is unchanged and consistently stated: `go run`-only, never
      a goreleaser artifact (the pin changes WHICH source `go run` builds, not
      how it ships)._

#### Success Criteria

- From a clean checkout, `just dogfood` runs the **stable-pinned** CLI
  end-to-end green — released code provisions, branch code is what gets tested.
  **(live)** — _2026-07-12: MET (run-on-host form): the pinned CLI was built
  from the module proxy with no checkout and provisioned/tore down a quorate
  3-node lab on r740a; the recipes run that identical pinned module path. The
  designed chicken-and-egg state is over — Phase 4 is COMPLETE._

---

### Phase 5: Evolution — templates, version matrix, certification

On-demand from here: faster spin-up via templates, multiple PVE minors, and the
machine-readable certification record that says which mockpve behaviour was
verified against which real PVE version.

#### Tasks

- [x] `pvelab template build`: run the unattended install once per
      `nested.pve_version` → convert the result to an outer-node template;
      template VMID/naming convention recorded in `pvelab.example.yaml` —
      _2026-07-11: implemented + mock-verified (go-architect designed; see the
      pvelab-template-clone-architecture memory). New SDK op
      `qemu.ConvertToTemplate` (action-option pattern, **maybe-UPID hedge** —
      the return shape is unconfirmed on 9.x, callers check `Ref.IsZero()`; the
      `nodes.ApplyNetworkConfig` precedent); mockpve rejects running VMs and
      flags templates in the VM listing. `lab.BuildTemplate` reuses the
      provision path unchanged via a synthetic single-node `TemplateConfig`
      (name `tmpl-<version>` with dots dashed — it doubles as the guest hostname
      label) → graceful shutdown → installer-ISO detach → convert. Template name
      is COMPUTED (`pvelab-tmpl-<version>`), never configured; VMID is explicit
      config in the new **9210–9219 template sub-range**
      (`nested.template {vmid, cidr}`, validated against node collisions;
      recorded in `pvelab.example.yaml` with the one-template-per-minor matrix
      convention). Discovery is per-run by name (`FindTemplate`) — no state-file
      tracking, templates outlive labs. `-force` deletes ours by computed name;
      a foreign VM on the template VMID is always refused. The actual build
      against r740a is **live-only** (unattended install), still unrun._
- [x] `up` via **linked clones** when the version's template exists (ISO install
      as fallback when it doesn't); **(live)** measure clone-boot vs ISO-install
      wall-clock — _2026-07-11: implemented + mock-verified; only the **(live)**
      half keeps this box open. `cloneSource` picks the path: a real template +
      a configured `nested.template` block (its CIDR is the clone boot address)
      → `upViaClone` (no ISO, no answer server); else warn + the extracted
      `upViaISO`. Building a template IS the opt-in — no flag. `CloneNodeVMs`
      linked-clones all nodes STOPPED (Full unset = PVE's template default);
      `ReidentifyClones` then starts them **one at a time** — every clone boots
      the template's baked-in hostname/IP/host key, so parallel starts would
      collide — SSH-ing in at the template's IP (dial retried through the boot
      window; TOFU host-key pinned across the run's serialized dials), rewriting
      hostname + /etc/hosts + /etc/network/interfaces, regenerating SSH host
      keys, moving the pmxcfs node dir best-effort, rebooting, and polling the
      node's OWN endpoint before touching the next clone. **PVE tolerating the
      hostname/IP rename end-to-end is the clone path's load-bearing live-verify
      unknown** — tests pin command sequence, dial retry, serialization order,
      and the tolerated reboot drop, never PVE's behaviour. Wall-clock
      measurement + first live clone run remain **(live)**._ — _2026-07-12,
      first live clone run (Donald, r740a, pinned v0.6.0): **PVE TOLERATES THE
      RENAME — the clone path works end-to-end, first try.** `template build`
      installed + converted `pvelab-tmpl-9-2` (VMID 9210) in 4m20s (install
      4m00s, shutdown+detach+convert ~14s). The clone `up`: linked clones of all
      3 nodes materialized in ~1 s, serialized re-identify ran 51 s / 53 s / 51
      s per node, and the RENAMED nodes formed a quorate cluster with timings
      indistinguishable from ISO-installed ones (pve2 join 14 s → quorate(2) →
      pve3 join 6 s → quorate(3)) — pmxcfs, corosync, and the cert plumbing all
      healthy post-rename. **Wall-clock: 3m06s cloned vs 4m39–41 s ISO (three
      ISO baselines within 2 s) — ~1m34s (~33%) faster**, with the one-time
      4m20s template build amortizing across runs. `down` deleted the three node
      VMs and left template 9210 in place (by design — teardown enumerates only
      `cfg.Nested.Nodes`); state files removed, prepared ISO intact._
- [x] Version matrix: base ISOs/templates for the supported minors
      (9.0/9.1/9.2); `nested.pve_version` selects; **(live)** run the
      capability-gate tests against at least one real non-9.2 minor —
      _2026-07-11: the non-live half fell out of tasks 1–2 (deliberately
      doc-only, per the phase design): `nested.pve_version` already selects the
      prepared ISO AND the computed template name, and the template VMID
      sub-range (9210–9219) plus the one-config-file-per-minor convention are
      documented in `pvelab.example.yaml`. No new code. Open on the **(live)**
      half: a second minor's base ISO on r740a + the capability-gate run against
      it._ — _2026-07-13, first matrix run (Donald, r740a, pinned v0.6.0, config
      `pvelab-9.1.yaml`): **PASSED end-to-end on PVE 9.1.1.** The 9.2.7
      assistant prepared the 9.1 ISO; `template build` installed + converted
      `pvelab-tmpl-9-1` in 4m04s (install 3m44s — 9.1 installs slightly faster
      than 9.2); the clone `up` formed quorate(3) in **3m11s** (re-identify
      54/52/53 s per node — PVE 9.1 also tolerates the rename; formation shape
      identical). As-run VMIDs deviate from the example convention and that is
      fine: the 9.1 config gave its NODES 9211–9213 and the template 9219 —
      validation only demands nodes in 9200–9399, template in 9210–9219, no
      collisions; both templates (9210 for 9.2, 9219 for 9.1) now coexist on
      r740a. The inner suite ran green against 9.1.1
      (`connected to PVE     9.1.1`): all six read tests, placement (negative
      AND positive — the LRM-lag retry fired and settled again), and the live
      RFB greeting. No SDK or mockpve divergence surfaced. Donald recorded the
      run (`PVE_RECORD=1`): seven cassettes re-recorded against 9.1.1,
      leak-reviewed (first fully-automated scrub — the Request.Host fix held
      with zero hand-edits), `just test-replay` green on the mixed corpus, and
      `certification.yaml` gained the 9.1.1 batch entry._
- [x] `proxmox/integration/testdata/cassettes/certification.yaml` (design OQ-8
      schema: `pve_version`, `recorded`, `commit`, `harness`, `cassettes`,
      `notes`): first entry for the 9.2 batch, one entry per matrix run
      thereafter; mock divergences reconciled (fixed in mockpve or recorded in
      `notes`) before an entry lands — _2026-07-11: file created with the first
      entry describing the REAL existing batch: the ten committed cassettes
      recorded live on r740a (9.2-1, commit c36b7da, 2026-07-06, harness
      `branch` — pre-pvelab). All known divergences are reconciled and named in
      `notes` (SDK upload 501/400 fixes, recorder no-replayable-interactions,
      mockpve memory-as-string + create-form persistence). The cassette dir's
      `.gitignore` gains `!certification.yaml` — it is hand-maintained data, not
      a recording. Entries accrue per matrix run; the placement cassette joins
      the batch after the first full dogfood run (Phase 3, live-pending)._ —
      _2026-07-12: the placement cassette landed as its OWN batch entry (not
      appended to the r740a batch): pve_version 9.2.2, harness `pvelab`, with
      the run's fidelity findings named in `notes` (VNC ticket path-binding, HA
      rule update required-props, HA-active feasibility counting, join quorum
      gate)._
- [x] Runbook: `pve-schemadiff` drift → dogfood run → refresh recordings →
      re-certify (a TESTING.md section or docs page) — _2026-07-11: TESTING.md
      "Certification: drift → dogfood → refresh → re-certify" section (under
      Recording cassettes): schemadiff trip → `nested.pve_version` bump +
      dogfood run → stale-cassette re-record/review/force-add → reconcile
      mockpve + append the batch entry to `certification.yaml`, with
      `just test-replay` as the regression guard._
- [x] Conclude INV-0001 + INV-0002 (→ Concluded, final findings); DESIGN-0002 →
      Implemented; this IMPL → Completed — _2026-07-13: done, with every
      predecessor gate closed first. INV-0001 concluded with hardware-validated
      answers to all three sub-questions (the Terraform Phase 1 was deliberately
      skipped — recorded there); INV-0002 concluded with the steady-state tail
      done (ship+pin v0.6.0, two-minor clone matrix, three-batch certification);
      DESIGN-0002 → Implemented (it IS the settled methodology doc); this IMPL →
      Completed. **Every task in every phase of this ledger is checked, and
      every phase's Success Criteria — including all (live) ones — carries dated
      pass evidence.**_

#### Success Criteria

- A dogfood run against a **second PVE minor** completes via linked clones,
  measurably faster than the ISO-install path. **(live)** — _2026-07-13: MET —
  PVE 9.1.1 via linked clones in 3m11s vs the 4m39–41s ISO baselines (~32%
  faster), full inner-suite pass included._
- `certification.yaml` exists with at least one entry per tested PVE version,
  and `mockpve`'s behaviour has been reconciled against those recordings. —
  _2026-07-13: MET — three batch entries (9.2-1 r740a, 9.2.2 pvelab, 9.1.1
  pvelab matrix); every divergence found across them is fixed in the SDK/mock
  and named in `notes`; the 9.1.1 batch surfaced none._

---

## File Changes

| File                                                              | Action | Phase | Description                                          |
| ----------------------------------------------------------------- | ------ | ----- | ---------------------------------------------------- |
| `hack/pvelab-spike/`                                              | Create | 0     | committed throwaway spike driver (IQ-5 = b)          |
| `cmd/pvelab/main.go`                                              | Create | 1     | subcommand dispatch, slog, buildinfo version         |
| `cmd/pvelab/lab/{config,iso,answers,provision,teardown,state}.go` | Create | 1     | importable harness logic + tests                     |
| `cmd/pvelab/lab/answer.toml.tmpl`                                 | Create | 1     | embedded answer-file template (IQ-2)                 |
| `pvelab.example.yaml` + `.gitignore` entries                      | Create | 1     | committed schema example; real files git-ignored     |
| `justfile` (`dogfood-*` recipes)                                  | Modify | 1–4   | iso/up/down in P1, test + composite in P3, pin in P4 |
| `proxmox/cluster/config.go`                                       | Create | 2     | `CreateCluster` / `JoinInfo` / `JoinCluster`         |
| `proxmox/mockpve/cluster.go` (or extend existing)                 | Modify | 2     | cluster-config emulation                             |
| `cmd/pvelab/lab/cluster.go`                                       | Create | 2     | create + serialized joins + quorum poll              |
| `proxmox/integration/harness_test.go`                             | Modify | 3     | `PVE_USERNAME`/`PVE_PASSWORD` creds; gate consts     |
| `proxmox/integration/recorder_test.go`                            | Modify | 3     | multi-pair `topologyScrub`                           |
| `proxmox/integration/ha_test.go`                                  | Modify | 3     | `TestResourceAffinityPlacement`; old rule test gone  |
| `proxmox/integration/console_test.go`                             | Modify | 3     | `TestConsoleRFB`                                     |
| `testdata/cassettes/TestResourceAffinityPlacement.yaml`           | Create | 3     | the P4 cassette (live-recorded, scrubbed)            |
| `testdata/cassettes/certification.yaml`                           | Create | 5     | per-version certification record                     |
| `TESTING.md` / `CLAUDE.md` / `README.md`                          | Modify | 1–5   | dogfood docs, binary-statement amendments            |
| `docs/impl/0001-proxmox-ve-9x-sdk-coverage.md`                    | Modify | 3     | Outstanding-live-verification boxes checked, dated   |

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
