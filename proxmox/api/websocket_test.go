package api_test

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
)

// wsMagic is the RFC 6455 handshake GUID.
const wsMagic = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// upgradeEchoServer answers a WebSocket upgrade with 101 and then echoes every
// byte the client sends back to it (raw, no framing — enough to prove the
// duplex plumbing of DoWebSocket).
func upgradeEchoServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") != "websocket" {
			http.Error(w, "not an upgrade", http.StatusBadRequest)
			return
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "no hijack", http.StatusInternalServerError)
			return
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			return
		}
		defer conn.Close()

		sum := sha1.Sum([]byte(r.Header.Get("Sec-WebSocket-Key") + wsMagic))
		accept := base64.StdEncoding.EncodeToString(sum[:])
		_, _ = buf.WriteString("HTTP/1.1 101 Switching Protocols\r\n" +
			"Upgrade: websocket\r\nConnection: Upgrade\r\n" +
			"Sec-WebSocket-Accept: " + accept + "\r\n\r\n")
		_ = buf.Flush()

		_, _ = io.Copy(conn, buf) // echo until the client closes.
	}))
}

func TestDoWebSocketEcho(t *testing.T) {
	t.Parallel()
	ts := upgradeEchoServer(t)
	defer ts.Close()

	c, err := api.New(ts.URL, api.TokenCredentials("root@pam!t", "secret"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	stream, err := c.DoWebSocket(context.Background(), "/nodes/pve/vncwebsocket?port=5900&vncticket=abc")
	if err != nil {
		t.Fatalf("DoWebSocket: %v", err)
	}
	defer stream.Close()

	want := []byte("hello vnc")
	if _, err := stream.Write(want); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := make([]byte, len(want))
	if _, err := io.ReadFull(stream, got); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("echo = %q, want %q", got, want)
	}
}

func TestDoWebSocketRejectsNon101(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer ts.Close()

	c, err := api.New(ts.URL, api.TokenCredentials("root@pam!t", "secret"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.DoWebSocket(context.Background(), "/nodes/pve/vncwebsocket"); err == nil {
		t.Fatal("DoWebSocket to a 403 endpoint: error = nil, want non-nil")
	}
}
