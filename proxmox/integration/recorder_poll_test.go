package integration

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"
)

// TestRecorderRecordsEachPoll guards the recorder against the WithReplayableInteractions
// regression: a task-status poll loop makes many identical GETs to
// /tasks/{upid}/status, and if the recorder serves the first recording for all of
// them, the task is frozen at "running" and tasks.Wait spins to its deadline (the
// symptom that broke the live QEMU-lifecycle recording). The server here returns
// "running" for the first two calls then "stopped"; the recorder must hit the
// network every time (so the state actually advances), not replay the first hit.
func TestRecorderRecordsEachPoll(t *testing.T) {
	const polls = 5
	var seen int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		state := "running"
		if atomic.AddInt32(&seen, 1) >= 3 {
			state = "stopped"
		}
		fmt.Fprintf(w, `{"data":{"status":%q,"exitstatus":"OK"}}`, state)
	}))
	defer srv.Close()

	rec := newRecorder(t, filepath.Join(t.TempDir(), "poll"), recorder.ModeRecordOnly, http.DefaultTransport)
	defer func() { _ = rec.Stop() }()
	client := rec.GetDefaultClient()

	var last string
	for i := range polls {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/status", http.NoBody)
		if err != nil {
			t.Fatalf("req %d: %v", i, err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("req %d: %v", i, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		last = string(body)
	}

	if got := atomic.LoadInt32(&seen); got != polls {
		t.Errorf("server saw %d requests, want %d — recorder is replaying instead of recording each poll", got, polls)
	}
	if !strings.Contains(last, "stopped") {
		t.Errorf("final poll = %q, want it to reflect the advanced 'stopped' state", last)
	}
}
