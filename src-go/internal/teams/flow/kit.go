// Package flow is what every interactive ccy-teams flow shares: the questions (gum), the choices
// (fzf), the lines said to the person on stderr, the wait that holds the screen until the list
// has been read, and the error that ends the picker with an exit code. The picker and the flows
// it hands off to (seats, Element, passwords) all speak through a Kit.
package flow

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sudo"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// Result says how a key's action ended. Actions that start or attach end the picker through a
// LeaveError instead.
type Result int

const (
	// Done is an action that did what it was asked, or that the person called off.
	Done Result = iota
	// Refused is an action that could not be done: it counts toward the picker's limit of
	// refusals in a row.
	Refused
)

// LeaveError is a flow ending the picker with an exit code: not a failure, though 0 and a
// failure both end it.
type LeaveError struct {
	Code term.ExitCode
}

func (e *LeaveError) Error() string { return fmt.Sprintf("leave the picker with exit %d", e.Code) }

// Leave is the error that ends the picker with code.
func Leave(code term.ExitCode) error { return &LeaveError{Code: code} }

// ExitCodeOf is the exit code err asks for, if it asks for one.
func ExitCodeOf(err error) (term.ExitCode, bool) {
	req, ok := errors.AsType[*LeaveError](err)
	if !ok {
		return term.ExitOK, false
	}
	return req.Code, true
}

// TeamsCrumb is the first crumb of every ccy-teams screen.
const TeamsCrumb = "Teams"

// Here is the heading of a ccy-teams screen at Teams › crumbs.
func Here(crumbs ...string) ui.Heading {
	return ui.Here(append([]string{TeamsCrumb}, crumbs...)...)
}

// LaunchCrumb is the first crumb of the questions `ccy --teams` asks through ccy-teams.
const LaunchCrumb = "New session"

// LaunchTeam is the heading of the questions about team, the i-th of the n teams a launch joins:
// "New session › Team 2 of 3: alpha", or "New session › Team alpha" when it joins one.
func LaunchTeam(i, n int, team string) ui.Heading {
	if n > 1 {
		return ui.Here(LaunchCrumb, fmt.Sprintf("Team %d of %d: %s", i, n, team))
	}
	return ui.Here(LaunchCrumb, "Team "+team)
}

// Kit is the person-facing side of a run. Every field is a seam a test replaces.
type Kit struct {
	Term   *term.Term
	Out    *term.Printer
	Stderr io.Writer
	// Gum asks the questions, Picker is the fzf list every choice is made in.
	Gum    *ui.Gum
	Picker *ui.Picker
	Sudo   *sudo.Sudo
	// LookPath finds a tool on PATH. OpenTTY opens the terminal, which is read after fzf has
	// had the screen.
	LookPath func(string) (string, error)
	OpenTTY  func() (io.ReadCloser, error)
	Home     string
}

// New is the Kit of a host.
func New(h *host.Host) *Kit {
	return &Kit{
		Term: h.Term, Out: h.Out, Stderr: h.Stderr, Gum: h.Gum, Picker: h.Picker, Sudo: h.Sudo,
		LookPath: h.LookPath, OpenTTY: h.OpenTTY, Home: h.Home,
	}
}

// Tell says one line to the person, on stderr.
func (k *Kit) Tell(msg string) { k.Out.Print(k.Stderr, msg+"\n") }

// ReportErr prints why something failed, as the tool that failed said it.
func (k *Kit) ReportErr(err error) { k.Out.Print(k.Stderr, err.Error()+"\n") }

// Abbrev is path with the home directory shown as ~.
func (k *Kit) Abbrev(path string) string { return term.AbbrevHome(path, k.Home) }

// Cancelled says nothing was changed and leaves with the cancelled exit.
func (k *Kit) Cancelled() error {
	k.Tell("Cancelled; nothing was changed.")
	return Leave(term.ExitCancelled)
}

// Interrupted is the person's Ctrl-C, as a request to leave with 130.
func (k *Kit) Interrupted(ctx context.Context) error {
	if ctx.Err() == nil {
		return nil
	}
	return k.Cancelled()
}

// HasTool is whether name is on PATH.
func (k *Kit) HasTool(name string) bool {
	_, err := k.LookPath(name)
	return err == nil
}

// NeedGum refuses without gum, naming the play that installs it.
func (k *Kit) NeedGum() error {
	if !k.HasTool("gum") {
		k.Term.PrintError("gum is not installed, and ccy-teams needs it here. " +
			"playbooks/imports/play-claude-yolo.yml installs it; run that play and try again.")
		return Leave(term.ExitFailure)
	}
	return nil
}

// NeedFzf refuses without fzf, naming the play that installs it.
func (k *Kit) NeedFzf() error {
	if !k.HasTool("fzf") {
		k.Term.PrintError("fzf is not installed; playbooks/imports/play-claude-yolo.yml installs it.")
		return Leave(term.ExitFailure)
	}
	return nil
}

// AskYes is a yes/no question in gum, under the heading h. Ctrl-C ends the picker.
func (k *Kit) AskYes(ctx context.Context, h ui.Heading, question string, defaultYes bool) (bool, error) {
	yes, err := k.Gum.Confirm(ctx, h, question, defaultYes)
	if errors.Is(err, ui.ErrCancelled) {
		return false, k.Cancelled()
	}
	if err != nil {
		k.ReportErr(err)
		return false, Leave(term.ExitFailure)
	}
	return yes, nil
}

// Choose is one pick from the picker, under the heading h. Leaving it (Esc, q, the Exit row)
// is ui.ErrLeave.
func (k *Kit) Choose(ctx context.Context, h ui.Heading, spec ui.PickSpec) (ui.Picked, error) {
	if err := k.NeedFzf(); err != nil {
		return ui.Picked{}, err
	}
	picked, err := k.Picker.Pick(ctx, h, spec)
	// Leaving the list, leaving a row's menu and a key the row does not offer are the person's
	// choices, for the caller to answer; anything else is a failure.
	var notOffered *ui.NotOfferedError
	if err != nil && !errors.Is(err, ui.ErrLeave) && !errors.Is(err, ui.ErrBack) && !errors.As(err, &notOffered) {
		k.ReportErr(err)
		return ui.Picked{}, Leave(term.ExitFailure)
	}
	return picked, err
}

// Confirm is a yes/no in the picker, under the heading h: no, Esc and q are all a no.
func (k *Kit) Confirm(ctx context.Context, h ui.Heading, question, action string, defaultYes bool) (bool, error) {
	if err := k.NeedFzf(); err != nil {
		return false, err
	}
	yes, err := k.Picker.Confirm(ctx, h, question, action, defaultYes)
	if err != nil {
		k.ReportErr(err)
		return false, Leave(term.ExitFailure)
	}
	return yes, nil
}

// InPicker turns the exit code of a flow run from the picker into what the picker does next:
// success and failure both go back to the list, with the failure named; a cancel ends it.
func (k *Kit) InPicker(label string, rc term.ExitCode) error {
	switch rc {
	case term.ExitOK:
		return nil
	case term.ExitCancelled:
		return k.Cancelled()
	case term.ExitFailure, term.ExitUsage:
		// Named below, with the list to go back to.
	}
	k.Tell(fmt.Sprintf("(%s failed, exit %d: see above.)", label, rc))
	return nil
}

// Refuse says why a key could do nothing, and waits for the list to be read before it is drawn
// over the message. It counts toward the limit of refusals in a row.
func (k *Kit) Refuse(ctx context.Context, msg string) (Result, error) {
	k.Tell(msg)
	k.BackToList(ctx)
	return Refused, nil
}

// Note says something the person should read before the list is drawn again; it is not a
// refusal.
func (k *Kit) Note(ctx context.Context, msg string) (Result, error) {
	k.Tell(msg)
	k.BackToList(ctx)
	return Done, nil
}

// SudoReady is whether sudo is ready for a root command; when it is not, it has said why.
func (k *Kit) SudoReady(ctx context.Context, why string) bool {
	return k.Sudo.Ready(ctx, why) == nil
}

// ReportRootErr says why a root command failed when its own words have not already been shown:
// sudo wanting a password again prints nothing.
func (k *Kit) ReportRootErr(err error) {
	switch {
	case errors.Is(err, sudo.ErrNeedsPassword):
		k.Term.PrintError("sudo asked for a password again, so nothing was changed. Run sudo -v first.")
	case !agentbus.IsCommandError(err):
		k.ReportErr(err)
	}
}

// BackToList holds what was just printed on the screen until the person has read it, because
// the list is drawn over it.
func (k *Kit) BackToList(ctx context.Context) {
	k.Tell("Press Enter to go back to the list.")
	k.WaitForEnter(ctx)
}

// WaitForEnter holds the screen until a line is typed.
func (k *Kit) WaitForEnter(ctx context.Context) {
	tty, err := k.OpenTTY()
	if err != nil {
		k.Out.Print(k.Stderr, "\n")
		return
	}
	got, err := readLine(ctx, tty)
	if err != nil {
		k.Term.PrintError("the terminal could not be closed: " + err.Error())
	}
	if !got {
		k.Out.Print(k.Stderr, "\n")
	}
}

// readLine waits for a line from r, one byte at a time so nothing past it is taken, and closes
// r. It gives up at the end of the input, an error, or when ctx is done. Closing r is what ends
// the reader goroutine of a cancelled wait, so readLine waits for that goroutine before it
// returns and r's Read must return once Close has been called (a terminal file does): nothing
// is left blocked on the terminal to swallow the next wait's Enter. The error is Close's.
func readLine(ctx context.Context, r io.ReadCloser) (bool, error) {
	got := make(chan bool, 1)
	go func() {
		buf := make([]byte, 1)
		for {
			n, err := r.Read(buf)
			if n == 1 && buf[0] == '\n' {
				got <- true
				return
			}
			if err != nil {
				got <- false
				return
			}
		}
	}()
	select {
	case ok := <-got:
		return ok, r.Close()
	case <-ctx.Done():
		err := r.Close()
		<-got
		return false, err
	}
}
