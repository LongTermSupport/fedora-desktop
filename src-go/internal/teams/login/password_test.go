package login

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

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
	want := "\nElement sign-in for owner, team alpha\n" +
		"  Homeserver  http://192.0.2.10:6167\n" +
		"  Username    owner\n" +
		"  Matrix ID   @owner:alpha.agent-bus.internal\n" +
		"  Password    a-new-password\n" +
		"Shown this once and stored nowhere. The team's Element profile has the homeserver set already.\n"
	if !strings.Contains(h.ErrOut.String(), want) {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
	if h.Out.String() != "" {
		t.Fatalf("stdout %q: the password is not the payload", h.Out.String())
	}
	if !strings.HasSuffix(h.ErrOut.String(), "Press Enter to go back to the list.\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
	if len(h.Script.FzfArgs) != 1 || !slices.Contains(h.Script.FzfArgs[0], "--border-label= Open alpha in Element now? ") {
		t.Fatalf("someone with no sessions is asked nothing but whether to open Element: %q", h.Script.FzfArgs)
	}
	if slices.Contains(h.Script.FzfArgs[0], "--bind=start:last") {
		t.Fatal("opening Element defaults to Yes")
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

func TestPasswordLoginOfATeamWithNoAddressSaysSo(t *testing.T) {
	h := newRig(t)
	snap := h.pass(t)
	delete(snap.URL, "alpha")
	delete(snap.Server, "alpha")
	h.l.showLogin(snap, "alpha", "owner", "pw")
	for _, want := range []string{"Homeserver  unknown (not declared)\n", "@owner:alpha.agent-bus.internal\n"} {
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

func TestPasswordOffersTheClipboardOnWaylandAndPutsItOnStdin(t *testing.T) {
	h := newRig(t)
	h.passwordable()
	h.Env["WAYLAND_DISPLAY"] = "wayland-test"
	h.On("wl-copy", "", 0)
	h.On("systemd-run", "", 0)
	h.Script.Fzf = []string{"|YES", "|EXIT"}
	snap := h.pass(t)
	if _, err := h.l.PasswordRow(context.Background(), snap, h.row(t, snap, "owner")); err != nil {
		t.Fatal(err)
	}
	var copyCall execx.Cmd
	for _, c := range h.Fake.Calls() {
		if c.Name == "wl-copy" {
			copyCall = c
		}
	}
	if copyCall.Name == "" || len(copyCall.Args) != 0 {
		t.Fatalf("wl-copy call %+v: the password is never an argument", copyCall)
	}
	in, err := io.ReadAll(copyCall.Stdin)
	if err != nil || string(in) != "a-new-password" {
		t.Fatalf("stdin %q %v", in, err)
	}
	// Security audit L4: the password does not stay on the clipboard; a user timer clears it.
	if !h.Fake.Called("systemd-run --user --quiet --collect --on-active=45 --setenv=WAYLAND_DISPLAY=wayland-test -- wl-copy --clear") {
		t.Fatalf("no clear timer: %v", h.Fake.Calls())
	}
	if !strings.Contains(h.ErrOut.String(), "The password is on the clipboard for 45 seconds, then the clipboard is cleared.\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
	if slices.Contains(h.Script.FzfArgs[0], "--bind=start:last") {
		t.Fatal("the copy question defaults to Yes")
	}
	if !slices.Contains(h.Script.FzfArgs[0], "--border-label= Copy the password? ") {
		t.Fatalf("args %q", h.Script.FzfArgs[0])
	}
	if !slices.Contains(h.Script.Rows[0], "Yes, copy the password") {
		t.Fatalf("rows %q", h.Script.Rows[0])
	}
}

func TestPasswordClipboardThatCannotBeClearedSaysHowLongItStays(t *testing.T) {
	h := newRig(t)
	h.passwordable()
	h.Env["WAYLAND_DISPLAY"] = "wayland-test"
	h.On("wl-copy", "", 0)
	h.OnErr("systemd-run", "Failed to connect to bus", 1)
	h.Script.Fzf = []string{"|YES", "|EXIT"}
	snap := h.pass(t)
	if _, err := h.l.PasswordRow(context.Background(), snap, h.row(t, snap, "owner")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.ErrOut.String(), "ERROR: the clipboard could not be set to clear itself (above), so the password stays on it until you copy something else.\n") ||
		strings.Contains(h.ErrOut.String(), "for 45 seconds") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestPasswordClipboardFailureIsNotCalledSuccess(t *testing.T) {
	h := newRig(t)
	h.passwordable()
	h.Env["WAYLAND_DISPLAY"] = "wayland-test"
	h.OnErr("wl-copy", "no display", 1)
	h.Script.Fzf = []string{"|YES", "|EXIT"}
	snap := h.pass(t)
	if _, err := h.l.PasswordRow(context.Background(), snap, h.row(t, snap, "owner")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.ErrOut.String(), "ERROR: wl-copy failed (above), so the password is not on the clipboard; copy it from above.\n") ||
		strings.Contains(h.ErrOut.String(), "The password is on the clipboard.") {
		t.Fatalf("stderr %q", h.ErrOut.String())
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
