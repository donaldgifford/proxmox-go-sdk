package tasks

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// UPID is a decoded PVE worker identifier. PVE encodes every asynchronous task
// as a colon-delimited string:
//
//	UPID:<node>:<pid-hex>:<pstart-hex>:<starttime-hex>:<type>:<id>:<user>:
//
// e.g. "UPID:pve:000A1B2C:00ABCDEF:6489ABCD:qmstart:100:root@pam:".
type UPID struct {
	// Raw is the original UPID string, used verbatim in task API paths.
	Raw string
	// Node is the cluster node the task runs on.
	Node string
	// PID is the worker process ID.
	PID int
	// PStart is the process start counter (PVE uses it to disambiguate PIDs).
	PStart int
	// StartTime is when the task started (UTC).
	StartTime time.Time
	// Type is the worker type, e.g. "qmstart", "vzdump".
	Type string
	// ID is the resource the task acts on, e.g. "100"; may be empty.
	ID string
	// User is the initiating user, e.g. "root@pam".
	User string
}

// ParseUPID decodes a PVE UPID string. Trailing content after the user field
// (PVE appends a final colon) is ignored.
func ParseUPID(s string) (UPID, error) {
	parts := strings.Split(s, ":")
	if len(parts) < 8 || parts[0] != "UPID" {
		return UPID{}, fmt.Errorf("tasks: malformed UPID %q", s)
	}

	pid, err := strconv.ParseInt(parts[2], 16, 64)
	if err != nil {
		return UPID{}, fmt.Errorf("tasks: UPID %q: parse pid: %w", s, err)
	}
	pstart, err := strconv.ParseInt(parts[3], 16, 64)
	if err != nil {
		return UPID{}, fmt.Errorf("tasks: UPID %q: parse pstart: %w", s, err)
	}
	epoch, err := strconv.ParseInt(parts[4], 16, 64)
	if err != nil {
		return UPID{}, fmt.Errorf("tasks: UPID %q: parse starttime: %w", s, err)
	}

	return UPID{
		Raw:       s,
		Node:      parts[1],
		PID:       int(pid),
		PStart:    int(pstart),
		StartTime: time.Unix(epoch, 0).UTC(),
		Type:      parts[5],
		ID:        parts[6],
		User:      parts[7],
	}, nil
}

// Ref locates a task for the waiter and status calls. Node is authoritative for
// the API path (it is the node the task runs on, encoded in the UPID).
type Ref struct {
	Node string
	UPID string
}

// NewRef builds a Ref from a UPID string, deriving Node from the UPID. Callers
// that already hold the node may construct Ref{Node, UPID} directly.
func NewRef(upid string) (Ref, error) {
	u, err := ParseUPID(upid)
	if err != nil {
		return Ref{}, err
	}
	return Ref{Node: u.Node, UPID: upid}, nil
}

// valid reports whether the ref has both fields needed to build a path.
func (r Ref) valid() error {
	if r.Node == "" || r.UPID == "" {
		return fmt.Errorf("tasks: incomplete ref (node=%q upid=%q)", r.Node, r.UPID)
	}
	return nil
}
