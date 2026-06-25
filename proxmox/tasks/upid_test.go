package tasks

import "testing"

const testUPID = "UPID:pve:000A1B2C:00ABCDEF:6489ABCD:qmstart:100:root@pam:"

func TestParseUPID(t *testing.T) {
	u, err := ParseUPID(testUPID)
	if err != nil {
		t.Fatalf("ParseUPID(%q): %v", testUPID, err)
	}
	if u.Node != "pve" {
		t.Errorf("Node = %q, want pve", u.Node)
	}
	if u.PID != 0x000A1B2C {
		t.Errorf("PID = %d, want %d", u.PID, 0x000A1B2C)
	}
	if u.Type != "qmstart" {
		t.Errorf("Type = %q, want qmstart", u.Type)
	}
	if u.ID != "100" {
		t.Errorf("ID = %q, want 100", u.ID)
	}
	if u.User != "root@pam" {
		t.Errorf("User = %q, want root@pam", u.User)
	}
	if u.Raw != testUPID {
		t.Errorf("Raw = %q, want %q", u.Raw, testUPID)
	}
	if u.StartTime.IsZero() {
		t.Error("StartTime is zero, want decoded time")
	}
}

func TestParseUPIDMalformed(t *testing.T) {
	for _, in := range []string{
		"",
		"not-a-upid",
		"UPID:pve:short",
		"UPID:pve:ZZZZ:00AB:6489:qmstart:100:root@pam:", // bad hex pid
		"PID:pve:000A:00AB:6489:qmstart:100:root@pam:",  // wrong prefix
	} {
		if u, err := ParseUPID(in); err == nil {
			t.Errorf("ParseUPID(%q) = %+v, want error", in, u)
		}
	}
}

func TestNewRef(t *testing.T) {
	r, err := NewRef(testUPID)
	if err != nil {
		t.Fatalf("NewRef: %v", err)
	}
	if r.Node != "pve" || r.UPID != testUPID {
		t.Errorf("NewRef = %+v, want node pve and raw upid", r)
	}

	if _, err := NewRef("garbage"); err == nil {
		t.Error("NewRef(garbage) = nil error, want error")
	}
}
