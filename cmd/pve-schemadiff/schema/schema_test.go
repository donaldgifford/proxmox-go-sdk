package schema_test

import (
	"testing"

	"github.com/donaldgifford/proxmox-go-sdk/cmd/pve-schemadiff/schema"
)

// sampleAPIDoc is a synthetic apidoc.js: the JS `const … = [ … ];` wrapper
// around a small nested PVE-shaped tree, enough to exercise wrapper tolerance,
// recursion, HTTP-method filtering, and de-duplication.
const sampleAPIDoc = `// Proxmox VE API schema (synthetic)
const apiSchema = [
   {
      "path" : "/version",
      "info" : { "GET" : { "name" : "version" } }
   },
   {
      "path" : "/nodes",
      "info" : { "GET" : {} },
      "children" : [
         {
            "path" : "/nodes/{node}/qemu",
            "info" : {
               "GET"  : {},
               "POST" : {},
               "TEXT" : { "not" : "an http method, must be ignored" }
            },
            "children" : [
               {
                  "path" : "/nodes/{node}/qemu/{vmid}/vncproxy",
                  "info" : { "POST" : {} }
               }
            ]
         }
      ]
   }
];
`

func TestParse(t *testing.T) {
	t.Parallel()
	eps, err := schema.Parse([]byte(sampleAPIDoc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// GET /version, GET /nodes, GET+POST /nodes/{node}/qemu, POST vncproxy = 5.
	// TEXT is not an HTTP method and must be dropped.
	want := 5
	if len(eps) != want {
		t.Fatalf("Parse returned %d endpoints, want %d: %+v", len(eps), want, eps)
	}
	// Sorted by path then method: /nodes < /nodes/{node}/qemu < …/vncproxy < /version.
	if eps[0].Path != "/nodes" || eps[0].Method != "GET" {
		t.Errorf("first endpoint = %+v, want GET /nodes", eps[0])
	}
	for _, ep := range eps {
		if ep.Method == "TEXT" {
			t.Errorf("Parse kept a non-HTTP method: %+v", ep)
		}
	}
}

// TestParseTrailingAppCode pins the real-world apidoc.js shape: PVE ships the
// schema array followed by the ExtJS API-viewer application, so the file's
// last ']' belongs to viewer code, not the schema. A first-'['-to-last-']'
// slice (the pre-2026-07-19 implementation) fails this test with
// "invalid character ';' after top-level value".
func TestParseTrailingAppCode(t *testing.T) {
	t.Parallel()
	trailing := sampleAPIDoc + `
Ext.onReady(function() {
    let store = buildStore([{"path": "/fake"}]);
    window.onhashchange = function() { render(store.data[0]); };
});
`
	want, err := schema.Parse([]byte(sampleAPIDoc))
	if err != nil {
		t.Fatalf("Parse(clean): %v", err)
	}
	got, err := schema.Parse([]byte(trailing))
	if err != nil {
		t.Fatalf("Parse(trailing app code): %v", err)
	}
	if rep := schema.Diff(want, got); !rep.Empty() {
		t.Errorf("trailing app code changed the endpoint set: %+v", rep)
	}
}

func TestParseErrors(t *testing.T) {
	t.Parallel()
	if _, err := schema.Parse([]byte("const x = 42;")); err == nil {
		t.Error("Parse(no array) error = nil, want non-nil")
	}
	if _, err := schema.Parse([]byte("const x = [ {")); err == nil {
		t.Error("Parse(malformed array) error = nil, want non-nil")
	}
}

func TestDiff(t *testing.T) {
	t.Parallel()
	baseline := []schema.Endpoint{
		{Method: "GET", Path: "/version"},
		{Method: "GET", Path: "/nodes"},
		{Method: "DELETE", Path: "/nodes/{node}/qemu/{vmid}"},
	}
	current := []schema.Endpoint{
		{Method: "GET", Path: "/version"},
		{Method: "GET", Path: "/nodes"},
		{Method: "POST", Path: "/nodes/{node}/qemu"}, // added
		// DELETE …/{vmid} removed
	}

	rep := schema.Diff(baseline, current)
	if rep.Empty() {
		t.Fatal("Diff reported no drift, want drift")
	}
	if len(rep.Added) != 1 || rep.Added[0].Path != "/nodes/{node}/qemu" {
		t.Errorf("Added = %+v, want [POST /nodes/{node}/qemu]", rep.Added)
	}
	if len(rep.Removed) != 1 || rep.Removed[0].Method != "DELETE" {
		t.Errorf("Removed = %+v, want [DELETE …/{vmid}]", rep.Removed)
	}
}

func TestDiffNoDrift(t *testing.T) {
	t.Parallel()
	eps, err := schema.Parse([]byte(sampleAPIDoc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// A schema diffed against itself has no drift.
	if rep := schema.Diff(eps, eps); !rep.Empty() {
		t.Errorf("Diff(self, self) = %+v, want empty", rep)
	}
}

func TestBaselineRoundTrip(t *testing.T) {
	t.Parallel()
	eps, err := schema.Parse([]byte(sampleAPIDoc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	data, err := schema.MarshalBaseline(eps)
	if err != nil {
		t.Fatalf("MarshalBaseline: %v", err)
	}
	got, err := schema.UnmarshalBaseline(data)
	if err != nil {
		t.Fatalf("UnmarshalBaseline: %v", err)
	}
	if rep := schema.Diff(eps, got); !rep.Empty() {
		t.Errorf("round-trip changed the endpoint set: %+v", rep)
	}
}
