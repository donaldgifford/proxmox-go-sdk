# Contributing

Thanks for your interest in `proxmox-go-sdk` — an idiomatic Go SDK for Proxmox
VE 9.x. This guide covers how to get set up, the conventions we follow, and what
a mergeable pull request looks like.

For a deeper walkthrough of the local setup, the test model, and how to verify
against a live cluster, see [DEVELOPMENT.md](DEVELOPMENT.md). For using the SDK
as a consumer, see [USAGE.md](USAGE.md).

## Getting started

The toolchain is pinned with [mise](https://mise.jdx.dev/); everything else is
driven by [`just`](https://github.com/casey/just).

```sh
git clone https://github.com/donaldgifford/proxmox-go-sdk
cd proxmox-go-sdk
mise install          # installs the pinned Go + linters + tools
just                  # list available tasks
just build            # go build ./...
just test             # race detector + coverage
just lint             # go + yaml + actions + markdown linters
```

## Development workflow

1. **Branch** from `main`. Use a descriptive branch name (`feat/...`, `fix/...`,
   `docs/...`, `chore/...`).
2. **Write code and tests together.** Tests live next to the code they cover
   (`foo_test.go` beside `foo.go`) and run every exported operation against the
   in-memory `mockpve` responder — no live cluster required.
3. **Run `just fmt` and `just lint`** before committing.
4. **Run `just test`** (race) and make sure it is green.
5. **Open a pull request** and make sure CI passes.

## Commit messages

We use [Conventional Commits](https://www.conventionalcommits.org/). The commit
history feeds the generated `CHANGELOG.md` (via `git-cliff`), so the type and
scope matter:

```text
feat(qemu): add disk resize
fix(api): retry on 5xx with jitter
docs(usage): document capability gating
chore(deps): bump golang.org/x/crypto
```

- **Do not hand-edit `CHANGELOG.md`.** It is generated from commit messages and
  guarded by a drift check; edit `cliff.toml` if you need to change formatting.
- Sign your commits' intent through the type; `feat`/`fix`/`perf`/`refactor`
  appear in the changelog, `chore`/`ci`/`build` land under Miscellaneous.

## Pull request requirements

A PR is mergeable when:

- **CI is green** — build, lint, tests (race), govulncheck, Trivy, CodeQL, and
  the schema-drift check all pass.
- **A semver label is set** — one of `major`, `minor`, `patch`, or
  `dont-release`. This drives release tooling and is enforced by a required
  check.
- **New behavior is tested** against `mockpve`.
- **Docs are updated** when the public surface changes (`doc.go`, `USAGE.md`,
  package `Example`s).

## Code style

Write idiomatic Go. The repository follows the Uber Go Style Guide conventions
enforced by `golangci-lint`. Key project-specific rules:

- **`context.Context` is the first argument of every operation.** One `*Client`
  is safe for concurrent use.
- **Errors wrap with `%w`** and resolve to the SDK's taxonomy in `proxmox/pverr`
  (`*pverr.Error` + sentinels like `pverr.ErrNotFound`, `pverr.ErrUnsupported`).
  Consumers branch with `errors.Is` / `errors.As`.
- **A library must not configure global state.** No `slog.SetDefault`, no
  `init()` for behavior — wire everything through `NewClient` and functional
  options.
- **Public services live under `proxmox/`.** `proxmox/internal/` is for
  unexported helpers only; do not put SDK surface there.
- **Version-gate 9.x features** with `caps.Require(...)`, surface
  `pverr.ErrUnsupported`, and document the tech-preview status on the spec.

When unsure whether an unconfirmed PVE endpoint should ship, follow the
**honesty rule**: prefer a typed op with a capability gate and a documented
provisional path over a stub, and use a documented `pverr.ErrUnsupported` when
no REST endpoint is confirmed. Never fabricate working functionality.

## Reporting bugs and security issues

- **Bugs / features:** open a GitHub issue with a clear repro.
- **Security vulnerabilities:** do **not** open a public issue — follow
  [SECURITY.md](SECURITY.md).

## License

By contributing, you agree that your contributions are licensed under the
project's [Apache-2.0](LICENSE) license.
