package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"
)

type ctxKey int

const requestIDKey ctxKey = iota

// HeaderRequestID is echoed on every response so a user can quote it in a bug report
// and we can find the one log line that matters.
const HeaderRequestID = "X-Request-Id"

// RequestID attaches an id to the context and the response. An inbound
// X-Request-Id is trusted only for correlation and is length-capped, because it is
// attacker-controlled and ends up in our logs.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := r.Header.Get(HeaderRequestID)
		if rid == "" || len(rid) > 64 {
			rid = newID()
		}
		w.Header().Set(HeaderRequestID, rid)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, rid)))
	})
}

// RequestIDFrom returns the id, or "" outside a request.
func RequestIDFrom(ctx context.Context) string {
	rid, _ := ctx.Value(requestIDKey).(string)
	return rid
}

func newID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failing is not something to paper over with a weaker id, but it
		// must not take the request down either: correlation degrades, service does not.
		return "norand"
	}
	return hex.EncodeToString(b[:])
}

// Recover turns a panic into a 500 through the central mapper. Without it, chi's
// default writes a stack trace to the response body.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			slog.ErrorContext(r.Context(), "panic in handler",
				"panic", rec, "request_id", RequestIDFrom(r.Context()),
				"method", r.Method, "path", r.URL.Path)

			// If the handler already started writing, the status line is spent and the
			// client holds a partial body. Appending an error document would produce a
			// 200 followed by two concatenated JSON objects -- worse than the truncation,
			// because it parses as neither. Log it and let the connection end short.
			if sw, ok := w.(interface{ Wrote() bool }); ok && sw.Wrote() {
				slog.ErrorContext(r.Context(), "panic after the response began; body is truncated",
					"request_id", RequestIDFrom(r.Context()))
				return
			}
			Fail(w, r, errPanic)
		}()
		next.ServeHTTP(w, r)
	})
}

// errPanic is unexported and maps to no sentinel, so it becomes a 500 with the flat
// "internal error" body — a panic's value must never reach a client.
var errPanic = errPanicType{}

type errPanicType struct{}

func (errPanicType) Error() string { return "panic recovered" }

// statusWriter records the status so Log can report it, and whether anything has been
// written yet so Recover knows whether a body is still possible.
type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusWriter) WriteHeader(code int) {
	if w.wrote {
		return
	}
	w.status, w.wrote = code, true
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	w.wrote = true
	return w.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the underlying writer, so wrapping does not
// silently remove Flush, Hijack or SetWriteDeadline from anything downstream.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Wrote reports whether the response has already begun. Once it has, the status line is
// gone and nothing can turn the response into a clean error.
func (w *statusWriter) Wrote() bool { return w.wrote }

// Log emits one structured line per request.
func Log(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		slog.InfoContext(r.Context(), "request",
			"method", r.Method, "path", r.URL.Path, "status", sw.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", RequestIDFrom(r.Context()))
	})
}

// NotFound is chi's 404 handler, routed through the central mapper so an unknown path
// returns the same JSON shape as every other error rather than chi's plain text.
func NotFound(w http.ResponseWriter, r *http.Request) { Fail(w, r, ErrNotFound) }

// MethodNotAllowed does the same for a known path with the wrong verb, on the MACHINE
// surface (/v1, /health, /assets). It goes through Fail like everything else there.
//
// ⚠️ Amended by AOC-024: on the HTML surface the router writes a small page directly
// instead of calling this, because the renderer cannot be trusted to render a failure that
// may be the renderer. The logging consequence the original comment warned about does not
// return -- Log records every request from statusWriter no matter who wrote the body
// (verified, AOC-024 verify round 2). It was the claim that went stale, not the behaviour.
func MethodNotAllowed(w http.ResponseWriter, r *http.Request) { Fail(w, r, ErrMethodNotAllowed) }
