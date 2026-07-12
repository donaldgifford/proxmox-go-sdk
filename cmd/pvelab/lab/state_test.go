package lab

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadStateMissing(t *testing.T) {
	st, err := LoadState(filepath.Join(t.TempDir(), "nope.json"))
	if st != nil || !errors.Is(err, ErrNoState) {
		t.Errorf("LoadState(missing) = %v, %v; want nil, ErrNoState", st, err)
	}
}

func TestStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	cfg := provisionTestConfig()

	_, err := UpdateState(path, func(st *State) {
		st.ClusterName = "dogfood"
		st.PVEVersion = "9.2"
		st.ISOVolid = "local:iso/pvelab-9.2-auto-http.iso"
		st.SeedNodes(cfg.Nested.Nodes)
	})
	if err != nil {
		t.Fatalf("UpdateState: %v", err)
	}

	// A second update keeps existing progress (SeedNodes is idempotent).
	_, err = UpdateState(path, func(st *State) {
		st.SeedNodes(cfg.Nested.Nodes)
		st.ApplyReadiness([]NodeReadiness{
			{Node: "pve1-dogfood", Ready: true, Elapsed: 244 * time.Second},
		})
	})
	if err != nil {
		t.Fatalf("UpdateState second pass: %v", err)
	}

	st, err := LoadState(path)
	if err != nil || st == nil {
		t.Fatalf("LoadState: %v, %v", st, err)
	}
	if st.SchemaVersion != StateSchemaVersion || st.ClusterName != "dogfood" || len(st.Nodes) != 3 {
		t.Errorf("state = %+v", st)
	}
	ns := st.FindNode("pve1-dogfood")
	if ns == nil || !ns.Ready || ns.ReadySeconds != 244 {
		t.Errorf("pve1 state = %+v, want ready at 244s", ns)
	}
	if st.FindNode("pve2-dogfood").Ready {
		t.Error("pve2 marked ready without evidence")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("state file mode = %o, want 600", perm)
	}
}

func TestLoadStateNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"schema_version": 99}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadState(path)
	if err == nil || !strings.Contains(err.Error(), "newer than this pvelab") {
		t.Errorf("LoadState(newer schema) = %v, want newer-schema error", err)
	}
}

func TestNewEnvFileAndRender(t *testing.T) {
	cfg := provisionTestConfig()
	env, err := NewEnvFile(cfg, "hunter2")
	if err != nil {
		t.Fatalf("NewEnvFile: %v", err)
	}
	if env.Endpoint != "https://192.0.2.201:8006" || env.Node != "pve1-dogfood" {
		t.Errorf("env = %+v", env)
	}

	rendered := string(RenderEnv(env))
	for _, want := range []string{
		"export PVE_ENDPOINT='https://192.0.2.201:8006'\n",
		"export PVE_USERNAME='root@pam'\n",
		"export PVE_PASSWORD='hunter2'\n",
		"export PVE_INSECURE_TLS='1'\n",
		"export PVE_NODE='pve1-dogfood'\n",
		"export PVE_TEST_STORAGE='local-lvm'\n",
		"export PVE_TEST_PLACEMENT_VMID_1='9301'\n",
		"export PVE_TEST_PLACEMENT_VMID_2='9302'\n",
		"export PVE_TEST_CONSOLE_VMID='9303'\n",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered env missing %q:\n%s", want, rendered)
		}
	}
}

func TestWriteEnvFile(t *testing.T) {
	cfg := provisionTestConfig()
	env, err := NewEnvFile(cfg, "hunter2")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), ".pvelab.env")
	if err := WriteEnvFile(path, env); err != nil {
		t.Fatalf("WriteEnvFile: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("env file mode = %o, want 600 (carries the password)", perm)
	}
}
