package gitstore

import "testing"

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
