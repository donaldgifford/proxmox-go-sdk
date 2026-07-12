// Package lab is the importable logic behind the pvelab CLI (DESIGN-0002 /
// IMPL-0002): YAML config loading + validation, auto-install ISO preparation
// on the outer host, per-node answer rendering and the embedded answer
// server, node-VM provisioning and readiness, cluster formation, teardown
// with blast-radius guards, and the state/env handoff to the integration
// suite. It follows the cmd/pve-schemadiff/schema precedent — unit-testable
// package under cmd/, consumed only by cmd/pvelab — and depends solely on the
// public proxmox/... SDK surface.
package lab
