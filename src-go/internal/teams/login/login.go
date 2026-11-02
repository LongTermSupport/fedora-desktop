// Package login is how a person gets into a team's Element: setting up the team's Element
// profile, opening it, and issuing a person's password (shown once, never stored, offered for
// the clipboard). It owns those flows; the picker decides when to call them.
package login

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/playlock"
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
	Runner  execx.Runner // wl-copy, wl-paste
	Getenv  func(string) string

	// copied is the password this run put on the clipboard, until SettleClipboard clears it.
	copied string

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
// picker, which goes on; a copied password is settled once it has started.
func (l *Login) OpenElement(ctx context.Context, snap *board.Snapshot, team string) (flow.Result, error) {
	defer l.SettleClipboard(ctx)
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
			// No "copy it": with Clipboard Indicator on, the password was not offered for the
			// clipboard and is typed from the screen.
			l.Kit.Tell("Press Enter to open Element, then sign in there as " + row.Member + " with the password shown above.")
			l.Kit.WaitForEnter(ctx)
		}
	}
	l.Kit.Tell("Opening " + team + " in Element.")
	return flow.Done, l.LaunchElement(ctx, team)
}

// OpenEveryTeam opens every team that has an Element profile on this desktop, each in its own
// Element window: a team is its own homeserver, and an Element window is signed in to one
// account. A team with no profile is named with how to set one up; nothing is asked, and no
// password is set (opening that one team does both).
func (l *Login) OpenEveryTeam(ctx context.Context, snap *board.Snapshot) (flow.Result, error) {
	var ready []string
	for _, team := range snap.Teams {
		_, statErr := os.Stat(l.Ops.ElementLauncher(team))
		switch {
		case statErr == nil:
			ready = append(ready, team)
		case errors.Is(statErr, os.ErrNotExist):
			l.Kit.Tell(team + " has no Element profile on this desktop yet: Open " + team + " in Element (on its row's menu) sets one up.")
		default:
			l.Kit.Term.PrintError("the Element launcher of " + team + " could not be looked for: " + statErr.Error())
			return flow.Refused, flow.Leave(term.ExitFailure)
		}
	}
	if len(ready) == 0 {
		return flow.Done, nil
	}
	l.Kit.Tell("Opening " + strings.Join(ready, ", ") + " in Element: one window each (each team is its own Matrix server, and an Element window holds one account).")
	for _, team := range ready {
		if err := l.LaunchElement(ctx, team); err != nil {
			return flow.Done, err
		}
	}
	return flow.Done, nil
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
	if err == nil && set {
		err = l.offerElement(ctx, row.Team)
	}
	l.SettleClipboard(ctx)
	if err != nil {
		return flow.Done, err
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
// was set. The answer defaults to Yes only for someone with no Element session (read, not
// unknown): they cannot sign in without a password, and a new one signs nothing out. Anyone
// else defaults to No, as a new password signs out their other devices.
func (l *Login) SetPassword(ctx context.Context, snap *board.Snapshot, team, name string) (bool, error) {
	count, known := snap.SessionsOf(team, name)
	yes, err := l.Kit.Confirm(ctx, passwordHeading(snap, team, name),
		"New Element password for "+name+"? Sets a new password for "+name+" on "+team+"'s homeserver and shows it here, once: it is stored nowhere. "+
			sessionsWarning(snap, team, name),
		"set a new password for "+name, known && count == 0)
	if err != nil {
		return false, err
	}
	if !yes {
		l.Kit.Tell("No password was set for " + name + ".")
		return false, nil
	}
	return l.IssuePassword(ctx, snap, team, name)
}

// passwordHeading is the heading of the questions about name's Element password in team.
func passwordHeading(snap *board.Snapshot, team, name string) ui.Heading {
	context := name + " in team " + team
	if count, known := snap.SessionsOf(team, name); known {
		context += " · " + strconv.Itoa(count) + " Element sessions"
	}
	return flow.Here(team, name, "Element password").About(context)
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
	return true, l.offerCopy(ctx, passwordHeading(snap, team, name), name, password)
}

// NewPassword is the sign-in offered straight after a team is made, for a person of it: the
// teams are read again for the homeserver the sign-in is shown for. A copied password is left
// on the clipboard for the wizard's SettleClipboard, once Element has been offered. A leave with success is
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
	if len(teams) == 0 {
		return nil
	}
	defer l.SettleClipboard(ctx)
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

// SettleClipboard ends a copy: it waits for Enter (at most pasteWait), then clears the
// clipboard if it still holds the password, and leaves it alone if something else was copied
// since. A read or a clear that fails is said, with the password possibly still on the
// clipboard. The read and the clear run even when ctx is done (Ctrl-C), so an interrupted run
// does not leave the password behind. The primary selection is never set, so it is not touched.
// It does nothing when no password was copied.
func (l *Login) SettleClipboard(ctx context.Context) {
	password := l.copied
	if password == "" {
		return
	}
	l.copied = ""
	l.Kit.Tell("Paste the password into Element, then press Enter here (within " + pasteWaitWords + ") to clear it from the clipboard.")
	waitCtx, cancel := context.WithTimeout(ctx, pasteWait)
	l.Kit.WaitForEnter(waitCtx)
	timedOut := errors.Is(waitCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil
	cancel()
	if timedOut {
		l.Kit.Tell("No Enter within " + pasteWaitWords + ".")
	}
	const stillThere = "the password may still be on the clipboard; copy something else to replace it."
	held, err := l.clipboardHolds(context.WithoutCancel(ctx), password)
	if err != nil {
		l.Kit.Term.PrintError("the clipboard could not be read back (" + err.Error() + "), so it was not cleared: " + stillThere)
		return
	}
	if !held {
		l.Kit.Tell("The clipboard no longer holds the password (something else was copied), so it was left as it is.")
		return
	}
	if err = l.clearClipboard(context.WithoutCancel(ctx)); err != nil {
		l.Kit.Term.PrintError("wl-copy --clear failed (" + err.Error() + "): " + stillThere)
		return
	}
	l.Kit.Tell("The password was cleared from the clipboard. GNOME or a clipboard history may still keep a copy of it: " +
		"copy something else if in doubt.")
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
		set, err := l.askSignIn(ctx, snap, team, name)
		if err != nil || set {
			return err
		}
	}
	l.Kit.Tell(howToSignIn(team))
	return nil
}

// askSignIn asks, then sets the password. Leaving the question (Ctrl-C) is a no, said without
// "nothing was changed": the apply ran. It says whether a password was set. No is the default
// (decision A12).
func (l *Login) askSignIn(ctx context.Context, snap *board.Snapshot, team, name string) (bool, error) {
	yes, err := l.Kit.Gum.Confirm(ctx, flow.Here(team, name, "Sign in").About(name+" in team "+team+" · just added"), "Sign "+name+" in to Element now? A password is set and shown here once.", false)
	if errors.Is(err, ui.ErrCancelled) || (err == nil && !yes) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	set, err := l.IssuePassword(ctx, snap, team, name)
	if errors.Is(offerResult(err), ui.ErrCancelled) {
		return set, nil // left the copy offer; the password was shown
	}
	return set, err
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
// it has none. The cursor starts on Exit (decision A12).
func (l *Login) offerElement(ctx context.Context, team string) error {
	yes, err := l.Kit.Confirm(ctx, flow.Here(team, "Open in Element").About("team "+team),
		"Open "+team+" in Element now? Element opens in a window of its own; ccy-teams stays here.", "open "+team+" in Element", false)
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
	yes, err := l.Kit.AskYes(ctx, flow.Here(team, "Element profile").About("team "+team+" · no Element profile on this desktop"), "Set one up now? It installs Element Desktop if needed (sudo asks for your password).", true)
	if err != nil {
		return flow.Refused, err
	}
	if !yes {
		return l.Kit.Refuse(ctx, "Nothing set up. Opening the team in Element offers it again.")
	}
	// The profile is declared and its play run in one hold of the play lock: a wait for it that
	// does not end in the lock declares nothing.
	err = l.Ops.Locked(ctx, "set up Element for "+team, func(ctx context.Context) error {
		if _, declErr := l.Declare.Element(ctx, team); declErr != nil {
			l.Kit.ReportErr(declErr)
			return declErr
		}
		return l.Ops.RunElementPlay(ctx, []string{team})
	})
	if errors.Is(err, playlock.ErrNotTaken) {
		return l.Kit.Refuse(ctx, "Nothing set up: the play lock was not free (above). Opening the team in Element offers it again.")
	}
	if err != nil {
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
	// The homeserver keeps no record of a lockout a tool could read (Plan 00203 gap 10).
	l.Kit.Tell("A few sign-ins in a row, or a few wrong passwords, and Element says \"Too many login attempts " +
		"for this account\": wait, it clears by itself within minutes and retrying does not lengthen it " +
		"(agent-bus health status gives the exact limits).")
}

// offerCopy, in a Wayland session, offers to put the password on the clipboard. It goes to
// wl-copy on stdin, never in its arguments, which any process can read. GNOME reads a new
// selection at once and keeps serving its own copy, so nothing erases itself: the password
// stays on the clipboard until SettleClipboard, which the flow calls once Element is open to
// paste into, clears it if the clipboard still holds it. Outside Wayland nothing is offered;
// the password is on screen. Nor is it without wl-paste (the clipboard could not be read back
// to clear it), or while the Clipboard Indicator extension is on (or that cannot be read): it
// keeps every new selection in its history, past any clear; it honours no password-manager hint.
func (l *Login) offerCopy(ctx context.Context, h ui.Heading, name, password string) error {
	if l.Getenv("WAYLAND_DISPLAY") == "" {
		return nil
	}
	if !l.Kit.HasTool("wl-copy") {
		l.Kit.Term.PrintError("wl-copy is not installed, so the password cannot be put on the clipboard; copy it from above. " +
			"playbooks/imports/play-claude-yolo.yml installs wl-clipboard.")
		return nil
	}
	if !l.Kit.HasTool("wl-paste") {
		l.Kit.Term.PrintError("wl-paste is not installed, so a copied password could not be cleared from the clipboard; copy it from above. " +
			"playbooks/imports/play-claude-yolo.yml installs wl-clipboard.")
		return nil
	}
	kept, keptErr := l.clipboardHistory(ctx)
	switch kept {
	case historyOff:
	case historyUnknown:
		l.Kit.Term.PrintError("whether the Clipboard Indicator extension is on could not be read (" + keptErr.Error() +
			"), so the password is not offered for the clipboard; type it from above.")
		return nil
	case historyOn:
		l.Kit.Tell("The password is not offered for the clipboard: the Clipboard Indicator extension is on, " +
			"and it keeps everything copied in its history, past any clear. Type it from above.")
		return nil
	}
	yes, err := l.Kit.Confirm(ctx, h.Push("Copy"),
		"Copy the password? Put "+name+"'s new password on the clipboard, to paste into Element. "+
			"Once you have pasted it, press Enter here: ccy-teams then clears the clipboard if it still holds the password "+
			"(after "+pasteWaitWords+" it does so without the Enter).",
		"copy the password", true)
	if err != nil || !yes {
		return err
	}
	if failed := l.copyToClipboard(ctx, password); failed != "" {
		l.Kit.Term.PrintError("wl-copy failed (" + failed + "), so the password is not on the clipboard; copy it from above.")
		return nil
	}
	l.copied = password
	l.Kit.Tell("The password is on the clipboard.")
	return nil
}

// pasteWait is how long SettleClipboard waits for the Enter that says the password was pasted;
// clipboardBound bounds each read and clear of the clipboard.
const (
	pasteWait      = 45 * time.Second
	pasteWaitWords = "45 seconds"
	clipboardBound = 10 * time.Second
)

// clipboardHolds is whether the clipboard holds exactly password. The content is read from
// wl-paste's output, never shown or logged, and compared in constant time.
func (l *Login) clipboardHolds(ctx context.Context, password string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, clipboardBound)
	defer cancel()
	res, err := l.Runner.Run(ctx, execx.Cmd{Name: "wl-paste", Args: []string{"--no-newline"}})
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare(res.Stdout, []byte(password)) == 1, nil
}

// clearClipboard empties the clipboard (not the primary selection).
func (l *Login) clearClipboard(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, clipboardBound)
	defer cancel()
	_, err := l.Runner.Run(ctx, execx.Cmd{Name: "wl-copy", Args: []string{"--clear"}, Stderr: l.Kit.Stderr})
	return err
}

// clipboardHistoryUUID is the Clipboard Indicator extension vars/gnome-shell-extensions.yml
// installs and enables.
const clipboardHistoryUUID = "clipboard-indicator@tudmotu.com"

// history is whether a clipboard history keeps what is copied.
type history int

const (
	historyOff     history = iota
	historyOn              // Clipboard Indicator is enabled
	historyUnknown         // the enabled extensions could not be read (gsettings said why)
)

// clipboardHistory is whether GNOME Shell's enabled extensions include Clipboard Indicator,
// and, when that could not be read, why. With no gsettings there is no GNOME Shell, so it is off.
func (l *Login) clipboardHistory(ctx context.Context) (history, error) {
	if !l.Kit.HasTool("gsettings") {
		return historyOff, nil
	}
	res, err := l.Runner.Run(ctx, execx.Cmd{
		Name: "gsettings", Args: []string{"get", "org.gnome.shell", "enabled-extensions"}, Stderr: l.Kit.Stderr,
	})
	switch {
	case err != nil:
		return historyUnknown, err
	case strings.Contains(string(res.Stdout), "'"+clipboardHistoryUUID+"'"):
		return historyOn, nil
	}
	return historyOff, nil
}

// copyToClipboard puts text on the clipboard; it is how wl-copy failed, or "" when it took it.
func (l *Login) copyToClipboard(ctx context.Context, text string) string {
	_, err := l.Runner.Run(ctx, execx.Cmd{Name: "wl-copy", Stdin: strings.NewReader(text), Stderr: l.Kit.Stderr})
	if err != nil {
		return err.Error()
	}
	return ""
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
