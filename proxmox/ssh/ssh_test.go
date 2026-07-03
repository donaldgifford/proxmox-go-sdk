package ssh

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pkg/sftp"
	gossh "golang.org/x/crypto/ssh"
)

const (
	testUser = "uploader"
	testPass = "s3cret"
)

// newTestClient builds a Client wired through an in-process SSH+SFTP server on a
// loopback TCP listener (buffered, so concurrent SFTP packets do not deadlock as
// they would on an unbuffered net.Pipe), pinning the server's ephemeral host
// key. The returned client is connected and registered for cleanup.
func newTestClient(t *testing.T) *Client {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	signer, err := gossh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}

	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, aerr := ln.Accept()
			if aerr != nil {
				return // listener closed.
			}
			go serveSFTP(conn, signer)
		}
	}()

	dial := func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, ln.Addr().String())
	}

	c := NewClient(
		WithUser(testUser),
		WithPassword(testPass),
		WithHostKey(signer.PublicKey()),
		withDialFunc(dial),
	)
	if err := c.Connect(context.Background(), "pve.test"); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// serveSFTP runs one SSH server connection that accepts a session channel and
// serves the SFTP subsystem against the real filesystem (tests upload into
// t.TempDir paths).
func serveSFTP(conn net.Conn, signer gossh.Signer) {
	cfg := &gossh.ServerConfig{
		PasswordCallback: func(meta gossh.ConnMetadata, pass []byte) (*gossh.Permissions, error) {
			if meta.User() == testUser && string(pass) == testPass {
				return nil, nil
			}
			return nil, errAuthDenied
		},
	}
	cfg.AddHostKey(signer)

	sc, chans, reqs, err := gossh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	defer func() { _ = sc.Close() }()
	go gossh.DiscardRequests(reqs)

	for newCh := range chans {
		if newCh.ChannelType() != "session" {
			_ = newCh.Reject(gossh.UnknownChannelType, "only sessions")
			continue
		}
		ch, requests, aerr := newCh.Accept()
		if aerr != nil {
			return
		}
		go serveSession(ch, requests)
	}
}

func serveSession(ch gossh.Channel, requests <-chan *gossh.Request) {
	for req := range requests {
		isSFTP := req.Type == "subsystem" && len(req.Payload) >= 4 &&
			string(req.Payload[4:]) == "sftp"
		_ = req.Reply(isSFTP, nil)
		if isSFTP {
			server, err := sftp.NewServer(ch)
			if err != nil {
				_ = ch.Close()
				return
			}
			_ = server.Serve()
			_ = ch.Close()
			return
		}
	}
}

var errAuthDenied = &authError{}

type authError struct{}

func (*authError) Error() string { return "authentication denied" }

func TestUploadSnippet(t *testing.T) {
	t.Parallel()
	c := newTestClient(t)
	dest := filepath.Join(t.TempDir(), "cloud-init.yaml")

	payload := "#cloud-config\nhostname: web\n"
	if err := c.UploadSnippet(context.Background(), dest, strings.NewReader(payload)); err != nil {
		t.Fatalf("UploadSnippet: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read uploaded file: %v", err)
	}
	if string(got) != payload {
		t.Errorf("uploaded content = %q, want %q", got, payload)
	}
}

func TestUploadBackup(t *testing.T) {
	t.Parallel()
	c := newTestClient(t)
	dest := filepath.Join(t.TempDir(), "vzdump-qemu-100.vma.zst")

	payload := "FAKE-VZDUMP-ARCHIVE"
	if err := c.UploadBackup(context.Background(), dest, strings.NewReader(payload)); err != nil {
		t.Fatalf("UploadBackup: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read uploaded file: %v", err)
	}
	if string(got) != payload {
		t.Errorf("uploaded content = %q, want %q", got, payload)
	}
}

func TestUploadRejectsRelativePath(t *testing.T) {
	t.Parallel()
	c := newTestClient(t)
	if err := c.UploadSnippet(context.Background(), "relative/path.yaml", strings.NewReader("x")); err == nil {
		t.Error("UploadSnippet(relative) error = nil, want non-nil")
	}
}

func TestUploadNotConnected(t *testing.T) {
	t.Parallel()
	c := NewClient(WithUser(testUser), WithPassword(testPass))
	err := c.UploadSnippet(context.Background(), "/tmp/x.yaml", strings.NewReader("x"))
	if err == nil {
		t.Fatal("UploadSnippet before Connect error = nil, want non-nil")
	}
}

func TestConnectRequiresHostKey(t *testing.T) {
	t.Parallel()
	c := NewClient(WithUser(testUser), WithPassword(testPass)) // no host-key verification.
	if err := c.Connect(context.Background(), "pve.test"); err == nil {
		t.Fatal("Connect without host-key config error = nil, want non-nil")
	}
}

func TestConnectRequiresAuth(t *testing.T) {
	t.Parallel()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := gossh.NewSignerFromKey(priv)
	c := NewClient(WithUser(testUser), WithHostKey(signer.PublicKey())) // no auth method.
	if err := c.Connect(context.Background(), "pve.test"); err == nil {
		t.Fatal("Connect without auth error = nil, want non-nil")
	}
}

func TestExecNotConnected(t *testing.T) {
	t.Parallel()
	c := NewClient(WithUser(testUser), WithPassword(testPass))
	if _, err := c.Exec(context.Background(), "uptime"); err == nil {
		t.Fatal("Exec before Connect error = nil, want non-nil")
	}
}
