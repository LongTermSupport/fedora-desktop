// Package ccclaude is cc-claude: the claude that host cc starts, on both of its launch paths
// (plain, and after `pingbus seat exec` for --teams and --bus). It gives the session the
// environment a ccy session gets, then becomes claude in the same process, so the pid cc and
// pingbus see is claude's.
//
// What it does, numbered as its tests are:
//
//   - C1. Every argument goes to claude unchanged; cc-claude has no options of its own. claude
//     replaces cc-claude (no fork), found on PATH.
//   - C2. CLAUDE_CODE_AUTO_COMPACT_WINDOW unset or empty is set to DefaultWindow, so Claude
//     Code compacts at that many tokens and not near the whole window of a 1M-context model.
//   - C3. A value already set is kept, as ccy keeps it: the owner's override wins.
//   - C4. claude not on PATH: `claude is not installed`, exit 1. Any other failure to exec:
//     exit 1, naming the error. Never a silent no-op.
//   - C5. DefaultWindow is the default the ccy launcher passes into its container.
//
// stdout is claude's; cc-claude writes only errors, to stderr.
package ccclaude

import (
	"fmt"
	"io"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	tool = "cc-claude"
	play = "playbooks/imports/play-claude-yolo.yml"
	// WindowVar is Claude Code's auto-compact window: the context size it compacts at.
	WindowVar = "CLAUDE_CODE_AUTO_COMPACT_WINDOW"
	// DefaultWindow is that size in tokens, ccy's (claude-yolo, Plan 00133).
	DefaultWindow = "600000"
)

// App is one run. Every field is a seam a test replaces.
type App struct {
	Execer execx.Execer
	Stderr io.Writer
	Getenv func(string) string

	out term.Printer
}

// NewApp is a run of the real process.
func NewApp() *App {
	return &App{Execer: execx.OS{}, Stderr: os.Stderr, Getenv: os.Getenv}
}

// Run execs claude; it returns only when that failed, or under a fake.
func (a *App) Run(args []string) term.ExitCode {
	cmd := execx.Cmd{Name: "claude", Args: args}
	if a.Getenv(WindowVar) == "" {
		cmd.Env = []string{WindowVar + "=" + DefaultWindow}
	}
	if err := a.Execer.Exec(cmd); err != nil {
		if execx.IsNotInstalled(err) {
			a.out.Print(a.Stderr, fmt.Sprintf("%s: claude is not installed on this host, so cc cannot start a session (install Claude Code, then run cc again; %s installs cc)\n", tool, play))
		} else {
			a.out.Print(a.Stderr, fmt.Sprintf("%s: cannot start claude: %v\n", tool, err))
		}
		return term.ExitFailure
	}
	return term.ExitOK
}
