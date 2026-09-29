package httpx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/pierrehrt/aoc-api/internal/httpx"
)

// AOC-026 verify round 2. TestCacheWriterPreservesFlush reaches the flush through
// http.ResponseController only, and the http.Flusher case of TestAFlushCannotGetPastThePolicy reads
// the header but not whether anything was flushed. So a cacheWriter.Flush that did nothing passed
// the whole suite (measured: the mutant `func (w *cacheWriter) Flush() {}` survived). A handler that
// streams through http.Flusher must really flush, through the production chain too (Log's writer
// sits underneath and has only Unwrap), and the flush must still carry the policy.
func TestCacheWriterFlusherReallyFlushes(t *testing.T) {
	var isFlusher bool
	stream := func(w http.ResponseWriter, _ *http.Request) {
		var f http.Flusher
		if f, isFlusher = w.(http.Flusher); isFlusher {
			f.Flush()
		}
	}
	for name, h := range map[string]http.Handler{
		"Cache alone": httpx.Cache(http.HandlerFunc(stream)),
		"production router": httpx.NewRouterWithSite(httpx.Build{Version: "dev", Commit: "none", Env: "test"},
			func(r chi.Router) { r.Get("/test-stream", stream) }, nil),
	} {
		t.Run(name, func(t *testing.T) {
			isFlusher = false
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/test-stream", nil))
			if !isFlusher {
				t.Fatal("the handler's writer is not an http.Flusher")
			}
			if !rr.Flushed {
				t.Error("a flush through http.Flusher never reached the writer underneath — streaming is broken")
			}
			if got := rr.Result().Header.Get("Cache-Control"); got != wantPage {
				t.Errorf("Cache-Control = %q, want %q", got, wantPage)
			}
		})
	}
}
