package podfreeze

import (
	"context"
	_ "embed" // usage.txt is embedded verbatim
	"errors"
	"fmt"
	"os"
	"os/signal"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/freeze"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/freeze/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// usageText is what --help prints to stdout: the text of the bash tool's usage(), byte for byte
// (it is not the header comment).
//
//go:embed usage.txt
var usageText string

const needsValue = "a value"

var spec = cli.Spec{
	Tool: tool,
	Flags: []cli.Flag{
		{Name: "--ccy"},
		{Name: "--all"},
		{Name: "--network", Needs: "a network name"},
		{Name: "--github", Needs: needsValue},
		{Name: "--token", Needs: needsValue},
		{Name: "--ssh-key", Needs: needsValue},
	},
}

// App is one podfreeze run: the environment it touches (every seam of it a field, none of them
// reachable from the command line) and, for tests, a Chooser standing in for the person.
type App struct {
	Env freeze.Env
	// Chooser answers the menu; nil means the person at the terminal (fzf, else numbered prompts).
	Chooser freeze.Chooser
}

// NewApp is the production App on the process's own streams.
func NewApp() *App {
	return &App{Env: freeze.NewEnv(os.Stdout, os.Stderr, execx.OS{})}
}

// Run executes args (without the program name) and returns the exit status. Ctrl-C reaches an
// open prompt through the context.
func (a *App) Run(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return a.RunContext(ctx, args)
}

// RunContext is Run on a caller-supplied context.
func (a *App) RunContext(ctx context.Context, args []string) int {
	return freeze.ExitCode(a.Env.Err, tool, a.execute(ctx, args))
}

func (a *App) execute(ctx context.Context, args []string) error {
	parsed, err := cli.Parse(spec, args)
	if err != nil {
		return err
	}
	if parsed.Help {
		return a.printUsage()
	}
	// A verb that takes no target is refused before anything is asked of the machine.
	if parsed.List() && parsed.HasTarget() {
		return freeze.Die("'list' takes no target — it reports everything.")
	}
	cfg := Config()
	// Called with the real paths and no arguments from the command line: nothing a caller can
	// pass loosens the guard.
	if err = freeze.AssertOnHost(cfg, a.Env); err != nil {
		return err
	}
	engine := &Engine{Runner: a.Env.Runner, LookPath: a.Env.LookPath, Err: a.Env.Err}
	if err = engine.Preflight(ctx); err != nil {
		return err
	}
	chooser, closer := a.chooser(cfg)
	err = a.session(ctx, cfg, engine, chooser, parsed)
	if closeErr := closer(); closeErr != nil {
		err = errors.Join(err, closeErr)
	}
	return err
}

func (a *App) printUsage() error {
	if _, err := fmt.Fprint(a.Env.Out, usageText); err != nil {
		return fmt.Errorf("write usage: %w", err)
	}
	return nil
}

func (a *App) session(ctx context.Context, cfg freeze.Config, engine *Engine, chooser freeze.Chooser, parsed cli.Parsed) error {
	s := freeze.NewSession(cfg, a.Env, engine, chooser)
	s.Action, s.DryRun = parsed.Action(), parsed.DryRun
	// One load serves the one-shot paths; the menu reuses it for its first pass.
	if err := s.Refresh(ctx); err != nil {
		return err
	}
	switch {
	case parsed.List():
		return s.List()
	case !parsed.HasTarget():
		return s.Loop(ctx)
	}
	selected, err := engine.SelectTarget(ctx, s.Inventory(), parsed.Mode, parsed.Value, parsed.Names)
	if err != nil {
		return err
	}
	return s.DoAction(ctx, parsed.Action(), selected)
}

// chooser is the person at the terminal unless a test stands in, and the function that lets go of
// the terminal when the run ends.
func (a *App) chooser(cfg freeze.Config) (freeze.Chooser, func() error) {
	if a.Chooser != nil {
		return a.Chooser, func() error { return nil }
	}
	tc := &freeze.TTYChooser{
		Cfg: cfg, Env: a.Env,
		Fzf: &ui.Picker{Runner: a.Env.Runner, Stderr: a.Env.Err},
	}
	return tc, tc.Close
}
