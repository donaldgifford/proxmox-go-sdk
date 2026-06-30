package ssh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"

	"github.com/pkg/sftp"
	gossh "golang.org/x/crypto/ssh"
)

// closeQuietly closes c and discards the error. Taking an io.Closer keeps the
// discard within errcheck's allowed (io.Closer).Close set; it is used for the
// fire-and-forget cleanup closes whose errors are not actionable.
func closeQuietly(c io.Closer) { _ = c.Close() }

// dialFunc opens the underlying transport connection. The default dials TCP with
// a context; tests inject one end of a net.Pipe.
type dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// Client is an SSH/SFTP side-channel to one PVE node. It is not safe for
// concurrent use: Connect, use, then Close from a single goroutine. Construct it
// with NewClient (or the root client's SSH accessor).
type Client struct {
	cfg  clientConfig
	ssh  *gossh.Client
	sftp *sftp.Client
}

var _ io.Closer = (*Client)(nil)

// NewClient builds a disconnected Client. Call Connect before uploading.
func NewClient(opts ...Option) *Client {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Client{cfg: cfg}
}

// Connect opens the SSH and SFTP sessions to host (port from WithPort). It must
// be called once before the upload/exec methods. ctx bounds the TCP dial.
func (c *Client) Connect(ctx context.Context, host string) error {
	if c.ssh != nil {
		return errors.New("ssh: already connected")
	}
	hostKeyCallback, err := c.hostKeyCallback()
	if err != nil {
		return err
	}
	auth, err := c.authMethods()
	if err != nil {
		return err
	}

	clientCfg := &gossh.ClientConfig{
		User:            c.cfg.user,
		Auth:            auth,
		HostKeyCallback: hostKeyCallback,
		Timeout:         c.cfg.timeout,
	}
	addr := net.JoinHostPort(host, strconv.Itoa(c.cfg.port))

	dial := c.cfg.dial
	if dial == nil {
		dial = func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{Timeout: c.cfg.timeout}).DialContext(ctx, network, address)
		}
	}
	conn, err := dial(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("ssh.Connect: dial %s: %w", addr, err)
	}

	sshConn, chans, reqs, err := gossh.NewClientConn(conn, addr, clientCfg)
	if err != nil {
		closeQuietly(conn)
		return fmt.Errorf("ssh.Connect: handshake: %w", err)
	}
	c.ssh = gossh.NewClient(sshConn, chans, reqs)

	sftpClient, err := sftp.NewClient(c.ssh)
	if err != nil {
		closeQuietly(c.ssh)
		c.ssh = nil
		return fmt.Errorf("ssh.Connect: start sftp: %w", err)
	}
	c.sftp = sftpClient
	return nil
}

// Close tears down the SFTP and SSH sessions. It is safe to call on a client
// that never connected.
func (c *Client) Close() error {
	var sftpErr, sshErr error
	if c.sftp != nil {
		sftpErr = c.sftp.Close()
		c.sftp = nil
	}
	if c.ssh != nil {
		sshErr = c.ssh.Close()
		c.ssh = nil
	}
	return errors.Join(sftpErr, sshErr)
}

// hostKeyCallback resolves the verification policy: an explicit callback wins,
// then a pinned key, then a known_hosts file. With none set Connect refuses to
// proceed rather than silently skipping verification.
func (c *Client) hostKeyCallback() (gossh.HostKeyCallback, error) {
	switch {
	case c.cfg.hostKeyCallback != nil:
		return c.cfg.hostKeyCallback, nil
	case c.cfg.fixedHostKey != nil:
		return gossh.FixedHostKey(c.cfg.fixedHostKey), nil
	case c.cfg.knownHostsPath != "":
		cb, err := knownHostsCallback(c.cfg.knownHostsPath)
		if err != nil {
			return nil, fmt.Errorf("ssh.Connect: known_hosts: %w", err)
		}
		return cb, nil
	default:
		return nil, errors.New("ssh.Connect: no host-key verification configured " +
			"(use WithKnownHostsFile, WithHostKey, or WithHostKeyCallback)")
	}
}

// authMethods finalises the auth list, parsing any private key supplied via
// WithPrivateKey.
func (c *Client) authMethods() ([]gossh.AuthMethod, error) {
	methods := c.cfg.authMethods
	if len(c.cfg.privateKeyPEM) > 0 {
		signer, err := gossh.ParsePrivateKey(c.cfg.privateKeyPEM)
		if err != nil {
			return nil, fmt.Errorf("ssh.Connect: parse private key: %w", err)
		}
		methods = append(methods, gossh.PublicKeys(signer))
	}
	if len(methods) == 0 {
		return nil, errors.New("ssh.Connect: no authentication configured " +
			"(use WithPassword or WithPrivateKey)")
	}
	return methods, nil
}
