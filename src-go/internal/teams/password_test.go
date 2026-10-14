package teams

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

func (h *pickerHarness) passwordable() {
	h.on("sudo -n -v", "", 0)
	h.on(sudoBus+"human password alpha owner", "a-new-password\nignored\n", 0)
}

func TestPasswordRowSetsAPasswordAndShowsTheLoginOnce(t *testing.T) {
	h := newPickerHarness(t)
	h.passwordable()
	h.script.fzf = []string{"|YES"}
	r := h.pass(t)
	res, err := r.passwordRow(context.Background(), r.row(t, "owner"))
	if err != nil || res != done {
		t.Fatalf("%v %v", res, err)
	}
	want := "\nElement sign-in for owner, team alpha\n" +
		"  Homeserver  http://192.0.2.10:6167\n" +
		"  Username    owner\n" +
		"  Matrix ID   @owner:alpha.agent-bus.internal\n" +
		"  Password    a-new-password\n" +
		"Shown this once and stored nowhere. The team's Element profile has the homeserver set already.\n"
	if !strings.Contains(h.errOut.String(), want) {
		t.Fatalf("stderr %q", h.errOut.String())
	}
	if h.out.String() != "" {
		t.Fatalf("stdout %q: the password is not the payload", h.out.String())
	}
	if !strings.Contains(h.headerOf(0), "Sets a new password for owner on alpha's homeserver and shows it here, once: it is stored nowhere. owner's other Element sessions are signed out.") {
		t.Fatalf("header %q", h.headerOf(0))
	}
	if !slices.Contains(h.script.fzfArgs[0], "--bind=start:last") {
		t.Fatal("the reset question defaults to Exit")
	}
	if !strings.HasSuffix(h.errOut.String(), "Press Enter to go back to the list.\n") {
		t.Fatalf("stderr %q", h.errOut.String())
	}
}

func TestANewPasswordMakesTheListReadWhoIsSignedInAgain(t *testing.T) {
	h := newPickerHarness(t)
	h.passwordable()
	h.app.devices["alpha|owner"] = "2 signed in"
	h.app.devices["beta|owner"] = "1 signed in"
	r := h.pass(t)
	if set, err := r.issuePassword(context.Background(), "alpha", "owner"); err != nil || !set {
		t.Fatalf("%v %v", set, err)
	}
	if _, kept := h.app.devices["alpha|owner"]; kept || h.app.devices["beta|owner"] != "1 signed in" {
		t.Fatalf("devices %v: only the team whose password changed is read again", h.app.devices)
	}
}

func TestIssuePasswordAsksNothingAndShowsTheLogin(t *testing.T) {
	h := newPickerHarness(t)
	h.passwordable()
	r := h.pass(t)
	set, err := r.issuePassword(context.Background(), "alpha", "owner")
	if err != nil || !set {
		t.Fatalf("%v %v", set, err)
	}
	if !strings.Contains(h.errOut.String(), "Password    a-new-password\n") || len(h.script.fzfArgs) != 0 {
		t.Fatalf("stderr %q, fzf asked %d times", h.errOut.String(), len(h.script.fzfArgs))
	}
}

func TestPasswordLoginOfATeamWithNoAddressSaysSo(t *testing.T) {
	h := newPickerHarness(t)
	r := h.pass(t)
	delete(r.snap.URL, "alpha")
	delete(r.snap.Server, "alpha")
	r.showLogin("alpha", "owner", "pw")
	for _, want := range []string{"Homeserver  unknown (not declared)\n", "@owner:alpha.agent-bus.internal\n"} {
		if !strings.Contains(h.errOut.String(), want) {
			t.Fatalf("stderr %q lacks %q", h.errOut.String(), want)
		}
	}
}

func TestPasswordIsBoldOnAColourTerminal(t *testing.T) {
	h := newPickerHarness(t)
	delete(h.env, "NO_COLOR")
	r := h.pass(t)
	r.showLogin("alpha", "owner", "pw")
	if !strings.Contains(h.errOut.String(), "Password    "+cBold+"pw"+cReset+"\n") {
		t.Fatalf("stderr %q", h.errOut.String())
	}
}

func TestPasswordOffersTheClipboardOnWaylandAndPutsItOnStdin(t *testing.T) {
	h := newPickerHarness(t)
	h.passwordable()
	h.env["WAYLAND_DISPLAY"] = "wayland-test"
	h.on("wl-copy", "", 0)
	h.script.fzf = []string{"ctrl-p|YES", "|YES"}
	r := h.pass(t)
	if _, err := r.passwordRow(context.Background(), r.row(t, "owner")); err != nil {
		t.Fatal(err)
	}
	var copyCall execx.Cmd
	for _, c := range h.fake.Calls() {
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
	if !strings.Contains(h.errOut.String(), "The password is on the clipboard.\n") {
		t.Fatalf("stderr %q", h.errOut.String())
	}
	if slices.Contains(h.script.fzfArgs[1], "--bind=start:last") {
		t.Fatal("the copy question defaults to Yes")
	}
	if !slices.Contains(h.script.fzfArgs[1], "--border-label= Copy the password? ") {
		t.Fatalf("args %q", h.script.fzfArgs[1])
	}
	if !slices.Contains(h.script.rows[1], "Yes, copy the password") {
		t.Fatalf("rows %q", h.script.rows[1])
	}
}

func TestPasswordClipboardFailureIsNotCalledSuccess(t *testing.T) {
	h := newPickerHarness(t)
	h.passwordable()
	h.env["WAYLAND_DISPLAY"] = "wayland-test"
	h.onErr("wl-copy", "no display", 1)
	h.script.fzf = []string{"|YES", "|YES"}
	r := h.pass(t)
	if _, err := r.passwordRow(context.Background(), r.row(t, "owner")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.errOut.String(), "ERROR: wl-copy failed (above), so the password is not on the clipboard; copy it from above.\n") ||
		strings.Contains(h.errOut.String(), "The password is on the clipboard.") {
		t.Fatalf("stderr %q", h.errOut.String())
	}
}

func TestPasswordWithoutWlCopySaysWhatInstallsIt(t *testing.T) {
	h := newPickerHarness(t)
	h.passwordable()
	h.env["WAYLAND_DISPLAY"] = "wayland-test"
	h.app.LookPath = func(name string) (string, error) {
		if name == "wl-copy" {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + name, nil
	}
	h.script.fzf = []string{"|YES"}
	r := h.pass(t)
	if _, err := r.passwordRow(context.Background(), r.row(t, "owner")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.errOut.String(), "ERROR: wl-copy is not installed, so the password cannot be put on the clipboard; copy it from above. playbooks/imports/play-claude-yolo.yml installs wl-clipboard.\n") {
		t.Fatalf("stderr %q", h.errOut.String())
	}
}

func TestPasswordDeclinedOrRefusedSetsNone(t *testing.T) {
	h := newPickerHarness(t)
	h.passwordable()
	h.script.fzf = []string{"|EXIT"}
	r := h.pass(t)
	if _, err := r.passwordRow(context.Background(), r.row(t, "owner")); err != nil {
		t.Fatal(err)
	}
	if h.fake.Called(sudoBus+"human password") || !strings.Contains(h.errOut.String(), "No password was set for owner.\n") {
		t.Fatalf("stderr %q", h.errOut.String())
	}

	h = newPickerHarness(t)
	h.onErr("sudo -n -v", "sudo: a password is required", 1)
	h.onErr("sudo -v", "sudo: 3 incorrect password attempts", 1)
	h.script.fzf = []string{"|YES"}
	r = h.pass(t)
	if _, err := r.passwordRow(context.Background(), r.row(t, "owner")); err != nil {
		t.Fatal(err)
	}
	if h.fake.Called(sudoBus+"human password") || !strings.Contains(h.errOut.String(), "No password was set for owner.\n") {
		t.Fatalf("stderr %q", h.errOut.String())
	}

	h = newPickerHarness(t)
	h.on("sudo -n -v", "", 0)
	h.onErr(sudoBus+"human password alpha owner", "no such person", 1)
	h.script.fzf = []string{"|YES"}
	r = h.pass(t)
	if _, err := r.passwordRow(context.Background(), r.row(t, "owner")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.errOut.String(), "No password was set for owner, for the reason above.\n") {
		t.Fatalf("stderr %q", h.errOut.String())
	}
}

func TestPasswordOnARowThatIsNotAPersonSaysWhere(t *testing.T) {
	h := newPickerHarness(t)
	r := h.pass(t)
	res, err := r.passwordRow(context.Background(), r.row(t, "1"))
	if err != nil || res != done {
		t.Fatalf("%v %v", res, err)
	}
	if !strings.Contains(h.errOut.String(), "Ctrl-P sets a person's Element password: choose one of the team's people (KIND human).\nPress Enter to go back to the list.\n") {
		t.Fatalf("stderr %q", h.errOut.String())
	}
}
