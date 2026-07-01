package mockpve

import (
	"net/http"
	"strconv"
)

// clusterState is the cluster slice of the mock model, embedded in state and
// guarded by state.mu. The datacenter options live here (except HA's "crs",
// which stays in haState); resources and status entries are seeded for reads.
type clusterState struct {
	options   map[string]string // datacenter options (description, migration, …).
	resources []clusterResourceRecord
	status    []clusterStatusRecord
}

// clusterResourceRecord is one /cluster/resources entry.
type clusterResourceRecord struct {
	ID     string
	Type   string
	Node   string
	Status string
	Name   string
	VMID   int
}

// clusterStatusRecord is one /cluster/status entry.
type clusterStatusRecord struct {
	ID      string
	Type    string
	Name    string
	Nodes   int
	Quorate bool
	Online  bool
}

type clusterResourcePayload struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Node   string `json:"node,omitempty"`
	Status string `json:"status,omitempty"`
	Name   string `json:"name,omitempty"`
	VMID   int    `json:"vmid,omitempty"`
}

type clusterStatusPayload struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name,omitempty"`
	Nodes   int    `json:"nodes,omitempty"`
	Quorate int    `json:"quorate,omitempty"`
	Online  int    `json:"online,omitempty"`
}

// SetClusterOptions seeds datacenter options (description, migration, …). Call
// before serving.
func (s *Server) SetClusterOptions(description, migration string) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	if s.st.cluster.options == nil {
		s.st.cluster.options = make(map[string]string)
	}
	if description != "" {
		s.st.cluster.options["description"] = description
	}
	if migration != "" {
		s.st.cluster.options["migration"] = migration
	}
}

// AddClusterResource seeds one /cluster/resources entry. Call before serving.
func (s *Server) AddClusterResource(resourceType, node, status string, vmid int) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	rec := clusterResourceRecord{Type: resourceType, Node: node, Status: status, VMID: vmid}
	switch resourceType {
	case "qemu", "lxc":
		rec.ID = resourceType + "/" + strconv.Itoa(vmid)
	case "node":
		rec.ID = "node/" + node
	default:
		rec.ID = resourceType
	}
	s.st.cluster.resources = append(s.st.cluster.resources, rec)
}

// AddClusterStatusNode seeds a "node" /cluster/status entry. Call before serving.
func (s *Server) AddClusterStatusNode(name string, online bool) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	s.st.cluster.status = append(s.st.cluster.status,
		clusterStatusRecord{ID: "node/" + name, Type: "node", Name: name, Online: online})
}

// SetClusterStatusInfo seeds the "cluster" /cluster/status entry. Call before
// serving.
func (s *Server) SetClusterStatusInfo(name string, nodes int, quorate bool) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	s.st.cluster.status = append(s.st.cluster.status,
		clusterStatusRecord{ID: "cluster", Type: "cluster", Name: name, Nodes: nodes, Quorate: quorate})
}

func (s *Server) registerClusterRoutes() {
	s.mux.HandleFunc("GET /api2/json/cluster/resources", s.handleClusterResources)
	s.mux.HandleFunc("GET /api2/json/cluster/status", s.handleClusterStatus)
	// /cluster/options GET+PUT are registered by registerHARoutes (shared route).
}

func (s *Server) handleClusterResources(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	filter := r.URL.Query().Get("type")
	s.st.mu.Lock()
	out := make([]clusterResourcePayload, 0, len(s.st.cluster.resources))
	for _, rec := range s.st.cluster.resources {
		if filter != "" && !resourceMatchesType(rec.Type, filter) {
			continue
		}
		out = append(out, clusterResourcePayload(rec))
	}
	s.st.mu.Unlock()
	s.writeData(w, out)
}

// resourceMatchesType maps a stored resource type to the /cluster/resources
// filter value. The "vm" filter matches both qemu and lxc guests.
func resourceMatchesType(recType, filter string) bool {
	if filter == "vm" {
		return recType == "qemu" || recType == "lxc"
	}
	return recType == filter
}

func (s *Server) handleClusterStatus(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.st.mu.Lock()
	out := make([]clusterStatusPayload, 0, len(s.st.cluster.status))
	for _, rec := range s.st.cluster.status {
		out = append(out, clusterStatusPayload{
			ID: rec.ID, Type: rec.Type, Name: rec.Name, Nodes: rec.Nodes,
			Quorate: boolToInt(rec.Quorate), Online: boolToInt(rec.Online),
		})
	}
	s.st.mu.Unlock()
	s.writeData(w, out)
}
