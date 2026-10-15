package watchlist

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const usage = `usage: watchlist [--root DIR] <command>

  sync [repo]                    clone or fetch each watched repository and report what changed
                                 since it was last reviewed (repo: owner/repo or its URL)
  mark <repo> <sha> --run ID     record that repo was reviewed up to sha, in run ID
  add <url>                      watch a new repository (https URL); it starts unreviewed

--root DIR is the fedora-desktop checkout (default: the working directory).
`

// App is the watchlist command over a checkout, a process runner and two output streams.
type App struct {
	root string
	git  *gitx.Git
	out  io.Writer
	err  io.Writer
	p    term.Printer
}

// NewApp is the command rooted at root (overridden by --root), running git through r.
func NewApp(root string, r execx.Runner, out, err io.Writer) *App {
	return &App{root: root, git: gitx.New(r), out: out, err: err}
}

// Run executes one command line and returns the exit code (W1 to W3).
func (a *App) Run(ctx context.Context, args []string) term.ExitCode {
	code := a.dispatch(ctx, args)
	if a.p.Err() != nil && code == term.ExitOK {
		return term.ExitFailure
	}
	return code
}

// flags consumes the leading flags. done is true when the run is already decided (help, or a
// usage mistake), with the exit code to return.
func (a *App) flags(args []string) ([]string, term.ExitCode, bool) {
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "-h", "--help":
			a.p.Print(a.out, usage)
			return nil, term.ExitOK, true
		case "--root":
			if len(args) < 2 {
				return nil, a.usageError("--root needs a directory"), true
			}
			a.root, args = args[1], args[2:]
		default:
			return nil, a.usageError("unknown flag " + args[0]), true
		}
	}
	return args, term.ExitOK, false
}

func (a *App) dispatch(ctx context.Context, args []string) term.ExitCode {
	args, code, done := a.flags(args)
	if done {
		return code
	}
	if len(args) == 0 {
		return a.usageError("no command given")
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "sync":
		if len(rest) > 1 {
			return a.usageError("sync takes at most one repository")
		}
		return a.finish(a.sync(ctx, rest))
	case "add":
		if len(rest) != 1 {
			return a.usageError("add takes exactly one URL")
		}
		return a.finish(a.add(ctx, rest[0]))
	case "mark":
		if len(rest) != 4 || rest[2] != "--run" {
			return a.usageError("mark takes <repo> <sha> --run ID")
		}
		return a.finish(a.mark(ctx, rest[0], rest[1], rest[3]))
	default:
		return a.usageError("unknown command " + cmd)
	}
}

func (a *App) usageError(msg string) term.ExitCode {
	a.p.Print(a.err, "ERROR: "+msg+"\n"+usage)
	return term.ExitUsage
}

func (a *App) finish(err error) term.ExitCode {
	if err == nil {
		return term.ExitOK
	}
	a.p.Print(a.err, "ERROR: "+err.Error()+"\n")
	return term.ExitFailure
}

func (a *App) load() ([]Entry, error) {
	path := filepath.Clean(ListPath(a.root))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read the watch list: %w", err)
	}
	return Parse(data, path)
}

func (a *App) save(entries []Entry) error {
	return fsx.WriteAtomic(ListPath(a.root), "watch list", Format(entries), 0o644)
}

// find is the index of the entry named by name, owner/repo or URL, in any case.
func find(entries []Entry, name string) (int, error) {
	want := strings.ToLower(strings.TrimPrefix(name, "https://github.com/"))
	for i, e := range entries {
		if strings.ToLower(e.Name()) == want || strings.EqualFold(e.URL, name) {
			return i, nil
		}
	}
	return -1, fmt.Errorf("%s is not on the watch list", name)
}

// add is W10 to W12.
func (a *App) add(ctx context.Context, url string) error {
	if err := validateURL(url); err != nil {
		return err
	}
	entries, err := a.load()
	if err != nil {
		return err
	}
	if i, dupErr := find(entries, url); dupErr == nil {
		return fmt.Errorf("%s is already on the watch list (%s)", url, entries[i].Name())
	}
	branch, err := a.git.DefaultBranch(ctx, url)
	if err != nil {
		return err
	}
	entry := Entry{URL: url, Branch: branch}
	if err := a.save(append(entries, entry)); err != nil {
		return err
	}
	a.p.Print(a.out, fmt.Sprintf("added %s (%s): initial full review pending\n", entry.Name(), branch))
	return nil
}

// sync is W13 to W21; it never writes the list.
func (a *App) sync(ctx context.Context, only []string) error {
	entries, err := a.load()
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return errors.New("the watch list is empty")
	}
	if len(only) == 1 {
		i, findErr := find(entries, only[0])
		if findErr != nil {
			return findErr
		}
		entries = entries[i : i+1]
	}
	for _, e := range entries {
		if err := a.syncOne(ctx, e); err != nil {
			return fmt.Errorf("%s: %w", e.Name(), err)
		}
	}
	return nil
}

func (a *App) syncOne(ctx context.Context, e Entry) error {
	dir := e.MirrorDir(a.root)
	if err := a.refresh(ctx, e, dir); err != nil {
		return err
	}
	head, err := a.git.RemoteHead(ctx, dir, e.Branch)
	if err != nil {
		return err
	}
	a.p.Print(a.out, fmt.Sprintf("== %s (%s)\n", e.Name(), e.Branch))
	if e.LastSHA == "" {
		a.p.Print(a.out, fmt.Sprintf("initial: full review\nhead: %s\nmirror: %s\n", head, dir))
		return nil
	}
	a.p.Print(a.out, fmt.Sprintf("last-checked: %s (run %s)\nhead: %s\n", e.LastSHA, e.Run, head))
	if head == e.LastSHA {
		a.p.Print(a.out, "unchanged\n")
		return nil
	}
	return a.printRange(ctx, dir, e.LastSHA, head)
}

// refresh clones the mirror at dir when it is absent and fetches it otherwise (W14).
func (a *App) refresh(ctx context.Context, e Entry, dir string) error {
	_, statErr := os.Stat(dir)
	switch {
	case statErr == nil:
		return a.git.FetchBranch(ctx, dir, e.Branch)
	case errors.Is(statErr, os.ErrNotExist):
		if err := os.MkdirAll(filepath.Dir(dir), 0o750); err != nil {
			return fmt.Errorf("cannot create the mirror directory: %w", err)
		}
		return a.git.CloneBranch(ctx, e.URL, e.Branch, dir)
	default:
		return fmt.Errorf("cannot inspect the mirror %s: %w", dir, statErr)
	}
}

func (a *App) printRange(ctx context.Context, dir, last, head string) error {
	held, err := a.git.HasCommit(ctx, dir, last)
	if err != nil {
		return err
	}
	if !held {
		return fmt.Errorf("the last-checked commit %s is not in the mirror (the remote rewrote its history); review it again in full with a fresh mirror", last)
	}
	commits, err := a.git.CommitsBetween(ctx, dir, last, head)
	if err != nil {
		return err
	}
	paths, err := a.git.ChangedPaths(ctx, dir, last, head)
	if err != nil {
		return err
	}
	a.p.Print(a.out, fmt.Sprintf("commits (%d):\n", len(commits)))
	for _, c := range commits {
		a.p.Print(a.out, "  "+c+"\n")
	}
	a.p.Print(a.out, fmt.Sprintf("changed paths (%d):\n", len(paths)))
	for _, p := range paths {
		a.p.Print(a.out, "  "+p+"\n")
	}
	return nil
}

// mark is W22 to W24.
func (a *App) mark(ctx context.Context, repo, sha, run string) error {
	if !shaPattern.MatchString(sha) {
		return fmt.Errorf("%q is not a full 40-digit lowercase commit sha", sha)
	}
	if run == none || !runPattern.MatchString(run) {
		return fmt.Errorf("run %q must be a single token with no whitespace, and not -", run)
	}
	entries, err := a.load()
	if err != nil {
		return err
	}
	i, err := find(entries, repo)
	if err != nil {
		return err
	}
	dir := entries[i].MirrorDir(a.root)
	if _, statErr := os.Stat(dir); statErr != nil {
		return fmt.Errorf("%s has no mirror at %s (run watchlist sync first): %w", entries[i].Name(), dir, statErr)
	}
	held, err := a.git.HasCommit(ctx, dir, sha)
	if err != nil {
		return err
	}
	if !held {
		return fmt.Errorf("%s is not in the mirror of %s; marking it would record a review of nothing", sha, entries[i].Name())
	}
	entries[i].LastSHA, entries[i].Run = sha, run
	if err := a.save(entries); err != nil {
		return err
	}
	a.p.Print(a.out, fmt.Sprintf("marked %s at %s (run %s)\n", entries[i].Name(), sha, run))
	return nil
}
