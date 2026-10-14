// Package app is the one flow both freeze tools run, so the order of operations is written once:
// parse, help, refuse a list with a target, the host guard, the engine's preflight, one load, then
// the list, the menu or the one-shot. A tool supplies its grammar, its usage text and its engine.
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/freeze"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/freeze/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// TargetEngine is an Engine that can also resolve the command line's target to container names.
// A vanished group (a network, a bridge, an identity value) is fatal there, with the same words
// the menu says when it re-prompts.
type TargetEngine interface {
	freeze.Engine
	SelectTarget(ctx context.Context, inv freeze.Inventory, mode, value string, names []string) ([]string, error)
}

// Tool is everything that differs between the freeze tools.
type Tool struct {
	Spec   cli.Spec
	Usage  string // what --help prints to stdout, byte for byte
	Cfg    freeze.Config
	Engine TargetEngine
}

// ChooserFactory makes the person at the keyboard, and the function that lets go of the terminal
// when the run ends.
type ChooserFactory func() (freeze.Chooser, func() error)

// Run executes args and returns the exit status. Ctrl-C reaches an open prompt through the
// context. A nil chooser means the person at the terminal (fzf, else numbered prompts).
func Run(env freeze.Env, chooser freeze.Chooser, tool Tool, args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return freeze.ExitCode(env.Err, tool.Cfg.Tool, Execute(ctx, env, chooser, tool, args))
}

// Execute is Run without the signal handling and the exit status: the error the run ended with.
func Execute(ctx context.Context, env freeze.Env, chooser freeze.Chooser, tool Tool, args []string) error {
	return ExecuteClosing(ctx, env, func() (freeze.Chooser, func() error) {
		if chooser != nil {
			return chooser, func() error { return nil }
		}
		return terminalChooser(tool.Cfg, env)
	}, tool, args)
}

// ExecuteClosing is Execute with the chooser made only once the run needs one, and closed when it
// ends; a failure to close is joined to the run's own result.
func ExecuteClosing(ctx context.Context, env freeze.Env, newChooser ChooserFactory, tool Tool, args []string) error {
	parsed, err := cli.Parse(tool.Spec, args)
	if err != nil {
		return err
	}
	if parsed.Help {
		if _, err = fmt.Fprint(env.Out, tool.Usage); err != nil {
			return fmt.Errorf("write usage: %w", err)
		}
		return nil
	}
	// A verb that takes no target is refused before anything is asked of the machine.
	if parsed.List() && parsed.HasTarget() {
		return freeze.Die("'list' takes no target — it reports everything.")
	}
	// Called with the real paths and no arguments from the command line: nothing a caller can
	// pass loosens the guard.
	if err = freeze.AssertOnHost(tool.Cfg, env); err != nil {
		return err
	}
	if err = tool.Engine.Preflight(ctx); err != nil {
		return err
	}
	chooser, closer := newChooser()
	err = session(ctx, env, chooser, tool, parsed)
	if closeErr := closer(); closeErr != nil {
		err = errors.Join(err, closeErr)
	}
	return err
}

func session(ctx context.Context, env freeze.Env, chooser freeze.Chooser, tool Tool, parsed cli.Parsed) error {
	s := freeze.NewSession(tool.Cfg, env, tool.Engine, chooser)
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
	selected, err := tool.Engine.SelectTarget(ctx, s.Inventory(), parsed.Mode, parsed.Value, parsed.Names)
	if err != nil {
		return err
	}
	return s.DoAction(ctx, parsed.Action(), selected)
}

// terminalChooser is the person at the terminal, and the function that lets go of it.
func terminalChooser(cfg freeze.Config, env freeze.Env) (freeze.Chooser, func() error) {
	tc := &freeze.TTYChooser{
		Cfg: cfg, Env: env,
		Fzf: &ui.Picker{Runner: env.Runner, Stderr: env.Err},
	}
	return tc, tc.Close
}
