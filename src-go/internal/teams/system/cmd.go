package system

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// ExitUndecided is a team whose system cannot be decided yet.
const ExitUndecided = term.ExitConfig

// Cmd is `ccy-teams system <team>...`: one line per team on stdout, `<given> <id> <system>`, or
// the refusal on stderr and exit 64 (78 for a team that cannot be decided yet). The checkout is
// the working directory with its links resolved, the key the launch's seat take uses.
type Cmd struct {
	Getenv   func(string) string
	Getwd    func() (string, error)
	Stdout   io.Writer
	Stderr   io.Writer
	Declared func(context.Context) ([]string, error)
	Status   func(paths.TeamID) (status.Record, bool, error)
}

// Here is the seat store and the checkout key of the working directory, from HOME and
// XDG_STATE_HOME.
func Here(getenv func(string) string, getwd func() (string, error)) (paths.Store, paths.CheckoutKey, string, error) {
	state, err := paths.StateHome(getenv("HOME"), getenv("XDG_STATE_HOME"))
	if err != nil {
		return paths.Store{}, paths.CheckoutKey{}, "", fmt.Errorf("the state home: %w", err)
	}
	store, err := paths.SeatStore(state)
	if err != nil {
		return paths.Store{}, paths.CheckoutKey{}, "", fmt.Errorf("the seat store: %w", err)
	}
	wd, err := getwd()
	if err != nil {
		return paths.Store{}, paths.CheckoutKey{}, "", fmt.Errorf("the working directory: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(wd)
	if err != nil {
		return paths.Store{}, paths.CheckoutKey{}, "", fmt.Errorf("the working directory: %w", err)
	}
	key, err := paths.CheckoutKeyOf(resolved)
	if err != nil {
		return paths.Store{}, paths.CheckoutKey{}, "", fmt.Errorf("the checkout key of %s: %w", resolved, err)
	}
	return store, key, state, nil
}

// Lines resolves teams for the working directory's checkout.
func (c *Cmd) Lines(ctx context.Context, teams []string) ([]Line, error) {
	store, key, _, err := Here(c.Getenv, c.Getwd)
	if err != nil {
		return nil, err
	}
	return Resolve(ctx, teams, Sources{Status: c.Status, PoolHolds: PoolHolds(store, key), Declared: c.Declared})
}

// Run prints the lines, or refuses.
func (c *Cmd) Run(ctx context.Context, teams []string) term.ExitCode {
	lines, err := c.Lines(ctx, teams)
	if err != nil {
		return c.Fail(err)
	}
	for _, l := range lines {
		if _, err := fmt.Fprintf(c.Stdout, "%s %s %s\n", l.Given, l.ID, l.System); err != nil {
			return c.Fail(err)
		}
	}
	return term.ExitOK
}

// Fail reports err on stderr and is its exit code: 64 refused, 78 undecided, 1 otherwise.
func (c *Cmd) Fail(err error) term.ExitCode {
	code := term.ExitFailure
	switch {
	case errors.Is(err, ErrRefused):
		code = term.ExitUsage
	case errors.Is(err, ErrUndecided):
		code = ExitUndecided
	}
	if _, werr := fmt.Fprintf(c.Stderr, "ccy-teams system: %v.\n", err); werr != nil {
		return term.ExitFailure
	}
	return code
}
