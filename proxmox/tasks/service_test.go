package tasks

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
)

// fastService returns a Service polling on a millisecond cadence so waiter
// tests do not sleep on the default 1s interval.
func fastService(t *testing.T, srv *httptest.Server) *Service {
	t.Helper()
	c, err := api.New(srv.URL, api.TokenCredentials("root@pam!test", "secret"))
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}
	return NewService(c, WithWaitPolicy(WaitPolicy{
		Initial: time.Millisecond,
		Max:     5 * time.Millisecond,
		Factor:  1.5,
	}))
}

func TestStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"data":{"upid":"`+testUPID+`","node":"pve","type":"qmstart","status":"stopped","exitstatus":"OK"}}`)
	}))
	defer srv.Close()

	st, err := fastService(t, srv).Status(context.Background(), Ref{Node: "pve", UPID: testUPID})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !st.Done() || !st.OK() {
		t.Errorf("Status = %+v, want done+OK", st)
	}
}

func TestWaitSuccess(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&calls, 1) < 3 {
			io.WriteString(w, `{"data":{"upid":"`+testUPID+`","status":"running"}}`)
			return
		}
		io.WriteString(w, `{"data":{"upid":"`+testUPID+`","status":"stopped","exitstatus":"OK"}}`)
	}))
	defer srv.Close()

	st, err := fastService(t, srv).Wait(context.Background(), Ref{Node: "pve", UPID: testUPID})
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !st.OK() {
		t.Errorf("Wait status = %+v, want OK", st)
	}
	if got := atomic.LoadInt32(&calls); got < 3 {
		t.Errorf("polled %d times, want >= 3 (should have seen running first)", got)
	}
}

func TestWaitWarningsIsSuccess(t *testing.T) {
	// PVE finishes some tasks (routinely an LXC create) as "WARNINGS: N": the
	// operation completed, so Wait must treat it as success, not ErrTaskFailed.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"data":{"upid":"`+testUPID+`","status":"stopped","exitstatus":"WARNINGS: 1"}}`)
	}))
	defer srv.Close()

	st, err := fastService(t, srv).Wait(context.Background(), Ref{Node: "pve", UPID: testUPID})
	if err != nil {
		t.Fatalf("Wait on a WARNINGS task = %v, want success", err)
	}
	if !st.OK() {
		t.Errorf("OK() = false on %q, want true", st.ExitStatus)
	}
	if !st.Warnings() {
		t.Errorf("Warnings() = false on %q, want true", st.ExitStatus)
	}
}

func TestWaitFailureCarriesLog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/status"):
			io.WriteString(w, `{"data":{"upid":"`+testUPID+`","status":"stopped","exitstatus":"command 'qm start 100' failed: exit code 1"}}`)
		case strings.HasSuffix(req.URL.Path, "/log"):
			io.WriteString(w, `{"data":[{"n":1,"t":"starting VM"},{"n":2,"t":"kvm: failed to find romfile"}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	st, err := fastService(t, srv).Wait(context.Background(), Ref{Node: "pve", UPID: testUPID})
	if !errors.Is(err, pverr.ErrTaskFailed) {
		t.Fatalf("Wait error = %v, want ErrTaskFailed", err)
	}
	if st.OK() {
		t.Error("status reported OK on a failed task")
	}

	var pe *pverr.Error
	if !errors.As(err, &pe) {
		t.Fatalf("error is not *pverr.Error: %v", err)
	}
	if pe.UPID != testUPID {
		t.Errorf("UPID = %q, want %q", pe.UPID, testUPID)
	}
	if !strings.Contains(pe.Message, "romfile") {
		t.Errorf("Message = %q, want it to carry the last log line", pe.Message)
	}
}

func TestWaitForCustomCondition(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"data":{"upid":"`+testUPID+`","status":"running"}}`)
	}))
	defer srv.Close()

	// Stop as soon as we observe the running state — exercises WaitFor without
	// the Wait failure path.
	st, err := fastService(t, srv).WaitFor(context.Background(), Ref{Node: "pve", UPID: testUPID}, func(s *Status) bool { return s.Running() })
	if err != nil {
		t.Fatalf("WaitFor: %v", err)
	}
	if !st.Running() {
		t.Errorf("WaitFor status = %+v, want running", st)
	}
}

func TestLog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"data":[{"n":1,"t":"line one"},{"n":2,"t":"line two"}]}`)
	}))
	defer srv.Close()

	lines, err := fastService(t, srv).Log(context.Background(), Ref{Node: "pve", UPID: testUPID})
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if len(lines) != 2 || lines[1].Text != "line two" || lines[1].Number != 2 {
		t.Errorf("Log = %+v, want two lines", lines)
	}
}

func TestStatusInvalidRef(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()

	if _, err := fastService(t, srv).Status(context.Background(), Ref{UPID: testUPID}); err == nil {
		t.Error("Status with empty Node = nil error, want error")
	}
}

func TestWaitContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"data":{"upid":"`+testUPID+`","status":"running"}}`)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := fastService(t, srv).Wait(ctx, Ref{Node: "pve", UPID: testUPID})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Wait error = %v, want context.DeadlineExceeded", err)
	}
}
