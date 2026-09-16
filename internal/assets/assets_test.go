package assets_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/pierrehrt/aoc-api/internal/assets"
)

// ⭐ "The build succeeded and produced nothing" is the failure shape this project has
// chased through three tickets. Here it would mean a site that serves perfectly and
// renders unstyled, so the binary refuses to start instead.
func TestLoadRefusesEmptyOrAbsentAssets(t *testing.T) {
	cases := []struct {
		name string
		fsys fstest.MapFS
		want string
	}{
		{"an empty file is a failed build", fstest.MapFS{"built/app.css": &fstest.MapFile{Data: []byte{}}}, "is empty"},
		{"no assets at all", fstest.MapFS{"built/.keep": &fstest.MapFile{Data: []byte("x")}}, "no assets are embedded"},
		{"missing directory", fstest.MapFS{}, "cannot read"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := assets.LoadFS(c.fsys, "built")
			if err == nil {
				t.Fatal("LoadFS accepted it — the binary would boot with no working stylesheet")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %q, want it to mention %q", err, c.want)
			}
		})
	}
}

func TestLoadHashesByContent(t *testing.T) {
	one := fstest.MapFS{"built/app.css": &fstest.MapFile{Data: []byte("a{}")}}
	two := fstest.MapFS{"built/app.css": &fstest.MapFile{Data: []byte("b{}")}}

	s1, err := assets.LoadFS(one, "built")
	if err != nil {
		t.Fatal(err)
	}
	s2, err := assets.LoadFS(two, "built")
	if err != nil {
		t.Fatal(err)
	}
	p1, _ := s1.Path("app.css")
	p2, _ := s2.Path("app.css")
	if p1 == p2 {
		t.Fatalf("different content produced the same URL %q — a deploy would serve a stale cached file", p1)
	}
	// Same content must be stable, or every deploy would bust every cache for nothing.
	s1b, _ := assets.LoadFS(one, "built")
	p1b, _ := s1b.Path("app.css")
	if p1 != p1b {
		t.Errorf("same content produced %q then %q — hashing is not deterministic", p1, p1b)
	}
}
