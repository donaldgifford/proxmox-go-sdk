package lxc

import "strconv"

// lxcPath returns /nodes/{node}/lxc — the list and create endpoint.
func (s *Service) lxcPath() string {
	return "/nodes/" + s.node + "/lxc"
}

// ctPath returns /nodes/{node}/lxc/{vmid} — the base for per-container endpoints.
func (s *Service) ctPath(vmid int) string {
	return s.lxcPath() + "/" + strconv.Itoa(vmid)
}
