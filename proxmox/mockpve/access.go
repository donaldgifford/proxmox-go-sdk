package mockpve

import (
	"net/http"
	"strconv"
	"strings"
)

// accessState is the access-control slice of the mock model, embedded in state
// and guarded by state.mu. It is SEPARATE from state.users (which holds the
// username→password map for ticket auth); this models the /access management
// surface (users, groups, roles, ACLs, tokens).
type accessState struct {
	users  map[string]*accessUserRecord
	groups map[string]*accessGroupRecord
	roles  map[string]*accessRoleRecord
	acls   []accessACLRecord
	tokens map[string]map[string]*accessTokenRecord // userid -> tokenid.
}

type accessUserRecord struct {
	UserID    string
	Email     string
	Comment   string
	FirstName string
	LastName  string
	Enable    bool
	Expire    int64
	Groups    []string
}

type accessGroupRecord struct {
	GroupID string
	Comment string
	Members []string
}

type accessRoleRecord struct {
	RoleID  string
	Privs   []string
	Special bool
}

type accessACLRecord struct {
	Path      string
	RoleID    string
	Type      string
	UGID      string
	Propagate bool
}

type accessTokenRecord struct {
	TokenID string
	Comment string
	Expire  int64
	Privsep bool
}

type accessUserPayload struct {
	UserID    string   `json:"userid"`
	Enable    int      `json:"enable"`
	Expire    int64    `json:"expire,omitempty"`
	FirstName string   `json:"firstname,omitempty"`
	LastName  string   `json:"lastname,omitempty"`
	Email     string   `json:"email,omitempty"`
	Comment   string   `json:"comment,omitempty"`
	Groups    []string `json:"groups,omitempty"`
}

type accessGroupPayload struct {
	GroupID string   `json:"groupid"`
	Comment string   `json:"comment,omitempty"`
	Members []string `json:"members,omitempty"`
}

type accessRolePayload struct {
	RoleID  string `json:"roleid"`
	Privs   string `json:"privs,omitempty"`
	Special int    `json:"special,omitempty"`
}

type accessACLPayload struct {
	Path      string `json:"path"`
	RoleID    string `json:"roleid"`
	Type      string `json:"type"`
	UGID      string `json:"ugid"`
	Propagate int    `json:"propagate,omitempty"`
}

type accessTokenPayload struct {
	TokenID string `json:"tokenid"`
	Comment string `json:"comment,omitempty"`
	Expire  int64  `json:"expire,omitempty"`
	Privsep int    `json:"privsep,omitempty"`
}

// AddAccessUser seeds a user in the access-management model. Call before serving.
func (s *Server) AddAccessUser(userid string) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	if s.st.access.users == nil {
		s.st.access.users = make(map[string]*accessUserRecord)
	}
	s.st.access.users[userid] = &accessUserRecord{UserID: userid, Enable: true}
}

// AddGroup seeds a group. Call before serving.
func (s *Server) AddGroup(groupid string) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	if s.st.access.groups == nil {
		s.st.access.groups = make(map[string]*accessGroupRecord)
	}
	s.st.access.groups[groupid] = &accessGroupRecord{GroupID: groupid}
}

// AddRole seeds a role with the given privileges. Call before serving.
func (s *Server) AddRole(roleid string, privs []string) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	if s.st.access.roles == nil {
		s.st.access.roles = make(map[string]*accessRoleRecord)
	}
	s.st.access.roles[roleid] = &accessRoleRecord{RoleID: roleid, Privs: privs}
}

// AddACL seeds an ACL entry. Call before serving.
func (s *Server) AddACL(path, ugid, roleid string, propagate bool) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	s.st.access.acls = append(s.st.access.acls, accessACLRecord{
		Path: path, UGID: ugid, RoleID: roleid, Type: "user", Propagate: propagate,
	})
}

// AddToken seeds an API token for a user. Call before serving.
func (s *Server) AddToken(userid, tokenid string) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	s.ensureTokenUserLocked(userid)
	s.st.access.tokens[userid][tokenid] = &accessTokenRecord{TokenID: tokenid}
}

// ensureTokenUserLocked makes the token maps ready for userid. Caller holds mu.
func (s *Server) ensureTokenUserLocked(userid string) {
	if s.st.access.tokens == nil {
		s.st.access.tokens = make(map[string]map[string]*accessTokenRecord)
	}
	if s.st.access.tokens[userid] == nil {
		s.st.access.tokens[userid] = make(map[string]*accessTokenRecord)
	}
}

func (s *Server) registerAccessRoutes() {
	s.mux.HandleFunc("GET /api2/json/access/users", s.handleUserList)
	s.mux.HandleFunc("POST /api2/json/access/users", s.handleUserCreate)
	s.mux.HandleFunc("GET /api2/json/access/users/{userid}", s.handleUserGet)
	s.mux.HandleFunc("PUT /api2/json/access/users/{userid}", s.handleUserUpdate)
	s.mux.HandleFunc("DELETE /api2/json/access/users/{userid}", s.handleUserDelete)
	s.mux.HandleFunc("GET /api2/json/access/groups", s.handleGroupList)
	s.mux.HandleFunc("POST /api2/json/access/groups", s.handleGroupCreate)
	s.mux.HandleFunc("GET /api2/json/access/groups/{groupid}", s.handleGroupGet)
	s.mux.HandleFunc("PUT /api2/json/access/groups/{groupid}", s.handleGroupUpdate)
	s.mux.HandleFunc("DELETE /api2/json/access/groups/{groupid}", s.handleGroupDelete)
	s.mux.HandleFunc("GET /api2/json/access/roles", s.handleRoleList)
	s.mux.HandleFunc("POST /api2/json/access/roles", s.handleRoleCreate)
	s.mux.HandleFunc("GET /api2/json/access/roles/{roleid}", s.handleRoleGet)
	s.mux.HandleFunc("PUT /api2/json/access/roles/{roleid}", s.handleRoleUpdate)
	s.mux.HandleFunc("DELETE /api2/json/access/roles/{roleid}", s.handleRoleDelete)
	s.mux.HandleFunc("GET /api2/json/access/acl", s.handleACLList)
	s.mux.HandleFunc("PUT /api2/json/access/acl", s.handleACLSet)
	s.mux.HandleFunc("GET /api2/json/access/users/{userid}/token", s.handleTokenList)
	s.mux.HandleFunc("GET /api2/json/access/users/{userid}/token/{tokenid}", s.handleTokenGet)
	s.mux.HandleFunc("POST /api2/json/access/users/{userid}/token/{tokenid}", s.handleTokenCreate)
	s.mux.HandleFunc("PUT /api2/json/access/users/{userid}/token/{tokenid}", s.handleTokenUpdate)
	s.mux.HandleFunc("DELETE /api2/json/access/users/{userid}/token/{tokenid}", s.handleTokenDelete)
}

func (s *Server) handleUserList(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.st.mu.Lock()
	out := make([]accessUserPayload, 0, len(s.st.access.users))
	for _, rec := range s.st.access.users {
		out = append(out, accessUserToPayload(rec))
	}
	s.st.mu.Unlock()
	s.writeData(w, out)
}

func (s *Server) handleUserGet(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	userid := r.PathValue("userid")
	s.st.mu.Lock()
	rec := s.st.access.users[userid]
	var payload accessUserPayload
	if rec != nil {
		payload = accessUserToPayload(rec)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchUser)
		return
	}
	s.writeData(w, payload)
}

func (s *Server) handleUserCreate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	if !s.parseForm(w, r) {
		return
	}
	userid := r.PostForm.Get("userid")
	if userid == "" {
		s.writeError(w, http.StatusBadRequest, "missing userid")
		return
	}
	rec := &accessUserRecord{
		UserID: userid, Email: r.PostForm.Get("email"), Comment: r.PostForm.Get("comment"),
		FirstName: r.PostForm.Get("firstname"), LastName: r.PostForm.Get("lastname"),
		Enable: r.PostForm.Get("enable") != "0",
	}
	if g := r.PostForm.Get("groups"); g != "" {
		rec.Groups = strings.Split(g, ",")
	}
	s.st.mu.Lock()
	if s.st.access.users == nil {
		s.st.access.users = make(map[string]*accessUserRecord)
	}
	s.st.access.users[userid] = rec
	s.st.mu.Unlock()
	s.writeData(w, nil)
}

func (s *Server) handleUserUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	if !s.parseForm(w, r) {
		return
	}
	userid := r.PathValue("userid")
	s.st.mu.Lock()
	rec := s.st.access.users[userid]
	if rec != nil {
		applyUserForm(rec, r)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchUser)
		return
	}
	s.writeData(w, nil)
}

func applyUserForm(rec *accessUserRecord, r *http.Request) {
	if v := r.PostForm.Get("email"); v != "" {
		rec.Email = v
	}
	if v := r.PostForm.Get("comment"); v != "" {
		rec.Comment = v
	}
	if v := r.PostForm.Get("groups"); v != "" {
		rec.Groups = strings.Split(v, ",")
	}
	if v := r.PostForm.Get("enable"); v != "" {
		rec.Enable = v == "1"
	}
}

func (s *Server) handleUserDelete(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	userid := r.PathValue("userid")
	s.st.mu.Lock()
	_, found := s.st.access.users[userid]
	if found {
		delete(s.st.access.users, userid)
	}
	s.st.mu.Unlock()
	if !found {
		s.writeError(w, http.StatusNotFound, msgNoSuchUser)
		return
	}
	s.writeData(w, nil)
}

func accessUserToPayload(rec *accessUserRecord) accessUserPayload {
	return accessUserPayload{
		UserID: rec.UserID, Enable: boolToInt(rec.Enable), Expire: rec.Expire,
		FirstName: rec.FirstName, LastName: rec.LastName, Email: rec.Email,
		Comment: rec.Comment, Groups: rec.Groups,
	}
}

func (s *Server) handleGroupList(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.st.mu.Lock()
	out := make([]accessGroupPayload, 0, len(s.st.access.groups))
	for _, rec := range s.st.access.groups {
		out = append(out, accessGroupPayload{GroupID: rec.GroupID, Comment: rec.Comment, Members: rec.Members})
	}
	s.st.mu.Unlock()
	s.writeData(w, out)
}

func (s *Server) handleGroupGet(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	groupid := r.PathValue("groupid")
	s.st.mu.Lock()
	rec := s.st.access.groups[groupid]
	var payload accessGroupPayload
	if rec != nil {
		payload = accessGroupPayload{GroupID: rec.GroupID, Comment: rec.Comment, Members: rec.Members}
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchGroup)
		return
	}
	s.writeData(w, payload)
}

func (s *Server) handleGroupCreate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	if !s.parseForm(w, r) {
		return
	}
	groupid := r.PostForm.Get("groupid")
	if groupid == "" {
		s.writeError(w, http.StatusBadRequest, "missing groupid")
		return
	}
	s.st.mu.Lock()
	if s.st.access.groups == nil {
		s.st.access.groups = make(map[string]*accessGroupRecord)
	}
	s.st.access.groups[groupid] = &accessGroupRecord{GroupID: groupid, Comment: r.PostForm.Get("comment")}
	s.st.mu.Unlock()
	s.writeData(w, nil)
}

func (s *Server) handleGroupUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	if !s.parseForm(w, r) {
		return
	}
	groupid := r.PathValue("groupid")
	s.st.mu.Lock()
	rec := s.st.access.groups[groupid]
	if rec != nil {
		if v := r.PostForm.Get("comment"); v != "" {
			rec.Comment = v
		}
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchGroup)
		return
	}
	s.writeData(w, nil)
}

func (s *Server) handleGroupDelete(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	groupid := r.PathValue("groupid")
	s.st.mu.Lock()
	_, found := s.st.access.groups[groupid]
	if found {
		delete(s.st.access.groups, groupid)
	}
	s.st.mu.Unlock()
	if !found {
		s.writeError(w, http.StatusNotFound, msgNoSuchGroup)
		return
	}
	s.writeData(w, nil)
}

func (s *Server) handleRoleList(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.st.mu.Lock()
	out := make([]accessRolePayload, 0, len(s.st.access.roles))
	for _, rec := range s.st.access.roles {
		out = append(out, accessRolePayload{
			RoleID: rec.RoleID, Privs: strings.Join(rec.Privs, ","), Special: boolToInt(rec.Special),
		})
	}
	s.st.mu.Unlock()
	s.writeData(w, out)
}

// handleRoleGet returns the single-role shape: a privilege→1 object.
func (s *Server) handleRoleGet(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	roleid := r.PathValue("roleid")
	s.st.mu.Lock()
	rec := s.st.access.roles[roleid]
	var privMap map[string]int
	if rec != nil {
		privMap = make(map[string]int, len(rec.Privs))
		for _, p := range rec.Privs {
			privMap[p] = 1
		}
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchRole)
		return
	}
	s.writeData(w, privMap)
}

func (s *Server) handleRoleCreate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	if !s.parseForm(w, r) {
		return
	}
	roleid := r.PostForm.Get("roleid")
	if roleid == "" {
		s.writeError(w, http.StatusBadRequest, "missing roleid")
		return
	}
	rec := &accessRoleRecord{RoleID: roleid}
	if p := r.PostForm.Get("privs"); p != "" {
		rec.Privs = strings.Split(p, ",")
	}
	s.st.mu.Lock()
	if s.st.access.roles == nil {
		s.st.access.roles = make(map[string]*accessRoleRecord)
	}
	s.st.access.roles[roleid] = rec
	s.st.mu.Unlock()
	s.writeData(w, nil)
}

func (s *Server) handleRoleUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	if !s.parseForm(w, r) {
		return
	}
	roleid := r.PathValue("roleid")
	s.st.mu.Lock()
	rec := s.st.access.roles[roleid]
	if rec != nil {
		applyRoleForm(rec, r)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchRole)
		return
	}
	s.writeData(w, nil)
}

func applyRoleForm(rec *accessRoleRecord, r *http.Request) {
	p := r.PostForm.Get("privs")
	if p == "" {
		return
	}
	privs := strings.Split(p, ",")
	if r.PostForm.Get("append") == "1" {
		rec.Privs = append(rec.Privs, privs...)
		return
	}
	rec.Privs = privs
}

func (s *Server) handleRoleDelete(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	roleid := r.PathValue("roleid")
	s.st.mu.Lock()
	_, found := s.st.access.roles[roleid]
	if found {
		delete(s.st.access.roles, roleid)
	}
	s.st.mu.Unlock()
	if !found {
		s.writeError(w, http.StatusNotFound, msgNoSuchRole)
		return
	}
	s.writeData(w, nil)
}

func (s *Server) handleACLList(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.st.mu.Lock()
	out := make([]accessACLPayload, 0, len(s.st.access.acls))
	for _, rec := range s.st.access.acls {
		out = append(out, accessACLPayload{
			Path: rec.Path, RoleID: rec.RoleID, Type: rec.Type,
			UGID: rec.UGID, Propagate: boolToInt(rec.Propagate),
		})
	}
	s.st.mu.Unlock()
	s.writeData(w, out)
}

// handleACLSet grants or (with delete=1) revokes ACL entries. Synchronous.
func (s *Server) handleACLSet(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	if !s.parseForm(w, r) {
		return
	}
	path := r.PostForm.Get("path")
	roles := splitCSV(r.PostForm.Get("roles"))
	if path == "" || len(roles) == 0 {
		s.writeError(w, http.StatusBadRequest, "missing path or roles")
		return
	}
	remove := r.PostForm.Get("delete") == "1"
	propagate := r.PostForm.Get("propagate") != "0"
	subjects := aclSubjects(r)

	s.st.mu.Lock()
	for _, subj := range subjects {
		for _, role := range roles {
			if remove {
				s.st.access.acls = removeACL(s.st.access.acls, path, subj.ugid, role)
				continue
			}
			s.st.access.acls = append(s.st.access.acls, accessACLRecord{
				Path: path, UGID: subj.ugid, RoleID: role, Type: subj.kind, Propagate: propagate,
			})
		}
	}
	s.st.mu.Unlock()
	s.writeData(w, nil)
}

type aclSubject struct {
	ugid string
	kind string
}

// aclSubjects flattens the users/groups/tokens form params into typed subjects.
func aclSubjects(r *http.Request) []aclSubject {
	users := splitCSV(r.PostForm.Get("users"))
	groups := splitCSV(r.PostForm.Get("groups"))
	tokens := splitCSV(r.PostForm.Get("tokens"))
	out := make([]aclSubject, 0, len(users)+len(groups)+len(tokens))
	for _, u := range users {
		out = append(out, aclSubject{ugid: u, kind: "user"})
	}
	for _, g := range groups {
		out = append(out, aclSubject{ugid: g, kind: "group"})
	}
	for _, tok := range tokens {
		out = append(out, aclSubject{ugid: tok, kind: "token"})
	}
	return out
}

func removeACL(acls []accessACLRecord, path, ugid, role string) []accessACLRecord {
	out := acls[:0]
	for _, a := range acls {
		if a.Path == path && a.UGID == ugid && a.RoleID == role {
			continue
		}
		out = append(out, a)
	}
	return out
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func (s *Server) handleTokenList(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	userid := r.PathValue("userid")
	s.st.mu.Lock()
	toks := s.st.access.tokens[userid]
	out := make([]accessTokenPayload, 0, len(toks))
	for _, rec := range toks {
		out = append(out, tokenToPayload(rec))
	}
	s.st.mu.Unlock()
	s.writeData(w, out)
}

func (s *Server) handleTokenGet(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	userid := r.PathValue("userid")
	tokenid := r.PathValue("tokenid")
	s.st.mu.Lock()
	rec := s.lookupTokenLocked(userid, tokenid)
	var payload accessTokenPayload
	if rec != nil {
		payload = tokenToPayload(rec)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchToken)
		return
	}
	s.writeData(w, payload)
}

func (s *Server) lookupTokenLocked(userid, tokenid string) *accessTokenRecord {
	if s.st.access.tokens[userid] == nil {
		return nil
	}
	return s.st.access.tokens[userid][tokenid]
}

// handleTokenCreate mints a token and returns its one-time secret. Synchronous.
func (s *Server) handleTokenCreate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	if !s.parseForm(w, r) {
		return
	}
	userid := r.PathValue("userid")
	tokenid := r.PathValue("tokenid")
	rec := &accessTokenRecord{
		TokenID: tokenid, Comment: r.PostForm.Get("comment"),
		Privsep: r.PostForm.Get("privsep") == "1",
	}
	if v, err := strconv.ParseInt(r.PostForm.Get("expire"), 10, 64); err == nil {
		rec.Expire = v
	}
	s.st.mu.Lock()
	s.ensureTokenUserLocked(userid)
	s.st.access.tokens[userid][tokenid] = rec
	s.st.mu.Unlock()
	s.writeData(w, tokenSecretPayload(userid, tokenid, "secret-"+tokenid))
}

// handleTokenUpdate updates a token, or regenerates its secret when
// regenerate=1 is set (returning the new secret). Synchronous.
func (s *Server) handleTokenUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	if !s.parseForm(w, r) {
		return
	}
	userid := r.PathValue("userid")
	tokenid := r.PathValue("tokenid")
	regenerate := r.PostForm.Get("regenerate") == "1"
	s.st.mu.Lock()
	rec := s.lookupTokenLocked(userid, tokenid)
	if rec != nil && !regenerate {
		applyTokenForm(rec, r)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchToken)
		return
	}
	if regenerate {
		s.writeData(w, tokenSecretPayload(userid, tokenid, "rotated-"+tokenid))
		return
	}
	s.writeData(w, nil)
}

func applyTokenForm(rec *accessTokenRecord, r *http.Request) {
	// A present "comment" key (even empty) sets the comment, so clearing works.
	if _, ok := r.PostForm["comment"]; ok {
		rec.Comment = r.PostForm.Get("comment")
	}
	if v := r.PostForm.Get("privsep"); v != "" {
		rec.Privsep = v == "1"
	}
	if v, err := strconv.ParseInt(r.PostForm.Get("expire"), 10, 64); err == nil {
		rec.Expire = v
	}
}

func (s *Server) handleTokenDelete(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	userid := r.PathValue("userid")
	tokenid := r.PathValue("tokenid")
	s.st.mu.Lock()
	rec := s.lookupTokenLocked(userid, tokenid)
	if rec != nil {
		delete(s.st.access.tokens[userid], tokenid)
	}
	s.st.mu.Unlock()
	if rec == nil {
		s.writeError(w, http.StatusNotFound, msgNoSuchToken)
		return
	}
	s.writeData(w, nil)
}

func tokenToPayload(rec *accessTokenRecord) accessTokenPayload {
	return accessTokenPayload{
		TokenID: rec.TokenID, Comment: rec.Comment,
		Expire: rec.Expire, Privsep: boolToInt(rec.Privsep),
	}
}

func tokenSecretPayload(userid, tokenid, value string) map[string]string {
	return map[string]string{
		"full-tokenid": userid + "!" + tokenid,
		"value":        value,
	}
}
