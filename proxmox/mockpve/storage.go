package mockpve

import (
	"net/http"
	"strconv"
)

// storageState is the storage slice of the mock model, embedded in state and
// guarded by state.mu. Datastore config is cluster-scoped (stores); content is
// keyed by node then storage.
type storageState struct {
	stores  map[string]*storeRecord            // storage id -> config.
	content map[string]map[string][]*volRecord // node -> storage -> volumes.
}

// storeRecord is one datastore's cluster-scoped configuration plus mock usage.
type storeRecord struct {
	Storage string
	Type    string
	Content string
	Path    string
	Pool    string
	Shared  bool
	Total   int64
	Used    int64
}

// volRecord is one stored object on a node's storage.
type volRecord struct {
	Volid   string
	Content string
	Format  string
	Size    int64
	VMID    int
}

// datastorePayload mirrors GET /storage entries.
type datastorePayload struct {
	Storage string `json:"storage"`
	Type    string `json:"type"`
	Content string `json:"content,omitempty"`
	Path    string `json:"path,omitempty"`
	Pool    string `json:"pool,omitempty"`
	Shared  int    `json:"shared,omitempty"`
}

// storageStatusPayload mirrors GET /nodes/{node}/storage entries.
type storageStatusPayload struct {
	Storage string `json:"storage"`
	Type    string `json:"type"`
	Content string `json:"content,omitempty"`
	Active  int    `json:"active,omitempty"`
	Enabled int    `json:"enabled,omitempty"`
	Shared  int    `json:"shared,omitempty"`
	Total   int64  `json:"total,omitempty"`
	Used    int64  `json:"used,omitempty"`
	Avail   int64  `json:"avail,omitempty"`
}

// contentPayload mirrors GET /nodes/{node}/storage/{storage}/content entries.
type contentPayload struct {
	Volid   string `json:"volid"`
	Content string `json:"content,omitempty"`
	Format  string `json:"format,omitempty"`
	Size    int64  `json:"size,omitempty"`
	VMID    int    `json:"vmid,omitempty"`
}

// AddStorage registers a datastore config (cluster-scoped). total/used seed the
// usage metrics node-status reads report. Call before serving.
func (s *Server) AddStorage(id, storageType, content string, total, used int64) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	if s.st.storage.stores == nil {
		s.st.storage.stores = make(map[string]*storeRecord)
	}
	s.st.storage.stores[id] = &storeRecord{
		Storage: id, Type: storageType, Content: content, Total: total, Used: used,
	}
}

// AddVolume seeds a stored object on node/storage. Call before serving; the
// storage need not be registered with AddStorage first.
func (s *Server) AddVolume(node, storage, volid, content, format string, size int64) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	if s.st.storage.content == nil {
		s.st.storage.content = make(map[string]map[string][]*volRecord)
	}
	if s.st.storage.content[node] == nil {
		s.st.storage.content[node] = make(map[string][]*volRecord)
	}
	s.st.storage.content[node][storage] = append(s.st.storage.content[node][storage],
		&volRecord{Volid: volid, Content: content, Format: format, Size: size})
}

func (s *Server) registerStorageRoutes() {
	s.mux.HandleFunc("GET /api2/json/storage", s.handleDatastoreList)
	s.mux.HandleFunc("GET /api2/json/storage/{storage}", s.handleDatastoreGet)
	s.mux.HandleFunc("GET /api2/json/nodes/{node}/storage", s.handleNodeStorageList)
	s.mux.HandleFunc("GET /api2/json/nodes/{node}/storage/{storage}/status", s.handleNodeStorageStatus)
	s.mux.HandleFunc("GET /api2/json/nodes/{node}/storage/{storage}/content", s.handleContentList)
	s.mux.HandleFunc("GET /api2/json/nodes/{node}/storage/{storage}/content/{volid}", s.handleVolumeGet)
}

func (s *Server) handleDatastoreList(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.st.mu.Lock()
	out := make([]datastorePayload, 0, len(s.st.storage.stores))
	for _, rec := range s.st.storage.stores {
		out = append(out, datastoreToPayload(rec))
	}
	s.st.mu.Unlock()
	s.writeData(w, out)
}

func (s *Server) handleDatastoreGet(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	id := r.PathValue("storage")
	s.st.mu.Lock()
	rec := s.st.storage.stores[id]
	var payload datastorePayload
	if rec != nil {
		payload = datastoreToPayload(rec)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchStorage)
		return
	}
	s.writeData(w, payload)
}

func (s *Server) handleNodeStorageList(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.st.mu.Lock()
	out := make([]storageStatusPayload, 0, len(s.st.storage.stores))
	for _, rec := range s.st.storage.stores {
		out = append(out, storageToStatus(rec))
	}
	s.st.mu.Unlock()
	s.writeData(w, out)
}

func (s *Server) handleNodeStorageStatus(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	id := r.PathValue("storage")
	s.st.mu.Lock()
	rec := s.st.storage.stores[id]
	var payload storageStatusPayload
	if rec != nil {
		payload = storageToStatus(rec)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchStorage)
		return
	}
	s.writeData(w, payload)
}

func (s *Server) handleContentList(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	storage := r.PathValue("storage")
	filterContent := r.URL.Query().Get("content")
	filterVMID := r.URL.Query().Get("vmid")
	s.st.mu.Lock()
	vols := s.st.storage.content[node][storage]
	out := make([]contentPayload, 0, len(vols))
	for _, v := range vols {
		if filterContent != "" && v.Content != filterContent {
			continue
		}
		if filterVMID != "" && filterVMID != strconv.Itoa(v.VMID) {
			continue
		}
		out = append(out, contentPayload{
			Volid: v.Volid, Content: v.Content, Format: v.Format, Size: v.Size, VMID: v.VMID,
		})
	}
	s.st.mu.Unlock()
	s.writeData(w, out)
}

func (s *Server) handleVolumeGet(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	storage := r.PathValue("storage")
	volid := r.PathValue("volid")
	s.st.mu.Lock()
	var found *volRecord
	for _, v := range s.st.storage.content[node][storage] {
		if v.Volid == volid {
			found = v
			break
		}
	}
	var payload contentPayload
	if found != nil {
		payload = contentPayload{
			Volid: found.Volid, Content: found.Content, Format: found.Format, Size: found.Size, VMID: found.VMID,
		}
	}
	s.st.mu.Unlock()
	if found == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchVolume)
		return
	}
	s.writeData(w, payload)
}

func datastoreToPayload(rec *storeRecord) datastorePayload {
	return datastorePayload{
		Storage: rec.Storage, Type: rec.Type, Content: rec.Content,
		Path: rec.Path, Pool: rec.Pool, Shared: boolToInt(rec.Shared),
	}
}

func storageToStatus(rec *storeRecord) storageStatusPayload {
	return storageStatusPayload{
		Storage: rec.Storage, Type: rec.Type, Content: rec.Content,
		Active: 1, Enabled: 1, Shared: boolToInt(rec.Shared),
		Total: rec.Total, Used: rec.Used, Avail: rec.Total - rec.Used,
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
