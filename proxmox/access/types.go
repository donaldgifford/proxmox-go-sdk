package access

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

// User is one entry from GET /access/users[/{userid}]. Reads are lossless.
type User struct {
	UserID    string        `json:"userid"`
	Enable    types.PVEBool `json:"enable,omitempty"`
	Expire    int64         `json:"expire,omitempty"` // unix epoch; 0 = never.
	FirstName string        `json:"firstname,omitempty"`
	LastName  string        `json:"lastname,omitempty"`
	Email     string        `json:"email,omitempty"`
	Comment   string        `json:"comment,omitempty"`
	Groups    []string      `json:"groups,omitempty"`
	// Extra carries user keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

var userKnownFields = map[string]bool{
	"userid": true, "enable": true, "expire": true, "firstname": true,
	"lastname": true, "email": true, "comment": true, "groups": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (u *User) UnmarshalJSON(data []byte) error {
	type alias User
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode user: %w", err)
	}
	*u = User(a)
	extra, err := svcutil.DecodeExtra(data, userKnownFields)
	if err != nil {
		return fmt.Errorf("decode user: %w", err)
	}
	u.Extra = extra
	return nil
}

// UserSpec is the body of POST /access/users. UserID (e.g. "alice@pve") is
// required. Groups is CSV-joined into the "groups" param. Pass it by pointer.
type UserSpec struct {
	UserID    string         `json:"userid"`
	Password  string         `json:"password,omitempty"`
	Enable    *types.PVEBool `json:"enable,omitempty"`
	Expire    int64          `json:"expire,omitempty"`
	FirstName string         `json:"firstname,omitempty"`
	LastName  string         `json:"lastname,omitempty"`
	Email     string         `json:"email,omitempty"`
	Comment   string         `json:"comment,omitempty"`
	Groups    []string       `json:"-"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// UserUpdate is the body of PUT /access/users/{userid}. All fields optional; use
// Delete to unset keys. Pass it by pointer.
type UserUpdate struct {
	Enable    *types.PVEBool `json:"enable,omitempty"`
	Expire    int64          `json:"expire,omitempty"`
	FirstName string         `json:"firstname,omitempty"`
	LastName  string         `json:"lastname,omitempty"`
	Email     string         `json:"email,omitempty"`
	Comment   string         `json:"comment,omitempty"`
	Groups    []string       `json:"-"`
	Delete    string         `json:"delete,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// Group is one entry from GET /access/groups[/{groupid}]. Reads are lossless.
type Group struct {
	GroupID string   `json:"groupid"`
	Comment string   `json:"comment,omitempty"`
	Members []string `json:"members,omitempty"`
	// Extra carries group keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

var groupKnownFields = map[string]bool{
	"groupid": true, "comment": true, "members": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (g *Group) UnmarshalJSON(data []byte) error {
	type alias Group
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode group: %w", err)
	}
	*g = Group(a)
	extra, err := svcutil.DecodeExtra(data, groupKnownFields)
	if err != nil {
		return fmt.Errorf("decode group: %w", err)
	}
	g.Extra = extra
	return nil
}

// GroupSpec is the body of POST /access/groups. GroupID is required. Pass it by
// pointer.
type GroupSpec struct {
	GroupID string `json:"groupid"`
	Comment string `json:"comment,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// GroupUpdate is the body of PUT /access/groups/{groupid}. Pass it by pointer.
type GroupUpdate struct {
	Comment string `json:"comment,omitempty"`
	Delete  string `json:"delete,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// Role is one entry from GET /access/roles[/{roleid}]. Its privileges follow the
// 9.x model (VM.Replicate, granular VM.GuestAgent.* — VM.Monitor is gone).
//
// The two role endpoints return different shapes: the list gives each role a CSV
// "privs" string, while a single-role GET returns a privilege→1 object. Role's
// UnmarshalJSON normalises both into Privs.
type Role struct {
	RoleID  string        `json:"-"`
	Privs   []string      `json:"-"`
	Special types.PVEBool `json:"-"`
	// Extra carries role keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

// UnmarshalJSON normalises the two /access/roles response shapes into Role.
func (r *Role) UnmarshalJSON(data []byte) error {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return fmt.Errorf("decode role: %w", err)
	}
	if _, isListEntry := probe["roleid"]; isListEntry {
		var e struct {
			RoleID  string        `json:"roleid"`
			Privs   string        `json:"privs"`
			Special types.PVEBool `json:"special"`
		}
		if err := json.Unmarshal(data, &e); err != nil {
			return fmt.Errorf("decode role entry: %w", err)
		}
		r.RoleID = e.RoleID
		r.Special = e.Special
		if e.Privs != "" {
			r.Privs = strings.Split(e.Privs, ",")
		}
		return nil
	}
	// Single-role GET: a privilege→1 map. Append to a nil slice so an empty
	// role leaves Privs nil, matching the list-entry branch above.
	for priv := range probe {
		r.Privs = append(r.Privs, priv)
	}
	sort.Strings(r.Privs)
	return nil
}

// RoleSpec is the body of POST /access/roles. RoleID is required. Privs is
// CSV-joined into the "privs" param. Pass it by pointer.
type RoleSpec struct {
	RoleID string   `json:"roleid"`
	Privs  []string `json:"-"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// RoleUpdate is the body of PUT /access/roles/{roleid}. Set Append to add Privs
// to the role rather than replacing them. Pass it by pointer.
type RoleUpdate struct {
	Privs  []string       `json:"-"`
	Append *types.PVEBool `json:"append,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// ACLEntry is one entry from GET /access/acl — a role granted to a user, group,
// or token on a path. Reads are lossless.
type ACLEntry struct {
	Path      string        `json:"path"`
	RoleID    string        `json:"roleid"`
	Type      string        `json:"type"` // user, group, or token.
	UGID      string        `json:"ugid"`
	Propagate types.PVEBool `json:"propagate,omitempty"`
	// Extra carries ACL keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

var aclKnownFields = map[string]bool{
	"path": true, "roleid": true, "type": true, "ugid": true, "propagate": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (a *ACLEntry) UnmarshalJSON(data []byte) error {
	type alias ACLEntry
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("decode acl entry: %w", err)
	}
	*a = ACLEntry(raw)
	extra, err := svcutil.DecodeExtra(data, aclKnownFields)
	if err != nil {
		return fmt.Errorf("decode acl entry: %w", err)
	}
	a.Extra = extra
	return nil
}

// ACLSpec is the body of PUT /access/acl — granting (or, with Delete, revoking)
// Roles on Path to some combination of Users, Groups, and Tokens. Path and at
// least one Role plus one subject are required. The slice fields are CSV-joined
// into their params. Pass it by pointer.
type ACLSpec struct {
	Path      string         `json:"path"`
	Roles     []string       `json:"-"`
	Users     []string       `json:"-"`
	Groups    []string       `json:"-"`
	Tokens    []string       `json:"-"`
	Propagate *types.PVEBool `json:"propagate,omitempty"`
	Delete    *types.PVEBool `json:"delete,omitempty"` // delete=1 revokes instead of grants.
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// Token is one entry from GET /access/users/{userid}/token[/{tokenid}]. Reads
// are lossless.
type Token struct {
	TokenID string        `json:"tokenid"`
	Comment string        `json:"comment,omitempty"`
	Expire  int64         `json:"expire,omitempty"`
	Privsep types.PVEBool `json:"privsep,omitempty"`
	// Extra carries token keys the SDK does not model.
	Extra map[string]string `json:"-"`
}

var tokenKnownFields = map[string]bool{
	"tokenid": true, "comment": true, "expire": true, "privsep": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (t *Token) UnmarshalJSON(data []byte) error {
	type alias Token
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode token: %w", err)
	}
	*t = Token(a)
	extra, err := svcutil.DecodeExtra(data, tokenKnownFields)
	if err != nil {
		return fmt.Errorf("decode token: %w", err)
	}
	t.Extra = extra
	return nil
}

// TokenSpec is the body of POST (create) and PUT (update) on a token. All fields
// optional. Pass it by pointer.
type TokenSpec struct {
	Comment string         `json:"comment,omitempty"`
	Expire  int64          `json:"expire,omitempty"`
	Privsep *types.PVEBool `json:"privsep,omitempty"`
	// Extra carries PVE parameters the SDK does not model.
	Extra map[string]string `json:"-"`
}

// TokenSecret is the one-time secret PVE returns when a token is created or its
// secret is regenerated. The Value is shown only once; store it immediately.
type TokenSecret struct {
	FullTokenID string `json:"full-tokenid"`
	Value       string `json:"value"`
	// Extra carries keys the SDK does not model (e.g. the nested "info").
	Extra map[string]string `json:"-"`
}

var tokenSecretKnownFields = map[string]bool{
	"full-tokenid": true, "value": true,
}

// UnmarshalJSON decodes the modelled fields and routes unknown keys into Extra.
func (s *TokenSecret) UnmarshalJSON(data []byte) error {
	type alias TokenSecret
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("decode token secret: %w", err)
	}
	*s = TokenSecret(a)
	extra, err := svcutil.DecodeExtra(data, tokenSecretKnownFields)
	if err != nil {
		return fmt.Errorf("decode token secret: %w", err)
	}
	s.Extra = extra
	return nil
}
