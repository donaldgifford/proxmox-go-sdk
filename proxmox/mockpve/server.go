package mockpve

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
)

// Server is an in-memory PVE responder. It implements [http.Handler] and is
// safe for concurrent use. Construct it with [New]; seed it with the Seed/Add
// methods before serving requests.
type Server struct {
	mux        *http.ServeMux
	st         state
	logger     *slog.Logger
	cache      ResponseCache
	httpClient *http.Client
	tls        bool
}

// Option configures a Server.
type Option func(*Server)

var _ http.Handler = (*Server)(nil)

// ResponseCache is the seam for corpus-seeded responses (OQ-10). When set via
// [WithCache], a hit for (method, path) is returned verbatim as the envelope's
// data payload, bypassing the stateful model. The recorded-corpus loader that
// fills it is deferred to a later phase.
type ResponseCache interface {
	Lookup(method, path string) (json.RawMessage, bool)
}

// New returns a Server seeded with a default state: version "9.0.3", a single
// node "pve", and no tasks, tickets, or users.
func New(opts ...Option) *Server {
	s := &Server{
		mux:    http.NewServeMux(),
		logger: slog.Default(),
		st: state{
			version: versionData{Version: "9.0.3", Release: "9.0", RepoID: "mockpve", Console: "xtermjs"},
			nodes:   map[string]*nodeState{"pve": {tasks: make(map[string]*taskRecord)}},
			tickets: make(map[string]ticketRecord),
			users:   make(map[string]string),
		},
	}
	for _, opt := range opts {
		opt(s)
	}
	s.registerRoutes()
	return s
}

// WithLogger sets the slog.Logger used for per-request debug lines. A nil
// logger is ignored.
func WithLogger(l *slog.Logger) Option {
	return func(s *Server) {
		if l != nil {
			s.logger = l
		}
	}
}

// WithCache injects a ResponseCache consulted before the stateful model on
// every request (see [ResponseCache]).
func WithCache(c ResponseCache) Option {
	return func(s *Server) { s.cache = c }
}

// WithHTTPClient makes [Server.NewClient] hand back an api.Client that uses h
// as its transport (e.g. to set a custom timeout). Without it, NewClient builds
// a default client.
func WithHTTPClient(h *http.Client) Option {
	return func(s *Server) { s.httpClient = h }
}

// WithTLS serves over HTTPS with httptest's self-signed certificate, so tests
// can exercise the SDK transport's InsecureSkipVerify path. [Server.NewClient]
// then sets InsecureSkipVerify automatically.
func WithTLS() Option {
	return func(s *Server) { s.tls = true }
}

// ServeHTTP routes a request: a cache hit short-circuits the model, otherwise
// the registered handlers serve it.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.logger.Debug("mockpve request", "method", r.Method, "path", r.URL.Path)
	if s.cache != nil {
		if raw, ok := s.cache.Lookup(r.Method, r.URL.Path); ok {
			s.writeData(w, raw)
			return
		}
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /api2/json/version", s.handleVersion)
	s.mux.HandleFunc("POST /api2/json/access/ticket", s.handleTicket)
	s.mux.HandleFunc("GET /api2/json/nodes/{node}/tasks/{upid}/status", s.handleTaskStatus)
	s.mux.HandleFunc("GET /api2/json/nodes/{node}/tasks/{upid}/log", s.handleTaskLog)
	s.registerQEMURoutes()
	s.registerLXCRoutes()
	s.registerStorageRoutes()
	s.registerHARoutes()
	s.registerNodeNetworkRoutes()
	s.registerSDNRoutes()
}

// Serve starts an httptest.Server for this mock (HTTPS when [WithTLS] is set,
// otherwise HTTP). The caller must Close it.
func (s *Server) Serve() *httptest.Server {
	if s.tls {
		return httptest.NewTLSServer(s)
	}
	return httptest.NewServer(s)
}

// NewClient starts a test server for this mock and returns an api.Client wired
// to it (with mock token credentials) plus a cleanup func that shuts the server
// down. opts are forwarded to api.New. Usage:
//
//	c, cleanup := mock.NewClient()
//	defer cleanup()
func (s *Server) NewClient(opts ...api.TransportOption) (api.Client, func()) {
	ts := s.Serve()
	full := make([]api.TransportOption, 0, len(opts)+2)
	full = append(full, opts...)
	if s.tls {
		full = append(full, api.WithInsecureSkipVerify(true))
	}
	if s.httpClient != nil {
		full = append(full, api.WithHTTPClient(s.httpClient))
	}
	c, err := api.New(ts.URL, api.TokenCredentials("root@pam!mock", "mock-secret"), full...)
	if err != nil {
		ts.Close()
		// Unreachable: ts.URL is valid and the credentials are non-nil.
		panic("mockpve: wiring client: " + err.Error())
	}
	return c, ts.Close
}

// SeedVersion sets what GET /version reports. Call before the first request.
func (s *Server) SeedVersion(version, release, repoID string) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	s.st.version = versionData{Version: version, Release: release, RepoID: repoID, Console: "xtermjs"}
}

// AddNode registers a node by name. "pve" is pre-registered by New.
func (s *Server) AddNode(node string) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	if _, ok := s.st.nodes[node]; !ok {
		s.st.nodes[node] = &nodeState{tasks: make(map[string]*taskRecord)}
	}
}

// AddTask registers a running task on node. The caller supplies the full UPID
// string and the other display fields; log may be nil. The node is created if
// it does not exist.
func (s *Server) AddTask(node, upid, taskType, id, user string, log []tasks.LogLine) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	n, ok := s.st.nodes[node]
	if !ok {
		n = &nodeState{tasks: make(map[string]*taskRecord)}
		s.st.nodes[node] = n
	}
	n.tasks[upid] = &taskRecord{
		UPID:      upid,
		Node:      node,
		Type:      taskType,
		ID:        id,
		User:      user,
		StartTime: time.Now(),
		PID:       1000,
		Log:       log,
	}
}

// FinishTask transitions a task to stopped with exitStatus ("OK" for success,
// any other string for failure). It is a no-op if the task is unknown.
func (s *Server) FinishTask(node, upid, exitStatus string) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	if n, ok := s.st.nodes[node]; ok {
		if rec, ok := n.tasks[upid]; ok {
			rec.Stopped = true
			rec.ExitStatus = exitStatus
		}
	}
}

// AddUser registers a username/password pair accepted by POST /access/ticket.
// The minted ticket and CSRF token are deterministic ("mock-ticket-<user>" /
// "mock-csrf-<user>") so tests can assert them.
func (s *Server) AddUser(username, password string) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	s.st.users[username] = password
}

// RegisterHandler mounts an extra handler at pattern (Go 1.22 ServeMux syntax,
// e.g. "GET /api2/json/cluster/status"). This is the extension seam for service
// packages and the corpus loader; call it before serving requests.
func (s *Server) RegisterHandler(pattern string, h http.Handler) {
	s.mux.Handle(pattern, h)
}

// checkAuth accepts a well-formed API token header or a ticket cookie minted by
// this server. On failure it writes a 401 and returns false.
func (s *Server) checkAuth(w http.ResponseWriter, r *http.Request) bool {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "PVEAPIToken=") {
		if strings.Contains(strings.TrimPrefix(auth, "PVEAPIToken="), "=") {
			return true
		}
	}
	if ck, err := r.Cookie("PVEAuthCookie"); err == nil {
		s.st.mu.Lock()
		_, ok := s.st.tickets[ck.Value]
		s.st.mu.Unlock()
		if ok {
			return true
		}
	}
	s.writeError(w, http.StatusUnauthorized, "authentication failure")
	return false
}

// envelope is the PVE response wrapper.
type envelope struct {
	Data    any               `json:"data,omitempty"`
	Message string            `json:"message,omitempty"`
	Errors  map[string]string `json:"errors,omitempty"`
}

// writeData writes {"data": v} with status 200.
func (s *Server) writeData(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(envelope{Data: v}); err != nil {
		s.logger.Debug("mockpve: encode response", "err", err)
	}
}

// writeError writes {"message": msg} with the given status.
func (s *Server) writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(envelope{Message: msg}); err != nil {
		s.logger.Debug("mockpve: encode error", "err", err)
	}
}
