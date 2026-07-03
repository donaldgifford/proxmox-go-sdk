package pverr

import (
	"errors"
	"net"
	"net/http"
	"strings"
)

// PVEBody is the envelope PVE wraps responses in. Classification reads only the
// error fields; api.DoRequest decodes the "data" member separately. It is
// exported so the transport can hand the decoded body to Classify.
type PVEBody struct {
	Message string            `json:"message"`
	Errors  map[string]string `json:"errors"`
}

// Classify maps an HTTP status and decoded body to an *Error wrapping the
// appropriate sentinel. The transport calls it after reading a response. cause
// is the underlying transport error when one exists (otherwise nil).
func Classify(status int, path string, body PVEBody, cause error) *Error {
	e := &Error{
		Path:    path,
		Status:  status,
		Message: body.Message,
		Params:  body.Errors,
	}

	switch {
	case cause != nil && isNetworkError(cause):
		e.err = ErrTransient
		if e.Message == "" {
			e.Message = cause.Error()
		}
	case status == http.StatusNotFound:
		e.err = ErrNotFound
	case status == http.StatusConflict:
		e.err = ErrConflict
	case status == http.StatusUnauthorized:
		// PVE reports an expired/invalid ticket in the message; distinguish it
		// from a plain authentication failure so the transport can re-auth.
		msg := strings.ToLower(body.Message)
		if strings.Contains(msg, "ticket expired") || strings.Contains(msg, "invalid ticket") {
			e.err = ErrTicketExpired
		} else {
			e.err = ErrUnauthorized
		}
	case status == http.StatusForbidden:
		e.err = ErrForbidden
	case status == 596: // PVE-specific: connection-failure status, retryable.
		e.err = ErrTransient
	case status >= 500:
		e.err = ErrTransient
	default:
		// Other 4xx: no specific sentinel. Callers still get Status, Message,
		// and Params; errors.Is against a sentinel returns false.
		e.err = nil
	}

	return e
}

// ClassifyNetError wraps a transport/dial error that never produced an HTTP
// response (connection refused, TLS handshake, timeout) as an ErrTransient.
func ClassifyNetError(path string, err error) *Error {
	return &Error{
		Path:    path,
		Message: err.Error(),
		err:     ErrTransient,
	}
}

// isNetworkError reports whether err is a net.Error. Context cancellation and
// deadline errors are deliberately excluded: they are the caller's intent, not
// a transient fault to retry.
func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, net.ErrClosed) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}
