package access

import (
	"context"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/version"
)

// Service wraps PVE access control: users, groups, roles, ACLs, and API tokens,
// all under the 9.x privilege model (VM.Replicate and granular VM.GuestAgent.*
// privileges; VM.Monitor is removed). It is cluster-scoped — every endpoint
// lives under /access and binds no node. Construct it with NewService or via the
// root client's Access accessor; one *Service is safe for concurrent use.
type Service struct {
	c    api.Client
	caps version.Capabilities
}

// NewService returns an access Service. caps gates the token operations added in
// later 9.x minors (comment clearing 9.1, secret rotation 9.2).
func NewService(c api.Client, caps version.Capabilities) *Service {
	return &Service{c: c, caps: caps}
}

// API is the access service contract, published so consumers can stand in a test
// double for *Service. All config writes are synchronous (return an error, not a
// tasks.Ref); reads return typed data. Token create and secret regeneration
// additionally return the one-time secret.
type API interface {
	// Users.
	ListUsers(ctx context.Context) ([]User, error)
	GetUser(ctx context.Context, userid string) (*User, error)
	CreateUser(ctx context.Context, spec *UserSpec) error
	UpdateUser(ctx context.Context, userid string, update *UserUpdate) error
	DeleteUser(ctx context.Context, userid string) error

	// Groups.
	ListGroups(ctx context.Context) ([]Group, error)
	GetGroup(ctx context.Context, groupid string) (*Group, error)
	CreateGroup(ctx context.Context, spec *GroupSpec) error
	UpdateGroup(ctx context.Context, groupid string, update *GroupUpdate) error
	DeleteGroup(ctx context.Context, groupid string) error

	// Roles (9.x privilege model).
	ListRoles(ctx context.Context) ([]Role, error)
	GetRole(ctx context.Context, roleid string) (*Role, error)
	CreateRole(ctx context.Context, spec *RoleSpec) error
	UpdateRole(ctx context.Context, roleid string, update *RoleUpdate) error
	DeleteRole(ctx context.Context, roleid string) error

	// ACLs. SetACL grants; set ACLSpec.Delete to revoke.
	ListACLs(ctx context.Context) ([]ACLEntry, error)
	SetACL(ctx context.Context, spec *ACLSpec) error

	// API tokens (under /access/users/{userid}/token). CreateToken and
	// RegenerateTokenSecret return the one-time secret. ClearTokenComment is
	// gated on 9.1; RegenerateTokenSecret on 9.2 (TokenSecretRotation).
	ListTokens(ctx context.Context, userid string) ([]Token, error)
	GetToken(ctx context.Context, userid, tokenid string) (*Token, error)
	CreateToken(ctx context.Context, userid, tokenid string, spec *TokenSpec) (*TokenSecret, error)
	UpdateToken(ctx context.Context, userid, tokenid string, spec *TokenSpec) error
	RevokeToken(ctx context.Context, userid, tokenid string) error
	ClearTokenComment(ctx context.Context, userid, tokenid string) error
	RegenerateTokenSecret(ctx context.Context, userid, tokenid string) (*TokenSecret, error)
}

// Compile-time assertion that *Service implements the published contract.
var _ API = (*Service)(nil)
