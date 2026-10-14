package lxcfreeze

import (
	"context"
	_ "embed" // usage.txt is embedded verbatim
	"io"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/freeze"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/freeze/app"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/freeze/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sudo"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// usageText is what --help prints to stdout: the text of the bash tool's usage(), byte for byte
// (it is not the header comment).
//
//go:embed usage.txt
var usageText string

var spec = cli.Spec{
	Tool: tool,
	Flags: []cli.Flag{
		{Name: "--all"},
		{Name: "--bridge", Needs: "a bridge name"},
	},
}

// App is one lxcfreeze run: the environment it touches (every seam of it a field, none of them
// reachable from the command line) and, for tests, a Chooser standing in for the person.
type App struct {
	Env freeze.Env
	// Stdin is where sudo reads a password it has to ask for.
	Stdin io.Reader
	// Chooser answers the menu; nil means the person at the terminal (fzf, else numbered prompts).
	Chooser freeze.Chooser
}

// NewApp is the production App on the process's own streams.
func NewApp() *App {
	return &App{Env: freeze.NewEnv(os.Stdout, os.Stderr, execx.OS{}), Stdin: os.Stdin}
}

// Run executes args (without the program name) and returns the exit status. Ctrl-C reaches an
// open prompt through the context.
func (a *App) Run(args []string) term.ExitCode {
	return app.Run(a.Env, a.Chooser, a.tool(), args)
}

// RunContext is Run on a caller-supplied context.
func (a *App) RunContext(ctx context.Context, args []string) term.ExitCode {
	return freeze.ExitCode(a.Env.Err, tool, app.Execute(ctx, a.Env, a.Chooser, a.tool(), args))
}

// plain is the environment as sudo's prompts see it: NO_COLOR is always set, because no freeze tool
// uses colour anywhere.
func (a *App) plain(key string) string {
	if key == "NO_COLOR" {
		return "1"
	}
	return a.Env.Getenv(key)
}

func (a *App) tool() app.Tool[record] {
	asker := &sudo.Sudo{
		Runner:  a.Env.Runner,
		Term:    &term.Term{Err: a.Env.Err, IsTerminal: a.Env.IsTerminal, Getenv: a.plain},
		Stdin:   a.Stdin,
		Stdout:  a.Env.Out,
		OpenTTY: a.Env.OpenTTY,
	}
	engine := &Engine{Sudo: asker, LookPath: a.Env.LookPath, Err: a.Env.Err}
	return app.Tool[record]{Spec: spec, Usage: usageText, Cfg: Config(), Engine: engine}
}
