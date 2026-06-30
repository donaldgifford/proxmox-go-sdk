package ssh

import (
	"time"

	gossh "golang.org/x/crypto/ssh"
)

const (
	// defaultPort is the SSH port used when WithPort is not given.
	defaultPort = 22
	// defaultTimeout bounds the TCP dial and SSH handshake when WithTimeout is
	// not given.
	defaultTimeout = 30 * time.Second
)

// Option configures a Client.
type Option func(*clientConfig)

// clientConfig is the resolved configuration a Client connects with. Host-key
// verification is mandatory: Connect fails unless one of WithKnownHostsFile,
// WithHostKey, or WithHostKeyCallback is set.
type clientConfig struct {
	user            string
	port            int
	timeout         time.Duration
	authMethods     []gossh.AuthMethod
	privateKeyPEM   []byte
	knownHostsPath  string
	fixedHostKey    gossh.PublicKey
	hostKeyCallback gossh.HostKeyCallback
	dial            dialFunc
}

func defaultConfig() clientConfig {
	return clientConfig{port: defaultPort, timeout: defaultTimeout}
}

// WithUser sets the SSH username (typically a PAM account on the PVE node).
func WithUser(user string) Option {
	return func(c *clientConfig) { c.user = user }
}

// WithPort overrides the SSH port (default 22).
func WithPort(port int) Option {
	return func(c *clientConfig) { c.port = port }
}

// WithTimeout bounds the TCP dial and SSH handshake (default 30s).
func WithTimeout(d time.Duration) Option {
	return func(c *clientConfig) { c.timeout = d }
}

// WithPassword authenticates with a password (PAM).
func WithPassword(password string) Option {
	return func(c *clientConfig) {
		c.authMethods = append(c.authMethods, gossh.Password(password))
	}
}

// WithPrivateKey authenticates with a PEM-encoded private key (OpenSSH or
// PKCS#8). The key is parsed at Connect time so a malformed key surfaces there.
func WithPrivateKey(pemBytes []byte) Option {
	return func(c *clientConfig) { c.privateKeyPEM = pemBytes }
}

// WithKnownHostsFile verifies the server against an OpenSSH known_hosts file.
// This is the recommended production setting.
func WithKnownHostsFile(path string) Option {
	return func(c *clientConfig) { c.knownHostsPath = path }
}

// WithHostKey pins a single expected server public key. Useful when the key is
// known out of band (and in tests against an ephemeral server).
func WithHostKey(key gossh.PublicKey) Option {
	return func(c *clientConfig) { c.fixedHostKey = key }
}

// WithHostKeyCallback sets the host-key verification callback directly — the
// escape hatch for advanced policies. Supplying gossh.InsecureIgnoreHostKey
// here disables verification; that is the caller's explicit, auditable choice.
func WithHostKeyCallback(cb gossh.HostKeyCallback) Option {
	return func(c *clientConfig) { c.hostKeyCallback = cb }
}

// withDialFunc injects the connection dialer; unexported, used by tests to wire
// an in-process server over net.Pipe.
func withDialFunc(d dialFunc) Option {
	return func(c *clientConfig) { c.dial = d }
}
