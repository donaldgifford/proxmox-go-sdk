package api

import (
	"math/rand/v2"
	"time"
)

// RetryPolicy governs per-endpoint retry before the transport rotates to the
// next cluster node. Each endpoint gets Attempts tries with exponential
// backoff; only then does failover advance (sticky+passive, OQ-2).
type RetryPolicy struct {
	// Attempts is the number of tries per endpoint.
	Attempts int
	// InitialDelay is the wait before the first retry.
	InitialDelay time.Duration
	// MaxDelay caps the exponential growth.
	MaxDelay time.Duration
	// BackoffFactor multiplies the delay each attempt.
	BackoffFactor float64
	// Jitter adds up to 50% of the delay at random to avoid thundering herds.
	Jitter bool
}

// DefaultRetryPolicy returns a policy suitable for most PVE clusters: three
// attempts, 200ms initial backoff doubling to a 10s cap, with jitter.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		Attempts:      3,
		InitialDelay:  200 * time.Millisecond,
		MaxDelay:      10 * time.Second,
		BackoffFactor: 2.0,
		Jitter:        true,
	}
}

// delay returns the backoff before the given 1-indexed retry attempt.
func (p RetryPolicy) delay(attempt int) time.Duration {
	d := p.InitialDelay
	for i := 1; i < attempt; i++ {
		d = time.Duration(float64(d) * p.BackoffFactor)
		if d >= p.MaxDelay {
			d = p.MaxDelay
			break
		}
	}
	if p.Jitter {
		if half := int64(d) / 2; half > 0 {
			d += time.Duration(rand.Int64N(half)) //nolint:gosec // backoff jitter, not security-sensitive
		}
	}
	return d
}
