package ha

import "net/url"

// All HA endpoints are cluster-scoped under /cluster/ha. SIDs such as "vm:100"
// carry a colon, so they are percent-escaped as path segments (url.PathEscape
// encodes ":" as %3A); Go's ServeMux {sid} + PathValue round-trips them.

func haResourcesPath() string { return "/cluster/ha/resources" }

func haResourcePath(sid string) string {
	return haResourcesPath() + "/" + url.PathEscape(sid)
}

func haRulesPath() string { return "/cluster/ha/rules" }

func haRulePath(rule string) string {
	return haRulesPath() + "/" + url.PathEscape(rule)
}
