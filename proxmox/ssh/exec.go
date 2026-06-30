package ssh

import (
	"context"
	"fmt"
)

// Exec runs cmd on the node over SSH and returns its combined stdout+stderr.
// Connect must have been called. It is intended for the rare host-level
// operation with no REST equivalent; prefer the typed REST services otherwise.
func (c *Client) Exec(ctx context.Context, cmd string) ([]byte, error) {
	if c.ssh == nil {
		return nil, fmt.Errorf("ssh.Exec: %w", errNotConnected)
	}
	session, err := c.ssh.NewSession()
	if err != nil {
		return nil, fmt.Errorf("ssh.Exec: new session: %w", err)
	}
	defer closeQuietly(session)

	type result struct {
		out []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, runErr := session.CombinedOutput(cmd)
		done <- result{out: out, err: runErr}
	}()

	select {
	case <-ctx.Done():
		closeQuietly(session) // unblock CombinedOutput.
		<-done                // wait for the goroutine to finish before returning.
		return nil, fmt.Errorf("ssh.Exec: %w", ctx.Err())
	case res := <-done:
		if res.err != nil {
			return res.out, fmt.Errorf("ssh.Exec: run %q: %w", cmd, res.err)
		}
		return res.out, nil
	}
}
