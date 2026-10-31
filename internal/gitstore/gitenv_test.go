package gitstore

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

func TestGitEnvKeepsCallerIdentity(t *testing.T) {
	env := gitEnv([]string{"GIT_AUTHOR_NAME=ops"})
	if !slices.Contains(env, "GIT_AUTHOR_NAME=ops") || slices.Contains(env, "GIT_AUTHOR_NAME=rusted") {
		t.Fatalf("caller identity overridden: %v", env)
	}
	if !slices.Contains(env, "GIT_COMMITTER_EMAIL=rusted@localhost") {
		t.Fatalf("missing default committer email: %v", env)
	}
}

// A repo rusted didn't initialise has no local identity; with HOME pointing somewhere
// unreadable git has no global one either. Save must still commit.
func TestSaveCommitsWithoutAnyGitIdentity(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	t.Setenv("HOME", filepath.Join(dir, "no-such-home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "no-such-home"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL", "EMAIL"} {
		os.Unsetenv(k)
	}

	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.Save("dev.cfg", "/system identity set name=x\n", "backup dev")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !res.Changed || res.Commit == "" {
		t.Fatalf("expected a new commit, got %+v", res)
	}
}
