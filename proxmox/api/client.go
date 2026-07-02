package api

import (
	"context"
	"io"
	"net/http"
)

// Client is the low-level Proxmox VE transport. Every service depends on this
// interface (not the concrete type), which lets tests inject a double.
//
// DoRequest performs one complete PVE call: credential refresh, CSRF on
// writes, the {"data": …} envelope unwrap into out, types.PVEBool 0/1
// normalisation, error classification, and a single re-auth-and-replay on
// ticket expiry. ExpandPath normalises a relative path to the full
// /api2/json/… request path; it does not interpolate node or vmid (services
// build those). HTTP exposes the underlying client as an escape hatch.
//
// DoUpload streams a multipart/form-data POST (the caller supplies the body
// reader and its Content-Type, typically from a multipart.Writer fed by an
// io.Pipe) and unwraps the envelope into out. It applies the same auth and CSRF
// as a write but does NOT retry — an upload is not idempotent, so on failure the
// caller must restart it with a fresh reader.
//
// DoWebSocket opens a WebSocket to path (a GET that negotiates the 101 upgrade)
// and returns the post-handshake byte stream as an io.ReadWriteCloser for
// duplex use — the console service builds on it (Connect). It applies auth like
// a read and does not retry or fail over. The bytes on the returned stream are
// whatever PVE sends after the upgrade (WebSocket-framed); the framing and the
// console protocol on top are the caller's concern. Close it to release the
// connection.
type Client interface {
	DoRequest(ctx context.Context, method, path string, body, out any) error
	DoUpload(ctx context.Context, path string, body io.Reader, contentType string, out any) error
	DoWebSocket(ctx context.Context, path string) (io.ReadWriteCloser, error)
	ExpandPath(path string) string
	HTTP() *http.Client
}

// transport is the concrete Client; New returns it behind the interface.
var _ Client = (*transport)(nil)
