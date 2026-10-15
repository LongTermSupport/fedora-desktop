// Package login is how a person gets into a team's Element: setting up the team's Element
// profile, opening it, and issuing a person's password (shown once, never stored, offered for
// the clipboard). It owns those flows; the picker decides when to call them.
package login

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/systemd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/ops"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// Login is the Element and password flows. Every dependency is a field so a test stands in for
// each.
type Login struct {
	Kit     *flow.Kit
	Board   *board.Collector
	Ops     *ops.Ops
	Bus     *agentbus.Client
	Systemd *systemd.Systemd
	Declare *declare.Client
	Runner  execx.Runner // wl-copy
	Getenv  func(string) string

	Home     string
	StateDir string
}

// New is the Login flows over a host.
func New(h *host.Host, kit *flow.Kit, b *board.Collector, o *ops.Ops) *Login {
	return &Login{
		Kit: kit, Board: b, Ops: o, Bus: h.Bus, Systemd: h.Systemd, Declare: h.Declare, Runner: h.Runner, Getenv: h.Getenv,
		Home: h.Home, StateDir: h.StateDir,
	}
}

// OpenElement opens the team in Element, setting up its profile first when it has none, and
// offering a password to the people who have never signed in. Element starts apart from the
// picker, which goes on.
func (l *Login) OpenElement(ctx context.Context, snap *board.Snapshot, team string) (flow.Result, error) {
	if res, err := l.ensureProfile(ctx, team); err != nil || res != flow.Done {
		return res, err
	}
	for _, row := range snap.Rows {
		if row.Team != team || row.Kind != board.KindHuman || snap.SignedIn[team+"|"+row.Member] != board.WordNeverSignedIn {
			continue
		}
		l.Kit.Tell(row.Member + " has never signed in to " + team + ".")
		set, err := l.SetPassword(ctx, snap, team, row.Member)
		if err != nil {
			return flow.Done, err
		}
		if set {
			l.Kit.Tell("Copy it now. Press Enter to open Element.")
			l.Kit.WaitForEnter(ctx)
		}
	}
	l.Kit.Tell("Opening " + team + " in Element.")
	return flow.Done, l.LaunchElement(ctx, team)
}

// LaunchElement starts the team's Element apart from this terminal, in a scope of its own, and
// returns: the picker goes on. It ends the picker with a failure when Element did not start,
// which is the answer, not a side effect of an unchecked error. Who is signed in is read again
// afterwards, as the person is about to sign in.
func (l *Login) LaunchElement(ctx context.Context, team string) error {
	log := filepath.Join(l.StateDir, "element-"+team+".log")
	err := l.Systemd.LaunchElement(ctx, systemd.Launch{
		Team: team, Desktop: l.Ops.ElementLauncher(team), PID: os.Getpid(), Log: log,
	})
	if launchErr, ok := errors.AsType[*systemd.LaunchError](err); ok {
		l.Kit.Term.PrintError("Element did not start; its messages are in " + l.Kit.Abbrev(launchErr.Log) + ":")
		if launchErr.Tail != "" {
			l.Kit.Tell(launchErr.Tail)
		}
		if launchErr.TailErr != nil {
			l.Kit.Tell(launchErr.TailErr.Error())
		}
		return flow.Leave(term.ExitFailure)
	}
	if err != nil {
		l.Kit.Term.PrintError(err.Error())
		return flow.Leave(term.ExitFailure)
	}
	l.Board.ForgetDevices(team)
	l.Kit.Tell("Element is starting; its own messages go to " + l.Kit.Abbrev(log) + ".")
	return nil
}

// PasswordRow sets a new Element password for the person on row. Someone with Element
// sessions is asked first, with the sessions it would sign out named; someone with none is
// signing in for the first time and is asked nothing. Either way Element is offered at once.
func (l *Login) PasswordRow(ctx context.Context, snap *board.Snapshot, row board.Row) (flow.Result, error) {
	if row.Kind != board.KindHuman {
		return l.Kit.Note(ctx, "A new Element password is for one of the team's people (KIND human): choose one of their rows.")
	}
	set, err := l.passwordFor(ctx, snap, row)
	if err != nil {
		return flow.Done, err
	}
	if set {
		if err = l.offerElement(ctx, row.Team); err != nil {
			return flow.Done, err
		}
	}
	l.Kit.BackToList(ctx)
	return flow.Done, nil
}

// SignOutPhrase is what a new password does to name's Element sessions, in words that follow
// "New password": "signs out guest's 2 Element sessions". It is empty when there are none.
func SignOutPhrase(snap *board.Snapshot, team, name string) string {
	count, known := snap.SessionsOf(team, name)
	switch {
	case !known:
		return "signs out " + name + "'s Element sessions"
	case count == 0:
		return ""
	case count == 1:
		return "signs out " + name + "'s 1 Element session"
	}
	return "signs out " + name + "'s " + strconv.Itoa(count) + " Element sessions"
}

// sessionsWarning is SignOutPhrase as the sentence the question carries.
func sessionsWarning(snap *board.Snapshot, team, name string) string {
	if phrase := SignOutPhrase(snap, team, name); phrase != "" {
		return "This " + phrase + "."
	}
	return name + " has no Element session to sign out."
}

// SetPassword asks, then sets name's password on team and shows it once. It says whether one
// was set.
func (l *Login) SetPassword(ctx context.Context, snap *board.Snapshot, team, name string) (bool, error) {
	yes, err := l.Kit.Confirm(ctx, "New Element password for "+name+"?",
		"Sets a new password for "+name+" on "+team+"'s homeserver and shows it here, once: it is stored nowhere. "+
			sessionsWarning(snap, team, name),
		"set a new password for "+name, false)
	if err != nil {
		return false, err
	}
	if !yes {
		l.Kit.Tell("No password was set for " + name + ".")
		return false, nil
	}
	return l.IssuePassword(ctx, snap, team, name)
}

// IssuePassword sets name's password on team and shows it once, the question having been
// asked already. It says whether one was set.
func (l *Login) IssuePassword(ctx context.Context, snap *board.Snapshot, team, name string) (bool, error) {
	if !l.Kit.SudoReady(ctx, "A password is set as root, by agent-bus (the team's admin token is root's).") {
		l.Kit.Tell("No password was set for " + name + ".")
		return false, nil
	}
	password, err := l.Bus.HumanPassword(ctx, team, name)
	if err != nil {
		l.Kit.ReportRootErr(err)
		l.Kit.Tell("No password was set for " + name + ", for the reason above.")
		return false, nil
	}
	// The person's other Element sessions were signed out.
	l.Board.ForgetDevices(team)
	l.showLogin(snap, team, name, password)
	return true, l.offerCopy(ctx, name, password)
}

// NewPassword is the sign-in offered straight after a team is made, for a person of it: the
// teams are read again for the homeserver the sign-in is shown for. A leave with success is
// success, the cancelled exit is a cancellation, and any other leave is a failure this flow
// has already explained.
func (l *Login) NewPassword(ctx context.Context, team, human string) error {
	snap, err := l.Board.Refresh(ctx)
	if err != nil {
		return err
	}
	_, err = l.IssuePassword(ctx, snap, team, human)
	return offerResult(err)
}

// SignInGap is, after teams were applied (whether or not the apply passed: the play creates
// each person's account with a password nobody is shown), the sign-in of everyone declared in
// them who has no Element session: offered when ask, else said with the exact way to do it.
// What cannot be read (the homeserver may not be up after a failed apply) is said too, with
// the same way, never skipped.
func (l *Login) SignInGap(ctx context.Context, teams []string, ask bool) error {
	snap, err := l.Board.Collect(ctx)
	if err != nil {
		l.Kit.ReportErr(err)
		l.Kit.Tell("Whether the people of " + strings.Join(teams, " ") + " can sign in to Element could not be read (above).")
		l.Kit.Tell(howToSignIn(teams[0]))
		return nil
	}
	for _, team := range teams {
		if err = l.signInGapOf(ctx, snap, team, ask); err != nil {
			return offerResult(err)
		}
	}
	return nil
}

// howToSignIn is the way a person who has no session gets a password, for the first one of
// team's people (the picker offers each person who has never signed in).
func howToSignIn(team string) string {
	return "To sign in: ccy-teams, Enter on " + team + ", choose Open " + team +
		" in Element: it offers each person who has never signed in a password."
}

// Open is Element opened straight after a team is made, with the same answers as NewPassword.
func (l *Login) Open(ctx context.Context, team string) error {
	return offerResult(l.LaunchElement(ctx, team))
}

func (l *Login) signInGapOf(ctx context.Context, snap *board.Snapshot, team string, ask bool) error {
	if why := snap.MembersUnknown[team]; why != "" {
		l.Kit.Tell("Who is signed in to " + team + " could not be read (" + why + ").")
		l.Kit.Tell(howToSignIn(team))
		return nil
	}
	for _, row := range snap.Rows {
		if row.Team != team || row.Kind != board.KindHuman {
			continue
		}
		var err error
		switch word := snap.SignedIn[team+"|"+row.Member]; {
		case word == board.WordNeverSignedIn:
			err = l.offerSignIn(ctx, snap, team, row.Member, ask)
		case strings.HasSuffix(word, " signed in"):
		default:
			l.Kit.Tell("Whether " + row.Member + " has signed in to " + team + " could not be read (" + word + ").")
			l.Kit.Tell(howToSignIn(team))
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (l *Login) offerSignIn(ctx context.Context, snap *board.Snapshot, team, name string, ask bool) error {
	l.Kit.Tell(name + " has never signed in to " + team + ".")
	if ask {
		yes, err := l.Kit.AskYes(ctx, "Sign "+name+" in to Element now? A password is set and shown here once.", true)
		if err != nil {
			return err
		}
		if yes {
			_, err = l.IssuePassword(ctx, snap, team, name)
			return err
		}
	}
	l.Kit.Tell(howToSignIn(team))
	return nil
}

// ensureProfile is Done when the team has an Element profile, setting one up when it has none.
func (l *Login) ensureProfile(ctx context.Context, team string) (flow.Result, error) {
	_, statErr := os.Stat(l.Ops.ElementLauncher(team))
	switch {
	case errors.Is(statErr, os.ErrNotExist):
		return l.setUpElement(ctx, team)
	case statErr != nil:
		l.Kit.Term.PrintError("the Element launcher could not be looked for: " + statErr.Error())
		return flow.Refused, flow.Leave(term.ExitFailure)
	}
	return flow.Done, nil
}

// passwordFor sets row's password, asking first unless the person has no Element session.
func (l *Login) passwordFor(ctx context.Context, snap *board.Snapshot, row board.Row) (bool, error) {
	if count, known := snap.SessionsOf(row.Team, row.Member); known && count == 0 {
		return l.IssuePassword(ctx, snap, row.Team, row.Member)
	}
	return l.SetPassword(ctx, snap, row.Team, row.Member)
}

// offerElement asks whether to open the team in Element now, setting its profile up first when
// it has none.
func (l *Login) offerElement(ctx context.Context, team string) error {
	yes, err := l.Kit.Confirm(ctx, "Open "+team+" in Element now?",
		"Element opens in a window of its own; ccy-teams stays here.", "open "+team+" in Element", true)
	if err != nil || !yes {
		return err
	}
	res, err := l.ensureProfile(ctx, team)
	if err != nil || res != flow.Done {
		return err
	}
	l.Kit.Tell("Opening " + team + " in Element.")
	return l.LaunchElement(ctx, team)
}

// setUpElement offers a missing Element profile: declare it, and run the play that writes it.
// Done means the profile now exists; anything else is why the caller should stop.
func (l *Login) setUpElement(ctx context.Context, team string) (flow.Result, error) {
	if err := l.Kit.NeedGum(); err != nil {
		return flow.Refused, err
	}
	l.Kit.Term.Say(team + " has no Element profile on this desktop yet.")
	yes, err := l.Kit.AskYes(ctx, "Set one up now? It installs Element Desktop if needed (sudo asks for your password).", true)
	if err != nil {
		return flow.Refused, err
	}
	if !yes {
		return l.Kit.Refuse(ctx, "Nothing set up. Opening the team in Element offers it again.")
	}
	if _, err := l.Declare.Element(ctx, team); err != nil {
		l.Kit.ReportErr(err)
		return l.Kit.Refuse(ctx, team+" was not set up in Element, for the reason above.")
	}
	if err := l.Ops.RunElementPlay(ctx, []string{team}); err != nil {
		return l.Kit.Refuse(ctx, team+" was not set up in Element, for the reason above.")
	}
	return flow.Done, nil
}

// showLogin prints what to sign in to Element with, to the terminal only: it is shown once and
// kept nowhere.
func (l *Login) showLogin(snap *board.Snapshot, team, name, password string) {
	url := snap.URL[team]
	if url == "" {
		url = "unknown (not declared)"
	}
	server := snap.Server[team]
	if server == "" {
		server = team + ".agent-bus.internal"
	}
	shown := password
	if l.Kit.Term.ColourOn(l.Kit.Stderr) {
		shown = term.Bold + password + term.Reset
	}
	l.Kit.Out.Print(l.Kit.Stderr, "\n")
	l.Kit.Term.Say("Element sign-in for " + name + ", team " + team)
	l.Kit.Out.Print(l.Kit.Stderr, fmt.Sprintf("  %-11s %s\n", "Homeserver", url))
	l.Kit.Out.Print(l.Kit.Stderr, fmt.Sprintf("  %-11s %s\n", "Username", name))
	l.Kit.Out.Print(l.Kit.Stderr, fmt.Sprintf("  %-11s %s\n", "Matrix ID", "@"+name+":"+server))
	l.Kit.Out.Print(l.Kit.Stderr, fmt.Sprintf("  %-11s %s\n", "Password", shown))
	l.Kit.Tell("Shown this once and stored nowhere. The team's Element profile has the homeserver set already.")
}

// offerCopy, in a Wayland session, offers to put the password on the clipboard. It goes to
// wl-copy on stdin, never in its arguments, which any process can read.
func (l *Login) offerCopy(ctx context.Context, name, password string) error {
	if l.Getenv("WAYLAND_DISPLAY") == "" {
		return nil
	}
	if !l.Kit.HasTool("wl-copy") {
		l.Kit.Term.PrintError("wl-copy is not installed, so the password cannot be put on the clipboard; copy it from above. " +
			"playbooks/imports/play-claude-yolo.yml installs wl-clipboard.")
		return nil
	}
	yes, err := l.Kit.Confirm(ctx, "Copy the password?",
		"Put "+name+"'s new password on the clipboard, to paste into Element. A clipboard manager may keep a copy.",
		"copy the password", true)
	if err != nil || !yes {
		return err
	}
	if !l.copyToClipboard(ctx, password) {
		l.Kit.Term.PrintError("wl-copy failed (above), so the password is not on the clipboard; copy it from above.")
		return nil
	}
	l.Kit.Tell("The password is on the clipboard.")
	return nil
}

// copyToClipboard puts text on the clipboard; it says whether wl-copy took it.
func (l *Login) copyToClipboard(ctx context.Context, text string) bool {
	_, err := l.Runner.Run(ctx, execx.Cmd{Name: "wl-copy", Stdin: strings.NewReader(text), Stderr: l.Kit.Stderr})
	return err == nil
}

func offerResult(err error) error {
	code, left := flow.ExitCodeOf(err)
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
