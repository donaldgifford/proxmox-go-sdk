package mockpve

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// lxcState is the LXC slice of the mock model, embedded in state and guarded by
// state.mu. Containers reuse the shared vmRecord type.
type lxcState struct {
	cts map[string]map[int]*vmRecord // node -> vmid -> record.
}

// AddContainer seeds a container on node with the given name and status
// ("stopped" or "running"). Call before serving.
func (s *Server) AddContainer(node string, vmid int, name, status string) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	if s.st.lxc.cts == nil {
		s.st.lxc.cts = make(map[string]map[int]*vmRecord)
	}
	if s.st.lxc.cts[node] == nil {
		s.st.lxc.cts[node] = make(map[int]*vmRecord)
	}
	rec := &vmRecord{
		VMID:      vmid,
		Node:      node,
		Name:      name,
		Status:    status,
		Config:    make(map[string]any),
		Snapshots: make(map[string]*snapRecord),
	}
	// Real PVE reports the hostname in container config reads too.
	if name != "" {
		rec.Config["hostname"] = name
	}
	s.st.lxc.cts[node][vmid] = rec
}

// SetCTConfig merges fields into a seeded container's config. It is a no-op if
// the container does not exist.
func (s *Server) SetCTConfig(node string, vmid int, fields map[string]any) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	if rec := s.lookupCT(node, vmid); rec != nil {
		for k, v := range fields {
			rec.Config[k] = v
		}
	}
}

// lookupCT returns the record for node/vmid, or nil. The caller must hold st.mu.
func (s *Server) lookupCT(node string, vmid int) *vmRecord {
	n, ok := s.st.lxc.cts[node]
	if !ok {
		return nil
	}
	return n[vmid]
}

// ctExists reports whether node/vmid is seeded. It locks st.mu itself.
func (s *Server) ctExists(node string, vmid int) bool {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	return s.lookupCT(node, vmid) != nil
}

func (s *Server) registerLXCRoutes() {
	s.mux.HandleFunc("GET /api2/json/nodes/{node}/lxc", s.handleLXCList)
	s.mux.HandleFunc("POST /api2/json/nodes/{node}/lxc", s.handleLXCCreate)
	s.mux.HandleFunc("GET /api2/json/nodes/{node}/lxc/{vmid}/status/current", s.handleLXCStatus)
	s.mux.HandleFunc("GET /api2/json/nodes/{node}/lxc/{vmid}/config", s.handleLXCConfig)
	s.mux.HandleFunc("PUT /api2/json/nodes/{node}/lxc/{vmid}/config", s.handleLXCSetConfig)
	s.mux.HandleFunc("POST /api2/json/nodes/{node}/lxc/{vmid}/clone", s.handleLXCClone)
	s.mux.HandleFunc("DELETE /api2/json/nodes/{node}/lxc/{vmid}", s.handleLXCDelete)
	s.mux.HandleFunc("POST /api2/json/nodes/{node}/lxc/{vmid}/status/{action}", s.handleLXCPower)
	s.mux.HandleFunc("GET /api2/json/nodes/{node}/lxc/{vmid}/snapshot", s.handleLXCSnapshotList)
	s.mux.HandleFunc("POST /api2/json/nodes/{node}/lxc/{vmid}/snapshot", s.handleLXCSnapshotCreate)
	s.mux.HandleFunc("POST /api2/json/nodes/{node}/lxc/{vmid}/snapshot/{snap}/rollback", s.handleLXCSnapshotRollback)
	s.mux.HandleFunc("DELETE /api2/json/nodes/{node}/lxc/{vmid}/snapshot/{snap}", s.handleLXCSnapshotDelete)
	// Storage download-url backs lxc.PullOCITemplate; it moves to the storage
	// mock when that service lands (Phase 3).
	s.mux.HandleFunc("POST /api2/json/nodes/{node}/storage/{storage}/download-url", s.handleStorageDownloadURL)
}

// handleStorageDownloadURL models POST /nodes/{node}/storage/{storage}/download-url,
// the endpoint lxc.PullOCITemplate drives to fetch an OCI image into vztmpl
// content. It validates the form and returns a download task without modelling
// the resulting template volume.
func (s *Server) handleStorageDownloadURL(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidForm)
		return
	}
	if r.PostForm.Get("url") == "" || r.PostForm.Get("filename") == "" {
		s.writeError(w, http.StatusBadRequest, "missing url or filename")
		return
	}
	s.writeData(w, s.finishedTask(node, "download", r.PathValue("storage")))
}

func (s *Server) handleLXCList(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	s.st.mu.Lock()
	entries := make([]qemuListEntry, 0, len(s.st.lxc.cts[node]))
	for _, rec := range s.st.lxc.cts[node] {
		entries = append(entries, qemuListEntry{VMID: rec.VMID, Name: rec.Name, Status: rec.Status})
	}
	s.st.mu.Unlock()
	s.writeData(w, entries)
}

func (s *Server) handleLXCStatus(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	vmid, err := strconv.Atoi(r.PathValue("vmid"))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidVMID)
		return
	}
	s.st.mu.Lock()
	rec := s.lookupCT(node, vmid)
	var payload qemuStatusPayload
	if rec != nil {
		payload = qemuStatusPayload{VMID: rec.VMID, Name: rec.Name, Status: rec.Status}
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchVM)
		return
	}
	s.writeData(w, payload)
}

func (s *Server) handleLXCConfig(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	vmid, err := strconv.Atoi(r.PathValue("vmid"))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidVMID)
		return
	}
	s.st.mu.Lock()
	rec := s.lookupCT(node, vmid)
	var cfg map[string]any
	if rec != nil {
		cfg = make(map[string]any, len(rec.Config))
		for k, v := range rec.Config {
			cfg[k] = v
		}
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchVM)
		return
	}
	s.writeData(w, cfg)
}

func (s *Server) handleLXCSetConfig(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	vmid, err := strconv.Atoi(r.PathValue("vmid"))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidVMID)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if perr := r.ParseForm(); perr != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidForm)
		return
	}
	s.st.mu.Lock()
	rec := s.lookupCT(node, vmid)
	if rec != nil {
		applyConfigForm(rec, r.PostForm)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchVM)
		return
	}
	s.writeData(w, nil)
}

func (s *Server) handleLXCCreate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidForm)
		return
	}
	id, err := strconv.Atoi(r.PostForm.Get("vmid"))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidVMID)
		return
	}
	s.AddContainer(node, id, r.PostForm.Get("hostname"), vmStatusStopped)
	// Real PVE persists every create key into the container config (a later
	// GET /config returns them), so mirror that for create-then-read
	// consumers. vmid addresses the record; it is not a config key.
	r.PostForm.Del("vmid")
	s.st.mu.Lock()
	if rec := s.lookupCT(node, id); rec != nil {
		applyConfigForm(rec, r.PostForm)
	}
	s.st.mu.Unlock()
	s.writeData(w, s.finishedTask(node, "vzcreate", strconv.Itoa(id)))
}

func (s *Server) handleLXCClone(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	src, err := strconv.Atoi(r.PathValue("vmid"))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidVMID)
		return
	}
	if !s.ctExists(node, src) {
		s.writeError(w, http.StatusNotFound, msgNoSuchVM)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if perr := r.ParseForm(); perr != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidForm)
		return
	}
	newID, err := strconv.Atoi(r.PostForm.Get("newid"))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid newid")
		return
	}
	s.AddContainer(node, newID, r.PostForm.Get("hostname"), vmStatusStopped)
	s.writeData(w, s.finishedTask(node, "vzclone", strconv.Itoa(src)))
}

func (s *Server) handleLXCDelete(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	vmid, err := strconv.Atoi(r.PathValue("vmid"))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidVMID)
		return
	}
	if !s.ctExists(node, vmid) {
		s.writeError(w, http.StatusNotFound, msgNoSuchVM)
		return
	}
	s.st.mu.Lock()
	if n, ok := s.st.lxc.cts[node]; ok {
		delete(n, vmid)
	}
	s.st.mu.Unlock()
	s.writeData(w, s.finishedTask(node, "vzdestroy", strconv.Itoa(vmid)))
}

func (s *Server) handleLXCPower(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	action := r.PathValue("action")
	newStatus, ok := qemuPowerStatus[action]
	if !ok {
		s.writeError(w, http.StatusBadRequest, "unknown power action")
		return
	}
	vmid, err := strconv.Atoi(r.PathValue("vmid"))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidVMID)
		return
	}
	s.st.mu.Lock()
	rec := s.lookupCT(node, vmid)
	if rec != nil {
		rec.Status = newStatus
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchVM)
		return
	}
	s.writeData(w, s.finishedTask(node, "vz"+action, strconv.Itoa(vmid)))
}

func (s *Server) handleLXCSnapshotList(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	vmid, err := strconv.Atoi(r.PathValue("vmid"))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidVMID)
		return
	}
	s.st.mu.Lock()
	rec := s.lookupCT(node, vmid)
	var snaps []qemuSnapshotPayload
	if rec != nil {
		snaps = make([]qemuSnapshotPayload, 0, len(rec.Snapshots)+1)
		for _, snap := range rec.Snapshots {
			snaps = append(snaps, qemuSnapshotPayload{
				Name:        snap.Name,
				Description: snap.Description,
				SnapTime:    snap.SnapTime,
			})
		}
		// PVE always appends a synthetic "current" entry for the live state.
		snaps = append(snaps, qemuSnapshotPayload{Name: "current", Description: "You are here!"})
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchVM)
		return
	}
	s.writeData(w, snaps)
}

func (s *Server) handleLXCSnapshotCreate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	vmid, err := strconv.Atoi(r.PathValue("vmid"))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidVMID)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if perr := r.ParseForm(); perr != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidForm)
		return
	}
	name := r.PostForm.Get("snapname")
	if name == "" {
		s.writeError(w, http.StatusBadRequest, "missing snapname")
		return
	}
	s.st.mu.Lock()
	rec := s.lookupCT(node, vmid)
	if rec != nil {
		rec.Snapshots[name] = &snapRecord{
			Name:        name,
			Description: r.PostForm.Get("description"),
		}
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchVM)
		return
	}
	s.writeData(w, s.finishedTask(node, "vzsnapshot", strconv.Itoa(vmid)))
}

func (s *Server) handleLXCSnapshotRollback(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node, vmid, ok := s.lxcSnapshotTarget(w, r)
	if !ok {
		return
	}
	s.writeData(w, s.finishedTask(node, "vzrollback", strconv.Itoa(vmid)))
}

func (s *Server) handleLXCSnapshotDelete(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node, vmid, ok := s.lxcSnapshotTarget(w, r)
	if !ok {
		return
	}
	name := r.PathValue("snap")
	s.st.mu.Lock()
	if rec := s.lookupCT(node, vmid); rec != nil {
		delete(rec.Snapshots, name)
	}
	s.st.mu.Unlock()
	s.writeData(w, s.finishedTask(node, "vzdelsnapshot", strconv.Itoa(vmid)))
}

// lxcSnapshotTarget resolves and validates the {node}/{vmid}/{snap} of a
// container snapshot sub-request, writing the appropriate error and returning
// ok=false on failure.
func (s *Server) lxcSnapshotTarget(w http.ResponseWriter, r *http.Request) (node string, vmid int, ok bool) {
	node = r.PathValue("node")
	vmid, err := strconv.Atoi(r.PathValue("vmid"))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidVMID)
		return "", 0, false
	}
	name := r.PathValue("snap")
	s.st.mu.Lock()
	rec := s.lookupCT(node, vmid)
	hasSnap := false
	if rec != nil {
		_, hasSnap = rec.Snapshots[name]
	}
	s.st.mu.Unlock()
	if rec == nil || !hasSnap {
		s.writeError(w, http.StatusNotFound, "no such snapshot")
		return "", 0, false
	}
	return node, vmid, true
}

// applyConfigForm merges a config PUT's form fields into rec.Config, honoring
// the PVE "delete" parameter (comma-separated keys to unset). Numeric values are
// stored as ints so a Config read round-trips their JSON type. It is shared by
// the qemu and lxc config handlers.
func applyConfigForm(rec *vmRecord, form url.Values) {
	if del := form.Get("delete"); del != "" {
		for _, k := range strings.Split(del, ",") {
			delete(rec.Config, strings.TrimSpace(k))
		}
	}
	for key := range form {
		if key == "delete" {
			continue
		}
		rec.Config[key] = parseConfigValue(form.Get(key))
	}
}
