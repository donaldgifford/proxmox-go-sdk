package storage

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"

	"github.com/donaldgifford/proxmox-go-sdk/proxmox/internal/svcutil"
	"github.com/donaldgifford/proxmox-go-sdk/proxmox/tasks"
)

// UploadSpec carries a streaming upload. Filename is the name PVE stores the
// object as; Reader supplies the bytes and is streamed (never buffered whole).
// Pass it by pointer.
type UploadSpec struct {
	Filename string    // required, e.g. "debian-12.iso".
	Reader   io.Reader // required; streamed to PVE.
}

// UploadISO streams an ISO image to a storage's "iso" content and returns the
// import task.
func (s *Service) UploadISO(ctx context.Context, node, storage string, spec *UploadSpec) (tasks.Ref, error) {
	return s.upload(ctx, node, storage, "iso", spec)
}

// UploadDiskImage streams a disk image to a storage's "import" content and
// returns the import task.
func (s *Service) UploadDiskImage(ctx context.Context, node, storage string, spec *UploadSpec) (tasks.Ref, error) {
	return s.upload(ctx, node, storage, "import", spec)
}

// upload performs the shared streaming multipart POST to .../upload. It pipes a
// multipart body so the file is never buffered whole: a goroutine writes the
// form fields and copies Reader into the pipe while DoUpload streams the read
// end to PVE.
func (s *Service) upload(ctx context.Context, node, storage, content string, spec *UploadSpec) (tasks.Ref, error) {
	if spec == nil {
		return tasks.Ref{}, fmt.Errorf("storage.Upload: %w", svcutil.ErrNilSpec)
	}
	if spec.Filename == "" || spec.Reader == nil {
		return tasks.Ref{}, fmt.Errorf("storage.Upload: filename and reader: %w", svcutil.ErrMissingField)
	}

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		err := writeUploadParts(mw, content, spec)
		// Closing the multipart writer flushes the trailing boundary; propagate
		// any error to the reader so DoUpload sees a failed body.
		if cerr := mw.Close(); err == nil {
			err = cerr
		}
		_ = pw.CloseWithError(err)
	}()

	var upid string
	path := nodeStoragePath(node, storage) + "/upload"
	if err := s.c.DoUpload(ctx, path, pr, mw.FormDataContentType(), &upid); err != nil {
		// Closing the read end makes the writer goroutine's blocked Write return
		// ErrClosedPipe, so it cannot leak. (*io.PipeReader).Close never errors;
		// the io.Closer-typed value keeps errcheck satisfied.
		var rc io.Closer = pr
		_ = rc.Close()
		return tasks.Ref{}, fmt.Errorf("storage.Upload: %w", err)
	}
	return svcutil.TaskRef("storage.Upload", upid)
}

// writeUploadParts writes the content/filename fields and streams the file part.
func writeUploadParts(mw *multipart.Writer, content string, spec *UploadSpec) error {
	if err := mw.WriteField("content", content); err != nil {
		return fmt.Errorf("write content field: %w", err)
	}
	if err := mw.WriteField("filename", spec.Filename); err != nil {
		return fmt.Errorf("write filename field: %w", err)
	}
	part, err := mw.CreateFormFile("filename", spec.Filename)
	if err != nil {
		return fmt.Errorf("create file part: %w", err)
	}
	if _, err := io.Copy(part, spec.Reader); err != nil {
		return fmt.Errorf("stream file: %w", err)
	}
	return nil
}
