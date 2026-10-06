package pages_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// AOC-051 verify round 3: the gear_id contract at the HTTP level. gear_id is the island's name for one
// of a browser's kept builds. A malformed one is a 400 on every surface; /v1/builds/compute answers the
// same with or without it and never echoes it; and without JavaScript nothing ever makes one (a "+"
// with no id answers a 303 with none, and with one the 303 keeps it). Over the fake gear corpus
// (test-* rows only).

func TestVerifyAMalformedGearIDIsA400OnEverySurface(t *testing.T) {
	h := gearRouter(t, gearCorpus())
	for _, id := range []string{"ABC", "a-b", strings.Repeat("a", 25), "%C3%A9", "a%20b"} {
		for _, c := range []struct {
			path string
			hdr  map[string]string
		}{
			{"/armory?gear=test-head:1&gear_id=" + id, nil},
			{"/armory?gear=test-head:1&gear_id=" + id, map[string]string{"HX-Request": "true"}},
			{"/v1/builds/compute?gear=test-head:1&gear_id=" + id, nil},
		} {
			if rr := get(t, h, http.MethodGet, c.path, c.hdr, ""); rr.Code != http.StatusBadRequest {
				t.Errorf("%s (htmx %v): %d, want 400", c.path, c.hdr != nil, rr.Code)
			}
		}
	}
}

func TestVerifyGearIDChangesNothingV1ComputesAndIsNotEchoed(t *testing.T) {
	h := gearRouter(t, gearCorpus())
	for _, q := range []string{"gear=", "gear=test-head:1&gear=test-off:3", "gear=test-main:2&gear_class=test-other"} {
		without := get(t, h, http.MethodGet, "/v1/builds/compute?"+q, nil, "")
		with := get(t, h, http.MethodGet, "/v1/builds/compute?"+q+"&gear_id=verify3id", nil, "")
		if without.Code != http.StatusOK || with.Code != http.StatusOK {
			t.Fatalf("%s: %d without the id, %d with it", q, without.Code, with.Code)
		}
		if with.Body.String() != without.Body.String() {
			t.Errorf("%s: the id changed /v1's answer", q)
		}
		if strings.Contains(with.Body.String(), "verify3id") {
			t.Errorf("%s: /v1 echoes the id", q)
		}
	}
}

var gearIDOnAPage = regexp.MustCompile(`gear_id=|name="gear_id"`)

func TestVerifyWithoutJavaScriptNoGearIDIsEverMade(t *testing.T) {
	h := gearRouter(t, gearCorpus())
	rr := get(t, h, http.MethodGet, "/armory?add=2", nil, "")
	if rr.Code != http.StatusSeeOther {
		t.Fatalf(`"+" with no build: %d, want 303`, rr.Code)
	}
	loc := rr.Header().Get("Location")
	if strings.Contains(loc, "gear_id") {
		t.Errorf("a 303 made an id: %s", loc)
	}
	if body := get(t, h, http.MethodGet, loc, nil, "").Body.String(); gearIDOnAPage.MatchString(body) {
		t.Errorf("%s: a page reached without an id carries one", loc)
	}
	// An id the URL already holds is kept by the 303 (it names the reader's kept build).
	rr = get(t, h, http.MethodGet, "/armory?gear=test-main:2&gear_id=verify3id&add=1", nil, "")
	if loc := rr.Header().Get("Location"); rr.Code != http.StatusSeeOther || !strings.Contains(loc, "gear_id=verify3id") {
		t.Errorf(`"+" on a build with an id: %d %q, want a 303 keeping the id`, rr.Code, loc)
	}
}
