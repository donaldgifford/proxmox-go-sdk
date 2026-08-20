// This file is deliberately NOT behind the `integration` build tag: the
// classification below is what decides whether a live run registers an ACME
// account or fails, and it got that decision wrong once already. Keeping it in
// the default build means CI checks it without a node.

package integration

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
)

// acmeAccountAbsent reports whether err means "that ACME account is not
// registered", as opposed to a real failure.
//
// PVE answers a missing account with 400 Parameter verification failed —
// `[name: ACME account config file 'sdk-staging' does not exist.]` — not 404,
// so errors.Is(err, pverr.ErrNotFound) does not fire. Found live 2026-08-19 on
// a freshly formed cluster, where the harness fataled on the first run against
// a cluster that had never registered one.
//
// The per-parameter detail is structured, so this reads Params["name"] rather
// than matching the whole message: PVE's prose can be reworded between
// releases, but the parameter it blames is the API's own vocabulary.
func acmeAccountAbsent(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, pverr.ErrNotFound) {
		return true
	}
	var perr *pverr.Error
	if !errors.As(err, &perr) || perr.Status != http.StatusBadRequest {
		return false
	}
	return strings.Contains(perr.Params["name"], "does not exist")
}

// TestACMEAccountAbsent pins the classification against the shape PVE actually
// returned, so the "register it then" path cannot regress into a hard failure
// on a cluster that has never held the account.
func TestACMEAccountAbsent(t *testing.T) {
	missing := &pverr.Error{
		Status:  http.StatusBadRequest,
		Message: "Parameter verification failed.",
		Params:  map[string]string{"name": "ACME account config file 'sdk-staging' does not exist."},
	}
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "the 400 PVE actually returns", err: missing, want: true},
		{name: "wrapped", err: fmt.Errorf("nodes.GetACMEAccount: %w", missing), want: true},
		{name: "a 404, should PVE ever send one", err: pverr.ErrNotFound, want: true},
		{name: "nil is not absence", err: nil, want: false},
		{
			// A 400 about a DIFFERENT parameter is a real error; treating it as
			// absence would register an account over the top of a genuine bug.
			name: "another parameter's 400",
			err: &pverr.Error{
				Status: http.StatusBadRequest,
				Params: map[string]string{"directory": "invalid URL"},
			},
			want: false,
		},
		{name: "permission denied", err: pverr.ErrForbidden, want: false},
		{name: "a plain error", err: errors.New("boom"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := acmeAccountAbsent(tt.err); got != tt.want {
				t.Errorf("acmeAccountAbsent(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
