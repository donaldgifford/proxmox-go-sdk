package lab

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"sort"
	"sync"
	"text/template"
	"time"
)

//go:embed answer.toml.tmpl
var answerTemplateText string

var answerTemplate = template.Must(template.New("answer").Parse(answerTemplateText))

// answerData feeds answer.toml.tmpl for one node.
type answerData struct {
	FQDN         string
	CIDR         string
	Gateway      string
	DNS          string
	RootPassword string
}

// RenderAnswer renders the auto-install answer file for one node. The result
// carries the root password: serve it, never persist or log it.
func RenderAnswer(cfg *Config, node Node, rootPassword string) ([]byte, error) {
	var buf bytes.Buffer
	err := answerTemplate.Execute(&buf, answerData{
		FQDN:         node.FQDN(cfg.Nested.Domain),
		CIDR:         node.CIDR,
		Gateway:      cfg.Nested.Gateway,
		DNS:          cfg.Nested.DNS,
		RootPassword: rootPassword,
	})
	if err != nil {
		return nil, fmt.Errorf("render answer for %s: %w", node.Name, err)
	}
	return buf.Bytes(), nil
}

// maxAnswerBody bounds how much of an installer's request body is read.
const maxAnswerBody = 1 << 20

// AnswerServer serves per-node answer files to the nested installers for the
// duration of `pvelab up` (the 2026-07-10 design amendment: one http-mode ISO,
// per-node answers rendered at install time). It matches each installer's
// request to a node by the DMI serial stamped into the VM at create
// (smbios1 serial=<node name>) and implements http.Handler directly.
//
// The exact POST payload shape the assistant sends is a live-verify item, so
// matching is deliberately shape-agnostic: the raw body is scanned for each
// configured node's serial (raw and base64 forms) rather than parsing a
// guessed schema, and every request body is logged at Debug as the
// live-verification instrument.
type AnswerServer struct {
	cfg          *Config
	rootPassword string
	logger       *slog.Logger

	mu     sync.Mutex
	served map[string]time.Time // node name → first answer time (status/debug only).

	srv  *http.Server
	addr net.Addr
	done chan struct{} // closed when the serving goroutine exits.
}

// NewAnswerServer builds a server for cfg's nodes; call Start to serve.
func NewAnswerServer(cfg *Config, rootPassword string, logger *slog.Logger) *AnswerServer {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &AnswerServer{
		cfg:          cfg,
		rootPassword: rootPassword,
		logger:       logger,
		served:       make(map[string]time.Time),
	}
}

// Start binds nested.answer_listen and serves in a background goroutine. A
// bind failure returns synchronously; the routable URL the installers call is
// nested.answer_url (baked into the ISO), which must reach this listener.
func (s *AnswerServer) Start(ctx context.Context) error {
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", s.cfg.Nested.AnswerListen)
	if err != nil {
		return fmt.Errorf("answer server listen %s: %w", s.cfg.Nested.AnswerListen, err)
	}
	s.addr = ln.Addr()
	s.srv = &http.Server{Handler: s, ReadHeaderTimeout: 10 * time.Second}
	s.done = make(chan struct{})
	go func() {
		defer close(s.done)
		if err := s.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("answer server", "err", err)
		}
	}()
	s.logger.Info("answer server listening", "addr", s.addr.String(), "answer_url", s.cfg.Nested.AnswerURL)
	return nil
}

// Addr is the bound listen address (useful when answer_listen ends in :0).
func (s *AnswerServer) Addr() string {
	if s.addr == nil {
		return ""
	}
	return s.addr.String()
}

// Shutdown gracefully stops the server, bounded by ctx, and waits for the
// serving goroutine to exit — no goroutine outlives the call.
func (s *AnswerServer) Shutdown(ctx context.Context) error {
	if s.srv == nil {
		return nil
	}
	err := s.srv.Shutdown(ctx)
	select {
	case <-s.done:
	case <-ctx.Done():
		return errors.Join(err, ctx.Err())
	}
	return err
}

// Served reports which nodes have fetched an answer (first-served times).
// Debug/status visibility only — readiness is measured by polling each node's
// /version, not by answer delivery.
func (s *AnswerServer) Served() map[string]time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]time.Time, len(s.served))
	maps.Copy(out, s.served)
	return out
}

// ServeHTTP answers any method/path: it reads the (bounded) body, scans it
// for a configured node's serial, and returns that node's rendered
// answer.toml. A GET may instead identify itself via a ?serial= query
// parameter (manual/debug fallback). Unmatched requests get 404.
func (s *AnswerServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxAnswerBody))
	if err != nil {
		s.logger.Warn("answer request body read failed", "remote", r.RemoteAddr, "err", err)
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	s.logger.Debug("answer request", "method", r.Method, "path", r.URL.Path,
		"remote", r.RemoteAddr, "body", string(body))

	node, ok := s.matchNode(body, r.URL.Query().Get("serial"))
	if !ok {
		s.logger.Warn("answer request matched no configured node",
			"remote", r.RemoteAddr, "body", string(truncateBytes(body, 256)))
		http.Error(w, "no matching node", http.StatusNotFound)
		return
	}

	answer, err := RenderAnswer(s.cfg, node, s.rootPassword)
	if err != nil {
		s.logger.Error("render answer", "node", node.Name, "err", err)
		http.Error(w, "render answer", http.StatusInternalServerError)
		return
	}

	s.mu.Lock()
	if _, seen := s.served[node.Name]; !seen {
		s.served[node.Name] = time.Now()
	}
	s.mu.Unlock()

	s.logger.Info("served answer", "node", node.Name, "remote", r.RemoteAddr)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if _, err := w.Write(answer); err != nil {
		s.logger.Debug("write answer", "node", node.Name, "err", err)
	}
}

// matchNode picks the configured node whose serial (the node name, raw or
// base64 — provision stamps it base64-encoded into smbios1) appears in the
// request body or the ?serial= fallback. Longest name wins so one node name
// being a prefix of another cannot misroute.
func (s *AnswerServer) matchNode(body []byte, serialParam string) (Node, bool) {
	nodes := make([]Node, len(s.cfg.Nested.Nodes))
	copy(nodes, s.cfg.Nested.Nodes)
	sort.Slice(nodes, func(i, j int) bool { return len(nodes[i].Name) > len(nodes[j].Name) })

	for _, n := range nodes {
		if serialParam == n.Name {
			return n, true
		}
		if bytes.Contains(body, []byte(n.Name)) {
			return n, true
		}
		if bytes.Contains(body, []byte(base64.StdEncoding.EncodeToString([]byte(n.Name)))) {
			return n, true
		}
	}
	return Node{}, false
}

// truncateBytes caps b at n bytes for log lines.
func truncateBytes(b []byte, n int) []byte {
	if len(b) > n {
		return b[:n]
	}
	return b
}
