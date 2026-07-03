package qemu

import "strconv"

// qemuPath returns /nodes/{node}/qemu — the list and create endpoint.
func (s *Service) qemuPath() string {
	return "/nodes/" + s.node + "/qemu"
}

// vmPath returns /nodes/{node}/qemu/{vmid} — the base for per-VM endpoints.
func (s *Service) vmPath(vmid int) string {
	return s.qemuPath() + "/" + strconv.Itoa(vmid)
}
