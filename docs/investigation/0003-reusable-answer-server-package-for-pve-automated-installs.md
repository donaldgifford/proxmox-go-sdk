---
id: INV-0003
title: "Reusable answer-server package for PVE automated installs"
status: Open
author: Donald Gifford
created: 2026-07-12
---

<!-- markdownlint-disable-file MD025 MD041 -->

# INV 0003: Reusable answer-server package for PVE automated installs

**Status:** Open **Author:** Donald Gifford **Date:** 2026-07-12

<!--toc:start-->

- [Question](#question)
- [Hypothesis](#hypothesis)
- [Context](#context)
- [Approach](#approach)
- [Environment](#environment)
- [Findings](#findings)
  - [Observation 1 — reachability is the hard part, not the server](#observation-1--reachability-is-the-hard-part-not-the-server)
- [Conclusion](#conclusion)
- [Recommendation](#recommendation)
- [References](#references)
<!--toc:end-->

> **Parked on purpose.** This INV is a reminder, not active work: do not start
> it before IMPL-0002 concludes. It exists so the idea — and the live facts the
> IMPL-0002 acceptance runs will produce about it — have a home.

## Question

Should this repo ship a **reusable answer-server package** (importable, plus
optionally a runnable helper binary on the `mockpve` precedent) that provides
the primitives for Proxmox automated installation over HTTP
(`proxmox-auto-install-assistant prepare-iso --fetch-from http`): per-node
answer-file rendering, installer-request → node matching, and a hardened little
HTTP(S) listener — so a consumer provisioning real PVE fleets does not rebuild
what `pvelab` embeds?

## Hypothesis

Yes, eventually. The primitives already exist, unexported, inside
`cmd/pvelab/lab/answers.go` (`RenderAnswer` over an embedded `text/template`,
`AnswerServer` as an `http.Handler` with bounded request bodies and first-answer
introspection, matching by the DMI serial stamped at VM create, and the
serve-never-persist discipline for answers that carry a root password). A
production auto-install setup has the same shape of problem plus a few more
requirements (TLS + `--cert-fingerprint`, alternate matchers such as MAC
address, audit/logging of which machine fetched what) — and today the only home
for that logic is a `go run`-only dev tool, which nobody can import.

## Context

**Triggered by:** IMPL-0002 Phase 1 acceptance-run prep (2026-07-12) and the
`lab/answers.go` design (the 2026-07-10 DESIGN-0002 amendment: one http-mode ISO
per PVE version + an embedded answer server during `pvelab up`).

While preparing the first acceptance run, the baked `answer_url` surfaced the
operational reality: the URL is fetched **by the installing nodes**, so the
answer endpoint must be reachable from the install network. A workstation
running `pvelab up` from another VLAN generally is not — the natural posture is
to run the answer service _near the nodes_ (on the PVE host itself, or on some
always-up box inside the fabric). That posture is exactly what a production
deployment would want: a cheap, easy-to-run HTTP answer service for
`proxmox-auto-install-assistant`-prepared ISOs. Proxmox documents the
answer-file contract but ships no server; everyone building fleet installs
writes this small service themselves.

## Approach

When this is picked up (post-IMPL-0002):

1. Harvest the live facts the IMPL-0002 Phase 1 acceptance run records on the
   `lab/answers.go` ledger box: the installer POST payload shape (DMI serial
   field name), plain-HTTP vs HTTPS + `--cert-fingerprint` behaviour, and the
   reachability posture actually used (direct / tunnel / run-on-host).
2. Survey prior art (Proxmox's own tooling, community answer servers) to confirm
   the gap is real and worth owning.
3. Decide surface + home: this is not a PVE REST-client wrapper, so it does not
   obviously belong under the DESIGN-0001 service-package layout — candidate
   shapes are a `proxmox/autoinstall` package, a separate module, or
   package-plus-binary on the `mockpve` precedent (importable package, runnable
   `cmd/`). Needs an ADR for placement if confirmed.
4. Sketch the API: matcher strategies (SMBIOS serial, MAC, caller-supplied
   predicate), answer rendering (typed struct vs bring-your-own template),
   TLS/cert-fingerprint support, secret handling (answers carry the root
   password: serve, never persist or log), lifecycle (`Start`/`Shutdown`,
   served-request introspection).
5. If confirmed: write the DESIGN; if refuted: conclude with the pointer to
   whatever existing tool covers it.

## Environment

| Component               | Version / Value                                           |
| ----------------------- | --------------------------------------------------------- |
| Proxmox VE / assistant  | 9.2-1 (`proxmox-auto-install-assistant`, http fetch mode) |
| Existing implementation | `cmd/pvelab/lab/answers.go` (unexported, dev-tool only)   |

## Findings

### Observation 1 — reachability is the hard part, not the server

(2026-07-12, acceptance-run prep.) The server side is trivial — `pvelab`'s
embedded one is ~200 lines. The operational friction is entirely about _where it
runs_: the baked URL must be reachable from the install network, which pushes
the service toward the PVE host or a persistent in-fabric box rather than an
operator workstation. `nested.answer_listen` already binds all interfaces by
default, so running `pvelab` on the outer host needs no code change — evidence
that the primitives, packaged consumably, would cover the production posture
too. Confirmed 2026-07-12: the first live acceptance runs used exactly this
posture (binary + config on the PVE node), and the flow worked first try — six
installer fetches across two runs, matched by SMBIOS serial over plain HTTP.

## Conclusion

<!-- Deliberately open — this INV is parked until IMPL-0002 concludes. -->

**Answer:** —

## Recommendation

None yet. Revisit after IMPL-0002 is Completed and the acceptance-run facts are
recorded; then either promote to an ADR/DESIGN (package placement + API) or
conclude with the prior-art pointer.

## References

- IMPL-0002 — the `lab/answers.go` task (inline live-verify items) and the Phase
  1 acceptance run
  (`docs/impl/0002-dogfood-harness-buildout-pvelab-cluster-surface-p4p6-closure.md`)
- DESIGN-0002 — the 2026-07-10 http-mode/answer-server amendment
  (`docs/design/0002-dogfood-harness-pvelab-cli-nested-cluster-provisioning-and.md`)
- `cmd/pvelab/lab/answers.go` — the existing embedded implementation
- `proxmox/mockpve` + `cmd/mockpve` — the package-plus-binary precedent
- PVE wiki — Automated Installation (`proxmox-auto-install-assistant`)
