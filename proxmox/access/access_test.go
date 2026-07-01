package access_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/access"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/version"
)

func newService(t *testing.T, mock *mockpve.Server) *access.Service {
	t.Helper()
	c, cleanup := mock.NewClient()
	t.Cleanup(cleanup)
	return access.NewService(c, version.Capabilities{})
}

func newCappedService(t *testing.T, mock *mockpve.Server, ver string) *access.Service {
	t.Helper()
	caps, err := version.Parse(ver)
	if err != nil {
		t.Fatalf("version.Parse(%q): %v", ver, err)
	}
	c, cleanup := mock.NewClient()
	t.Cleanup(cleanup)
	return access.NewService(c, caps)
}

func TestUserCRUD(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.CreateUser(ctx, &access.UserSpec{
		UserID: "alice@pve",
		Email:  "alice@example.com",
		Groups: []string{"admins"},
	}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	u, err := svc.GetUser(ctx, "alice@pve")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if u.Email != "alice@example.com" || len(u.Groups) != 1 || u.Groups[0] != "admins" {
		t.Errorf("user = %+v, want email + group admins", u)
	}

	if err := svc.UpdateUser(ctx, "alice@pve", &access.UserUpdate{Comment: "ops"}); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	users, err := svc.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("ListUsers returned %d, want 1", len(users))
	}

	if err := svc.DeleteUser(ctx, "alice@pve"); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := svc.GetUser(ctx, "alice@pve"); !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("GetUser after delete = %v, want ErrNotFound", err)
	}
}

func TestCreateUserValidation(t *testing.T) {
	t.Parallel()
	svc := newService(t, mockpve.New())
	ctx := context.Background()
	if err := svc.CreateUser(ctx, nil); err == nil {
		t.Error("CreateUser(nil) error = nil, want non-nil")
	}
	if err := svc.CreateUser(ctx, &access.UserSpec{}); err == nil {
		t.Error("CreateUser(no userid) error = nil, want non-nil")
	}
}

func TestGroupCRUD(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.CreateGroup(ctx, &access.GroupSpec{GroupID: "admins", Comment: "ops team"}); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	g, err := svc.GetGroup(ctx, "admins")
	if err != nil {
		t.Fatalf("GetGroup: %v", err)
	}
	if g.Comment != "ops team" {
		t.Errorf("group comment = %q, want 'ops team'", g.Comment)
	}
	if err := svc.DeleteGroup(ctx, "admins"); err != nil {
		t.Fatalf("DeleteGroup: %v", err)
	}
	if _, err := svc.GetGroup(ctx, "admins"); !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("GetGroup after delete = %v, want ErrNotFound", err)
	}
}

func TestRoleCRUD(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	// A 9.x role using VM.Replicate (VM.Monitor is gone in 9.x).
	privs := []string{"VM.Audit", "VM.Replicate", "VM.GuestAgent.Audit"}
	if err := svc.CreateRole(ctx, &access.RoleSpec{RoleID: "Replicator", Privs: privs}); err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	roles, err := svc.ListRoles(ctx)
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	if len(roles) != 1 || roles[0].RoleID != "Replicator" || len(roles[0].Privs) != 3 {
		t.Fatalf("ListRoles = %+v, want one Replicator with 3 privs", roles)
	}

	// Single-role GET returns the priv→1 map, normalised into Privs.
	r, err := svc.GetRole(ctx, "Replicator")
	if err != nil {
		t.Fatalf("GetRole: %v", err)
	}
	if r.RoleID != "Replicator" || len(r.Privs) != 3 {
		t.Errorf("GetRole = %+v, want RoleID set + 3 privs", r)
	}

	if err := svc.DeleteRole(ctx, "Replicator"); err != nil {
		t.Fatalf("DeleteRole: %v", err)
	}
}

func TestRoleUnmarshalPrivMap(t *testing.T) {
	t.Parallel()
	// The single-role GET shape: a privilege→1 object with no roleid key.
	const blob = `{"VM.Audit":1,"VM.Replicate":1}`
	var r access.Role
	if err := json.Unmarshal([]byte(blob), &r); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(r.Privs) != 2 {
		t.Errorf("Privs = %v, want 2 entries", r.Privs)
	}
}

func TestACL(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.SetACL(ctx, &access.ACLSpec{
		Path:  "/vms/100",
		Roles: []string{"PVEVMAdmin"},
		Users: []string{"alice@pve"},
	}); err != nil {
		t.Fatalf("SetACL: %v", err)
	}
	acls, err := svc.ListACLs(ctx)
	if err != nil {
		t.Fatalf("ListACLs: %v", err)
	}
	if len(acls) != 1 || acls[0].Path != "/vms/100" || acls[0].UGID != "alice@pve" {
		t.Fatalf("ListACLs = %+v, want one entry for alice on /vms/100", acls)
	}

	// Revoke it.
	if err := svc.SetACL(ctx, &access.ACLSpec{
		Path:   "/vms/100",
		Roles:  []string{"PVEVMAdmin"},
		Users:  []string{"alice@pve"},
		Delete: ptrBool(true),
	}); err != nil {
		t.Fatalf("SetACL(delete): %v", err)
	}
	acls, err = svc.ListACLs(ctx)
	if err != nil {
		t.Fatalf("ListACLs after revoke: %v", err)
	}
	if len(acls) != 0 {
		t.Fatalf("ListACLs after revoke = %+v, want empty", acls)
	}
}

func TestSetACLValidation(t *testing.T) {
	t.Parallel()
	svc := newService(t, mockpve.New())
	ctx := context.Background()
	if err := svc.SetACL(ctx, &access.ACLSpec{Path: "/", Roles: []string{"X"}}); err == nil {
		t.Error("SetACL(no subject) error = nil, want non-nil")
	}
	if err := svc.SetACL(ctx, &access.ACLSpec{Path: "/", Users: []string{"a"}}); err == nil {
		t.Error("SetACL(no roles) error = nil, want non-nil")
	}
}

func TestTokenLifecycle(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	secret, err := svc.CreateToken(ctx, "alice@pve", "automation", &access.TokenSpec{Comment: "ci"})
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	if secret.Value == "" || secret.FullTokenID != "alice@pve!automation" {
		t.Errorf("token secret = %+v, want value + full-tokenid", secret)
	}

	tokens, err := svc.ListTokens(ctx, "alice@pve")
	if err != nil {
		t.Fatalf("ListTokens: %v", err)
	}
	if len(tokens) != 1 || tokens[0].TokenID != "automation" {
		t.Fatalf("ListTokens = %+v, want one 'automation'", tokens)
	}

	if err := svc.UpdateToken(ctx, "alice@pve", "automation", &access.TokenSpec{Comment: "prod"}); err != nil {
		t.Fatalf("UpdateToken: %v", err)
	}
	tok, err := svc.GetToken(ctx, "alice@pve", "automation")
	if err != nil {
		t.Fatalf("GetToken: %v", err)
	}
	if tok.Comment != "prod" {
		t.Errorf("token comment = %q, want prod", tok.Comment)
	}

	if err := svc.RevokeToken(ctx, "alice@pve", "automation"); err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}
	if _, err := svc.GetToken(ctx, "alice@pve", "automation"); !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("GetToken after revoke = %v, want ErrNotFound", err)
	}
}

func TestClearTokenCommentGate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mock90 := mockpve.New()
	svc90 := newCappedService(t, mock90, "9.0") // below the 9.1 gate.
	if err := svc90.ClearTokenComment(ctx, "alice@pve", "tok"); !errors.Is(err, pverr.ErrUnsupported) {
		t.Fatalf("ClearTokenComment on 9.0 = %v, want ErrUnsupported", err)
	}

	mock91 := mockpve.New()
	mock91.AddToken("alice@pve", "tok")
	svc91 := newCappedService(t, mock91, "9.1")
	if err := svc91.ClearTokenComment(ctx, "alice@pve", "tok"); err != nil {
		t.Fatalf("ClearTokenComment on 9.1 = %v, want nil", err)
	}
}

func TestRegenerateTokenSecretGate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mock91 := mockpve.New()
	svc91 := newCappedService(t, mock91, "9.1") // below the 9.2 gate.
	if _, err := svc91.RegenerateTokenSecret(ctx, "alice@pve", "tok"); !errors.Is(err, pverr.ErrUnsupported) {
		t.Fatalf("RegenerateTokenSecret on 9.1 = %v, want ErrUnsupported", err)
	}

	mock92 := mockpve.New()
	mock92.AddToken("alice@pve", "tok")
	svc92 := newCappedService(t, mock92, "9.2")
	secret, err := svc92.RegenerateTokenSecret(ctx, "alice@pve", "tok")
	if err != nil {
		t.Fatalf("RegenerateTokenSecret on 9.2 = %v, want nil", err)
	}
	if secret.Value == "" {
		t.Error("regenerated secret Value is empty")
	}
}

func ptrBool(b bool) *types.PVEBool {
	v := types.PVEBool(b)
	return &v
}
