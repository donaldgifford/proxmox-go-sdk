---
id: DESIGN-0002
title:
  "Dogfood harness: pvelab CLI, nested cluster provisioning, and recording
  pipeline"
status: Implemented
author: Donald Gifford
created: 2026-07-09
---

<!-- markdownlint-disable-file MD025 MD041 -->

# DESIGN 0002: Dogfood harness: pvelab CLI, nested cluster provisioning, and recording pipeline

**Status:** Implemented **Author:** Donald Gifford **Date:** 2026-07-09
(implemented 2026-07-13 — IMPL-0002 Completed; every phase live-verified)

<!--toc:start-->

- [Overview](#overview)
- [Goals and Non-Goals](#goals-and-non-goals)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
- [Background](#background)
- [Detailed Design](#detailed-design)
  - [Component map](#component-map)
  - [The pvelab CLI](#the-pvelab-cli)
  - [YAML configuration](#yaml-configuration)
  - [State and environment handoff](#state-and-environment-handoff)
  - [New SDK surface: cluster create/join](#new-sdk-surface-cluster-createjoin)
  - [ISO preparation (Stage 0)](#iso-preparation-stage-0)
  - [Readiness, retries, and the join restart](#readiness-retries-and-the-join-restart)
  - [Inner suite changes: password creds, P4 placement, P6 RFB](#inner-suite-changes-password-creds-p4-placement-p6-rfb)
  - [Recording, scrubbing, and why P6 has no cassette](#recording-scrubbing-and-why-p6-has-no-cassette)
  - [Failure and abort semantics](#failure-and-abort-semantics)
  - [mockpve certification](#mockpve-certification)
  - [Multi-version evolution (templates)](#multi-version-evolution-templates)
- [Implementation Phases](#implementation-phases)
  - [Phase 0: Substrate check + naive single-node spike](#phase-0-substrate-check--naive-single-node-spike)
  - [Phase 1: pvelab CLI skeleton — up/down, no cluster](#phase-1-pvelab-cli-skeleton--updown-no-cluster)
  - [Phase 2: Cluster surface + formation](#phase-2-cluster-surface--formation)
  - [Phase 3: Inner suite — P4 placement + P6 RFB, recordings](#phase-3-inner-suite--p4-placement--p6-rfb-recordings)
  - [Phase 4: Ship + pin — the steady state](#phase-4-ship--pin--the-steady-state)
  - [Phase 5: Evolution — templates, version matrix, certification](#phase-5-evolution--templates-version-matrix-certification)
- [Testing Strategy](#testing-strategy)
- [Open Questions](#open-questions)
- [References](#references)
<!--toc:end-->

## Overview

Implements INV-0002's dogfood harness: a `pvelab` CLI (name settled, OQ-1) that
uses the SDK to provision an ephemeral **3-node nested PVE cluster** on the
physical node `r740a`, a `just` target that runs the current branch's
integration suite against that cluster, and a teardown that leaves the host
clean. The harness closes the last two IMPL-0001 live criteria (P4
resource-affinity placement, P6 VNC/RFB payload), feeds the recorded-cassette
pipeline, and evolves into on-demand, multi-version, clean-room testing that
certifies `mockpve` per PVE version.

## Goals and Non-Goals

### Goals

- One-command lifecycle: `just dogfood` = CLI `up` → branch integration tests
  (with recording) → CLI `down`.
- **Released code provisions; branch code is tested.** The CLI runs from the
  most recent stable tag (`go run <module>/cmd/pvelab@vX.Y.Z`) once the first
  tag shipping it exists; during Phase 0/buildout it runs from the branch by
  design (INV-0002's accepted chicken-and-egg).
- Close P4 (with committed, CI-replayable cassettes) and P6 (live RFB assertion)
  — then keep the harness as the standing tool for refreshing recordings when
  the PVE API changes.
- New honest SDK surface for cluster formation
  (`cluster.CreateCluster`/`JoinInfo`/`JoinCluster`), unit-tested against
  mockpve like every other service op.
- Deterministic cleanliness: repeated `up`/`down` cycles leave r740a exactly as
  found.

### Non-Goals

- **Not CI-per-PR.** Dogfood runs are on-demand (PVE API drift, new PVE feature,
  new SDK surface). Per-PR coverage remains `just test` + `just test-replay`.
- **Not a general-purpose PVE provisioner.** pvelab provisions exactly the
  topology this repo's testing needs; it is a test tool, not a product.
- **Not HA failover testing.** P4 asserts placement (rules honored with all
  nodes healthy), not fencing/failover behaviour.
- **No secrets in YAML or state files.** Credentials stay in env (`op run`
  compatible), matching the existing harness convention.

## Background

Decided in INV-0002 (reviewed 2026-07-09): 3 nested nodes (PVE's documented
quorum minimum; local infra confirmed capable), stable-CLI/branch-tests split,
YAML config + `just` orchestration, on-demand cadence feeding mockpve
certification, and a hard execution gate (no implementation without explicit
approval). Verified inputs from the INV's research: cluster create/join are REST
(`POST /cluster/config`, `GET`/`POST /cluster/config/join`, upstream
`apidoc.js`); unattended install via `proxmox-auto-install-assistant` answer
files (static IP, root password, first-boot hook, 8.3-1+); a 3-node cluster with
softdog satisfies the HA stack; the RFB greeting (`"RFB 003.008\n"`, RFC 6143
§7.1.1) is the 12-byte P6 assertion; password credentials
(`api.UserCredentials`) avoid the join-wipes-tokens problem entirely.

Existing plumbing this design builds on: the integration suite +
`recorder_test.go` go-vcr harness (record/replay, redaction, `topologyScrub`),
`just test-replay` CI job, `cmd/pve-schemadiff` (drift trigger), and the
`cmd/pve-schemadiff/schema` precedent of an importable, unit-testable package
under `cmd/`.

## Detailed Design

### Component map

```text
justfile
  dogfood            composite: dogfood-up → dogfood-test → dogfood-down
  dogfood-up         go run ./cmd/pvelab up   --config pvelab.yaml   (Phase 0–3: branch)
  dogfood-test       go test -tags=integration ./proxmox/integration/ (branch, PVE_RECORD=1)
  dogfood-down       go run ./cmd/pvelab down --config pvelab.yaml
  dogfood-iso        go run ./cmd/pvelab iso  --config pvelab.yaml

cmd/pvelab/
  main.go            flag parsing + subcommand dispatch (stdlib flag, like cmd/mockpve)
  lab/               importable, unit-testable logic (cmd/pve-schemadiff/schema precedent)
    config.go        YAML schema, load + validate (settings only; secrets via env)
    iso.go           render per-node answer.toml; drive the assistant on r740a via ssh
    provision.go     ensure prepared ISOs exist, create/start node VMs, readiness polling
    cluster.go       create + serialized joins via the new SDK cluster surface
    teardown.go      stop/delete VMs (+ optional ISO removal), bounded contexts
    state.go         .pvelab-state.json + .pvelab.env emission

proxmox/cluster/     NEW: config.go — CreateCluster / JoinInfo / JoinCluster
proxmox/mockpve/     NEW: cluster-config handlers (create/join emulation)
proxmox/integration/ password-cred env support; P4 placement test; P6 RFB test;
                     multi-host topologyScrub
```

`cmd/pvelab` depends only on the public `proxmox/...` packages (the Go
`internal` rule already prevents anything else — same constraint as
`cmd/mockpve`).

### The `pvelab` CLI

Subcommands (stdlib `flag`, one `FlagSet` per subcommand — repo precedent, no
new CLI dependency; OQ-3 allows switching to cobra later if the surface earns
it):

| Command  | Does                                                                            |
| -------- | ------------------------------------------------------------------------------- |
| `iso`    | render per-node `answer.toml` + run the install assistant **on r740a over SSH** |
| `up`     | ISOs present? → create node VMs → start → wait ready → cluster → emit env/state |
| `down`   | stop + delete node VMs (`--force` skips missing), optionally `--purge-isos`     |
| `status` | read state file, poll each node + cluster quorum, print a table                 |
| `env`    | print `export PVE_*=…` lines for the inner suite (also written by `up`)         |

Version pinning: the `just` recipes call `go run ./cmd/pvelab` during buildout
(Phase 0–3) and switch to `go run <module>/cmd/pvelab@<stable>` in Phase 4.
`down` must remain compatible with state files written by the same minor — state
schema carries a `version` field from day one.

### YAML configuration

Settings only; anything secret is an **env var name**, not a value. Default path
`pvelab.yaml` at the repo root; a committed `pvelab.example.yaml` documents the
schema (real file git-ignored, OQ-4).

```yaml
outer:
  endpoint: https://r740a.example:8006
  token_id_env: PVE_TOKEN_ID # env var NAMES, not values
  token_secret_env: PVE_TOKEN_SECRET
  insecure_tls: true
  storage: local-zfs # node VM disks
  iso_storage: local # prepared installer ISOs
  bridge: vmbr0
  ssh: # for `pvelab iso` (proxmox/ssh side-channel)
    user: root
    known_hosts: ~/.ssh/known_hosts # host-key verification is mandatory
nested:
  pve_version: "9.2" # selects ISO (later: template)
  base_iso: /var/lib/vz/template/iso/proxmox-ve_9.2-1.iso # already on r740a
  cluster_name: dogfood
  root_password_env: PVELAB_ROOT_PW # answer-file password, supplied via env
  gateway: 10.0.0.1
  dns: 10.0.0.1
  cores: 4
  memory_mb: 8192
  disk_gb: 32
  nodes:
    - { name: pve1, vmid: 9201, cidr: 10.0.0.201/24 }
    - { name: pve2, vmid: 9202, cidr: 10.0.0.202/24 }
    - { name: pve3, vmid: 9203, cidr: 10.0.0.203/24 }
```

Validation is strict and fail-fast: exactly 3+ nodes, unique VMIDs/names/IPs,
referenced env vars present, VMIDs not already in use on the outer node (checked
at `up`).

### State and environment handoff

`up` writes two git-ignored files:

- **`.pvelab-state.json`** — what was created (VMIDs, ISO volids, node IPs,
  schema version, timestamps). `down` and `status` read it; `down` also works
  from config alone (`--no-state`) so a lost state file never strands VMs.
- **`.pvelab.env`** — the inner suite's environment:
  `PVE_ENDPOINT=https://<pve1-ip>:8006`, `PVE_USERNAME=root@pam`, `PVE_PASSWORD`
  (resolved from `root_password_env`), `PVE_INSECURE_TLS=1`, `PVE_NODE=pve1`,
  plus the `PVE_TEST_*` gates the P4/P6 tests need. The `dogfood-test` recipe
  sources it.

### New SDK surface: cluster create/join

`proxmox/cluster/config.go`, following the established service pattern (pointer
specs, `svcutil` helpers, mockpve handlers, unit tests):

```go
type ClusterCreateSpec struct { Name string /* + Links, NodeID via Extra */ }
type JoinInfo struct { /* lossless: Fingerprint, Nodelist, PreferredNode, Extra */ }
type JoinSpec struct { Hostname, Password, Fingerprint string /* + Links, Force */ }

func (s *Service) CreateCluster(ctx context.Context, spec *ClusterCreateSpec) error
func (s *Service) JoinInfo(ctx context.Context) (*JoinInfo, error)
func (s *Service) JoinCluster(ctx context.Context, spec *JoinSpec) error
```

**REST-with-caveat:** the upstream schema confirms paths/params but the **return
shapes are unverified** (a UPID may or may not come back; join restarts the
responding daemons so any returned task is unreliable anyway). Design decision:
treat both writes as **fire-and-poll** — ignore the response body beyond error
status, then poll `GET /cluster/config/nodes` (create) / cluster membership
(join) for convergence. Verify actual return shapes live in Phase 2 and tighten
if warranted. `JoinCluster` is documented as **only meaningful against a fresh
node** (it wipes the node's local pmxcfs config — users/tokens do not survive;
the reason the harness uses password creds).

mockpve gains cluster-config emulation: `create` marks the mock clustered,
`join` requires the fingerprint from the mock's `join_info`, membership shows up
in `/cluster/config/nodes` — enough for unit tests + CLI `lab` tests.

### ISO preparation (Stage 0)

**Built into the CLI, executed on r740a over SSH** (OQ-5 = b): r740a already
carries `proxmox-auto-install-assistant`, `xorriso`, and the base PVE ISO, so
there is no container and no 1.5 GB upload from the dev box — preparation
happens where the artifacts live. `pvelab iso`:

1. renders one `answer.toml` per node from the YAML (hostname, static
   IP/gateway/DNS, root password from env, filesystem ext4/LVM defaults),
2. connects via the SDK's **`proxmox/ssh` side-channel** (`Client.SSH(…)`,
   host-key verification via `outer.ssh.known_hosts` — mandatory, the package
   refuses without it), SFTPs the answer files to a temp dir,
3. runs
   `proxmox-auto-install-assistant prepare-iso <base_iso> --fetch-from iso --answer-file …`
   per node via `Exec`, writing `pve-<version>-<node>.iso` into the
   `iso_storage` template dir, and
4. verifies the resulting volids are visible via `Storage().ListContent`.

`pvelab up` then only checks the prepared volids exist (erroring with a "run
`pvelab iso`" hint if not). This gives `proxmox/ssh` — in-process-tested only
today — its **first live exercise**, ahead of any cluster-formation fallback
need. The answer files enable the first-boot hook only when a need appears (none
for Phase 0–3). Phase 0 proves the exact assistant commands manually over SSH
before `lab/iso.go` automates them.

**Amended 2026-07-10 (Donald, during Phase 0): the CLI uses `--fetch-from http`
with an embedded answer server, not per-node baked ISOs.** Two Phase 0 realities
drove this: the assistant/xorriso/base-ISO were _not_ pre-installed on r740a
(the OQ-5 premise was stale; `pvelab iso` must apt-install them or error with
instructions), and baking answers means 3×1.6 GiB prepared ISOs that all need
re-prepping on any IP/password change. Instead: `pvelab iso` prepares **one**
ISO per PVE version (`--fetch-from http`, answer URL baked); `pvelab up` runs a
small embedded HTTP answer server for the duration of the installs, rendering
each node's `answer.toml` on demand and matching requests by the
`smbios1: serial=<node>` stamped into each VM at create (the installer POSTs its
system identity — MACs + DMI — to the answer URL). This also scales the Phase 5
matrix to one ISO per version instead of nodes×versions. **REST-with-caveat
pieces to verify live in Phase 1:** the exact POST payload shape (DMI serial
field name), whether plain HTTP is accepted or HTTPS + a pinned
`--cert-fingerprint` is required (if so, pvelab keeps a persistent self-signed
cert in its state dir), and nested-VM → workstation reachability on the lab LAN.
The Phase 0 spike deliberately used the baked single-node ISO
(`--fetch-from iso`) — that mode remains the documented fallback if the
answer-server path proves unreliable.

### Readiness, retries, and the join restart

- **Install readiness:** after `Start`, poll each node's
  `GET /api2/json/version` with a fresh SDK client (`api.UserCredentials`,
  `WithInsecureSkipVerify` — fresh installs are self-signed) every 15 s with a
  25-minute ceiling (desk estimate; Phase 0 measures reality and tightens).
- **Join churn:** joins run **serialized** (corosync membership changes must not
  race). Each `JoinCluster` call tolerates the connection dropping mid-request
  (the joining node restarts `pveproxy`) — the error is swallowed and
  convergence is decided by polling node 1's `GET /cluster/config/nodes` until
  the joined node appears (bounded, ~3 min per join). TLS: nested nodes stay
  `InsecureSkipVerify` for the harness's lifetime (their certs churn at join;
  pinning is pointless for a throwaway cluster).
- **Quorum readiness:** after both joins, poll `GET /cluster/status` until 3
  nodes report online + quorate before declaring `up` complete.

### Inner suite changes: password creds, P4 placement, P6 RFB

- **Password creds in the harness:** `newClient` learns
  `PVE_USERNAME`/`PVE_PASSWORD` (used when `PVE_TOKEN_*` are absent) →
  `api.UserCredentials(user, password, "")`. This is also the first live
  exercise of the user/pass mint+refresh path (mock-only today).
- **P4 — `TestResourceAffinityPlacement`** (new, self-provisioning like
  `TestConsoleMint`): gated on its own `PVE_TEST_*` vars; creates two **diskless
  dummy VMs** (no disk, minimal RAM — a running QEMU with no bootable device
  still counts as started, zero storage dependency); registers both as HA
  resources; creates a **negative** resource-affinity rule; polls
  `Cluster().ListResources` until they sit on different nodes (~5 min budget);
  then flips to the **positive** variant and waits for co-location; cleans up
  rule → resources → VMs with `cleanupCtx`. The existing
  `TestResourceAffinityRule` (define + read-back, `PVE_TEST_HA_SIDS`) is
  subsumed and removed (OQ-9).
- **P6 — `TestConsoleRFB`** (new): scratch VM via the console-mint pattern,
  `MintVNCTicket` → `console.Connect` → read exactly 12 bytes → assert prefix
  `"RFB "` + version `003.00x\n`. Live-only (below); skipped under
  `PVE_REPLAY=1`.

### Recording, scrubbing, and why P6 has no cassette

P4 runs under `PVE_RECORD=1`; every placement poll is its own interaction (the
`WithReplayableInteractions` lesson — never set it), so the cassette replays
deterministically. `topologyScrub` is extended to a list of host→placeholder and
name→placeholder pairs (outer endpoint + three nested IPs; nested hostnames
`pve1/2/3` and cluster name `dogfood` are chosen placeholder-safe up front so
only IPs need rewriting). The committed P4 cassette joins `just test-replay`,
which is what finally checks the IMPL-0001 P4 box with a regression guard rather
than a one-off observation.

**P6 cannot be a cassette.** go-vcr records `http.RoundTripper` round trips;
`DoWebSocket` is a 101 upgrade whose response body becomes a hijacked duplex
stream — a recorder in the chain either breaks the upgrade or cannot capture the
stream. Decision: P6 is asserted **live on every dogfood run** and recorded as a
dated note (not a cassette) when checked off in IMPL-0001. No custom websocket
recorder is built (OQ-6).

### Failure and abort semantics

`up` is **fail-fast, leave-for-debug**: on any stage failure it stops, prints
what exists (state file is already on disk), and exits non-zero. It does **not**
auto-rollback — a half-built environment is evidence. Recovery is always
`pvelab down --force` (idempotent: missing VMs are skipped, state + config are
both consulted). `down` uses bounded per-op contexts (the `cleanupCtx` pattern)
so a wedged task fails the command rather than hanging it. Repeated `up` with
leftovers present fails validation ("VMID 9201 already exists on r740a") rather
than adopting them — recovery is `down --force` then `up` (OQ-7).

### mockpve certification

Each dogfood run's scrubbed cassettes are ground truth for a specific PVE
version. Certification is **machine-readable** (OQ-8 = b): a committed
`proxmox/integration/testdata/cassettes/certification.yaml`, one entry per
recording batch:

```yaml
- pve_version: 9.2-1
  recorded: 2026-07-09 # date of the dogfood run
  commit: abc1234 # commit that introduced/refreshed the batch
  harness: pvelab@v0.2.0 # or "branch" during buildout
  cassettes: [TestResourceAffinityPlacement, TestVersionRoundTrip, …]
  notes: "" # divergences found + fixed in mockpve, if any
```

Being data, tooling can consume it later (a `just` check that mockpve's
certified version is stale vs the schemadiff baseline, a badge, etc.);
human-readable views (mockpve godoc line, cassette README) can be derived from
it if wanted, but the YAML is the record. Divergences found while comparing
mockpve behaviour to fresh cassettes are fixed in mockpve (or documented in
`notes`) before the entry is updated — an entry means "the mock's envelope
matches what PVE X.Y-Z actually returned for the recorded surface".

### Multi-version evolution (templates)

Phase 5 replaces per-run installs with **template-per-minor**:
`pvelab template build` installs a version once and converts it to an outer-node
template; `up` then **linked-clones** 3 nodes from it (fast, storage-cheap) —
`qemu.Clone` is live-verified. `nested.pve_version` selects the template; the
9.0/9.1/9.2 matrix makes `version.Capabilities` gates testable against real
minors, and each version's run refreshes that version's certification. Template
rebuilds on PVE point releases are a manual chore triggered by `pve-schemadiff`
drift (the on-demand signal), not a scheduled job.

## Implementation Phases

> Coverage legend matches IMPL-0001: `[ ]` not started · `[~]` partial · `[x]`
> done. **Execution gate (INV-0002): no phase starts without explicit
> approval.**

### Phase 0: Substrate check + naive single-node spike

Manual/script-assisted; produces measurements. The throwaway driver is committed
under `hack/pvelab-spike/` for the record (IMPL-0002 IQ-5 = b — amending this
phase's original "no committed harness code" stance); it is spike evidence, not
harness code, and Phase 1's CLI supersedes it.

- [ ] Verify nested virt on r740a (`/sys/module/kvm_intel/parameters/nested` →
      `Y`) and headroom for 3× 8 GiB VMs; confirm the assistant + `xorriso` +
      base 9.2 ISO versions already on the node
- [ ] Write the first `answer.toml` (pve1: static IP, root password via env,
      ext4/LVM), copy it to r740a, and run
      `proxmox-auto-install-assistant prepare-iso` there **manually over SSH**
      against the on-node base ISO — proving the exact commands `lab/iso.go`
      will automate
- [ ] Create/start the pve1 VM from the prepared ISO **via the SDK** (throwaway
      script; CPU `host`, 4 vCPU, 8 GiB, 32 GiB disk, vmbr0)
- [ ] Time the unattended install; confirm `GET /version` answers with
      `api.UserCredentials` + insecure TLS
- [ ] Tear down by hand via the SDK; confirm r740a is clean (no VM; ISO
      optionally kept)
- [ ] Record timings + gotchas in INV-0002's Findings (replace the desk
      estimates)

**Success criteria:** one nested PVE node installs unattended from a prepared
ISO inside a VM on r740a, answers the API with password creds, and tears down
cleanly — with measured install wall-clock recorded in INV-0002.

### Phase 1: pvelab CLI skeleton — up/down, no cluster

- [ ] `cmd/pvelab` skeleton: stdlib-flag subcommand dispatch (`iso`, `up`,
      `down`, `status`, `env`), version stamp, `slog` logger to stderr
- [ ] `lab/config.go`: YAML schema + strict validation (node count/uniqueness,
      env-var presence, VMID-collision check against the outer node)
- [ ] `lab/iso.go`: render per-node `answer.toml` from config; SFTP + `Exec` the
      assistant on r740a via `proxmox/ssh` (known-hosts from `outer.ssh`);
      verify prepared volids via `ListContent` — first live use of the ssh
      side-channel
- [ ] `lab/provision.go`: prepared-ISO presence check (point at `pvelab iso` on
      miss), node-VM create (CPU `host`), start, per-node `/version` readiness
      poll with bounded ceiling
- [ ] `lab/teardown.go`: stop+delete with bounded contexts; `--force`
      idempotency; `--purge-isos`
- [ ] `lab/state.go`: `.pvelab-state.json` (schema-versioned) + `.pvelab.env`
      emission; `down --no-state` path from config alone
- [ ] Unit tests: `lab` package against `mockpve` (config validation,
      provision/teardown call sequences, state round-trip) + `lab/iso.go`
      against the ssh package's in-process SSH/SFTP test server
- [ ] `justfile`: `dogfood-up` / `dogfood-down` / `dogfood-iso` recipes
      (branch-run `go run ./cmd/pvelab`); `.gitignore` entries for `pvelab.yaml`
      / state / env (+ committed `pvelab.example.yaml`)
- [ ] Docs: CLAUDE.md layout note; amend the two "mockpve is the only binary"
      statements (CLAUDE.md, README) to name pvelab as a dev tool
- [ ] `just lint` + `just test` green; changelog regenerated

**Success criteria:** `just dogfood-up` provisions 3 booted, API-answering
nested nodes on r740a and `just dogfood-down` removes them completely —
repeatable back-to-back without manual cleanup (live-verified); `lab` logic is
unit-tested against mockpve in default CI.

### Phase 2: Cluster surface + formation

- [ ] SDK: `cluster.CreateCluster` / `JoinInfo` / `JoinCluster` +
      `ClusterCreateSpec`/`JoinInfo`/`JoinSpec` types (lossless reads,
      fire-and-poll writes, docs noting the fresh-node-only join semantics)
- [ ] mockpve: cluster-config handlers (create / join_info / join + membership
      in `/cluster/config/nodes`); seeders as needed
- [ ] Unit tests for the new surface (happy path, bad fingerprint, double
      create)
- [ ] `lab/cluster.go`: create on pve1 → `JoinInfo` → serialized joins for
      pve2/pve3 with restart-tolerant convergence polling → quorate check via
      `GET /cluster/status`
- [ ] Live verification on the nested nodes: REST create/join works end-to-end;
      record the actual return shapes (UPID or null) and tighten the SDK
      docs/impl accordingly
- [ ] Fallback: `ssh.Exec pvecm` path behind a config flag **only if** REST
      proves unreliable live (else a doc note explaining why it's absent)
- [ ] `just lint` + `just test` green; changelog regenerated

**Success criteria:** `just dogfood-up` ends with a **3-node quorate cluster**
(`/cluster/status`: 3 online, quorate) reproducibly from scratch
(live-verified); the new cluster surface is mock-tested in default CI.

### Phase 3: Inner suite — P4 placement + P6 RFB, recordings

- [ ] Harness: `PVE_USERNAME`/`PVE_PASSWORD` support in `newClient`
      (`api.UserCredentials`), documented in TESTING.md's env table
- [ ] `topologyScrub` → multi-pair (outer endpoint + N nested IPs/hostnames)
- [ ] `TestResourceAffinityPlacement`: diskless dummy VMs → HA resources →
      negative rule → placement poll → positive variant → full cleanup; replaces
      `TestResourceAffinityRule` (OQ-9)
- [ ] `TestConsoleRFB`: scratch VM → mint → `Connect` → assert the 12-byte RFB
      greeting; skipped under `PVE_REPLAY=1`
- [ ] `justfile`: `dogfood-test` (sources `.pvelab.env`, `PVE_RECORD=1`, runs
      the targeted tests) + composite `dogfood` (up → test → down)
- [ ] Run it: capture the P4 cassette (+ re-record any suite cassettes worth
      refreshing against the nested cluster), review + scrub, commit
- [ ] Wire the P4 cassette into `just test-replay` / the CI replay job
- [ ] Check **both** IMPL-0001 "Outstanding live verification" boxes with dated
      verification notes (P4: cassette + CI replay; P6: live RFB assertion,
      dated — no cassette by design); update INV-0002 Findings/Conclusion
- [ ] TESTING.md: dogfood section (prereqs, YAML, `just dogfood`, recording
      flow); CLAUDE.md testing-reality update
- [ ] `just lint` + `just test` + `just test-replay` green; changelog
      regenerated

**Success criteria:** IMPL-0001's P4 and P6 boxes are checked from a single
`just dogfood` run: the negative/positive affinity placements are observed on
the nested cluster with the P4 cassette replaying in CI, and the RFB greeting is
read over `console.Connect` from a real QEMU VNC server.

### Phase 4: Ship + pin — the steady state

- [ ] Apply OQ-2's resolution (a: `go run`-only dev tool — no goreleaser
      artifact; mockpve stays the only shipped binary)
- [ ] Land pvelab + the cluster surface in a stable tag (target `v0.2.0`)
- [ ] Switch the `just` dogfood recipes to `go run <module>/cmd/pvelab@<stable>`
      (branch-run stays available behind a `PVELAB_DEV=1` escape hatch)
- [ ] Post-tag smoke: from a clean checkout, `just dogfood` end-to-end with the
      pinned CLI
- [ ] Final doc sweep: README / CLAUDE.md / TESTING.md consistent about the
      CLI's status; changelog regenerated

**Success criteria:** from a clean checkout with only `pvelab.yaml` + env
configured, `just dogfood` runs the **stable-pinned** CLI end-to-end green —
released code provisions, branch code is tested.

### Phase 5: Evolution — templates, version matrix, certification

- [ ] `pvelab template build`: unattended install once → convert to an
      outer-node template (per `nested.pve_version`)
- [ ] `up` via **linked clones** when a template exists (fall back to ISO
      install when not); measure the speedup
- [ ] Version matrix: prepared ISOs/templates for the supported minors
      (9.0/9.1/9.2); config selects; capability-gate tests exercised against a
      real non-9.2 minor at least once
- [ ] mockpve certification records (OQ-8 = b): `certification.yaml` beside the
      cassettes, one entry per recording batch; reconcile any mock divergences
      found against fresh cassettes before entering them
- [ ] Runbook note: `pve-schemadiff` drift → dogfood run → refresh recordings →
      re-certify (TESTING.md or a docs/ page)
- [ ] Conclude INV-0001 + INV-0002 (status → Concluded, final findings); promote
      this DESIGN's status to Implemented

**Success criteria:** a dogfood run against a **second PVE minor** completes via
linked clones measurably faster than ISO installs, and `mockpve` carries a
current certification stamp naming the PVE version(s) its behaviour was verified
against.

## Testing Strategy

- **Unit (default CI):** the `lab` package + new `cluster` surface against
  mockpve; config-validation table tests; state-file round-trips. No live
  dependency.
- **Replay (default CI):** the P4 cassette joins `just test-replay`; P6 is
  explicitly excluded (no cassette possible).
- **Live (on-demand):** `just dogfood` is itself the live test — its success
  criteria are the phase gates above. Destructive scope is confined to
  config-declared VMIDs/ISOs on r740a, and every created object is torn down by
  `down`.

## Open Questions

**All resolved by Donald, 2026-07-09** (answers:
`1a 2a 3a* 4a 5b* 6a 7a 8b 9a 10a`). The chosen letter is marked on each
heading; the lettered options are kept as the record. The design body reflects
every resolution.

**OQ-1 — CLI name — RESOLVED (a: `pvelab`).** Used throughout as `cmd/pvelab`,
`pvelab.yaml`, `just dogfood-*`.

- **a. `pvelab` (recommended)** — one word like `mockpve`; says what it is (a
  PVE lab).
- b. `pve-lab` — hyphenated, matching `cmd/pve-schemadiff`.
- c. `dogfood` — names the activity, not the tool.
- d. `labctl` — generic; loses the PVE association.

**OQ-2 — CLI release shape — RESOLVED (a: `go run`-only).** The "mockpve is the
only binary" statements get amended to "only _shipped_ binary".

- **a. `go run`-only dev tool (recommended)** — no goreleaser artifact; the
  `@stable` pin already works via the module proxy, and a test harness doesn't
  need archives/SBOM/signing upkeep. mockpve stays the only _shipped_ binary.
- b. goreleaser artifact — multi-arch archives next to mockpve; heavier release
  surface, useful only if pvelab should run where Go isn't installed.

**OQ-3 — CLI framework — RESOLVED (a\*: stdlib `flag`, with license to adopt
cobra later if the subcommand surface grows enough to earn the dependency).**

- **a. stdlib `flag`, one FlagSet per subcommand (recommended)** — repo
  precedent (`cmd/mockpve`, `cmd/pve-schemadiff`); zero new dependencies.
- b. `spf13/cobra` — nicer help/completion at the cost of a dependency tree on a
  library module.
- c. `urfave/cli` — middle ground, still a new dependency.

**OQ-4 — config + artifact hygiene — RESOLVED (a: ignore real files, commit
`pvelab.example.yaml`).** The real YAML holds lab IPs (settings-not-secrets, but
it is topology).

- **a. git-ignore `pvelab.yaml` / `.pvelab-state.json` / `.pvelab.env`; commit
  `pvelab.example.yaml` (recommended)** — same pattern as `.env.local` + the
  TESTING.md examples; topology never lands in a commit.
- b. commit a placeholder `pvelab.yaml` edited in place — one file, but every
  local edit is a dirty tracked file and a leak risk.
- c. config outside the repo (`~/.config/pvelab.yaml`) — nothing to ignore, but
  the repo no longer documents its own harness shape.

**OQ-5 — Stage 0 (ISO prep) home — RESOLVED (b\*: on r740a over SSH).** r740a
already carries the assistant packages **and** the base ISO, so the "mutates the
host" objection was moot — nothing to install, and prep happens where the
artifacts live. Follow-up decision (script vs CLI): **built into the CLI** as
`pvelab iso` over the `proxmox/ssh` side-channel — the answer files render from
the same YAML config, and the ssh package gets its first live exercise (see "ISO
preparation").

- a. local Docker recipe — throwaway Debian container running
  `proxmox-auto-install-assistant` + `xorriso`; hermetic, no state on r740a.
- **b. run the assistant on r740a over SSH (chosen)** — no container needed; the
  packages + base ISO are already there.
- c. both — Docker default with an SSH fallback flag; more surface to maintain.

**OQ-6 — P6 stays cassette-less — RESOLVED (a: live-only assertion).** go-vcr
cannot capture a 101-upgrade duplex stream, so the RFB assertion cannot replay
in CI.

- **a. accept live-only P6 (recommended)** — asserted on every dogfood run,
  skipped under `PVE_REPLAY=1`; checked off in IMPL-0001 with a dated note.
- b. build a custom websocket recorder (capture RFB bytes to a fixture, replay
  through a fake conn) — real work for 12 bytes of coverage.

**OQ-7 — leftover/failed-`up` semantics — RESOLVED (a: fail-fast,
leave-for-debug, `down --force` to recover).**

- **a. fail-fast, leave-for-debug; recovery is `down --force` (recommended)** —
  a half-built environment is evidence; `up` never adopts existing
  config-declared VMIDs.
- b. auto-adopt matching leftovers (resume semantics) — convenient but can
  silently continue from a corrupt half-state.
- c. auto-rollback on failed `up` — clean but destroys the evidence needed to
  debug the failure.

**OQ-8 — mockpve certification record format — RESOLVED (b:
`certification.yaml`).** Machine-readable from day one; human-readable views can
be derived from it (see "mockpve certification" for the schema).

- a. human-readable dual stamp — a "Certified against" line in `mockpve/doc.go`
  (shows up in godoc) + a batch table row in the cassette README.
- **b. machine-readable `certification.yaml` next to the cassettes (chosen)** —
  data tooling can consume later.
- c. both a + b from day one.

**OQ-9 — fate of the existing `TestResourceAffinityRule` — RESOLVED (a: retire
it).**

- **a. retire it (recommended)** — the new self-provisioning
  `TestResourceAffinityPlacement` subsumes define+read-back and drops the
  pre-existing `PVE_TEST_HA_SIDS` requirement.
- b. keep both — retains a cheap rules-CRUD check that works on a single node,
  at the cost of overlapping tests and one more env var.

**OQ-10 — VMID/IP reservations — RESOLVED (a: VMIDs 9201–9203).** The three
static lab IPs are supplied in the real (git-ignored) `pvelab.yaml` when Phase 0
runs; confirm they sit outside the DHCP pool.

- **a. VMIDs 9201–9203 + three static lab IPs you designate, recorded in
  `pvelab.example.yaml` (chosen)** — continues the 9xxx scratch convention
  (9101/9102 are the integration suite's).
- b. a different VMID range.

## References

- INV-0002 — Dogfood harness investigation (direction + research; the execution
  gate)
  (`docs/investigation/0002-dogfood-harness-sdk-provisioned-nested-pve-clusters-for-p4p6.md`)
- INV-0001 — Nested Proxmox nodes (carried-over desk findings)
- IMPL-0001 — Outstanding live verification (P4 + P6): the boxes Phase 3 closes
  (`docs/impl/0001-proxmox-ve-9x-sdk-coverage.md`)
- DESIGN-0001 — package layout / service-pattern contract the new `cluster`
  surface follows
- `TESTING.md` — recording/replay harness this design extends
- PVE API viewer schema (`apidoc.js`) — `/cluster/config*` endpoints
- PVE wiki — Automated Installation (`proxmox-auto-install-assistant`)
- RFC 6143 §7.1.1 — RFB ProtocolVersion handshake
