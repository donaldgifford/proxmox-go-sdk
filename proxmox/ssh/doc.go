// Package ssh is the SFTP/exec side-channel for the few Proxmox operations the
// REST API cannot do — uploading snippets and backup archives to a node's
// storage via SFTP under a PAM account, and running the occasional command over
// SSH.
//
// It is separate from the REST transport: the consumer supplies SSH credentials
// and host-key verification. Obtain a Client from the root client's SSH
// accessor (or NewClient), Connect to a node, use it, then Close:
//
//	sc := client.SSH(ssh.WithUser("root"), ssh.WithPassword(pw),
//		ssh.WithKnownHostsFile("/home/me/.ssh/known_hosts"))
//	if err := sc.Connect(ctx, "pve.example.com"); err != nil {
//		// ...
//	}
//	defer sc.Close()
//	err := sc.UploadSnippet(ctx, "/var/lib/vz/snippets/ci.yaml", r)
//
// Host-key verification is mandatory: Connect fails unless one of
// WithKnownHostsFile, WithHostKey, or WithHostKeyCallback is configured.
//
// A Client wraps a single connection and is not safe for concurrent use:
// Connect, use, then Close from one goroutine. Obtain a fresh Client per node.
//
// Live behaviour (real PAM auth, writing under /var/lib/vz) cannot be verified
// without a reachable node; unit tests exercise the client against an in-process
// SFTP server. See docs/design/0001-proxmox-sdk-package-layout.md.
package ssh
