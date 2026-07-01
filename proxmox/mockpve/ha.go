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
}

// handleClusterOptionsGet returns the datacenter options the mock models (just
// the "crs" key today).
func (s *Server) handleClusterOptionsGet(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.st.mu.Lock()
	crs := s.st.ha.crs
	s.st.mu.Unlock()
	s.writeData(w, map[string]string{"crs": crs})
}

// handleClusterOptionsSet stores the "crs" property-string. Synchronous.
func (s *Server) handleClusterOptionsSet(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidForm)
		return
	}
	if v := r.PostForm.Get("crs"); v != "" {
		s.st.mu.Lock()
		s.st.ha.crs = v
		s.st.mu.Unlock()
	}
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
