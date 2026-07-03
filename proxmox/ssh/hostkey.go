package ssh

import (
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// knownHostsCallback builds a HostKeyCallback that verifies the server against
// an OpenSSH known_hosts file.
func knownHostsCallback(path string) (gossh.HostKeyCallback, error) {
	return knownhosts.New(path)
}
