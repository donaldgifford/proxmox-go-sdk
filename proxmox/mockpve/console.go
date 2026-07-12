package mockpve

import (
	"crypto/sha1" //nolint:gosec // SHA-1 is mandated by the WebSocket handshake (RFC 6455), not used for security.
	"encoding/base64"
	"io"
	"net/http"
)

// This file models the PVE console surface (task 8): minting VNC/SPICE/terminal
// tickets and the vncwebsocket dial. Ticket mints are plain JSON responses.
// The vncwebsocket route performs a real 101 WebSocket upgrade (hijack) and then
// echoes bytes, which is enough to exercise the SDK's Connect duplex plumbing;
// the live PVE RFB payload is not modelled.

// wsHandshakeMagic is the RFC 6455 handshake GUID.
const wsHandshakeMagic = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// consoleState is the console slice of the mock model, embedded in state and
// guarded by state.mu. It records the VNC tickets minted so far — each bound
// to the vncwebsocket path allowed to present it, mirroring real PVE (a guest
// ticket at the node-shell path is a 401; found live 2026-07-12) — so the
// upgrade rejects unknown AND misrouted tickets.
type consoleState struct {
	vncTickets map[string]string // vncticket value → required dial path.
}

func (s *Server) registerConsoleRoutes() {
	// Guest consoles. The kind (qemu/lxc) is a literal path segment — a {kind}
	// wildcard would conflict with the sibling /nodes/{node}/firewall/... routes,
	// so register each kind explicitly and close over it.
	for _, kind := range []string{"qemu", "lxc"} {
		base := "/api2/json/nodes/{node}/" + kind + "/{vmid}/"
		s.mux.HandleFunc("POST "+base+"vncproxy", func(w http.ResponseWriter, r *http.Request) {
			s.handleGuestVNCProxy(w, r, kind)
		})
		s.mux.HandleFunc("POST "+base+"spiceproxy", s.handleGuestSpiceProxy)
		s.mux.HandleFunc("POST "+base+"termproxy", func(w http.ResponseWriter, r *http.Request) {
			s.handleGuestTermProxy(w, r, kind)
		})
		// The guest dial — a guest ticket must be presented HERE, not at the
		// node-shell path (real PVE binds the ticket to its mint surface).
		s.mux.HandleFunc("GET "+base+"vncwebsocket", s.handleVNCWebSocket)
	}
	// Node shell consoles.
	s.mux.HandleFunc("POST /api2/json/nodes/{node}/vncshell", s.handleNodeVNCShell)
	s.mux.HandleFunc("POST /api2/json/nodes/{node}/termproxy", s.handleNodeTermProxy)
	// The node-shell dial.
	s.mux.HandleFunc("GET /api2/json/nodes/{node}/vncwebsocket", s.handleVNCWebSocket)
}

// mintVNCTicket records a deterministic VNC ticket for node/id, bound to the
// vncwebsocket path allowed to present it, and returns the wire payload
// (ticket, proxy port, user, cert, and the proxy worker UPID).
func (s *Server) mintVNCTicket(node, id, dialPath string) map[string]any {
	ticket := "mock-vncticket-" + node + "-" + id
	s.st.mu.Lock()
	if s.st.console.vncTickets == nil {
		s.st.console.vncTickets = make(map[string]string)
	}
	s.st.console.vncTickets[ticket] = dialPath
	s.st.mu.Unlock()
	return map[string]any{
		"ticket": ticket,
		"port":   "5900",
		"user":   mockTaskUser,
		"cert":   "MOCKCERT",
		"upid":   synthUPID(node, "vncproxy", id),
	}
}

func (s *Server) handleGuestVNCProxy(w http.ResponseWriter, r *http.Request, kind string) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	vmid := r.PathValue("vmid")
	dial := "/api2/json/nodes/" + node + "/" + kind + "/" + vmid + "/vncwebsocket"
	s.writeData(w, s.mintVNCTicket(node, kind+"-"+vmid, dial))
}

func (s *Server) handleNodeVNCShell(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	s.writeData(w, s.mintVNCTicket(node, "shell", "/api2/json/nodes/"+node+"/vncwebsocket"))
}

func (s *Server) handleGuestTermProxy(w http.ResponseWriter, r *http.Request, kind string) {
	if !s.checkAuth(w, r) {
		return
	}
	node := r.PathValue("node")
	id := kind + "-" + r.PathValue("vmid")
	s.writeData(w, termTicketPayload(node, id))
}

func (s *Server) handleNodeTermProxy(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.writeData(w, termTicketPayload(r.PathValue("node"), "shell"))
}

func termTicketPayload(node, id string) map[string]any {
	return map[string]any{
		"ticket": "mock-termticket-" + node + "-" + id,
		"port":   "5901",
		"user":   mockTaskUser,
		"upid":   synthUPID(node, "termproxy", id),
	}
}

func (s *Server) handleGuestSpiceProxy(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	// release-cursor is an unmodelled key, so the SDK routes it into
	// SPICETicket.Extra — exercising the lossless decode.
	//nolint:gosec // G101: the fixed SPICE password below is a static mock fixture, not a real credential.
	s.writeData(w, map[string]any{
		"host":           "10.0.0.1",
		"proxy":          r.PathValue("node"),
		"tls-port":       61000,
		"password":       "mock-spice-pw",
		"type":           "spice",
		"title":          "guest console",
		"release-cursor": "1",
	})
}

// handleVNCWebSocket verifies the vncticket — including that it is presented
// at the path it was minted for, like real PVE — performs a 101 WebSocket
// upgrade by hijacking the connection, and then echoes every byte the client
// writes.
func (s *Server) handleVNCWebSocket(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	ticket := r.URL.Query().Get("vncticket")
	s.st.mu.Lock()
	dialPath, ok := s.st.console.vncTickets[ticket]
	s.st.mu.Unlock()
	if !ok || dialPath != r.URL.Path {
		s.writeError(w, http.StatusUnauthorized, msgBadVNCTicket)
		return
	}

	hj, canHijack := w.(http.Hijacker)
	if !canHijack {
		s.writeError(w, http.StatusInternalServerError, "connection not hijackable")
		return
	}
	conn, buf, err := hj.Hijack()
	if err != nil {
		s.logger.Debug("mockpve: hijack for vnc upgrade", "err", err)
		return
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			s.logger.Debug("mockpve: close hijacked conn", "err", cerr)
		}
	}()

	accept := wsAcceptKey(r.Header.Get("Sec-WebSocket-Key"))
	if _, err := buf.WriteString("HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"); err != nil {
		s.logger.Debug("mockpve: write vnc handshake", "err", err)
		return
	}
	if err := buf.Flush(); err != nil {
		s.logger.Debug("mockpve: flush vnc handshake", "err", err)
		return
	}
	if _, err := io.Copy(conn, buf); err != nil { // echo until the client closes.
		s.logger.Debug("mockpve: vnc echo copy", "err", err)
	}
}

// wsAcceptKey computes the RFC 6455 Sec-WebSocket-Accept for a client key.
func wsAcceptKey(key string) string {
	sum := sha1.Sum([]byte(key + wsHandshakeMagic)) //nolint:gosec // see import note.
	return base64.StdEncoding.EncodeToString(sum[:])
}
