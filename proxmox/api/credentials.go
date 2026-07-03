package api

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const (
	// ticketSkew is how long before a ticket's expiry the transport refreshes
	// it.
	ticketSkew = 5 * time.Minute
	// ticketLifetime is how long a freshly minted PVE ticket is valid. PVE does
	// not return the expiry, so it is derived from the mint time.
	ticketLifetime = 2 * time.Hour
)

// rawDoer is the unauthenticated send function the transport hands to
// refresh, so credentials can mint a ticket without re-entering the auth path.
type rawDoer func(ctx context.Context, method, path string, body, out any) error

// Credentials is the authentication strategy. Implementations are unexported;
// callers pick one with TokenCredentials, TicketCredentials, or
// UserCredentials. Precedence is resolved at the call site: the caller chooses
// exactly one (ticket > API token > user/password).
//
// The transport calls the methods in this order: needsRefresh before each
// request (cheap, no I/O); refresh if it returned true (may POST
// /access/ticket); authorize to set the auth header/cookie; and csrfToken on
// writes (empty for token auth, which needs no CSRF token). When the server
// rejects a ticket as expired, the transport calls refresh with force=true to
// re-mint even if the local clock still considers the ticket valid.
type Credentials interface {
	needsRefresh() bool
	refresh(ctx context.Context, do rawDoer, force bool) error
	authorize(r *http.Request)
	csrfToken() string
}

// TokenCredentials authenticates with a PVE API token. tokenID has the form
// "user@realm!tokenname". Token auth never expires and carries no CSRF token.
func TokenCredentials(tokenID, secret string) Credentials {
	return &tokenCreds{id: tokenID, secret: secret}
}

// TicketCredentials authenticates with a pre-minted ticket and CSRF token. The
// caller owns the ticket's lifetime; the SDK will not refresh it (use
// UserCredentials for automatic refresh).
func TicketCredentials(ticket, csrf string) Credentials {
	return &ticketCreds{ticket: ticket, csrf: csrf}
}

// UserCredentials authenticates with a username, password, and optional TOTP.
// The transport mints a ticket on first use and refreshes it before the 2h
// expiry. The username has the form "user@realm", e.g. "root@pam".
func UserCredentials(user, password, otp string) Credentials {
	return &userCreds{user: user, password: password, otp: otp}
}

// ---- tokenCreds ----

type tokenCreds struct {
	id     string
	secret string
}

func (*tokenCreds) needsRefresh() bool                           { return false }
func (*tokenCreds) refresh(context.Context, rawDoer, bool) error { return nil }
func (*tokenCreds) csrfToken() string                            { return "" }
func (t *tokenCreds) authorize(r *http.Request) {
	r.Header.Set("Authorization", "PVEAPIToken="+t.id+"="+t.secret)
}

// ---- ticketCreds ----

// ticketCreds holds a live ticket and CSRF token. A zero expiry means the
// ticket was supplied by the caller, who owns its lifetime, so needsRefresh
// reports false.
type ticketCreds struct {
	mu     sync.Mutex
	ticket string
	csrf   string
	expiry time.Time
}

func (t *ticketCreds) needsRefresh() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.expiry.IsZero() {
		return false
	}
	return time.Now().Add(ticketSkew).After(t.expiry)
}

// refresh is a no-op: a pre-supplied ticket cannot be re-minted without
// credentials. UserCredentials is the refreshable variant.
func (*ticketCreds) refresh(context.Context, rawDoer, bool) error { return nil }

func (t *ticketCreds) authorize(r *http.Request) {
	t.mu.Lock()
	ticket := t.ticket
	t.mu.Unlock()
	r.AddCookie(&http.Cookie{Name: "PVEAuthCookie", Value: ticket})
}

func (t *ticketCreds) csrfToken() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.csrf
}

func (t *ticketCreds) setTicket(ticket, csrf string, expiry time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ticket = ticket
	t.csrf = csrf
	t.expiry = expiry
}

// ---- userCreds ----

// userCreds mints and refreshes tickets from a username/password. It embeds a
// ticketCreds for the live ticket and serialises minting with mintMu so
// concurrent requests do not mint twice.
type userCreds struct {
	user     string
	password string
	otp      string
	inner    ticketCreds
	mintMu   sync.Mutex
}

func (u *userCreds) needsRefresh() bool {
	u.inner.mu.Lock()
	defer u.inner.mu.Unlock()
	if u.inner.expiry.IsZero() {
		return true // not yet minted
	}
	return time.Now().Add(ticketSkew).After(u.inner.expiry)
}

// refresh POSTs /access/ticket to mint a new ticket. It serialises on mintMu
// and re-checks need under the lock, so a goroutine that waited while another
// minted returns without minting again. force=true mints unconditionally — used
// when the server rejected the current ticket as expired.
func (u *userCreds) refresh(ctx context.Context, do rawDoer, force bool) error {
	u.mintMu.Lock()
	defer u.mintMu.Unlock()

	if !force && !u.needsRefresh() {
		return nil
	}

	body := struct {
		Username string `json:"username"`
		Password string `json:"password"`
		OTP      string `json:"otp,omitempty"`
	}{Username: u.user, Password: u.password, OTP: u.otp}

	var resp struct {
		Ticket              string `json:"ticket"`
		CSRFPreventionToken string `json:"CSRFPreventionToken"`
	}
	if err := do(ctx, http.MethodPost, "/access/ticket", body, &resp); err != nil {
		return fmt.Errorf("api: mint ticket: %w", err)
	}
	if resp.Ticket == "" {
		return fmt.Errorf("api: mint ticket: empty ticket in response")
	}

	u.inner.setTicket(resp.Ticket, resp.CSRFPreventionToken, time.Now().Add(ticketLifetime))
	return nil
}

func (u *userCreds) authorize(r *http.Request) { u.inner.authorize(r) }
func (u *userCreds) csrfToken() string         { return u.inner.csrfToken() }
