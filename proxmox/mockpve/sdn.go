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
	// fabricNodes is keyed by fabric id, then by node id.
	fabricNodes map[string]map[string]*sdnFabricNodeRecord
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

// sdnFabricRecord is one SDN fabric. A fabric carries no node list — node
// membership is the fabricNodes sub-collection, as on real PVE.
type sdnFabricRecord struct {
	Fabric      string
	Protocol    string
	IPPrefix    string
	IP6Prefix   string
	RouteFilter string
}

// sdnFabricNodeRecord is one node's membership in a fabric.
type sdnFabricNodeRecord struct {
	NodeID     string
	Fabric     string
	Protocol   string
	IP         string
	IP6        string
	Interfaces []string
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

// sdnFabricPayload mirrors GET /cluster/sdn/fabrics/fabric entries.
type sdnFabricPayload struct {
	Fabric      string `json:"id"`
	Protocol    string `json:"protocol,omitempty"`
	IPPrefix    string `json:"ip_prefix,omitempty"`
	IP6Prefix   string `json:"ip6_prefix,omitempty"`
	RouteFilter string `json:"route_filter,omitempty"`
}

// sdnFabricNodePayload mirrors GET /cluster/sdn/fabrics/node/{fabric} entries.
type sdnFabricNodePayload struct {
	NodeID   string `json:"node_id"`
	Fabric   string `json:"fabric_id,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	IP       string `json:"ip,omitempty"`
	IP6      string `json:"ip6,omitempty"`
}

// Node-scoped SDN live-status payloads (GET /nodes/{node}/sdn/…). The mock
// synthesizes status from seeded config state: seeded objects report as
// available/up, and the route/VRF tables carry one static synthetic row so
// decoders see every field shape.

// sdnZoneStatusPayload mirrors GET /nodes/{node}/sdn/zones entries.
type sdnZoneStatusPayload struct {
	Zone   string `json:"zone"`
	Status string `json:"status"`
}

// sdnVNetStatusPayload mirrors GET /nodes/{node}/sdn/zones/{zone}/content.
type sdnVNetStatusPayload struct {
	VNet      string `json:"vnet"`
	Status    string `json:"status,omitempty"`
	StatusMsg string `json:"statusmsg,omitempty"`
}

// sdnZoneBridgePayload mirrors GET /nodes/{node}/sdn/zones/{zone}/bridges.
type sdnZoneBridgePayload struct {
	Name          string                 `json:"name"`
	VLANFiltering string                 `json:"vlan_filtering,omitempty"`
	Ports         []sdnBridgePortPayload `json:"ports"`
}

// sdnBridgePortPayload is one member port of a zone bridge.
type sdnBridgePortPayload struct {
	Name string `json:"name"`
}

// sdnIPVRFPayload mirrors GET /nodes/{node}/sdn/zones/{zone}/ip-vrf.
type sdnIPVRFPayload struct {
	IP       string   `json:"ip"`
	Metric   int      `json:"metric"`
	Protocol string   `json:"protocol"`
	Nexthops []string `json:"nexthops"`
}

// sdnMACVRFPayload mirrors GET /nodes/{node}/sdn/vnets/{vnet}/mac-vrf.
type sdnMACVRFPayload struct {
	IP      string `json:"ip"`
	MAC     string `json:"mac"`
	Nexthop string `json:"nexthop"`
}

// sdnFabricIfacePayload mirrors GET …/sdn/fabrics/{fabric}/interfaces.
type sdnFabricIfacePayload struct {
	Name  string `json:"name"`
	State string `json:"state"`
	Type  string `json:"type"`
}

// sdnFabricNeighborPayload mirrors GET …/sdn/fabrics/{fabric}/neighbors.
type sdnFabricNeighborPayload struct {
	Neighbor string `json:"neighbor"`
	Status   string `json:"status"`
	Uptime   string `json:"uptime"`
}

// sdnFabricRoutePayload mirrors GET …/sdn/fabrics/{fabric}/routes.
type sdnFabricRoutePayload struct {
	Route string   `json:"route"`
	Via   []string `json:"via"`
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

// AddFabricNode seeds a node's membership in a fabric (the fabric itself must
// be seeded separately with AddFabric). Call before serving.
func (s *Server) AddFabricNode(fabric, node string) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	s.ensureFabricNodesLocked(fabric)
	protocol := ""
	if f := s.st.sdn.fabrics[fabric]; f != nil {
		protocol = f.Protocol
	}
	s.st.sdn.fabricNodes[fabric][node] = &sdnFabricNodeRecord{
		NodeID: node, Fabric: fabric, Protocol: protocol,
	}
}

// ensureFabricNodesLocked makes the fabric-node maps ready for fabric. Caller
// holds mu.
func (s *Server) ensureFabricNodesLocked(fabric string) {
	if s.st.sdn.fabricNodes == nil {
		s.st.sdn.fabricNodes = make(map[string]map[string]*sdnFabricNodeRecord)
	}
	if s.st.sdn.fabricNodes[fabric] == nil {
		s.st.sdn.fabricNodes[fabric] = make(map[string]*sdnFabricNodeRecord)
	}
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
	s.mux.HandleFunc("GET /api2/json/cluster/sdn/fabrics/fabric", s.handleFabricList)
	s.mux.HandleFunc("POST /api2/json/cluster/sdn/fabrics/fabric", s.handleFabricCreate)
	s.mux.HandleFunc("GET /api2/json/cluster/sdn/fabrics/fabric/{fabric}", s.handleFabricGet)
	s.mux.HandleFunc("PUT /api2/json/cluster/sdn/fabrics/fabric/{fabric}", s.handleFabricUpdate)
	s.mux.HandleFunc("DELETE /api2/json/cluster/sdn/fabrics/fabric/{fabric}", s.handleFabricDelete)
	s.mux.HandleFunc("GET /api2/json/cluster/sdn/fabrics/node/{fabric}", s.handleFabricNodeList)
	s.mux.HandleFunc("POST /api2/json/cluster/sdn/fabrics/node/{fabric}", s.handleFabricNodeCreate)
	s.mux.HandleFunc("GET /api2/json/cluster/sdn/fabrics/node/{fabric}/{node}", s.handleFabricNodeGet)
	s.mux.HandleFunc("PUT /api2/json/cluster/sdn/fabrics/node/{fabric}/{node}", s.handleFabricNodeUpdate)
	s.mux.HandleFunc("DELETE /api2/json/cluster/sdn/fabrics/node/{fabric}/{node}", s.handleFabricNodeDelete)
	s.mux.HandleFunc("PUT /api2/json/cluster/sdn", s.handleSDNApply)
	s.mux.HandleFunc("GET /api2/json/nodes/{node}/sdn/zones", s.handleNodeSDNZones)
	s.mux.HandleFunc("GET /api2/json/nodes/{node}/sdn/zones/{zone}/content", s.handleNodeSDNZoneContent)
	s.mux.HandleFunc("GET /api2/json/nodes/{node}/sdn/zones/{zone}/bridges", s.handleNodeSDNZoneBridges)
	s.mux.HandleFunc("GET /api2/json/nodes/{node}/sdn/zones/{zone}/ip-vrf", s.handleNodeSDNZoneIPVRF)
	s.mux.HandleFunc("GET /api2/json/nodes/{node}/sdn/vnets/{vnet}/mac-vrf", s.handleNodeSDNVNetMACVRF)
	s.mux.HandleFunc("GET /api2/json/nodes/{node}/sdn/fabrics/{fabric}/interfaces", s.handleNodeSDNFabricInterfaces)
	s.mux.HandleFunc("GET /api2/json/nodes/{node}/sdn/fabrics/{fabric}/neighbors", s.handleNodeSDNFabricNeighbors)
	s.mux.HandleFunc("GET /api2/json/nodes/{node}/sdn/fabrics/{fabric}/routes", s.handleNodeSDNFabricRoutes)
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
		IPPrefix:    r.PostForm.Get("ip_prefix"),
		IP6Prefix:   r.PostForm.Get("ip6_prefix"),
		RouteFilter: r.PostForm.Get("route_filter"),
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
		applyFabricForm(rec, r.PostForm)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchFabric)
		return
	}
	s.writeData(w, nil)
}

// applyFabricForm applies a PUT form's fields to rec. The caller holds st.mu.
func applyFabricForm(rec *sdnFabricRecord, form url.Values) {
	if v := form.Get("protocol"); v != "" {
		rec.Protocol = v
	}
	if v := form.Get("ip_prefix"); v != "" {
		rec.IPPrefix = v
	}
	if v := form.Get("ip6_prefix"); v != "" {
		rec.IP6Prefix = v
	}
	if v := form.Get("route_filter"); v != "" {
		rec.RouteFilter = v
	}
}

// handleFabricDelete removes a fabric and its node membership. Synchronous.
func (s *Server) handleFabricDelete(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	fabric := r.PathValue("fabric")
	s.st.mu.Lock()
	_, found := s.st.sdn.fabrics[fabric]
	if found {
		delete(s.st.sdn.fabrics, fabric)
		delete(s.st.sdn.fabricNodes, fabric)
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
		Fabric: rec.Fabric, Protocol: rec.Protocol, IPPrefix: rec.IPPrefix,
		IP6Prefix: rec.IP6Prefix, RouteFilter: rec.RouteFilter,
	}
}

func (s *Server) handleFabricNodeList(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	fabric := r.PathValue("fabric")
	s.st.mu.Lock()
	_, found := s.st.sdn.fabrics[fabric]
	nodes := s.st.sdn.fabricNodes[fabric]
	out := make([]sdnFabricNodePayload, 0, len(nodes))
	for _, rec := range nodes {
		out = append(out, sdnFabricNodeToPayload(rec))
	}
	s.st.mu.Unlock()
	if !found {
		s.writeError(w, http.StatusNotFound, msgNoSuchFabric)
		return
	}
	s.writeData(w, out)
}

func (s *Server) handleFabricNodeGet(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	fabric := r.PathValue("fabric")
	node := r.PathValue("node")
	s.st.mu.Lock()
	rec := s.lookupFabricNodeLocked(fabric, node)
	var payload sdnFabricNodePayload
	if rec != nil {
		payload = sdnFabricNodeToPayload(rec)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchFabricNode)
		return
	}
	s.writeData(w, payload)
}

// lookupFabricNodeLocked returns the fabric-node record or nil. The caller
// holds st.mu.
func (s *Server) lookupFabricNodeLocked(fabric, node string) *sdnFabricNodeRecord {
	if s.st.sdn.fabricNodes[fabric] == nil {
		return nil
	}
	return s.st.sdn.fabricNodes[fabric][node]
}

// handleFabricNodeCreate adds a node to a fabric. Synchronous (data null).
func (s *Server) handleFabricNodeCreate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	fabric := r.PathValue("fabric")
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidForm)
		return
	}
	node := r.PostForm.Get("node_id")
	if node == "" {
		s.writeError(w, http.StatusBadRequest, "missing node_id")
		return
	}
	rec := &sdnFabricNodeRecord{
		NodeID: node, Fabric: fabric, Protocol: r.PostForm.Get("protocol"),
		IP: r.PostForm.Get("ip"), IP6: r.PostForm.Get("ip6"),
		Interfaces: r.PostForm["interfaces"],
	}
	s.st.mu.Lock()
	_, found := s.st.sdn.fabrics[fabric]
	if found {
		s.ensureFabricNodesLocked(fabric)
		s.st.sdn.fabricNodes[fabric][node] = rec
	}
	s.st.mu.Unlock()
	if !found {
		s.writeError(w, http.StatusNotFound, msgNoSuchFabric)
		return
	}
	s.writeData(w, nil)
}

// handleFabricNodeUpdate mutates a node's fabric membership. Synchronous.
func (s *Server) handleFabricNodeUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	fabric := r.PathValue("fabric")
	node := r.PathValue("node")
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidForm)
		return
	}
	s.st.mu.Lock()
	rec := s.lookupFabricNodeLocked(fabric, node)
	if rec != nil {
		applyFabricNodeForm(rec, r.PostForm)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchFabricNode)
		return
	}
	s.writeData(w, nil)
}

// applyFabricNodeForm applies a PUT form's fields to rec. The caller holds
// st.mu.
func applyFabricNodeForm(rec *sdnFabricNodeRecord, form url.Values) {
	if v := form.Get("ip"); v != "" {
		rec.IP = v
	}
	if v := form.Get("ip6"); v != "" {
		rec.IP6 = v
	}
	if v := form["interfaces"]; len(v) > 0 {
		rec.Interfaces = v
	}
}

// handleFabricNodeDelete removes a node from a fabric. Synchronous.
func (s *Server) handleFabricNodeDelete(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	fabric := r.PathValue("fabric")
	node := r.PathValue("node")
	s.st.mu.Lock()
	rec := s.lookupFabricNodeLocked(fabric, node)
	if rec != nil {
		delete(s.st.sdn.fabricNodes[fabric], node)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchFabricNode)
		return
	}
	s.writeData(w, nil)
}

func sdnFabricNodeToPayload(rec *sdnFabricNodeRecord) sdnFabricNodePayload {
	return sdnFabricNodePayload{
		NodeID: rec.NodeID, Fabric: rec.Fabric, Protocol: rec.Protocol,
		IP: rec.IP, IP6: rec.IP6,
	}
}

// handleNodeSDNZones reports every seeded zone as available on the node.
func (s *Server) handleNodeSDNZones(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.st.mu.Lock()
	out := make([]sdnZoneStatusPayload, 0, len(s.st.sdn.zones))
	for _, rec := range s.st.sdn.zones {
		out = append(out, sdnZoneStatusPayload{Zone: rec.Zone, Status: "available"})
	}
	s.st.mu.Unlock()
	s.writeData(w, out)
}

// handleNodeSDNZoneContent reports the zone's VNets as available.
func (s *Server) handleNodeSDNZoneContent(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	zone := r.PathValue("zone")
	s.st.mu.Lock()
	_, found := s.st.sdn.zones[zone]
	var out []sdnVNetStatusPayload
	for _, rec := range s.st.sdn.vnets {
		if rec.Zone == zone {
			out = append(out, sdnVNetStatusPayload{VNet: rec.VNet, Status: "available"})
		}
	}
	s.st.mu.Unlock()
	if !found {
		s.writeError(w, http.StatusNotFound, msgNoSuchZone)
		return
	}
	s.writeData(w, out)
}

// handleNodeSDNZoneBridges reports one bridge per VNet in the zone, with a
// static member port so decoders see the nested `ports` array.
func (s *Server) handleNodeSDNZoneBridges(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	zone := r.PathValue("zone")
	s.st.mu.Lock()
	_, found := s.st.sdn.zones[zone]
	var out []sdnZoneBridgePayload
	for _, rec := range s.st.sdn.vnets {
		if rec.Zone == zone {
			out = append(out, sdnZoneBridgePayload{
				Name:          rec.VNet,
				VLANFiltering: "0",
				Ports:         []sdnBridgePortPayload{{Name: "tap100i0"}},
			})
		}
	}
	s.st.mu.Unlock()
	if !found {
		s.writeError(w, http.StatusNotFound, msgNoSuchZone)
		return
	}
	s.writeData(w, out)
}

// handleNodeSDNZoneIPVRF returns one static synthetic VRF row for a seeded
// zone, so decoders exercise every field shape (including the nexthops array).
func (s *Server) handleNodeSDNZoneIPVRF(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	zone := r.PathValue("zone")
	s.st.mu.Lock()
	_, found := s.st.sdn.zones[zone]
	s.st.mu.Unlock()
	if !found {
		s.writeError(w, http.StatusNotFound, msgNoSuchZone)
		return
	}
	s.writeData(w, []sdnIPVRFPayload{{
		IP: "10.0.0.0/24", Metric: 20, Protocol: "bgp",
		Nexthops: []string{"192.0.2.1"},
	}})
}

// handleNodeSDNVNetMACVRF returns one static synthetic MAC-VRF row for a
// seeded VNet.
func (s *Server) handleNodeSDNVNetMACVRF(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	vnet := r.PathValue("vnet")
	s.st.mu.Lock()
	_, found := s.st.sdn.vnets[vnet]
	s.st.mu.Unlock()
	if !found {
		s.writeError(w, http.StatusNotFound, msgNoSuchVNet)
		return
	}
	s.writeData(w, []sdnMACVRFPayload{{
		IP: "10.0.0.10", MAC: "BC:24:11:00:00:01", Nexthop: "192.0.2.1",
	}})
}

// handleNodeSDNFabricInterfaces reports the requesting node's seeded fabric
// interfaces as up.
func (s *Server) handleNodeSDNFabricInterfaces(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	fabric := r.PathValue("fabric")
	s.st.mu.Lock()
	_, found := s.st.sdn.fabrics[fabric]
	out := make([]sdnFabricIfacePayload, 0)
	if rec := s.lookupFabricNodeLocked(fabric, node); rec != nil {
		for _, iface := range rec.Interfaces {
			out = append(out, sdnFabricIfacePayload{
				Name: iface, State: "up", Type: "Point-to-Point",
			})
		}
	}
	s.st.mu.Unlock()
	if !found {
		s.writeError(w, http.StatusNotFound, msgNoSuchFabric)
		return
	}
	s.writeData(w, out)
}

// handleNodeSDNFabricNeighbors reports every OTHER fabric member node as an
// established neighbor of the requesting node.
func (s *Server) handleNodeSDNFabricNeighbors(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	fabric := r.PathValue("fabric")
	s.st.mu.Lock()
	_, found := s.st.sdn.fabrics[fabric]
	out := make([]sdnFabricNeighborPayload, 0)
	for _, rec := range s.st.sdn.fabricNodes[fabric] {
		if rec.NodeID != node {
			out = append(out, sdnFabricNeighborPayload{
				Neighbor: rec.NodeID, Status: "Up", Uptime: "8h24m12s",
			})
		}
	}
	s.st.mu.Unlock()
	if !found {
		s.writeError(w, http.StatusNotFound, msgNoSuchFabric)
		return
	}
	s.writeData(w, out)
}

// handleNodeSDNFabricRoutes returns one static synthetic route row for a
// seeded fabric (the fabric's ip_prefix when set), so decoders exercise the
// `via` array.
func (s *Server) handleNodeSDNFabricRoutes(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	fabric := r.PathValue("fabric")
	s.st.mu.Lock()
	rec := s.st.sdn.fabrics[fabric]
	route := "10.0.0.0/24"
	if rec != nil && rec.IPPrefix != "" {
		route = rec.IPPrefix
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchFabric)
		return
	}
	s.writeData(w, []sdnFabricRoutePayload{{Route: route, Via: []string{"192.0.2.1"}}})
}
