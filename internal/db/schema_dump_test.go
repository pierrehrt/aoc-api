package db_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// ⭐ A FAILED `make schema-dump` LEAVES docs/database-schema.sql ALONE (AOC-029).
//
// The target used to redirect straight into the document, which the shell truncates before
// pg_dump runs: on a laptop with no container runtime the 95-line schema became a 13-line header
// that still parsed as SQL and read like a real file. And its `pg_dump | grep` reported grep's
// status, so a dump that died mid-stream counted as a success.
//
// These run the REAL target, from a copy of the Makefile in a temporary directory, with COMPOSE
// pointed at a fake. The repo's own document is never in reach.

// The document as committed before the run; a failed dump must leave exactly these bytes.
const schemaDocBefore = "-- the schema document as it stood before the run\nCREATE TABLE test_alpha (id int);\n"

// schemaDumpCmd prepares `make schema-dump` in a fresh directory holding a copy of the Makefile
// and a document with schemaDocBefore in it. compose is what the target runs as $(COMPOSE).
func schemaDumpCmd(t *testing.T, compose string) (dir string, cmd *exec.Cmd) {
	t.Helper()
	if _, err := exec.LookPath("make"); err != nil {
		t.Fatalf("make is not on PATH: %v", err)
	}
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(readRepoFile(t, "Makefile")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "database-schema.sql"), []byte(schemaDocBefore), 0o644); err != nil {
		t.Fatal(err)
	}
	// Bounded: a hung make fails the test instead of hanging the suite. (go.mod is 1.23, so no
	// t.Context.)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	cmd = exec.CommandContext(ctx, "make", "-f", "Makefile", "schema-dump", "COMPOSE="+compose)
	cmd.Dir = dir
	// Run under `make test`, the parent's MAKEFLAGS would carry its own command-line variables
	// into this make; this run must see only its own.
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "MAKEFLAGS", "MFLAGS", "MAKELEVEL", "MAKEOVERRIDES":
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	return dir, cmd
}

// runSchemaDump runs schemaDumpCmd to the end.
func runSchemaDump(t *testing.T, compose string) (dir string, out []byte, err error) {
	t.Helper()
	dir, cmd := schemaDumpCmd(t, compose)
	out, err = cmd.CombinedOutput()
	return dir, out, err
}

// fakeCompose writes an executable script that stands in for `docker compose` and returns its path.
func fakeCompose(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fake-compose")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// leftovers lists what the run left in docs/ besides the document itself.
func leftovers(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, "docs"))
	if err != nil {
		t.Fatal(err)
	}
	var extra []string
	for _, e := range entries {
		if e.Name() != "database-schema.sql" {
			extra = append(extra, e.Name())
		}
	}
	return extra
}

func TestSchemaDumpLeavesTheDocumentAloneWhenTheDumpFails(t *testing.T) {
	cases := []struct {
		name    string
		compose func(t *testing.T) string
		prepare func(t *testing.T, cmd *exec.Cmd) // optional, before the run
		wantOut string                            // optional, what the output must say
	}{
		{
			// The measured case: no container runtime on the machine.
			name:    "the container runtime is missing",
			compose: func(t *testing.T) string { return filepath.Join(t.TempDir(), "no-such-docker") + " compose" },
		},
		{
			// pg_dump writes part of a schema, then dies. Through a pipe into grep, this one
			// used to exit 0.
			name: "the dump fails mid-stream",
			compose: func(t *testing.T) string {
				return fakeCompose(t, `printf 'CREATE TABLE test_partial (\n'; exit 1`)
			},
		},
		{
			// Exit 0 and no schema at all, e.g. the wrong container answering. A document
			// reduced to its header is the very file this ticket exists to prevent.
			name:    "the dump succeeds and is empty",
			compose: func(t *testing.T) string { return fakeCompose(t, `exit 0`) },
			wantOut: "the dump is empty; docs/database-schema.sql is unchanged",
		},
		{
			// The dump is fine and the step that writes the document fails (here grep, as a
			// full disk would). Only writing beside the document and moving it in survives this.
			name: "writing the document fails",
			compose: func(t *testing.T) string {
				return fakeCompose(t, `printf 'CREATE TABLE test_delta (id int);\n'`)
			},
			prepare: func(t *testing.T, cmd *exec.Cmd) {
				bin := t.TempDir()
				if err := os.WriteFile(filepath.Join(bin, "grep"), []byte("#!/bin/sh\nexit 2\n"), 0o755); err != nil {
					t.Fatal(err)
				}
				for i, kv := range cmd.Env {
					if strings.HasPrefix(kv, "PATH=") {
						cmd.Env[i] = "PATH=" + bin + string(os.PathListSeparator) + strings.TrimPrefix(kv, "PATH=")
					}
				}
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir, cmd := schemaDumpCmd(t, c.compose(t))
			if c.prepare != nil {
				c.prepare(t, cmd)
			}
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Errorf("make schema-dump exited 0 for a dump that failed; output:\n%s", out)
			}
			if c.wantOut != "" && !strings.Contains(string(out), c.wantOut) {
				t.Errorf("the output does not say %q:\n%s", c.wantOut, out)
			}
			got, rerr := os.ReadFile(filepath.Join(dir, "docs", "database-schema.sql"))
			if rerr != nil {
				t.Fatalf("the document is gone after a failed dump: %v", rerr)
			}
			if !bytes.Equal(got, []byte(schemaDocBefore)) {
				t.Errorf("a failed dump changed the document:\n--- before\n%s--- after\n%s", schemaDocBefore, got)
			}
			if extra := leftovers(t, dir); len(extra) > 0 {
				t.Errorf("a failed dump left files behind in docs/: %v", extra)
			}
		})
	}
}

func TestSchemaDumpReplacesTheDocumentWhenTheDumpSucceeds(t *testing.T) {
	// A dump as pg_dump 18 shapes it: the schema between a \restrict / \unrestrict pair whose
	// token is random on every run.
	compose := fakeCompose(t, `printf '\\restrict TestTokenAlpha\nCREATE TABLE test_beta (id int);\n\\unrestrict TestTokenAlpha\n'`)
	dir, out, err := runSchemaDump(t, compose)
	if err != nil {
		t.Fatalf("make schema-dump failed on a dump that succeeded: %v\n%s", err, out)
	}
	got, err := os.ReadFile(filepath.Join(dir, "docs", "database-schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(got)
	for _, want := range []string{
		"-- aoc_api — the CURRENT database schema, as a living document.\n",
		"\nCREATE TABLE test_beta (id int);\n",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("the new document lacks %q:\n%s", want, doc)
		}
	}
	if strings.Contains(doc, "test_alpha") {
		t.Errorf("the old document survived a successful dump:\n%s", doc)
	}
	if strings.Contains(doc, "restrict") {
		t.Errorf("the random-token \\restrict lines were not stripped:\n%s", doc)
	}
	if extra := leftovers(t, dir); len(extra) > 0 {
		t.Errorf("a successful dump left files behind in docs/: %v", extra)
	}
}

// Ctrl-C in the middle of a dump. The document was always safe here, but the shell died on the
// signal without running its EXIT trap, so the raw dump stayed in docs/ for a `git add -A` to
// pick up (measured before the INT/TERM/HUP trap, under sh and dash).
func TestSchemaDumpInterruptedLeavesNothingBehind(t *testing.T) {
	started := filepath.Join(t.TempDir(), "started")
	compose := fakeCompose(t, `printf 'CREATE TABLE test_gamma (\n'; touch '`+started+`'; sleep 30`)
	dir, cmd := schemaDumpCmd(t, compose)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // its own group, as a terminal's job is
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// If the test stops early, the fake's `sleep 30` must not outlive it.
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
	waitFor(t, "the fake dump to start", func() bool { _, err := os.Stat(started); return err == nil })
	// What Ctrl-C does: the signal goes to the whole foreground process group.
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Error("an interrupted make schema-dump exited 0")
	}
	// make can return before its shell has finished the trap; give the shell a moment.
	waitFor(t, "docs/ to hold only the document", func() bool { return len(leftovers(t, dir)) == 0 })
	got, err := os.ReadFile(filepath.Join(dir, "docs", "database-schema.sql"))
	if err != nil {
		t.Fatalf("the document is gone after an interrupted dump: %v", err)
	}
	if !bytes.Equal(got, []byte(schemaDocBefore)) {
		t.Errorf("an interrupted dump changed the document:\n%s", got)
	}
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("gave up after 10 s waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A `kill` or a closed terminal in the middle of a dump. The recipe traps TERM and HUP as well as
// INT, and only INT was pinned: a recipe that dropped either one from its trap still passed every
// test above (AOC-029 verify round 1, mutation check). Without the trap the shell dies on the
// signal without running its EXIT trap, and the raw dump stays in docs/.
func TestSchemaDumpTerminatedLeavesNothingBehind(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(sig.String(), func(t *testing.T) {
			started := filepath.Join(t.TempDir(), "started")
			compose := fakeCompose(t, `printf 'CREATE TABLE test_epsilon (\n'; touch '`+started+`'; sleep 30`)
			dir, cmd := schemaDumpCmd(t, compose)
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
			waitFor(t, "the fake dump to start", func() bool { _, err := os.Stat(started); return err == nil })
			if err := syscall.Kill(-cmd.Process.Pid, sig); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Wait(); err == nil {
				t.Errorf("make schema-dump exited 0 after %v", sig)
			}
			waitFor(t, "docs/ to hold only the document", func() bool { return len(leftovers(t, dir)) == 0 })
			got, err := os.ReadFile(filepath.Join(dir, "docs", "database-schema.sql"))
			if err != nil {
				t.Fatalf("the document is gone after %v: %v", sig, err)
			}
			if !bytes.Equal(got, []byte(schemaDocBefore)) {
				t.Errorf("%v in the middle of a dump changed the document:\n%s", sig, got)
			}
		})
	}
}
