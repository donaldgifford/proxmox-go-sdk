package mockpve

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// haState is the HA slice of the mock model, embedded in state and guarded by
// state.mu. HA is cluster-scoped, so records are not keyed by node.
type haState struct {
	resources map[string]*haResourceRecord // keyed by SID, e.g. "vm:100".
}

// haResourceRecord is one guest under HA management.
type haResourceRecord struct {
	SID         string
	Type        string
	State       string
	MaxRestart  int
	MaxRelocate int
	Comment     string
}

// haResourcePayload mirrors GET /cluster/ha/resources entries.
type haResourcePayload struct {
	SID         string `json:"sid"`
	Type        string `json:"type,omitempty"`
	State       string `json:"state,omitempty"`
	MaxRestart  int    `json:"max_restart,omitempty"`
	MaxRelocate int    `json:"max_relocate,omitempty"`
	Comment     string `json:"comment,omitempty"`
}

// AddHAResource seeds a guest under HA management. Call before serving.
func (s *Server) AddHAResource(sid, state string) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	if s.st.ha.resources == nil {
		s.st.ha.resources = make(map[string]*haResourceRecord)
	}
	s.st.ha.resources[sid] = &haResourceRecord{SID: sid, Type: sidType(sid), State: state}
}

// sidType extracts the resource type ("vm"/"ct") from a SID like "vm:100".
func sidType(sid string) string {
	if i := strings.IndexByte(sid, ':'); i > 0 {
		return sid[:i]
	}
	return ""
}

func (s *Server) registerHARoutes() {
	s.mux.HandleFunc("GET /api2/json/cluster/ha/resources", s.handleHAResourceList)
	s.mux.HandleFunc("POST /api2/json/cluster/ha/resources", s.handleHAResourceCreate)
	s.mux.HandleFunc("GET /api2/json/cluster/ha/resources/{sid}", s.handleHAResourceGet)
	s.mux.HandleFunc("PUT /api2/json/cluster/ha/resources/{sid}", s.handleHAResourceUpdate)
	s.mux.HandleFunc("DELETE /api2/json/cluster/ha/resources/{sid}", s.handleHAResourceDelete)
}

func (s *Server) handleHAResourceList(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.st.mu.Lock()
	out := make([]haResourcePayload, 0, len(s.st.ha.resources))
	for _, rec := range s.st.ha.resources {
		out = append(out, haResourceToPayload(rec))
	}
	s.st.mu.Unlock()
	s.writeData(w, out)
}

func (s *Server) handleHAResourceGet(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	sid := r.PathValue("sid")
	s.st.mu.Lock()
	rec := s.st.ha.resources[sid]
	var payload haResourcePayload
	if rec != nil {
		payload = haResourceToPayload(rec)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchHAResource)
		return
	}
	s.writeData(w, payload)
}

// handleHAResourceCreate places a guest under HA management. The write is
// synchronous in PVE (data null, no task).
func (s *Server) handleHAResourceCreate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidForm)
		return
	}
	sid := r.PostForm.Get("sid")
	if sid == "" {
		s.writeError(w, http.StatusBadRequest, "missing sid")
		return
	}
	rec := &haResourceRecord{
		SID: sid, Type: sidType(sid), State: r.PostForm.Get("state"),
		Comment: r.PostForm.Get("comment"),
	}
	if rec.State == "" {
		rec.State = "started"
	}
	if v, err := strconv.Atoi(r.PostForm.Get("max_restart")); err == nil {
		rec.MaxRestart = v
	}
	if v, err := strconv.Atoi(r.PostForm.Get("max_relocate")); err == nil {
		rec.MaxRelocate = v
	}
	s.st.mu.Lock()
	if s.st.ha.resources == nil {
		s.st.ha.resources = make(map[string]*haResourceRecord)
	}
	s.st.ha.resources[sid] = rec
	s.st.mu.Unlock()
	s.writeData(w, nil)
}

// handleHAResourceUpdate mutates an existing resource. Synchronous (data null).
func (s *Server) handleHAResourceUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	sid := r.PathValue("sid")
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidForm)
		return
	}
	s.st.mu.Lock()
	rec := s.st.ha.resources[sid]
	if rec != nil {
		applyHAResourceForm(rec, r.PostForm)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchHAResource)
		return
	}
	s.writeData(w, nil)
}

// applyHAResourceForm applies a PUT form's fields to rec. The caller holds
// st.mu.
func applyHAResourceForm(rec *haResourceRecord, form url.Values) {
	if v := form.Get("state"); v != "" {
		rec.State = v
	}
	if v := form.Get("comment"); v != "" {
		rec.Comment = v
	}
	if v, err := strconv.Atoi(form.Get("max_restart")); err == nil {
		rec.MaxRestart = v
	}
	if v, err := strconv.Atoi(form.Get("max_relocate")); err == nil {
		rec.MaxRelocate = v
	}
}

// handleHAResourceDelete removes a resource from HA management. Synchronous.
func (s *Server) handleHAResourceDelete(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	sid := r.PathValue("sid")
	s.st.mu.Lock()
	_, found := s.st.ha.resources[sid]
	if found {
		delete(s.st.ha.resources, sid)
	}
	s.st.mu.Unlock()
	if !found {
		s.writeError(w, http.StatusNotFound, msgNoSuchHAResource)
		return
	}
	s.writeData(w, nil)
}

func haResourceToPayload(rec *haResourceRecord) haResourcePayload {
	return haResourcePayload{
		SID: rec.SID, Type: rec.Type, State: rec.State,
		MaxRestart: rec.MaxRestart, MaxRelocate: rec.MaxRelocate, Comment: rec.Comment,
	}
}
