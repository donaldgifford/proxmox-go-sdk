package mockpve

import (
	"net/http"
	"net/url"
)

// netIfaceRecord is one node network interface in the mock.
type netIfaceRecord struct {
	Iface     string
	Type      string
	Address   string
	Gateway   string
	Ports     string
	VLANAware bool
	Autostart bool
	Comments  string
}

// netIfacePayload mirrors GET /nodes/{node}/network entries.
type netIfacePayload struct {
	Iface       string `json:"iface"`
	Type        string `json:"type,omitempty"`
	Address     string `json:"address,omitempty"`
	Gateway     string `json:"gateway,omitempty"`
	BridgePorts string `json:"bridge_ports,omitempty"`
	VLANAware   int    `json:"bridge_vlan_aware,omitempty"`
	Autostart   int    `json:"autostart,omitempty"`
	Comments    string `json:"comments,omitempty"`
}

// AddInterface seeds a network interface on node. Call before serving; the node
// is registered if absent.
func (s *Server) AddInterface(node, iface, ifaceType string) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	n := s.ensureNodeLocked(node)
	if n.netIfaces == nil {
		n.netIfaces = make(map[string]*netIfaceRecord)
	}
	n.netIfaces[iface] = &netIfaceRecord{Iface: iface, Type: ifaceType}
}

// ensureNodeLocked returns node's state, creating it if absent. Caller holds mu.
func (s *Server) ensureNodeLocked(node string) *nodeState {
	n, ok := s.st.nodes[node]
	if !ok {
		n = &nodeState{tasks: make(map[string]*taskRecord)}
		s.st.nodes[node] = n
	}
	return n
}

func (s *Server) registerNodeNetworkRoutes() {
	s.mux.HandleFunc("GET /api2/json/nodes/{node}/network", s.handleNetworkList)
	s.mux.HandleFunc("POST /api2/json/nodes/{node}/network", s.handleNetworkCreate)
	s.mux.HandleFunc("PUT /api2/json/nodes/{node}/network", s.handleNetworkApply)
	s.mux.HandleFunc("GET /api2/json/nodes/{node}/network/{iface}", s.handleNetworkGet)
	s.mux.HandleFunc("PUT /api2/json/nodes/{node}/network/{iface}", s.handleNetworkUpdate)
	s.mux.HandleFunc("DELETE /api2/json/nodes/{node}/network/{iface}", s.handleNetworkDelete)
}

func (s *Server) handleNetworkList(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	s.st.mu.Lock()
	var out []netIfacePayload
	if n := s.st.nodes[node]; n != nil {
		out = make([]netIfacePayload, 0, len(n.netIfaces))
		for _, rec := range n.netIfaces {
			out = append(out, netIfaceToPayload(rec))
		}
	}
	s.st.mu.Unlock()
	s.writeData(w, out)
}

func (s *Server) handleNetworkGet(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node, iface := r.PathValue("node"), r.PathValue("iface")
	s.st.mu.Lock()
	rec := s.lookupIfaceLocked(node, iface)
	var payload netIfacePayload
	if rec != nil {
		payload = netIfaceToPayload(rec)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchIface)
		return
	}
	s.writeData(w, payload)
}

// handleNetworkCreate stages a new interface. Synchronous (data null, no task).
func (s *Server) handleNetworkCreate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidForm)
		return
	}
	iface := r.PostForm.Get("iface")
	if iface == "" {
		s.writeError(w, http.StatusBadRequest, "missing iface")
		return
	}
	rec := &netIfaceRecord{
		Iface: iface, Type: r.PostForm.Get("type"),
		Address: r.PostForm.Get("address"), Gateway: r.PostForm.Get("gateway"),
		Ports: r.PostForm.Get("bridge_ports"), Comments: r.PostForm.Get("comments"),
		VLANAware: r.PostForm.Get("bridge_vlan_aware") == "1",
		Autostart: r.PostForm.Get("autostart") == "1",
	}
	s.st.mu.Lock()
	n := s.ensureNodeLocked(node)
	if n.netIfaces == nil {
		n.netIfaces = make(map[string]*netIfaceRecord)
	}
	n.netIfaces[iface] = rec
	s.st.mu.Unlock()
	s.writeData(w, nil)
}

// handleNetworkUpdate mutates a staged interface. Synchronous.
func (s *Server) handleNetworkUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node, iface := r.PathValue("node"), r.PathValue("iface")
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidForm)
		return
	}
	s.st.mu.Lock()
	rec := s.lookupIfaceLocked(node, iface)
	if rec != nil {
		applyIfaceForm(rec, r.PostForm)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchIface)
		return
	}
	s.writeData(w, nil)
}

// applyIfaceForm applies a PUT form's fields to rec. The caller holds st.mu.
func applyIfaceForm(rec *netIfaceRecord, form url.Values) {
	if v := form.Get("address"); v != "" {
		rec.Address = v
	}
	if v := form.Get("gateway"); v != "" {
		rec.Gateway = v
	}
	if v := form.Get("bridge_ports"); v != "" {
		rec.Ports = v
	}
	if v := form.Get("comments"); v != "" {
		rec.Comments = v
	}
	if v := form.Get("bridge_vlan_aware"); v != "" {
		rec.VLANAware = v == "1"
	}
	if v := form.Get("autostart"); v != "" {
		rec.Autostart = v == "1"
	}
}

// handleNetworkDelete removes a staged interface. Synchronous.
func (s *Server) handleNetworkDelete(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node, iface := r.PathValue("node"), r.PathValue("iface")
	s.st.mu.Lock()
	rec := s.lookupIfaceLocked(node, iface)
	if rec != nil {
		delete(s.st.nodes[node].netIfaces, iface)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchIface)
		return
	}
	s.writeData(w, nil)
}

// handleNetworkApply activates pending network config and returns a reload task
// (exercising the SDK's async-apply path).
func (s *Server) handleNetworkApply(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	s.writeData(w, s.finishedTask(node, "srvreload", "networking"))
}

// lookupIfaceLocked returns node's iface record or nil. Caller holds mu.
func (s *Server) lookupIfaceLocked(node, iface string) *netIfaceRecord {
	n := s.st.nodes[node]
	if n == nil {
		return nil
	}
	return n.netIfaces[iface]
}

func netIfaceToPayload(rec *netIfaceRecord) netIfacePayload {
	return netIfacePayload{
		Iface: rec.Iface, Type: rec.Type, Address: rec.Address, Gateway: rec.Gateway,
		BridgePorts: rec.Ports, VLANAware: boolToInt(rec.VLANAware),
		Autostart: boolToInt(rec.Autostart), Comments: rec.Comments,
	}
}
