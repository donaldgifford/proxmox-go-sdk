package qemu

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// Guest-agent operations require the QEMU guest agent to be installed and
// enabled in the VM. Under PVE 9.x's fine-grained agent privileges, the calling
// token needs the relevant VM.GuestAgent.* privilege (e.g. VM.GuestAgent.Audit
// to read, VM.GuestAgent.Unrestricted to exec arbitrary commands); a missing
// privilege surfaces as pverr.ErrForbidden.

const (
	agentPollInitial = 100 * time.Millisecond
	agentPollMax     = 2 * time.Second
)

// AgentExecStatus is the result of GET /agent/exec-status: the state of a
// command started by AgentExec.
type AgentExecStatus struct {
	Exited       types.PVEBool `json:"exited"`
	ExitCode     int           `json:"exitcode"`
	Signal       int           `json:"signal,omitempty"`
	OutData      string        `json:"out-data,omitempty"`
	ErrData      string        `json:"err-data,omitempty"`
	OutTruncated types.PVEBool `json:"out-truncated,omitempty"`
	ErrTruncated types.PVEBool `json:"err-truncated,omitempty"`
}

// agentExecResponse is the result of POST /agent/exec.
type agentExecResponse struct {
	PID int `json:"pid"`
}

// AgentPing checks that the guest agent is responsive. It returns nil on a
// successful ping and a classified error (e.g. pverr.ErrForbidden) otherwise.
func (s *Service) AgentPing(ctx context.Context, vmid int) error {
	if err := s.c.DoRequest(ctx, http.MethodPost, s.vmPath(vmid)+"/agent/ping", nil, nil); err != nil {
		return fmt.Errorf("qemu.AgentPing: %w", err)
	}
	return nil
}

// AgentExec starts command (program plus arguments) in the guest via the agent
// and returns the guest PID. Poll AgentExecStatus with that PID for the result,
// or use AgentExecWait to do both.
func (s *Service) AgentExec(ctx context.Context, vmid int, command []string) (int, error) {
	if len(command) == 0 {
		return 0, fmt.Errorf("qemu.AgentExec: command: %w", errMissingField)
	}
	body := url.Values{"command": command}
	var res agentExecResponse
	if err := s.c.DoRequest(ctx, http.MethodPost, s.vmPath(vmid)+"/agent/exec", body, &res); err != nil {
		return 0, fmt.Errorf("qemu.AgentExec: %w", err)
	}
	return res.PID, nil
}

// AgentExecStatus reports the state of the command running under pid (as
// returned by AgentExec).
func (s *Service) AgentExecStatus(ctx context.Context, vmid, pid int) (*AgentExecStatus, error) {
	var st AgentExecStatus
	path := s.vmPath(vmid) + "/agent/exec-status?pid=" + strconv.Itoa(pid)
	if err := s.c.DoRequest(ctx, http.MethodGet, path, nil, &st); err != nil {
		return nil, fmt.Errorf("qemu.AgentExecStatus: %w", err)
	}
	return &st, nil
}

// AgentExecWait runs command in the guest and polls (with capped backoff) until
// it exits, returning the final status. It stops early if ctx is cancelled.
func (s *Service) AgentExecWait(ctx context.Context, vmid int, command []string) (*AgentExecStatus, error) {
	pid, err := s.AgentExec(ctx, vmid, command)
	if err != nil {
		return nil, err
	}
	delay := agentPollInitial
	for {
		st, err := s.AgentExecStatus(ctx, vmid, pid)
		if err != nil {
			return nil, err
		}
		if st.Exited.Bool() {
			return st, nil
		}
		if serr := sleepCtx(ctx, delay); serr != nil {
			return nil, fmt.Errorf("qemu.AgentExecWait: %w", serr)
		}
		delay *= 2
		if delay > agentPollMax {
			delay = agentPollMax
		}
	}
}

// sleepCtx waits for d or until ctx is done, returning ctx.Err on cancellation.
func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
