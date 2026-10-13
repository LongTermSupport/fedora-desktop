package teams

import (
	"context"
	"fmt"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// create is ccy-teams create: the checkout must be there, then the wizard.
func (a *App) create(ctx context.Context, args []string) int {
	if err := a.needCheckout(); err != nil {
		return term.ExitFailure
	}
	return a.newCreator().run(ctx, args)
}

// newCreator is the create wizard over the App's adapters. Its Term is its own, for the step
// counters live there, and the offers after create are the picker's own password and Element
// flows.
func (a *App) newCreator() *creator {
	return &creator{
		declare: a.Declare, gum: a.Gum, git: a.Git, term: a.freshTerm(), runner: a.Runner,
		stdin: a.Stdin, stdout: a.Stdout,
		cwd: a.here(), hostVars: a.HostVars, home: a.Home, launcher: a.Launcher,
		lookPath: a.LookPath,
		applyTeams: func(ctx context.Context, teams ...string) error {
			return a.Ops.applyTeams(ctx, teams)
		},
		newPassword: a.newPasswordFor,
		launchElement: func(ctx context.Context, team string) error {
			return offerResult(a.launchElement(ctx, team))
		},
		execLauncher: func(dir string, argv []string) error {
			return a.Exec(a.Launcher, argv, dir)
		},
	}
}

// newPasswordFor is the picker's own "new Element password" flow for a person of a team just
// made, which the wizard has already asked about: it reads the teams again, for the
// homeserver it shows the sign-in for.
func (a *App) newPasswordFor(ctx context.Context, team, human string) error {
	snap, err := a.refresh(ctx)
	if err != nil {
		return err
	}
	_, err = (&pickerRun{a: a, snap: snap}).issuePassword(ctx, team, human)
	return offerResult(err)
}

// offerResult turns what a picker flow answers into what an offer after create needs: the
// picker's leave with success is success, its cancelled exit is a cancellation, and any other
// leave is a failure that flow has already explained.
func offerResult(err error) error {
	code, left := exitCodeOf(err)
	switch {
	case err == nil, left && code == term.ExitOK:
		return nil
	case left && code == term.ExitCancelled:
		return ui.ErrCancelled
	case left:
		return fmt.Errorf("it stopped with exit %d", code)
	}
	return err
}
