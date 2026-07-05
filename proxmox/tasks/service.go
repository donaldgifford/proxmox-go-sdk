package tasks

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
)

// exitOK is the PVE exit status string for a task that succeeded cleanly.
const exitOK = "OK"

// warnPrefix is the exit status prefix PVE uses when a task completes but logs
// non-fatal warnings, e.g. "WARNINGS: 1". The operation still succeeded (an LXC
// create routinely finishes this way), so it counts as success, not failure.
const warnPrefix = "WARNINGS:"

// Status is a task's status as reported by GET /nodes/{node}/tasks/{upid}/status.
type Status struct {
	UPID string `json:"upid"`
	Node string `json:"node"`
	Type string `json:"type"`
	ID   string `json:"id"`
	User string `json:"user"`
	// State is "running" while the worker is active, "stopped" once it exits.
	State string `json:"status"`
	// ExitStatus is "OK" on success or an error string on failure; empty while
	// the task is still running.
	ExitStatus string `json:"exitstatus"`
	PID        int    `json:"pid"`
	StartTime  int64  `json:"starttime"`
}

// Running reports whether the task is still executing.
func (s *Status) Running() bool { return s.State == "running" }

// Done reports whether the task has exited (successfully or not).
func (s *Status) Done() bool { return s.State == "stopped" }

// OK reports whether the task exited successfully. PVE reports a clean success
// as "OK" and a success-with-non-fatal-warnings as "WARNINGS: N" (the operation
// still completed) — both count as success. Any other exit status is a real
// failure. Use [Status.Warnings] to distinguish the two successes.
func (s *Status) OK() bool {
	return s.ExitStatus == exitOK || strings.HasPrefix(s.ExitStatus, warnPrefix)
}

// Warnings reports whether the task completed but logged non-fatal warnings
// (exit status "WARNINGS: N"). Such a task is still [Status.OK].
func (s *Status) Warnings() bool { return strings.HasPrefix(s.ExitStatus, warnPrefix) }

// LogLine is one line of a task log from GET /nodes/{node}/tasks/{upid}/log.
type LogLine struct {
	Number int    `json:"n"`
	Text   string `json:"t"`
}

// WaitPolicy controls the waiter's poll cadence. The interval grows by Factor
// after each poll up to Max; the overall deadline is the caller's context.
type WaitPolicy struct {
	Initial time.Duration
	Max     time.Duration
	Factor  float64
}

// DefaultWaitPolicy polls every second, backing off by 1.5x to a 10s ceiling.
func DefaultWaitPolicy() WaitPolicy {
	return WaitPolicy{Initial: time.Second, Max: 10 * time.Second, Factor: 1.5}
}

func (p WaitPolicy) next(d time.Duration) time.Duration {
	grown := time.Duration(float64(d) * p.Factor)
	if grown > p.Max {
		return p.Max
	}
	return grown
}

// Service reads task status and logs and waits on completion over an api.Client.
type Service struct {
	c      api.Client
	policy WaitPolicy
}

// Option configures a Service.
type Option func(*Service)

// WithWaitPolicy overrides the default poll cadence.
func WithWaitPolicy(p WaitPolicy) Option {
	return func(s *Service) { s.policy = p }
}

// NewService returns a task Service bound to c.
func NewService(c api.Client, opts ...Option) *Service {
	s := &Service{c: c, policy: DefaultWaitPolicy()}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Status fetches the current status of the task r.
func (s *Service) Status(ctx context.Context, r Ref) (Status, error) {
	if err := r.valid(); err != nil {
		return Status{}, err
	}
	var st Status
	path := "/nodes/" + r.Node + "/tasks/" + r.UPID + "/status"
	if err := s.c.DoRequest(ctx, http.MethodGet, path, nil, &st); err != nil {
		return Status{}, err
	}
	return st, nil
}

// Log fetches the task log lines for r.
func (s *Service) Log(ctx context.Context, r Ref) ([]LogLine, error) {
	if err := r.valid(); err != nil {
		return nil, err
	}
	var lines []LogLine
	path := "/nodes/" + r.Node + "/tasks/" + r.UPID + "/log"
	if err := s.c.DoRequest(ctx, http.MethodGet, path, nil, &lines); err != nil {
		return nil, err
	}
	return lines, nil
}

// WaitFor polls the task status until cond reports true, then returns that
// status. It backs off per the WaitPolicy and stops when ctx is cancelled. cond
// takes *Status (not Status) so the 128-byte snapshot is not copied per poll —
// the repo's hugeParam rule passes large structs by pointer.
func (s *Service) WaitFor(ctx context.Context, r Ref, cond func(*Status) bool) (Status, error) {
	interval := s.policy.Initial
	for {
		st, err := s.Status(ctx, r)
		if err != nil {
			return Status{}, err
		}
		if cond(&st) {
			return st, nil
		}
		if err := sleep(ctx, interval); err != nil {
			return Status{}, err
		}
		interval = s.policy.next(interval)
	}
}

// Wait blocks until the task exits. A successful exit returns its final status;
// a non-OK exit returns an *pverr.Error wrapping pverr.ErrTaskFailed, carrying
// the UPID, exit status, and the tail of the task log.
func (s *Service) Wait(ctx context.Context, r Ref) (Status, error) {
	st, err := s.WaitFor(ctx, r, (*Status).Done)
	if err != nil {
		return st, err
	}
	if !st.OK() {
		failErr := s.failure(ctx, r, &st)
		return st, failErr
	}
	return st, nil
}

// failure builds the ErrTaskFailed error for a non-OK exit, best-effort
// attaching the last log line as the failure reason.
func (s *Service) failure(ctx context.Context, r Ref, st *Status) error {
	e := pverr.NewTaskFailed("tasks.Wait", st.UPID, st.ExitStatus)
	e.Path = "/nodes/" + r.Node + "/tasks/" + r.UPID + "/status"
	if lines, err := s.Log(ctx, r); err == nil && len(lines) > 0 {
		e.Message = st.ExitStatus + ": " + lines[len(lines)-1].Text
	}
	return e
}

// sleep waits for d or until ctx is cancelled, returning ctx.Err() if cancelled.
func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
