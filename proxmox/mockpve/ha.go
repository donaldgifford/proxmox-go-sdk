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
	rules     map[string]*haRuleRecord     // keyed by rule name.
	crs       string                       // the datacenter "crs" property-string.
	// armed is the 9.2 cluster-wide HA switch; New() starts it true (real
	// PVE default). resourceMode holds the mode chosen at disarm time.
	armed        bool
	resourceMode string
	repl         map[string]*haReplRecord // keyed by replication job ID.
}

// haDefaultNode is the node the synthesized HA status rows report when a
// seeded resource carries no node (the mock's canonical node from New).
const haDefaultNode = "pve"

// haMockTimestamp is the fixed unix timestamp the synthesized status rows
// carry, keeping mock responses deterministic.
const haMockTimestamp = 1752000000

// haReplRecord is one storage/ZFS replication job.
type haReplRecord struct {
	ID       string
	Type     string
	Target   string
	Schedule string
	Rate     float64
	Disable  bool
	Comment  string
}

// haRuleRecord is one HA rule (node-affinity or resource-affinity).
type haRuleRecord struct {
	Rule      string
	Type      string
	Nodes     string
	Resources string
	Affinity  string
	Disable   bool
	Comment   string
}

// haResourceRecord is one guest under HA management.
type haResourceRecord struct {
	SID         string
	Type        string
	State       string
	Node        string // where the CRM currently places it; migrate/relocate move it.
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

// haRulePayload mirrors GET /cluster/ha/rules entries.
type haRulePayload struct {
	Rule      string `json:"rule"`
	Type      string `json:"type,omitempty"`
	Nodes     string `json:"nodes,omitempty"`
	Resources string `json:"resources,omitempty"`
	Affinity  string `json:"affinity,omitempty"`
	Disable   int    `json:"disable,omitempty"`
	Comment   string `json:"comment,omitempty"`
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

// AddHARule seeds an HA rule. Call before serving.
func (s *Server) AddHARule(rule, ruleType string) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	if s.st.ha.rules == nil {
		s.st.ha.rules = make(map[string]*haRuleRecord)
	}
	s.st.ha.rules[rule] = &haRuleRecord{Rule: rule, Type: ruleType}
}

// SetCRS seeds the datacenter "crs" property-string (e.g.
// "ha=static,ha-rebalance-on-start=1"). Call before serving.
func (s *Server) SetCRS(crs string) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	s.st.ha.crs = crs
}

// haReplPayload mirrors GET /cluster/replication entries.
type haReplPayload struct {
	ID       string  `json:"id"`
	Type     string  `json:"type,omitempty"`
	Target   string  `json:"target,omitempty"`
	Schedule string  `json:"schedule,omitempty"`
	Rate     float64 `json:"rate,omitempty"`
	Disable  int     `json:"disable,omitempty"`
	Comment  string  `json:"comment,omitempty"`
}

// AddReplicationJob seeds a replication job. Call before serving.
func (s *Server) AddReplicationJob(id, target, schedule string) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	if s.st.ha.repl == nil {
		s.st.ha.repl = make(map[string]*haReplRecord)
	}
	s.st.ha.repl[id] = &haReplRecord{ID: id, Type: "local", Target: target, Schedule: schedule}
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
	s.mux.HandleFunc("GET /api2/json/cluster/ha/rules", s.handleHARuleList)
	s.mux.HandleFunc("POST /api2/json/cluster/ha/rules", s.handleHARuleCreate)
	s.mux.HandleFunc("GET /api2/json/cluster/ha/rules/{rule}", s.handleHARuleGet)
	s.mux.HandleFunc("PUT /api2/json/cluster/ha/rules/{rule}", s.handleHARuleUpdate)
	s.mux.HandleFunc("DELETE /api2/json/cluster/ha/rules/{rule}", s.handleHARuleDelete)
	s.mux.HandleFunc("GET /api2/json/cluster/options", s.handleClusterOptionsGet)
	s.mux.HandleFunc("PUT /api2/json/cluster/options", s.handleClusterOptionsSet)
	s.mux.HandleFunc("GET /api2/json/cluster/ha/status/current", s.handleHAStatusCurrent)
	s.mux.HandleFunc("GET /api2/json/cluster/ha/status/manager_status", s.handleHAManagerStatus)
	s.mux.HandleFunc("POST /api2/json/cluster/ha/status/arm-ha", s.handleHAArm)
	s.mux.HandleFunc("POST /api2/json/cluster/ha/status/disarm-ha", s.handleHADisarm)
	s.mux.HandleFunc("POST /api2/json/cluster/ha/resources/{sid}/migrate", s.handleHAResourceMove)
	s.mux.HandleFunc("POST /api2/json/cluster/ha/resources/{sid}/relocate", s.handleHAResourceMove)
	s.mux.HandleFunc("GET /api2/json/cluster/replication", s.handleReplList)
	s.mux.HandleFunc("POST /api2/json/cluster/replication", s.handleReplCreate)
	s.mux.HandleFunc("GET /api2/json/cluster/replication/{id}", s.handleReplGet)
	s.mux.HandleFunc("PUT /api2/json/cluster/replication/{id}", s.handleReplUpdate)
	s.mux.HandleFunc("DELETE /api2/json/cluster/replication/{id}", s.handleReplDelete)
}

func (s *Server) handleReplList(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.st.mu.Lock()
	out := make([]haReplPayload, 0, len(s.st.ha.repl))
	for _, rec := range s.st.ha.repl {
		out = append(out, haReplToPayload(rec))
	}
	s.st.mu.Unlock()
	s.writeData(w, out)
}

func (s *Server) handleReplGet(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	id := r.PathValue("id")
	s.st.mu.Lock()
	rec := s.st.ha.repl[id]
	var payload haReplPayload
	if rec != nil {
		payload = haReplToPayload(rec)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchReplJob)
		return
	}
	s.writeData(w, payload)
}

// handleReplCreate defines a replication job. Synchronous (data null, no task).
func (s *Server) handleReplCreate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidForm)
		return
	}
	id := r.PostForm.Get("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "missing id")
		return
	}
	rec := &haReplRecord{
		ID: id, Type: r.PostForm.Get("type"), Target: r.PostForm.Get("target"),
		Schedule: r.PostForm.Get("schedule"), Comment: r.PostForm.Get("comment"),
	}
	if rec.Type == "" {
		rec.Type = "local"
	}
	if v, err := strconv.ParseFloat(r.PostForm.Get("rate"), 64); err == nil {
		rec.Rate = v
	}
	s.st.mu.Lock()
	if s.st.ha.repl == nil {
		s.st.ha.repl = make(map[string]*haReplRecord)
	}
	s.st.ha.repl[id] = rec
	s.st.mu.Unlock()
	s.writeData(w, nil)
}

// handleReplUpdate mutates a replication job. Synchronous.
func (s *Server) handleReplUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidForm)
		return
	}
	s.st.mu.Lock()
	rec := s.st.ha.repl[id]
	if rec != nil {
		applyReplForm(rec, r.PostForm)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchReplJob)
		return
	}
	s.writeData(w, nil)
}

// applyReplForm applies a PUT form's fields to rec. The caller holds st.mu.
func applyReplForm(rec *haReplRecord, form url.Values) {
	if v := form.Get("target"); v != "" {
		rec.Target = v
	}
	if v := form.Get("schedule"); v != "" {
		rec.Schedule = v
	}
	if v := form.Get("comment"); v != "" {
		rec.Comment = v
	}
	if v := form.Get("disable"); v != "" {
		rec.Disable = v == "1"
	}
	if v, err := strconv.ParseFloat(form.Get("rate"), 64); err == nil {
		rec.Rate = v
	}
}

// handleReplDelete removes a replication job. Synchronous.
func (s *Server) handleReplDelete(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	id := r.PathValue("id")
	s.st.mu.Lock()
	_, found := s.st.ha.repl[id]
	if found {
		delete(s.st.ha.repl, id)
	}
	s.st.mu.Unlock()
	if !found {
		s.writeError(w, http.StatusNotFound, msgNoSuchReplJob)
		return
	}
	s.writeData(w, nil)
}

func haReplToPayload(rec *haReplRecord) haReplPayload {
	p := haReplPayload{
		ID: rec.ID, Type: rec.Type, Target: rec.Target,
		Schedule: rec.Schedule, Rate: rec.Rate, Comment: rec.Comment,
	}
	if rec.Disable {
		p.Disable = 1
	}
	return p
}

// haStatusPayload mirrors one GET /cluster/ha/status/current row. The real
// endpoint returns a heterogeneous array (quorum/master/fencing/lrm/service
// rows); the mock synthesizes quorum + master + fencing + one service row per
// seeded resource. Live 9.2.2 carries armed-state on the fencing row, not the
// master row (2026-07-23).
type haStatusPayload struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	SID          string `json:"sid,omitempty"`
	Node         string `json:"node,omitempty"`
	State        string `json:"state,omitempty"`
	Status       string `json:"status,omitempty"`
	RequestState string `json:"request_state,omitempty"`
	Quorate      int    `json:"quorate,omitempty"`
	ArmedState   string `json:"armed-state,omitempty"`
	ResourceMode string `json:"resource_mode,omitempty"`
	MaxRestart   int    `json:"max_restart,omitempty"`
	MaxRelocate  int    `json:"max_relocate,omitempty"`
	Timestamp    int64  `json:"timestamp,omitempty"`
	Comment      string `json:"comment,omitempty"`
}

// haArmedState renders the armed flag as the 9.2 armed-state enum. The caller
// holds st.mu.
func (s *Server) haArmedState() string {
	if s.st.ha.armed {
		return "armed"
	}
	return "disarmed"
}

// handleHAStatusCurrent synthesizes the live HA manager view: a quorum row, a
// master row, a fencing row reporting armed-state from the cluster switch
// (where live 9.2.2 puts it — never the master row), and one service row per
// seeded resource. The mock does not schedule — rows reflect seeded state,
// and migrate/relocate handlers move a resource's node.
func (s *Server) handleHAStatusCurrent(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.st.mu.Lock()
	out := []haStatusPayload{
		{ID: "quorum", Type: "quorum", Node: haDefaultNode, Status: "OK", Quorate: 1},
		{
			ID: "master", Type: "master", Node: haDefaultNode, Status: "active",
			Timestamp: haMockTimestamp,
		},
		{
			ID: "fencing", Type: "fencing", Node: haDefaultNode,
			Status: "watchdog", ArmedState: s.haArmedState(),
		},
	}
	for _, rec := range s.st.ha.resources {
		row := haStatusPayload{
			ID: "service:" + rec.SID, Type: "service", SID: rec.SID,
			Node: rec.Node, State: rec.State, Status: rec.State,
			RequestState: rec.State, MaxRestart: rec.MaxRestart,
			MaxRelocate: rec.MaxRelocate, ResourceMode: s.st.ha.resourceMode,
			Comment: rec.Comment,
		}
		if row.Node == "" {
			row.Node = haDefaultNode
		}
		out = append(out, row)
	}
	s.st.mu.Unlock()
	s.writeData(w, out)
}

// handleHAManagerStatus returns a CRM master state blob built from the seeded
// resources, in the live-confirmed 9.2.2 envelope: the state file nests under
// manager_status and a quorum summary rides alongside (quorate is the string
// "1" there, mirroring real PVE).
func (s *Server) handleHAManagerStatus(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.st.mu.Lock()
	services := make(map[string]map[string]string, len(s.st.ha.resources))
	for _, rec := range s.st.ha.resources {
		node := rec.Node
		if node == "" {
			node = haDefaultNode
		}
		services[rec.SID] = map[string]string{
			"node": node, "state": rec.State, "uid": "mock-" + rec.SID,
		}
	}
	s.st.mu.Unlock()
	payload := map[string]any{
		"manager_status": map[string]any{
			"master_node":    haDefaultNode,
			"node_status":    map[string]string{haDefaultNode: "online"},
			"service_status": services,
			"timestamp":      haMockTimestamp,
		},
		"quorum": map[string]string{"node": haDefaultNode, "quorate": "1"},
	}
	s.writeData(w, payload)
}

// handleHAArm flips the cluster-wide HA switch on. Synchronous, no params.
func (s *Server) handleHAArm(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.st.mu.Lock()
	s.st.ha.armed = true
	s.st.ha.resourceMode = ""
	s.st.mu.Unlock()
	s.writeData(w, nil)
}

// handleHADisarm flips the cluster-wide HA switch off. The resource-mode
// parameter is required on real PVE (enum freeze|ignore), so the mock rejects
// a missing or unknown value.
func (s *Server) handleHADisarm(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	if !s.parseForm(w, r) {
		return
	}
	mode := r.PostForm.Get("resource-mode")
	if mode != "freeze" && mode != "ignore" {
		s.writeError(w, http.StatusBadRequest, "missing or invalid resource-mode")
		return
	}
	s.st.mu.Lock()
	s.st.ha.armed = false
	s.st.ha.resourceMode = mode
	s.st.mu.Unlock()
	s.writeData(w, nil)
}

// handleHAResourceMove serves both migrate and relocate: it records the
// requested node as the resource's placement and echoes the accepted intent
// (the mock evaluates no affinity rules, so blocking-resources is never set).
func (s *Server) handleHAResourceMove(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	sid := r.PathValue("sid")
	if !s.parseForm(w, r) {
		return
	}
	node := r.PostForm.Get("node")
	if node == "" {
		s.writeError(w, http.StatusBadRequest, "missing node")
		return
	}
	s.st.mu.Lock()
	rec := s.st.ha.resources[sid]
	if rec != nil {
		rec.Node = node
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchHAResource)
		return
	}
	s.writeData(w, map[string]string{"sid": sid, "requested-node": node})
}

// handleClusterOptionsGet returns the datacenter options block. HA owns the
// "crs" property-string (state.ha.crs); the cluster service's options
// (description, migration, …) live in state.cluster.options. Both are merged
// into one response, since PVE exposes them at the single /cluster/options.
func (s *Server) handleClusterOptionsGet(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.st.mu.Lock()
	out := make(map[string]string, len(s.st.cluster.options)+1)
	for k, v := range s.st.cluster.options {
		out[k] = v
	}
	if s.st.ha.crs != "" {
		out["crs"] = s.st.ha.crs
	}
	s.st.mu.Unlock()
	s.writeData(w, out)
}

// handleClusterOptionsSet stores datacenter options. "crs" routes to the HA
// state (so the HA CRS handlers keep working); every other key is stored as a
// cluster option. Synchronous.
func (s *Server) handleClusterOptionsSet(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidForm)
		return
	}
	s.st.mu.Lock()
	if s.st.cluster.options == nil {
		s.st.cluster.options = make(map[string]string)
	}
	for k, vs := range r.PostForm {
		if len(vs) == 0 {
			continue
		}
		if k == "crs" {
			s.st.ha.crs = vs[0]
			continue
		}
		s.st.cluster.options[k] = vs[0]
	}
	s.st.mu.Unlock()
	s.writeData(w, nil)
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

func (s *Server) handleHARuleList(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.st.mu.Lock()
	out := make([]haRulePayload, 0, len(s.st.ha.rules))
	for _, rec := range s.st.ha.rules {
		out = append(out, haRuleToPayload(rec))
	}
	s.st.mu.Unlock()
	s.writeData(w, out)
}

func (s *Server) handleHARuleGet(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	rule := r.PathValue("rule")
	s.st.mu.Lock()
	rec := s.st.ha.rules[rule]
	var payload haRulePayload
	if rec != nil {
		payload = haRuleToPayload(rec)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchHARule)
		return
	}
	s.writeData(w, payload)
}

// handleHARuleCreate defines a rule. Synchronous (data null, no task).
func (s *Server) handleHARuleCreate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidForm)
		return
	}
	rule := r.PostForm.Get("rule")
	if rule == "" {
		s.writeError(w, http.StatusBadRequest, "missing rule")
		return
	}
	rec := &haRuleRecord{
		Rule: rule, Type: r.PostForm.Get("type"),
		Nodes: r.PostForm.Get("nodes"), Resources: r.PostForm.Get("resources"),
		Affinity: r.PostForm.Get("affinity"), Comment: r.PostForm.Get("comment"),
		Disable: r.PostForm.Get("disable") == "1",
	}
	s.st.mu.Lock()
	if s.st.ha.rules == nil {
		s.st.ha.rules = make(map[string]*haRuleRecord)
	}
	s.st.ha.rules[rule] = rec
	s.st.mu.Unlock()
	s.writeData(w, nil)
}

// handleHARuleUpdate mutates a rule. Synchronous (data null).
func (s *Server) handleHARuleUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	rule := r.PathValue("rule")
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidForm)
		return
	}
	s.st.mu.Lock()
	rec := s.st.ha.rules[rule]
	if rec != nil {
		applyHARuleForm(rec, r.PostForm)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchHARule)
		return
	}
	s.writeData(w, nil)
}

// applyHARuleForm applies a PUT form's fields to rec. The caller holds st.mu.
func applyHARuleForm(rec *haRuleRecord, form url.Values) {
	if v := form.Get("nodes"); v != "" {
		rec.Nodes = v
	}
	if v := form.Get("resources"); v != "" {
		rec.Resources = v
	}
	if v := form.Get("affinity"); v != "" {
		rec.Affinity = v
	}
	if v := form.Get("comment"); v != "" {
		rec.Comment = v
	}
	if v := form.Get("disable"); v != "" {
		rec.Disable = v == "1"
	}
}

// handleHARuleDelete removes a rule. Synchronous.
func (s *Server) handleHARuleDelete(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	rule := r.PathValue("rule")
	s.st.mu.Lock()
	_, found := s.st.ha.rules[rule]
	if found {
		delete(s.st.ha.rules, rule)
	}
	s.st.mu.Unlock()
	if !found {
		s.writeError(w, http.StatusNotFound, msgNoSuchHARule)
		return
	}
	s.writeData(w, nil)
}

func haRuleToPayload(rec *haRuleRecord) haRulePayload {
	p := haRulePayload{
		Rule: rec.Rule, Type: rec.Type, Nodes: rec.Nodes, Resources: rec.Resources,
		Affinity: rec.Affinity, Comment: rec.Comment,
	}
	if rec.Disable {
		p.Disable = 1
	}
	return p
}
