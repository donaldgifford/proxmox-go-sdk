package mockpve

import (
	"net/http"
	"slices"
	"strconv"
)

// ClusterFingerprint is the synthetic certificate fingerprint the mock's
// join-info advertises for every member. The join handler requires exactly
// this value, so a test exercises the fingerprint round-trip (join-info →
// JoinSpec.Fingerprint) end to end.
const ClusterFingerprint = "A1:B2:C3:D4:E5:F6:07:18:29:3A:4B:5C:6D:7E:8F:90:" +
	"A1:B2:C3:D4:E5:F6:07:18:29:3A:4B:5C:6D:7E:8F:90"

// clusterState is the cluster slice of the mock model, embedded in state and
// guarded by state.mu. The datacenter options live here (except HA's "crs",
// which stays in haState); resources and status entries are seeded for reads.
// The cluster-config fields emulate formation: create marks the mock
// clustered, join grows members, and /cluster/config/nodes reads members.
type clusterState struct {
	options   map[string]string // datacenter options (description, migration, …).
	resources []clusterResourceRecord
	status    []clusterStatusRecord

	clustered bool
	name      string   // corosync cluster name (set by create).
	selfNode  string   // this mock's own node name; "" means "pve".
	members   []string // corosync nodelist, in join order.
	joinQueue []string // names the next joins assume (see QueueClusterJoin).
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

// SetClusterSelfNode names the node this mock plays (default "pve"): the
// first member after CreateCluster and the preferred node in join-info. Call
// before serving.
func (s *Server) SetClusterSelfNode(name string) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	s.st.cluster.selfNode = name
}

// QueueClusterJoin seeds the node name the next successful
// POST /cluster/config/join adds to the membership. On real PVE the joining
// node's identity is implicit — the request is served BY the joining node —
// but one mock serves every role, so tests supply the identity out of band.
// Queue one name per expected join, in order.
func (s *Server) QueueClusterJoin(name string) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	s.st.cluster.joinQueue = append(s.st.cluster.joinQueue, name)
}

func (s *Server) registerClusterRoutes() {
	s.mux.HandleFunc("GET /api2/json/cluster/resources", s.handleClusterResources)
	s.mux.HandleFunc("GET /api2/json/cluster/status", s.handleClusterStatus)
	s.mux.HandleFunc("POST /api2/json/cluster/config", s.handleClusterCreate)
	s.mux.HandleFunc("GET /api2/json/cluster/config/join", s.handleClusterJoinInfo)
	s.mux.HandleFunc("POST /api2/json/cluster/config/join", s.handleClusterJoin)
	s.mux.HandleFunc("GET /api2/json/cluster/config/nodes", s.handleClusterConfigNodes)
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

type clusterJoinNodePayload struct {
	Name           string `json:"name"`
	NodeID         int    `json:"nodeid"`
	QuorumVotes    int    `json:"quorum_votes"`
	PVEFingerprint string `json:"pve_fp"`
}

type clusterJoinInfoPayload struct {
	PreferredNode string                   `json:"preferred_node"`
	ConfigDigest  string                   `json:"config_digest"`
	Totem         map[string]string        `json:"totem"`
	Nodelist      []clusterJoinNodePayload `json:"nodelist"`
}

type clusterConfigNodePayload struct {
	Node        string `json:"node"`
	NodeID      int    `json:"nodeid"`
	QuorumVotes int    `json:"quorum_votes"`
}

// clusterSelfLocked returns the mock's own node name; callers hold st.mu.
func (s *Server) clusterSelfLocked() string {
	if s.st.cluster.selfNode != "" {
		return s.st.cluster.selfNode
	}
	return "pve"
}

// handleClusterCreate emulates POST /cluster/config: it marks the mock
// clustered with the mock's own node as the first member. Creating twice
// errors, like real PVE ("cluster config already exists").
func (s *Server) handleClusterCreate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	if !s.parseForm(w, r) {
		return
	}
	name := r.PostForm.Get("clustername")
	if name == "" {
		s.writeError(w, http.StatusBadRequest, "missing clustername")
		return
	}
	s.st.mu.Lock()
	if s.st.cluster.clustered {
		s.st.mu.Unlock()
		s.writeError(w, http.StatusBadRequest, "cluster config already exists")
		return
	}
	self := s.clusterSelfLocked()
	s.st.cluster.clustered = true
	s.st.cluster.name = name
	s.st.cluster.members = []string{self}
	s.st.mu.Unlock()
	s.writeData(w, s.finishedTask(self, "clustercreate", name))
}

// handleClusterJoinInfo emulates GET /cluster/config/join: the membership
// nodelist with ClusterFingerprint as every member's certificate fingerprint.
// A standalone (never-created) mock errors, like real PVE.
func (s *Server) handleClusterJoinInfo(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.st.mu.Lock()
	if !s.st.cluster.clustered {
		s.st.mu.Unlock()
		s.writeError(w, http.StatusBadRequest, "node is not in a cluster")
		return
	}
	payload := clusterJoinInfoPayload{
		PreferredNode: s.clusterSelfLocked(),
		ConfigDigest:  "mockpve-config-digest",
		Totem:         map[string]string{"cluster_name": s.st.cluster.name},
		Nodelist:      make([]clusterJoinNodePayload, 0, len(s.st.cluster.members)),
	}
	for i, m := range s.st.cluster.members {
		payload.Nodelist = append(payload.Nodelist, clusterJoinNodePayload{
			Name: m, NodeID: i + 1, QuorumVotes: 1, PVEFingerprint: ClusterFingerprint,
		})
	}
	s.st.mu.Unlock()
	s.writeData(w, payload)
}

// handleClusterJoin emulates POST /cluster/config/join from the CLUSTER's
// point of view (the mock plays the cluster being joined): it validates the
// fingerprint issued by the mock's own join-info and grows the membership by
// the next QueueClusterJoin name. Real PVE serves this request on the joining
// node and derives its identity implicitly; the queue is the mock's stand-in
// for that identity.
func (s *Server) handleClusterJoin(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	if !s.parseForm(w, r) {
		return
	}
	for _, field := range []string{"hostname", "password", "fingerprint"} {
		if r.PostForm.Get(field) == "" {
			s.writeError(w, http.StatusBadRequest, "missing "+field)
			return
		}
	}
	if r.PostForm.Get("fingerprint") != ClusterFingerprint {
		s.writeError(w, http.StatusBadRequest, "wrong fingerprint")
		return
	}
	s.st.mu.Lock()
	if !s.st.cluster.clustered {
		s.st.mu.Unlock()
		s.writeError(w, http.StatusBadRequest, "no cluster to join (create one first)")
		return
	}
	if len(s.st.cluster.joinQueue) == 0 {
		s.st.mu.Unlock()
		s.writeError(w, http.StatusBadRequest,
			"mockpve: no queued joining node — seed one with QueueClusterJoin before joining")
		return
	}
	joined := s.st.cluster.joinQueue[0]
	s.st.cluster.joinQueue = s.st.cluster.joinQueue[1:]
	if slices.Contains(s.st.cluster.members, joined) {
		s.st.mu.Unlock()
		s.writeError(w, http.StatusBadRequest, "node "+joined+" is already a cluster member")
		return
	}
	s.st.cluster.members = append(s.st.cluster.members, joined)
	self := s.clusterSelfLocked()
	s.st.mu.Unlock()
	s.writeData(w, s.finishedTask(self, "clusterjoin", joined))
}

// handleClusterConfigNodes emulates GET /cluster/config/nodes: the corosync
// nodelist. A standalone mock returns an empty list (live shape unverified —
// REST-with-caveat).
func (s *Server) handleClusterConfigNodes(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.st.mu.Lock()
	out := make([]clusterConfigNodePayload, 0, len(s.st.cluster.members))
	for i, m := range s.st.cluster.members {
		out = append(out, clusterConfigNodePayload{Node: m, NodeID: i + 1, QuorumVotes: 1})
	}
	s.st.mu.Unlock()
	s.writeData(w, out)
}
