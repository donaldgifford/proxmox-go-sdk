package mockpve

import (
	"net/http"
	"strconv"
)

// This file models the PVE-side backup surface (task 7): scheduled backup jobs
// (/cluster/backup) and immediate vzdump backups (/nodes/{node}/vzdump). Backup
// listing reuses the storage content route (seed with AddVolume, content
// "backup"); restore reuses the qemu/lxc create routes. RBD-style verify has no
// PVE endpoint, so none is registered.

// pbsState is the backup slice of the mock model, embedded in state and guarded
// by state.mu.
type pbsState struct {
	jobs map[string]*backupJobRecord // keyed by job id.
	seq  int                         // for auto-assigned job ids.
}

type backupJobRecord struct {
	ID       string
	Schedule string
	Storage  string
	Mode     string
	Enabled  bool
	Mailto   string
	Comment  string
}

type backupJobPayload struct {
	ID       string `json:"id"`
	Schedule string `json:"schedule,omitempty"`
	Storage  string `json:"storage,omitempty"`
	Mode     string `json:"mode,omitempty"`
	Enabled  int    `json:"enabled,omitempty"`
	Mailto   string `json:"mailto,omitempty"`
	Comment  string `json:"comment,omitempty"`
}

// AddBackupJob seeds a scheduled backup job. Call before serving.
func (s *Server) AddBackupJob(id, storage, schedule string) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	if s.st.pbs.jobs == nil {
		s.st.pbs.jobs = make(map[string]*backupJobRecord)
	}
	s.st.pbs.jobs[id] = &backupJobRecord{
		ID: id, Storage: storage, Schedule: schedule, Mode: "snapshot", Enabled: true,
	}
}

func (s *Server) registerPBSRoutes() {
	s.mux.HandleFunc("GET /api2/json/cluster/backup", s.handleBackupJobList)
	s.mux.HandleFunc("POST /api2/json/cluster/backup", s.handleBackupJobCreate)
	s.mux.HandleFunc("GET /api2/json/cluster/backup/{id}", s.handleBackupJobGet)
	s.mux.HandleFunc("PUT /api2/json/cluster/backup/{id}", s.handleBackupJobUpdate)
	s.mux.HandleFunc("DELETE /api2/json/cluster/backup/{id}", s.handleBackupJobDelete)
	s.mux.HandleFunc("POST /api2/json/nodes/{node}/vzdump", s.handleVzdump)
}

func (s *Server) handleBackupJobList(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.st.mu.Lock()
	out := make([]backupJobPayload, 0, len(s.st.pbs.jobs))
	for _, rec := range s.st.pbs.jobs {
		out = append(out, backupJobToPayload(rec))
	}
	s.st.mu.Unlock()
	s.writeData(w, out)
}

func (s *Server) handleBackupJobGet(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	id := r.PathValue("id")
	s.st.mu.Lock()
	rec := s.st.pbs.jobs[id]
	var payload backupJobPayload
	if rec != nil {
		payload = backupJobToPayload(rec)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchBackupJob)
		return
	}
	s.writeData(w, payload)
}

func (s *Server) handleBackupJobCreate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	if !s.parseForm(w, r) {
		return
	}
	if r.PostForm.Get("storage") == "" {
		s.writeError(w, http.StatusBadRequest, "missing storage")
		return
	}
	s.st.mu.Lock()
	if s.st.pbs.jobs == nil {
		s.st.pbs.jobs = make(map[string]*backupJobRecord)
	}
	id := r.PostForm.Get("id")
	if id == "" {
		s.st.pbs.seq++
		id = "backup-" + strconv.Itoa(s.st.pbs.seq)
	}
	s.st.pbs.jobs[id] = &backupJobRecord{
		ID: id, Storage: r.PostForm.Get("storage"), Schedule: r.PostForm.Get("schedule"),
		Mode: r.PostForm.Get("mode"), Enabled: r.PostForm.Get("enabled") != "0",
		Mailto: r.PostForm.Get("mailto"), Comment: r.PostForm.Get("comment"),
	}
	s.st.mu.Unlock()
	s.writeData(w, nil)
}

func (s *Server) handleBackupJobUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	if !s.parseForm(w, r) {
		return
	}
	id := r.PathValue("id")
	s.st.mu.Lock()
	rec := s.st.pbs.jobs[id]
	if rec != nil {
		applyBackupJobForm(rec, r)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchBackupJob)
		return
	}
	s.writeData(w, nil)
}

func applyBackupJobForm(rec *backupJobRecord, r *http.Request) {
	if v := r.PostForm.Get("schedule"); v != "" {
		rec.Schedule = v
	}
	if v := r.PostForm.Get("storage"); v != "" {
		rec.Storage = v
	}
	if v := r.PostForm.Get("mode"); v != "" {
		rec.Mode = v
	}
	if v := r.PostForm.Get("mailto"); v != "" {
		rec.Mailto = v
	}
	if v := r.PostForm.Get("comment"); v != "" {
		rec.Comment = v
	}
	if v := r.PostForm.Get("enabled"); v != "" {
		rec.Enabled = v == "1"
	}
}

func (s *Server) handleBackupJobDelete(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	id := r.PathValue("id")
	s.st.mu.Lock()
	rec := s.st.pbs.jobs[id]
	if rec != nil {
		delete(s.st.pbs.jobs, id)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchBackupJob)
		return
	}
	s.writeData(w, nil)
}

// handleVzdump starts an immediate backup and returns a worker task.
func (s *Server) handleVzdump(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	if !s.parseForm(w, r) {
		return
	}
	node := r.PathValue("node")
	if r.PostForm.Get("storage") == "" {
		s.writeError(w, http.StatusBadRequest, "missing storage")
		return
	}
	s.writeData(w, s.finishedTask(node, "vzdump", "vzdump"))
}

func backupJobToPayload(rec *backupJobRecord) backupJobPayload {
	return backupJobPayload{
		ID: rec.ID, Schedule: rec.Schedule, Storage: rec.Storage, Mode: rec.Mode,
		Enabled: boolToInt(rec.Enabled), Mailto: rec.Mailto, Comment: rec.Comment,
	}
}
