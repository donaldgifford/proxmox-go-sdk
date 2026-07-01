package mockpve

import (
	"net/http"
	"strconv"
)

// This file models the metrics surface (task 5): node/guest RRD series and
// status (synthesized static), plus cluster-scoped external metric servers.

// metricsState is the metrics slice of the mock model, embedded in state and
// guarded by state.mu.
type metricsState struct {
	servers map[string]*metricServerRecord // keyed by id.
}

type metricServerRecord struct {
	ID      string
	Type    string
	Server  string
	Port    int
	Disable bool
}

type metricServerPayload struct {
	ID      string `json:"id"`
	Type    string `json:"type,omitempty"`
	Server  string `json:"server,omitempty"`
	Port    int    `json:"port,omitempty"`
	Disable int    `json:"disable,omitempty"`
}

type rrdPointPayload struct {
	Time   int64   `json:"time"`
	CPU    float64 `json:"cpu"`
	MaxCPU float64 `json:"maxcpu"`
	Mem    float64 `json:"mem"`
	MaxMem float64 `json:"maxmem"`
	NetIn  float64 `json:"netin"`
	NetOut float64 `json:"netout"`
}

type memoryInfoPayload struct {
	Total int64 `json:"total"`
	Used  int64 `json:"used"`
	Free  int64 `json:"free"`
}

type nodeStatusPayload struct {
	Uptime     int64             `json:"uptime"`
	CPU        float64           `json:"cpu"`
	Wait       float64           `json:"wait"`
	LoadAvg    []string          `json:"loadavg"`
	KVersion   string            `json:"kversion"`
	PVEVersion string            `json:"pveversion"`
	Memory     memoryInfoPayload `json:"memory"`
}

// AddMetricServer seeds an external metric server. Call before serving.
func (s *Server) AddMetricServer(id, serverType, host string, port int) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	if s.st.metrics.servers == nil {
		s.st.metrics.servers = make(map[string]*metricServerRecord)
	}
	s.st.metrics.servers[id] = &metricServerRecord{ID: id, Type: serverType, Server: host, Port: port}
}

func (s *Server) registerMetricsRoutes() {
	s.mux.HandleFunc("GET /api2/json/nodes/{node}/rrddata", s.handleNodeRRD)
	s.mux.HandleFunc("GET /api2/json/nodes/{node}/status", s.handleNodeStatus)
	s.mux.HandleFunc("GET /api2/json/nodes/{node}/qemu/{vmid}/rrddata", s.handleVMRRD)
	s.mux.HandleFunc("GET /api2/json/nodes/{node}/lxc/{vmid}/rrddata", s.handleVMRRD)
	s.mux.HandleFunc("GET /api2/json/cluster/metrics/server", s.handleMetricServerList)
	s.mux.HandleFunc("GET /api2/json/cluster/metrics/server/{id}", s.handleMetricServerGet)
	s.mux.HandleFunc("POST /api2/json/cluster/metrics/server/{id}", s.handleMetricServerCreate)
	s.mux.HandleFunc("PUT /api2/json/cluster/metrics/server/{id}", s.handleMetricServerUpdate)
	s.mux.HandleFunc("DELETE /api2/json/cluster/metrics/server/{id}", s.handleMetricServerDelete)
}

// syntheticRRD returns a fixed two-point series so RRD reads are deterministic.
func syntheticRRD() []rrdPointPayload {
	return []rrdPointPayload{
		{Time: 1719792000, CPU: 0.05, MaxCPU: 4, Mem: 2 << 30, MaxMem: 8 << 30, NetIn: 1000, NetOut: 2000},
		{Time: 1719792060, CPU: 0.07, MaxCPU: 4, Mem: 2 << 30, MaxMem: 8 << 30, NetIn: 1100, NetOut: 2200},
	}
}

func (s *Server) handleNodeRRD(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.writeData(w, syntheticRRD())
}

func (s *Server) handleVMRRD(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.writeData(w, syntheticRRD())
}

func (s *Server) handleNodeStatus(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.writeData(w, nodeStatusPayload{
		Uptime: 123456, CPU: 0.05, Wait: 0.01,
		LoadAvg: []string{"0.00", "0.01", "0.05"}, KVersion: "Linux 6.14",
		PVEVersion: "pve-manager/9.0.3", Memory: memoryInfoPayload{Total: 8 << 30, Used: 2 << 30, Free: 6 << 30},
	})
}

func (s *Server) handleMetricServerList(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.st.mu.Lock()
	out := make([]metricServerPayload, 0, len(s.st.metrics.servers))
	for _, rec := range s.st.metrics.servers {
		out = append(out, metricServerToPayload(rec))
	}
	s.st.mu.Unlock()
	s.writeData(w, out)
}

func (s *Server) handleMetricServerGet(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	id := r.PathValue("id")
	s.st.mu.Lock()
	rec := s.st.metrics.servers[id]
	var payload metricServerPayload
	if rec != nil {
		payload = metricServerToPayload(rec)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchMetricServer)
		return
	}
	s.writeData(w, payload)
}

func (s *Server) handleMetricServerCreate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	if !s.parseForm(w, r) {
		return
	}
	id := r.PathValue("id")
	var port int
	if v := r.PostForm.Get("port"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			port = p
		}
	}
	rec := &metricServerRecord{
		ID: id, Type: r.PostForm.Get("type"), Server: r.PostForm.Get("server"),
		Port: port, Disable: r.PostForm.Get("disable") == "1",
	}
	s.st.mu.Lock()
	if s.st.metrics.servers == nil {
		s.st.metrics.servers = make(map[string]*metricServerRecord)
	}
	s.st.metrics.servers[id] = rec
	s.st.mu.Unlock()
	s.writeData(w, nil)
}

func (s *Server) handleMetricServerUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	if !s.parseForm(w, r) {
		return
	}
	id := r.PathValue("id")
	s.st.mu.Lock()
	rec := s.st.metrics.servers[id]
	if rec != nil {
		applyMetricServerForm(rec, r)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchMetricServer)
		return
	}
	s.writeData(w, nil)
}

func applyMetricServerForm(rec *metricServerRecord, r *http.Request) {
	if v := r.PostForm.Get("server"); v != "" {
		rec.Server = v
	}
	if v := r.PostForm.Get("port"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			rec.Port = p
		}
	}
	if v := r.PostForm.Get("disable"); v != "" {
		rec.Disable = v == "1"
	}
}

func (s *Server) handleMetricServerDelete(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	id := r.PathValue("id")
	s.st.mu.Lock()
	rec := s.st.metrics.servers[id]
	if rec != nil {
		delete(s.st.metrics.servers, id)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchMetricServer)
		return
	}
	s.writeData(w, nil)
}

func metricServerToPayload(rec *metricServerRecord) metricServerPayload {
	return metricServerPayload{
		ID: rec.ID, Type: rec.Type, Server: rec.Server,
		Port: rec.Port, Disable: boolToInt(rec.Disable),
	}
}
