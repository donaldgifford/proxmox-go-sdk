package api

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/pverr"
)

const (
	apiPrefix        = "/api2/json"
	defaultUserAgent = "proxmox-go-sdk"
)

// logger is the minimal sink the transport logs debug lines to. The consumer
// supplies one via WithLogger; the zero behaviour is the no-op below. A
// *slog.Logger satisfies it.
type logger interface {
	Debug(msg string, args ...any)
}

type noopLogger struct{}

func (noopLogger) Debug(string, ...any) {}

// transport is the concrete Client. It is immutable after New; all mutable
// state lives behind locks in connection and the credentials.
type transport struct {
	conn      *connection
	creds     Credentials
	policy    RetryPolicy
	http      *http.Client
	userAgent string
	logger    logger
}

// TransportOption configures New.
type TransportOption func(*transportConfig)

type transportConfig struct {
	policy         RetryPolicy
	httpClient     *http.Client
	insecure       bool
	minTLS         uint16
	requestTimeout time.Duration
	userAgent      string
	logger         logger
	extraEndpoints []Endpoint
}

// New builds a Client targeting one PVE cluster, reachable at primary and
// authenticated by creds. It is safe for concurrent use.
func New(primary string, creds Credentials, opts ...TransportOption) (Client, error) {
	if creds == nil {
		return nil, fmt.Errorf("api: nil credentials")
	}

	cfg := transportConfig{
		policy:    DefaultRetryPolicy(),
		minTLS:    tls.VersionTLS12,
		userAgent: defaultUserAgent,
		logger:    noopLogger{},
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	conn, err := newConnection(primary, cfg.extraEndpoints)
	if err != nil {
		return nil, err
	}

	httpClient := cfg.httpClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: cfg.requestTimeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: cfg.insecure, //nolint:gosec // self-signed/IP PVE hosts are a documented, opt-in case
					MinVersion:         cfg.minTLS,
				},
			},
		}
	}

	return &transport{
		conn:      conn,
		creds:     creds,
		policy:    cfg.policy,
		http:      httpClient,
		userAgent: cfg.userAgent,
		logger:    cfg.logger,
	}, nil
}

// WithRetryPolicy overrides the default per-endpoint retry policy.
func WithRetryPolicy(p RetryPolicy) TransportOption {
	return func(c *transportConfig) { c.policy = p }
}

// WithHTTPClient supplies a pre-built *http.Client, bypassing the TLS and
// timeout options (the caller owns the client's configuration).
func WithHTTPClient(h *http.Client) TransportOption {
	return func(c *transportConfig) { c.httpClient = h }
}

// WithInsecureSkipVerify disables TLS certificate verification — needed for
// self-signed or IP-only PVE hosts. Off by default.
func WithInsecureSkipVerify(v bool) TransportOption {
	return func(c *transportConfig) { c.insecure = v }
}

// WithMinTLS sets the minimum TLS version (default tls.VersionTLS12).
func WithMinTLS(v uint16) TransportOption {
	return func(c *transportConfig) { c.minTLS = v }
}

// WithRequestTimeout sets the per-request timeout on the default HTTP client.
func WithRequestTimeout(d time.Duration) TransportOption {
	return func(c *transportConfig) { c.requestTimeout = d }
}

// WithUserAgent overrides the User-Agent header sent on every request.
func WithUserAgent(ua string) TransportOption {
	return func(c *transportConfig) { c.userAgent = ua }
}

// WithLogger supplies a debug logger (e.g. a *slog.Logger). Default: no-op.
func WithLogger(l logger) TransportOption {
	return func(c *transportConfig) {
		if l != nil {
			c.logger = l
		}
	}
}

// WithClusterEndpoints appends additional cluster node addresses for
// transport-level failover (see Endpoint and OQ-2).
func WithClusterEndpoints(eps ...Endpoint) TransportOption {
	return func(c *transportConfig) { c.extraEndpoints = append(c.extraEndpoints, eps...) }
}

// HTTP returns the underlying *http.Client (escape hatch).
func (t *transport) HTTP() *http.Client { return t.http }

// ExpandPath prepends /api2/json to a relative path, ensuring a leading slash.
// It does not interpolate node or vmid; services build those segments. A path
// already under /api2/json is returned unchanged (escape-hatch guard).
func (*transport) ExpandPath(path string) string {
	if strings.HasPrefix(path, apiPrefix) {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return apiPrefix + path
}

// DoRequest performs one PVE call. It lazily refreshes credentials, runs the
// request through the retry+failover loop, and on ticket expiry re-authenticates
// and replays once.
func (t *transport) DoRequest(ctx context.Context, method, path string, body, out any) error {
	if t.creds.needsRefresh() {
		if err := t.creds.refresh(ctx, t.doRaw, false); err != nil {
			return err
		}
	}

	err := t.execute(ctx, method, path, body, out)
	if err == nil {
		return nil
	}

	if errors.Is(err, pverr.ErrTicketExpired) {
		if rerr := t.creds.refresh(ctx, t.doRaw, true); rerr != nil {
			return rerr
		}
		return t.execute(ctx, method, path, body, out)
	}
	return err
}

// execute runs the request with per-endpoint retry and sticky failover.
func (t *transport) execute(ctx context.Context, method, path string, body, out any) error {
	expandedPath := t.ExpandPath(path)
	total := t.conn.count()

	for ep := 0; ep < total; ep++ {
		base := t.conn.baseURL()
		fullURL := base.String() + expandedPath

		var lastErr error
		for attempt := 1; attempt <= t.policy.Attempts; attempt++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			t.logger.Debug("proxmox request", "method", method, "url", fullURL, "attempt", attempt)

			resp, err := t.doHTTP(ctx, method, fullURL, body)
			if err != nil {
				lastErr = pverr.ClassifyNetError(expandedPath, err)
				if attempt < t.policy.Attempts {
					wait(ctx, t.policy.delay(attempt))
				}
				continue
			}

			apiErr := readResponse(resp, expandedPath, out)
			if apiErr == nil {
				return nil
			}
			if !errors.Is(apiErr, pverr.ErrTransient) {
				return apiErr // 4xx / task failure: do not retry or rotate
			}
			lastErr = apiErr
			if attempt < t.policy.Attempts {
				wait(ctx, t.policy.delay(attempt))
			}
		}

		if !t.conn.failover() {
			return lastErr
		}
	}

	return fmt.Errorf("api: all %d endpoints exhausted: %w", total, pverr.ErrTransient)
}

func isWriteMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodDelete:
		return true
	default:
		return false
	}
}

// doHTTP builds and sends one authenticated HTTP request.
func (t *transport) doHTTP(ctx context.Context, method, fullURL string, body any) (*http.Response, error) {
	var bodyReader io.Reader
	write := isWriteMethod(method)
	if body != nil && write {
		encoded, err := formEncode(body)
		if err != nil {
			return nil, err
		}
		bodyReader = strings.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("api: build request: %w", err)
	}
	if t.userAgent != "" {
		req.Header.Set("User-Agent", t.userAgent)
	}
	if write {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	t.creds.authorize(req)
	if write {
		if csrf := t.creds.csrfToken(); csrf != "" {
			req.Header.Set("CSRFPreventionToken", csrf)
		}
	}

	return t.http.Do(req) //nolint:wrapcheck // wrapped by the caller via ClassifyNetError
}

// doRaw sends an unauthenticated POST, used by UserCredentials.refresh to mint
// a ticket (the credentials travel in the body, not headers).
func (t *transport) doRaw(ctx context.Context, method, path string, body, out any) error {
	base := t.conn.baseURL()
	expandedPath := t.ExpandPath(path)
	fullURL := base.String() + expandedPath

	encoded, err := formEncode(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, strings.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("api: build request: %w", err)
	}
	if t.userAgent != "" {
		req.Header.Set("User-Agent", t.userAgent)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := t.http.Do(req)
	if err != nil {
		return pverr.ClassifyNetError(expandedPath, err)
	}
	return readResponse(resp, expandedPath, out)
}

// pveEnvelope is the outer JSON wrapper PVE returns on every call.
type pveEnvelope struct {
	Data    json.RawMessage   `json:"data"`
	Errors  map[string]string `json:"errors"`
	Message string            `json:"message"`
}

// readResponse consumes the response, unwraps the envelope, decodes data into
// out (when non-nil), and classifies any error.
func readResponse(resp *http.Response, path string, out any) error {
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return pverr.ClassifyNetError(path, err)
	}

	var env pveEnvelope
	if len(raw) > 0 {
		if uerr := json.Unmarshal(raw, &env); uerr != nil {
			// On a 2xx the decoded body matters; on an error status the code
			// already classifies the failure, so a non-JSON body is tolerated
			// (e.g. an HTML error page from a fronting proxy).
			if isSuccess(resp.StatusCode) && out != nil {
				return fmt.Errorf("api: decode response from %s: %w", path, uerr)
			}
		}
	}

	if !isSuccess(resp.StatusCode) {
		return pverr.Classify(resp.StatusCode, path, pverr.PVEBody{Message: env.Message, Errors: env.Errors}, nil)
	}

	if out != nil && len(env.Data) > 0 {
		if uerr := json.Unmarshal(env.Data, out); uerr != nil {
			return fmt.Errorf("api: decode response data from %s: %w", path, uerr)
		}
	}
	return nil
}

func isSuccess(status int) bool { return status >= 200 && status < 300 }

// formEncode renders v as application/x-www-form-urlencoded. A url.Values is
// encoded directly; anything else is marshalled to JSON and flattened, so
// structs with json tags (and omitempty) and types.PVEBool work as expected.
// PVE request bodies are flat, so nested objects are not supported here — pass
// url.Values for those.
func formEncode(v any) (string, error) {
	if v == nil {
		return "", nil
	}
	if vals, ok := v.(url.Values); ok {
		return vals.Encode(), nil
	}

	data, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("api: marshal form body: %w", err)
	}
	var flat map[string]json.RawMessage
	if err := json.Unmarshal(data, &flat); err != nil {
		return "", fmt.Errorf("api: form body must encode to a JSON object: %w", err)
	}

	vals := make(url.Values, len(flat))
	for key, raw := range flat {
		s := string(raw)
		if s != "" && s[0] == '"' {
			var str string
			if err := json.Unmarshal(raw, &str); err != nil {
				return "", fmt.Errorf("api: decode form field %q: %w", key, err)
			}
			s = str
		}
		vals.Set(key, s)
	}
	return vals.Encode(), nil
}

// wait sleeps for d or until ctx is cancelled, whichever comes first.
func wait(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}
