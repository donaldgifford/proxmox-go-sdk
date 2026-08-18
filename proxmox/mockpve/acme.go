package mockpve

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// This file models the cluster-scoped ACME challenge-plugin surface and the
// discovery reads (IMPL-0007 Phase 1). ACME accounts live in nodesadmin.go with
// the node-certificate routes they were written beside.

const (
	msgNoSuchACMEPlugin = "no such acme plugin"
	msgDigestMismatch   = "config digest mismatch - configuration changed"

	// mockACMEPluginDigest is the digest every plugin read reports. Real PVE
	// derives it from the config file contents; the mock only needs a stable
	// value a caller can echo back, plus one it will never mint so a wrong
	// digest can be tested.
	mockACMEPluginDigest = "mockpve-acme-plugin-digest"
)

// acmePluginRecord is one ACME challenge plugin (cluster-scoped). Data is stored
// verbatim — the base64 blob the SDK rendered — because real PVE stores and
// returns it unchanged, and the SDK's verbatim-round-trip property is what the
// unit tests assert.
type acmePluginRecord struct {
	ID              string
	Type            string
	API             string
	Data            string
	ValidationDelay string
	Nodes           string
	Disable         string
}

// acmePluginPayload is the wire shape of a plugin read. PVE names the id field
// "plugin" in responses even though the create parameter is "id".
type acmePluginPayload struct {
	Plugin          string `json:"plugin"`
	Type            string `json:"type,omitempty"`
	API             string `json:"api,omitempty"`
	Data            string `json:"data,omitempty"`
	ValidationDelay int    `json:"validation-delay,omitempty"`
	Nodes           string `json:"nodes,omitempty"`
	Disable         int    `json:"disable,omitempty"`
	Digest          string `json:"digest,omitempty"`
}

func (rec *acmePluginRecord) payload() acmePluginPayload {
	return acmePluginPayload{
		Plugin: rec.ID, Type: rec.Type, API: rec.API, Data: rec.Data,
		ValidationDelay: atoiOrZero(rec.ValidationDelay),
		Nodes:           rec.Nodes,
		Disable:         atoiOrZero(rec.Disable),
		Digest:          mockACMEPluginDigest,
	}
}

// atoiOrZero yields 0 for an empty or non-numeric value. The mock keeps numeric
// plugin fields as the strings the form carried — which is what makes a partial
// update's "only overwrite what was sent" rule trivial — so this is where they
// become wire integers.
func atoiOrZero(n string) int {
	v, err := strconv.Atoi(n)
	if err != nil {
		return 0
	}
	return v
}

// AddACMEPlugin seeds an ACME challenge plugin. data is stored and returned
// verbatim, so a seed can carry a pre-rendered base64 payload. Call before
// serving.
func (s *Server) AddACMEPlugin(id, challengeType, api, data string) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	if s.st.acmePlugins == nil {
		s.st.acmePlugins = make(map[string]*acmePluginRecord)
	}
	s.st.acmePlugins[id] = &acmePluginRecord{
		ID: id, Type: challengeType, API: api, Data: data,
	}
}

// --- routes ---

func (s *Server) registerACMERoutes() {
	s.handle("GET /api2/json/cluster/acme/plugins", s.handleACMEPluginList)
	s.handle("POST /api2/json/cluster/acme/plugins", s.handleACMEPluginCreate)
	s.handle("GET /api2/json/cluster/acme/plugins/{id}", s.handleACMEPluginGet)
	s.handle("PUT /api2/json/cluster/acme/plugins/{id}", s.handleACMEPluginUpdate)
	s.handle("DELETE /api2/json/cluster/acme/plugins/{id}", s.handleACMEPluginDelete)
	s.handle("GET /api2/json/cluster/acme/challenge-schema", s.handleACMEChallengeSchema)
	s.handle("GET /api2/json/cluster/acme/directories", s.handleACMEDirectories)
	s.handle("GET /api2/json/cluster/acme/meta", s.handleACMEMeta)
}

func (s *Server) handleACMEPluginList(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.st.mu.Lock()
	out := make([]acmePluginPayload, 0, len(s.st.acmePlugins))
	for _, rec := range s.st.acmePlugins {
		out = append(out, rec.payload())
	}
	s.st.mu.Unlock()
	// Map iteration is random; sort so a consumer's Example (or a test asserting
	// on the first entry) is reproducible.
	slices.SortFunc(out, func(a, b acmePluginPayload) int {
		return strings.Compare(a.Plugin, b.Plugin)
	})
	s.writeData(w, out)
}

func (s *Server) handleACMEPluginGet(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	id := r.PathValue("id")
	s.st.mu.Lock()
	rec := s.st.acmePlugins[id]
	var payload acmePluginPayload
	if rec != nil {
		payload = rec.payload()
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchACMEPlugin)
		return
	}
	s.writeData(w, payload)
}

// handleACMEPluginCreate registers a plugin. Synchronous, like real PVE: the
// config write returns 200 with null data, never a UPID.
func (s *Server) handleACMEPluginCreate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	if !s.parseForm(w, r) {
		return
	}
	id := r.PostForm.Get("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "missing id")
		return
	}
	s.st.mu.Lock()
	if s.st.acmePlugins == nil {
		s.st.acmePlugins = make(map[string]*acmePluginRecord)
	}
	s.st.acmePlugins[id] = &acmePluginRecord{
		ID:              id,
		Type:            r.PostForm.Get("type"),
		API:             r.PostForm.Get("api"),
		Data:            r.PostForm.Get("data"),
		ValidationDelay: r.PostForm.Get("validation-delay"),
		Nodes:           r.PostForm.Get("nodes"),
		Disable:         r.PostForm.Get("disable"),
	}
	s.st.mu.Unlock()
	s.writeData(w, nil)
}

// handleACMEPluginUpdate applies a partial update. It honours PVE's digest
// guard (a stale digest is refused) and its delete parameter, and only
// overwrites the fields the form carries so an update that omits data keeps the
// stored credentials.
func (s *Server) handleACMEPluginUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	if !s.parseForm(w, r) {
		return
	}
	id := r.PathValue("id")
	s.st.mu.Lock()
	rec := s.st.acmePlugins[id]
	stale := rec != nil && r.PostForm.Get("digest") != "" &&
		r.PostForm.Get("digest") != mockACMEPluginDigest
	if rec != nil && !stale {
		applyACMEPluginForm(rec, r.PostForm)
	}
	s.st.mu.Unlock()
	switch {
	case rec == nil:
		s.writeError(w, http.StatusNotFound, msgNoSuchACMEPlugin)
	case stale:
		s.writeError(w, http.StatusBadRequest, msgDigestMismatch)
	default:
		s.writeData(w, nil)
	}
}

// applyACMEPluginForm mutates rec with the form's set fields, then applies the
// delete list. Deletes run last so a single request that both sets and unsets is
// unambiguous, matching how PVE processes its delete parameter.
func applyACMEPluginForm(rec *acmePluginRecord, form url.Values) {
	set := func(key string, dst *string) {
		if v := form.Get(key); v != "" {
			*dst = v
		}
	}
	set("type", &rec.Type)
	set("api", &rec.API)
	set("data", &rec.Data)
	set("validation-delay", &rec.ValidationDelay)
	set("nodes", &rec.Nodes)
	set("disable", &rec.Disable)

	if del := form.Get("delete"); del != "" {
		for _, key := range strings.Split(del, ",") {
			switch strings.TrimSpace(key) {
			case "validation-delay":
				rec.ValidationDelay = ""
			case "nodes":
				rec.Nodes = ""
			case "disable":
				rec.Disable = ""
			case "data":
				rec.Data = ""
			}
		}
	}
}

func (s *Server) handleACMEPluginDelete(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	id := r.PathValue("id")
	s.st.mu.Lock()
	rec := s.st.acmePlugins[id]
	if rec != nil {
		delete(s.st.acmePlugins, id)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchACMEPlugin)
		return
	}
	s.writeData(w, nil)
}

// challengeSchemaPayload is one entry of GET /cluster/acme/challenge-schema.
// Schema is raw JSON because it is provider-defined on real PVE.
type challengeSchemaPayload struct {
	ID     string          `json:"id"`
	Name   string          `json:"name"`
	Type   string          `json:"type"`
	Schema json.RawMessage `json:"schema"`
}

// handleACMEChallengeSchema serves a static two-provider schema: the provider
// the SDK types first (cf) and the one that proves the model generalises
// (namecheap). The field sets mirror the acme.sh variables the SDK's typed
// providers render, which is what makes this useful to a consumer discovering
// fields for RawPluginData.
func (s *Server) handleACMEChallengeSchema(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.writeData(w, []challengeSchemaPayload{
		{
			ID: "cf", Name: "Cloudflare", Type: "dns",
			Schema: json.RawMessage(`{"fields":{` +
				`"CF_Token":{"description":"API token","type":"string"},` +
				`"CF_Account_ID":{"description":"Account ID","type":"string"},` +
				`"CF_Zone_ID":{"description":"Zone ID","optional":1,"type":"string"},` +
				`"CF_Key":{"description":"Global API key","type":"string"},` +
				`"CF_Email":{"description":"Account email","type":"string"}}}`),
		},
		{
			ID: "namecheap", Name: "Namecheap", Type: "dns",
			Schema: json.RawMessage(`{"fields":{` +
				`"NAMECHEAP_USERNAME":{"description":"API username","type":"string"},` +
				`"NAMECHEAP_API_KEY":{"description":"API key","type":"string"},` +
				`"NAMECHEAP_SOURCEIP":{"description":"Allowlisted source IP","type":"string"}}}`),
		},
	})
}

type acmeDirectoryPayload struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// handleACMEDirectories serves the named CA endpoints PVE ships. Staging is
// included because it is the one a test suite should point at.
func (s *Server) handleACMEDirectories(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.writeData(w, []acmeDirectoryPayload{
		{Name: "Let's Encrypt V2", URL: "https://acme-v02.api.letsencrypt.org/directory"},
		{
			Name: "Let's Encrypt V2 Staging",
			URL:  "https://acme-staging-v02.api.letsencrypt.org/directory",
		},
	})
}

// handleACMEMeta serves the CA directory metadata. The response carries a key
// the SDK does not model (externalAccountBinding) so the lossless-read path is
// exercised by the unit tests rather than assumed, and it echoes the optional
// directory query parameter so a test can prove the option reached the wire.
func (s *Server) handleACMEMeta(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	terms := "https://acme.example/terms/v1"
	if dir := r.URL.Query().Get("directory"); dir != "" {
		terms = dir + "/terms"
	}
	s.writeData(w, map[string]any{
		"termsOfService":          terms,
		"website":                 "https://acme.example",
		"caaIdentities":           []string{"acme.example"},
		"externalAccountRequired": false,
		"externalAccountBinding":  "unmodelled",
	})
}
