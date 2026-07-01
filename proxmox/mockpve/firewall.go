package mockpve

import (
	"net/http"
	"strconv"
)

// firewallState is the firewall slice of the mock model, embedded in state and
// guarded by state.mu. PVE exposes the same firewall surface at three scopes
// (cluster, node, guest); the mock keys one fwScope per scope string
// ("cluster", "node:pve", "guest:qemu:100"), matching the SDK's scope model.
type firewallState struct {
	scopes map[string]*fwScope // keyed by scope string.
}

// fwScope holds one scope's rules, IPSets, and options.
type fwScope struct {
	rules   []*fwRuleRecord
	ipsets  map[string]*fwIPSetRecord // keyed by IPSet name.
	options map[string]string         // raw option key/value.
}

// fwRuleRecord is one firewall rule (its position is its index in fwScope.rules).
type fwRuleRecord struct {
	Type    string
	Action  string
	Enable  bool
	Macro   string
	Proto   string
	Source  string
	Dest    string
	Sport   string
	Dport   string
	Iface   string
	Log     string
	Comment string
}

// fwIPSetRecord is one named IPSet and its CIDR entries.
type fwIPSetRecord struct {
	Name    string
	Comment string
	entries map[string]*fwEntryRecord // keyed by CIDR.
}

// fwEntryRecord is one CIDR in an IPSet.
type fwEntryRecord struct {
	CIDR    string
	NoMatch bool
	Comment string
}

// fwRulePayload mirrors GET {scope}/firewall/rules entries.
type fwRulePayload struct {
	Pos     int    `json:"pos"`
	Type    string `json:"type,omitempty"`
	Action  string `json:"action,omitempty"`
	Enable  int    `json:"enable,omitempty"`
	Macro   string `json:"macro,omitempty"`
	Proto   string `json:"proto,omitempty"`
	Source  string `json:"source,omitempty"`
	Dest    string `json:"dest,omitempty"`
	Sport   string `json:"sport,omitempty"`
	Dport   string `json:"dport,omitempty"`
	Iface   string `json:"iface,omitempty"`
	Log     string `json:"log,omitempty"`
	Comment string `json:"comment,omitempty"`
}

// fwIPSetPayload mirrors GET {scope}/firewall/ipset entries.
type fwIPSetPayload struct {
	Name    string `json:"name"`
	Comment string `json:"comment,omitempty"`
}

// fwEntryPayload mirrors GET {scope}/firewall/ipset/{name} entries.
type fwEntryPayload struct {
	CIDR    string `json:"cidr"`
	NoMatch int    `json:"nomatch,omitempty"`
	Comment string `json:"comment,omitempty"`
}

// fwScopeKeyFunc derives a scope key from a request (its path wildcards).
type fwScopeKeyFunc func(*http.Request) string

// fwScopeLocked returns the fwScope for key, creating it on first use. The
// caller holds st.mu.
func (s *Server) fwScopeLocked(key string) *fwScope {
	if s.st.firewall.scopes == nil {
		s.st.firewall.scopes = make(map[string]*fwScope)
	}
	sc := s.st.firewall.scopes[key]
	if sc == nil {
		sc = &fwScope{ipsets: make(map[string]*fwIPSetRecord), options: make(map[string]string)}
		s.st.firewall.scopes[key] = sc
	}
	return sc
}

func (s *Server) registerFirewallRoutes() {
	s.registerFirewallScope("/api2/json/cluster/firewall",
		func(_ *http.Request) string { return "cluster" })
	s.registerFirewallScope("/api2/json/nodes/{node}/firewall",
		func(r *http.Request) string { return "node:" + r.PathValue("node") })
	s.registerFirewallScope("/api2/json/nodes/{node}/{kind}/{vmid}/firewall",
		func(r *http.Request) string { return "guest:" + r.PathValue("kind") + ":" + r.PathValue("vmid") })
}

// registerFirewallScope wires the identical rule/IPSet/options routes for one
// scope prefix; key turns a matched request into its scope string.
func (s *Server) registerFirewallScope(prefix string, key fwScopeKeyFunc) {
	s.mux.HandleFunc("GET "+prefix+"/rules", s.fwRuleList(key))
	s.mux.HandleFunc("POST "+prefix+"/rules", s.fwRuleCreate(key))
	s.mux.HandleFunc("GET "+prefix+"/rules/{pos}", s.fwRuleGet(key))
	s.mux.HandleFunc("PUT "+prefix+"/rules/{pos}", s.fwRuleUpdate(key))
	s.mux.HandleFunc("DELETE "+prefix+"/rules/{pos}", s.fwRuleDelete(key))
	s.mux.HandleFunc("GET "+prefix+"/ipset", s.fwIPSetList(key))
	s.mux.HandleFunc("POST "+prefix+"/ipset", s.fwIPSetCreate(key))
	s.mux.HandleFunc("GET "+prefix+"/ipset/{name}", s.fwIPSetEntryList(key))
	s.mux.HandleFunc("POST "+prefix+"/ipset/{name}", s.fwIPSetEntryAdd(key))
	s.mux.HandleFunc("DELETE "+prefix+"/ipset/{name}", s.fwIPSetDelete(key))
	s.mux.HandleFunc("DELETE "+prefix+"/ipset/{name}/{cidr}", s.fwIPSetEntryDelete(key))
	s.mux.HandleFunc("GET "+prefix+"/options", s.fwOptionsGet(key))
	s.mux.HandleFunc("PUT "+prefix+"/options", s.fwOptionsSet(key))
}

func (s *Server) fwRuleList(key fwScopeKeyFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.checkAuth(w, r) {
			return
		}
		s.st.mu.Lock()
		sc := s.fwScopeLocked(key(r))
		out := make([]fwRulePayload, 0, len(sc.rules))
		for i, rec := range sc.rules {
			out = append(out, fwRuleToPayload(i, rec))
		}
		s.st.mu.Unlock()
		s.writeData(w, out)
	}
}

func (s *Server) fwRuleCreate(key fwScopeKeyFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.checkAuth(w, r) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
		if err := r.ParseForm(); err != nil {
			s.writeError(w, http.StatusBadRequest, msgInvalidForm)
			return
		}
		rec := fwRuleFromForm(r)
		s.st.mu.Lock()
		sc := s.fwScopeLocked(key(r))
		sc.rules = append(sc.rules, rec)
		s.st.mu.Unlock()
		s.writeData(w, nil)
	}
}

func (s *Server) fwRuleGet(key fwScopeKeyFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.checkAuth(w, r) {
			return
		}
		pos, ok := fwParsePos(r)
		s.st.mu.Lock()
		sc := s.fwScopeLocked(key(r))
		var payload fwRulePayload
		found := ok && pos < len(sc.rules)
		if found {
			payload = fwRuleToPayload(pos, sc.rules[pos])
		}
		s.st.mu.Unlock()
		if !found {
			s.writeError(w, http.StatusNotFound, msgNoSuchRule)
			return
		}
		s.writeData(w, payload)
	}
}

func (s *Server) fwRuleUpdate(key fwScopeKeyFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.checkAuth(w, r) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
		if err := r.ParseForm(); err != nil {
			s.writeError(w, http.StatusBadRequest, msgInvalidForm)
			return
		}
		pos, ok := fwParsePos(r)
		s.st.mu.Lock()
		sc := s.fwScopeLocked(key(r))
		found := ok && pos < len(sc.rules)
		if found {
			fwApplyRuleForm(sc.rules[pos], r)
		}
		s.st.mu.Unlock()
		if !found {
			s.writeError(w, http.StatusNotFound, msgNoSuchRule)
			return
		}
		s.writeData(w, nil)
	}
}

func (s *Server) fwRuleDelete(key fwScopeKeyFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.checkAuth(w, r) {
			return
		}
		pos, ok := fwParsePos(r)
		s.st.mu.Lock()
		sc := s.fwScopeLocked(key(r))
		found := ok && pos < len(sc.rules)
		if found {
			sc.rules = append(sc.rules[:pos], sc.rules[pos+1:]...)
		}
		s.st.mu.Unlock()
		if !found {
			s.writeError(w, http.StatusNotFound, msgNoSuchRule)
			return
		}
		s.writeData(w, nil)
	}
}

func (s *Server) fwIPSetList(key fwScopeKeyFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.checkAuth(w, r) {
			return
		}
		s.st.mu.Lock()
		sc := s.fwScopeLocked(key(r))
		out := make([]fwIPSetPayload, 0, len(sc.ipsets))
		for _, rec := range sc.ipsets {
			out = append(out, fwIPSetPayload{Name: rec.Name, Comment: rec.Comment})
		}
		s.st.mu.Unlock()
		s.writeData(w, out)
	}
}

// fwIPSetCreate creates an IPSet, or renames one when a "rename" param is set
// (PVE renames via POST to the collection with name=new, rename=old).
func (s *Server) fwIPSetCreate(key fwScopeKeyFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.checkAuth(w, r) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
		if err := r.ParseForm(); err != nil {
			s.writeError(w, http.StatusBadRequest, msgInvalidForm)
			return
		}
		name := r.PostForm.Get("name")
		if name == "" {
			s.writeError(w, http.StatusBadRequest, "missing name")
			return
		}
		if old := r.PostForm.Get("rename"); old != "" {
			s.fwRenameIPSet(w, key(r), old, name)
			return
		}
		s.st.mu.Lock()
		sc := s.fwScopeLocked(key(r))
		sc.ipsets[name] = &fwIPSetRecord{
			Name: name, Comment: r.PostForm.Get("comment"),
			entries: make(map[string]*fwEntryRecord),
		}
		s.st.mu.Unlock()
		s.writeData(w, nil)
	}
}

// fwRenameIPSet moves the IPSet old to new within scope key.
func (s *Server) fwRenameIPSet(w http.ResponseWriter, scopeKey, old, newName string) {
	s.st.mu.Lock()
	sc := s.fwScopeLocked(scopeKey)
	rec, found := sc.ipsets[old]
	if found {
		rec.Name = newName
		sc.ipsets[newName] = rec
		delete(sc.ipsets, old)
	}
	s.st.mu.Unlock()
	if !found {
		s.writeError(w, http.StatusNotFound, msgNoSuchIPSet)
		return
	}
	s.writeData(w, nil)
}

func (s *Server) fwIPSetDelete(key fwScopeKeyFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.checkAuth(w, r) {
			return
		}
		name := r.PathValue("name")
		s.st.mu.Lock()
		sc := s.fwScopeLocked(key(r))
		_, found := sc.ipsets[name]
		if found {
			delete(sc.ipsets, name)
		}
		s.st.mu.Unlock()
		if !found {
			s.writeError(w, http.StatusNotFound, msgNoSuchIPSet)
			return
		}
		s.writeData(w, nil)
	}
}

func (s *Server) fwIPSetEntryList(key fwScopeKeyFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.checkAuth(w, r) {
			return
		}
		name := r.PathValue("name")
		s.st.mu.Lock()
		sc := s.fwScopeLocked(key(r))
		rec, found := sc.ipsets[name]
		var out []fwEntryPayload
		if found {
			out = make([]fwEntryPayload, 0, len(rec.entries))
			for _, e := range rec.entries {
				out = append(out, fwEntryToPayload(e))
			}
		}
		s.st.mu.Unlock()
		if !found {
			s.writeError(w, http.StatusNotFound, msgNoSuchIPSet)
			return
		}
		s.writeData(w, out)
	}
}

func (s *Server) fwIPSetEntryAdd(key fwScopeKeyFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.checkAuth(w, r) {
			return
		}
		name := r.PathValue("name")
		r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
		if err := r.ParseForm(); err != nil {
			s.writeError(w, http.StatusBadRequest, msgInvalidForm)
			return
		}
		cidr := r.PostForm.Get("cidr")
		if cidr == "" {
			s.writeError(w, http.StatusBadRequest, "missing cidr")
			return
		}
		s.st.mu.Lock()
		sc := s.fwScopeLocked(key(r))
		rec, found := sc.ipsets[name]
		if found {
			rec.entries[cidr] = &fwEntryRecord{
				CIDR: cidr, Comment: r.PostForm.Get("comment"),
				NoMatch: r.PostForm.Get("nomatch") == "1",
			}
		}
		s.st.mu.Unlock()
		if !found {
			s.writeError(w, http.StatusNotFound, msgNoSuchIPSet)
			return
		}
		s.writeData(w, nil)
	}
}

func (s *Server) fwIPSetEntryDelete(key fwScopeKeyFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.checkAuth(w, r) {
			return
		}
		name := r.PathValue("name")
		cidr := r.PathValue("cidr")
		s.st.mu.Lock()
		sc := s.fwScopeLocked(key(r))
		rec, found := sc.ipsets[name]
		if found {
			if _, ok := rec.entries[cidr]; ok {
				delete(rec.entries, cidr)
			} else {
				found = false
			}
		}
		s.st.mu.Unlock()
		if !found {
			s.writeError(w, http.StatusNotFound, msgNoSuchIPSetEntry)
			return
		}
		s.writeData(w, nil)
	}
}

func (s *Server) fwOptionsGet(key fwScopeKeyFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.checkAuth(w, r) {
			return
		}
		s.st.mu.Lock()
		sc := s.fwScopeLocked(key(r))
		out := make(map[string]string, len(sc.options))
		for k, v := range sc.options {
			out[k] = v
		}
		s.st.mu.Unlock()
		s.writeData(w, out)
	}
}

func (s *Server) fwOptionsSet(key fwScopeKeyFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.checkAuth(w, r) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
		if err := r.ParseForm(); err != nil {
			s.writeError(w, http.StatusBadRequest, msgInvalidForm)
			return
		}
		s.st.mu.Lock()
		sc := s.fwScopeLocked(key(r))
		for k, vs := range r.PostForm {
			if len(vs) > 0 {
				sc.options[k] = vs[0]
			}
		}
		s.st.mu.Unlock()
		s.writeData(w, nil)
	}
}

// fwParsePos reads the {pos} path value as a non-negative rule index.
func fwParsePos(r *http.Request) (int, bool) {
	pos, err := strconv.Atoi(r.PathValue("pos"))
	if err != nil || pos < 0 {
		return 0, false
	}
	return pos, true
}

// fwRuleFromForm builds a rule record from a create form.
func fwRuleFromForm(r *http.Request) *fwRuleRecord {
	return &fwRuleRecord{
		Type: r.PostForm.Get("type"), Action: r.PostForm.Get("action"),
		Enable: r.PostForm.Get("enable") == "1", Macro: r.PostForm.Get("macro"),
		Proto: r.PostForm.Get("proto"), Source: r.PostForm.Get("source"),
		Dest: r.PostForm.Get("dest"), Sport: r.PostForm.Get("sport"),
		Dport: r.PostForm.Get("dport"), Iface: r.PostForm.Get("iface"),
		Log: r.PostForm.Get("log"), Comment: r.PostForm.Get("comment"),
	}
}

// fwApplyRuleForm applies a PUT form's set fields to rec. The caller holds st.mu.
func fwApplyRuleForm(rec *fwRuleRecord, r *http.Request) {
	set := func(dst *string, field string) {
		if v := r.PostForm.Get(field); v != "" {
			*dst = v
		}
	}
	set(&rec.Type, "type")
	set(&rec.Action, "action")
	set(&rec.Macro, "macro")
	set(&rec.Proto, "proto")
	set(&rec.Source, "source")
	set(&rec.Dest, "dest")
	set(&rec.Sport, "sport")
	set(&rec.Dport, "dport")
	set(&rec.Iface, "iface")
	set(&rec.Log, "log")
	set(&rec.Comment, "comment")
	if v := r.PostForm.Get("enable"); v != "" {
		rec.Enable = v == "1"
	}
}

func fwRuleToPayload(pos int, rec *fwRuleRecord) fwRulePayload {
	p := fwRulePayload{
		Pos: pos, Type: rec.Type, Action: rec.Action, Macro: rec.Macro,
		Proto: rec.Proto, Source: rec.Source, Dest: rec.Dest, Sport: rec.Sport,
		Dport: rec.Dport, Iface: rec.Iface, Log: rec.Log, Comment: rec.Comment,
	}
	if rec.Enable {
		p.Enable = 1
	}
	return p
}

func fwEntryToPayload(e *fwEntryRecord) fwEntryPayload {
	p := fwEntryPayload{CIDR: e.CIDR, Comment: e.Comment}
	if e.NoMatch {
		p.NoMatch = 1
	}
	return p
}
