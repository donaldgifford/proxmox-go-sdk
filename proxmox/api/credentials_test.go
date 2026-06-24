package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTokenCredentials(t *testing.T) {
	c := TokenCredentials("root@pam!mytok", "secret-uuid")
	if c.needsRefresh() {
		t.Error("token needsRefresh() = true, want false")
	}
	if c.csrfToken() != "" {
		t.Errorf("token csrfToken() = %q, want empty", c.csrfToken())
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "http://x/api2/json/version", http.NoBody)
	c.authorize(req)
	want := "PVEAPIToken=root@pam!mytok=secret-uuid"
	if got := req.Header.Get("Authorization"); got != want {
		t.Errorf("Authorization = %q, want %q", got, want)
	}
}

func TestTicketCredentials(t *testing.T) {
	c := TicketCredentials("TICKETVAL", "CSRFVAL")
	if c.needsRefresh() {
		t.Error("pre-supplied ticket needsRefresh() = true, want false")
	}
	if got := c.csrfToken(); got != "CSRFVAL" {
		t.Errorf("csrfToken() = %q, want CSRFVAL", got)
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "http://x/api2/json/x", http.NoBody)
	c.authorize(req)
	ck, err := req.Cookie("PVEAuthCookie")
	if err != nil {
		t.Fatalf("PVEAuthCookie cookie not set: %v", err)
	}
	if ck.Value != "TICKETVAL" {
		t.Errorf("PVEAuthCookie = %q, want TICKETVAL", ck.Value)
	}
}

func TestUserCredentialsNeedsMintBeforeFirstUse(t *testing.T) {
	c := UserCredentials("root@pam", "pw", "")
	if !c.needsRefresh() {
		t.Error("un-minted userCreds needsRefresh() = false, want true")
	}
	if got := c.csrfToken(); got != "" {
		t.Errorf("csrfToken() before mint = %q, want empty", got)
	}
}
