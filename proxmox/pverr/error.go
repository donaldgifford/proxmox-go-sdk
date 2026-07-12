package pverr

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Sentinel errors classify a failure by kind. Branch on them with
// errors.Is(err, pverr.ErrNotFound) and friends; reach for the concrete
// *Error (errors.As) when you need the status code, message, or task UPID.
var (
	// ErrNotFound is the resource (guest, node, volume, …) does not exist.
	ErrNotFound = errors.New("proxmox: not found")
	// ErrConflict is the request conflicts with current state (e.g. a guest
	// that is already running, or a duplicate VMID).
	ErrConflict = errors.New("proxmox: conflict")
	// ErrUnauthorized is authentication failed or was missing.
	ErrUnauthorized = errors.New("proxmox: unauthorized")
	// ErrTicketExpired is the auth ticket is no longer valid; the transport
	// re-authenticates and replays once before surfacing this.
	ErrTicketExpired = errors.New("proxmox: ticket expired")
	// ErrForbidden is the caller authenticated but lacks the privilege.
	ErrForbidden = errors.New("proxmox: forbidden")
	// ErrTaskFailed is an asynchronous PVE task ran to completion with a
	// non-OK exit status. The *Error carries the UPID.
	ErrTaskFailed = errors.New("proxmox: task failed")
	// ErrUnsupported is the operation requires a newer PVE minor than the
	// connected cluster reports (see version.Capabilities).
	ErrUnsupported = errors.New("proxmox: unsupported on this PVE version")
	// ErrTransient is a retryable failure: a dial/connection error, timeout,
	// TLS handshake failure, or 5xx/596 response.
	ErrTransient = errors.New("proxmox: transient")
)

// Error is the structured error every SDK operation returns. Use errors.Is to
// test the sentinel kind and errors.As to read the fields.
type Error struct {
	// Op is the service operation, e.g. "qemu.Start". It is set by the
	// service layer; the transport leaves it empty.
	Op string
	// Path is the full API path the request targeted, e.g.
	// "/api2/json/nodes/pve/qemu/100/status/start".
	Path string
	// Status is the HTTP status code, or 0 for a pre-response transport error.
	Status int
	// Message is the human-readable PVE error message.
	Message string
	// Params holds per-parameter validation errors PVE returns, keyed by
	// parameter name. It is nil when there are none.
	Params map[string]string
	// UPID identifies the task when this wraps ErrTaskFailed.
	UPID string

	// err is the wrapped sentinel (and/or underlying cause).
	err error
}

// Error implements the error interface.
func (e *Error) Error() string {
	var b strings.Builder
	if e.Op != "" {
		b.WriteString(e.Op)
		b.WriteString(": ")
	}
	if e.Status != 0 {
		fmt.Fprintf(&b, "HTTP %d", e.Status)
	} else {
		b.WriteString("request failed")
	}
	if e.Message != "" {
		b.WriteString(": ")
		b.WriteString(e.Message)
	}
	if len(e.Params) > 0 {
		// PVE's per-parameter validation errors are the actionable half of a
		// "Parameter verification failed." response — render them (sorted for
		// stable output) instead of leaving them silently in Params (a bare
		// message hid the failing field on live 9.2, 2026-07-12).
		names := make([]string, 0, len(e.Params))
		for name := range e.Params {
			names = append(names, name)
		}
		sort.Strings(names)
		b.WriteString(" [")
		for i, name := range names {
			if i > 0 {
				b.WriteString("; ")
			}
			fmt.Fprintf(&b, "%s: %s", name, strings.TrimSpace(e.Params[name]))
		}
		b.WriteString("]")
	}
	if e.Path != "" {
		fmt.Fprintf(&b, " (%s)", e.Path)
	}
	return b.String()
}

// Unwrap returns the wrapped sentinel so errors.Is and errors.As traverse it.
func (e *Error) Unwrap() error { return e.err }

// IsTransient reports whether err is a retryable transient failure.
func IsTransient(err error) bool { return errors.Is(err, ErrTransient) }

// IsNotFound reports whether err indicates a missing resource.
func IsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }

// NewTaskFailed builds an ErrTaskFailed for a finished task whose exit status
// was not "OK". op is the originating operation, upid the task identifier, and
// exitStatus the PVE exit string.
func NewTaskFailed(op, upid, exitStatus string) *Error {
	return &Error{
		Op:      op,
		UPID:    upid,
		Message: exitStatus,
		err:     ErrTaskFailed,
	}
}
