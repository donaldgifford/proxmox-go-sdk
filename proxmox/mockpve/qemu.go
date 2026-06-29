package mockpve

import (
	"net/http"
	"strconv"
	"time"
)

// Repeated literals, pulled out so goconst stays quiet and the wire values live
// in one place.
const (
	mockTaskUser    = "root@pam"
	vmStatusStopped = "stopped"
	msgNoSuchVM     = "no such VM"
	msgInvalidVMID  = "invalid vmid"
	msgInvalidForm  = "invalid form body"
)

// qemuState is the QEMU slice of the mock model, embedded in state and guarded
// by state.mu.
type qemuState struct {
	vms map[string]map[int]*vmRecord // node -> vmid -> record.
}

// vmRecord models one QEMU VM in the mock. Config holds the per-key config
// values a Config read returns; values are Go-typed so they marshal back as the
// JSON types the SDK's typed fields expect.
type vmRecord struct {
	VMID   int
	Node   string
	Name   string
	Status string
	Config map[string]any
}

// qemuListEntry is one element of GET /nodes/{node}/qemu.
type qemuListEntry struct {
	VMID   int    `json:"vmid"`
	Name   string `json:"name,omitempty"`
	Status string `json:"status"`
}

// qemuStatusPayload is the data of GET /nodes/{node}/qemu/{vmid}/status/current.
type qemuStatusPayload struct {
	VMID   int    `json:"vmid"`
	Name   string `json:"name,omitempty"`
	Status string `json:"status"`
}

// AddVM seeds a VM on node with the given name and status ("stopped" or
// "running"). The node need not be registered first. Call before serving.
func (s *Server) AddVM(node string, vmid int, name, status string) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	if s.st.qemu.vms == nil {
		s.st.qemu.vms = make(map[string]map[int]*vmRecord)
	}
	if s.st.qemu.vms[node] == nil {
		s.st.qemu.vms[node] = make(map[int]*vmRecord)
	}
	s.st.qemu.vms[node][vmid] = &vmRecord{
		VMID:   vmid,
		Node:   node,
		Name:   name,
		Status: status,
		Config: make(map[string]any),
	}
}

// SetVMConfig merges fields into a seeded VM's config. Values should use the Go
// types the SDK decodes (int for memory/cores, string for net0, …) so a Config
// read round-trips. It is a no-op if the VM does not exist.
func (s *Server) SetVMConfig(node string, vmid int, fields map[string]any) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	rec := s.lookupVM(node, vmid)
	if rec == nil {
		return
	}
	for k, v := range fields {
		rec.Config[k] = v
	}
}

// lookupVM returns the record for node/vmid, or nil. The caller must hold st.mu.
func (s *Server) lookupVM(node string, vmid int) *vmRecord {
	n, ok := s.st.qemu.vms[node]
	if !ok {
		return nil
	}
	return n[vmid]
}

// vmExists reports whether node/vmid is seeded. It locks st.mu itself.
func (s *Server) vmExists(node string, vmid int) bool {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	return s.lookupVM(node, vmid) != nil
}

// removeVM deletes node/vmid if present. It locks st.mu itself.
func (s *Server) removeVM(node string, vmid int) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	if n, ok := s.st.qemu.vms[node]; ok {
		delete(n, vmid)
	}
}

// synthUPID builds a parseable UPID for a synthetic mock task. The pid/pstart
// fields are fixed; only node, type, and id vary.
func synthUPID(node, taskType, id string) string {
	ts := strconv.FormatInt(time.Now().Unix(), 16)
	return "UPID:" + node + ":00000001:00000001:" + ts + ":" + taskType + ":" + id + ":" + mockTaskUser + ":"
}

func (s *Server) registerQEMURoutes() {
	s.mux.HandleFunc("GET /api2/json/nodes/{node}/qemu", s.handleQEMUList)
	s.mux.HandleFunc("POST /api2/json/nodes/{node}/qemu", s.handleQEMUCreate)
	s.mux.HandleFunc("GET /api2/json/nodes/{node}/qemu/{vmid}/status/current", s.handleQEMUStatus)
	s.mux.HandleFunc("GET /api2/json/nodes/{node}/qemu/{vmid}/config", s.handleQEMUConfig)
	s.mux.HandleFunc("PUT /api2/json/nodes/{node}/qemu/{vmid}/config", s.handleQEMUSetConfig)
	s.mux.HandleFunc("POST /api2/json/nodes/{node}/qemu/{vmid}/clone", s.handleQEMUClone)
	s.mux.HandleFunc("DELETE /api2/json/nodes/{node}/qemu/{vmid}", s.handleQEMUDelete)
}

func (s *Server) handleQEMUList(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	s.st.mu.Lock()
	entries := make([]qemuListEntry, 0, len(s.st.qemu.vms[node]))
	for _, rec := range s.st.qemu.vms[node] {
		entries = append(entries, qemuListEntry{VMID: rec.VMID, Name: rec.Name, Status: rec.Status})
	}
	s.st.mu.Unlock()
	s.writeData(w, entries)
}

func (s *Server) handleQEMUStatus(w http.ResponseWriter, r *http.Request) {
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
	rec := s.lookupVM(node, vmid)
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

func (s *Server) handleQEMUConfig(w http.ResponseWriter, r *http.Request) {
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
	rec := s.lookupVM(node, vmid)
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

func (s *Server) handleQEMUSetConfig(w http.ResponseWriter, r *http.Request) {
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
	rec := s.lookupVM(node, vmid)
	if rec != nil {
		for key := range r.PostForm {
			rec.Config[key] = parseConfigValue(r.PostForm.Get(key))
		}
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchVM)
		return
	}
	// A config-only change is synchronous in PVE: data is null, no task.
	s.writeData(w, nil)
}

func (s *Server) handleQEMUCreate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidForm)
		return
	}
	vmid := r.PostForm.Get("vmid")
	id, err := strconv.Atoi(vmid)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidVMID)
		return
	}
	s.AddVM(node, id, r.PostForm.Get("name"), vmStatusStopped)
	s.writeData(w, s.finishedTask(node, "qmcreate", vmid))
}

func (s *Server) handleQEMUClone(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	src, err := strconv.Atoi(r.PathValue("vmid"))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidVMID)
		return
	}
	if !s.vmExists(node, src) {
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
	s.AddVM(node, newID, r.PostForm.Get("name"), vmStatusStopped)
	s.writeData(w, s.finishedTask(node, "qmclone", strconv.Itoa(src)))
}

func (s *Server) handleQEMUDelete(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	vmid, err := strconv.Atoi(r.PathValue("vmid"))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidVMID)
		return
	}
	if !s.vmExists(node, vmid) {
		s.writeError(w, http.StatusNotFound, msgNoSuchVM)
		return
	}
	s.removeVM(node, vmid)
	s.writeData(w, s.finishedTask(node, "qmdestroy", strconv.Itoa(vmid)))
}

// finishedTask records a synthetic task that is already complete with exit
// status OK and returns its UPID, so the caller can await it and observe
// success immediately.
func (s *Server) finishedTask(node, taskType, id string) string {
	upid := synthUPID(node, taskType, id)
	s.AddTask(node, upid, taskType, id, mockTaskUser, nil)
	s.FinishTask(node, upid, "OK")
	return upid
}

// parseConfigValue stores an int when the form value is an integer and a string
// otherwise, so numeric config keys round-trip as JSON numbers.
func parseConfigValue(v string) any {
	if n, err := strconv.Atoi(v); err == nil {
		return n
	}
	return v
}
