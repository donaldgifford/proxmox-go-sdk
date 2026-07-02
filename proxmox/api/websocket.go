package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
)

// DoWebSocket opens a WebSocket to path and returns the post-handshake byte
// stream. It refreshes credentials and authorizes the request like a read, then
// issues a GET that requests the "websocket" upgrade; on a 101 Switching
// Protocols response the http.Transport hands back a body that is also writable,
// which is returned as the duplex stream. Any other status is classified as an
// error. It targets the primary endpoint and does not retry or fail over.
//
// The returned stream carries whatever PVE sends after the upgrade (WebSocket
// frames); de-framing and the console protocol are the caller's concern. Close
// it to release the underlying connection.
func (t *transport) DoWebSocket(ctx context.Context, path string) (io.ReadWriteCloser, error) {
	if t.creds.needsRefresh() {
		if err := t.creds.refresh(ctx, t.doRaw, false); err != nil {
			return nil, err
		}
	}

	expandedPath := t.ExpandPath(path)
	fullURL := t.conn.baseURL().String() + expandedPath

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("api: build websocket request: %w", err)
	}
	key, err := websocketKey()
	if err != nil {
		return nil, err
	}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", key)
	req.Header.Set("Sec-WebSocket-Protocol", "binary")
	if t.userAgent != "" {
		req.Header.Set("User-Agent", t.userAgent)
	}
	t.creds.authorize(req)

	resp, err := t.http.Do(req)
	if err != nil {
		return nil, pverr.ClassifyNetError(expandedPath, err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		return nil, readResponse(resp, expandedPath, nil)
	}

	stream, ok := resp.Body.(io.ReadWriteCloser)
	if !ok {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("api: websocket upgrade to %s did not yield a writable stream", expandedPath)
	}
	return stream, nil
}

// websocketKey returns a fresh base64 Sec-WebSocket-Key (16 random bytes) for
// the opening handshake.
func websocketKey() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("api: generate websocket key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(buf[:]), nil
}
