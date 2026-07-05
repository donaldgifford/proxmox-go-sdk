# Cassettes

Recorded `go-vcr` HTTP fixtures captured from a live Proxmox VE node.

- **How they are made:** run the integration suite with `PVE_RECORD=1`. Each
  test records to `<TestName>.yaml` here. See
  [TESTING.md](../../../../TESTING.md).
- **Redaction:** credentials (token secret, tickets, CSRF, passwords) are
  scrubbed to `REDACTED` before write, guarded by `TestRedactInteraction`.
- **Committing:** cassettes are git-ignored by default. Review a cassette, then
  `git add -f <name>.yaml` to commit it intentionally. Wiring committed
  cassettes into CI replay is a planned follow-up.
