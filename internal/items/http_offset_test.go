package items

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/httpx"
)

// AOC-012, verify round 3. `offset` is parsed as a Go int and cast to int32 for the query, with
// nothing in between checking it fits.
//
// Measured against the live endpoint on the real corpus:
//
//	offset=2147483647  → 200, empty page, total 4646   (correct — representable, just past the end)
//	offset=2147483648  → 500 internal error            (wraps to -2147483648; Postgres rejects it)
//	offset=4294967296  → 200 with the FIRST PAGE of items, and "offset": 4294967296 echoed back
//
// The last one is the load-bearing case: a 200 whose rows contradict its own envelope. A caller
// paging on `offset` silently restarts at the beginning while being told it is four billion rows
// in. /v1 is a public contract (CLAUDE.md rule 5c), and the HTML armory page calls the same
// service, so both surfaces carry it.
//
// parseFilters already decided that an out-of-range offset is a 400 — it rejects offset < 0 right
// below this. The upper bound is the same decision, missed.
//
// Asserted on parseFilters rather than through the handler on purpose: this is a request-validation
// question, and it must be answered before anything reaches a querier.
func TestAnOffsetTooLargeForTheQueryIsRejectedNotWrapped(t *testing.T) {
	parse := func(t *testing.T, raw string) (Filters, error) {
		t.Helper()
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/items?offset="+raw, nil)
		return parseFilters(req)
	}

	for _, tc := range []struct {
		name   string
		offset string
	}{
		{"one past the int32 ceiling — a 500 today", strconv.FormatInt(math.MaxInt32+1, 10)},
		{"a multiple of 2^32, which wraps to 0 and serves page one", "4294967296"},
		{"the int64 ceiling", strconv.FormatInt(math.MaxInt64, 10)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, err := parse(t, tc.offset)
			if err == nil {
				t.Fatalf("offset=%s was accepted as %d; an offset the query cannot represent must "+
					"be rejected like a negative one, never wrapped", tc.offset, f.Offset)
			}
			if !errors.Is(err, httpx.ErrInvalid) {
				t.Errorf("offset=%s → %v; want an ErrInvalid so the mapper answers 400, not 500",
					tc.offset, err)
			}
		})
	}

	// The boundary itself stays a legal request: it is representable, it is merely past the end.
	if _, err := parse(t, strconv.FormatInt(math.MaxInt32, 10)); err != nil {
		t.Errorf("offset=%d → %v; the largest representable offset is a valid request", math.MaxInt32, err)
	}
	// And the rejection that already exists must not regress.
	if _, err := parse(t, "-1"); !errors.Is(err, httpx.ErrInvalid) {
		t.Errorf("offset=-1 → %v, want ErrInvalid", err)
	}
}
