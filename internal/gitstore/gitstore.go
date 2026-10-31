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
	// Every pathspec we pass is an exact file path; literal pathspecs stop a
	// device name containing "*" or "[" from matching other devices' files.
	cmd.Env = append(gitEnv(os.Environ()), "GIT_LITERAL_PATHSPECS=1")
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// gitEnv fills in a commit identity when the environment doesn't carry one. The
// repo-local identity written by Open only exists for repos rusted initialised
// itself, and a service manager that drops privileges but keeps root's HOME
// leaves git with no readable global config either, so commits failed with
// "Author identity unknown". An identity the caller does set still wins.
func gitEnv(env []string) []string {
	defaults := map[string]string{
		"GIT_AUTHOR_NAME":     "rusted",
		"GIT_AUTHOR_EMAIL":    "rusted@localhost",
		"GIT_COMMITTER_NAME":  "rusted",
		"GIT_COMMITTER_EMAIL": "rusted@localhost",
	}
	for _, kv := range env {
		if k, _, ok := strings.Cut(kv, "="); ok {
			delete(defaults, k)
		}
	}
	for k, v := range defaults {
		env = append(env, k+"="+v)
	}
	return env
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
// If the file has been moved since (device rename or group change), the path it
// had at that commit is used, so old versions stay readable under the new name.
func (s *Store) At(relPath, commit string) (string, error) {
	return s.git("show", commit+":"+s.pathAt(filepath.Clean(relPath), commit))
}

// pathAt reports the path rel had at commit. It is rel itself when that exists
// there; otherwise the earlier names in rel's rename history (git log --follow)
// are tried newest first. Falls back to rel, letting the caller's git command
// report the miss.
func (s *Store) pathAt(rel, commit string) string {
	if s.existsAt(rel, commit) {
		return rel
	}
	// -z keeps names with spaces or quotes intact; --name-only lists the path
	// each commit touched, which walks back through every name rel has had.
	out, err := s.git("log", "--follow", "--name-only", "-z", "--format=", "--", rel)
	if err != nil {
		return rel
	}
	seen := map[string]bool{rel: true}
	for _, p := range strings.Split(out, "\x00") {
		p = strings.TrimLeft(p, "\n")
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		if s.existsAt(p, commit) {
			return p
		}
	}
	return rel
}

func (s *Store) existsAt(rel, commit string) bool {
	_, err := s.git("cat-file", "-e", commit+":"+rel)
	return err == nil
}

// Diff returns a unified diff of relPath between two commits. An empty `to`
// means HEAD; an empty `from` diffs the commit against its own parent, which is
// the natural "what changed in this backup" view. Each side uses the path the
// file had at that commit, so a rename in between doesn't blank the diff.
func (s *Store) Diff(relPath, from, to string) (string, error) {
	rel := filepath.Clean(relPath)
	if from != "" && to == "" {
		// Changes introduced by `from` itself. diff-tree --root makes even the first
		// backup (no parent) show as a full addition rather than erroring.
		return s.git("diff-tree", "-p", "--root", "--no-commit-id", from, "--", s.pathAt(rel, from))
	}
	if to == "" {
		to = "HEAD"
	}
	pf, pt := s.pathAt(rel, from), s.pathAt(rel, to)
	if pf != pt && s.existsAt(pf, from) && s.existsAt(pt, to) {
		// Renamed in between: compare the two blobs directly.
		return s.git("diff", from+":"+pf, to+":"+pt)
	}
	return s.git("diff", from, to, "--", pt)
}

// Log returns up to n short log entries ("<hash> <subject>") for relPath.
// --follow walks across renames, so history survives a device rename.
func (s *Store) Log(relPath string, n int) ([]string, error) {
	out, err := s.git("log", fmt.Sprintf("-n%d", n), "--follow", "--pretty=format:%h %ad %s", "--date=short", "--", relPath)
	if err != nil {
		return nil, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// LogEntry is one stored version of a file.
type LogEntry struct {
	Commit  string // abbreviated commit hash
	Author  string // commit author name
	Date    string // author date, RFC 3339 with offset (when it was captured)
	Subject string
}

// LogDetailed is Log with structured entries, newest first. It follows
// renames, and leaves out commits that only deleted or moved the file, so every
// entry is a version whose content can be viewed.
func (s *Store) LogDetailed(relPath string, n int) ([]LogEntry, error) {
	// Unit separators can't occur in any of the fields.
	out, err := s.git("log", fmt.Sprintf("-n%d", n), "--follow", "--diff-filter=dr",
		"--pretty=format:%h%x1f%an%x1f%aI%x1f%s", "--", relPath)
	if err != nil {
		return nil, err
	}
	var entries []LogEntry
	for _, ln := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.SplitN(ln, "\x1f", 4)
		if len(f) != 4 {
			continue
		}
		entries = append(entries, LogEntry{Commit: f[0], Author: f[1], Date: f[2], Subject: f[3]})
	}
	return entries, nil
}

// RemoveFile deletes relPath (one file, never a directory) from the repository
// in its own commit and prunes directories left empty. A file that was never
// committed is not an error: there is nothing to remove. The returned undo
// reverts the removal commit.
func (s *Store) RemoveFile(relPath, message string) (undo func() error, err error) {
	rel, err := checkRel(relPath)
	if err != nil {
		return nil, err
	}
	if !s.tracked(rel) {
		return nil, nil
	}
	// -f only overrides git's "local modifications" check for this one file,
	// which is being deleted anyway; without -r a directory is refused.
	if _, err := s.git("rm", "-q", "-f", "--", rel); err != nil {
		return nil, fmt.Errorf("removing %s from the backup repository: %w", rel, err)
	}
	if _, err := s.git("commit", "-q", "-m", message, "--", rel); err != nil {
		_, _ = s.git("reset", "-q", "HEAD", "--", rel)
		_, _ = s.git("checkout", "HEAD", "--", rel)
		return nil, fmt.Errorf("committing removal of %s: %w", rel, err)
	}
	s.pruneEmptyParents(rel)
	return s.revertHead(), nil
}

// MoveFile moves oldRel to newRel with git mv in its own commit, so the file's
// history continues under the new path (Log, At and Diff follow it). A file
// that was never committed has nothing to move and is not an error; an
// existing newRel is refused rather than overwritten. The returned undo
// reverts the move commit.
func (s *Store) MoveFile(oldRel, newRel, message string) (undo func() error, err error) {
	from, err := checkRel(oldRel)
	if err != nil {
		return nil, err
	}
	to, err := checkRel(newRel)
	if err != nil {
		return nil, err
	}
	if from == to {
		return nil, nil
	}
	if _, err := os.Lstat(filepath.Join(s.Dir, to)); err == nil || s.tracked(to) {
		return nil, fmt.Errorf("%s already exists in the backup repository", to)
	}
	if !s.tracked(from) {
		return nil, nil
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Join(s.Dir, to)), 0o755); err != nil {
		return nil, err
	}
	if _, err := s.git("mv", "--", from, to); err != nil {
		s.pruneEmptyParents(to)
		return nil, fmt.Errorf("moving %s to %s: %w", from, to, err)
	}
	if _, err := s.git("commit", "-q", "-m", message, "--", from, to); err != nil {
		_, _ = s.git("mv", "--", to, from)
		s.pruneEmptyParents(to)
		return nil, fmt.Errorf("committing move of %s to %s: %w", from, to, err)
	}
	s.pruneEmptyParents(from)
	revert := s.revertHead()
	return func() error {
		err := revert()
		s.pruneEmptyParents(to)
		return err
	}, nil
}

// revertHead returns a func that reverts the commit HEAD points at now. It
// reverts by hash, so commits made in between (a concurrent backup) are kept.
func (s *Store) revertHead() func() error {
	head, err := s.git("rev-parse", "HEAD")
	if err != nil {
		return func() error { return err }
	}
	head = strings.TrimSpace(head)
	return func() error {
		_, err := s.git("revert", "--no-edit", head)
		return err
	}
}

func (s *Store) tracked(rel string) bool {
	_, err := s.git("ls-files", "--error-unmatch", "--", rel)
	return err == nil
}

// checkRel cleans relPath and refuses anything that would leave the repository
// or name its root.
func checkRel(relPath string) (string, error) {
	rel := filepath.Clean(relPath)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") || filepath.IsAbs(rel) {
		return "", fmt.Errorf("invalid backup path %q", relPath)
	}
	return rel, nil
}

// pruneEmptyParents removes the directories above rel that are now empty, up
// to (never including) the repository root.
func (s *Store) pruneEmptyParents(rel string) {
	for dir := filepath.Dir(rel); dir != "." && dir != "/"; dir = filepath.Dir(dir) {
		if os.Remove(filepath.Join(s.Dir, dir)) != nil {
			return // not empty (or not there): stop
		}
	}
}
