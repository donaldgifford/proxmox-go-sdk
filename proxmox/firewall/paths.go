package firewall

import (
	"net/url"
	"strconv"
)

// Firewall REST paths, all built on the Service's scope prefix (scope.path), so
// the cluster/node/guest split lives in exactly one place. IPSet names and CIDR
// entries are url.PathEscape'd (a CIDR carries a '/').

func (s *Service) rulesPath() string { return s.scope.path() + "/rules" }

func (s *Service) rulePath(pos int) string {
	return s.rulesPath() + "/" + strconv.Itoa(pos)
}

func (s *Service) ipsetsPath() string { return s.scope.path() + "/ipset" }

func (s *Service) ipsetPath(name string) string {
	return s.ipsetsPath() + "/" + url.PathEscape(name)
}

func (s *Service) ipsetEntryPath(name, cidr string) string {
	return s.ipsetPath(name) + "/" + url.PathEscape(cidr)
}

func (s *Service) optionsPath() string { return s.scope.path() + "/options" }
