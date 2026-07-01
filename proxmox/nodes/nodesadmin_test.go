package nodes_test

import (
	"context"
	"errors"
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/mockpve"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/nodes"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/types"
)

func ptrBool(b bool) *types.PVEBool {
	v := types.PVEBool(b)
	return &v
}

// --- apt ---

func TestListAptUpdates(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddAptUpdate(testNode, "pve-manager", "9.0.5", "9.0.3")
	mock.AddAptUpdate(testNode, "libpve-common-perl", "9.0.2", "9.0.1")
	svc := newService(t, mock)

	updates, err := svc.ListAptUpdates(context.Background(), testNode)
	if err != nil {
		t.Fatalf("ListAptUpdates: %v", err)
	}
	if len(updates) != 2 {
		t.Fatalf("ListAptUpdates returned %d, want 2", len(updates))
	}
}

func TestRefreshAptCache(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc, ts := newServiceAndTasks(t, mock)
	ctx := context.Background()

	ref, err := svc.RefreshAptCache(ctx, testNode)
	if err != nil {
		t.Fatalf("RefreshAptCache: %v", err)
	}
	if ref.IsZero() {
		t.Fatal("RefreshAptCache returned a zero Ref, want a worker task")
	}
	st, err := ts.Wait(ctx, ref)
	if err != nil {
		t.Fatalf("Wait(apt refresh): %v", err)
	}
	if !st.OK() {
		t.Errorf("apt-refresh task exit = %q, want OK", st.ExitStatus)
	}
}

func TestRepositories(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddRepository(testNode, "/etc/apt/sources.list.d/pve.sources", "https://enterprise.proxmox.com", true)
	svc := newService(t, mock)
	ctx := context.Background()

	repos, err := svc.ListRepositories(ctx, testNode)
	if err != nil {
		t.Fatalf("ListRepositories: %v", err)
	}
	if len(repos.Files) != 1 || len(repos.Files[0].Repositories) != 1 {
		t.Fatalf("ListRepositories = %+v, want one file with one repo", repos)
	}
	if repos.Files[0].Repositories[0].Enabled != 1 {
		t.Errorf("seeded repo Enabled = %d, want 1", repos.Files[0].Repositories[0].Enabled)
	}

	if err := svc.UpdateRepository(ctx, testNode, &nodes.RepositoryUpdate{
		Path: "/etc/apt/sources.list.d/pve.sources", Index: 0, Enabled: ptrBool(false),
	}); err != nil {
		t.Fatalf("UpdateRepository: %v", err)
	}
	repos, err = svc.ListRepositories(ctx, testNode)
	if err != nil {
		t.Fatalf("ListRepositories after update: %v", err)
	}
	if repos.Files[0].Repositories[0].Enabled != 0 {
		t.Errorf("repo Enabled after disable = %d, want 0", repos.Files[0].Repositories[0].Enabled)
	}
}

func TestUpdateRepositoryValidation(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	if err := svc.UpdateRepository(ctx, testNode, nil); err == nil {
		t.Error("UpdateRepository(nil) error = nil, want non-nil")
	}
	if err := svc.UpdateRepository(ctx, testNode, &nodes.RepositoryUpdate{}); err == nil {
		t.Error("UpdateRepository(no path) error = nil, want non-nil")
	}
}

// --- disks ---

func TestListDisks(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddDisk(testNode, "/dev/sda", "ssd")
	mock.AddDisk(testNode, "/dev/sdb", "hdd")
	svc := newService(t, mock)

	disks, err := svc.ListDisks(context.Background(), testNode)
	if err != nil {
		t.Fatalf("ListDisks: %v", err)
	}
	if len(disks) != 2 {
		t.Fatalf("ListDisks returned %d, want 2", len(disks))
	}
}

func TestGetDiskSMART(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddDisk(testNode, "/dev/sda", "ssd")
	svc := newService(t, mock)

	smart, err := svc.GetDiskSMART(context.Background(), testNode, "/dev/sda")
	if err != nil {
		t.Fatalf("GetDiskSMART: %v", err)
	}
	if smart.Health != "PASSED" {
		t.Errorf("SMART health = %q, want PASSED", smart.Health)
	}
}

func TestGetDiskSMARTNotFound(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)

	if _, err := svc.GetDiskSMART(context.Background(), testNode, "/dev/ghost"); !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("GetDiskSMART(ghost) = %v, want ErrNotFound", err)
	}
}

func TestGetDiskSMARTValidation(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)

	if _, err := svc.GetDiskSMART(context.Background(), testNode, ""); err == nil {
		t.Error("GetDiskSMART(no disk) error = nil, want non-nil")
	}
}

func TestInitializeDisk(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc, ts := newServiceAndTasks(t, mock)
	ctx := context.Background()

	ref, err := svc.InitializeDisk(ctx, testNode, "/dev/sdb")
	if err != nil {
		t.Fatalf("InitializeDisk: %v", err)
	}
	if ref.IsZero() {
		t.Fatal("InitializeDisk returned a zero Ref, want a worker task")
	}
	st, err := ts.Wait(ctx, ref)
	if err != nil {
		t.Fatalf("Wait(initgpt): %v", err)
	}
	if !st.OK() {
		t.Errorf("initgpt task exit = %q, want OK", st.ExitStatus)
	}
}

func TestInitializeDiskValidation(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)

	if _, err := svc.InitializeDisk(context.Background(), testNode, ""); err == nil {
		t.Error("InitializeDisk(no disk) error = nil, want non-nil")
	}
}

// --- certificates ---

func TestNodeCertificates(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddNodeCertificate(testNode, "pveproxy-ssl.pem")
	svc := newService(t, mock)
	ctx := context.Background()

	certs, err := svc.GetNodeCertificates(ctx, testNode)
	if err != nil {
		t.Fatalf("GetNodeCertificates: %v", err)
	}
	if len(certs) != 1 || certs[0].Filename != "pveproxy-ssl.pem" {
		t.Fatalf("GetNodeCertificates = %+v, want one pveproxy-ssl.pem", certs)
	}

	after, err := svc.UploadCustomCertificate(ctx, testNode, &nodes.CustomCertificateSpec{
		Certificates: "-----BEGIN CERTIFICATE-----\nmock\n-----END CERTIFICATE-----",
	})
	if err != nil {
		t.Fatalf("UploadCustomCertificate: %v", err)
	}
	if len(after) != 2 {
		t.Errorf("cert count after upload = %d, want 2", len(after))
	}

	if err := svc.DeleteCustomCertificate(ctx, testNode); err != nil {
		t.Fatalf("DeleteCustomCertificate: %v", err)
	}
	certs, err = svc.GetNodeCertificates(ctx, testNode)
	if err != nil {
		t.Fatalf("GetNodeCertificates after delete: %v", err)
	}
	if len(certs) != 0 {
		t.Errorf("cert count after delete = %d, want 0", len(certs))
	}
}

func TestUploadCustomCertificateValidation(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	if _, err := svc.UploadCustomCertificate(ctx, testNode, nil); err == nil {
		t.Error("UploadCustomCertificate(nil) error = nil, want non-nil")
	}
	if _, err := svc.UploadCustomCertificate(ctx, testNode, &nodes.CustomCertificateSpec{}); err == nil {
		t.Error("UploadCustomCertificate(no pem) error = nil, want non-nil")
	}
}

func TestNodeCertificateACMEOps(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc, ts := newServiceAndTasks(t, mock)
	ctx := context.Background()

	for name, op := range map[string]func() (tasks.Ref, error){
		"order":  func() (tasks.Ref, error) { return svc.OrderNodeCertificate(ctx, testNode) },
		"renew":  func() (tasks.Ref, error) { return svc.RenewNodeCertificate(ctx, testNode) },
		"revoke": func() (tasks.Ref, error) { return svc.RevokeNodeCertificate(ctx, testNode) },
	} {
		ref, err := op()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if ref.IsZero() {
			t.Fatalf("%s returned a zero Ref, want a worker task", name)
		}
		st, err := ts.Wait(ctx, ref)
		if err != nil {
			t.Fatalf("Wait(%s): %v", name, err)
		}
		if !st.OK() {
			t.Errorf("%s task exit = %q, want OK", name, st.ExitStatus)
		}
	}
}

// --- ACME accounts (cluster-scoped) ---

func TestACMEAccounts(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	mock.AddACMEAccount("default")
	svc, ts := newServiceAndTasks(t, mock)
	ctx := context.Background()

	names, err := svc.ListACMEAccounts(ctx)
	if err != nil {
		t.Fatalf("ListACMEAccounts: %v", err)
	}
	if len(names) != 1 || names[0] != "default" {
		t.Fatalf("ListACMEAccounts = %v, want [default]", names)
	}

	acc, err := svc.GetACMEAccount(ctx, "default")
	if err != nil {
		t.Fatalf("GetACMEAccount: %v", err)
	}
	if acc.Directory == "" {
		t.Error("GetACMEAccount returned empty Directory")
	}

	ref, err := svc.RegisterACMEAccount(ctx, &nodes.ACMEAccountSpec{
		Name: "staging", Contact: []string{"admin@example.com"},
		Directory: "https://acme-staging.example/directory", TOSURL: "https://acme.example/tos",
	})
	if err != nil {
		t.Fatalf("RegisterACMEAccount: %v", err)
	}
	if _, err := ts.Wait(ctx, ref); err != nil {
		t.Fatalf("Wait(register acme): %v", err)
	}

	if err := svc.UpdateACMEAccount(ctx, "staging", &nodes.ACMEAccountUpdate{
		Contact: []string{"ops@example.com"},
	}); err != nil {
		t.Fatalf("UpdateACMEAccount: %v", err)
	}

	ref, err = svc.DeactivateACMEAccount(ctx, "staging")
	if err != nil {
		t.Fatalf("DeactivateACMEAccount: %v", err)
	}
	if _, err := ts.Wait(ctx, ref); err != nil {
		t.Fatalf("Wait(deactivate acme): %v", err)
	}
	if _, err := svc.GetACMEAccount(ctx, "staging"); !errors.Is(err, pverr.ErrNotFound) {
		t.Fatalf("GetACMEAccount(staging) after deactivate = %v, want ErrNotFound", err)
	}
}

func TestACMEAccountValidation(t *testing.T) {
	t.Parallel()
	mock := mockpve.New()
	svc := newService(t, mock)
	ctx := context.Background()

	if _, err := svc.RegisterACMEAccount(ctx, nil); err == nil {
		t.Error("RegisterACMEAccount(nil) error = nil, want non-nil")
	}
	if _, err := svc.RegisterACMEAccount(ctx, &nodes.ACMEAccountSpec{Name: "x"}); err == nil {
		t.Error("RegisterACMEAccount(no contact) error = nil, want non-nil")
	}
	if _, err := svc.GetACMEAccount(ctx, ""); err == nil {
		t.Error("GetACMEAccount(empty) error = nil, want non-nil")
	}
}
