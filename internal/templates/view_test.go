package templates_test

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/pierrehrt/aoc-api/internal/templates"
)

func TestTruncateCutsOnAWordBoundary(t *testing.T) {
	cases := []struct {
		name, in string
		max      int
		want     string
	}{
		{"short strings are untouched", "Vistrix", 60, "Vistrix"},
		{"exactly at the cap is untouched", "abcde", 5, "abcde"},
		{"one over the cap cuts at the space", "alpha bravo", 10, "alpha…"},
		// ⚠️ This case does NOT reach the walk-back loop: r[13] is a space, so the early
		// boundary branch returns first. It was named for a property it never exercised,
		// and a mutant reversing the walk survived because of it (verify round 1).
		{"cut lands exactly on a boundary", "one two three four", 14, "one two three…"},
		// These DO reach the walk-back loop — the cut falls mid-word with more than one
		// space behind it, so walking to the first space instead of the last is visible.
		{"walks back to the LAST space, not the first", "one two three four", 12, "one two…"},
		{"walks back past several words", "alpha bravo charlie delta", 20, "alpha bravo charlie…"},
		{"a long title keeps everything that fits", "AoC Codex the Age of Conan Hyborian Adventures reference guide", 40, "AoC Codex the Age of Conan Hyborian…"},
		{"a single unbreakable word is cut hard", strings.Repeat("x", 30), 10, strings.Repeat("x", 9) + "…"},
		{"runs of whitespace collapse", "a    b", 60, "a b"},
		{"newlines collapse", "a\nb", 60, "a b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := templates.Truncate(c.in, c.max)
			if got != c.want {
				t.Errorf("Truncate(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
			}
			if utf8.RuneCountInString(got) > c.max {
				t.Errorf("result is %d runes, over the cap of %d", utf8.RuneCountInString(got), c.max)
			}
		})
	}
}

// Boss names carry accented characters. Slicing bytes instead of runes puts U+FFFD in a
// meta tag, which is the kind of thing nobody notices until it is in a search result.
func TestTruncateNeverSplitsARune(t *testing.T) {
	in := strings.Repeat("é", 40)
	got := templates.Truncate(in, 10)
	if strings.ContainsRune(got, '�') {
		t.Fatalf("Truncate split a multi-byte rune: %q", got)
	}
	if utf8.RuneCountInString(got) > 10 {
		t.Fatalf("got %d runes, want <= 10", utf8.RuneCountInString(got))
	}
}

// ⭐ The caps the acceptance criteria NAME, pinned as literals.
//
// Every other assertion is written against MaxTitle/MaxDescription, so setting
// MaxTitle = 600 left the whole suite green — the 60 and 155 were pinned by nothing
// (verify round 1). A test that reads the constant it is checking asserts only that the
// code equals itself.
func TestTheCapsAreTheNumbersTheCriteriaName(t *testing.T) {
	if templates.MaxTitle != 60 {
		t.Errorf("MaxTitle = %d, want 60 — the length Google renders before truncating", templates.MaxTitle)
	}
	if templates.MaxDescription != 155 {
		t.Errorf("MaxDescription = %d, want 155", templates.MaxDescription)
	}
	// And prove the literal actually bites, not just that the constant reads 60.
	long := strings.Repeat("word ", 40)
	if n := utf8.RuneCountInString(templates.Truncate(long, 60)); n > 60 {
		t.Errorf("a 200-character title truncated to %d runes, want <= 60", n)
	}
}

// Truncate used to panic on a max of 0 (slice bounds [:-1]). No caller passes 0 today;
// a future one must get an empty string, not a panic inside a meta tag.
func TestTruncateHandlesDegenerateCaps(t *testing.T) {
	for _, c := range []struct {
		max  int
		want string
	}{{0, ""}, {-1, ""}, {1, "…"}, {2, "a…"}} {
		if got := templates.Truncate("abcdef", c.max); got != c.want {
			t.Errorf("Truncate(\"abcdef\", %d) = %q, want %q", c.max, got, c.want)
		}
	}
}

func TestNewViewAppliesTheCaps(t *testing.T) {
	v := templates.NewView(strings.Repeat("title ", 40), strings.Repeat("desc ", 80), "https://x/")
	if n := utf8.RuneCountInString(v.Title); n > templates.MaxTitle {
		t.Errorf("title is %d runes, over MaxTitle %d", n, templates.MaxTitle)
	}
	if n := utf8.RuneCountInString(v.Description); n > templates.MaxDescription {
		t.Errorf("description is %d runes, over MaxDescription %d", n, templates.MaxDescription)
	}
}

// A page must not be able to skip its head fields — that is the contract SEO rests on.
func TestValidRejectsAMissingHeadField(t *testing.T) {
	full := templates.NewView("t", "d", "https://x/")
	for _, c := range []struct {
		name string
		v    templates.View
		want error
	}{
		{"complete view is valid", full, nil},
		{"no title", templates.View{Description: "d", Canonical: "c"}, templates.ErrNoTitle},
		{"no description", templates.View{Title: "t", Canonical: "c"}, templates.ErrNoDescription},
		{"no canonical", templates.View{Title: "t", Description: "d"}, templates.ErrNoCanonical},
		{"whitespace is not a title", templates.View{Title: "   ", Description: "d", Canonical: "c"}, templates.ErrNoTitle},
	} {
		t.Run(c.name, func(t *testing.T) {
			// errors.Is, not !=: Valid may one day wrap its sentinels, and a == check
			// would start passing for the wrong reason without anyone noticing.
			if got := c.v.Valid(); !errors.Is(got, c.want) {
				t.Errorf("Valid() = %v, want %v", got, c.want)
			}
		})
	}
}
