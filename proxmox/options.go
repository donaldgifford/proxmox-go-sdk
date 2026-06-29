package proxmox

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/api"
)

// Logger is the minimal logging sink the SDK writes debug lines to; a
// *slog.Logger satisfies it. A library must not log on its own initiative, so
// the default is a no-op and the SDK prescribes nothing.
type Logger interface {
	Debug(msg string, args ...any)
}

var _ Logger = (*slog.Logger)(nil)

// Option configures a Client at construction. Most options are thin adapters
// over the transport's options; NewClient applies them when building the
// underlying api.Client.
type Option func(*clientConfig)

type clientConfig struct {
	transport []api.TransportOption
}

// WithHTTPClient supplies a pre-built *http.Client (bring your own pooled
// client); it bypasses the SDK's TLS and timeout options.
func WithHTTPClient(h *http.Client) Option {
	return func(c *clientConfig) { c.transport = append(c.transport, api.WithHTTPClient(h)) }
}

// WithLogger sets the debug logger. A nil logger is ignored; the default is a
// no-op.
func WithLogger(l Logger) Option {
	return func(c *clientConfig) {
		if l != nil {
			c.transport = append(c.transport, api.WithLogger(l))
		}
	}
}

// WithRequestTimeout sets the per-request timeout on the default HTTP client.
func WithRequestTimeout(d time.Duration) Option {
	return func(c *clientConfig) { c.transport = append(c.transport, api.WithRequestTimeout(d)) }
}

// WithRetry overrides the per-endpoint retry/backoff policy.
func WithRetry(p api.RetryPolicy) Option {
	return func(c *clientConfig) { c.transport = append(c.transport, api.WithRetryPolicy(p)) }
}

// WithClusterEndpoints appends fallback cluster-node addresses for
// transport-level failover (the endpoint passed to NewClient is the primary).
func WithClusterEndpoints(eps ...api.Endpoint) Option {
	return func(c *clientConfig) { c.transport = append(c.transport, api.WithClusterEndpoints(eps...)) }
}

// WithMinTLS sets the minimum TLS version (default TLS 1.2).
func WithMinTLS(v uint16) Option {
	return func(c *clientConfig) { c.transport = append(c.transport, api.WithMinTLS(v)) }
}

// WithInsecureSkipVerify disables TLS certificate verification, for self-signed
// or IP-only PVE hosts. Off by default.
func WithInsecureSkipVerify(v bool) Option {
	return func(c *clientConfig) { c.transport = append(c.transport, api.WithInsecureSkipVerify(v)) }
}

// WithUserAgent overrides the User-Agent header sent on every request.
func WithUserAgent(ua string) Option {
	return func(c *clientConfig) { c.transport = append(c.transport, api.WithUserAgent(ua)) }
}
