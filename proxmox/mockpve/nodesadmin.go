package mockpve

import (
	"net/http"
	"strconv"
	"strings"
)

// This file models the node-administration surface (task 4): apt updates and
// repositories, disks and SMART, node certificates, and cluster-scoped ACME
// accounts.

// aptUpdateRecord is one pending package update in the mock.
type aptUpdateRecord struct {
	Package    string
	Title      string
	Version    string
	OldVersion string
	Priority   string
}

// nodeRepoFileRecord is one apt source file and the repositories it declares.
type nodeRepoFileRecord struct {
	Path     string
	FileType string
	Repos    []nodeRepoRecord
}

type nodeRepoRecord struct {
	Types      []string
	URIs       []string
	Suites     []string
	Components []string
	Enabled    bool
	Comment    string
}

// nodeDiskRecord is one physical disk in the mock.
type nodeDiskRecord struct {
	DevPath string
	Model   string
	Serial  string
	Size    int64
	Type    string
	Health  string
	GPT     bool
}

// nodeCertRecord is one node certificate in the mock.
type nodeCertRecord struct {
	Filename    string
	Fingerprint string
	Subject     string
	Issuer      string
	NotAfter    int64
	SAN         []string
	PEM         string
}

// acmeAccountRecord is one registered ACME account (cluster-scoped).
type acmeAccountRecord struct {
	Name      string
	Directory string
	Location  string
	TOS       string
	Contact   []string
}

// --- payloads ---

type aptUpdatePayload struct {
	Package    string `json:"Package"`
	Title      string `json:"Title,omitempty"`
	Version    string `json:"Version,omitempty"`
	OldVersion string `json:"OldVersion,omitempty"`
	Priority   string `json:"Priority,omitempty"`
}

type repoPayload struct {
	Types      []string `json:"Types,omitempty"`
	URIs       []string `json:"URIs,omitempty"`
	Suites     []string `json:"Suites,omitempty"`
	Components []string `json:"Components,omitempty"`
	Enabled    int      `json:"Enabled"`
	Comment    string   `json:"Comment,omitempty"`
}

type repoFilePayload struct {
	Path         string        `json:"path"`
	FileType     string        `json:"file-type,omitempty"`
	Repositories []repoPayload `json:"repositories,omitempty"`
}

type repositoriesPayload struct {
	Files  []repoFilePayload `json:"files"`
	Errors []string          `json:"errors,omitempty"`
	Digest string            `json:"digest,omitempty"`
}

type diskPayload struct {
	DevPath string `json:"devpath"`
	Model   string `json:"model,omitempty"`
	Serial  string `json:"serial,omitempty"`
	Size    int64  `json:"size,omitempty"`
	Type    string `json:"type,omitempty"`
	Health  string `json:"health,omitempty"`
	GPT     int    `json:"gpt,omitempty"`
}

type smartAttrPayload struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Raw  string `json:"raw"`
}

type smartPayload struct {
	Health     string             `json:"health"`
	Type       string             `json:"type"`
	Attributes []smartAttrPayload `json:"attributes,omitempty"`
}

type certPayload struct {
	Filename    string   `json:"filename"`
	Fingerprint string   `json:"fingerprint,omitempty"`
	Subject     string   `json:"subject,omitempty"`
	Issuer      string   `json:"issuer,omitempty"`
	NotAfter    int64    `json:"notafter,omitempty"`
	SAN         []string `json:"san,omitempty"`
	PEM         string   `json:"pem,omitempty"`
}

type acmeAccountPayload struct {
	Location  string `json:"location,omitempty"`
	Directory string `json:"directory,omitempty"`
	TOS       string `json:"tos,omitempty"`
	// Account is the CA's own account object. Real PVE passes it through
	// verbatim, and the contact addresses live inside it rather than in a
	// top-level field — so a caller reading back a contact change has to look
	// here, and the mock has to carry it for that to be testable.
	Account *acmeCAAccount `json:"account,omitempty"`
}

// acmeCAAccount is the CA-side account object: what an ACME server returns for
// a registration, which PVE passes through untouched.
type acmeCAAccount struct {
	Status  string   `json:"status"`
	Contact []string `json:"contact"`
}

// acmeAccountObject renders the CA-side account object for a record, with each
// contact as an RFC 8555 mailto URI. A nil record renders nothing, so the
// payload's omitempty drops the field rather than the handler having to guard
// the call (the record is passed by pointer because it is over gocritic's
// hugeParam threshold, which makes nil reachable).
func acmeAccountObject(rec *acmeAccountRecord) *acmeCAAccount {
	if rec == nil {
		return nil
	}
	out := &acmeCAAccount{Status: "valid", Contact: make([]string, 0, len(rec.Contact))}
	for _, c := range rec.Contact {
		if c = strings.TrimSpace(c); c == "" {
			continue
		}
		if !strings.HasPrefix(c, "mailto:") {
			c = "mailto:" + c
		}
		out.Contact = append(out.Contact, c)
	}
	return out
}

// --- seeders ---

// AddAptUpdate seeds a pending package update on node. Call before serving.
func (s *Server) AddAptUpdate(node, pkg, version, oldVersion string) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	n := s.ensureNodeLocked(node)
	n.aptUpdates = append(n.aptUpdates, aptUpdateRecord{
		Package: pkg, Title: pkg, Version: version, OldVersion: oldVersion, Priority: "standard",
	})
}

// AddRepository seeds an apt source file with one repository on node. Call
// before serving.
func (s *Server) AddRepository(node, path, uri string, enabled bool) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	n := s.ensureNodeLocked(node)
	n.repos = append(n.repos, nodeRepoFileRecord{
		Path: path, FileType: "sources",
		Repos: []nodeRepoRecord{{
			Types: []string{"deb"}, URIs: []string{uri},
			Suites: []string{"trixie"}, Components: []string{"main"}, Enabled: enabled,
		}},
	})
}

// AddDisk seeds a physical disk on node. Call before serving.
func (s *Server) AddDisk(node, devpath, diskType string) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	n := s.ensureNodeLocked(node)
	n.disks = append(n.disks, nodeDiskRecord{
		DevPath: devpath, Model: "MOCK-DISK", Serial: "SN-" + devpath,
		Size: 512110190592, Type: diskType, Health: "PASSED", GPT: true,
	})
}

// AddNodeCertificate seeds a certificate on node. Call before serving.
func (s *Server) AddNodeCertificate(node, filename string) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	n := s.ensureNodeLocked(node)
	n.certs = append(n.certs, nodeCertRecord{
		Filename: filename, Fingerprint: "AA:BB:CC", Subject: "CN=" + node,
		Issuer: "CN=" + node, NotAfter: 4102444800, SAN: []string{node},
	})
}

// AddACMEAccount seeds a registered ACME account (cluster-scoped). Call before
// serving.
func (s *Server) AddACMEAccount(name string) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	if s.st.acmeAccounts == nil {
		s.st.acmeAccounts = make(map[string]*acmeAccountRecord)
	}
	s.st.acmeAccounts[name] = &acmeAccountRecord{
		Name: name, Directory: "https://acme.example/directory",
		Location: "https://acme.example/acct/1", TOS: "https://acme.example/tos",
	}
}

// --- routes ---

func (s *Server) registerNodeAdminRoutes() {
	s.handle("GET /api2/json/nodes/{node}/apt/update", s.handleAptUpdateList)
	s.handle("POST /api2/json/nodes/{node}/apt/update", s.handleAptRefresh)
	s.handle("GET /api2/json/nodes/{node}/apt/repositories", s.handleRepoList)
	s.handle("POST /api2/json/nodes/{node}/apt/repositories", s.handleRepoUpdate)
	s.handle("GET /api2/json/nodes/{node}/disks/list", s.handleDiskList)
	s.handle("GET /api2/json/nodes/{node}/disks/smart", s.handleDiskSMART)
	s.handle("POST /api2/json/nodes/{node}/disks/initgpt", s.handleDiskInitGPT)
	s.handle("GET /api2/json/nodes/{node}/certificates/info", s.handleCertInfo)
	s.handle("POST /api2/json/nodes/{node}/certificates/custom", s.handleCertUpload)
	s.handle("DELETE /api2/json/nodes/{node}/certificates/custom", s.handleCertDelete)
	s.handle("POST /api2/json/nodes/{node}/certificates/acme/certificate", s.handleCertACME)
	s.handle("PUT /api2/json/nodes/{node}/certificates/acme/certificate", s.handleCertACME)
	s.handle("DELETE /api2/json/nodes/{node}/certificates/acme/certificate", s.handleCertACME)
	s.handle("GET /api2/json/cluster/acme/account", s.handleACMEAccountList)
	s.handle("POST /api2/json/cluster/acme/account", s.handleACMEAccountCreate)
	s.handle("GET /api2/json/cluster/acme/account/{name}", s.handleACMEAccountGet)
	s.handle("PUT /api2/json/cluster/acme/account/{name}", s.handleACMEAccountUpdate)
	s.handle("DELETE /api2/json/cluster/acme/account/{name}", s.handleACMEAccountDelete)
}

func (s *Server) handleAptUpdateList(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	s.st.mu.Lock()
	var out []aptUpdatePayload
	if n := s.st.nodes[node]; n != nil {
		out = make([]aptUpdatePayload, 0, len(n.aptUpdates))
		for _, rec := range n.aptUpdates {
			out = append(out, aptUpdatePayload(rec))
		}
	}
	s.st.mu.Unlock()
	s.writeData(w, out)
}

// handleAptRefresh runs `apt update` and returns a worker task.
func (s *Server) handleAptRefresh(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	s.writeData(w, s.finishedTask(node, "aptupdate", "aptupdate"))
}

func (s *Server) handleRepoList(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	s.st.mu.Lock()
	var files []repoFilePayload
	if n := s.st.nodes[node]; n != nil {
		files = make([]repoFilePayload, 0, len(n.repos))
		for _, f := range n.repos {
			files = append(files, repoFileToPayload(f))
		}
	}
	s.st.mu.Unlock()
	s.writeData(w, repositoriesPayload{Files: files, Digest: "mock-digest"})
}

// handleRepoUpdate toggles one repository (by path + index). Synchronous.
func (s *Server) handleRepoUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	if !s.parseForm(w, r) {
		return
	}
	node := r.PathValue("node")
	path := r.PostForm.Get("path")
	index := 0
	if v := r.PostForm.Get("index"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid index")
			return
		}
		index = parsed
	}
	enabled := r.PostForm.Get("enabled") == "1"
	s.st.mu.Lock()
	found := false
	if n := s.st.nodes[node]; n != nil {
		for fi := range n.repos {
			if n.repos[fi].Path != path {
				continue
			}
			if index >= 0 && index < len(n.repos[fi].Repos) {
				n.repos[fi].Repos[index].Enabled = enabled
				found = true
			}
		}
	}
	s.st.mu.Unlock()
	if !found {
		s.writeError(w, http.StatusNotFound, msgNoSuchRepo)
		return
	}
	s.writeData(w, nil)
}

func (s *Server) handleDiskList(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	s.st.mu.Lock()
	var out []diskPayload
	if n := s.st.nodes[node]; n != nil {
		out = make([]diskPayload, 0, len(n.disks))
		for _, rec := range n.disks {
			out = append(out, diskPayload{
				DevPath: rec.DevPath, Model: rec.Model, Serial: rec.Serial,
				Size: rec.Size, Type: rec.Type, Health: rec.Health, GPT: boolToInt(rec.GPT),
			})
		}
	}
	s.st.mu.Unlock()
	s.writeData(w, out)
}

func (s *Server) handleDiskSMART(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	disk := r.URL.Query().Get("disk")
	s.st.mu.Lock()
	found := false
	if n := s.st.nodes[node]; n != nil {
		for _, rec := range n.disks {
			if rec.DevPath == disk {
				found = true
				break
			}
		}
	}
	s.st.mu.Unlock()
	if !found {
		s.writeError(w, http.StatusNotFound, msgNoSuchDisk)
		return
	}
	s.writeData(w, smartPayload{
		Health: "PASSED", Type: "ata",
		Attributes: []smartAttrPayload{{ID: 5, Name: "Reallocated_Sector_Ct", Raw: "0"}},
	})
}

// handleDiskInitGPT writes a GPT label and returns a worker task.
func (s *Server) handleDiskInitGPT(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	if !s.parseForm(w, r) {
		return
	}
	node := r.PathValue("node")
	if r.PostForm.Get("disk") == "" {
		s.writeError(w, http.StatusBadRequest, msgNoSuchDisk)
		return
	}
	s.writeData(w, s.finishedTask(node, "diskinit", "diskinit"))
}

func (s *Server) handleCertInfo(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	s.st.mu.Lock()
	out := certRecordsToPayload(s.certsForNodeLocked(node))
	s.st.mu.Unlock()
	s.writeData(w, out)
}

// handleCertUpload installs a custom certificate and returns the cert set.
func (s *Server) handleCertUpload(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	if !s.parseForm(w, r) {
		return
	}
	node := r.PathValue("node")
	if r.PostForm.Get("certificates") == "" {
		s.writeError(w, http.StatusBadRequest, "missing certificates")
		return
	}
	s.st.mu.Lock()
	n := s.ensureNodeLocked(node)
	n.certs = append(n.certs, nodeCertRecord{
		Filename: "pveproxy-ssl.pem", Subject: "CN=custom", Issuer: "CN=custom",
		NotAfter: 4102444800, PEM: r.PostForm.Get("certificates"),
	})
	out := certRecordsToPayload(n.certs)
	s.st.mu.Unlock()
	s.writeData(w, out)
}

// handleCertDelete removes the custom certificate. Synchronous.
func (s *Server) handleCertDelete(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	s.st.mu.Lock()
	if n := s.st.nodes[node]; n != nil {
		n.certs = nil
	}
	s.st.mu.Unlock()
	s.writeData(w, nil)
}

// handleCertACME serves order (POST) / renew (PUT) / revoke (DELETE); all return
// a worker task.
//
// It moves the node's certificate state, because on real PVE that is the whole
// point of the call: an order installs a certificate the node then serves, and a
// revoke takes it away. A mock that answered with a task and changed nothing
// would let a consumer's ACME automation pass its tests while never checking the
// thing it exists to check.
//
// The failure side is NOT modelled: ordering on a node with no ACME config, or
// one naming a plugin that does not exist, installs nothing here and still
// reports OK. Real PVE fails that — but whether it fails the API call or the
// worker task is unverified (IMPL-0007 Phase 4 exercises the happy path), and
// guessing would hand consumers an error shape to code against that may not be
// the real one.
func (s *Server) handleCertACME(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")

	s.st.mu.Lock()
	n := s.ensureNodeLocked(node)
	n.certs = withoutACMECert(n.certs)
	if r.Method != http.MethodDelete {
		if san := acmeSANsLocked(n); len(san) > 0 {
			n.certs = append(n.certs, nodeCertRecord{
				Filename: acmeCertFilename, Fingerprint: "AC:AC:AC",
				Subject: "CN=" + san[0], Issuer: mockACMEIssuer,
				NotAfter: 4102444800, SAN: san,
			})
		}
	}
	s.st.mu.Unlock()

	s.writeData(w, s.finishedTask(node, "acmecert", "acmecert"))
}

// The filename PVE installs an ACME certificate under, and the issuer this mock
// stamps on it — deliberately not a real CA name, so a test asserting on a
// trusted issuer cannot pass here and fail live.
const (
	acmeCertFilename = "pveproxy-ssl.pem"
	mockACMEIssuer   = "CN=mockpve ACME CA"
	// PVE declares acmedomain0..acmedomain5 (the GET's property enum, with
	// additionalProperties:0 on the PUT), so six is the whole range.
	acmeDomainSlots = 6
)

// withoutACMECert returns recs with any mock-issued ACME certificate removed. A
// custom uploaded certificate shares the filename, so the issuer is what
// distinguishes them: an order replaces an ACME cert, not the operator's own.
func withoutACMECert(recs []nodeCertRecord) []nodeCertRecord {
	out := recs[:0:0]
	for _, rec := range recs {
		if rec.Filename == acmeCertFilename && rec.Issuer == mockACMEIssuer {
			continue
		}
		out = append(out, rec)
	}
	return out
}

// acmeSANsLocked collects the names an order would certify from the node's own
// config: the acmedomain slots, then the legacy acme=domains list. Caller holds
// mu. An empty result means the node was never wired for ACME, which is why an
// order there installs nothing.
func acmeSANsLocked(n *nodeState) []string {
	var san []string
	seen := make(map[string]bool)
	add := func(domain string) {
		if domain = strings.TrimSpace(domain); domain != "" && !seen[domain] {
			seen[domain] = true
			san = append(san, domain)
		}
	}
	for i := range acmeDomainSlots {
		v, ok := n.config["acmedomain"+strconv.Itoa(i)]
		if !ok {
			continue
		}
		// "domain=host,plugin=cf" or the bare-default-key "host,plugin=cf".
		first, _, _ := strings.Cut(v, ",")
		add(strings.TrimPrefix(first, "domain="))
	}
	for _, part := range strings.Split(n.config["acme"], ",") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(part), "domains="); ok {
			for _, d := range strings.Split(rest, ";") {
				add(d)
			}
		}
	}
	return san
}

func (s *Server) handleACMEAccountList(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.st.mu.Lock()
	out := make([]map[string]string, 0, len(s.st.acmeAccounts))
	for name := range s.st.acmeAccounts {
		out = append(out, map[string]string{"name": name})
	}
	s.st.mu.Unlock()
	s.writeData(w, out)
}

// handleACMEAccountCreate registers an account and returns a worker task.
func (s *Server) handleACMEAccountCreate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	if !s.parseForm(w, r) {
		return
	}
	name := r.PostForm.Get("name")
	if name == "" {
		s.writeError(w, http.StatusBadRequest, "missing name")
		return
	}
	s.st.mu.Lock()
	if s.st.acmeAccounts == nil {
		s.st.acmeAccounts = make(map[string]*acmeAccountRecord)
	}
	s.st.acmeAccounts[name] = &acmeAccountRecord{
		Name: name, Directory: r.PostForm.Get("directory"),
		Location: "https://acme.example/acct/" + name, TOS: r.PostForm.Get("tos_url"),
		Contact: splitCSV(r.PostForm.Get("contact")),
	}
	s.st.mu.Unlock()
	// ACME registration runs on the local node; the mock issues it on "pve".
	s.writeData(w, s.finishedTask("pve", "acmeregister", name))
}

func (s *Server) handleACMEAccountGet(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	name := r.PathValue("name")
	s.st.mu.Lock()
	rec := s.st.acmeAccounts[name]
	var payload acmeAccountPayload
	if rec != nil {
		payload = acmeAccountPayload{
			Location: rec.Location, Directory: rec.Directory, TOS: rec.TOS,
			Account: acmeAccountObject(rec),
		}
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchACMEAccount)
		return
	}
	s.writeData(w, payload)
}

// handleACMEAccountUpdate changes an account's contact and returns a worker
// task. PVE talks to the CA to apply the change (its own description notes that
// sending no new information triggers a refresh), and the 9.2 schema declares a
// bare string return — its house style for a UPID.
func (s *Server) handleACMEAccountUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	if !s.parseForm(w, r) {
		return
	}
	name := r.PathValue("name")
	s.st.mu.Lock()
	rec := s.st.acmeAccounts[name]
	if rec != nil {
		if v := r.PostForm.Get("contact"); v != "" {
			rec.Contact = splitCSV(v)
		}
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchACMEAccount)
		return
	}
	s.writeData(w, s.finishedTask("pve", "acmeupdate", name))
}

// handleACMEAccountDelete deactivates an account and returns a worker task.
func (s *Server) handleACMEAccountDelete(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	name := r.PathValue("name")
	s.st.mu.Lock()
	rec := s.st.acmeAccounts[name]
	if rec != nil {
		delete(s.st.acmeAccounts, name)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchACMEAccount)
		return
	}
	s.writeData(w, s.finishedTask("pve", "acmedeactivate", name))
}

// --- helpers ---

func (s *Server) certsForNodeLocked(node string) []nodeCertRecord {
	if n := s.st.nodes[node]; n != nil {
		return n.certs
	}
	return nil
}

func certRecordsToPayload(recs []nodeCertRecord) []certPayload {
	out := make([]certPayload, 0, len(recs))
	for _, rec := range recs {
		out = append(out, certPayload(rec))
	}
	return out
}

func repoFileToPayload(f nodeRepoFileRecord) repoFilePayload {
	repos := make([]repoPayload, 0, len(f.Repos))
	for _, rp := range f.Repos {
		repos = append(repos, repoPayload{
			Types: rp.Types, URIs: rp.URIs, Suites: rp.Suites, Components: rp.Components,
			Enabled: boolToInt(rp.Enabled), Comment: rp.Comment,
		})
	}
	return repoFilePayload{Path: f.Path, FileType: f.FileType, Repositories: repos}
}
