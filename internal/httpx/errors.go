package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
)

// The sentinel errors a service may return. A service knows it could not find a thing;
// it does not know that "not found" is 404. That translation happens here, once.
//
// This is the ONLY place an HTTP status is chosen for an error. `http.Error` is banned
// in this repo (bin/gate greps for it): it writes text/plain, skips the request id, and
// spreads status decisions across every handler until no two 404s look the same.
var (
	ErrNotFound     = errors.New("not found")
	ErrInvalid      = errors.New("invalid request")
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
	ErrConflict     = errors.New("conflict")

	// ErrMethodNotAllowed exists so chi's 405 can go through Fail like every other
	// rejection, rather than being the one status that writes its own body.
	ErrMethodNotAllowed = errors.New("method not allowed")
)

// ErrorBody is the single shape of every error response. Clients can rely on it.
type ErrorBody struct {
	Error     string `json:"error"`
	RequestID string `json:"request_id,omitempty"`
}

// Fail writes err as JSON with the status it maps to, and logs it with the request id.
//
// The message sent to the client is the SENTINEL's text, never err's. A wrapped error
// often carries a table name, a column, a file path or a query fragment, and those
// belong in the log, not in a public response body.
func Fail(w http.ResponseWriter, r *http.Request, err error) {
	status := statusFor(err)
	rid := RequestIDFrom(r.Context())

	if status >= 500 {
		slog.ErrorContext(r.Context(), "request failed",
			"error", err, "status", status, "request_id", rid,
			"method", r.Method, "path", r.URL.Path)
	} else {
		slog.InfoContext(r.Context(), "request rejected",
			"error", err, "status", status, "request_id", rid,
			"method", r.Method, "path", r.URL.Path)
	}

	Respond(w, r, status, ErrorBody{Error: publicMessage(err, status), RequestID: rid})
}

func statusFor(err error) int {
	switch {
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrInvalid):
		return http.StatusBadRequest
	case errors.Is(err, ErrUnauthorized):
		return http.StatusUnauthorized
	case errors.Is(err, ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, ErrConflict):
		return http.StatusConflict
	case errors.Is(err, ErrMethodNotAllowed):
		return http.StatusMethodNotAllowed
	default:
		return http.StatusInternalServerError
	}
}

// publicMessage returns what the client is allowed to read. For 5xx it is always the
// same flat string: an unmapped error is by definition one we did not anticipate, so we
// cannot know what it contains.
func publicMessage(err error, status int) string {
	if status >= 500 {
		return "internal error"
	}
	for _, sentinel := range []error{ErrNotFound, ErrInvalid, ErrUnauthorized, ErrForbidden,
		ErrConflict, ErrMethodNotAllowed} {
		if errors.Is(err, sentinel) {
			return sentinel.Error()
		}
	}
	return http.StatusText(status)
}

// Respond writes v as JSON with the given status.
func Respond(w http.ResponseWriter, r *http.Request, status int, v any) {
	buf, err := json.Marshal(v)
	if err != nil {
		// Encoding our own response failed, so the body is unusable. Say so in the
		// status rather than sending a 200 with half a JSON document.
		slog.ErrorContext(r.Context(), "encoding response failed", "error", err)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":"internal error"}`)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf)
}
