package api

import (
	"testing"
	"time"
)

func TestRetryPolicyDelayBackoff(t *testing.T) {
	p := RetryPolicy{
		Attempts:      5,
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      1 * time.Second,
		BackoffFactor: 2.0,
		Jitter:        false,
	}
	want := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		800 * time.Millisecond,
		1000 * time.Millisecond, // capped at MaxDelay
	}
	for i, w := range want {
		if got := p.delay(i + 1); got != w {
			t.Errorf("delay(%d) = %v, want %v", i+1, got, w)
		}
	}
}

func TestRetryPolicyDelayJitterBounds(t *testing.T) {
	p := RetryPolicy{
		Attempts:      3,
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      1 * time.Second,
		BackoffFactor: 2.0,
		Jitter:        true,
	}
	for i := 0; i < 100; i++ {
		d := p.delay(1)
		if d < 100*time.Millisecond || d >= 150*time.Millisecond {
			t.Fatalf("jittered delay(1) = %v, want [100ms, 150ms)", d)
		}
	}
}

// TestRetryPolicyDelayJitterZeroGuard ensures the jitter math does not panic
// when the half-delay rounds to zero.
func TestRetryPolicyDelayJitterZeroGuard(t *testing.T) {
	p := RetryPolicy{Attempts: 1, InitialDelay: 1, MaxDelay: 1, BackoffFactor: 2.0, Jitter: true}
	if got := p.delay(1); got != 1 {
		t.Errorf("delay(1) = %v, want 1ns (no jitter added when half rounds to 0)", got)
	}
}
