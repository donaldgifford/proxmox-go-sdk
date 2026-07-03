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
