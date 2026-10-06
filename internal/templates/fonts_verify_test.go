package templates_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/templates"
)

// AOC-075 verify round 1. The faces are a hand-kept table of IBM's own files; these pin what a
// careless edit to it would break without any other test noticing.

// The weights Google served before (Sans 400–700, Mono 400–600), each in IBM's three subsets — no
// more, no fewer — and each face's file is the one its family and weight name. A face that points at
// another weight's file draws the wrong weight with every test green, because the file still loads.
func TestTheFacesAreTheWeightsGoogleServedEachInItsOwnFile(t *testing.T) {
	weightName := map[int]string{400: "Regular", 500: "Medium", 600: "SemiBold", 700: "Bold"}
	want := map[string][]int{"IBM Plex Sans": {400, 500, 600, 700}, "IBM Plex Mono": {400, 500, 600}}
	subsets := []string{"Latin1", "Latin2", "Pi"}

	seen := map[string]bool{}
	for _, f := range templates.FontFaces {
		stem := "IBMPlex" + strings.TrimPrefix(f.Family, "IBM Plex ")
		name, ok := weightName[f.Weight]
		if !ok {
			t.Errorf("%s: weight %d is not one Google served", f.File, f.Weight)
			continue
		}
		matched := false
		for _, s := range subsets {
			if f.File == fmt.Sprintf("%s-%s-%s.woff2", stem, name, s) {
				matched = true
			}
		}
		if !matched {
			t.Errorf("%s is declared as %s %d: the file is not that family and weight", f.File, f.Family, f.Weight)
		}
		if seen[f.File] {
			t.Errorf("%s is declared twice", f.File)
		}
		seen[f.File] = true
	}
	for fam, ws := range want {
		for _, w := range ws {
			for _, s := range subsets {
				file := fmt.Sprintf("IBMPlex%s-%s-%s.woff2", strings.TrimPrefix(fam, "IBM Plex "), weightName[w], s)
				if !seen[file] {
					t.Errorf("%s %d has no %s face (%s)", fam, w, s, file)
				}
			}
		}
	}
	if n := 3 * (len(want["IBM Plex Sans"]) + len(want["IBM Plex Mono"])); len(templates.FontFaces) != n {
		t.Errorf("%d faces, want %d", len(templates.FontFaces), n)
	}
}

// IBM gives every weight of a family the same unicode-range for a subset. One weight's range
// differing from its siblings' is a paste error: that weight would fetch the wrong file for some
// characters, or none.
func TestEveryWeightOfAFamilyCoversTheSameCharactersPerSubset(t *testing.T) {
	ranges := map[string]map[string]string{} // family+subset -> range -> first file
	for _, f := range templates.FontFaces {
		subset := strings.TrimSuffix(f.File[strings.LastIndex(f.File, "-")+1:], ".woff2")
		key := f.Family + " " + subset
		if ranges[key] == nil {
			ranges[key] = map[string]string{}
		}
		if _, ok := ranges[key][string(f.Range)]; !ok {
			ranges[key][string(f.Range)] = f.File
		}
		if strings.TrimSpace(string(f.Range)) == "" || strings.ContainsAny(string(f.Range), ";{}<>\"'") {
			t.Errorf("%s: a unicode-range must be a plain list of U+ ranges, got %q", f.File, f.Range)
		}
	}
	for key, rs := range ranges {
		if len(rs) != 1 {
			var files []string
			for _, file := range rs {
				files = append(files, file)
			}
			sort.Strings(files)
			t.Errorf("%s: %d different ranges across its weights (%s)", key, len(rs), strings.Join(files, ", "))
		}
	}
}

// What ships is what is declared: every woff2 in web/src/fonts and in the embedded build has a face,
// and every face has its file in both. A font removed from the table but left in built/ would still
// be embedded and served — `make assets` copies, it never deletes. The OFL travels with the files.
func TestEveryFontFileIsDeclaredAndTheLicenceIsBesideThem(t *testing.T) {
	declared := map[string]bool{}
	for _, f := range templates.FontFaces {
		declared[f.File] = true
	}
	for _, dir := range []string{"../../web/src/fonts", "../assets/built"} {
		files, err := filepath.Glob(filepath.Join(dir, "*.woff2"))
		if err != nil {
			t.Fatal(err)
		}
		have := map[string]bool{}
		for _, p := range files {
			have[filepath.Base(p)] = true
			if !declared[filepath.Base(p)] {
				t.Errorf("%s: no face declares it, yet it ships", p)
			}
		}
		for file := range declared {
			if !have[file] {
				t.Errorf("%s: declared but missing from %s", file, dir)
			}
		}
	}
	ofl, err := os.ReadFile("../../web/src/fonts/OFL.txt")
	if err != nil {
		t.Fatalf("the SIL OFL must ship beside the fonts: %v", err)
	}
	for _, want := range []string{`IBM Corp. with Reserved Font Name "Plex"`, "SIL OPEN FONT LICENSE Version 1.1"} {
		if !strings.Contains(string(ofl), want) {
			t.Errorf("web/src/fonts/OFL.txt does not carry %q", want)
		}
	}
}
