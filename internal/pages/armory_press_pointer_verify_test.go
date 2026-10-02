package pages_test

// AOC-065 delta verify 9 — a test added by the independent verify. Fake corpus only; nothing here
// states a fact about the game.
//
// The behaviour below was measured in headless Chrome on the real corpus (the ticket's Log, delta
// verify 9): a link sent during a touch press, then a scroll that cancels the touch, ends on the
// link's state (N12); another pointer lifting or cancelling does not end, or put back, a finger's
// drag. No JavaScript engine runs in this suite, so this pins the three lines that carry it, read
// from the page as served, and in the order that makes them work. It fails at bd7f18f, where none of
// them exists.

import (
	"net/http"
	"strings"
	"testing"
)

// jsBlock returns the text from the first occurrence of start to the brace that closes the first
// block opened after it. The armory's script has no braces inside strings, so counting is enough.
func jsBlock(t *testing.T, src, start string) string {
	t.Helper()
	i := strings.Index(src, start)
	if i < 0 {
		t.Fatalf("the armory's script has no %q", start)
	}
	open := strings.Index(src[i:], "{")
	if open < 0 {
		t.Fatalf("no block after %q", start)
	}
	depth := 0
	for j := i + open; j < len(src); j++ {
		switch src[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[i : j+1]
			}
		}
	}
	t.Fatalf("the block after %q never closes", start)
	return ""
}

// before fails unless a occurs in block, b occurs in block, and a comes first.
func before(t *testing.T, name, block, a, b string) {
	t.Helper()
	ia, ib := strings.Index(block, a), strings.Index(block, b)
	switch {
	case ia < 0:
		t.Errorf("%s: missing %q", name, a)
	case ib < 0:
		t.Errorf("%s: missing %q", name, b)
	case ia > ib:
		t.Errorf("%s: %q must come before %q", name, a, b)
	}
}

func TestAPressIsPutBackToALinksStateAndEndsOnlyOnItsOwnPointer(t *testing.T) {
	rr := get(t, router(t), http.MethodGet, "/armory", nil, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	body := rr.Body.String()

	// The press remembers its own pointer.
	down := jsBlock(t, body, `aocPane.addEventListener("pointerdown", function (e) {`)
	if !strings.Contains(down, "aocPressId = e.pointerId") {
		t.Error("the slider's pointerdown does not record its pointerId: any pointer's end would end the press")
	}

	// N12: a link's state written into the form during a press is what that press is put back to.
	// It must be read after the sliders are set from the link's bounds, not before.
	form := jsBlock(t, body, "function aocFormFrom(url) {")
	before(t, "aocFormFrom", form, `querySelectorAll("[data-range]").forEach(aocRangeFromBounds)`, "aocPressFrom = aocPress.value")

	// Another pointer's cancel neither puts back nor ends this press: the check comes first.
	cancel := jsBlock(t, body, `document.addEventListener("pointercancel", function (e) {`)
	before(t, "pointercancel", cancel, "e.pointerId !== aocPressId", "s.value = aocPressFrom")
	before(t, "pointercancel", cancel, "e.pointerId !== aocPressId", "aocPressEnd(e)")

	// Another pointer's lift does not end this press (which would let an answer replace the slider
	// under the finger mid-drag, F19).
	end := jsBlock(t, body, "function aocPressEnd(e) {")
	before(t, "aocPressEnd", end, "e.pointerId !== aocPressId", "aocPress = null")
	if !strings.Contains(body, `document.addEventListener("pointerup", aocPressEnd);`) {
		t.Error("pointerup no longer reaches aocPressEnd with its event")
	}
}
