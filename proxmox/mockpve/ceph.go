package mockpve

import (
	"net/http"
	"strconv"
)

// This file models the Ceph surface (task 6): pools, the OSD/CRUSH tree, and
// cluster status/config. Ceph is a single cluster-wide entity, so the state is
// flat (not per-node) — every MON node answers with the same data. RBD
// mirroring has no PVE REST endpoint (the SDK returns ErrUnsupported), so no
// mirror routes are registered.

// cephState is the Ceph slice of the mock model, embedded in state and guarded
// by state.mu. It is flat: pools and OSDs belong to the cluster, not a node.
type cephState struct {
	pools map[string]*cephPoolRecord // keyed by pool name.
	osds  []cephOSDRecord
}

type cephPoolRecord struct {
	Name    string
	Size    int
	MinSize int
	PGNum   int
	Type    string
}

type cephOSDRecord struct {
	ID   int
	Host string
}

type cephPoolPayload struct {
	Name    string `json:"pool_name"`
	Size    int    `json:"size,omitempty"`
	MinSize int    `json:"min_size,omitempty"`
	PGNum   int    `json:"pg_num,omitempty"`
	Type    string `json:"type,omitempty"`
}

type cephOSDNodePayload struct {
	ID       int                  `json:"id"`
	Name     string               `json:"name"`
	Type     string               `json:"type"`
	Status   string               `json:"status,omitempty"`
	Children []cephOSDNodePayload `json:"children,omitempty"`
}

type cephOSDTreePayload struct {
	Flags string             `json:"flags"`
	Root  cephOSDNodePayload `json:"root"`
}

type cephHealthPayload struct {
	Status string `json:"status"`
}

type cephStatusPayload struct {
	FSID   string            `json:"fsid"`
	Health cephHealthPayload `json:"health"`
}

// AddCephPool seeds a Ceph pool. Call before serving.
func (s *Server) AddCephPool(name string) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	if s.st.ceph.pools == nil {
		s.st.ceph.pools = make(map[string]*cephPoolRecord)
	}
	s.st.ceph.pools[name] = &cephPoolRecord{Name: name, Size: 3, MinSize: 2, PGNum: 128, Type: "replicated"}
}

// AddCephOSD seeds an OSD on host. Call before serving.
func (s *Server) AddCephOSD(id int, host string) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	s.st.ceph.osds = append(s.st.ceph.osds, cephOSDRecord{ID: id, Host: host})
}

func (s *Server) registerCephRoutes() {
	s.mux.HandleFunc("GET /api2/json/nodes/{node}/ceph/pools", s.handleCephPoolList)
	s.mux.HandleFunc("POST /api2/json/nodes/{node}/ceph/pools", s.handleCephPoolCreate)
	s.mux.HandleFunc("GET /api2/json/nodes/{node}/ceph/pools/{name}", s.handleCephPoolGet)
	s.mux.HandleFunc("DELETE /api2/json/nodes/{node}/ceph/pools/{name}", s.handleCephPoolDelete)
	s.mux.HandleFunc("GET /api2/json/nodes/{node}/ceph/osd", s.handleCephOSDList)
	s.mux.HandleFunc("POST /api2/json/nodes/{node}/ceph/osd", s.handleCephOSDCreate)
	s.mux.HandleFunc("DELETE /api2/json/nodes/{node}/ceph/osd/{osdid}", s.handleCephOSDDestroy)
	s.mux.HandleFunc("GET /api2/json/nodes/{node}/ceph/status", s.handleCephStatus)
	s.mux.HandleFunc("GET /api2/json/nodes/{node}/ceph/config", s.handleCephConfig)
}

func (s *Server) handleCephPoolList(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.st.mu.Lock()
	out := make([]cephPoolPayload, 0, len(s.st.ceph.pools))
	for _, rec := range s.st.ceph.pools {
		out = append(out, cephPoolToPayload(rec))
	}
	s.st.mu.Unlock()
	s.writeData(w, out)
}

func (s *Server) handleCephPoolGet(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	name := r.PathValue("name")
	s.st.mu.Lock()
	rec := s.st.ceph.pools[name]
	var payload cephPoolPayload
	if rec != nil {
		payload = cephPoolToPayload(rec)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchCephPool)
		return
	}
	s.writeData(w, payload)
}

// handleCephPoolCreate creates a pool and returns a worker task.
func (s *Server) handleCephPoolCreate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	if !s.parseForm(w, r) {
		return
	}
	node := r.PathValue("node")
	name := r.PostForm.Get("name")
	if name == "" {
		s.writeError(w, http.StatusBadRequest, "missing name")
		return
	}
	s.st.mu.Lock()
	if s.st.ceph.pools == nil {
		s.st.ceph.pools = make(map[string]*cephPoolRecord)
	}
	s.st.ceph.pools[name] = &cephPoolRecord{Name: name, Size: 3, MinSize: 2, PGNum: 128, Type: "replicated"}
	s.st.mu.Unlock()
	s.writeData(w, s.finishedTask(node, "cephcreatepool", name))
}

// handleCephPoolDelete destroys a pool and returns a worker task.
func (s *Server) handleCephPoolDelete(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	name := r.PathValue("name")
	s.st.mu.Lock()
	rec := s.st.ceph.pools[name]
	if rec != nil {
		delete(s.st.ceph.pools, name)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchCephPool)
		return
	}
	s.writeData(w, s.finishedTask(node, "cephdestroypool", name))
}

func (s *Server) handleCephOSDList(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.st.mu.Lock()
	tree := s.buildOSDTreeLocked()
	s.st.mu.Unlock()
	s.writeData(w, tree)
}

// buildOSDTreeLocked assembles the CRUSH tree from the flat OSD records, one
// host node per distinct host. Caller holds st.mu.
func (s *Server) buildOSDTreeLocked() cephOSDTreePayload {
	hosts := make(map[string]*cephOSDNodePayload)
	var order []string
	for _, osd := range s.st.ceph.osds {
		h, ok := hosts[osd.Host]
		if !ok {
			h = &cephOSDNodePayload{ID: -1, Name: osd.Host, Type: "host"}
			hosts[osd.Host] = h
			order = append(order, osd.Host)
		}
		h.Children = append(h.Children, cephOSDNodePayload{
			ID: osd.ID, Name: "osd." + strconv.Itoa(osd.ID), Type: "osd", Status: "up",
		})
	}
	root := cephOSDNodePayload{ID: -1, Name: "default", Type: "root"}
	for _, host := range order {
		root.Children = append(root.Children, *hosts[host])
	}
	return cephOSDTreePayload{Flags: "sortbitwise", Root: root}
}

// handleCephOSDCreate creates an OSD and returns a worker task.
func (s *Server) handleCephOSDCreate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	if !s.parseForm(w, r) {
		return
	}
	node := r.PathValue("node")
	if r.PostForm.Get("dev") == "" {
		s.writeError(w, http.StatusBadRequest, "missing dev")
		return
	}
	s.writeData(w, s.finishedTask(node, "cephcreateosd", "osd"))
}

// handleCephOSDDestroy removes an OSD and returns a worker task.
func (s *Server) handleCephOSDDestroy(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	osdid := r.PathValue("osdid")
	s.writeData(w, s.finishedTask(node, "cephdestroyosd", osdid))
}

func (s *Server) handleCephStatus(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.writeData(w, cephStatusPayload{FSID: "mock-fsid", Health: cephHealthPayload{Status: "HEALTH_OK"}})
}

func (s *Server) handleCephConfig(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.writeData(w, "[global]\n\tfsid = mock-fsid\n")
}

func cephPoolToPayload(rec *cephPoolRecord) cephPoolPayload {
	return cephPoolPayload{
		Name: rec.Name, Size: rec.Size, MinSize: rec.MinSize, PGNum: rec.PGNum, Type: rec.Type,
	}
}
