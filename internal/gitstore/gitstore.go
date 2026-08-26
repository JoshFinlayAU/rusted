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

// At returns the stored content of relPath as it was at a specific commit
// (git show <commit>:<relPath>). The commit ref is validated by the caller.
func (s *Store) At(relPath, commit string) (string, error) {
	return s.git("show", commit+":"+filepath.Clean(relPath))
}

// Diff returns a unified diff of relPath between two commits. An empty `to`
// means HEAD; an empty `from` diffs the commit against its own parent, which is
// the natural "what changed in this backup" view.
func (s *Store) Diff(relPath, from, to string) (string, error) {
	rel := filepath.Clean(relPath)
	if from != "" && to == "" {
		// Changes introduced by `from` itself. diff-tree --root makes even the first
		// backup (no parent) show as a full addition rather than erroring.
		return s.git("diff-tree", "-p", "--root", "--no-commit-id", from, "--", rel)
	}
	if to == "" {
		to = "HEAD"
	}
	return s.git("diff", from, to, "--", rel)
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

// LogEntry is one revision of a file, with the metadata needed to tell WHEN a
// configuration was captured and by whom.
type LogEntry struct {
	Commit  string // abbreviated commit hash
	Author  string // commit author name
	Date    string // author date, RFC3339 with local offset
	Subject string // commit subject line
}

// LogDetailed is like Log but returns structured entries including the full
// timestamp and author, so exports can state exactly when each version was
// stored. Newest first. Removal commits (a deleted device) are excluded, so
// callers only ever see revisions where the configuration actually existed.
func (s *Store) LogDetailed(relPath string, n int) ([]LogEntry, error) {
	// "|" separates fields; it cannot appear in %h/%aI and is rare enough in
	// names/subjects that SplitN(…, 4) keeps any later pipes inside Subject.
	const sep = "|"
	out, err := s.git("log", fmt.Sprintf("-n%d", n),
		"--diff-filter=d", // skip commits that deleted the path
		"--pretty=format:%h"+sep+"%an"+sep+"%aI"+sep+"%s", "--", relPath)
	if err != nil {
		return nil, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	var entries []LogEntry
	for _, ln := range strings.Split(out, "\n") {
		parts := strings.SplitN(ln, sep, 4)
		if len(parts) < 4 {
			continue
		}
		entries = append(entries, LogEntry{Commit: parts[0], Author: parts[1], Date: parts[2], Subject: parts[3]})
	}
	return entries, nil
}

// RemoveDevice deletes relPath from the repository and commits the removal,
// pruning parent directories that became empty. A path that was never backed
// up (or already gone) is not an error, so deletes stay idempotent.
func (s *Store) RemoveDevice(relPath string) error {
	relPath = filepath.Clean(relPath)
	if strings.HasPrefix(relPath, "..") || filepath.IsAbs(relPath) || relPath == "." {
		return fmt.Errorf("invalid backup path %q", relPath)
	}
	full := filepath.Join(s.Dir, relPath)
	if _, err := os.Stat(full); err != nil {
		if os.IsNotExist(err) {
			return nil // never backed up / already removed
		}
		return err
	}
	if _, err := s.git("rm", "-rf", "-q", "--", relPath); err != nil {
		return fmt.Errorf("removing %s from the backup repository: %w", relPath, err)
	}
	if _, err := s.git("commit", "-m", "Remove backup of "+relPath); err != nil {
		return fmt.Errorf("committing removal of %s: %w", relPath, err)
	}
	s.pruneEmptyParents(full)
	return nil
}

// FilterRepoAvailable reports whether the "git filter-repo" subcommand can be
// run on this host. PurgeHistory needs it and refuses to run without. The
// probe mirrors how PurgeHistory invokes it: git resolves external
// subcommands through its own exec-path too, so a bare PATH lookup would miss
// distro installs that only git can see.
func FilterRepoAvailable() bool {
	return exec.Command("git", "filter-repo", "--version").Run() == nil
}

// PurgeHistory irreversibly erases relPath from EVERY commit of the repository
// by rewriting history with "git filter-repo --path <rel> --invert-paths".
// Unlike RemoveDevice (which only hides the file going forward), afterwards
// even "git show <old-commit>:<rel>" finds nothing. This rewrites all commit
// hashes; use CommitMap afterwards to remap references stored elsewhere.
// The device's file must have been removed from HEAD first (RemoveDevice) or
// be absent already; a missing path is fine, filter-repo still cleans history.
func (s *Store) PurgeHistory(relPath string) error {
	if !FilterRepoAvailable() {
		return errors.New("irreversible purge requires git-filter-repo, which is not installed on this host (e.g. \"pip install git-filter-repo\" or your distro's package)")
	}
	relPath = filepath.Clean(relPath)
	if strings.HasPrefix(relPath, "..") || filepath.IsAbs(relPath) || relPath == "." {
		return fmt.Errorf("invalid backup path %q", relPath)
	}
	// --force is required because ./backups is a working repository, not a
	// fresh clone as filter-repo prefers. There is no remote here to lose.
	if _, err := s.git("filter-repo", "--path", relPath, "--invert-paths", "--force"); err != nil {
		return fmt.Errorf("rewriting history without %s: %w", relPath, err)
	}
	s.pruneEmptyParents(filepath.Join(s.Dir, relPath))
	return nil
}

// CommitMap parses the old→new commit-hash map that git-filter-repo leaves in
// .git/filter-repo/commit-map after a rewrite ("old new" per line, "-" for
// commits that were dropped). Callers use it to keep stored references (e.g.
// recorded backup-run hashes) pointing at the rewritten history.
func (s *Store) CommitMap() (map[string]string, error) {
	b, err := os.ReadFile(filepath.Join(s.Dir, ".git", "filter-repo", "commit-map"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no rewrite has happened here
		}
		return nil, err
	}
	m := make(map[string]string)
	for _, ln := range strings.Split(string(b), "\n") {
		parts := strings.Fields(ln)
		if len(parts) != 2 || parts[0] == "old" || parts[0] == parts[1] {
			continue // header line or unchanged hash
		}
		// Dropped commits map to "-" (filter-repo <2.x wording) or to the
		// all-zero hash; either way there is no new commit to point at.
		if parts[1] == "-" || strings.Trim(parts[1], "0") == "" {
			continue
		}
		m[parts[0]] = parts[1]
	}
	return m, nil
}

// pruneEmptyParents removes directories above full (exclusive) that held only
// this file, up to the repo root which is never touched.
func (s *Store) pruneEmptyParents(full string) {
	dir := filepath.Dir(full)
	for dir != s.Dir && strings.HasPrefix(dir, s.Dir) {
		if err := os.Remove(dir); err != nil {
			break // not empty or unreadable: keep it
		}
		dir = filepath.Dir(dir)
	}
}
