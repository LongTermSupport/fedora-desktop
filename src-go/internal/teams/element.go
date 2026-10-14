package teams

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/systemd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// elementLauncher is the desktop file that starts the team's Element profile.
func (a *App) elementLauncher(team string) string {
	return filepath.Join(a.Home, ".local", "share", "applications", "agent-bus-"+team+"-element.desktop")
}

// openElement opens the team in Element, setting up its profile first when it has none, and
// offering a password to the people who have never signed in. Opening ends the picker.
func (r *pickerRun) openElement(ctx context.Context, team string) (result, error) {
	a := r.a
	_, statErr := os.Stat(a.elementLauncher(team))
	switch {
	case errors.Is(statErr, os.ErrNotExist):
		if res, err := r.setUpElement(ctx, team); err != nil || res != done {
			return res, err
		}
	case statErr != nil:
		a.Term.PrintError("the Element launcher could not be looked for: " + statErr.Error())
		return refused, leave(term.ExitFailure)
	}
	for _, row := range r.snap.Rows {
		if row.Team != team || row.Kind != kindHuman || r.snap.SignedIn[team+"|"+row.Member] != "never signed in" {
			continue
		}
		a.tell(row.Member + " has never signed in to " + team + ".")
		set, err := r.setPassword(ctx, team, row.Member)
		if err != nil {
			return done, err
		}
		if set {
			a.tell("Copy it now. Press Enter to open Element.")
			a.waitForEnter(ctx)
		}
	}
	a.tell("Opening " + team + " in Element.")
	return done, a.launchElement(ctx, team)
}

// setUpElement offers a missing Element profile: declare it, and run the play that writes it.
// done means the profile now exists; anything else is why the caller should stop.
func (r *pickerRun) setUpElement(ctx context.Context, team string) (result, error) {
	a := r.a
	if err := a.needGum(); err != nil {
		return refused, err
	}
	a.Term.Say(team + " has no Element profile on this desktop yet.")
	yes, err := a.askYes(ctx, "Set one up now? It installs Element Desktop if needed (sudo asks for your password).", true)
	if err != nil {
		return refused, err
	}
	if !yes {
		return r.refuse(ctx, "Nothing set up. Ctrl-E offers it again.")
	}
	if _, err := a.Declare.Element(ctx, team); err != nil {
		a.reportErr(err)
		return r.refuse(ctx, team+" was not set up in Element, for the reason above.")
	}
	if err := a.Ops.runElementPlay(ctx, []string{team}); err != nil {
		return r.refuse(ctx, team+" was not set up in Element, for the reason above.")
	}
	return done, nil
}

// launchElement starts the team's Element apart from this terminal. Ending the picker with a
// failure when it did not start is the answer, not a side effect of an unchecked error.
func (a *App) launchElement(ctx context.Context, team string) error {
	log := filepath.Join(a.StateDir, "element-"+team+".log")
	err := a.Systemd.LaunchElement(ctx, systemd.Launch{
		Team: team, Desktop: a.elementLauncher(team), PID: os.Getpid(), Log: log,
	})
	if launchErr, ok := errors.AsType[*systemd.LaunchError](err); ok {
		a.Term.PrintError("Element did not start; its messages are in " + a.abbrev(launchErr.Log) + ":")
		if launchErr.Tail != "" {
			a.tell(launchErr.Tail)
		}
		if launchErr.TailErr != nil {
			a.tell(launchErr.TailErr.Error())
		}
		return leave(term.ExitFailure)
	}
	if err != nil {
		a.Term.PrintError(err.Error())
		return leave(term.ExitFailure)
	}
	a.tell("Element is starting; its own messages go to " + a.abbrev(log) + ".")
	return leave(term.ExitOK)
}

// waitForEnter holds the screen until a line is typed.
func (a *App) waitForEnter(ctx context.Context) {
	tty, err := a.OpenTTY()
	if err != nil {
		a.print(a.Stderr, "\n")
		return
	}
	got, err := readLine(ctx, tty)
	if err != nil {
		a.Term.PrintError("the terminal could not be closed: " + err.Error())
	}
	if !got {
		a.print(a.Stderr, "\n")
	}
}
