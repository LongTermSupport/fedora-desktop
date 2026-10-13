package teams

import (
	"context"
	"fmt"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// passwordRow sets a new Element password for the person on row.
func (r *pickerRun) passwordRow(ctx context.Context, row Row) (result, error) {
	if row.Kind != kindHuman {
		return r.note(ctx, "Ctrl-P sets a person's Element password: choose one of the team's people (KIND human).")
	}
	if _, err := r.setPassword(ctx, row.Team, row.Member); err != nil {
		return done, err
	}
	r.a.backToList(ctx)
	return done, nil
}

// setPassword asks, then sets name's password on team and shows it once. It says whether one
// was set.
func (r *pickerRun) setPassword(ctx context.Context, team, name string) (bool, error) {
	a := r.a
	yes, err := a.confirm(ctx, "New Element password for "+name+"?",
		"Sets a new password for "+name+" on "+team+"'s homeserver and shows it here, once: it is stored nowhere. "+
			name+"'s other Element sessions are signed out.",
		"set a new password for "+name, false)
	if err != nil {
		return false, err
	}
	if !yes {
		a.tell("No password was set for " + name + ".")
		return false, nil
	}
	if !a.sudoReady(ctx, "A password is set as root, by agent-bus (the team's admin token is root's).") {
		a.tell("No password was set for " + name + ".")
		return false, nil
	}
	password, err := a.Bus.HumanPassword(ctx, team, name)
	if err != nil {
		a.reportRootErr(err)
		a.tell("No password was set for " + name + ", for the reason above.")
		return false, nil
	}
	r.showLogin(team, name, password)
	return true, r.offerCopy(ctx, name, password)
}

// showLogin prints what to sign in to Element with, to the terminal only: it is shown once and
// kept nowhere.
func (r *pickerRun) showLogin(team, name, password string) {
	a := r.a
	url := r.snap.URL[team]
	if url == "" {
		url = "unknown (not declared)"
	}
	server := r.snap.Server[team]
	if server == "" {
		server = team + ".agent-bus.internal"
	}
	shown := password
	if a.Term.ColourOn(a.Stderr) {
		shown = cBold + password + cReset
	}
	a.print(a.Stderr, "\n")
	a.Term.Say("Element sign-in for " + name + ", team " + team)
	a.print(a.Stderr, fmt.Sprintf("  %-11s %s\n", "Homeserver", url))
	a.print(a.Stderr, fmt.Sprintf("  %-11s %s\n", "Username", name))
	a.print(a.Stderr, fmt.Sprintf("  %-11s %s\n", "Matrix ID", "@"+name+":"+server))
	a.print(a.Stderr, fmt.Sprintf("  %-11s %s\n", "Password", shown))
	a.tell("Shown this once and stored nowhere. The team's Element profile has the homeserver set already.")
}

// offerCopy, in a Wayland session, offers to put the password on the clipboard. It goes to
// wl-copy on stdin, never in its arguments, which any process can read.
func (r *pickerRun) offerCopy(ctx context.Context, name, password string) error {
	a := r.a
	if a.Getenv("WAYLAND_DISPLAY") == "" {
		return nil
	}
	if !a.hasTool("wl-copy") {
		a.Term.PrintError("wl-copy is not installed, so the password cannot be put on the clipboard; copy it from above. " +
			"playbooks/imports/play-claude-yolo.yml installs wl-clipboard.")
		return nil
	}
	yes, err := a.confirm(ctx, "Copy the password?",
		"Put "+name+"'s new password on the clipboard, to paste into Element. A clipboard manager may keep a copy.",
		"copy the password", true)
	if err != nil || !yes {
		return err
	}
	if !a.copyToClipboard(ctx, password) {
		a.Term.PrintError("wl-copy failed (above), so the password is not on the clipboard; copy it from above.")
		return nil
	}
	a.tell("The password is on the clipboard.")
	return nil
}

// copyToClipboard puts text on the clipboard; it says whether wl-copy took it.
func (a *App) copyToClipboard(ctx context.Context, text string) bool {
	_, err := a.Runner.Run(ctx, execx.Cmd{Name: "wl-copy", Stdin: strings.NewReader(text), Stderr: a.Stderr})
	return err == nil
}

// sudoReady is whether sudo is ready for a root command; when it is not, it has said why.
func (a *App) sudoReady(ctx context.Context, why string) bool {
	return a.Sudo.Ready(ctx, why) == nil
}
