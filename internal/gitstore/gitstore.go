// Package gitstore manages the git repository under ./backups where device
// configurations are versioned. It shells out to the system git binary, which
// is ubiquitous and avoids pinning a large in-process git implementation.
package gitstore

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Store is a handle to the backup repository rooted at Dir.
type Store struct {
	Dir string
}

// Open ensures dir exists and is an initialised git repository, then returns a
// handle to it.
func Open(dir string) (*Store, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if _, err := exec.LookPath("git"); err != nil {
		return nil, errors.New("git executable not found in PATH")
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, err
	}
	s := &Store{Dir: abs}
	if _, err := os.Stat(filepath.Join(abs, ".git")); os.IsNotExist(err) {
		if _, err := s.git("init"); err != nil {
			return nil, fmt.Errorf("git init: %w", err)
		}
		// Ensure commits succeed even on a host without global git identity.
		_, _ = s.git("config", "user.email", "rusted@localhost")
		_, _ = s.git("config", "user.name", "rusted")
		// Seed a first commit so the repo has a HEAD.
		readme := filepath.Join(abs, "README.md")
		if _, err := os.Stat(readme); os.IsNotExist(err) {
			_ = os.WriteFile(readme, []byte("# rusted backups\n\nDevice configuration backups, one file per device.\n"), 0o644)
			_, _ = s.git("add", "README.md")
			_, _ = s.git("commit", "-m", "Initialise backup repository")
		}
	}
	return s, nil
}

func (s *Store) git(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = s.Dir
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// SaveResult reports what happened during a Save.
type SaveResult struct {
	Changed bool   // false if the content was identical to the last backup
	Commit  string // commit hash (new commit, or current HEAD if unchanged)
	Bytes   int
}

// Save writes content to relPath within the repo and, if it changed, commits
// it with the given message. relPath may contain sub-directories.
func (s *Store) Save(relPath, content, message string) (*SaveResult, error) {
	relPath = filepath.Clean(relPath)
	if strings.HasPrefix(relPath, "..") || filepath.IsAbs(relPath) {
		return nil, fmt.Errorf("invalid backup path %q", relPath)
	}
	full := filepath.Join(s.Dir, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return nil, err
	}
	if _, err := s.git("add", "--", relPath); err != nil {
		return nil, err
	}
	res := &SaveResult{Bytes: len(content)}
	// Did staging produce a change?
	if _, err := s.git("diff", "--cached", "--quiet", "--", relPath); err == nil {
		// Exit 0 => no staged changes.
		res.Changed = false
		head, _ := s.git("rev-parse", "HEAD")
		res.Commit = strings.TrimSpace(head)
		return res, nil
	}
	if _, err := s.git("commit", "-m", message, "--", relPath); err != nil {
		return nil, err
	}
	head, err := s.git("rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	res.Changed = true
	res.Commit = strings.TrimSpace(head)
	return res, nil
}

// Latest returns the most recent stored content for relPath, or os.ErrNotExist.
func (s *Store) Latest(relPath string) (string, error) {
	full := filepath.Join(s.Dir, filepath.Clean(relPath))
	b, err := os.ReadFile(full)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Log returns up to n short log entries ("<hash> <subject>") for relPath.
func (s *Store) Log(relPath string, n int) ([]string, error) {
	out, err := s.git("log", fmt.Sprintf("-n%d", n), "--pretty=format:%h %ad %s", "--date=short", "--", relPath)
	if err != nil {
		return nil, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}
