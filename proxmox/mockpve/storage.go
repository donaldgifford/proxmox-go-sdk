package mockpve

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// maxUploadBytes bounds the in-memory multipart parse for the mock upload
// handler. The mock stores no bytes (it drains the file to measure size), so a
// modest cap is fine for tests.
const maxUploadBytes = 32 << 20 // 32 MiB

// storageState is the storage slice of the mock model, embedded in state and
// guarded by state.mu. Datastore config is cluster-scoped (stores); content is
// keyed by node then storage.
type storageState struct {
	stores   map[string]*storeRecord            // storage id -> config.
	content  map[string]map[string][]*volRecord // node -> storage -> volumes.
	zfsPools map[string]map[string]*zfsRecord   // node -> pool name -> pool.
	// cfgVersion/cfgDigest model the storage.cfg FILE digest: one value shared
	// by every entry of a read (what the live cassette shows), changed by every
	// write — so read → update(digest) → update(same digest) fails the second
	// time and the guard is testable without a race.
	cfgVersion int
	cfgDigest  string
}

// bumpStorageDigest advances the config digest. Callers hold state.mu.
func (st *storageState) bumpStorageDigest() {
	st.cfgVersion++
	st.cfgDigest = fmt.Sprintf("%040x", st.cfgVersion)
}

// zfsRecord is one ZFS pool local to a node.
type zfsRecord struct {
	Name   string
	Size   int64
	Free   int64
	Health string
	State  string
}

// storeRecord is one datastore's cluster-scoped configuration plus mock usage.
// List-valued options (Content, Nodes) are stored NORMALIZED — parsed to a
// set and re-joined sorted — because that is real PVE's behaviour class:
// submission order is not preserved on read-back, so list-valued options are
// sets and consumers must compare them as sets. Extra carries submitted keys
// the record does not type (sparse, blocksize, …) for faithful read-back.
type storeRecord struct {
	Storage string
	Type    string
	Content string
	Path    string
	Pool    string
	Nodes   string
	Shared  bool
	Disable bool
	Total   int64
	Used    int64
	Extra   map[string]string
}

// volRecord is one stored object on a node's storage. PVE has no storage-level
// snapshot endpoint (see storage.VolumeSnapshots), so a volume carries no
// snapshot state in the mock.
type volRecord struct {
	Volid   string
	Content string
	Format  string
	Size    int64
	VMID    int
}

// datastoreWritePayload mirrors the object POST /storage and
// PUT /storage/{storage} return. The mock never emits the optional config
// member: it supports no auto-generating storage type, and fabricating one
// would teach consumers to expect it.
type datastoreWritePayload struct {
	Storage string `json:"storage"`
	Type    string `json:"type"`
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

// zfsListPayload mirrors GET /nodes/{node}/disks/zfs entries.
type zfsListPayload struct {
	Name   string `json:"name"`
	Size   int64  `json:"size,omitempty"`
	Free   int64  `json:"free,omitempty"`
	Health string `json:"health,omitempty"`
}

// zfsStatusPayload mirrors GET /nodes/{node}/disks/zfs/{name}.
type zfsStatusPayload struct {
	Name  string `json:"name"`
	State string `json:"state,omitempty"`
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
		Storage: id, Type: storageType, Content: normalizeSet(content),
		Total: total, Used: used,
	}
	s.st.storage.bumpStorageDigest()
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

// AddZFSPool seeds a ZFS pool on node. Call before serving.
func (s *Server) AddZFSPool(node, name string, size, free int64) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	if s.st.storage.zfsPools == nil {
		s.st.storage.zfsPools = make(map[string]map[string]*zfsRecord)
	}
	if s.st.storage.zfsPools[node] == nil {
		s.st.storage.zfsPools[node] = make(map[string]*zfsRecord)
	}
	s.st.storage.zfsPools[node][name] = &zfsRecord{
		Name: name, Size: size, Free: free, Health: "ONLINE", State: "ONLINE",
	}
}

func (s *Server) registerStorageRoutes() {
	s.handle("GET /api2/json/storage", s.handleDatastoreList)
	s.handle("POST /api2/json/storage", s.handleDatastoreCreate)
	s.handle("GET /api2/json/storage/{storage}", s.handleDatastoreGet)
	s.handle("PUT /api2/json/storage/{storage}", s.handleDatastoreUpdate)
	s.handle("DELETE /api2/json/storage/{storage}", s.handleDatastoreDelete)
	s.handle("GET /api2/json/nodes/{node}/storage", s.handleNodeStorageList)
	s.handle("GET /api2/json/nodes/{node}/storage/{storage}/status", s.handleNodeStorageStatus)
	s.handle("GET /api2/json/nodes/{node}/storage/{storage}/content", s.handleContentList)
	s.handle("POST /api2/json/nodes/{node}/storage/{storage}/content", s.handleVolumeCreate)
	s.handle("GET /api2/json/nodes/{node}/storage/{storage}/content/{volid}", s.handleVolumeGet)
	s.handle("DELETE /api2/json/nodes/{node}/storage/{storage}/content/{volid}", s.handleVolumeDelete)
	// No .../content/{volid}/snapshot routes: PVE exposes no storage-level
	// volume-snapshot endpoint (see storage.VolumeSnapshots).
	s.handle("POST /api2/json/nodes/{node}/storage/{storage}/upload", s.handleStorageUpload)
	s.handle("GET /api2/json/nodes/{node}/disks/zfs", s.handleZFSList)
	s.handle("POST /api2/json/nodes/{node}/disks/zfs", s.handleZFSCreate)
	s.handle("GET /api2/json/nodes/{node}/disks/zfs/{name}", s.handleZFSGet)
}

// handleStorageUpload models the streaming multipart upload endpoint. It reads
// the multipart form (buffering is fine in the test responder), records the
// uploaded object as content, and returns an import task.
func (s *Server) handleStorageUpload(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	storage := r.PathValue("storage")
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid multipart body")
		return
	}
	content := r.FormValue("content")
	if content == "" {
		s.writeError(w, http.StatusBadRequest, "missing content")
		return
	}
	file, header, err := r.FormFile("filename")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "missing file part")
		return
	}
	n, cerr := io.Copy(io.Discard, file) // drain to measure size; the mock stores no bytes.
	_ = file.Close()
	if cerr != nil {
		s.writeError(w, http.StatusBadRequest, "read upload body")
		return
	}

	volid := storage + ":" + content + "/" + header.Filename
	s.AddVolume(node, storage, volid, content, "", n)
	s.writeData(w, s.finishedTask(node, "imgcopy", storage))
}

func (s *Server) handleDatastoreList(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.st.mu.Lock()
	out := make([]map[string]any, 0, len(s.st.storage.stores))
	for _, rec := range s.st.storage.stores {
		out = append(out, datastoreToPayload(rec, s.st.storage.cfgDigest))
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
	var payload map[string]any
	if rec != nil {
		payload = datastoreToPayload(rec, s.st.storage.cfgDigest)
	}
	s.st.mu.Unlock()
	if rec == nil {
		// Real PVE answers a missing id with HTTP 500 "storage '<id>' does
		// not exist" — NOT 404 (live-observed by the hoomlab consumer,
		// 2026-08-27; same wart class as GET /cluster/acme/plugins/{id}).
		// Mirroring it keeps consumer code that branches on ErrNotFound from
		// passing here and breaking on first real contact; existence checks
		// go through ListDatastores. The update/delete handlers keep 404 —
		// their real missing-id shape is unobserved.
		s.writeError(w, http.StatusInternalServerError,
			fmt.Sprintf("storage '%s' does not exist", id))
		return
	}
	s.writeData(w, payload)
}

// normalizeSet parses a comma-joined list into a set and re-joins it sorted.
// Real PVE treats list-valued storage options (content, nodes) as sets and
// does not preserve submission order on read-back; storing sorted makes a
// consumer that diffs read-vs-submitted strings break in the mock the same
// way it would live. Compare list-valued options as sets.
func normalizeSet(csv string) string {
	if csv == "" {
		return ""
	}
	seen := make(map[string]bool)
	for _, item := range strings.Split(csv, ",") {
		if item = strings.TrimSpace(item); item != "" {
			seen[item] = true
		}
	}
	items := make([]string, 0, len(seen))
	for item := range seen {
		items = append(items, item)
	}
	sort.Strings(items)
	return strings.Join(items, ",")
}

// Datastore config form keys consulted in more than one place, pulled out so
// goconst stays quiet.
const (
	storageKeyNodes   = "nodes"
	storageKeyDisable = "disable"
	storageKeyDelete  = "delete"
)

// createFixedKeys are the POST-only /storage parameters: PVE's update schema
// omits them, so identity and backing location are immutable after create and
// the update handler refuses them.
var createFixedKeys = []string{
	"type", "path", "export", "share", "target", "portal", "vgname",
	"thinpool", "base", "datastore", "iscsiprovider", "authsupported",
}

// applyStorageForm routes one submitted config key into the record: typed
// fields for what handlers consult, Extra for the rest. content/nodes are
// normalized as sets on the way in.
func applyStorageForm(rec *storeRecord, key, val string) {
	switch key {
	case "content":
		rec.Content = normalizeSet(val)
	case storageKeyNodes:
		rec.Nodes = normalizeSet(val)
	case "path":
		rec.Path = val
	case "pool":
		rec.Pool = val
	case "shared":
		rec.Shared = val == "1"
	case storageKeyDisable:
		rec.Disable = val == "1"
	default:
		if rec.Extra == nil {
			rec.Extra = make(map[string]string)
		}
		rec.Extra[key] = val
	}
}

// clearStorageKey unsets one config key (PVE's delete parameter).
func clearStorageKey(rec *storeRecord, key string) {
	switch key {
	case "content":
		rec.Content = ""
	case storageKeyNodes:
		rec.Nodes = ""
	case "path":
		rec.Path = ""
	case "pool":
		rec.Pool = ""
	case "shared":
		rec.Shared = false
	case storageKeyDisable:
		rec.Disable = false
	default:
		delete(rec.Extra, key)
	}
}

func (s *Server) handleDatastoreCreate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	if !s.parseForm(w, r) {
		return
	}
	id := r.PostForm.Get("storage")
	typ := r.PostForm.Get("type")
	if id == "" || typ == "" {
		s.writeError(w, http.StatusBadRequest, "missing storage or type")
		return
	}
	s.st.mu.Lock()
	if s.st.storage.stores[id] != nil {
		s.st.mu.Unlock()
		s.writeError(w, http.StatusBadRequest,
			fmt.Sprintf("storage ID '%s' already defined", id))
		return
	}
	rec := &storeRecord{Storage: id, Type: typ}
	for key := range r.PostForm {
		if key == "storage" || key == "type" {
			continue
		}
		applyStorageForm(rec, key, r.PostForm.Get(key))
	}
	// Real PVE materializes server-generated properties into the entry:
	// creating a zfspool storage writes "mountpoint /<pool>" to storage.cfg,
	// and reads carry it forever after (live-observed by the hoomlab
	// consumer, 2026-08-27). Mirroring it makes a consumer that full-map
	// compares read-vs-submitted rotate here, in tests, instead of live.
	if typ == "zfspool" && rec.Pool != "" && rec.Extra["mountpoint"] == "" {
		if rec.Extra == nil {
			rec.Extra = make(map[string]string)
		}
		rec.Extra["mountpoint"] = "/" + rec.Pool
	}
	if s.st.storage.stores == nil {
		s.st.storage.stores = make(map[string]*storeRecord)
	}
	s.st.storage.stores[id] = rec
	s.st.storage.bumpStorageDigest()
	s.st.mu.Unlock()
	s.writeData(w, datastoreWritePayload{Storage: id, Type: typ})
}

func (s *Server) handleDatastoreUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	if !s.parseForm(w, r) {
		return
	}
	for _, key := range createFixedKeys {
		if r.PostForm.Has(key) {
			s.writeError(w, http.StatusBadRequest,
				fmt.Sprintf("parameter '%s' is fixed at creation", key))
			return
		}
	}
	id := r.PathValue("storage")
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	rec := s.st.storage.stores[id]
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchStorage)
		return
	}
	if digest := r.PostForm.Get("digest"); digest != "" && digest != s.st.storage.cfgDigest {
		s.writeError(w, http.StatusBadRequest,
			"detected modified configuration - file changed by other user? Try again.")
		return
	}
	// delete applies before set-keys, the same contract as guest config.
	if del := r.PostForm.Get(storageKeyDelete); del != "" {
		for _, key := range strings.Split(del, ",") {
			clearStorageKey(rec, strings.TrimSpace(key))
		}
	}
	for key := range r.PostForm {
		if key == storageKeyDelete || key == "digest" {
			continue
		}
		applyStorageForm(rec, key, r.PostForm.Get(key))
	}
	s.st.storage.bumpStorageDigest()
	s.writeData(w, datastoreWritePayload{Storage: rec.Storage, Type: rec.Type})
}

func (s *Server) handleDatastoreDelete(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	id := r.PathValue("storage")
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	if s.st.storage.stores[id] == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchStorage)
		return
	}
	delete(s.st.storage.stores, id)
	s.st.storage.bumpStorageDigest()
	s.writeData(w, nil)
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

// handleVolumeCreate allocates a volume and returns its synthesized volid (PVE
// allocates synchronously, so the data is the volid string, not a task).
func (s *Server) handleVolumeCreate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	storage := r.PathValue("storage")
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		s.writeError(w, http.StatusBadRequest, msgInvalidForm)
		return
	}
	filename := r.PostForm.Get("filename")
	if filename == "" {
		s.writeError(w, http.StatusBadRequest, "missing filename")
		return
	}
	volid := storage + ":" + filename
	s.AddVolume(node, storage, volid, "images", r.PostForm.Get("format"), 0)
	if vmid, perr := strconv.Atoi(r.PostForm.Get("vmid")); perr == nil {
		s.st.mu.Lock()
		for _, v := range s.st.storage.content[node][storage] {
			if v.Volid == volid {
				v.VMID = vmid
			}
		}
		s.st.mu.Unlock()
	}
	s.writeData(w, volid)
}

// handleVolumeDelete frees a volume and returns a removal task.
func (s *Server) handleVolumeDelete(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	storage := r.PathValue("storage")
	volid := r.PathValue("volid")
	s.st.mu.Lock()
	vols := s.st.storage.content[node][storage]
	found := false
	kept := vols[:0]
	for _, v := range vols {
		if v.Volid == volid {
			found = true
			continue
		}
		kept = append(kept, v)
	}
	if found {
		s.st.storage.content[node][storage] = kept
	}
	s.st.mu.Unlock()
	if !found {
		s.writeError(w, http.StatusNotFound, msgNoSuchVolume)
		return
	}
	s.writeData(w, s.finishedTask(node, "imgdel", storage))
}

func (s *Server) handleZFSList(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	s.st.mu.Lock()
	pools := s.st.storage.zfsPools[node]
	out := make([]zfsListPayload, 0, len(pools))
	for _, p := range pools {
		out = append(out, zfsListPayload{Name: p.Name, Size: p.Size, Free: p.Free, Health: p.Health})
	}
	s.st.mu.Unlock()
	s.writeData(w, out)
}

func (s *Server) handleZFSGet(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node, name := r.PathValue("node"), r.PathValue("name")
	s.st.mu.Lock()
	rec := s.st.storage.zfsPools[node][name]
	var payload zfsStatusPayload
	if rec != nil {
		payload = zfsStatusPayload{Name: rec.Name, State: rec.State}
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchZFSPool)
		return
	}
	s.writeData(w, payload)
}

// handleZFSCreate registers a new pool from the "name" form param and returns
// the creation task. The "devices" param is accepted but not modelled.
func (s *Server) handleZFSCreate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
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
	s.AddZFSPool(node, name, 0, 0)
	s.writeData(w, s.finishedTask(node, "zfscreate", name))
}

// datastoreToPayload builds the flat object a datastore config read returns.
// A map, not a struct: submitted-but-untyped keys (Extra) ride the same flat
// JSON object on real PVE. digest is the cluster-wide storage.cfg digest.
func datastoreToPayload(rec *storeRecord, digest string) map[string]any {
	p := map[string]any{"storage": rec.Storage, "type": rec.Type}
	if rec.Content != "" {
		p["content"] = rec.Content
	}
	if rec.Path != "" {
		p["path"] = rec.Path
	}
	if rec.Pool != "" {
		p["pool"] = rec.Pool
	}
	if rec.Nodes != "" {
		p[storageKeyNodes] = rec.Nodes
	}
	if rec.Shared {
		p["shared"] = 1
	}
	if rec.Disable {
		p[storageKeyDisable] = 1
	}
	if digest != "" {
		p["digest"] = digest
	}
	for k, v := range rec.Extra {
		p[k] = v
	}
	return p
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
