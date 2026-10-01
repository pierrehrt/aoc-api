// Package templates owns every HTML page this service renders.
//
// THE CONTRACT, which every later page ticket inherits:
//
//  1. A handler builds a View and calls Render. It does not touch a template by name,
//     does not hold a *template.Template, and never writes HTML itself.
//  2. Templates are parsed ONCE at startup from an embed.FS. A broken template fails
//     the binary at boot, not on the first request that happens to reach it — the
//     difference between finding it in CI and finding it from a raid at 21:00.
//  3. A page CANNOT omit its title or description. NewView requires them, and Render
//     refuses a View that is missing either. SEO is the entire reason this service
//     renders HTML at all (DECISIONS.md, 2026-09-16); a page that forgets its meta
//     tags is a page that quietly undoes that decision.
//  4. Templates never query. A View holds finished values.
package templates

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// Caps are presentation limits, not schema limits, so they live here rather than in
// reference/ux-principles.md § 3. They are what Google renders before truncating: past
// these lengths the tail is invisible, so a longer string is not a longer result, it is
// a sentence cut mid-word in public.
const (
	MaxTitle       = 60
	MaxDescription = 155
)

// View is everything a page needs. The <head> fields are not optional: see contract 3.
type View struct {
	Title       string // ≤ MaxTitle, truncated on a word boundary
	Description string // ≤ MaxDescription, truncated on a word boundary
	Canonical   string // absolute URL of this page, and only this page
	OGImage     string // absolute URL, optional
	// Card is the twitter:card beside OGImage: "" means summary_large_image (the wide site card);
	// a page whose image is portrait — an item's tooltip — sets "summary", or X crops it to 2:1.
	Card    string
	NoIndex bool // true keeps the page out of search results
	Data    any  // whatever the page's own template needs
	// JSONLD is the page's structured data (AOC-048), a value base.html writes into
	// <script type="application/ld+json">. html/template marshals it as JSON in that context, and
	// escapes `<`, so no field can close the script. nil writes no block.
	JSONLD any

	// The shell (AOC-046): what base.html renders around every page. Filled by the handler
	// package's view helper. The footer carries no credit and no notice (AOC-055, Pierre): where the
	// data came from is said once, on the Info page (AOC-056).
	Nav []NavItem // the header's section links — only routes that exist

	// App lays the page out as the validated design's full-window app from lg up (AOC-065): the
	// header and the page's own bars fixed, its panes scrolling inside, the document never scrolling.
	// Below lg every page is an ordinary scrolling document.
	App bool
}

// ThingLD is schema.org's Thing — what an item page describes. Not Product: Google reports a
// Product with no offer, review or rating as an error, and a game item has none of them.
type ThingLD struct {
	Context     string `json:"@context"`
	Type        string `json:"@type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Image       string `json:"image,omitempty"`
}

// NewThingLD fills the two constant fields, so a page cannot misspell them.
func NewThingLD(name, description, url, image string) ThingLD {
	return ThingLD{Context: "https://schema.org", Type: "Thing", Name: name, Description: description, URL: url, Image: image}
}

// NavItem is one header link.
type NavItem struct {
	Label   string
	Path    string
	Current bool // the section this page belongs to: the design's filled pill (AOC-065)
}

var (
	ErrNoTitle       = errors.New("view has no title")
	ErrNoDescription = errors.New("view has no description")
	ErrNoCanonical   = errors.New("view has no canonical URL")
)

// NewView builds a View with its required fields, truncating the two capped ones.
func NewView(title, description, canonical string) View {
	return View{
		Title:       Truncate(title, MaxTitle),
		Description: Truncate(description, MaxDescription),
		Canonical:   canonical,
	}
}

// Valid reports the first thing missing, so a page cannot render half a <head>.
func (v View) Valid() error {
	switch {
	case strings.TrimSpace(v.Title) == "":
		return ErrNoTitle
	case strings.TrimSpace(v.Description) == "":
		return ErrNoDescription
	case strings.TrimSpace(v.Canonical) == "":
		return ErrNoCanonical
	}
	return nil
}

// Truncate shortens s to at most max characters, cutting at a WORD BOUNDARY and
// appending an ellipsis.
//
// Cutting mid-word is the failure this exists to prevent: "Vistrix, the Frost Wy…"
// is a search result that looks broken, and it is the first thing a stranger sees of
// this site. Counts runes, not bytes — boss names carry accented characters, and
// slicing a byte string mid-rune produces U+FFFD in a meta tag.
func Truncate(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ") // collapse newlines and runs of spaces
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	// max < 2 leaves no room for content plus the ellipsis, and max-1 would index
	// backwards off the slice. No caller passes 0 today; a future one should get an empty
	// string, not a panic in a meta tag. (AOC-024 verify round 1.)
	if max <= 0 {
		return ""
	}
	if max == 1 {
		return "…"
	}
	// Leave room for the ellipsis itself, which is one rune.
	cut := r[:max-1]
	// ⚠️ First: the cut may ALREADY be on a boundary, when the rune just past it is a
	// space. Walking back without checking throws away a whole word that fitted —
	// "one two three four" capped at 14 became "one two…" instead of "one two three…".
	// Caught by the test, not by reading it.
	if unicode.IsSpace(r[max-1]) {
		return strings.TrimRight(string(cut), " ") + "…"
	}
	// Otherwise walk back to the last space so the cut lands between words.
	for i := len(cut) - 1; i >= 0; i-- {
		if unicode.IsSpace(cut[i]) {
			return strings.TrimRight(string(cut[:i]), " ") + "…"
		}
	}
	// One enormous word and nowhere to break: a hard cut is the only option left.
	return string(cut) + "…"
}

func (v View) String() string {
	return fmt.Sprintf("View{Title:%q, Canonical:%q, NoIndex:%v}", v.Title, v.Canonical, v.NoIndex)
}
