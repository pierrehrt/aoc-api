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
			if rec := recover(); rec != nil {
				slog.ErrorContext(r.Context(), "panic in handler",
					"panic", rec, "request_id", RequestIDFrom(r.Context()),
					"method", r.Method, "path", r.URL.Path)
				Fail(w, r, errPanic)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// errPanic is unexported and maps to no sentinel, so it becomes a 500 with the flat
// "internal error" body — a panic's value must never reach a client.
var errPanic = errPanicType{}

type errPanicType struct{}

func (errPanicType) Error() string { return "panic recovered" }

// statusWriter records the status so Log can report it.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

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

// MethodNotAllowed does the same for a known path with the wrong verb.
func MethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	Respond(w, r, http.StatusMethodNotAllowed,
		ErrorBody{Error: "method not allowed", RequestID: RequestIDFrom(r.Context())})
}
