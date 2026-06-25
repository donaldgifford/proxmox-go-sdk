package mockpve

import (
	"net/http"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
)

// maxFormBytes caps the size of a parsed form body, guarding ParseForm against
// memory exhaustion (gosec G120).
const maxFormBytes = 1 << 20 // 1 MiB

// versionPayload is the data of GET /version.
type versionPayload struct {
	Version string `json:"version"`
	Release string `json:"release"`
	RepoID  string `json:"repoid"`
	Console string `json:"console,omitempty"`
}

// ticketPayload is the data of POST /access/ticket.
type ticketPayload struct {
	Ticket              string `json:"ticket"`
	CSRFPreventionToken string `json:"CSRFPreventionToken"`
	Username            string `json:"username"`
}

// taskStatusPayload is the data of GET /nodes/{node}/tasks/{upid}/status.
type taskStatusPayload struct {
	UPID       string `json:"upid"`
	Node       string `json:"node"`
	Type       string `json:"type"`
	ID         string `json:"id"`
	User       string `json:"user"`
	Status     string `json:"status"`
	ExitStatus string `json:"exitstatus,omitempty"`
	PID        int    `json:"pid"`
	StartTime  int64  `json:"starttime"`
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.st.mu.Lock()
	v := s.st.version
	s.st.mu.Unlock()
	s.writeData(w, versionPayload(v))
}

func (s *Server) handleTicket(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid form body")
		return
	}
	user := r.Form.Get("username")
	pass := r.Form.Get("password")

	s.st.mu.Lock()
	want, ok := s.st.users[user]
	authed := ok && want == pass
	ticket := "mock-ticket-" + user
	csrf := "mock-csrf-" + user
	if authed {
		s.st.tickets[ticket] = ticketRecord{Username: user, CSRF: csrf}
	}
	s.st.mu.Unlock()

	if !authed {
		s.writeError(w, http.StatusUnauthorized, "authentication failure")
		return
	}
	s.writeData(w, ticketPayload{Ticket: ticket, CSRFPreventionToken: csrf, Username: user})
}

func (s *Server) handleTaskStatus(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	upid := r.PathValue("upid")

	s.st.mu.Lock()
	rec, ok := s.lookupTask(node, upid)
	var payload taskStatusPayload
	if ok {
		status := "running"
		if rec.Stopped {
			status = "stopped"
		}
		payload = taskStatusPayload{
			UPID:       rec.UPID,
			Node:       rec.Node,
			Type:       rec.Type,
			ID:         rec.ID,
			User:       rec.User,
			Status:     status,
			ExitStatus: rec.ExitStatus,
			PID:        rec.PID,
			StartTime:  rec.StartTime.Unix(),
		}
	}
	s.st.mu.Unlock()

	if !ok {
		s.writeError(w, http.StatusNotFound, "no such task")
		return
	}
	s.writeData(w, payload)
}

func (s *Server) handleTaskLog(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	upid := r.PathValue("upid")

	s.st.mu.Lock()
	rec, ok := s.lookupTask(node, upid)
	var lines []tasks.LogLine
	if ok {
		lines = append(lines, rec.Log...)
	}
	s.st.mu.Unlock()

	if !ok {
		s.writeError(w, http.StatusNotFound, "no such task")
		return
	}
	s.writeData(w, lines)
}

// lookupTask returns the task record for node/upid. The caller must hold st.mu.
func (s *Server) lookupTask(node, upid string) (*taskRecord, bool) {
	n, ok := s.st.nodes[node]
	if !ok {
		return nil, false
	}
	rec, ok := n.tasks[upid]
	return rec, ok
}
