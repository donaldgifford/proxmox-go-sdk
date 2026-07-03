package pverr

import (
	"errors"
	"fmt"
	"net"
	"testing"
)

func TestClassifySentinels(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   PVEBody
		want   error
	}{
		{name: "not found", status: 404, want: ErrNotFound},
		{name: "conflict", status: 409, want: ErrConflict},
		{name: "forbidden", status: 403, want: ErrForbidden},
		{
			name:   "ticket expired",
			status: 401,
			body:   PVEBody{Message: "invalid PVE ticket: ticket expired"},
			want:   ErrTicketExpired,
		},
		{
			name:   "plain unauthorized",
			status: 401,
			body:   PVEBody{Message: "authentication failure"},
			want:   ErrUnauthorized,
		},
		{name: "internal error", status: 500, want: ErrTransient},
		{name: "pve 596", status: 596, want: ErrTransient},
		{name: "bad gateway", status: 502, want: ErrTransient},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Classify(tt.status, "/api2/json/x", tt.body, nil)
			if !errors.Is(err, tt.want) {
				t.Errorf("Classify(%d) = %v, want errors.Is %v", tt.status, err, tt.want)
			}
			if err.Status != tt.status {
				t.Errorf("Status = %d, want %d", err.Status, tt.status)
			}
		})
	}
}

// TestClassifyUnmappedClientError verifies a generic 4xx carries data but
// matches no sentinel.
func TestClassifyUnmappedClientError(t *testing.T) {
	err := Classify(400, "/api2/json/x", PVEBody{Message: "bad param"}, nil)
	for _, sentinel := range []error{ErrNotFound, ErrConflict, ErrTransient, ErrUnauthorized} {
		if errors.Is(err, sentinel) {
			t.Errorf("Classify(400) unexpectedly matched %v", sentinel)
		}
	}
	if err.Message != "bad param" {
		t.Errorf("Message = %q, want %q", err.Message, "bad param")
	}
}

func TestClassifyParamsPreserved(t *testing.T) {
	body := PVEBody{
		Message: "parameter verification failed",
		Errors:  map[string]string{"vmid": "value 5 is too low"},
	}
	err := Classify(400, "/api2/json/x", body, nil)
	if got := err.Params["vmid"]; got != "value 5 is too low" {
		t.Errorf("Params[vmid] = %q, want %q", got, "value 5 is too low")
	}
}

func TestClassifyNetError(t *testing.T) {
	err := ClassifyNetError("/api2/json/x", errors.New("connection refused"))
	if !errors.Is(err, ErrTransient) {
		t.Errorf("ClassifyNetError = %v, want errors.Is ErrTransient", err)
	}
	if err.Status != 0 {
		t.Errorf("Status = %d, want 0 for pre-response error", err.Status)
	}
}

// TestClassifyNetCause routes a net.Error cause to ErrTransient even on an
// otherwise-unmapped status.
func TestClassifyNetCause(t *testing.T) {
	cause := &net.OpError{Op: "dial", Err: errors.New("timeout")}
	err := Classify(0, "/api2/json/x", PVEBody{}, cause)
	if !errors.Is(err, ErrTransient) {
		t.Errorf("Classify with net cause = %v, want ErrTransient", err)
	}
}

func TestErrorUnwrapAndAs(t *testing.T) {
	base := Classify(404, "/api2/json/nodes/pve/qemu/100", PVEBody{Message: "not found"}, nil)
	wrapped := fmt.Errorf("qemu.Get: %w", base)

	if !errors.Is(wrapped, ErrNotFound) {
		t.Error("errors.Is through wrap failed")
	}

	var target *Error
	if !errors.As(wrapped, &target) {
		t.Fatal("errors.As did not find *Error through wrap")
	}
	if target.Status != 404 {
		t.Errorf("As target Status = %d, want 404", target.Status)
	}
}

func TestErrorMessageFormat(t *testing.T) {
	e := &Error{
		Op:      "qemu.Start",
		Status:  500,
		Message: "boom",
		Path:    "/api2/json/nodes/pve/qemu/100/status/start",
	}
	want := "qemu.Start: HTTP 500: boom (/api2/json/nodes/pve/qemu/100/status/start)"
	if got := e.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestNewTaskFailed(t *testing.T) {
	upid := "UPID:pve:00001234:..:qmstart:100:root@pam:"
	err := NewTaskFailed("qemu.Start", upid, "exit code 1")
	if !errors.Is(err, ErrTaskFailed) {
		t.Error("NewTaskFailed not errors.Is ErrTaskFailed")
	}
	if err.UPID != upid {
		t.Errorf("UPID = %q, want %q", err.UPID, upid)
	}
}

func TestHelperPredicates(t *testing.T) {
	if !IsNotFound(Classify(404, "/x", PVEBody{}, nil)) {
		t.Error("IsNotFound(404) = false")
	}
	if !IsTransient(Classify(503, "/x", PVEBody{}, nil)) {
		t.Error("IsTransient(503) = false")
	}
	if IsNotFound(Classify(409, "/x", PVEBody{}, nil)) {
		t.Error("IsNotFound(409) = true")
	}
}
