package mockpve

import (
	"net/http"
	"net/url"
	"strconv"
)

// sdnState is the SDN slice of the mock model, embedded in state and guarded by
// state.mu. SDN is cluster-scoped, so records are not keyed by node. The mock
// does not model the pending/applied config split — writes take effect
// immediately and ApplySDN is a no-op that returns success.
type sdnState struct {
	zones   map[string]*sdnZoneRecord   // keyed by zone name.
	vnets   map[string]*sdnVNetRecord   // keyed by vnet name.
	fabrics map[string]*sdnFabricRecord // keyed by fabric id.
	// subnets is keyed by vnet, then by subnet CIDR.
	subnets map[string]map[string]*sdnSubnetRecord
}

// sdnZoneRecord is one SDN zone.
type sdnZoneRecord struct {
	Zone       string
	Type       string
	MTU        int
	Nodes      string
	IPAM       string
	Controller string
	VRFVXLan   int
	Peers      string
	Bridge     string
}

// sdnVNetRecord is one SDN VNet.
type sdnVNetRecord struct {
	VNet  string
	Zone  string
	Tag   int
	Alias string
}

// sdnSubnetRecord is one subnet under a VNet.
type sdnSubnetRecord struct {
	Subnet  string
	VNet    string
	Gateway string
	SNAT    bool
}

// sdnFabricRecord is one SDN fabric.
type sdnFabricRecord struct {
	Fabric   string
	Protocol string
	Nodes    string
	Comment  string
}

// sdnZonePayload mirrors GET /cluster/sdn/zones entries.
type sdnZonePayload struct {
	Zone       string `json:"zone"`
	Type       string `json:"type,omitempty"`
	MTU        int    `json:"mtu,omitempty"`
	Nodes      string `json:"nodes,omitempty"`
	IPAM       string `json:"ipam,omitempty"`
	Controller string `json:"controller,omitempty"`
	VRFVXLan   int    `json:"vrf-vxlan,omitempty"`
	Peers      string `json:"peers,omitempty"`
	Bridge     string `json:"bridge,omitempty"`
}

// sdnVNetPayload mirrors GET /cluster/sdn/vnets entries.
type sdnVNetPayload struct {
	VNet  string `json:"vnet"`
	Zone  string `json:"zone,omitempty"`
	Tag   int    `json:"tag,omitempty"`
	Alias string `json:"alias,omitempty"`
}

// sdnSubnetPayload mirrors GET /cluster/sdn/vnets/{vnet}/subnets entries.
type sdnSubnetPayload struct {
	Subnet  string `json:"subnet"`
	VNet    string `json:"vnet,omitempty"`
	Gateway string `json:"gateway,omitempty"`
	SNAT    int    `json:"snat,omitempty"`
}

// sdnFabricPayload mirrors GET /cluster/sdn/fabrics entries.
type sdnFabricPayload struct {
	Fabric   string `json:"id"`
	Protocol string `json:"protocol,omitempty"`
	Nodes    string `json:"nodes,omitempty"`
	Comment  string `json:"comment,omitempty"`
}

// AddZone seeds an SDN zone. Call before serving.
func (s *Server) AddZone(zone, zoneType string) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	if s.st.sdn.zones == nil {
		s.st.sdn.zones = make(map[string]*sdnZoneRecord)
	}
	s.st.sdn.zones[zone] = &sdnZoneRecord{Zone: zone, Type: zoneType}
}

// AddVNet seeds an SDN VNet in a zone. Call before serving.
func (s *Server) AddVNet(vnet, zone string) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	if s.st.sdn.vnets == nil {
		s.st.sdn.vnets = make(map[string]*sdnVNetRecord)
	}
	s.st.sdn.vnets[vnet] = &sdnVNetRecord{VNet: vnet, Zone: zone}
}

// AddSubnet seeds a subnet (a CIDR) under a VNet. Call before serving.
func (s *Server) AddSubnet(vnet, subnet string) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	s.ensureSubnetVNetLocked(vnet)
	s.st.sdn.subnets[vnet][subnet] = &sdnSubnetRecord{Subnet: subnet, VNet: vnet}
}

// AddFabric seeds an SDN fabric. Call before serving.
func (s *Server) AddFabric(fabric, protocol string) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	if s.st.sdn.fabrics == nil {
		s.st.sdn.fabrics = make(map[string]*sdnFabricRecord)
	}
	s.st.sdn.fabrics[fabric] = &sdnFabricRecord{Fabric: fabric, Protocol: protocol}
}

// ensureSubnetVNetLocked makes the subnet maps ready for vnet. Caller holds mu.
func (s *Server) ensureSubnetVNetLocked(vnet string) {
	if s.st.sdn.subnets == nil {
		s.st.sdn.subnets = make(map[string]map[string]*sdnSubnetRecord)
	}
	if s.st.sdn.subnets[vnet] == nil {
		s.st.sdn.subnets[vnet] = make(map[string]*sdnSubnetRecord)
	}
}

func (s *Server) registerSDNRoutes() {
	s.mux.HandleFunc("GET /api2/json/cluster/sdn/zones", s.handleZoneList)
	s.mux.HandleFunc("POST /api2/json/cluster/sdn/zones", s.handleZoneCreate)
	s.mux.HandleFunc("GET /api2/json/cluster/sdn/zones/{zone}", s.handleZoneGet)
	s.mux.HandleFunc("PUT /api2/json/cluster/sdn/zones/{zone}", s.handleZoneUpdate)
	s.mux.HandleFunc("DELETE /api2/json/cluster/sdn/zones/{zone}", s.handleZoneDelete)
	s.mux.HandleFunc("GET /api2/json/cluster/sdn/vnets", s.handleVNetList)
	s.mux.HandleFunc("POST /api2/json/cluster/sdn/vnets", s.handleVNetCreate)
	s.mux.HandleFunc("GET /api2/json/cluster/sdn/vnets/{vnet}", s.handleVNetGet)
	s.mux.HandleFunc("PUT /api2/json/cluster/sdn/vnets/{vnet}", s.handleVNetUpdate)
	s.mux.HandleFunc("DELETE /api2/json/cluster/sdn/vnets/{vnet}", s.handleVNetDelete)
	s.mux.HandleFunc("GET /api2/json/cluster/sdn/vnets/{vnet}/subnets", s.handleSubnetList)
	s.mux.HandleFunc("POST /api2/json/cluster/sdn/vnets/{vnet}/subnets", s.handleSubnetCreate)
	s.mux.HandleFunc("GET /api2/json/cluster/sdn/vnets/{vnet}/subnets/{subnet}", s.handleSubnetGet)
	s.mux.HandleFunc("PUT /api2/json/cluster/sdn/vnets/{vnet}/subnets/{subnet}", s.handleSubnetUpdate)
	s.mux.HandleFunc("DELETE /api2/json/cluster/sdn/vnets/{vnet}/subnets/{subnet}", s.handleSubnetDelete)
	s.mux.HandleFunc("GET /api2/json/cluster/sdn/fabrics", s.handleFabricList)
	s.mux.HandleFunc("POST /api2/json/cluster/sdn/fabrics", s.handleFabricCreate)
	s.mux.HandleFunc("GET /api2/json/cluster/sdn/fabrics/{fabric}", s.handleFabricGet)
	s.mux.HandleFunc("PUT /api2/json/cluster/sdn/fabrics/{fabric}", s.handleFabricUpdate)
	s.mux.HandleFunc("DELETE /api2/json/cluster/sdn/fabrics/{fabric}", s.handleFabricDelete)
	s.mux.HandleFunc("PUT /api2/json/cluster/sdn", s.handleSDNApply)
}

// handleSDNApply commits the pending SDN config. The mock applies writes
// immediately, so this just returns success. Synchronous (data null).
func (s *Server) handleSDNApply(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.writeData(w, nil)
}

func (s *Server) handleZoneList(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.st.mu.Lock()
	out := make([]sdnZonePayload, 0, len(s.st.sdn.zones))
	for _, rec := range s.st.sdn.zones {
		out = append(out, sdnZoneToPayload(rec))
	}
	s.st.mu.Unlock()
	s.writeData(w, out)
}

func (s *Server) handleZoneGet(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	zone := r.PathValue("zone")
	s.st.mu.Lock()
	rec := s.st.sdn.zones[zone]
	var payload sdnZonePayload
	if rec != nil {
		payload = sdnZoneToPayload(rec)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchZone)
		return
	}
	s.writeData(w, payload)
}

// handleZoneCreate defines a zone. Synchronous (data null, no task).
func (s *Server) handleZoneCreate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidForm)
		return
	}
	zone := r.PostForm.Get("zone")
	if zone == "" {
		s.writeError(w, http.StatusBadRequest, "missing zone")
		return
	}
	rec := &sdnZoneRecord{
		Zone: zone, Type: r.PostForm.Get("type"), Nodes: r.PostForm.Get("nodes"),
		IPAM: r.PostForm.Get("ipam"), Controller: r.PostForm.Get("controller"),
		Peers: r.PostForm.Get("peers"), Bridge: r.PostForm.Get("bridge"),
	}
	if v, err := strconv.Atoi(r.PostForm.Get("mtu")); err == nil {
		rec.MTU = v
	}
	if v, err := strconv.Atoi(r.PostForm.Get("vrf-vxlan")); err == nil {
		rec.VRFVXLan = v
	}
	s.st.mu.Lock()
	if s.st.sdn.zones == nil {
		s.st.sdn.zones = make(map[string]*sdnZoneRecord)
	}
	s.st.sdn.zones[zone] = rec
	s.st.mu.Unlock()
	s.writeData(w, nil)
}

// handleZoneUpdate mutates a zone. Synchronous.
func (s *Server) handleZoneUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	zone := r.PathValue("zone")
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidForm)
		return
	}
	s.st.mu.Lock()
	rec := s.st.sdn.zones[zone]
	if rec != nil {
		applyZoneForm(rec, r.PostForm)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchZone)
		return
	}
	s.writeData(w, nil)
}

// applyZoneForm applies a PUT form's fields to rec. The caller holds st.mu.
func applyZoneForm(rec *sdnZoneRecord, form url.Values) {
	if v := form.Get("nodes"); v != "" {
		rec.Nodes = v
	}
	if v := form.Get("ipam"); v != "" {
		rec.IPAM = v
	}
	if v, err := strconv.Atoi(form.Get("mtu")); err == nil {
		rec.MTU = v
	}
}

// handleZoneDelete removes a zone. Synchronous.
func (s *Server) handleZoneDelete(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	zone := r.PathValue("zone")
	s.st.mu.Lock()
	_, found := s.st.sdn.zones[zone]
	if found {
		delete(s.st.sdn.zones, zone)
	}
	s.st.mu.Unlock()
	if !found {
		s.writeError(w, http.StatusNotFound, msgNoSuchZone)
		return
	}
	s.writeData(w, nil)
}

func sdnZoneToPayload(rec *sdnZoneRecord) sdnZonePayload {
	return sdnZonePayload{
		Zone: rec.Zone, Type: rec.Type, MTU: rec.MTU, Nodes: rec.Nodes,
		IPAM: rec.IPAM, Controller: rec.Controller, VRFVXLan: rec.VRFVXLan,
		Peers: rec.Peers, Bridge: rec.Bridge,
	}
}

func (s *Server) handleVNetList(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.st.mu.Lock()
	out := make([]sdnVNetPayload, 0, len(s.st.sdn.vnets))
	for _, rec := range s.st.sdn.vnets {
		out = append(out, sdnVNetToPayload(rec))
	}
	s.st.mu.Unlock()
	s.writeData(w, out)
}

func (s *Server) handleVNetGet(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	vnet := r.PathValue("vnet")
	s.st.mu.Lock()
	rec := s.st.sdn.vnets[vnet]
	var payload sdnVNetPayload
	if rec != nil {
		payload = sdnVNetToPayload(rec)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchVNet)
		return
	}
	s.writeData(w, payload)
}

// handleVNetCreate defines a VNet. Synchronous.
func (s *Server) handleVNetCreate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidForm)
		return
	}
	vnet := r.PostForm.Get("vnet")
	if vnet == "" {
		s.writeError(w, http.StatusBadRequest, "missing vnet")
		return
	}
	rec := &sdnVNetRecord{VNet: vnet, Zone: r.PostForm.Get("zone"), Alias: r.PostForm.Get("alias")}
	if v, err := strconv.Atoi(r.PostForm.Get("tag")); err == nil {
		rec.Tag = v
	}
	s.st.mu.Lock()
	if s.st.sdn.vnets == nil {
		s.st.sdn.vnets = make(map[string]*sdnVNetRecord)
	}
	s.st.sdn.vnets[vnet] = rec
	s.st.mu.Unlock()
	s.writeData(w, nil)
}

// handleVNetUpdate mutates a VNet. Synchronous.
func (s *Server) handleVNetUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	vnet := r.PathValue("vnet")
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidForm)
		return
	}
	s.st.mu.Lock()
	rec := s.st.sdn.vnets[vnet]
	if rec != nil {
		if v := r.PostForm.Get("alias"); v != "" {
			rec.Alias = v
		}
		if v, err := strconv.Atoi(r.PostForm.Get("tag")); err == nil {
			rec.Tag = v
		}
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchVNet)
		return
	}
	s.writeData(w, nil)
}

// handleVNetDelete removes a VNet. Synchronous.
func (s *Server) handleVNetDelete(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	vnet := r.PathValue("vnet")
	s.st.mu.Lock()
	_, found := s.st.sdn.vnets[vnet]
	if found {
		delete(s.st.sdn.vnets, vnet)
	}
	s.st.mu.Unlock()
	if !found {
		s.writeError(w, http.StatusNotFound, msgNoSuchVNet)
		return
	}
	s.writeData(w, nil)
}

func sdnVNetToPayload(rec *sdnVNetRecord) sdnVNetPayload {
	return sdnVNetPayload{VNet: rec.VNet, Zone: rec.Zone, Tag: rec.Tag, Alias: rec.Alias}
}

func (s *Server) handleSubnetList(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	vnet := r.PathValue("vnet")
	s.st.mu.Lock()
	subnets := s.st.sdn.subnets[vnet]
	out := make([]sdnSubnetPayload, 0, len(subnets))
	for _, rec := range subnets {
		out = append(out, sdnSubnetToPayload(rec))
	}
	s.st.mu.Unlock()
	s.writeData(w, out)
}

func (s *Server) handleSubnetGet(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	vnet := r.PathValue("vnet")
	subnet := r.PathValue("subnet")
	s.st.mu.Lock()
	rec := s.lookupSubnetLocked(vnet, subnet)
	var payload sdnSubnetPayload
	if rec != nil {
		payload = sdnSubnetToPayload(rec)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchSubnet)
		return
	}
	s.writeData(w, payload)
}

// lookupSubnetLocked returns the subnet record or nil. The caller holds st.mu.
func (s *Server) lookupSubnetLocked(vnet, subnet string) *sdnSubnetRecord {
	if s.st.sdn.subnets[vnet] == nil {
		return nil
	}
	return s.st.sdn.subnets[vnet][subnet]
}

// handleSubnetCreate defines a subnet under a VNet. Synchronous.
func (s *Server) handleSubnetCreate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	vnet := r.PathValue("vnet")
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidForm)
		return
	}
	subnet := r.PostForm.Get("subnet")
	if subnet == "" {
		s.writeError(w, http.StatusBadRequest, "missing subnet")
		return
	}
	rec := &sdnSubnetRecord{
		Subnet: subnet, VNet: vnet, Gateway: r.PostForm.Get("gateway"),
		SNAT: r.PostForm.Get("snat") == "1",
	}
	s.st.mu.Lock()
	s.ensureSubnetVNetLocked(vnet)
	s.st.sdn.subnets[vnet][subnet] = rec
	s.st.mu.Unlock()
	s.writeData(w, nil)
}

// handleSubnetUpdate mutates a subnet. Synchronous.
func (s *Server) handleSubnetUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	vnet := r.PathValue("vnet")
	subnet := r.PathValue("subnet")
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidForm)
		return
	}
	s.st.mu.Lock()
	rec := s.lookupSubnetLocked(vnet, subnet)
	if rec != nil {
		if v := r.PostForm.Get("gateway"); v != "" {
			rec.Gateway = v
		}
		if v := r.PostForm.Get("snat"); v != "" {
			rec.SNAT = v == "1"
		}
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchSubnet)
		return
	}
	s.writeData(w, nil)
}

// handleSubnetDelete removes a subnet. Synchronous.
func (s *Server) handleSubnetDelete(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	vnet := r.PathValue("vnet")
	subnet := r.PathValue("subnet")
	s.st.mu.Lock()
	rec := s.lookupSubnetLocked(vnet, subnet)
	if rec != nil {
		delete(s.st.sdn.subnets[vnet], subnet)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchSubnet)
		return
	}
	s.writeData(w, nil)
}

func sdnSubnetToPayload(rec *sdnSubnetRecord) sdnSubnetPayload {
	p := sdnSubnetPayload{Subnet: rec.Subnet, VNet: rec.VNet, Gateway: rec.Gateway}
	if rec.SNAT {
		p.SNAT = 1
	}
	return p
}

func (s *Server) handleFabricList(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.st.mu.Lock()
	out := make([]sdnFabricPayload, 0, len(s.st.sdn.fabrics))
	for _, rec := range s.st.sdn.fabrics {
		out = append(out, sdnFabricToPayload(rec))
	}
	s.st.mu.Unlock()
	s.writeData(w, out)
}

func (s *Server) handleFabricGet(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	fabric := r.PathValue("fabric")
	s.st.mu.Lock()
	rec := s.st.sdn.fabrics[fabric]
	var payload sdnFabricPayload
	if rec != nil {
		payload = sdnFabricToPayload(rec)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchFabric)
		return
	}
	s.writeData(w, payload)
}

// handleFabricCreate defines a fabric. Synchronous (data null, no task).
func (s *Server) handleFabricCreate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidForm)
		return
	}
	fabric := r.PostForm.Get("id")
	if fabric == "" {
		s.writeError(w, http.StatusBadRequest, "missing id")
		return
	}
	rec := &sdnFabricRecord{
		Fabric: fabric, Protocol: r.PostForm.Get("protocol"),
		Nodes: r.PostForm.Get("nodes"), Comment: r.PostForm.Get("comment"),
	}
	s.st.mu.Lock()
	if s.st.sdn.fabrics == nil {
		s.st.sdn.fabrics = make(map[string]*sdnFabricRecord)
	}
	s.st.sdn.fabrics[fabric] = rec
	s.st.mu.Unlock()
	s.writeData(w, nil)
}

// handleFabricUpdate mutates a fabric. Synchronous.
func (s *Server) handleFabricUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	fabric := r.PathValue("fabric")
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidForm)
		return
	}
	s.st.mu.Lock()
	rec := s.st.sdn.fabrics[fabric]
	if rec != nil {
		if v := r.PostForm.Get("protocol"); v != "" {
			rec.Protocol = v
		}
		if v := r.PostForm.Get("nodes"); v != "" {
			rec.Nodes = v
		}
		if v := r.PostForm.Get("comment"); v != "" {
			rec.Comment = v
		}
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchFabric)
		return
	}
	s.writeData(w, nil)
}

// handleFabricDelete removes a fabric. Synchronous.
func (s *Server) handleFabricDelete(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	fabric := r.PathValue("fabric")
	s.st.mu.Lock()
	_, found := s.st.sdn.fabrics[fabric]
	if found {
		delete(s.st.sdn.fabrics, fabric)
	}
	s.st.mu.Unlock()
	if !found {
		s.writeError(w, http.StatusNotFound, msgNoSuchFabric)
		return
	}
	s.writeData(w, nil)
}

func sdnFabricToPayload(rec *sdnFabricRecord) sdnFabricPayload {
	return sdnFabricPayload{
		Fabric: rec.Fabric, Protocol: rec.Protocol, Nodes: rec.Nodes, Comment: rec.Comment,
	}
}
