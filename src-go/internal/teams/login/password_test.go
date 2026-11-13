package login

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestPasswordRowSetsAPasswordAndShowsTheLoginOnce(t *testing.T) {
	h := newRig(t)
	h.passwordable()
	h.Script.Fzf = []string{"|EXIT"} // Element is offered afterwards, and declined
	snap := h.pass(t)
	res, err := h.l.PasswordRow(context.Background(), snap, h.row(t, snap, "owner"))
	if err != nil || res != flow.Done {
		t.Fatalf("%v %v", res, err)
	}
	want := term.AltScreenOn + "\nElement sign-in for owner, team alpha\n" +
		"  Homeserver  http://192.0.2.10:6167\n" +
		"  Username    owner\n" +
		"  Matrix ID   @owner:alpha.agent-bus.internal\n" +
		"  Password    a-new-password\n" +
		shownLine + "The team's Element profile has the homeserver set already.\n"
	if !strings.Contains(h.ErrOut.String(), want) {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
	if h.Out.String() != "" {
		t.Fatalf("stdout %q: the password is not the payload", h.Out.String())
	}
	// The Enter that clears the password from the screen is the one that goes back to the list.
	if !strings.HasSuffix(h.ErrOut.String(), clearScreenLine+term.AltScreenOff) || strings.Contains(h.ErrOut.String(), "Press Enter to go back to the list.") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
	if len(h.Script.FzfArgs) != 1 || !strings.Contains(h.HeaderOf(0), "Open alpha in Element now?") {
		t.Fatalf("someone with no sessions is asked nothing but whether to open Element: %q", h.Script.FzfArgs)
	}
	h.Script.HeadingAt(0, "Teams › alpha › Open in Element\nteam alpha")
	if !slices.Contains(h.Script.FzfArgs[0], "--bind=start:last") {
		t.Fatal("opening Element must start on Exit (decision A12): an offer that opens something defaults to No")
	}
}

func TestPasswordForSomeoneSignedInNamesTheSessionsItEndsAndDefaultsToNo(t *testing.T) {
	h := newRig(t)
	h.On("sudo -n -v", "", 0)
	h.On(teamtest.SudoBus+"human password alpha guest", "guest-pw\n", 0)
	h.Script.Fzf = []string{"|EXIT"}
	snap := h.pass(t)
	if _, err := h.l.PasswordRow(context.Background(), snap, h.row(t, snap, "guest")); err != nil {
		t.Fatal(err)
	}
	header := h.HeaderOf(0)
	if !strings.Contains(header, "Sets a new password for guest on alpha's homeserver and shows it here, once: it is stored nowhere.") ||
		!strings.Contains(header, "This signs out guest's 2 Element sessions.") {
		t.Fatalf("header %q", header)
	}
	if !slices.Contains(h.Script.FzfArgs[0], "--bind=start:last") {
		t.Fatal("the reset question defaults to No")
	}
	if h.Fake.Called(teamtest.SudoBus + "human password") {
		t.Fatal("declined, so no password was set")
	}

	h = newRig(t)
	h.On("sudo -n -v", "", 0)
	h.On(teamtest.SudoBus+"human password alpha guest", "guest-pw\n", 0)
	h.Script.Fzf = []string{"|YES", "|EXIT"}
	snap = h.pass(t)
	if _, err := h.l.PasswordRow(context.Background(), snap, h.row(t, snap, "guest")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.ErrOut.String(), "Password    guest-pw\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestPasswordWhenTheCountIsUnknownSaysTheSessionsAreSignedOut(t *testing.T) {
	h := newRig(t)
	h.Script.Fzf = []string{"|EXIT"}
	snap := h.pass(t)
	delete(snap.Sessions, "alpha|owner")
	if _, err := h.l.PasswordRow(context.Background(), snap, h.row(t, snap, "owner")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.HeaderOf(0), "This signs out owner's Element sessions.") {
		t.Fatalf("header %q", h.HeaderOf(0))
	}
}

func TestAfterAPasswordElementIsOfferedAndOpensApart(t *testing.T) {
	h := newRig(t)
	h.withElementProfile(t)
	h.On(teamtest.SudoBus+"human devices alpha owner", "", 0)
	h.passwordable()
	h.Script.Fzf = []string{"|YES"}
	snap := h.pass(t)
	if _, err := h.l.PasswordRow(context.Background(), snap, h.row(t, snap, "owner")); err != nil {
		t.Fatal(err)
	}
	if !h.Fake.Called("systemd-run") || !strings.Contains(h.ErrOut.String(), "Element is starting; its own messages go to ") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestANewPasswordMakesTheListReadWhoIsSignedInAgain(t *testing.T) {
	h := newRig(t)
	h.passwordable()
	snap := h.pass(t)
	if snap.SignedIn["alpha|owner"] != "never signed in" {
		t.Fatalf("signed in %v", snap.SignedIn)
	}
	h.On(teamtest.SudoBus+"human devices alpha owner", "DEVICE\ta\t-\n", 0)
	if again := h.pass(t); again.SignedIn["alpha|owner"] != "never signed in" {
		t.Fatalf("the read was not kept: %v", again.SignedIn)
	}
	if set, err := h.l.IssuePassword(context.Background(), snap, "alpha", "owner"); err != nil || !set {
		t.Fatalf("%v %v", set, err)
	}
	if again := h.pass(t); again.SignedIn["alpha|owner"] != "1 signed in" {
		t.Fatalf("after a new password the devices are read again: %v", again.SignedIn)
	}
}

func TestIssuePasswordAsksNothingAndShowsTheLogin(t *testing.T) {
	h := newRig(t)
	h.passwordable()
	snap := h.pass(t)
	set, err := h.l.IssuePassword(context.Background(), snap, "alpha", "owner")
	if err != nil || !set {
		t.Fatalf("%v %v", set, err)
	}
	if !strings.Contains(h.ErrOut.String(), "Password    a-new-password\n") || len(h.Script.FzfArgs) != 0 {
		t.Fatalf("stderr %q, fzf asked %d times", h.ErrOut.String(), len(h.Script.FzfArgs))
	}
}

// A sign-in block whose homeserver address is not known names what the team lacks and where it
// is set, and never claims the Element profile has the homeserver.
func TestPasswordLoginOfATeamWithNoAddressSaysWhatIsMissing(t *testing.T) {
	cases := map[string]struct {
		port, want string
	}{
		"not declared":       {port: "", want: "Homeserver  unknown: alpha is not declared present in the host_vars\n"},
		"no port":            {port: "-", want: "Homeserver  unknown: alpha declares no port in the host_vars\n"},
		"no listen, no addr": {port: "6167", want: "Homeserver  unknown: alpha has no listen address and agent_bus_address is not set in the host_vars\n"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h := newRig(t)
			snap := h.pass(t)
			delete(snap.URL, "alpha")
			delete(snap.Server, "alpha")
			if c.port == "" {
				delete(snap.Port, "alpha")
			} else {
				snap.Port["alpha"] = c.port
			}
			h.l.showLogin(snap, "alpha", "owner", "pw")
			out := h.ErrOut.String()
			for _, want := range []string{
				c.want, "@owner:alpha.agent-bus.internal\n",
				shownLine + "Without the homeserver's address the team's Element profile cannot have it: " +
					"set it in the host_vars, then run ccy-teams apply alpha.\n",
			} {
				if !strings.Contains(out, want) {
					t.Fatalf("stderr %q lacks %q", out, want)
				}
			}
			if strings.Contains(out, "has the homeserver set already") || strings.Contains(out, "not declared)") {
				t.Fatalf("stderr %q", out)
			}
		})
	}
}

// Plan 00203 gap 10: signing in on several devices after a new password can reach the
// per-account limit, which only Element would show and nothing can read; the person is told
// first what it looks like and that it clears by itself.
func TestPasswordLoginSaysWhatTheSignInLimitLooksLike(t *testing.T) {
	h := newRig(t)
	snap := h.pass(t)
	h.l.showLogin(snap, "alpha", "owner", "pw")
	for _, want := range []string{"Too many login attempts for this account", "clears by itself", "agent-bus health status"} {
		if !strings.Contains(h.ErrOut.String(), want) {
			t.Fatalf("stderr %q lacks %q", h.ErrOut.String(), want)
		}
	}
}

func TestPasswordIsBoldOnAColourTerminal(t *testing.T) {
	h := newRig(t)
	delete(h.Env, "NO_COLOR")
	snap := h.pass(t)
	h.l.showLogin(snap, "alpha", "owner", "pw")
	if !strings.Contains(h.ErrOut.String(), "Password    "+term.Bold+"pw"+term.Reset+"\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

// Issue 255: the password is written only on the terminal's alternate screen, which is left once
// the person is done with it, so nothing of it is left in the scrollback (or tmux's history)
// when the list comes back.
func TestThePasswordIsShownOnlyOnTheAlternateScreenAndLeavesNoCopy(t *testing.T) {
	for name, copied := range map[string]bool{"typed from the screen": false, "copied": true} {
		t.Run(name, func(t *testing.T) {
			h := newRig(t)
			h.passwordable()
			fzf := []string{"|EXIT"}
			if copied {
				h.copiable("a-new-password")
				fzf = []string{"|YES", "|EXIT"}
			}
			h.Script.Fzf = fzf
			h.TTY = "\n\n"
			snap := h.pass(t)
			if _, err := h.l.PasswordRow(t.Context(), snap, h.row(t, snap, "owner")); err != nil {
				t.Fatal(err)
			}
			out := h.ErrOut.String()
			shown, after := onlyOnTheAlternateScreen(t, out, "a-new-password")
			if copied && (!strings.Contains(shown, pasteNow) || !strings.Contains(after, clearedLine) || !strings.HasSuffix(after, "Press Enter to go back to the list.\n")) {
				t.Fatalf("stderr %q: the paste's Enter leaves the screen, and the clear is said on the list's screen", out)
			}
			if !copied && !strings.HasSuffix(shown, clearScreenLine) {
				t.Fatalf("stderr %q: the screen is left on an Enter asked for", out)
			}
		})
	}
}

// onlyOnTheAlternateScreen holds that out enters and leaves the alternate screen once and
// writes secret only on it; it is what was written on the screen, and after it.
func onlyOnTheAlternateScreen(t *testing.T, out, secret string) (string, string) {
	t.Helper()
	before, rest, entered := strings.Cut(out, term.AltScreenOn)
	shown, after, left := strings.Cut(rest, term.AltScreenOff)
	if !entered || !left || strings.Count(out, term.AltScreenOn) != 1 || strings.Count(out, term.AltScreenOff) != 1 {
		t.Fatalf("stderr %q: one alternate screen, entered and left", out)
	}
	if !strings.Contains(shown, secret) || strings.Contains(before+after, secret) {
		t.Fatalf("stderr %q: the secret is written only on the alternate screen", out)
	}
	return shown, after
}

// An interrupted run (Ctrl-C) still leaves the alternate screen: the terminal is never left on it.
func TestAnInterruptedPasswordStillLeavesTheAlternateScreen(t *testing.T) {
	h := newRig(t)
	h.passwordable()
	snap := h.pass(t)
	if _, err := h.l.IssuePassword(t.Context(), snap, "alpha", "owner"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	h.l.SettleClipboard(ctx)
	if !strings.HasSuffix(h.ErrOut.String(), term.AltScreenOff) {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

// Where stderr is not a terminal there is no screen to leave: a password written there is a
// copy (a log, a file), so none is set.
func TestNoPasswordIsSetWhereItCannotBeShownWithoutACopy(t *testing.T) {
	h := newRig(t)
	h.passwordable()
	h.l.Kit.Secrets.Terminal = false
	snap := h.pass(t)
	set, err := h.l.IssuePassword(t.Context(), snap, "alpha", "owner")
	if err != nil || set {
		t.Fatalf("%v %v", set, err)
	}
	if h.Fake.Called(teamtest.SudoBus+"human password") || strings.Contains(h.ErrOut.String(), "a-new-password") {
		t.Fatalf("calls %v stderr %q", h.Fake.Calls(), h.ErrOut.String())
	}
	if !strings.Contains(h.ErrOut.String(), "stderr is not a terminal") || !strings.Contains(h.ErrOut.String(), "No password was set for owner.") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

// The texts of the copy: what the question promises, what is said once the password is on the
// clipboard, and what each end of the wait says.
const (
	shownLine       = "Shown on a screen of its own, which is cleared when you are done with it: it is kept nowhere. "
	clearScreenLine = "Once you have signed in to Element, press Enter here to clear the password from the screen.\n"

	copyQuestion = "Copy the password? Put owner's new password on the clipboard, to paste into Element. " +
		"Once you have pasted it, press Enter here: ccy-teams then clears the clipboard if it still holds the password " +
		"(after 45 seconds it does so without the Enter)."
	copiedLine  = "The password is on the clipboard.\n"
	pasteNow    = "Paste the password into Element, then press Enter here (within 45 seconds) to clear it from the clipboard and the screen.\n"
	clearedLine = "The password was cleared from the clipboard. GNOME or a clipboard history may still keep a copy of it: " +
		"copy something else if in doubt.\n"
	leftLine = "The clipboard no longer holds the password (something else was copied), so it was left as it is.\n"
)

func TestPasswordOffersTheClipboardOnWaylandAndPutsItOnStdin(t *testing.T) {
	h := newRig(t)
	h.passwordable()
	h.copiable("a-new-password")
	h.Script.Fzf = []string{"|YES", "|EXIT"}
	snap := h.pass(t)
	if _, err := h.l.PasswordRow(context.Background(), snap, h.row(t, snap, "owner")); err != nil {
		t.Fatal(err)
	}
	var copyCall execx.Cmd
	for _, c := range h.Fake.Calls() {
		if c.Name == "wl-copy" && len(c.Args) == 0 {
			copyCall = c
		}
	}
	if copyCall.Name == "" {
		t.Fatalf("no plain wl-copy in %v: the password is never an argument, and no flag makes it erase itself", h.Fake.Calls())
	}
	in, err := io.ReadAll(copyCall.Stdin)
	if err != nil || string(in) != "a-new-password" {
		t.Fatalf("stdin %q %v", in, err)
	}
	if h.Fake.Called("systemd-run") {
		t.Fatalf("a timer was started: %v", h.Fake.Calls())
	}
	for _, want := range []string{copiedLine, pasteNow, clearedLine} {
		if !strings.Contains(h.ErrOut.String(), want) {
			t.Fatalf("stderr %q lacks %q", h.ErrOut.String(), want)
		}
	}
	if slices.Contains(h.Script.FzfArgs[0], "--bind=start:last") {
		t.Fatal("the copy question defaults to Yes")
	}
	if !strings.Contains(h.HeaderOf(0), copyQuestion) {
		t.Fatalf("header %q", h.HeaderOf(0))
	}
	h.Script.HeadingAt(0, "Teams › alpha › owner › Element password › Copy\nowner in team alpha · 0 Element sessions")
	if !slices.Contains(h.Script.Rows[0], "Yes, copy the password") {
		t.Fatalf("rows %q", h.Script.Rows[0])
	}
}

// The clipboard is read back to compare, with the password nowhere in any argv, and cleared
// only after that read; the primary selection is never touched.
func TestPasswordOnTheClipboardIsReadBackWithoutTheSecretAndThenCleared(t *testing.T) {
	h := newRig(t)
	h.passwordable()
	h.copiable("a-new-password")
	h.Script.Fzf = []string{"|YES", "|EXIT"}
	snap := h.pass(t)
	if _, err := h.l.PasswordRow(context.Background(), snap, h.row(t, snap, "owner")); err != nil {
		t.Fatal(err)
	}
	for _, c := range h.Fake.Calls() {
		if slices.Contains(c.Args, "a-new-password") || slices.Contains(c.Args, "--primary") || slices.Contains(c.Args, "-p") {
			t.Fatalf("call %q: the password is never an argument, and the primary selection is left alone", c.Line())
		}
	}
	read, cleared := h.callIndex("wl-paste --no-newline"), h.callIndex("wl-copy --clear")
	if read < 0 || cleared < read || h.callIndex("wl-copy") > read {
		t.Fatalf("calls %v: copy, read back, then clear", h.Fake.Calls())
	}
	if h.Out.String() != "" {
		t.Fatalf("stdout %q", h.Out.String())
	}
	if strings.Count(h.ErrOut.String(), "a-new-password") != 1 {
		t.Fatalf("stderr %q: the password is shown once, never again from the read back", h.ErrOut.String())
	}
}

// Something the person copied after the password is theirs: it is never wiped.
func TestPasswordOnTheClipboardIsLeftAloneOnceSomethingElseIsCopied(t *testing.T) {
	h := newRig(t)
	h.passwordable()
	h.copiable("a link the person copied")
	h.Script.Fzf = []string{"|YES", "|EXIT"}
	snap := h.pass(t)
	if _, err := h.l.PasswordRow(context.Background(), snap, h.row(t, snap, "owner")); err != nil {
		t.Fatal(err)
	}
	if h.Fake.Called("wl-copy --clear") {
		t.Fatalf("the clipboard was cleared: %v", h.Fake.Calls())
	}
	if !strings.Contains(h.ErrOut.String(), leftLine) || strings.Contains(h.ErrOut.String(), clearedLine) {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

// heldTTY is a terminal nobody types on: a read waits until the terminal is closed.
type heldTTY struct{ closed chan struct{} }

func (h heldTTY) Read([]byte) (int, error) {
	<-h.closed
	return 0, io.EOF
}

func (h heldTTY) Close() error {
	close(h.closed)
	return nil
}

// No Enter within the wait: the clipboard is cleared all the same, and the person is told why.
func TestPasswordOnTheClipboardIsClearedWhenNoEnterComesInTime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newRig(t)
		h.passwordable()
		h.copiable("a-new-password")
		h.Script.Fzf = []string{"|YES"}
		snap := h.pass(t)
		if _, err := h.l.IssuePassword(context.Background(), snap, "alpha", "owner"); err != nil {
			t.Fatal(err)
		}
		h.l.Kit.OpenTTY = func() (io.ReadCloser, error) { return heldTTY{closed: make(chan struct{})}, nil }
		// In the bubble the wait can only end at its deadline: nobody types and nothing cancels.
		h.l.SettleClipboard(context.Background())
		if !h.Fake.Called("wl-copy --clear") {
			t.Fatalf("calls %v", h.Fake.Calls())
		}
		for _, want := range []string{"No Enter within 45 seconds.\n", clearedLine} {
			if !strings.Contains(h.ErrOut.String(), want) {
				t.Fatalf("stderr %q lacks %q", h.ErrOut.String(), want)
			}
		}
	})
}

// A run ended by Ctrl-C while it waits still clears the password it put on the clipboard.
func TestPasswordOnTheClipboardIsClearedWhenTheRunIsInterrupted(t *testing.T) {
	h := newRig(t)
	h.passwordable()
	h.copiable("a-new-password")
	h.Script.Fzf = []string{"|YES"}
	snap := h.pass(t)
	if _, err := h.l.IssuePassword(context.Background(), snap, "alpha", "owner"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h.l.SettleClipboard(ctx)
	if !h.Fake.Called("wl-copy --clear") || !strings.Contains(h.ErrOut.String(), clearedLine) {
		t.Fatalf("calls %v stderr %q", h.Fake.Calls(), h.ErrOut.String())
	}
}

func TestPasswordOnTheClipboardWhoseClearFailsSaysItMayStillBeThere(t *testing.T) {
	h := newRig(t)
	h.passwordable()
	h.copiable("a-new-password")
	h.OnErr("wl-copy --clear", "compositor gone", 1)
	h.Script.Fzf = []string{"|YES", "|EXIT"}
	snap := h.pass(t)
	if _, err := h.l.PasswordRow(context.Background(), snap, h.row(t, snap, "owner")); err != nil {
		t.Fatal(err)
	}
	want := "ERROR: wl-copy --clear failed (wl-copy exited 1: compositor gone): the password may still be on the clipboard; " +
		"copy something else to replace it.\n"
	if !strings.Contains(h.ErrOut.String(), want) || strings.Contains(h.ErrOut.String(), clearedLine) {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestPasswordOnTheClipboardThatCannotBeReadIsNotClearedAndSaysSo(t *testing.T) {
	h := newRig(t)
	h.passwordable()
	h.copiable("a-new-password")
	h.OnErr("wl-paste", "Failed to connect to a Wayland server", 1)
	h.Script.Fzf = []string{"|YES", "|EXIT"}
	snap := h.pass(t)
	if _, err := h.l.PasswordRow(context.Background(), snap, h.row(t, snap, "owner")); err != nil {
		t.Fatal(err)
	}
	if h.Fake.Called("wl-copy --clear") {
		t.Fatalf("cleared without knowing what it held: %v", h.Fake.Calls())
	}
	want := "ERROR: the clipboard could not be read back (wl-paste exited 1: Failed to connect to a Wayland server), so it was not cleared: " +
		"the password may still be on the clipboard; copy something else to replace it.\n"
	if !strings.Contains(h.ErrOut.String(), want) {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

// Nothing to settle when no password went to the clipboard.
func TestSettleClipboardWithNothingCopiedDoesNothing(t *testing.T) {
	h := newRig(t)
	h.l.SettleClipboard(context.Background())
	if len(h.Fake.Calls()) != 0 || h.ErrOut.String() != "" {
		t.Fatalf("calls %v stderr %q", h.Fake.Calls(), h.ErrOut.String())
	}
}

func TestPasswordOutsideWaylandOffersNoClipboard(t *testing.T) {
	h := newRig(t)
	h.passwordable()
	h.Script.Fzf = []string{"|EXIT"}
	snap := h.pass(t)
	if _, err := h.l.PasswordRow(context.Background(), snap, h.row(t, snap, "owner")); err != nil {
		t.Fatal(err)
	}
	for _, c := range h.Fake.Calls() {
		if c.Name == "wl-copy" || c.Name == "xclip" || c.Name == "xsel" {
			t.Fatalf("a clipboard tool ran outside Wayland: %+v", c)
		}
	}
	if strings.Contains(h.ErrOut.String(), "on the clipboard") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestPasswordClipboardFailureIsNotCalledSuccess(t *testing.T) {
	h := newRig(t)
	h.passwordable()
	h.Env["WAYLAND_DISPLAY"] = "wayland-test"
	h.withoutClipboardHistory()
	h.OnErr("wl-copy", "no display", 1)
	h.Script.Fzf = []string{"|YES", "|EXIT"}
	snap := h.pass(t)
	if _, err := h.l.PasswordRow(context.Background(), snap, h.row(t, snap, "owner")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.ErrOut.String(), "ERROR: wl-copy failed (wl-copy exited 1: no display), so the password is not on the clipboard; copy it from above.\n") ||
		strings.Contains(h.ErrOut.String(), copiedLine) || h.Fake.Called("wl-paste") {
		t.Fatalf("stderr %q calls %v", h.ErrOut.String(), h.Fake.Calls())
	}
}

// Review S1: the Clipboard Indicator extension reads every new selection and keeps it in its
// history, past any clear; it honours no password-manager hint. With it on, the clipboard is
// not offered, and the person is told why in one line.
func TestPasswordIsNotOfferedForTheClipboardWhileClipboardIndicatorIsOn(t *testing.T) {
	h := newRig(t)
	h.passwordable()
	h.Env["WAYLAND_DISPLAY"] = "wayland-test"
	h.On("gsettings get org.gnome.shell enabled-extensions", "['blur-my-shell@aunetx', 'clipboard-indicator@tudmotu.com']\n", 0)
	h.Script.Fzf = []string{"|EXIT"} // the Element offer only: no copy question
	snap := h.pass(t)
	if _, err := h.l.PasswordRow(context.Background(), snap, h.row(t, snap, "owner")); err != nil {
		t.Fatal(err)
	}
	if h.Fake.Called("wl-copy") {
		t.Fatalf("wl-copy ran: %v", h.Fake.Calls())
	}
	want := "The password is not offered for the clipboard: the Clipboard Indicator extension is on, " +
		"and it keeps everything copied in its history, past any clear. Type it from above.\n"
	if !strings.Contains(h.ErrOut.String(), want) || strings.Contains(h.ErrOut.String(), "is on the clipboard") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
	for _, args := range h.Script.FzfArgs {
		if slices.Contains(args, "--border-label= Copy the password? ") {
			t.Fatal("the copy question was asked")
		}
	}
}

func TestPasswordIsNotOfferedForTheClipboardWhenTheExtensionsCannotBeRead(t *testing.T) {
	h := newRig(t)
	h.passwordable()
	h.Env["WAYLAND_DISPLAY"] = "wayland-test"
	h.OnErr("gsettings get org.gnome.shell enabled-extensions", "No such schema", 1)
	h.Script.Fzf = []string{"|EXIT"}
	snap := h.pass(t)
	if _, err := h.l.PasswordRow(context.Background(), snap, h.row(t, snap, "owner")); err != nil {
		t.Fatal(err)
	}
	if h.Fake.Called("wl-copy") {
		t.Fatalf("wl-copy ran: %v", h.Fake.Calls())
	}
	if !strings.Contains(h.ErrOut.String(), "ERROR: whether the Clipboard Indicator extension is on could not be read (gsettings exited 1: No such schema), so the password is not offered for the clipboard; type it from above.\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestPasswordIsOfferedForTheClipboardWhereGNOMEShellIsNot(t *testing.T) {
	h := newRig(t)
	h.passwordable()
	h.Env["WAYLAND_DISPLAY"] = "wayland-test"
	h.l.Kit.LookPath = func(name string) (string, error) {
		if name == "gsettings" {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + name, nil
	}
	h.On("wl-copy", "", 0)
	h.On("wl-paste", "a-new-password", 0)
	h.Script.Fzf = []string{"|YES", "|EXIT"}
	snap := h.pass(t)
	if _, err := h.l.PasswordRow(context.Background(), snap, h.row(t, snap, "owner")); err != nil {
		t.Fatal(err)
	}
	if !h.Fake.Called("wl-copy --clear") {
		t.Fatalf("calls %v", h.Fake.Calls())
	}
}

func TestPasswordWithoutWlCopySaysWhatInstallsIt(t *testing.T) {
	h := newRig(t)
	h.passwordable()
	h.Env["WAYLAND_DISPLAY"] = "wayland-test"
	h.l.Kit.LookPath = func(name string) (string, error) {
		if name == "wl-copy" {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + name, nil
	}
	h.Script.Fzf = []string{"|EXIT"}
	snap := h.pass(t)
	if _, err := h.l.PasswordRow(context.Background(), snap, h.row(t, snap, "owner")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.ErrOut.String(), "ERROR: wl-copy is not installed, so the password cannot be put on the clipboard; copy it from above. playbooks/imports/play-claude-yolo.yml installs wl-clipboard.\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

// Without wl-paste the clipboard cannot be read back, so the copy could never be cleared: it is
// not offered.
func TestPasswordWithoutWlPasteIsNotOfferedForTheClipboard(t *testing.T) {
	h := newRig(t)
	h.passwordable()
	h.copiable("a-new-password")
	h.l.Kit.LookPath = func(name string) (string, error) {
		if name == "wl-paste" {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + name, nil
	}
	h.Script.Fzf = []string{"|EXIT"}
	snap := h.pass(t)
	if _, err := h.l.PasswordRow(context.Background(), snap, h.row(t, snap, "owner")); err != nil {
		t.Fatal(err)
	}
	if h.Fake.Called("wl-copy") {
		t.Fatalf("calls %v", h.Fake.Calls())
	}
	if !strings.Contains(h.ErrOut.String(), "ERROR: wl-paste is not installed, so a copied password could not be cleared from the clipboard; copy it from above. playbooks/imports/play-claude-yolo.yml installs wl-clipboard.\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestPasswordDeclinedOrRefusedSetsNone(t *testing.T) {
	h := newRig(t)
	h.passwordable()
	h.Script.Fzf = []string{"|EXIT"}
	snap := h.pass(t)
	if _, err := h.l.PasswordRow(context.Background(), snap, h.row(t, snap, "guest")); err != nil {
		t.Fatal(err)
	}
	if h.Fake.Called(teamtest.SudoBus+"human password") || !strings.Contains(h.ErrOut.String(), "No password was set for guest.\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}

	h = newRig(t)
	h.OnErr("sudo -n -v", "sudo: a password is required", 1)
	h.OnErr("sudo -v", "sudo: 3 incorrect password attempts", 1)
	snap = h.pass(t)
	if _, err := h.l.PasswordRow(context.Background(), snap, h.row(t, snap, "owner")); err != nil {
		t.Fatal(err)
	}
	if h.Fake.Called(teamtest.SudoBus+"human password") || !strings.Contains(h.ErrOut.String(), "No password was set for owner.\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}

	h = newRig(t)
	h.On("sudo -n -v", "", 0)
	h.OnErr(teamtest.SudoBus+"human password alpha owner", "no such person", 1)
	snap = h.pass(t)
	if _, err := h.l.PasswordRow(context.Background(), snap, h.row(t, snap, "owner")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.ErrOut.String(), "No password was set for owner, for the reason above.\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestPasswordOnARowThatIsNotAPersonSaysWhere(t *testing.T) {
	h := newRig(t)
	snap := h.pass(t)
	res, err := h.l.PasswordRow(context.Background(), snap, h.row(t, snap, "1"))
	if err != nil || res != flow.Done {
		t.Fatalf("%v %v", res, err)
	}
	if !strings.Contains(h.ErrOut.String(), "A new Element password is for one of the team's people (KIND human): choose one of their rows.\nPress Enter to go back to the list.\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}
