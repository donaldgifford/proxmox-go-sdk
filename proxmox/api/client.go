package api

import (
	"context"
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
type Client interface {
	DoRequest(ctx context.Context, method, path string, body, out any) error
	ExpandPath(path string) string
	HTTP() *http.Client
}

// transport is the concrete Client; New returns it behind the interface.
var _ Client = (*transport)(nil)
