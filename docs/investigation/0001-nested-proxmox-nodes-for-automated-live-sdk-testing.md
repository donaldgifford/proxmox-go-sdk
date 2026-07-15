---
id: INV-0001
title: "Nested Proxmox nodes for automated live SDK testing"
status: Concluded
author: Donald Gifford
created: 2026-07-05
---

<!-- markdownlint-disable-file MD025 MD041 -->

# INV 0001: Nested Proxmox nodes for automated live SDK testing

**Status:** Concluded **Author:** Donald Gifford **Date:** 2026-07-05 (concluded
2026-07-13)

<!--toc:start-->

- [Question](#question)
- [Hypothesis](#hypothesis)
- [Context](#context)
- [Approach](#approach)
  - [Phase 1: Terraform and a bootstrap script](#phase-1-terraform-and-a-bootstrap-script)
  - [Phase 2: Dogfood the SDK as the provisioner](#phase-2-dogfood-the-sdk-as-the-provisioner)
- [Environment](#environment)
- [Findings (desk analysis, not yet validated)](#findings-desk-analysis-not-yet-validated)
  - [Nested virtualization works with two host knobs](#nested-virtualization-works-with-two-host-knobs)
  - [Performance is adequate for API and lifecycle testing](#performance-is-adequate-for-api-and-lifecycle-testing)
  - [HA and SDN placement need a multi-node nested cluster](#ha-and-sdn-placement-need-a-multi-node-nested-cluster)
  - [Credential bootstrapping is the fiddly part](#credential-bootstrapping-is-the-fiddly-part)
- [Open questions](#open-questions)
- [Conclusion](#conclusion)
- [Recommendation](#recommendation)
- [References](#references)
<!--toc:end-->

## Question

Can we stand up **ephemeral, nested Proxmox VE nodes on our existing cluster**,
automatically, to run the SDK's live integration suite (and capture `go-vcr`
cassettes) — and eventually have the SDK **provision its own test node**
(dogfooding)? Concretely:

1. Does PVE-in-PVE (nested virtualization) work well enough to exercise the
   SDK's REST surface, guest lifecycle, storage, HA, and SDN?
2. Can a **template → clone → test → destroy** pipeline be automated
   (Terraform + a bootstrap script) so CI can run the currently live-only
   acceptance criteria?
3. Is it practical to later swap the provisioning step to use `proxmox-go-sdk`
   itself, so the SDK creates the very node it is tested against?

## Hypothesis

Yes to all three, with caveats. Nested KVM is a well-supported, common lab
pattern; a clone-per-run pipeline is standard Terraform/Packer territory; and
the SDK already exposes clone/start/wait/delete, so it can plausibly replace the
provisioner. The likely friction points are **nested-VM boot performance**,
**needing a multi-node nested cluster for HA/SDN placement**, and
**bootstrapping an API token** into a fresh node.

## Context

Phases 1–6 of the SDK are implementation-complete and mock-verified, but every
phase carries **live-only acceptance criteria** that remain
**written-but-unverified** — there is no live 9.x node or recorded cassette in
the dev environment (IMPL-0001, Testing Plan). The build-tagged integration
suite (`proxmox/integration/`) and the new **`go-vcr` record/replay harness**
exist and are proven against `mockpve`, but real capture is blocked on a
reachable node.

This investigation asks whether we can remove that blocker with infrastructure
we already own (a Proxmox cluster) rather than a dedicated bare-metal test node
— turning "live-only, unverified" into something CI can exercise on demand, and
feeding the cassette corpus (OQ-4/5/10) that lets CI later replay without a
node.

**Triggered by:** IMPL-0001 (live-only Success Criteria); the go-vcr recording
harness (PR that adds `proxmox/integration/recorder_test.go` + `TESTING.md`).

## Approach

Framed as two phases: prove the pipeline with off-the-shelf tooling first, then
dogfood the SDK once the shape is known.

### Phase 1: Terraform and a bootstrap script

Prove a single ephemeral node end to end:

1. **Enable nested virt** on the physical host(s): `options kvm-intel nested=1`
   (or `kvm-amd`), confirm `/sys/module/kvm_intel/parameters/nested` is `Y`.
2. **Build a PVE template** — install PVE 9.2 into a VM once (headless, via
   `proxmox-auto-install-assistant` + an answer file), enable the
   no-subscription repo, install `qemu-guest-agent`, pre-create an API token,
   convert to a template. Optionally automate the build with
   `packer-plugin-proxmox`.
3. **Clone per run** with Terraform (`bpg/proxmox` provider): clone the template
   with CPU type `host`, boot, wait for `:8006`, read the IP via guest-agent /
   cloud-init.
4. **Run the suite** against the clone: export `PVE_ENDPOINT` / `PVE_TOKEN_*`,
   run `go test -tags=integration ./proxmox/integration/...` — with
   `PVE_RECORD=1` to capture cassettes. Work the acceptance-criteria checklist
   in `TESTING.md`.
5. **Destroy** the clone (`terraform destroy`). Review + commit the redacted
   cassettes.
6. For HA/SDN placement, repeat with a **2–3 node nested cluster** (clone N
   nodes, `pvecm` them into a cluster).

### Phase 2: Dogfood the SDK as the provisioner

Replace step 3's Terraform clone with the SDK itself: a small `cmd/` tool or a
`TestMain` that uses `c.QEMU(host).Clone(...)` → `Start` → poll the nested
node's `/version` (via this same SDK) → hand the endpoint to the integration
suite → `Delete` on teardown. The outer cluster is the bootstrap host, so the
only chicken-and-egg is "you have one real cluster to run against."

## Environment

Proposed target versions (nothing run yet):

| Component           | Version / Value                           |
| ------------------- | ----------------------------------------- |
| Physical PVE host   | 9.x with `kvm_intel`/`kvm_amd` nested=1   |
| Nested PVE template | PVE 9.2 (for `9.2+` gated ops)            |
| Guest CPU type      | `host` (exposes `vmx`/`svm`)              |
| Provisioner (P1)    | Terraform + `bpg/proxmox` provider        |
| Template builder    | `proxmox-auto-install-assistant` / Packer |
| Provisioner (P2)    | `proxmox-go-sdk` (this repo)              |
| Cassette capture    | `go-vcr` v4 harness (`PVE_RECORD=1`)      |

## Findings (desk analysis, not yet validated)

> No spike has been run yet — the following is desk research to be confirmed
> empirically in Phase 1. Update this section with command output and timings as
> the PoC is built.

### Nested virtualization works with two host knobs

PVE runs as a guest and hosts inner LXC + QEMU when (a) the host module has
`nested=1` and (b) the nested-PVE VM uses CPU type `host` (or a type with
`+vmx`/`+svm`). Inner LXC is near-native; inner QEMU uses nested KVM. Without
nested KVM, inner VMs can still boot under TCG (`kvm=0`) — correct but very
slow.

### Performance is adequate for API and lifecycle testing

The SDK's tests exercise the **REST surface and control plane** (config CRUD,
snapshots, storage ops, task waiters), not guest workloads. Those are unaffected
by nested overhead. The slow part is booting an inner QEMU VM in the compute
lifecycle test; LXC lifecycle is cheap. Expectation: a full read-only + LXC pass
is fast; the QEMU lifecycle adds the bulk of wall-clock. **To be measured.**

### HA and SDN placement need a multi-node nested cluster

A single nested node can be a one-node cluster (`pvecm create`), enough for
HA/SDN **config** reads/writes. But the Phase 4 criterion's live half —
_observing the scheduler honor_ a resource-affinity rule — needs ≥2 nodes. So
the HA/SDN portion requires cloning a **2–3 node nested cluster**, which the
clone-per-run model supports but at higher cost. The rest of the suite is fine
on one node.

### Credential bootstrapping is the fiddly part

The suite needs `PVE_TOKEN_ID`/`PVE_TOKEN_SECRET` for a fresh node. Options:
bake a `root@pam` token into the template (simplest, but a static secret in the
image), or run `pveum user token add` in a first-boot / cloud-init script and
surface the secret to the runner. The first-boot approach is cleaner for
ephemeral nodes and avoids a long-lived secret in the template.

## Open questions

- How long does a full nested QEMU lifecycle run take end to end? Is it
  CI-viable per-PR, or nightly-only?
- Template refresh: how do we keep the PVE 9.2 template current (Renovate-style)
  as minors ship?
- Storage backends: does a nested node need ZFS-on-virtual-disk to cover the
  ZFS/volume-chain-snapshot criteria, or is `local-lvm` enough for most?
- Do we capture cassettes from the nested node and commit them (CI replays, no
  node needed thereafter), or keep the nested node in the loop for CI? The
  cassette route is cheaper and matches OQ-5.

## Conclusion

**Answer: Provisionally yes — feasible in theory, empirically unvalidated.**
Nothing here contradicts a working pipeline, and every piece (nested virt,
clone-per-run, SDK clone/start/delete) is individually well-established. The
open risks are cost/time (nested QEMU boots; multi-node clusters for HA/SDN)
rather than feasibility. This stays **Open** until a Phase 1 PoC produces real
timings and a captured cassette.

**CONCLUDED 2026-07-13 — answer: yes to all three, validated on hardware.** The
pipeline this INV asked for exists as `pvelab` (INV-0002 → DESIGN-0002 →
IMPL-0002), with one deliberate re-sequencing: the Terraform Phase 1 was
**skipped** — by the time execution started, the SDK's provisioning ops were
already live-verified against r740a, so INV-0002 went straight to this INV's
Phase 2 (the SDK provisions its own test environment). Empirical answers to the
three sub-questions: (1) PVE-in-PVE exercises everything the suite needs —
unattended nested installs run ~4 min, a 3-node nested cluster reaches quorum in
under 5 min total, the HA scheduler places guests, and a real RFB byte stream
flows over `console.Connect`; (2) the template → clone → test → destroy pipeline
is automated (template build 4m04–4m20s once per minor; linked-clone lab in
~3m10s, ~33% faster than ISO installs; teardown leaves the host clean — proven
back-to-back); (3) the SDK provisions its own test environment in production
form — the dogfood recipes run a stable-pinned `pvelab` while branch code is
what gets tested. The Open questions resolved: cadence is on-demand (not per-PR
CI); templates are one-per-minor in the 9210–9219 sub-range, rebuilt on demand;
`local-zfs` on the nested nodes suffices (storage-level volume-chain snapshots
turned out not to exist as a PVE REST surface at all); and cassettes are
committed + replayed in CI (`just test-replay`), certified per PVE version in
`certification.yaml` (9.2-1, 9.2.2, 9.1.1 batches). Final findings live in
INV-0002; the settled methodology is DESIGN-0002 (Implemented).

## Recommendation

1. **Build the Phase 1 PoC** — one ephemeral nested node via Terraform +
   `bpg/proxmox` + a bootstrap script — and run the read-only, compute (LXC +
   QEMU), and storage criteria with `PVE_RECORD=1`. Record timings here.
2. If timings are acceptable, add a **2–3 node nested cluster** variant for the
   HA/SDN placement criteria.
3. **Then dogfood (Phase 2):** replace the Terraform clone with an SDK-driven
   provisioner and fold the captured, redacted cassettes into CI replay so the
   default suite needs no node.
4. Promote the settled decision into an ADR/DESIGN if we commit to it; update
   IMPL-0001's live-only criteria to point at the pipeline once it verifies
   them.

## References

- IMPL-0001 — capability ledger and live-only Success Criteria
  (`docs/impl/0001-proxmox-ve-9x-sdk-coverage.md`)
- `TESTING.md` — manual live-node walkthrough + `go-vcr` recording
- `proxmox/integration/` — build-tagged suite + `recorder_test.go` harness
- Proxmox VE — Nested Virtualization (PVE wiki)
- `bpg/terraform-provider-proxmox` — Terraform provider for PVE
- `hashicorp/packer-plugin-proxmox` — Packer builder for PVE templates
- `proxmox-auto-install-assistant` — unattended PVE ISO installer (PVE 8.2+)
