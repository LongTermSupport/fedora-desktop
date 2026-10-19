// Package ccproject is cc-project-dir: the directory host cc starts its session in (Plan
// 00167, issue #90). Claude Code reads a project's .claude/settings.json (its hooks, its status
// line) from the session's working directory only, so a session started anywhere but at the
// top of a git work tree runs without them. cc runs this first and cds to what it prints.
//
// What it does, numbered as its tests are:
//
//   - P1. -h/--help prints the usage on stdout, exit 0. Any other argument is a usage error,
//     exit 64, nothing run.
//   - P2. Built without -ldflags "-X main.RootDir=<checkout>": exit 1, naming the play that
//     builds it, nothing run.
//   - P3. The working directory is inside a git work tree: its top level on stdout, exit 0,
//     nothing asked and nothing on stderr. A subdirectory gives the top, so the session loads
//     the repository's settings.
//   - P4. git not installed: `git is not installed`, exit 1; never reported as "not in a
//     repository". Any other failure to run git: exit 1.
//   - P5. Outside a work tree with no one to ask (stdin is not a terminal, or
//     CCY_SESSION_RESTORE is set: a restore has no answer to give): the refusal, exit 1,
//     nothing read.
//   - P6. Outside a work tree at a terminal: the directory is named and the fedora-desktop
//     checkout offered, `[Y/n]` on stderr. Empty, y or yes (any case) prints the checkout on
//     stdout, exit 0; n or no is the refusal, exit 1. Anything else re-prompts, up to
//     maxTries asks, then the refusal, exit 1. End of input is cancelled: the refusal, exit 1.
//   - P7. The checkout is not a git work tree (moved, deleted): exit 1 before asking, naming
//     it and the play that records it.
//
// The refusal names the directory and the ways forward: cd into a project, or run claude.
// stdout is only the directory; every message is on stderr.
package ccproject

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	tool     = "cc-project-dir"
	usage    = "usage: cc-project-dir   (prints the directory cc starts its session in)\n"
	maxTries = 3
	play     = "playbooks/imports/play-claude-yolo.yml"
)

var errCancelled = errors.New("cancelled")

// App is one run. Every field is a seam a test replaces.
type App struct {
	Runner execx.Runner
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// IsTerminal reports whether a stream is a terminal; it is asked of Stdin.
	IsTerminal func(any) bool
	Getenv     func(string) string
	Getwd      func() (string, error)
	// RootDir is the fedora-desktop checkout baked in at build time.
	RootDir string

	out term.Printer
}

// NewApp is a run of the real process with the checkout root it was built with.
func NewApp(root string) *App {
	return &App{
		Runner: execx.OS{}, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
		IsTerminal: term.FileIsTerminal, Getenv: os.Getenv, Getwd: os.Getwd, RootDir: root,
	}
}

// Run decides the directory and returns the exit code.
func (a *App) Run(ctx context.Context, args []string) term.ExitCode {
	return a.finish(a.run(ctx, args))
}

func (a *App) finish(code term.ExitCode) term.ExitCode {
	if a.out.Err() != nil && code == term.ExitOK {
		return term.ExitFailure
	}
	return code
}

func (a *App) sayf(format string, args ...any) {
	a.out.Print(a.Stderr, fmt.Sprintf(format, args...))
}

func (a *App) failf(format string, args ...any) term.ExitCode {
	a.sayf(tool+": "+format+"\n", args...)
	return term.ExitFailure
}

func (a *App) run(ctx context.Context, args []string) term.ExitCode {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		a.out.Print(a.Stdout, usage)
		return term.ExitOK
	}
	if len(args) > 0 {
		a.sayf("%s: unexpected argument %q\n%s", tool, args[0], usage)
		return term.ExitUsage
	}
	if a.RootDir == "" {
		return a.failf("built without -ldflags \"-X main.RootDir=<checkout>\"; build it with %s, which bakes the checkout in", play)
	}
	cwd, err := a.Getwd()
	if err != nil {
		return a.failf("cannot read the working directory: %v", err)
	}
	git := gitx.New(a.Runner)
	inside, err := git.ProjectHere(ctx, cwd)
	if err != nil {
		if execx.IsNotInstalled(err) {
			return a.failf("git is not installed, so cc cannot tell whether %s is in a project (re-run %s)", cwd, play)
		}
		return a.failf("%v", err)
	}
	if inside {
		top, err := git.TopLevel(ctx, cwd)
		if err != nil {
			return a.failf("%v", err)
		}
		a.out.Print(a.Stdout, top+"\n")
		return term.ExitOK
	}
	return a.offer(ctx, git, cwd)
}

// offer asks whether to start in the checkout instead of cwd, which is in no work tree.
func (a *App) offer(ctx context.Context, git *gitx.Git, cwd string) term.ExitCode {
	if !a.IsTerminal(a.Stdin) || a.Getenv("CCY_SESSION_RESTORE") != "" {
		return a.refuse(cwd)
	}
	if ok, err := git.ProjectHere(ctx, a.RootDir); err != nil || !ok {
		return a.failf("the fedora-desktop checkout recorded at build time, %s, is not a git work tree; re-run %s from the checkout", a.RootDir, play)
	}
	a.sayf("cc: %s is not in a git repository, so no project settings (hooks, status line) would load.\n", cwd)
	yes, err := a.ask()
	if errors.Is(err, errCancelled) {
		a.sayf("cc: cancelled.\n")
		return a.refuse(cwd)
	}
	if err != nil {
		a.sayf("cc: %v\n", err)
		return a.refuse(cwd)
	}
	if !yes {
		return a.refuse(cwd)
	}
	a.out.Print(a.Stdout, a.RootDir+"\n")
	return term.ExitOK
}

// ask is the bounded [Y/n] loop.
func (a *App) ask() (bool, error) {
	in := bufio.NewReader(a.Stdin)
	for range maxTries {
		a.sayf("Start cc in the fedora-desktop checkout, %s, instead? [Y/n] ", a.RootDir)
		line, err := in.ReadString('\n')
		if err != nil {
			a.sayf("\n")
			return false, errCancelled
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "", "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		a.sayf("Please answer y or n.\n")
	}
	return false, fmt.Errorf("no valid answer after %d tries", maxTries)
}

func (a *App) refuse(cwd string) term.ExitCode {
	a.sayf("cc: not starting: %s is not in a git repository.\n"+
		"  cd into a project first, or run 'claude' to start a session here without one.\n", cwd)
	return term.ExitFailure
}
