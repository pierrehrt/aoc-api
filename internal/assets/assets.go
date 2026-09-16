// Package assets serves the built CSS and JavaScript out of the binary.
//
// The files here are BUILT from web/src/ by `make assets` and COMMITTED
// (DECISIONS.md, 2026-09-16). `bin/gate api` fails if they are missing, empty, ignored
// or out of date, so what ships is always what someone reviewed.
package assets

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strings"

	"github.com/pierrehrt/aoc-api/internal/httpx"
)

//go:embed built
var built embed.FS

// Prefix is where assets are served. Content-hashed, so it can be cached forever.
const Prefix = "/assets/"

// Set is the resolved, hashed asset map, built once at startup.
type Set struct {
	// byLogical maps "app.css" -> "/assets/app.7f3a91c2.css"
	byLogical map[string]string
	// byServed maps "app.7f3a91c2.css" -> the bytes
	byServed map[string][]byte
}

// Load reads the embedded assets and hashes each one.
//
// ⭐ An EMPTY asset directory is an ERROR, not an empty set. The failure this prevents
// is the one the gate also guards: a build that "succeeded" and produced nothing, giving
// a site that serves perfectly and looks broken. Better to refuse to start.
func Load() (*Set, error) { return LoadFS(built, "built") }

// LoadFS is Load against any filesystem. It exists so the guards below can be TESTED:
// with only the real embed.FS they are unreachable from a test, and an unreachable guard
// is one nobody knows is broken (three of them survived mutation testing before this).
func LoadFS(fsys fs.FS, dir string) (*Set, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("assets: cannot read the embedded assets: %w", err)
	}
	s := &Set{byLogical: map[string]string{}, byServed: map[string][]byte{}}
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		b, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("assets: reading %s: %w", e.Name(), err)
		}
		if len(b) == 0 {
			return nil, fmt.Errorf("assets: %s is empty — a build that emits nothing is a failed build", e.Name())
		}
		sum := sha256.Sum256(b)
		hash := hex.EncodeToString(sum[:])[:8]
		ext := path.Ext(e.Name())
		servedName := strings.TrimSuffix(e.Name(), ext) + "." + hash + ext
		s.byLogical[e.Name()] = Prefix + servedName
		s.byServed[servedName] = b
	}
	if len(s.byLogical) == 0 {
		return nil, fmt.Errorf("assets: no assets are embedded — run `make assets` and commit internal/assets/built/")
	}
	return s, nil
}

// Path resolves a logical name to its hashed URL. Unknown names are an ERROR, which the
// template engine's startup probe turns into a boot failure: a typo'd asset reference
// can then never reach production as a silent 404 on the stylesheet.
func (s *Set) Path(name string) (string, error) {
	p, ok := s.byLogical[name]
	if !ok {
		return "", fmt.Errorf("assets: no asset named %q (have: %s)", name, strings.Join(s.names(), ", "))
	}
	return p, nil
}

func (s *Set) names() []string {
	out := make([]string, 0, len(s.byLogical))
	for k := range s.byLogical {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Handler serves the hashed files.
//
// Cache-Control is immutable and a year long, which is only safe BECAUSE the URL carries
// the content hash: change the file and the URL changes with it, so no cache anywhere is
// ever holding a stale asset under a live name.
func (s *Set) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, Prefix)
		b, ok := s.byServed[name]
		if !ok {
			// A wrong hash is a 404, deliberately: serving the current file under an old
			// hash would let a stale URL look valid forever.
			//
			// ⚠️ Through httpx.Fail, NOT http.NotFound. The stdlib helper calls the banned
			// error writer underneath, so it emits text/plain and skips the request id —
			// exactly what this repo forbids. The gate greps for the banned call by name
			// and does not recognise the stdlib wrapper, so a test caught this, not the
			// gate. (Spelling the banned name here would trip that same grep, which is a
			// false positive recorded on AOC-022.)
			httpx.Fail(w, r, httpx.ErrNotFound)
			return
		}
		switch path.Ext(name) {
		case ".css":
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
		case ".js":
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		case ".png":
			w.Header().Set("Content-Type", "image/png")
		case ".svg":
			w.Header().Set("Content-Type", "image/svg+xml")
		}
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b)
	})
}
