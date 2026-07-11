package mockpve

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// Repeated literals, pulled out so goconst stays quiet and the wire values live
// in one place.
const (
	mockTaskUser    = "root@pam"
	vmStatusStopped = "stopped"
	vmStatusRunning = "running"
	msgNoSuchVM     = "no such VM"
	msgInvalidVMID  = "invalid vmid"
	msgInvalidForm  = "invalid form body"

	msgNoSuchStorage = "no such storage"
	msgNoSuchVolume  = "no such volume"
	msgNoSuchZFSPool = "no such zfs pool"

	msgNoSuchHAResource = "no such ha resource"
	msgNoSuchHARule     = "no such ha rule"
	msgNoSuchReplJob    = "no such replication job"
	msgNoSuchIface      = "no such network interface"

	msgNoSuchZone   = "no such sdn zone"
	msgNoSuchVNet   = "no such sdn vnet"
	msgNoSuchSubnet = "no such sdn subnet"
	msgNoSuchFabric = "no such sdn fabric"

	msgNoSuchRule       = "no such firewall rule"
	msgNoSuchIPSet      = "no such firewall ipset"
	msgNoSuchIPSetEntry = "no such firewall ipset entry"

	msgNoSuchUser  = "no such user"
	msgNoSuchGroup = "no such group"
	msgNoSuchRole  = "no such role"
	msgNoSuchToken = "no such token"

	msgNoSuchRepo        = "no such apt repository"
	msgNoSuchDisk        = "no such disk"
	msgNoSuchACMEAccount = "no such acme account"

	msgNoSuchMetricServer = "no such metric server"
	msgNoSuchCephPool     = "no such ceph pool"
	msgNoSuchBackupJob    = "no such backup job"
	msgBadVNCTicket       = "invalid or expired vnc ticket"
)

// qemuPowerStatus maps a /status/{action} verb to the run state the mock VM
// settles into once the synthetic task completes.
var qemuPowerStatus = map[string]string{
	"start":    vmStatusRunning,
	"stop":     vmStatusStopped,
	"shutdown": vmStatusStopped,
	"reboot":   vmStatusRunning,
	"suspend":  "suspended",
	"resume":   vmStatusRunning,
}

// qemuState is the QEMU slice of the mock model, embedded in state and guarded
// by state.mu.
type qemuState struct {
	vms map[string]map[int]*vmRecord // node -> vmid -> record.
}

// vmRecord models one QEMU VM in the mock. Config holds the per-key config
// values a Config read returns; values are Go-typed so they marshal back as the
// JSON types the SDK's typed fields expect.
type vmRecord struct {
	VMID      int
	Node      string
	Name      string
	Status    string
	Config    map[string]any
	Snapshots map[string]*snapRecord // keyed by snapshot name.
	Agent     *agentResult           // seeded guest-agent exec result; nil = success.
}

// snapRecord models one VM snapshot in the mock.
type snapRecord struct {
	Name        string
	Description string
	VMState     bool
	SnapTime    int64
}

// agentResult is the canned guest-agent exec result a VM returns from
// /agent/exec-status. The mock always reports the command as exited.
type agentResult struct {
	ExitCode int
	OutData  string
	ErrData  string
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

// qemuSnapshotPayload is one element of GET /nodes/{node}/qemu/{vmid}/snapshot.
type qemuSnapshotPayload struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	VMState     int    `json:"vmstate,omitempty"`
	SnapTime    int64  `json:"snaptime,omitempty"`
}

// agentExecPayload is the data of POST /nodes/{node}/qemu/{vmid}/agent/exec.
type agentExecPayload struct {
	PID int `json:"pid"`
}

// agentExecStatusPayload is the data of GET .../agent/exec-status.
type agentExecStatusPayload struct {
	Exited   int    `json:"exited"`
	ExitCode int    `json:"exitcode"`
	OutData  string `json:"out-data,omitempty"`
	ErrData  string `json:"err-data,omitempty"`
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
		VMID:      vmid,
		Node:      node,
		Name:      name,
		Status:    status,
		Config:    make(map[string]any),
		Snapshots: make(map[string]*snapRecord),
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

// SetVMAgentResult seeds what the VM's guest agent returns from an exec: the
// exit code and captured stdout/stderr. It is a no-op if the VM does not exist.
func (s *Server) SetVMAgentResult(node string, vmid, exitCode int, outData, errData string) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	if rec := s.lookupVM(node, vmid); rec != nil {
		rec.Agent = &agentResult{ExitCode: exitCode, OutData: outData, ErrData: errData}
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
	s.mux.HandleFunc("POST /api2/json/nodes/{node}/qemu/{vmid}/status/{action}", s.handleQEMUPower)
	s.mux.HandleFunc("PUT /api2/json/nodes/{node}/qemu/{vmid}/resize", s.handleQEMUResize)
	s.mux.HandleFunc("POST /api2/json/nodes/{node}/qemu/{vmid}/move_disk", s.handleQEMUMoveDisk)
	s.mux.HandleFunc("POST /api2/json/nodes/{node}/qemu/{vmid}/migrate", s.handleQEMUMigrate)
	s.mux.HandleFunc("GET /api2/json/nodes/{node}/qemu/{vmid}/snapshot", s.handleQEMUSnapshotList)
	s.mux.HandleFunc("POST /api2/json/nodes/{node}/qemu/{vmid}/snapshot", s.handleQEMUSnapshotCreate)
	s.mux.HandleFunc("POST /api2/json/nodes/{node}/qemu/{vmid}/snapshot/{snap}/rollback", s.handleQEMUSnapshotRollback)
	s.mux.HandleFunc("DELETE /api2/json/nodes/{node}/qemu/{vmid}/snapshot/{snap}", s.handleQEMUSnapshotDelete)
	s.mux.HandleFunc("POST /api2/json/nodes/{node}/qemu/{vmid}/agent/ping", s.handleQEMUAgentPing)
	s.mux.HandleFunc("POST /api2/json/nodes/{node}/qemu/{vmid}/agent/exec", s.handleQEMUAgentExec)
	s.mux.HandleFunc("GET /api2/json/nodes/{node}/qemu/{vmid}/agent/exec-status", s.handleQEMUAgentExecStatus)
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
		// Live PVE 9.2.4 serializes memory as a quoted string in config
		// reads (9.2-1 returned a JSON number — the encoding drifted in a
		// point release; found by the IMPL-0002 Phase 0 dogfood spike).
		// Mirror it so unit tests exercise the SDK's tolerant decode.
		if v, ok := cfg["memory"]; ok {
			cfg["memory"] = fmt.Sprintf("%v", v)
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
		applyConfigForm(rec, r.PostForm)
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

func (s *Server) handleQEMUPower(w http.ResponseWriter, r *http.Request) {
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
	rec := s.lookupVM(node, vmid)
	if rec != nil {
		rec.Status = newStatus
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchVM)
		return
	}
	s.writeData(w, s.finishedTask(node, "qm"+action, strconv.Itoa(vmid)))
}

func (s *Server) handleQEMUResize(w http.ResponseWriter, r *http.Request) {
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
	// PVE resizes synchronously and answers with null data.
	s.writeData(w, nil)
}

func (s *Server) handleQEMUMoveDisk(w http.ResponseWriter, r *http.Request) {
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
	if r.PostForm.Get("disk") == "" || r.PostForm.Get("storage") == "" {
		s.writeError(w, http.StatusBadRequest, "missing disk or storage")
		return
	}
	if !s.vmExists(node, vmid) {
		s.writeError(w, http.StatusNotFound, msgNoSuchVM)
		return
	}
	s.writeData(w, s.finishedTask(node, "qmmove", strconv.Itoa(vmid)))
}

func (s *Server) handleQEMUMigrate(w http.ResponseWriter, r *http.Request) {
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
	target := r.PostForm.Get("target")
	if target == "" {
		s.writeError(w, http.StatusBadRequest, "missing target")
		return
	}

	s.st.mu.Lock()
	rec := s.lookupVM(node, vmid)
	if rec != nil {
		if s.st.qemu.vms[target] == nil {
			s.st.qemu.vms[target] = make(map[int]*vmRecord)
		}
		rec.Node = target
		s.st.qemu.vms[target][vmid] = rec
		delete(s.st.qemu.vms[node], vmid)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchVM)
		return
	}
	// The migration task runs on the source node.
	s.writeData(w, s.finishedTask(node, "qmigrate", strconv.Itoa(vmid)))
}

func (s *Server) handleQEMUSnapshotList(w http.ResponseWriter, r *http.Request) {
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
	var snaps []qemuSnapshotPayload
	if rec != nil {
		snaps = make([]qemuSnapshotPayload, 0, len(rec.Snapshots)+1)
		for _, snap := range rec.Snapshots {
			vmstate := 0
			if snap.VMState {
				vmstate = 1
			}
			snaps = append(snaps, qemuSnapshotPayload{
				Name:        snap.Name,
				Description: snap.Description,
				VMState:     vmstate,
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

func (s *Server) handleQEMUSnapshotCreate(w http.ResponseWriter, r *http.Request) {
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
	rec := s.lookupVM(node, vmid)
	if rec != nil {
		rec.Snapshots[name] = &snapRecord{
			Name:        name,
			Description: r.PostForm.Get("description"),
			VMState:     r.PostForm.Get("vmstate") == "1",
		}
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchVM)
		return
	}
	s.writeData(w, s.finishedTask(node, "qmsnapshot", strconv.Itoa(vmid)))
}

func (s *Server) handleQEMUSnapshotRollback(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node, vmid, ok := s.snapshotTarget(w, r)
	if !ok {
		return
	}
	s.writeData(w, s.finishedTask(node, "qmrollback", strconv.Itoa(vmid)))
}

func (s *Server) handleQEMUSnapshotDelete(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node, vmid, ok := s.snapshotTarget(w, r)
	if !ok {
		return
	}
	name := r.PathValue("snap")
	s.st.mu.Lock()
	if rec := s.lookupVM(node, vmid); rec != nil {
		delete(rec.Snapshots, name)
	}
	s.st.mu.Unlock()
	s.writeData(w, s.finishedTask(node, "qmdelsnapshot", strconv.Itoa(vmid)))
}

// snapshotTarget resolves and validates the {node}/{vmid}/{snap} of a snapshot
// sub-request, writing the appropriate error and returning ok=false on failure.
func (s *Server) snapshotTarget(w http.ResponseWriter, r *http.Request) (node string, vmid int, ok bool) {
	node = r.PathValue("node")
	vmid, err := strconv.Atoi(r.PathValue("vmid"))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidVMID)
		return "", 0, false
	}
	name := r.PathValue("snap")
	s.st.mu.Lock()
	rec := s.lookupVM(node, vmid)
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

// agentMockPID is the fixed guest PID the mock reports for every exec; the mock
// does not track real processes, so exec-status answers from the seeded result.
const agentMockPID = 1000

func (s *Server) handleQEMUAgentPing(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	if !s.agentVMExists(w, r) {
		return
	}
	s.writeData(w, nil)
}

func (s *Server) handleQEMUAgentExec(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	if !s.agentVMExists(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidForm)
		return
	}
	if len(r.PostForm["command"]) == 0 {
		s.writeError(w, http.StatusBadRequest, "missing command")
		return
	}
	s.writeData(w, agentExecPayload{PID: agentMockPID})
}

func (s *Server) handleQEMUAgentExecStatus(w http.ResponseWriter, r *http.Request) {
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
	payload := agentExecStatusPayload{Exited: 1}
	if rec != nil && rec.Agent != nil {
		payload.ExitCode = rec.Agent.ExitCode
		payload.OutData = rec.Agent.OutData
		payload.ErrData = rec.Agent.ErrData
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchVM)
		return
	}
	s.writeData(w, payload)
}

// agentVMExists validates the {node}/{vmid} of an agent request, writing the
// error and returning false when the VM is unknown.
func (s *Server) agentVMExists(w http.ResponseWriter, r *http.Request) bool {
	node := r.PathValue("node")
	vmid, err := strconv.Atoi(r.PathValue("vmid"))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidVMID)
		return false
	}
	if !s.vmExists(node, vmid) {
		s.writeError(w, http.StatusNotFound, msgNoSuchVM)
		return false
	}
	return true
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
