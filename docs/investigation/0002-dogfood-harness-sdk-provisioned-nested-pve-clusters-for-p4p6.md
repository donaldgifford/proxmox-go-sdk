---
id: INV-0002
title:
  "Dogfood harness: SDK-provisioned nested PVE clusters for P4/P6 live
  verification"
status: Open
author: Donald Gifford
created: 2026-07-08
---

<!-- markdownlint-disable-file MD025 MD041 -->

# INV 0002: Dogfood harness: SDK-provisioned nested PVE clusters for P4/P6 live verification

**Status:** Open **Author:** Donald Gifford **Date:** 2026-07-08 (review
feedback incorporated 2026-07-09)

<!--toc:start-->

- [Question](#question)
- [Hypothesis](#hypothesis)
- [Context](#context)
  - [Direction set at review (2026-07-09)](#direction-set-at-review-2026-07-09)
- [Approach](#approach)
  - [Phase 0 shape: stable CLI provisions, branch tests run](#phase-0-shape-stable-cli-provisions-branch-tests-run)
  - [Stage 0: ISO preparation (one-time per PVE version)](#stage-0-iso-preparation-one-time-per-pve-version)
  - [Stage 1: Provision (CLI, against r740a)](#stage-1-provision-cli-against-r740a)
  - [Stage 2: Cluster formation](#stage-2-cluster-formation)
  - [Stage 3: Inner suite — P4 with recording, then P6](#stage-3-inner-suite--p4-with-recording-then-p6)
  - [Stage 4: Teardown](#stage-4-teardown)
- [Environment](#environment)
- [Findings (desk + web research, 2026-07-08 — not yet validated on hardware)](#findings-desk--web-research-2026-07-08--not-yet-validated-on-hardware)
  - [Cluster create/join are real REST endpoints — new SDK surface](#cluster-createjoin-are-real-rest-endpoints--new-sdk-surface)
  - [Unattended PVE install is fully supported by upstream tooling](#unattended-pve-install-is-fully-supported-by-upstream-tooling)
  - [Three nodes match PVE's quorum guidance (decided)](#three-nodes-match-pves-quorum-guidance-decided)
  - [Password credentials are the right bootstrap — and close another gap](#password-credentials-are-the-right-bootstrap--and-close-another-gap)
  - [P6 needs only the RFB greeting](#p6-needs-only-the-rfb-greeting)
  - [What the harness stands on vs what it exercises for the first time](#what-the-harness-stands-on-vs-what-it-exercises-for-the-first-time)
  - [Recording cadence: on-demand runs certify mockpve per PVE version](#recording-cadence-on-demand-runs-certify-mockpve-per-pve-version)
  - [The multi-version clean-room evolution is a template problem](#the-multi-version-clean-room-evolution-is-a-template-problem)
- [Open questions](#open-questions)
- [Conclusion](#conclusion)
- [Recommendation](#recommendation)
- [References](#references)
<!--toc:end-->

## Question

Can a dogfood harness — **a CLI tool built from the most recent stable SDK
release**, orchestrated by a `just` target and configured by a YAML file — use
the SDK itself against the physical node `r740a` to:

1. provision **three nested PVE 9.x VMs** on r740a via the SDK's own
   live-verified ops (ISO upload, QEMU create/start/delete),
2. **cluster them** (REST if possible, SSH side-channel as fallback),
3. run the **current-branch** integration suite against that nested cluster —
   including the two outstanding live-only criteria, **P4** resource-affinity
   placement (capturing `go-vcr` cassettes for CI replay) and **P6** the VNC/RFB
   wire payload over `console.Connect`,
4. then **tear the cluster and VMs down** — leaving r740a clean,

and can that same methodology become the general testing model: **on-demand,
clean-room, multi-version PVE environments** whose recordings certify `mockpve`
against the PVE version they were captured from?

## Hypothesis

Yes. Every provisioning primitive the harness needs is already **live-verified
SDK surface** (ISO upload, QEMU lifecycle — P2/P3 cassettes exist), unattended
PVE installation is first-class upstream tooling
(`proxmox-auto-install-assistant`), and cluster create/join exist as REST
endpoints (confirmed in upstream `apidoc.js`, below). The expected friction: the
cluster-join operation restarting `pveproxy` mid-call (auth/TLS churn the SDK
client must ride out) and the wall-clock cost of three unattended installs per
run (solvable later with template-per-version + linked clones). One
chicken-and-egg is accepted up front: the cluster create/join surface is new, so
during buildout the CLI necessarily runs from the branch — the stable pin is the
**post-impl steady state**, not the Phase 0 starting point.

## Context

This advances **INV-0001** (nested Proxmox nodes) — and re-sequences it.
INV-0001 proposed proving the pipeline with Terraform first (its Phase 1) and
dogfooding the SDK second (its Phase 2). Since it was written, the landscape
changed: the live suite **has now run end-to-end against r740a** (9.2-1), ten
scrubbed cassettes are committed, and CI replays them on every push
(`just test-replay`). The SDK's provisioning ops are no longer
written-but-unverified — they are proven against real PVE. That removes the main
reason to de-risk with Terraform first, so this investigation goes **straight to
INV-0001's Phase 2**: the SDK provisions its own test environment. INV-0001's
desk findings (nested-virt knobs, performance expectations, credential
bootstrapping) carry over and are refined here.

The concrete payoff is closing the **two remaining unchecked boxes** in
IMPL-0001's "Outstanding live verification":

- **P4** — resource-affinity placement honored (needs a multi-node 9.x HA
  cluster; only one physical 9.2 node exists, so nesting is the only path short
  of new hardware);
- **P6** — the live VNC (RFB) byte stream carried by `console.Connect` (ticket
  mint is verified; the wire payload is not).

A P4 cassette recorded against the nested cluster also means
`TestResourceAffinityRule` joins the CI replay job — the criterion becomes
regression-guarded, not just once-verified.

**Triggered by:** IMPL-0001 (Outstanding live verification: P4 + P6); INV-0001
(nested nodes, Phase 2); the committed go-vcr replay harness (TESTING.md).

### Direction set at review (2026-07-09)

Decisions from the doc review, folded into the approach below:

1. **Three nested nodes, not two.** Local infra supports nested virt and the
   capacity; matching PVE's documented "at least three nodes for reliable
   quorum" removes the 2-node fencing fragility outright (the earlier QDevice /
   third-node escalation hedge is moot).
2. **Phase 0 of the eventual design/impl is local naive testing** against the
   cluster: a **CLI binary built from the most recent stable SDK release**
   creates (and destroys) the nested cluster; the **current-branch** SDK's
   integration tests run against it. Released code provisions; in-development
   code is what gets tested.
3. **Orchestration is a `just` target**: run the stable CLI with a **YAML config
   file** (infra settings) to spin up → run the branch integration tests → call
   the CLI again to spin down.
4. **Cadence is on-demand, not scheduled**: runs happen when recordings are
   needed — a PVE API change, a new PVE feature, new SDK surface. The captured
   recordings feed `mockpve`, which is then **certified for the PVE version it
   was tested against**.
5. **Execution is gated**: the spike does not start until explicitly approved —
   this document is the decision artifact to review first.

## Approach

### Phase 0 shape: stable CLI provisions, branch tests run

The original sketch here was a `//go:build dogfood` test package that did its
own provisioning. The review reshaped it: **provisioning moves into a CLI**
(working name `cmd/pvelab`, naming open) and the tests stay where they are — the
existing `integration`-tagged suite, pointed at the nested cluster. The `just`
target sequences the three:

```text
just dogfood  (name indicative)
  ├─ 1. go run <module>/cmd/pvelab@<latest-stable> up   --config dogfood.yaml
  │      → provisions 3 nested nodes on r740a, clusters them,
  │        prints/writes the nested endpoint + creds env
  ├─ 2. go test -tags=integration ./proxmox/integration/  (current branch)
  │      → PVE_ENDPOINT=<nested cluster>, PVE_RECORD=1,
  │        P4 placement + P6 RFB + any suite tests worth re-recording
  └─ 3. go run <module>/cmd/pvelab@<latest-stable> down --config dogfood.yaml
         → tears down the nested VMs, leaves r740a clean
```

Pinning the CLI with `go run <module>/cmd/pvelab@vX.Y.Z` gets "most recent
stable" for free from the module proxy (the module is published as of v0.1.1) —
no separate release artifact needed, and a broken working tree can never strand
infrastructure because `down` runs released code.

**Chicken-and-egg, by design:** the stable-pinned diagram above is the
**expected state after the design/impl completes** — it cannot be the starting
state, because the CLI and the cluster create/join surface have to be built
before any stable tag can contain them. During buildout, Phase 0's "local naive
testing" **runs the CLI from the branch**; that is a planned part of Phase 0,
not a deviation. The `@stable` pin switches on at the first tag that ships the
CLI + cluster surface (likely v0.2.0), and from then on released code provisions
while branch code is tested.

The YAML config carries **settings, not secrets** (credentials stay in env /
`op run`, matching the existing harness convention). Indicative shape:

```yaml
outer:
  endpoint: https://r740a.example:8006 # token via PVE_TOKEN_ID/_SECRET env
  storage: local-zfs
  bridge: vmbr0
nested:
  pve_version: "9.2" # selects the prepared ISO / template
  cluster_name: dogfood
  gateway: 10.0.0.1
  root_password_env: DOGFOOD_ROOT_PW # env var name, not the value
  nodes:
    - { name: pve1, vmid: 9201, cidr: 10.0.0.201/24 }
    - { name: pve2, vmid: 9202, cidr: 10.0.0.202/24 }
    - { name: pve3, vmid: 9203, cidr: 10.0.0.203/24 }
```

The provisioning stages are **not recorded**; only the inner test phase runs
with `PVE_RECORD=1`.

### Stage 0: ISO preparation (one-time per PVE version)

`proxmox-auto-install-assistant prepare-iso` embeds a TOML answer file into the
stock PVE ISO: static network (`source = "from-answer"`, per-node
cidr/gateway/dns), root password, disk/filesystem selection, and a **first-boot
hook** (PVE 8.3-1+) for any node prep (e.g. enabling the no-subscription repo).
Three prepared ISOs (one per hostname/IP) — or one ISO + the `--fetch-from http`
answer URL if we want a single artifact later. The assistant is a
Debian-packaged tool (`apt install proxmox-auto-install-assistant`, needs
`xorriso`) — run it in a throwaway Debian container locally, or directly on
r740a over SSH (it's a Debian host). This stage is scripted but outside the Go
harness.

### Stage 1: Provision (CLI, against r740a)

All live-verified SDK surface, driven by the stable-pinned CLI:

1. `Storage().UploadISO` the prepared ISOs to r740a (P3, live-verified — the
   chunked-body and multipart fixes came from exactly this path).
2. `QEMU(node).Create` three VMs: **CPU type `host`** (exposes VT-x to the guest
   — required for nested KVM), 4 vCPU / 6–8 GiB RAM / 32 GiB disk on
   `local-zfs`, NIC on `vmbr0`, the prepared ISO as boot CD.
3. `Start` all three; the unattended installs run in parallel, reboot into PVE.
4. Readiness: poll `https://<nested-ip>:8006/api2/json/version` with a fresh SDK
   client until each answers (bounded, generous timeout — installs take
   minutes). The answer file's **post-installation webhook** (8.3-1+) is a later
   refinement: it POSTs machine-id + SSH host keys on install success, which
   both signals completion and provides host-key pinning material for the `ssh`
   side-channel.

### Stage 2: Cluster formation

Confirmed REST endpoints (upstream `apidoc.js`, fetched 2026-07-08):

- `POST /cluster/config` (`create`, params `clustername`, `nodeid`, links) — run
  against nested node 1;
- `GET /cluster/config/join` (`join_info`, `allowtoken: 1`) — read node 1's join
  fingerprint;
- `POST /cluster/config/join` (`join`, params `hostname`, `password` — the
  **peer's root password** — `fingerprint`, `force`, links) — run against nested
  nodes 2 and 3, **serialized** (corosync membership changes should not race).

None of these are wrapped by the SDK yet → **the harness drives new SDK
surface**: `cluster.CreateCluster` / `JoinInfo` / `JoinCluster` (all
`allowtoken: 1` per the schema, though see Open questions — join restarts
`pveproxy` on the joining node mid-operation, so the client must tolerate a
dropped connection + changed TLS cert and re-poll until the node reappears as a
cluster member). Fallback if REST proves unreliable: `Client.SSH(...).Exec` with
`pvecm create` / `pvecm add` — which would give the `ssh` side-channel its first
live exercise (currently tested only against an in-process SSH server).

### Stage 3: Inner suite — P4 with recording, then P6

Against the nested cluster, authenticated with **password credentials** (the
answer-file root password — see Findings):

- **P4:** create two **diskless dummy VMs** (tiny RAM, no disk — the QEMU
  process running with no bootable device still counts as "started", so HA can
  place and migrate them with zero storage dependency); `HA().CreateResource`
  for both; `HA().CreateRule` with a **negative resource-affinity** rule; poll
  `Cluster().ListResources` until the two land on different nodes (bounded —
  ha-manager's reaction is tens of seconds for placement, ~2 min for error
  detection, so budget ~5 min). Run with `PVE_RECORD=1`; each status poll is its
  own cassette interaction (the replay-poll lesson from the recorder harness),
  so the cassette replays faithfully in CI afterwards. Then the
  positive-affinity variant (both on the same node) as a second assertion.
- **P6:** create/start one dummy VM, `Console().MintVNCTicket`,
  `console.Connect`, and **read the first 12 bytes** of the stream: the RFB
  protocol greeting `"RFB 003.008\n"` (RFC 6143 §7.1.1). That single read is the
  wire-payload verification the mock's hijack+echo cannot provide. No full VNC
  auth handshake needed.

Cassette scrubbing: pick **non-identifying nested names up front** (hostnames
`pve1`/`pve2`/`pve3`, cluster name `dogfood`) so only the nested IPs/endpoint
need scrubbing; extend `topologyScrub` to take multiple host→placeholder pairs.

### Stage 4: Teardown

Reverse order, all outer-node SDK ops (live-verified), via the stable CLI's
`down` command: stop + delete the nested VMs on r740a, delete the uploaded ISOs
(optional — they're reusable), using the `cleanupCtx` bounded-teardown pattern
so a wedged delete fails fast. The nested cluster needs no graceful dissolution
— it dies with its VMs. Because `down` is released code taking the same YAML, it
works even when the working tree is broken mid-development.

## Environment

| Component          | Version / Value                                          |
| ------------------ | -------------------------------------------------------- |
| Physical host      | `r740a`, PVE 9.2-1 (nested virt confirmed supported)     |
| Nested node VMs    | 3× — CPU `host`, 4 vCPU, 6–8 GiB RAM, 32 GiB, vmbr0      |
| Nested PVE version | 9.2 ISO first; 9.0/9.1 later (multi-version matrix)      |
| ISO preparation    | `proxmox-auto-install-assistant` (answer.toml, 8.3-1+)   |
| Nested hostnames   | `pve1` / `pve2` / `pve3`, cluster `dogfood`              |
| Nested networking  | static IPs on the lab bridge (from answer file)          |
| Provisioner        | CLI (`cmd/pvelab`, name TBD) pinned to latest stable tag |
| Config             | YAML file (settings only; secrets via env)               |
| Orchestration      | `just dogfood` (name indicative): up → test → down       |
| Tests under test   | current branch, `go test -tags=integration`              |
| Inner-suite auth   | root@pam password credentials (from answer file)         |
| Recording          | go-vcr v4, inner P4/P6 phase only, `PVE_RECORD=1`        |
| SDK version        | needs new `cluster` config surface in a stable tag       |

## Findings (desk + web research, 2026-07-08 — not yet validated on hardware)

> No spike has run yet. Facts below are sourced from the upstream PVE API schema
> (`https://pve.proxmox.com/pve-docs/api-viewer/apidoc.js`, fetched 2026-07-08),
> the PVE wiki/manual, and this repo's own live-verification record. Empirical
> timings and behaviour are to be filled in by the spike.

### Cluster create/join are real REST endpoints — new SDK surface

Verified in upstream `apidoc.js`: `POST /cluster/config` (create, params
`clustername`/`nodeid`), `GET /cluster/config/join` (join info), and
`POST /cluster/config/join` (join, params `hostname` + peer root `password` +
cert `fingerprint` + `force`). All three carry `allowtoken: 1` in the schema, so
API-token auth is nominally allowed — but join **replaces the joining node's
pmxcfs config with the cluster's** (local users/tokens on the joining node do
not survive the join) and restarts its API daemons mid-call. Password
credentials sidestep both problems since the same root password exists on every
node by answer-file construction. The SDK's `cluster` package currently wraps
only resources/status/options — `CreateCluster`/`JoinInfo`/`JoinCluster` are
new, honestly REST-backed surface this harness would drive in (with the usual
mockpve handlers + unit tests). This is dogfooding in the strongest sense: the
test harness grows the product. Consequence of the stable-pin model: this
surface must land **and ship in a stable tag** before the CLI can be pinned to a
release that provisions clusters — which is why Phase 0's buildout runs the CLI
from the branch by design (accepted chicken-and-egg; see the Phase 0 shape).

### Unattended PVE install is fully supported by upstream tooling

The PVE wiki (Automated Installation) confirms everything Stage 0 needs:
answer-file static networking (`source = "from-answer"` with cidr/dns/gateway),
mandatory root password (plain or hashed), disk/filesystem choice (ZFS/ext4/…),
a **first-boot hook** (`from-iso`/`from-url`, PVE 8.3-1+, ordering up to
`fully-up`), and a **post-installation webhook** that POSTs JSON including SSH
host keys — future host-key-pinning material for the `ssh` side-channel. The
assistant needs a Debian-ish environment with `xorriso`; a container or r740a
itself both qualify. The installer's boot menu auto-selects the automated
install after 10 s, so a VM booting the prepared ISO needs no interaction at
all.

### Three nodes match PVE's quorum guidance (decided)

PVE 9's HA rules chapter documents exactly the semantics P4 asserts: **resource
affinity** rules with **negative** (spread across nodes) and **positive** (keep
together) polarity, alongside node-affinity rules. The manual's stated HA
requirement is "at least three cluster nodes (to get reliable quorum)". The
original sketch here considered a 2-node cluster (quorate while both nodes are
up, but any transient quorum wobble triggers watchdog fencing and a nested-node
reboot) with QDevice/third-node escalation hedges. **Review decision
(2026-07-09): three nodes from the start** — local infra supports the capacity,
the fencing fragility disappears, and the harness matches the documented minimum
instead of testing at the edge of it. The CRM/LRM stack runs on the default
softdog watchdog (fine inside VMs). Budget generous placement-poll timeouts:
ha-manager reacts to new resources in tens of seconds and detects failures in ~2
min.

### Password credentials are the right bootstrap — and close another gap

INV-0001 flagged token bootstrapping as "the fiddly part". The clean answer is
to not use tokens inside the nested cluster at all: the answer file sets a known
root password, the SDK's **user/password credential strategy** (mint + 2 h
ticket refresh, Phase 1) authenticates with it, and the join-wipes-local-tokens
problem (above) evaporates. Bonus: the password/ticket mint path is currently
**mock-verified only** — the dogfood harness would give it its first live
exercise, closing a quiet P1 verification gap alongside P4/P6. The existing
cassette redaction already scrubs `password` form fields and `ticket` response
bodies, so recorded inner-suite cassettes stay clean.

### P6 needs only the RFB greeting

The VNC websocket payload check is 12 bytes: every RFB server opens with the
ProtocolVersion handshake `"RFB 003.008\n"` (RFC 6143 §7.1.1) immediately on
connect. `console.Connect` already returns the raw duplex stream; reading and
asserting that greeting against a real QEMU VNC server is the entire remaining
P6 gap. Doing it on a nested node (rather than r740a directly) keeps the
clean-room property — no touching real guests — though strictly P6 could be
closed against any live node with a scratch VM.

### What the harness stands on vs what it exercises for the first time

| Capability                       | Status today                        |
| -------------------------------- | ----------------------------------- |
| ISO upload (`UploadISO`)         | live-verified (P3 cassette)         |
| QEMU create/start/stop/delete    | live-verified (P2 cassette)         |
| Task waiters (`tasks.Wait`)      | live-verified (all lifecycles)      |
| HA resource + rule CRUD          | mock-verified (writes are sync)     |
| Console mint                     | live-verified (P6 cassette)         |
| `console.Connect` RFB payload    | **new** — the P6 gap itself         |
| Password credential mint/refresh | mock-verified → first live exercise |
| `cluster` create/join/join-info  | **new SDK surface** (REST-backed)   |
| `ssh` side-channel (ISO prep)    | in-process-verified → first live    |

The provisioning layer is entirely green — and under the Phase 0 model it runs
as **released** code, so the harness's novel risk is concentrated in cluster
formation and the nested environment itself, never in the code being tested.

### Recording cadence: on-demand runs certify mockpve per PVE version

The harness is **on-demand, not scheduled**: it runs when recordings are needed
— a PVE point release changes the API, a new PVE feature lands, or new SDK
surface needs live proof. Each run's scrubbed cassettes feed two consumers:

1. **CI replay** (`just test-replay`) — the per-PR regression net, no node
   needed;
2. **`mockpve`** — the recorded corpus is the ground truth the mock's handlers
   are checked against (the OQ-4/5/10 corpus-seeding pipeline), after which the
   mock is **certified for the PVE version the recordings came from** (e.g.
   "mockpve certified against 9.2-1"). How that certification is recorded — a
   stamp in `mockpve`'s docs, a metadata file next to the cassettes — is an open
   question.

`cmd/pve-schemadiff` completes the loop: its CI drift check against a real
`apidoc.js` dump is the **trigger signal** — schema drift detected → run the
dogfood harness against the new version → refresh recordings → update and
re-certify mockpve.

### The multi-version clean-room evolution is a template problem

Per-run unattended installs cost minutes per node (desk estimate: 5–10 min each,
parallelizable; **to be measured**). The evolution path: install each PVE minor
**once**, convert to an outer-node **template**, and let the harness
**linked-clone + boot** per run (INV-0001's clone-per-run model, now SDK-driven
— `qemu.Clone` is live-verified). Then a version matrix (9.0/9.1/9.2 templates)
makes the SDK's per-minor capability gates (`version.Capabilities`) testable
against real minors instead of only mocked version strings — something no amount
of single-node testing provides, and exactly the input the per-version mockpve
certification needs. Template freshness becomes a Renovate-style chore (rebuild
on minor release). Per-PR coverage stays the committed-cassette replay job,
which each dogfood run feeds.

## Open questions

- **CLI release shape** (the branch-run-during-buildout part is _decided_, not
  open — see the Phase 0 shape): confirm the tag boundary where the `@stable`
  pin switches on (likely v0.2.0, the first tag shipping the CLI + cluster
  surface), and whether the CLI is release-worthy (goreleaser) or stays a
  `go run`-only dev tool — CLAUDE.md/README currently state mockpve is "the only
  binary this repo produces", which this changes either way.
- **CLI name + config schema:** `cmd/pvelab` and the YAML shape above are
  indicative — settle both before building (settings in YAML, secrets via env is
  the fixed constraint).
- **`allowtoken: 1` vs reality for cluster create/join:** the schema says tokens
  work; confirm live (and how the SDK client behaves across the joining node's
  pveproxy restart + cert change mid-join — retry policy, TLS handling).
- **Install wall-clock nested:** how long does the unattended install actually
  take inside a nested VM on r740a? Determines whether per-run install is
  tolerable short-term or templates are needed immediately. (Nested-virt support
  and 3-node capacity on local infra: confirmed at review; the
  `kvm_intel nested=Y` knob is still worth a 10-second sanity check.)
- **IP allocation:** three static IPs from the lab range is simplest but encodes
  lab topology into the YAML (scrubbed from cassettes; fine?) — vs a NATed
  bridge (isolated, but the runner then needs a route to :8006).
- **Where does Stage 0 live long-term — RESOLVED (DESIGN-0002 OQ-5):** on r740a
  over SSH (the node already has the assistant packages + base ISO), built into
  the CLI as `pvelab iso` via the `proxmox/ssh` side-channel — which therefore
  gets its first live exercise here, not via the cluster fallback.
- **Recording volume for P4:** placement polling produces variable-length
  cassettes per run — acceptable for replay (each poll is one interaction), but
  confirm the recorded run replays deterministically in CI before committing.
- **mockpve certification format — RESOLVED (DESIGN-0002 OQ-8):** a
  machine-readable `certification.yaml` beside the cassettes (one entry per
  recording batch); human-readable views derivable from it.

## Conclusion

**Answer: pending the spike — desk-feasible with high confidence.** Every
load-bearing claim checked out against upstream sources: cluster formation is
REST (with a mid-join daemon-restart caveat), unattended install is first-class
tooling with exactly the knobs needed (static IP, root password, first-boot), a
3-node nested cluster meets PVE's documented quorum minimum, and the entire
provisioning layer is SDK surface this repo has already live-verified against
r740a — run as released code under the Phase 0 model. The novel risks are
operational (join churn, install wall-clock), not architectural. This stays
**Open** until the spike produces a green P4 cassette + P6 RFB assertion and
real timings.

## Recommendation

Phase 0 spike, in this order (each step independently useful). **Execution is
gated: do not start without explicit approval** (requested at review,
2026-07-09).

1. **Sanity-check the substrate** (10 min, manual): `kvm_intel nested=Y` + free
   RAM on r740a (capacity already confirmed at review).
2. **Hand-prove one nested node**: prepare one answer-file ISO, upload + boot it
   via the SDK, time the install, confirm `:8006` answers. This retires the
   biggest unknown (nested install wall-clock) before any harness code exists.
3. **Build the CLI skeleton** (`up`/`down`, YAML config) with Stages 1 + 4 only
   (provision 3 nodes + teardown, no cluster) — run from the branch, the
   designed Phase 0 state — and prove it leaves r740a clean across repeated
   runs.
4. **Add cluster formation**: implement `cluster.CreateCluster`/`JoinInfo`/
   `JoinCluster` (+ mockpve handlers, unit tests) and drive them from the CLI;
   fall back to `ssh.Exec pvecm` only if the REST path proves unreliable.
5. **Close P4 + P6**: `just dogfood` end-to-end — CLI up → branch integration
   tests with `PVE_RECORD=1` → CLI down; commit the scrubbed cassettes, add
   `TestResourceAffinityRule` to the replay job, check both boxes in IMPL-0001's
   Outstanding live verification, and update the live-verification-gaps
   tracking.
6. **Ship + pin**: land the CLI + cluster surface in a stable tag and switch the
   `just` target to the `@stable` pin — the transition from Phase 0's branch-run
   buildout to the steady state (released code provisions, branch code is
   tested).
7. **Then evolve**: template-per-minor + linked clones, the 9.0/9.1/9.2 matrix,
   per-version mockpve certification, schemadiff-triggered refresh — and promote
   the settled methodology into a DESIGN doc (harness architecture), at which
   point INV-0001 and this INV both conclude.

## References

- IMPL-0001 — Outstanding live verification (P4 + P6)
  (`docs/impl/0001-proxmox-ve-9x-sdk-coverage.md`)
- INV-0001 — Nested Proxmox nodes for automated live SDK testing
  (`docs/investigation/0001-nested-proxmox-nodes-for-automated-live-sdk-testing.md`)
- `TESTING.md` — live-node walkthrough, recording + replay harness
- `cmd/pve-schemadiff` — API drift guard (the on-demand trigger signal)
- PVE API viewer schema — `/cluster/config`, `/cluster/config/join`,
  `/cluster/config/qdevice` (`pve.proxmox.com/pve-docs/api-viewer/apidoc.js`,
  fetched 2026-07-08)
- PVE wiki — Automated Installation (`proxmox-auto-install-assistant`, answer
  file, first-boot hook, post-install webhook)
- PVE manual — HA Manager chapter (rules: node/resource affinity; quorum +
  watchdog requirements; ~2 min error-detection window)
- RFC 6143 §7.1.1 — RFB ProtocolVersion handshake (`"RFB 003.008\n"`)
