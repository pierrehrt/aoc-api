package pages_test

import (
	"net/http"
	"testing"
)

// AOC-050, verify round 2. `get` without a `source` is refused by the service (verify round 1, F6),
// and /v1/items answers it with a 400. /armory reads the same parameters with the same parser and
// the same service, but an error from List reaches the handler's fail path: a 500 logged at ERROR as
// "page render failed", a reader's own URL reported as an outage. Every other malformed filter is a
// 400 on this page (a bad sort, a level, an empty range), as plain HTML or, to htmx, the
// armory_invalid fragment.
func TestAHalfWithNoSourceIsA400OnTheArmoryToo(t *testing.T) {
	h := router(t)
	for _, path := range []string{"/armory?get=drop", "/armory?tab=pve&get=vendor"} {
		for _, hdr := range []map[string]string{nil, {"HX-Request": "true"}} {
			rr := get(t, h, http.MethodGet, path, hdr, "")
			if rr.Code != http.StatusBadRequest {
				t.Errorf("%s (HX-Request %v): status %d, want 400 as on /v1/items", path, hdr != nil, rr.Code)
			}
		}
	}
}
