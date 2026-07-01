package access

import "net/url"

// Access REST paths, all under /access. User, group, role, and token IDs can
// contain characters needing escaping (userids carry '@'), so url.PathEscape is
// applied to every path segment.

func usersPath() string { return "/access/users" }

func userPath(userid string) string {
	return usersPath() + "/" + url.PathEscape(userid)
}

func groupsPath() string { return "/access/groups" }

func groupPath(groupid string) string {
	return groupsPath() + "/" + url.PathEscape(groupid)
}

func rolesPath() string { return "/access/roles" }

func rolePath(roleid string) string {
	return rolesPath() + "/" + url.PathEscape(roleid)
}

func aclPath() string { return "/access/acl" }

func tokensPath(userid string) string {
	return userPath(userid) + "/token"
}

func tokenPath(userid, tokenid string) string {
	return tokensPath(userid) + "/" + url.PathEscape(tokenid)
}
