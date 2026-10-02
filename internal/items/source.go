package items

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/pierrehrt/aoc-api/internal/httpx"
)

// SourceNode is a node of the Armory's source panel (AOC-050), as a reader picks it: the levels of ONE
// source row, matched together (the shared filter CTE's source predicate). Every field is a slug of
// our own tables; the zero SourceNode picks nothing.
//
// Which tab the node is in is Filters.Tab, and which levels a tab draws is source_tabs.groups — data,
// not code. What IS code is the vocabulary of levels below, because each names one of our columns,
// like the sort keys: a section, a region, a map, a place (the location, then its wing), a boss, a
// vendor, a quest giver, a container.
type SourceNode struct {
	Section   string
	Region    string
	Map       string
	Places    []string // the location's places, each the parent of the next; the LAST is the one matched
	Boss      string
	Vendor    string
	Quest     string
	Container string
	// Group is the half of a location the reader picked: an acquisition_groups slug ("loot /
	// drops", "quest / vendor" in the design). It rides in its own parameter, `get`.
	Group string
}

// IsZero reports whether no node is picked.
func (s SourceNode) IsZero() bool {
	return s.Section == "" && s.Region == "" && s.Map == "" && len(s.Places) == 0 &&
		s.Boss == "" && s.Vendor == "" && s.Quest == "" && s.Container == "" && s.Group == ""
}

// Place is the place the node matches: the most specific one named, or "".
func (s SourceNode) Place() string {
	if len(s.Places) == 0 {
		return ""
	}
	return s.Places[len(s.Places)-1]
}

// The path's segment kinds, in the order String writes them.
var sourceKinds = []string{"s", "r", "m", "p", "b", "v", "q", "c"}

var slugRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// String is the `source` parameter that picks this node: typed segments joined by '.', in tree
// order — "s:pve-tier-3.p:<raid>.b:<boss>". Typed, not positional, so a level a row does not have
// (an Item Store row has no region) cannot shift the meaning of the ones after it. Group is not in
// it; it is `get`.
func (s SourceNode) String() string {
	var segs []string
	add := func(kind, slug string) {
		if slug != "" {
			segs = append(segs, kind+":"+slug)
		}
	}
	add("s", s.Section)
	add("r", s.Region)
	add("m", s.Map)
	for _, p := range s.Places {
		add("p", p)
	}
	add("b", s.Boss)
	add("v", s.Vendor)
	add("q", s.Quest)
	add("c", s.Container)
	return strings.Join(segs, ".")
}

// ParseSource reads a `source` value. Malformed is a 400: an unknown kind, a segment that is not
// kind:slug, a level named twice (places aside: a chain of them), more than eight places. "-" is a
// level the row does not have, for a region, a map, or the place before a location with none
// ("p:-", alone). An unknown SLUG is not malformed — it matches nothing, as an unknown rarity does
// (parseFilters' rule).
func ParseSource(raw string) (SourceNode, error) {
	var s SourceNode
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return s, nil
	}
	seen := map[string]bool{}
	for _, seg := range strings.Split(raw, ".") {
		kind, slug, ok := strings.Cut(seg, ":")
		if ok && slug == absent && (kind == "r" || kind == "m" || kind == "p") {
			// a level the row lacks; for a place, only as the one place named
		} else if !ok || !slugRe.MatchString(slug) {
			return SourceNode{}, fmt.Errorf("%w: source is levels like s:<section>.p:<place>, each kind:slug; got %q", httpx.ErrInvalid, seg)
		}
		if kind != "p" && seen[kind] {
			return SourceNode{}, fmt.Errorf("%w: source names %q twice", httpx.ErrInvalid, kind)
		}
		seen[kind] = true
		switch kind {
		case "s":
			s.Section = slug
		case "r":
			s.Region = slug
		case "m":
			s.Map = slug
		case "p":
			if len(s.Places) == 8 {
				return SourceNode{}, fmt.Errorf("%w: source names more than eight places", httpx.ErrInvalid)
			}
			if slug == absent && len(s.Places) > 0 || len(s.Places) == 1 && s.Places[0] == absent {
				return SourceNode{}, fmt.Errorf("%w: source's p:- (no place) stands alone", httpx.ErrInvalid)
			}
			s.Places = append(s.Places, slug)
		case "b":
			s.Boss = slug
		case "v":
			s.Vendor = slug
		case "q":
			s.Quest = slug
		case "c":
			s.Container = slug
		default:
			return SourceNode{}, fmt.Errorf("%w: source levels are %s; got %q", httpx.ErrInvalid, strings.Join(sourceKinds, ", "), kind)
		}
	}
	return s, nil
}

// parseGroup reads `get`: an acquisition group slug, or nothing.
func parseGroup(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw != "" && !slugRe.MatchString(raw) {
		return "", fmt.Errorf("%w: get must be an acquisition group slug, got %q", httpx.ErrInvalid, raw)
	}
	return raw, nil
}
