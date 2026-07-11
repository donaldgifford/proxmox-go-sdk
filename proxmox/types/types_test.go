package types

import (
	"encoding/json"
	"testing"
)

func TestPVEBoolUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    bool
		wantErr bool
	}{
		{name: "int one", in: `1`, want: true},
		{name: "int zero", in: `0`, want: false},
		{name: "bare true", in: `true`, want: true},
		{name: "bare false", in: `false`, want: false},
		{name: "string one", in: `"1"`, want: true},
		{name: "string zero", in: `"0"`, want: false},
		{name: "string yes", in: `"yes"`, want: true},
		{name: "string no", in: `"no"`, want: false},
		{name: "empty string", in: `""`, want: false},
		{name: "unknown string", in: `"maybe"`, wantErr: true},
		{name: "object", in: `{}`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b PVEBool
			err := json.Unmarshal([]byte(tt.in), &b)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Unmarshal(%s) = nil error, want error", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("Unmarshal(%s) = %v, want nil", tt.in, err)
			}
			if b.Bool() != tt.want {
				t.Errorf("Unmarshal(%s) = %v, want %v", tt.in, b.Bool(), tt.want)
			}
		})
	}
}

// TestPVEBoolUnmarshalNullNoop verifies that a JSON null leaves the value
// untouched rather than erroring, matching the encoding/json convention.
func TestPVEBoolUnmarshalNullNoop(t *testing.T) {
	b := PVEBool(true)
	if err := json.Unmarshal([]byte("null"), &b); err != nil {
		t.Fatalf("Unmarshal(null) = %v, want nil", err)
	}
	if !b.Bool() {
		t.Errorf("Unmarshal(null) changed value to false, want unchanged true")
	}
}

func TestPVEBoolMarshalJSON(t *testing.T) {
	tests := []struct {
		in   PVEBool
		want string
	}{
		{in: true, want: "1"},
		{in: false, want: "0"},
	}
	for _, tt := range tests {
		got, err := json.Marshal(tt.in)
		if err != nil {
			t.Fatalf("Marshal(%v) = %v, want nil", bool(tt.in), err)
		}
		if string(got) != tt.want {
			t.Errorf("Marshal(%v) = %s, want %s", bool(tt.in), got, tt.want)
		}
	}
}

// TestPVEBoolRoundTrip confirms a struct field survives an encode/decode cycle.
func TestPVEBoolRoundTrip(t *testing.T) {
	type config struct {
		Template PVEBool `json:"template"`
	}

	got := config{Template: true}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("Marshal = %v", err)
	}

	var back config
	if err := json.Unmarshal(encoded, &back); err != nil {
		t.Fatalf("Unmarshal = %v", err)
	}
	if back.Template != got.Template {
		t.Errorf("round trip = %v, want %v", back.Template, got.Template)
	}
}

func TestVMIDString(t *testing.T) {
	if got := VMID(100).String(); got != "100" {
		t.Errorf("VMID(100).String() = %q, want %q", got, "100")
	}
}

func TestGuestRefString(t *testing.T) {
	ref := GuestRef{Node: "pve", VMID: 100}
	if got, want := ref.String(), "pve/100"; got != want {
		t.Errorf("GuestRef.String() = %q, want %q", got, want)
	}
}

func TestPVEIntUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    int
		wantErr bool
	}{
		{name: "number", in: `8192`, want: 8192},
		{name: "zero", in: `0`, want: 0},
		// PVE 9.2.4 serializes integer config keys as quoted strings in
		// guest config reads (found live by the IMPL-0002 dogfood spike).
		{name: "quoted number", in: `"8192"`, want: 8192},
		{name: "quoted zero", in: `"0"`, want: 0},
		{name: "empty string", in: `""`, want: 0},
		{name: "non-numeric string", in: `"lots"`, wantErr: true},
		{name: "bool", in: `true`, wantErr: true},
		{name: "object", in: `{}`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var i PVEInt
			err := json.Unmarshal([]byte(tt.in), &i)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Unmarshal(%s) = nil error, want error", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("Unmarshal(%s) = %v, want nil", tt.in, err)
			}
			if i.Int() != tt.want {
				t.Errorf("Unmarshal(%s) = %d, want %d", tt.in, i.Int(), tt.want)
			}
		})
	}
}

// TestPVEIntUnmarshalNullNoop verifies that a JSON null leaves the value
// unchanged, matching PVEBool's convention.
func TestPVEIntUnmarshalNullNoop(t *testing.T) {
	i := PVEInt(7)
	if err := json.Unmarshal([]byte(`null`), &i); err != nil {
		t.Fatalf("Unmarshal(null) = %v, want nil", err)
	}
	if i != 7 {
		t.Errorf("Unmarshal(null) changed value to %d, want 7 (no-op)", i)
	}
}

func TestPVEIntMarshalJSON(t *testing.T) {
	out, err := json.Marshal(PVEInt(8192))
	if err != nil {
		t.Fatalf("Marshal = %v, want nil", err)
	}
	if string(out) != "8192" {
		t.Errorf("Marshal = %s, want 8192 (plain number)", out)
	}
}
