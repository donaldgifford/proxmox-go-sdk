# Testing

This is the hands-on guide to testing `proxmox-go-sdk` against a **real Proxmox
VE 9.x cluster** and to **recording cassettes** for later replay. If you just
want to build and run the unit suite, see [DEVELOPMENT.md](DEVELOPMENT.md); this
document picks up where that leaves off.

There are two goals here, and you can do them together in one run:

1. **Verify the SDK against real hardware.** Everything in the SDK is
   unit-tested against the in-process `mockpve` responder, which mimics the REST
   envelope but not a real hypervisor. A set of acceptance criteria can only be
   confirmed against a live node — this guide walks each one.
2. **Record cassettes.** With `PVE_RECORD=1`, the same live run captures the
   real HTTP exchanges (with secrets redacted) into `go-vcr` cassettes, so the
   suite can later replay them in CI without a cluster.

> **Heads-up on secrets.** Recording writes real API traffic to disk. The
> harness redacts credentials automatically (see
> [Recording](#recording-cassettes)), and cassettes are git-ignored by default
> so nothing lands in a commit until you review it. Read that section before you
> record.

## Mental model

```text
┌─────────────────┐     ┌──────────────────┐     ┌────────────────────┐
│  Unit tests     │     │ Integration tests│     │ Recorded cassettes │
│  (default)      │     │ (this guide)     │     │ (this guide)       │
│                 │     │                  │     │                    │
│  go test ./...  │     │ -tags=integration│     │ PVE_RECORD=1 →     │
│  → mockpve      │     │ → live 9.x node  │     │ testdata/cassettes │
│  no network     │     │ real cluster     │     │ → replay later     │
└─────────────────┘     └──────────────────┘     └────────────────────┘
```

- **Unit** runs everywhere, always, with no configuration.
- **Integration** runs only when you point it at a node (env vars below) and is
  a no-op otherwise — every test `t.Skip`s when its inputs are missing.
- **Recording** is integration + `PVE_RECORD=1`; it is otherwise identical.

## Before you start

You need:

- A reachable **Proxmox VE 9.0+** node you can afford to mutate. Use a **scratch
  cluster or a lab node** — the lifecycle tests create and destroy VMs,
  containers, volumes, snapshots, and HA rules.
- A second **9.2** node if you want to exercise the `9.2+` gated operations.
- Go tooling installed via `mise` (see [DEVELOPMENT.md](DEVELOPMENT.md#setup)).
- Free guest IDs and a storage you can scribble on (e.g. `local-lvm`).

Decide up front, and write them down:

| Thing           | Example                           | Notes                       |
| --------------- | --------------------------------- | --------------------------- |
| Node name       | `pve`                             | `pvesh get /nodes` to list  |
| Scratch storage | `local-lvm`                       | must allow `images` + `iso` |
| Scratch QEMU ID | `9101`                            | must be unused              |
| Scratch LXC ID  | `9102`                            | must be unused              |
| LXC template    | `local:vztmpl/debian-13-…tar.zst` | `pveam list local`          |

## Step 1 — Create an API token

The suite authenticates with an API token (recommended over a password). On a
scratch cluster the simplest choice is a full-privilege token on `root@pam`.

**On the node (CLI):**

```sh
# --privsep 0 makes the token inherit the user's privileges (root = full).
pveum user token add root@pam sdk --privsep 0
```

This prints the token id and secret **once** — copy the secret now:

```text
┌──────────────┬──────────────────────────────────────┐
│ key          │ value                                │
├──────────────┼──────────────────────────────────────┤
│ full-tokenid │ root@pam!sdk                         │
│ value        │ 3fb7…-…-…                            │  ← PVE_TOKEN_SECRET
└──────────────┴──────────────────────────────────────┘
```

**Or in the GUI:** _Datacenter → Permissions → API Tokens → Add_, uncheck
_Privilege Separation_, and copy the secret.

**Least privilege (optional):** if you would rather not use `root`, create a
user with a role that grants (across the phases you plan to run) `VM.*`,
`Datastore.*`, `Sys.*`, `SDN.*`, `Pool.*`, and the HA/console privileges. Grant
it at `/`, then create the token with privilege separation and add an ACL. On a
scratch cluster, the full-privilege `root@pam` token above is far less fiddly.

## Step 2 — Get the repo and toolchain

```sh
git clone https://github.com/donaldgifford/proxmox-go-sdk
cd proxmox-go-sdk
mise install
go vet -tags=integration ./proxmox/integration/...   # compile the suite
```

## Step 3 — Configure the environment

The harness reads everything from the environment. Put this in a file you can
`source` (e.g. `.env.local` — it is git-ignored) so you do not paste secrets
into your shell history:

```sh
# --- required: endpoint + ONE credential pair ---
export PVE_ENDPOINT="https://pve.example:8006"
export PVE_TOKEN_ID="root@pam!sdk"      # API-token auth (preferred for a real node)
export PVE_TOKEN_SECRET="3fb7…-…-…"
# export PVE_USERNAME="root@pam"        # password auth instead (what .pvelab.env
# export PVE_PASSWORD="…"               # uses — tokens don't survive a cluster join)

# --- common ---
export PVE_NODE="pve"          # default "pve"
export PVE_INSECURE_TLS=1      # if the node uses a self-signed cert

# --- destructive-test gates (set only the ones you want to run) ---
export PVE_TEST_STORAGE="local-lvm"
export PVE_TEST_ISO_STORAGE="local"   # ISO upload target (must allow "iso"); falls back to PVE_TEST_STORAGE
export PVE_TEST_VMID=9101
export PVE_TEST_CONSOLE_VMID=9103    # console-mint scratch VM; distinct so it runs alongside the lifecycle tests
export PVE_TEST_LXC_VMID=9102
export PVE_TEST_LXC_TEMPLATE="local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
export PVE_TEST_ISO_PATH="/path/to/tiny.iso"
export PVE_TEST_PLACEMENT_VMID_1=9301   # HA placement pair (needs a quorate
export PVE_TEST_PLACEMENT_VMID_2=9302   # multi-node cluster, e.g. the pvelab lab)
```

Every variable:

| Variable                    | Required | Purpose                                                    |
| --------------------------- | -------- | ---------------------------------------------------------- |
| `PVE_ENDPOINT`              | yes      | base URL, e.g. `https://pve.example:8006`                  |
| `PVE_TOKEN_ID`              | yes\*    | e.g. `root@pam!sdk`                                        |
| `PVE_TOKEN_SECRET`          | yes\*    | the token's secret                                         |
| `PVE_USERNAME`              | yes\*    | password auth, e.g. `root@pam` (when token vars absent)    |
| `PVE_PASSWORD`              | yes\*    | password auth; pairs with `PVE_USERNAME`                   |
| `PVE_NODE`                  | no       | node under test (default `pve`)                            |
| `PVE_INSECURE_TLS`          | no       | `1` to skip TLS verify (self-signed)                       |
| `PVE_RECORD`                | no       | `1` to record cassettes while running                      |
| `PVE_REPLAY`                | no       | `1` to replay committed cassettes (no node; see below)     |
| `PVE_DEBUG`                 | no       | `1` to stream a line per SDK request                       |
| `PVE_TEST_STORAGE`          | gate     | storage for scratch disks / uploads                        |
| `PVE_TEST_ISO_STORAGE`      | gate     | ISO-upload storage (allows `iso`); else `PVE_TEST_STORAGE` |
| `PVE_TEST_VMID`             | gate     | scratch QEMU VMID (created + destroyed)                    |
| `PVE_TEST_CONSOLE_VMID`     | gate     | scratch QEMU VMID for the console-mint test (own VMID)     |
| `PVE_TEST_LXC_VMID`         | gate     | scratch LXC VMID (created + destroyed)                     |
| `PVE_TEST_LXC_TEMPLATE`     | gate     | OS template volid for the LXC lifecycle                    |
| `PVE_TEST_ISO_PATH`         | gate     | local path to a small ISO to upload                        |
| `PVE_TEST_PLACEMENT_VMID_1` | gate     | scratch VMID for the HA placement pair (multi-node)        |
| `PVE_TEST_PLACEMENT_VMID_2` | gate     | the pair's second scratch VMID                             |
| `PVE_SCRUB_EXTRA`           | no       | extra `live=placeholder` recording-scrub pairs (CSV)       |

\* one credential pair is required: `PVE_TOKEN_ID`+`PVE_TOKEN_SECRET` (wins when
both pairs are set) or `PVE_USERNAME`+`PVE_PASSWORD`.

### How the harness finds these values

The suite reads the variables from the process environment. There are three ways
to get them there — the harness makes all three work, and **a value already
present in the environment always wins**:

1. **Export + run** (what Step 4 shows) — `source` a file of `export KEY=…`
   lines, then `go test`; the child process inherits the exported vars.
2. **`op run`** (1Password secret references) — if your file holds `op://…`
   references rather than literal values, the SDK does **not** resolve them; run
   the suite under 1Password's own resolver:

   ```sh
   op run --env-file=.env -- \
     go test -tags=integration ./proxmox/integration/... -run 'Reads|Version' -v
   ```

   `op run` reads the file once, resolves each `op://…` ref, and hands real
   values to `go test`. The vars are then already set, so the autoloader (below)
   does nothing.

3. **Autoload a dotenv file** — if the required vars are **not** already set, a
   `TestMain` in the suite loads `.env.local` (then `.env`) from the repo root
   with `godotenv`, so a plain `go test -tags=integration …` picks them up with
   no `source` at all. It only reads a file when the creds are missing and never
   overrides a var you set yourself. Because it does not resolve `op://…`, a
   file of raw 1Password references autoloaded this way sets the literal
   `op://…` strings and the node answers **401** (not a skip) — that is the
   signal to use `op run` (option 2) instead.

> **1Password `.env` mounted as a FIFO.** If 1Password mounts your `.env` as a
> named pipe (`prw-------` in `ls -l`), it is **single-use and blocks until a
> reader connects** — `source .env` twice, or letting both `op run` _and_ the
> autoloader read it, drains it. Pick **one** reader: either
> `op run --env-file=.env -- …` (resolves `op://…` refs), or, if the pipe
> already yields resolved `KEY=value` pairs, `set -a; source .env; set +a` once
> and then `go test`. The autoloader deliberately skips the file whenever the
> creds are already exported, so it never competes with your `op run`.

## Step 4 — Smoke test (read-only, safe anywhere)

Start with the read-only tests. They mutate nothing and prove auth + TLS + the
envelope round-trip work:

```sh
source .env.local
go test -tags=integration ./proxmox/integration/... -run 'Reads|Version' -v
```

Expect `PASS` for `TestVersionRoundTrip`, `TestComputeReads`,
`TestStorageReads`, `TestClusterAndHAReads`, `TestNetworkReads`, and
`TestAccessReads`. If any test `SKIP`s, its required env var is unset. If you
see an error, jump to [Troubleshooting](#troubleshooting).

## Step 5 — Lifecycle tests (destructive, one at a time)

Each destructive test is gated by its own variables and cleans up after itself.
Run them individually so you can watch each one. Every test maps to a phase's
acceptance criterion.

### QEMU lifecycle (Phase 2)

Creates → starts → snapshots → rolls back → stops → deletes a scratch VM.

```sh
# needs: PVE_TEST_STORAGE, PVE_TEST_VMID
go test -tags=integration ./proxmox/integration/... -run TestQEMULifecycle -v
```

### LXC lifecycle (Phase 2)

Same chain for a container.

```sh
# needs: PVE_TEST_STORAGE, PVE_TEST_LXC_VMID, PVE_TEST_LXC_TEMPLATE
go test -tags=integration ./proxmox/integration/... -run TestLXCLifecycle -v
```

### Storage (Phase 3)

Streams an ISO upload to a live node.

```sh
# ISO upload — needs: PVE_TEST_STORAGE (allows "iso") or PVE_TEST_ISO_STORAGE, PVE_TEST_ISO_PATH
go test -tags=integration ./proxmox/integration/... -run TestISOUpload -v
```

> **No volume-snapshot test.** PVE exposes no storage-level volume-snapshot REST
> endpoint (verified on a live 9.2 node — the content API stops at
> `.../content/{volume}`). `storage.VolumeSnapshots` and friends return
> `pverr.ErrUnsupported`; a volume is snapshotted through its owning guest,
> which the QEMU/LXC lifecycle tests already cover. See the unit test
> `TestVolumeSnapshotsUnsupported`.

### HA (Phase 4)

Creates two diskless dummy VMs, places them under HA management, and observes
the scheduler honor a **negative** resource-affinity rule (different nodes) then
the **positive** flip (co-location). Needs a quorate multi-node cluster — the
pvelab nested lab is the intended target (`.pvelab.env` sets the gates). This
supersedes the retired rule-only `TestResourceAffinityRule` (`PVE_TEST_HA_SIDS`
is gone with it).

```sh
# needs: PVE_TEST_PLACEMENT_VMID_1 + PVE_TEST_PLACEMENT_VMID_2 (scratch VMIDs)
go test -tags=integration ./proxmox/integration/... -run TestResourceAffinityPlacement -v
```

### Network / SDN (Phase 5)

Enumeration is covered by `TestNetworkReads` (Step 4). Note that **SDN live
status** (`SDNStatus`/`VNetStatus`) currently returns `pverr.ErrUnsupported` —
part of what you are confirming is whether a real endpoint exists on your node.

### Console / access (Phase 6)

Lists users and tokens under the 9.x privilege model and mints a VNC ticket.
`TestConsoleRFB` goes one step further: it dials the vncwebsocket via
`console.Connect` and asserts the live QEMU VNC server's RFB ProtocolVersion
greeting — the raw byte stream cannot be recorded (no cassette by design), so it
is live-only and skips under `PVE_REPLAY=1`.

```sh
# needs: PVE_TEST_STORAGE, PVE_TEST_CONSOLE_VMID
# (each spins up its own scratch VM, works against it, then tears it down)
go test -tags=integration ./proxmox/integration/... -run 'TestConsoleMint|TestConsoleRFB' -v
```

### Everything at once

Once you trust the individual runs:

```sh
go test -tags=integration ./proxmox/integration/... -v
```

## Acceptance-criteria checklist

Tick these off against your node. They map to the per-phase Success Criteria in
`docs/impl/0001-proxmox-ve-9x-sdk-coverage.md`.

- [ ] **Phase 1 — foundation:** auth + `GET /version` round-trips; task waiters
      drive a real start/stop to completion (`TestVersionRoundTrip`, and the
      lifecycle `Wait`s).
- [ ] **Phase 2 — compute:** create → start → snapshot → rollback → stop →
      delete for **both** QEMU and LXC (`TestQEMULifecycle`,
      `TestLXCLifecycle`).
- [ ] **Phase 3 — storage:** ISO upload streamed to a live node
      (`TestISOUpload`). Storage-level volume snapshots are unsupported (no PVE
      REST endpoint); volume chains are exercised via guest snapshots in the
      Phase 2 lifecycles.
- [ ] **Phase 4 — HA:** observe the scheduler honor negative then positive
      resource-affinity placement (`TestResourceAffinityPlacement`, on a quorate
      multi-node cluster).
- [ ] **Phase 5 — network/SDN:** enumerate zones / VNets / fabrics
      (`TestNetworkReads`); confirm whether a real SDN live-status endpoint
      exists.
- [ ] **Phase 6 — cluster/access/console:** list users/tokens and mint a VNC
      ticket (`TestAccessReads`, `TestConsoleMint`); read the live RFB greeting
      over `console.Connect` (`TestConsoleRFB`).
- [ ] **9.2-gated ops:** on a 9.2 node, confirm the real endpoints (or absence)
      behind Dynamic Load Balancer, HA arm/disarm, SDN BGP fabrics, ZFS RAIDZ
      expansion, and token-secret rotation.

## The dogfood lab (pvelab)

Two criteria above need a **quorate multi-node cluster** — the HA placement pair
and (conveniently, though one node suffices) the RFB read. `pvelab` (IMPL-0002)
provisions an ephemeral 3-node nested-PVE cluster on a single outer host so
those run without touching real guests.

**Prereqs:** an outer PVE host with nested virtualization enabled
(`kvm_intel nested=Y`), ~24 GiB RAM headroom, an API token for the outer host,
SSH root access to it (for the ISO preparation step), and a reserved VMID block
(9200–9399 — pvelab refuses anything else).

```sh
cp pvelab.example.yaml pvelab.yaml   # edit: outer endpoint/node, nested IPs, domain
export PVE_TOKEN_ID=… PVE_TOKEN_SECRET=…   # outer-host token (names set in pvelab.yaml)
export PVELAB_ROOT_PW=…                    # nested nodes' root password (never stored)

just dogfood-iso    # one-time per PVE version: prepare the auto-install ISO
just dogfood        # up -> inner suite (records cassettes) -> down
```

`dogfood-up` installs three nested nodes from the prepared ISO, forms a quorate
cluster, and writes `.pvelab.env` — the inner suite's environment:
`PVE_ENDPOINT` (first nested node), `PVE_USERNAME`/`PVE_PASSWORD` (root@pam —
API tokens do not survive a cluster join), the placement/console gates, and
`PVE_SCRUB_EXTRA` (the recording-scrub pairs for the other nodes' IPs, the site
domain, and the outer host). `dogfood-test` sources that file, sets
`PVE_RECORD=1`, and runs `TestResourceAffinityPlacement` + `TestConsoleRFB`;
placement records a cassette for CI replay, the RFB read is cassette-less by
design. Review cassettes as described below before committing. The composite
`dogfood` tears the lab down even when the suite fails; run the steps
individually to keep it alive for debugging.

### Running pvelab on the outer host

The answer fetch is the only connection the flow initiates _toward_ the machine
running `up` — the installing nodes POST to the baked `answer_url`; everything
else is outbound from wherever pvelab runs. If your lab network cannot reach
your workstation (typical inter-VLAN policy), run the CLI on the outer host
itself — the first live formation (2026-07-12) used exactly this posture, and
the answer server's default `:8442` bind needs no change:

```sh
GOOS=linux GOARCH=amd64 go build -o pvelab ./cmd/pvelab
scp pvelab pvelab.yaml root@<outer-host>:
# in pvelab.yaml on the host: answer_url points at the OUTER HOST's address;
# outer.ssh.known_hosts/key_file must be valid paths THERE (the host SSHes to
# itself for `iso`), then re-run `./pvelab iso` — the URL is baked into the ISO.
# export the token + root-password env vars in that shell, never into files.
./pvelab iso && ./pvelab up
```

`up` writes `.pvelab.env` into its working directory on the host — `scp` it back
(or run `./pvelab env` and paste) so the inner suite, which runs from your
checkout as usual, can source it. Run `./pvelab down` on the host too, so it
also removes the state/env files it wrote there.

### Faster spin-up via templates (Phase 5, live-verify pending)

`pvelab template build` runs the unattended install **once**, then converts the
result into an outer-host template named `pvelab-tmpl-<version>` (dots dashed;
VMID from the `nested.template` block, reserved sub-range 9210–9219). Once it
exists, `just dogfood-up` automatically provisions via **linked clones** instead
of ISO installs — building the template is the opt-in; delete it (or rebuild
with `pvelab template build -force`) to fall back.

Every clone boots the template's baked-in identity, so `up` starts clones one at
a time and re-identifies each over SSH at the template's address (new
hostname/IP, regenerated SSH host keys, pmxcfs node-dir move) before starting
the next. **PVE tolerating that rename end-to-end is written-but-unverified** —
the unit tests pin the command sequence, not PVE's behaviour; the first live
clone run (and the clone-vs-ISO wall-clock measurement) is the IMPL-0002 Phase 5
live gate. One template per PVE minor can coexist: keep one config file per
version, each with its own `nested.template.vmid`.

## Recording cassettes

Add `PVE_RECORD=1` to any run and the harness records each HTTP exchange into a
per-test cassette under
`proxmox/integration/testdata/cassettes/<TestName>.yaml`:

```sh
source .env.local
PVE_RECORD=1 go test -tags=integration ./proxmox/integration/... -run 'Reads|Version' -v
```

### What gets redacted

Cassettes are scrubbed **before** they touch disk (a `go-vcr` `BeforeSaveHook`),
so a live secret never lands in a file:

- the `Authorization` header (carries the token secret),
- `Cookie` / `Set-Cookie` (auth tickets) and `CSRFPreventionToken`,
- `password` / `secret` / `otp` in request forms,
- `ticket` / `CSRFPreventionToken` / token `value` in credential-endpoint
  responses.

Each is replaced with `REDACTED`. This redaction is itself guarded by a unit
test that runs in normal CI:

```sh
go test ./proxmox/integration/... -run 'Redact|RecordReplay' -v
```

`TestRedactInteraction` asserts secrets are scrubbed; `TestRecorderRecordReplay`
records against `mockpve`, confirms the secret is absent from the file, then
replays with the server shut down.

### Review before committing

Cassettes are **git-ignored by default** (`testdata/cassettes/.gitignore`) so a
record run cannot accidentally commit un-reviewed data. Before committing any
cassette, open the `.yaml` and confirm:

- no `PVE_TOKEN_SECRET`, ticket, or password appears (search for your secret),
- you are comfortable committing the infrastructure details that _are_ captured:
  node names, IP addresses, MAC addresses, storage names, VM configs.

When a cassette is reviewed and you intend to commit it, force-add it
(`git add -f testdata/cassettes/<name>.yaml`) or narrow the `.gitignore`. Before
committing, the recorder scrubs each cassette twice: `redactInteraction` blanks
secrets (auth/cookie/CSRF headers, `password`/`secret`/`otp` form fields, and
`ticket`/`csrfpreventiontoken`/`value`/`password` JSON response fields) and
`topologyScrub` rewrites the live host, IP, and node name to the placeholders
`pve.example:8006` / `pve` so a committed fixture never exposes lab topology. A
multipart upload body is truncated to a marker so a large ISO is not committed
verbatim.

### Replaying cassettes (no node)

Once cassettes are committed they can drive the integration suite with **no live
node** — this is what CI runs. Set `PVE_REPLAY=1` and the harness backs each
test with its committed cassette (`ModeReplayOnly`, never touches the network)
instead of a live client. A host-agnostic matcher (`matchReplayRequest`) matches
on method + path + query, so the placeholder endpoint the cassettes were
scrubbed to is irrelevant.

```bash
just test-replay
```

The recipe supplies the `PVE_TEST_*` gate values each cassette was recorded with
(node `pve`; QEMU `9101`, LXC/console `9102`; ISO storage `local`) and `-run`s
only the tests that have a cassette. `TestConsoleRFB` has none by design (a raw
byte stream over a hijacked websocket cannot replay — design OQ-6) and is
excluded. The `.github/workflows/ci.yml` `Test Replay (cassettes)` job runs
exactly this recipe.

A cassette that predates a code change replays as
`requested interaction not found` — re-record it against a live node
(`PVE_RECORD=1`).

### Certification: drift → dogfood → refresh → re-certify

`certification.yaml` (beside the cassettes) is the machine-readable record of
**which PVE version mockpve's behaviour was verified against** — one entry per
recording batch, with any mock divergences reconciled (fixed in mockpve, or
named in `notes`) before the entry lands. The runbook when the PVE surface
moves:

1. **Drift trips.** `just schemadiff` (in CI) fails against the committed
   baseline, or a new PVE minor lands on the lab host. Point `-apidoc` at the
   node's real `apidoc.js` to see what changed; rebaseline with `-update` once
   understood.
2. **Dogfood run.** Set `nested.pve_version` to the new version (base ISO on the
   outer host first — `just dogfood-iso`), then `just dogfood`. The inner suite
   records fresh cassettes as it verifies the live criteria.
3. **Refresh recordings.** Re-record any stale cassettes
   (`requested interaction not found` on replay) with `PVE_RECORD=1`, review the
   diff for leaks (see above), force-add.
4. **Re-certify.** Compare mockpve against the fresh cassettes: fix genuine
   envelope divergences in mockpve (or record them in `notes` when deliberate),
   then append the batch's entry to `certification.yaml` — `pve_version`,
   `recorded`, `commit`, `harness`, the cassette list, notes. `just test-replay`
   green on the new batch is the regression guard.

## Troubleshooting

| Symptom                                         | Likely cause / fix                                                                                                            |
| ----------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| `x509: certificate signed by unknown authority` | self-signed node — set `PVE_INSECURE_TLS=1`                                                                                   |
| `401 authentication failure`                    | wrong `PVE_TOKEN_ID`/`PVE_TOKEN_SECRET`; id is `user@realm!name`                                                              |
| `403` / `Permission check failed`               | token lacks a privilege — use a fuller role (see Step 1)                                                                      |
| a test `SKIP`s                                  | a required `PVE_TEST_*` var is unset (expected)                                                                               |
| `ErrUnsupported`                                | op needs a newer 9.x minor, or has no confirmed REST endpoint                                                                 |
| connection refused / timeout                    | wrong `PVE_ENDPOINT` host/port (`:8006`), or firewall                                                                         |
| a step sits silent for a while                  | normal — the task waiter polls quietly; set `PVE_DEBUG=1` to see each request, and note each step is bounded by a 90s context |
| `Wait(...): context deadline exceeded`          | the task never went terminal within 90s — run with `PVE_DEBUG=1` to watch the `/tasks/{upid}/status` poll loop                |
| replay: `requested interaction not found`       | the cassette predates a code change — re-record it                                                                            |

## Safety and teardown

- Run only against a **scratch/lab cluster**. Destructive tests are gated, but
  treat the whole suite as capable of mutating the node.
- Tests clean up their own scratch guests/volumes/rules. If a run is interrupted
  mid-lifecycle, check for a leftover VM/CT/volume with your scratch ID and
  remove it manually.
- Revoke the API token when you are done:
  `pveum user token remove root@pam sdk`.

## Reference

Test → phase → gates:

| Test                            | Phase | Required gates                                           |
| ------------------------------- | ----- | -------------------------------------------------------- |
| `TestVersionRoundTrip`          | 1     | (none beyond endpoint/token)                             |
| `TestComputeReads`              | 2     | (none)                                                   |
| `TestStorageReads`              | 3     | (none)                                                   |
| `TestClusterAndHAReads`         | 4     | (none)                                                   |
| `TestNetworkReads`              | 5     | (none)                                                   |
| `TestAccessReads`               | 6     | (none)                                                   |
| `TestQEMULifecycle`             | 2     | `PVE_TEST_STORAGE`, `PVE_TEST_VMID`                      |
| `TestLXCLifecycle`              | 2     | `PVE_TEST_STORAGE`, `PVE_TEST_LXC_VMID`, `…_TEMPLATE`    |
| `TestISOUpload`                 | 3     | `PVE_TEST_ISO_STORAGE`, `PVE_TEST_ISO_PATH`              |
| `TestResourceAffinityPlacement` | 4     | `PVE_TEST_PLACEMENT_VMID_1`, `PVE_TEST_PLACEMENT_VMID_2` |
| `TestConsoleMint`               | 6     | `PVE_TEST_STORAGE`, `PVE_TEST_CONSOLE_VMID`              |
| `TestConsoleRFB`                | 6     | `PVE_TEST_STORAGE`, `PVE_TEST_CONSOLE_VMID`              |

Command cheat-sheet:

```sh
go vet -tags=integration ./proxmox/integration/...          # compile only
go test -tags=integration ./proxmox/integration/... -run 'Reads|Version' -v   # read-only
go test -tags=integration ./proxmox/integration/... -v      # full suite
PVE_RECORD=1 go test -tags=integration ./proxmox/integration/... -run … -v    # record
go test ./proxmox/integration/... -run 'Redact|RecordReplay' -v               # guard redaction (no node)
```
