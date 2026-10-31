package gitstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveChangeDetection(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	r1, err := s.Save("dc/router1.cfg", "hostname router1\n", "first")
	if err != nil {
		t.Fatal(err)
	}
	if !r1.Changed {
		t.Fatal("first save should be a change")
	}

	// Identical content => no new commit.
	r2, err := s.Save("dc/router1.cfg", "hostname router1\n", "second")
	if err != nil {
		t.Fatal(err)
	}
	if r2.Changed {
		t.Fatal("identical content should not be a change")
	}
	if r2.Commit != r1.Commit {
		t.Fatalf("unchanged save should keep HEAD: %s != %s", r2.Commit, r1.Commit)
	}

	// Modified content => new commit.
	r3, err := s.Save("dc/router1.cfg", "hostname router2\n", "third")
	if err != nil {
		t.Fatal(err)
	}
	if !r3.Changed {
		t.Fatal("modified content should be a change")
	}
	if r3.Commit == r1.Commit {
		t.Fatal("modified save should create a new commit")
	}

	got, err := s.Latest("dc/router1.cfg")
	if err != nil {
		t.Fatal(err)
	}
	if got != "hostname router2\n" {
		t.Fatalf("Latest = %q", got)
	}

	log, err := s.Log("dc/router1.cfg", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(log) != 2 { // two commits touched this file
		t.Fatalf("expected 2 log entries, got %d: %v", len(log), log)
	}
}

func TestSaveRejectsTraversal(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save("../escape.cfg", "x", "m"); err == nil {
		t.Fatal("expected path traversal to be rejected")
	}
}

func mustSave(t *testing.T, s *Store, rel, content string) string {
	t.Helper()
	r, err := s.Save(rel, content, "backup "+rel)
	if err != nil {
		t.Fatal(err)
	}
	return r.Commit
}

// A move keeps every version reachable under the new path, including for names
// with spaces, and a diff spanning the move shows only the content change.
func TestMoveFileFollowsHistory(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldRel, newRel := "site a/old name.cfg", "site b/new name.cfg"
	c1 := mustSave(t, s, oldRel, "hostname a\n")
	c2 := mustSave(t, s, oldRel, "hostname a\nntp 1\n")
	if _, err := s.MoveFile(oldRel, newRel, "move"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "site a")); !os.IsNotExist(err) {
		t.Fatalf("emptied group directory should be pruned, stat err = %v", err)
	}
	c3 := mustSave(t, s, newRel, "hostname a\nntp 1\nntp 2\n")

	if got, err := s.At(newRel, c1); err != nil || got != "hostname a\n" {
		t.Fatalf("At(pre-move) = %q, %v", got, err)
	}
	diff, err := s.Diff(newRel, c1, c3)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "+ntp 1") || !strings.Contains(diff, "+ntp 2") {
		t.Fatalf("diff across the move lost content:\n%s", diff)
	}
	if diff, err := s.Diff(newRel, c2, ""); err != nil || !strings.Contains(diff, "+ntp 1") {
		t.Fatalf("diff of a pre-move commit = %q, %v", diff, err)
	}

	entries, err := s.LogDetailed(newRel, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 { // the move commit itself is left out
		t.Fatalf("LogDetailed = %+v, want 3 versions", entries)
	}
	if !strings.HasPrefix(c1, entries[2].Commit) || entries[0].Author != "rusted" {
		t.Fatalf("unexpected entries %+v", entries)
	}
	if _, err := time.Parse(time.RFC3339, entries[0].Date); err != nil {
		t.Fatalf("Date %q is not RFC 3339: %v", entries[0].Date, err)
	}
	if log, _ := s.Log(newRel, 10); len(log) != 4 {
		t.Fatalf("Log should follow the move, got %v", log)
	}
}

func TestMoveFileRefusesExistingTarget(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mustSave(t, s, "a.cfg", "a\n")
	mustSave(t, s, "b.cfg", "b\n")
	if _, err := s.MoveFile("a.cfg", "b.cfg", "move"); err == nil {
		t.Fatal("moving onto an existing file must fail")
	}
	if got, _ := s.Latest("b.cfg"); got != "b\n" {
		t.Fatalf("target overwritten: %q", got)
	}
	// Nothing committed under this name: nothing to move.
	if undo, err := s.MoveFile("never.cfg", "x.cfg", "move"); err != nil || undo != nil {
		t.Fatalf("untracked move: undo set %t, err %v", undo != nil, err)
	}
}

func TestMoveFileUndo(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mustSave(t, s, "a.cfg", "a\n")
	undo, err := s.MoveFile("a.cfg", "g/b.cfg", "move")
	if err != nil {
		t.Fatal(err)
	}
	if err := undo(); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Latest("a.cfg"); got != "a\n" {
		t.Fatalf("undo did not restore a.cfg: %q", got)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "g")); !os.IsNotExist(err) {
		t.Fatalf("undo should prune the target directory, stat err = %v", err)
	}
}

// RemoveFile deletes exactly one file: never a directory, never a glob match.
func TestRemoveFile(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mustSave(t, s, "g/r[1].cfg", "1\n")
	mustSave(t, s, "g/r1.cfg", "other\n")
	mustSave(t, s, "solo/x.cfg", "x\n")

	if _, err := s.RemoveFile("g/r[1].cfg", "rm"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Latest("g/r[1].cfg"); !os.IsNotExist(err) {
		t.Fatalf("file still present: %v", err)
	}
	if got, _ := s.Latest("g/r1.cfg"); got != "other\n" {
		t.Fatalf("pathspec glob removed a sibling: %q", got)
	}
	if _, err := s.RemoveFile("g", "rm"); err == nil {
		t.Fatal("removing a directory must fail")
	}
	if _, err := s.RemoveFile("../x.cfg", "rm"); err == nil {
		t.Fatal("removing outside the repo must fail")
	}
	undo, err := s.RemoveFile("solo/x.cfg", "rm")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "solo")); !os.IsNotExist(err) {
		t.Fatalf("emptied directory should be pruned, stat err = %v", err)
	}
	if err := undo(); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Latest("solo/x.cfg"); got != "x\n" {
		t.Fatalf("undo did not restore the file: %q", got)
	}
	// Never backed up: nothing to do.
	if undo, err := s.RemoveFile("none.cfg", "rm"); err != nil || undo != nil {
		t.Fatalf("untracked remove: undo set %t, err %v", undo != nil, err)
	}
	if out, _ := s.git("status", "--porcelain"); strings.TrimSpace(out) != "" {
		t.Fatalf("work tree left dirty:\n%s", out)
	}
}
