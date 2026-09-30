package pages

import (
	"encoding/xml"
	"strings"
	"testing"
)

// The protocol's two limits per file are 50,000 URLs and 50 MB uncompressed (sitemaps.org). The
// count is sitemapMaxURLs; the size is proved on the WORST case — a full chunk of the longest slug
// items.slug can hold (varchar 160) — so growth cannot cross it unnoticed.
func TestAFullChunkOfTheLongestURLsStaysUnder50MB(t *testing.T) {
	if sitemapMaxURLs != 50000 {
		t.Fatalf("sitemapMaxURLs = %d; the protocol's limit is 50,000", sitemapMaxURLs)
	}
	loc := sitemapLoc{Loc: "https://aoc-codex.app/armory/" + strings.Repeat("x", 160)}
	set := urlSet{Xmlns: sitemapNS, URLs: make([]sitemapLoc, sitemapMaxURLs)}
	for i := range set.URLs {
		set.URLs[i] = loc
	}
	b, err := xml.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	if size := len(xml.Header) + len(b); size >= 50*1024*1024 {
		t.Errorf("a full chunk is %d bytes, over the 50 MB limit", size)
	}
}
