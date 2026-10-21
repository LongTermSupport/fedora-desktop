// Package fetchage is the git-fetch-age command (Plan 00183, #52): the mark the shell prompt
// (files/var/local/ps1-prompt) shows beside the directory when the tracking refs the git
// status compares against may be stale. The ahead/behind counts are only as fresh as the last
// fetch, so a clean, up-to-date marker over an old fetch is a lie; the mark makes it visible.
//
// It never fetches, never starts a process and never touches the network: it reads a few files
// under the git directory, so it costs the prompt nothing and cannot ask for a passphrase.
//
// Expectations, each pinned by a test:
//
//   - A1 outside any checkout: no mark.
//   - A2 a repository with no [remote] in its config: no mark (nothing to go stale).
//   - A3 the last fetch is younger than Threshold: no mark.
//   - A4 older: " [fetched <N>h ago]" under 48 hours, " [fetched <N>d ago]" from then on.
//   - A5 a remote and no FETCH_HEAD anywhere: " [no fetch recorded]".
//   - A6 a FETCH_HEAD dated in the future (a clock step) counts as fresh.
//   - A7 git keeps FETCH_HEAD per worktree but shares the tracking refs, so the last fetch is
//     the newest FETCH_HEAD of the common git dir and of every worktree under it.
//   - A8 a .git file's gitdir is read against the checkout when relative; a git dir with no
//     commondir file is its own common dir (a submodule).
//   - A9 a malformed .git file, a gitdir that is not there or a git dir with no config is an
//     error, never a quiet "fresh".
//   - A10 the mark and its newline are the whole of stdout.
//   - A11 an error is one "git-fetch-age: " line on stderr, exit 1, nothing on stdout.
//   - A12 any argument is a usage error (exit 64).
//   - A13 a mark that could not be written is exit 1.
//   - A14 git empties FETCH_HEAD before it contacts the remote, so a fetch that failed (the
//     prompt's own background fetch with no key in the agent) leaves an empty, newly dated
//     FETCH_HEAD, as does one still running. An empty FETCH_HEAD is never a fetch: it is
//     FailedMark, however young. A fetch that succeeds but matches no ref (a remote with no
//     branches yet, a refspec that matches nothing) also leaves it empty, and is marked the
//     same until a fetch writes a record: the file cannot tell the two apart.
//   - A15 across worktrees the last fetch is the newest FETCH_HEAD with a record in it; within
//     Threshold it is no mark whatever failed since, and an empty one newer than a stale fetch
//     is FailedMark.
package fetchage

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Threshold is the age from which the last fetch is marked. bash-git-prompt fetches every
// five minutes while it works, so an hour is well past a working fetch.
const Threshold = time.Hour

const hoursUntilDays = 48

// FailedMark is the mark for a FETCH_HEAD git emptied and has not filled: the newest attempt
// to fetch failed, or is still running, so the tracking refs are as old as the fetch before it.
const FailedMark = " [fetch failed or running]"

var remoteSection = regexp.MustCompile(`(?m)^\s*\[remote\s+"`)

// Mark is what the prompt shows for dir at now: "" when there is nothing to say.
func Mark(dir string, now time.Time) (string, error) {
	common, found, err := commonDir(dir)
	if err != nil || !found {
		return "", err
	}
	config, err := os.ReadFile(filepath.Clean(filepath.Join(common, "config")))
	if err != nil {
		return "", fmt.Errorf("read git config: %w", err)
	}
	if !remoteSection.Match(config) {
		return "", nil
	}
	heads, err := lastFetch(common)
	if err != nil {
		return "", err
	}
	fetched, attempted := heads.fetched, heads.attempted
	if fetched.found && now.Sub(fetched.at) < Threshold {
		return "", nil
	}
	if attempted.found && (!fetched.found || attempted.at.After(fetched.at)) {
		return FailedMark, nil
	}
	if !fetched.found {
		return " [no fetch recorded]", nil
	}
	age := now.Sub(fetched.at)
	if hours := int(age / time.Hour); hours < hoursUntilDays {
		return fmt.Sprintf(" [fetched %dh ago]", hours), nil
	}
	return fmt.Sprintf(" [fetched %dd ago]", int(age/(24*time.Hour))), nil
}

// commonDir finds the git dir above dir and returns the common dir it shares with its other
// worktrees; found is false outside any checkout.
func commonDir(dir string) (string, bool, error) {
	for d := filepath.Clean(dir); ; d = filepath.Dir(d) {
		dotGit := filepath.Join(d, ".git")
		info, err := os.Stat(dotGit)
		switch {
		case err == nil && info.IsDir():
			return dotGit, true, nil
		case err == nil:
			common, gitFileErr := fromGitFile(d, dotGit)
			return common, gitFileErr == nil, gitFileErr
		case !errors.Is(err, fs.ErrNotExist):
			return "", false, fmt.Errorf("stat %s: %w", dotGit, err)
		}
		if filepath.Dir(d) == d {
			return "", false, nil
		}
	}
}

// fromGitFile reads a .git file's gitdir line and the commondir file under it.
func fromGitFile(checkout, dotGit string) (string, error) {
	content, err := os.ReadFile(filepath.Clean(dotGit))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", dotGit, err)
	}
	gitDir, ok := strings.CutPrefix(strings.TrimSpace(string(content)), "gitdir: ")
	if !ok || gitDir == "" {
		return "", fmt.Errorf("%s is not a gitdir line", dotGit)
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(checkout, gitDir)
	}
	gitDir = filepath.Clean(gitDir)
	if _, statErr := os.Stat(gitDir); statErr != nil {
		return "", fmt.Errorf("gitdir of %s: %w", dotGit, statErr)
	}
	rel, err := os.ReadFile(filepath.Clean(filepath.Join(gitDir, "commondir")))
	if errors.Is(err, fs.ErrNotExist) {
		return gitDir, nil
	}
	if err != nil {
		return "", fmt.Errorf("read commondir: %w", err)
	}
	common := strings.TrimSpace(string(rel))
	if !filepath.IsAbs(common) {
		common = filepath.Join(gitDir, common)
	}
	return common, nil
}

// newest is the latest of the FETCH_HEAD dates offered to it.
type newest struct {
	at    time.Time
	found bool
}

func (n *newest) offer(at time.Time) {
	if !n.found || at.After(n.at) {
		n.at, n.found = at, true
	}
}

// fetchHeads is what the FETCH_HEAD files of a repository say: the newest with a record in it
// is the last fetch, the newest empty one the last fetch that failed or is running.
type fetchHeads struct {
	fetched   newest
	attempted newest
}

// lastFetch reads the FETCH_HEAD of the common dir and of every worktree under it.
func lastFetch(common string) (fetchHeads, error) {
	var heads fetchHeads
	worktrees, err := filepath.Glob(filepath.Join(common, "worktrees", "*", "FETCH_HEAD"))
	if err != nil {
		return heads, fmt.Errorf("list worktree fetches: %w", err)
	}
	for _, path := range append([]string{filepath.Join(common, "FETCH_HEAD")}, worktrees...) {
		info, err := os.Stat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return heads, fmt.Errorf("stat %s: %w", path, err)
		}
		if info.Size() == 0 {
			heads.attempted.offer(info.ModTime())
		} else {
			heads.fetched.offer(info.ModTime())
		}
	}
	return heads, nil
}

// App is the command: the working directory and clock it reads, the streams it writes.
type App struct {
	Dir    string
	Now    time.Time
	Stdout io.Writer
	Stderr io.Writer
}

// Run prints the mark for a.Dir.
func (a App) Run(args []string) term.ExitCode {
	if len(args) > 0 {
		return a.fail(term.ExitUsage, "takes no arguments; usage: git-fetch-age")
	}
	mark, err := Mark(a.Dir, a.Now)
	if err != nil {
		return a.fail(term.ExitFailure, err.Error())
	}
	if mark == "" {
		return term.ExitOK
	}
	if _, err := fmt.Fprintln(a.Stdout, mark); err != nil {
		return a.fail(term.ExitFailure, "write the mark: "+err.Error())
	}
	return term.ExitOK
}

func (a App) fail(code term.ExitCode, msg string) term.ExitCode {
	if _, err := fmt.Fprintf(a.Stderr, "git-fetch-age: %s\n", msg); err != nil {
		return term.ExitFailure
	}
	return code
}
