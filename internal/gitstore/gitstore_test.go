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

func TestLogDetailed(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save("g/dev.cfg", "v1\n", "first backup"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save("g/dev.cfg", "v2\n", "second backup"); err != nil {
		t.Fatal(err)
	}

	entries, err := s.LogDetailed("g/dev.cfg", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	e := entries[0] // newest first
	if e.Commit == "" || e.Author == "" || e.Subject != "second backup" {
		t.Fatalf("unexpected entry: %+v", e)
	}
	// RFC3339 with offset, e.g. 2026-08-25T12:34:56+02:00
	if _, err := time.Parse(time.RFC3339, e.Date); err != nil {
		t.Fatalf("Date %q is not RFC3339: %v", e.Date, err)
	}
}

func TestRemoveDevice(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save("core/router1.cfg", "v1\n", "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save("core/router1.cfg", "v2\n", "second"); err != nil {
		t.Fatal(err)
	}
	// A sibling device in the same group must survive.
	if _, err := s.Save("core/router2.cfg", "x\n", "other"); err != nil {
		t.Fatal(err)
	}

	if err := s.RemoveDevice("core/router1.cfg"); err != nil {
		t.Fatalf("RemoveDevice: %v", err)
	}
	if _, err := s.Latest("core/router1.cfg"); !os.IsNotExist(err) {
		t.Fatalf("file still on disk after removal: %v", err)
	}
	// The version list must not surface the removal itself; only the commits
	// made while the file existed remain (inside raw git history).
	entries, err := s.LogDetailed("core/router1.cfg", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Subject, "Remove backup") {
			t.Fatalf("removal commit leaked into version list: %+v", e)
		}
	}
	// HEAD must contain the removal commit.
	out, err := s.git("show", "--name-only", "--format=%s", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Remove backup of core/router1.cfg") {
		t.Fatalf("HEAD = %q, want a removal commit", out)
	}
	// Sibling intact; group dir kept (still holds router2.cfg).
	if got, err := s.Latest("core/router2.cfg"); err != nil || got != "x\n" {
		t.Fatalf("sibling affected: %q %v", got, err)
	}

	// Idempotent: removing again (or a never-backed-up path) is a no-op.
	if err := s.RemoveDevice("core/router1.cfg"); err != nil {
		t.Fatalf("second remove should be a no-op, got %v", err)
	}
	if err := s.RemoveDevice("never/backed-up.cfg"); err != nil {
		t.Fatalf("unknown path should be a no-op, got %v", err)
	}

	// Traversal rejected.
	if err := s.RemoveDevice("../escape.cfg"); err == nil {
		t.Fatal("expected traversal to be rejected")
	}

	// Empty group dirs are pruned once their last device is gone.
	if err := s.RemoveDevice("core/router2.cfg"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "core")); !os.IsNotExist(err) {
		t.Fatalf("empty group dir not pruned: %v", err)
	}
}

// writeCommitMap plants a filter-repo-style commit-map inside the repo.
func writeCommitMap(t *testing.T, s *Store, content string) {
	t.Helper()
	dir := filepath.Join(s.Dir, ".git", "filter-repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "commit-map"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCommitMapParsing(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeCommitMap(t, s, "old new\n"+
		"1111111111111111111111111111111111111111 2222222222222222222222222222222222222222\n"+ // rewritten -> mapped
		"3333333333333333333333333333333333333333 3333333333333333333333333333333333333333\n"+ // unchanged  -> skipped
		"4444444444444444444444444444444444444444 -\n"+ // dropped    -> skipped
		"5555555555555555555555555555555555555555 0000000000000000000000000000000000000000\n") // dropped    -> skipped
	m, err := s.CommitMap()
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 1 || m["1111111111111111111111111111111111111111"] != "2222222222222222222222222222222222222222" {
		t.Fatalf("CommitMap = %v", m)
	}
	// A repo never rewritten has no map at all.
	s2, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if m, err := s2.CommitMap(); err != nil || m != nil {
		t.Fatalf("CommitMap on clean repo = %v, %v", m, err)
	}
}

func TestPurgeHistoryIntegration(t *testing.T) {
	if !FilterRepoAvailable() {
		t.Skip("git-filter-repo not runnable on this host")
	}
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r1, _ := s.Save("purged/dev.cfg", "secret-v1\n", "first")
	if _, err := s.Save("purged/dev.cfg", "secret-v2\n", "second"); err != nil {
		t.Fatal(err)
	}
	siblingOld, _ := s.Save("kept/sibling.cfg", "sib1\n", "sibling first")
	siblingNew, err := s.Save("kept/sibling.cfg", "sib2\n", "sibling second")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.RemoveDevice("purged/dev.cfg"); err != nil {
		t.Fatal(err)
	}
	if err := s.PurgeHistory("purged/dev.cfg"); err != nil {
		t.Fatal(err)
	}

	// The old revisions are gone even through direct history access...
	for _, c := range []string{r1.Commit} {
		if _, err := s.At("purged/dev.cfg", c); err == nil {
			t.Fatalf("content of %s still recoverable after purge", c)
		}
	}
	if entries, _ := s.LogDetailed("purged/dev.cfg", 10); len(entries) != 0 {
		t.Fatalf("history survived the purge: %+v", entries)
	}
	// ...while other devices keep their content.
	if got, err := s.Latest("kept/sibling.cfg"); err != nil || got != "sib2\n" {
		t.Fatalf("sibling affected: %q %v", got, err)
	}
	// Sibling commits were rewritten to new hashes; the commit-map reflects it.
	m, err := s.CommitMap()
	if err != nil {
		t.Fatal(err)
	}
	newSib, ok := m[siblingOld.Commit]
	if !ok || newSib == "" {
		t.Fatalf("sibling commit %s missing from commit-map (%d entries)", siblingOld.Commit, len(m))
	}
	if _, err := s.At("kept/sibling.cfg", newSib); err != nil {
		t.Fatalf("remapped sibling hash unusable: %v", err)
	}
	_ = siblingNew
}
