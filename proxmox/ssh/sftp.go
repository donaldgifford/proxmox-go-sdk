package ssh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
)

// errNotConnected is returned by the upload/exec methods when Connect has not
// run (or Close has been called).
var errNotConnected = errors.New("ssh: not connected (call Connect first)")

// UploadSnippet streams r to destPath on the node over SFTP. destPath is the
// absolute path on the PVE host, e.g.
// "/var/lib/vz/snippets/cloud-init.yaml". Intermediate directories must already
// exist. Connect must have been called.
func (c *Client) UploadSnippet(ctx context.Context, destPath string, r io.Reader) error {
	return c.upload(ctx, "UploadSnippet", destPath, r)
}

// UploadBackup streams r to destPath on the node over SFTP, e.g. a vzdump
// archive under "/var/lib/vz/dump/". Same contract as UploadSnippet.
func (c *Client) UploadBackup(ctx context.Context, destPath string, r io.Reader) error {
	return c.upload(ctx, "UploadBackup", destPath, r)
}

// upload is the shared SFTP create-and-copy. It honors ctx by closing the remote
// file if ctx is cancelled mid-copy, which unblocks the in-flight io.Copy.
func (c *Client) upload(ctx context.Context, op, destPath string, r io.Reader) error {
	if c.sftp == nil {
		return fmt.Errorf("ssh.%s: %w", op, errNotConnected)
	}
	if destPath == "" || !path.IsAbs(destPath) {
		return fmt.Errorf("ssh.%s: destPath must be an absolute path, got %q", op, destPath)
	}

	f, err := c.sftp.Create(destPath)
	if err != nil {
		return fmt.Errorf("ssh.%s: create %s: %w", op, destPath, err)
	}

	// Close the remote file when ctx is cancelled so a blocked Copy returns.
	// copyDone signals the watcher to stop; watcherDone lets upload wait for the
	// watcher goroutine to exit before returning, so it never outlives the call.
	copyDone := make(chan struct{})
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-ctx.Done():
			closeQuietly(f)
		case <-copyDone:
		}
	}()
	defer func() {
		close(copyDone)
		<-watcherDone
	}()

	if _, err := io.Copy(f, r); err != nil {
		closeQuietly(f)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("ssh.%s: %w", op, ctxErr)
		}
		return fmt.Errorf("ssh.%s: stream %s: %w", op, destPath, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("ssh.%s: close %s: %w", op, destPath, err)
	}
	return nil
}
