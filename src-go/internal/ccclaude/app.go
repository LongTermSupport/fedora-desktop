// Package ccclaude is cc-claude: the claude that host cc starts, on both of its launch paths
// (plain, and after `pingbus seat exec` for --teams and --bus). It gives the session the
// environment a ccy session gets, then becomes claude in the same process, so the pid cc and
// pingbus see is claude's.
//
// What it does, numbered as its tests are:
//
//   - C1. Every argument goes to claude unchanged (but C7's first `--`); cc-claude has no
//     options of its own. claude
//     replaces cc-claude (no fork), found on PATH.
//   - C2. Each of Defaults unset or empty is set to its value: CLAUDE_CODE_AUTO_COMPACT_WINDOW
//     to DefaultWindow, so Claude Code compacts at that many tokens and not near the whole
//     window of a 1M-context model, and CLAUDE_CODE_DISABLE_MOUSE_CLICKS to 1, so a click in
//     the terminal never answers a prompt or picks a menu item.
//   - C3. A value already set is kept, as ccy keeps it: the owner's override wins.
//   - C4. claude not on PATH: `claude is not installed`, exit 1. Any other failure to exec:
//     exit 1, naming the error. Never a silent no-op.
//   - C5. Each of Defaults is the default the ccy launcher passes into its container.
//   - C7. The wrapper form `cc-claude -- claude <args>` (Plan 00209 Task 5.1 H5), which
//     `teamchat run --claude` needs to find claude by name: a first `--` followed by `claude`
//     is dropped with that `claude`, and the arguments after them go to claude as in C1. A
//     first `--` followed by anything else is not the wrapper form and goes to claude
//     unchanged (C1), so `cc -- <prompt>` reaches claude as `claude -- <prompt>`; only a
//     prompt that is the one word `claude` cannot be given that way.
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
	// MouseClicksVar turns off Claude Code's mouse-click answers to prompts and menus.
	MouseClicksVar = "CLAUDE_CODE_DISABLE_MOUSE_CLICKS"
	// wrapperSeparator opens C7's wrapper form.
	wrapperSeparator = "--"
)

// Default is one variable cc-claude sets when the environment leaves it unset or empty.
type Default struct {
	Name  string
	Value string
}

// Defaults are the variables ccy passes its container, in the order cc-claude sets them.
var Defaults = []Default{{WindowVar, DefaultWindow}, {MouseClicksVar, "1"}}

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
	if len(args) > 1 && args[0] == wrapperSeparator && args[1] == "claude" {
		args = args[2:]
	}
	cmd := execx.Cmd{Name: "claude", Args: args}
	for _, d := range Defaults {
		if a.Getenv(d.Name) == "" {
			cmd.Env = append(cmd.Env, d.Name+"="+d.Value)
		}
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
