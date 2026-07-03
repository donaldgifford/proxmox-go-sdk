# Security Policy

`proxmox-go-sdk` is a Go library for Proxmox VE 9.x. This document explains
which versions receive security fixes, how to report a vulnerability, and the
security posture the SDK maintains for its consumers.

## Supported versions

The SDK is pre-1.0 and released by git tag; consumers pin a tag and `go get` it.
Security fixes land on `main` and in the next tagged release.

| Version       | Supported          |
| ------------- | ------------------ |
| latest `v0.x` | :white_check_mark: |
| older `v0.x`  | :x: (upgrade)      |

The SDK targets **Proxmox VE 9.0 and newer** only (ADR-0002). It is not tested
against, and does not support, Proxmox VE 8.x or earlier.

## Reporting a vulnerability

**Please do not open a public issue for security problems.**

Report privately through GitHub's private vulnerability reporting:

1. Go to the repository's **Security** tab.
2. Click **Report a vulnerability** to open a private security advisory.
3. Include the details below.

If you cannot use GitHub advisories, open a minimal public issue asking for a
private contact channel — without disclosing the vulnerability itself.

Please include:

- A description of the vulnerability and its impact.
- The affected version(s) or commit(s).
- Steps to reproduce, or a proof of concept.
- Any known mitigations or workarounds.

We aim to acknowledge a report within **3 business days** and to provide a
remediation timeline after triage. Please allow a reasonable disclosure window
before publishing details.

## Security posture

The SDK is a client library; it makes outbound calls to a Proxmox cluster you
configure. Some deliberate hardening choices:

- **TLS verification is on by default.** Certificate verification can only be
  disabled explicitly via `WithInsecureSkipVerify(true)` (for self-signed
  homelab clusters) — it is never off implicitly.
- **SSH host-key verification is mandatory.** The `proxmox/ssh` side-channel
  refuses to connect unless a host key is pinned (`WithKnownHostsFile` /
  `WithHostKey` / `WithHostKeyCallback`). There is no `InsecureIgnoreHostKey`
  escape hatch.
- **Credentials are never logged.** The SDK takes a consumer-supplied logger
  (`WithLogger`); it does not configure global logging and redacts secrets from
  any debug output.
- **No import-time side effects.** The library does not use `init()` for
  behavior; everything is wired explicitly through `NewClient`.

## Automated scanning

Every pull request runs, in CI:

- **govulncheck** — Go vulnerability database, symbol-reachability aware.
- **Trivy** — dependency and configuration scanning.
- **CodeQL** — static analysis for the Go code.
- **gosec** — via `golangci-lint`.
- **TruffleHog** — secret scanning on the diff.

A dependency vulnerability that is reachable from the SDK's code is treated as a
release blocker.
