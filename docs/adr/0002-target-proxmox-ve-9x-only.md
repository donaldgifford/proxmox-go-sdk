---
id: ADR-0002
title: "Target Proxmox VE 9.x only"
status: Accepted
author: Donald Gifford
created: 2026-06-22
---

<!-- markdownlint-disable-file MD025 MD041 -->

# 0002. Target Proxmox VE 9.x only

<!--toc:start-->

- [0002. Target Proxmox VE 9.x only](#0002-target-proxmox-ve-9x-only)
  - [Status](#status)
  - [Context](#context)
  - [Decision](#decision)
  - [Consequences](#consequences)
    - [Positive](#positive)
    - [Negative](#negative)
    - [Neutral](#neutral)
  - [Alternatives Considered](#alternatives-considered)
  - [References](#references)
  <!--toc:end-->

## Status

Accepted

<!-- ID is a placeholder — set the next number for your docs/adr/ or run `docz update`. -->

## Context

ADR-0001 commits us to a standalone Proxmox SDK plus a consumer service. That
SDK has to pin a supported Proxmox VE range, and the range materially changes
the design — the API is unversioned within a major release, and the 8.x→9.x
boundary moved several things we depend on.

Proxmox VE 9.0 shipped August 2025 on Debian 13 "Trixie"; 9.1 and 9.2 followed.
The reworks across the 8→9 boundary that matter to us:

- **HA model replaced.** HA **groups** are deprecated and superseded by HA
  **rules** — node-affinity and resource-affinity (resource-to-node and
  resource-to-resource), driven by `ha-manager rules`. Old groups auto-migrate
  on upgrade. An SDK that supported 8.x would have to model _both_ HA paradigms.
- **Privilege model changed.** `VM.Monitor` was removed, a new `VM.Replicate`
  privilege governs replication jobs, and QEMU guest-agent commands got
  fine-grained privileges. Custom roles must be adapted. Supporting 8.x means
  carrying the old privilege vocabulary too.
- **Native scheduling matured.** The Cluster Resource Scheduler gained the
  static-load scheduler, and **9.2 added a Dynamic Load Balancer** (continuous
  within-cluster rebalancing of HA guests, tunable under Datacenter → HA → CRS).
  This is server-side and changes what the consumer should _not_ reimplement.
- **APT sources format changed** to DEB822 `.sources`; **GlusterFS storage was
  dropped**; `maxfiles` deprecated; base is Debian 13 / newer kernels / QEMU
  10→11.
- **Capabilities also differ across 9.x minors:** SDN Fabrics arrived in 9.0 and
  gained protocols through 9.2 (OpenFabric/OSPF → +WireGuard/BGP/IPv6 underlay);
  OCI-based LXC templates and an OpenTelemetry metrics exporter landed in 9.1;
  the Dynamic Load Balancer and in-place API-token secret rotation in 9.2.

Our own fleet runs 9.x, and we have no requirement to manage 8.x hosts.

## Decision

**Support Proxmox VE 9.x only. The floor is PVE 9.0; we do not support 8.x or
earlier.**

- Model **HA rules** (node/resource affinity), not the deprecated HA groups.
- Use the **9.x privilege vocabulary** (`VM.Replicate`, fine-grained agent
  privileges; no `VM.Monitor`).
- Assume PVE 9 defaults: DEB822 `.sources`, an always-present CRS, no GlusterFS.
- Treat **within-cluster** load-balancing/affinity as **PVE-native** (CRS +
  Dynamic Load Balancer) that the SDK wraps — the consumer only owns
  _cross-cluster_ orchestration PVE has no API for (reinforces the ADR-0001
  boundary).
- The SDK's `version` service still gates **per-minor**:
  `MinimumProxmoxVersion = 9.0`, with `Support*()` checks for 9.1/9.2-only
  capabilities (OCI LXC, Dynamic Load Balancer, newer fabric protocols,
  token-secret rotation). "9.x only" is a major-version floor, not a claim that
  all minors are identical.

## Consequences

### Positive

- **One HA model, one privilege model, one APT format, one storage matrix** —
  far less branching than straddling 8.x and 9.x.
- **We target the current API** Proxmox itself is evolving, not a legacy
  surface.
- **Within-cluster balancing is free** — we wrap PVE 9.2's Dynamic Load Balancer
  instead of reimplementing ProxLB-style logic inside a cluster.

### Negative

- **No coverage for 8.x hosts** — anyone still on 8.x is unsupported until they
  upgrade. Acceptable given our fleet and roadmap.
- **Intra-9.x drift still exists.** 9.0 ≠ 9.2 in real capabilities, so the
  `version`-gating work does not disappear; it just shrinks to a single major.

### Neutral

- The floor is revisited when PVE 10 appears or when a 9.x minor becomes the
  practical minimum (e.g. if we decide to require 9.2 for the Dynamic Load
  Balancer). Tracked as capability rows in the coverage doc (IMPL-0001).

## Alternatives Considered

- **Support PVE 8.x + 9.x.** Doubles the HA and privilege models and the storage
  matrix for users we don't have; rejected.
- **Require the latest minor only (e.g. 9.2+).** Cleaner still, but needlessly
  excludes 9.0/9.1 hosts; rejected in favor of a 9.0 floor with per-minor
  gating.
- **Pin one exact version.** Too brittle against Proxmox's steady minor cadence;
  rejected.

## References

- ADR-0001 — Separate the Proxmox SDK into its own repository (defines the
  `version` service and SDK/consumer boundary this decision feeds)
- IMPL-0001 — Proxmox VE 9.x SDK coverage (tracks per-capability/per-minor
  support)
- Proxmox VE Roadmap and 9.0/9.1/9.2 release notes — HA rules, CRS + Dynamic
  Load Balancer, SDN Fabrics, thick-LVM snapshot chains, privilege changes,
  GlusterFS removal, DEB822 sources
