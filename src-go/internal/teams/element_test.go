package teams

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func (h *pickerHarness) elementLauncher() string {
	return filepath.Join(h.app.Home, ".local/share/applications/agent-bus-alpha-element.desktop")
}

func (h *pickerHarness) withElementProfile(t *testing.T) {
	t.Helper()
	mustWrite(t, h.elementLauncher(), "[Desktop Entry]\n")
	h.on("systemd-run", "", 0)
	// the people have each signed in, so no password is offered
	h.on(sudoBus+"human devices alpha owner", "DEVICE\ta\t-\n", 0)
}

func TestElementOpensTheProfileInItsOwnScopeAndEndsThePicker(t *testing.T) {
	h := newPickerHarness(t)
	h.withElementProfile(t)
	r := h.pass(t)
	res, err := r.openElement(context.Background(), "alpha")
	if code, ok := exitCodeOf(err); !ok || code != 0 || res != done {
		t.Fatalf("%v %v: %s", res, err, h.errOut.String())
	}
	var run execx.Cmd
	for _, c := range h.fake.Calls() {
		if c.Name == "systemd-run" {
			run = c
		}
	}
	line := run.Line()
	if !strings.HasPrefix(line, "systemd-run --user --scope --quiet --collect --unit ccy-teams-element-alpha-") ||
		!strings.HasSuffix(line, "setsid -w gio launch "+h.elementLauncher()) {
		t.Fatalf("systemd-run %q", line)
	}
	logPath := filepath.Join(h.app.StateDir, "element-alpha.log")
	for _, want := range []string{"Opening alpha in Element.\n", "Element is starting; its own messages go to " + term.AbbrevHome(logPath, h.app.Home) + ".\n"} {
		if !strings.Contains(h.errOut.String(), want) {
			t.Fatalf("stderr %q lacks %q", h.errOut.String(), want)
		}
	}
	if h.fake.Called(sudoBus + "human password") {
		t.Fatal("a person who has signed in is offered no password")
	}
}

func TestElementFailedLaunchIsExplicitlyAFailureWithTheLogsTail(t *testing.T) {
	h := newPickerHarness(t)
	h.withElementProfile(t)
	logPath := filepath.Join(h.app.StateDir, "element-alpha.log")
	mustWrite(t, logPath, "electron chatter on stderr\n")
	h.onErr("systemd-run", "gio: failed", 1)
	r := h.pass(t)
	_, err := r.openElement(context.Background(), "alpha")
	if code, ok := exitCodeOf(err); !ok || code != term.ExitFailure {
		t.Fatalf("%v", err)
	}
	for _, want := range []string{"ERROR: Element did not start; its messages are in ", "electron chatter on stderr\n"} {
		if !strings.Contains(h.errOut.String(), want) {
			t.Fatalf("stderr %q lacks %q", h.errOut.String(), want)
		}
	}
}

func TestElementLaunchFailureThatIsNotAnExitIsReportedToo(t *testing.T) {
	h := newPickerHarness(t)
	h.withElementProfile(t)
	// the log's directory is a file, so the launch cannot even open its log
	mustWrite(t, h.app.StateDir, "")
	r := h.pass(t)
	_, err := r.openElement(context.Background(), "alpha")
	if code, ok := exitCodeOf(err); !ok || code != term.ExitFailure {
		t.Fatalf("%v", err)
	}
	if !strings.Contains(h.errOut.String(), "ERROR: ") {
		t.Fatalf("stderr %q", h.errOut.String())
	}
}

func TestElementOffersNeverSignedInPeopleANewPassword(t *testing.T) {
	h := newPickerHarness(t)
	h.withElementProfile(t)
	h.on(sudoBus+"human devices alpha owner", "", 0)
	h.passwordable()
	h.script.fzf = []string{"|YES"}
	r := h.pass(t)
	_, err := r.openElement(context.Background(), "alpha")
	if code, ok := exitCodeOf(err); !ok || code != 0 {
		t.Fatalf("%v", err)
	}
	for _, want := range []string{
		"owner has never signed in to alpha.\n", "a-new-password", "Copy it now. Press Enter to open Element.\n", "Opening alpha in Element.\n",
	} {
		if !strings.Contains(h.errOut.String(), want) {
			t.Fatalf("stderr %q lacks %q", h.errOut.String(), want)
		}
	}
}

func TestElementNeverSignedInDeclinedStillOpens(t *testing.T) {
	h := newPickerHarness(t)
	h.withElementProfile(t)
	h.on(sudoBus+"human devices alpha owner", "", 0)
	h.script.fzf = []string{"|EXIT"}
	r := h.pass(t)
	_, err := r.openElement(context.Background(), "alpha")
	if code, ok := exitCodeOf(err); !ok || code != 0 {
		t.Fatalf("%v", err)
	}
	if h.fake.Called(sudoBus+"human password") || !h.fake.Called("systemd-run") {
		t.Fatalf("calls %v", h.fake.Calls())
	}
	if strings.Contains(h.errOut.String(), "Copy it now") {
		t.Fatal("nothing to copy when no password was set")
	}
}

func TestElementWithNoProfileOffersToSetItUp(t *testing.T) {
	h := newPickerHarness(t)
	h.script.gum = []string{"NO"}
	r := h.pass(t)
	res, err := r.openElement(context.Background(), "alpha")
	if err != nil || res != refused {
		t.Fatalf("%v %v", res, err)
	}
	for _, want := range []string{
		"alpha has no Element profile on this desktop yet.\n", "Nothing set up. Ctrl-E offers it again.\nPress Enter to go back to the list.\n",
	} {
		if !strings.Contains(h.errOut.String(), want) {
			t.Fatalf("stderr %q lacks %q", h.errOut.String(), want)
		}
	}
	if strings.Contains(h.errOut.String(), "agent_bus_element_teams") {
		t.Fatal("the host_vars key is never named")
	}
	if want := "gum confirm --default=true Set one up now? It installs Element Desktop if needed (sudo asks for your password)."; h.script.gumLog[0] != want {
		t.Fatalf("gum %q", h.script.gumLog)
	}
	if len(h.ops.calls) != 0 {
		t.Fatalf("ops %v", h.ops.calls)
	}
}

func TestElementSetUpDeclaresThenRunsThePlayThenOpens(t *testing.T) {
	h := newPickerHarness(t)
	h.onDeclare("element", "CHANGED\tteam alpha\n")
	h.on("systemd-run", "", 0)
	h.on(sudoBus+"human devices alpha owner", "DEVICE\ta\t-\n", 0)
	h.script.gum = []string{"YES"}
	h.ops.elementEr = nil
	r := h.pass(t)
	h.ops.calls = nil
	// the play writes the launcher; the fake operator does it here
	r.a.Ops = writingOps{fakeOps: h.ops, launcher: h.elementLauncher(), t: t}
	_, err := r.openElement(context.Background(), "alpha")
	if code, ok := exitCodeOf(err); !ok || code != 0 {
		t.Fatalf("%v: %s", err, h.errOut.String())
	}
	if len(h.ops.calls) != 1 || h.ops.calls[0] != "elementPlay alpha" {
		t.Fatalf("ops %v", h.ops.calls)
	}
}

type writingOps struct {
	*fakeOps

	launcher string
	t        *testing.T
}

func (w writingOps) runElementPlay(ctx context.Context, teams []string) error {
	mustWrite(w.t, w.launcher, "")
	return w.fakeOps.runElementPlay(ctx, teams)
}

func TestElementSetUpFailureIsSaidAndKeepsThePicker(t *testing.T) {
	h := newPickerHarness(t)
	h.onDeclare("element", "CHANGED\tx\n")
	h.script.gum = []string{"YES"}
	h.ops.elementEr = errors.New("play failed")
	r := h.pass(t)
	res, err := r.openElement(context.Background(), "alpha")
	if err != nil || res != refused {
		t.Fatalf("%v %v", res, err)
	}
	if !strings.Contains(h.errOut.String(), "alpha was not set up in Element, for the reason above.\nPress Enter to go back to the list.\n") {
		t.Fatalf("stderr %q", h.errOut.String())
	}
	if _, err := os.Stat(h.elementLauncher()); err == nil {
		t.Fatal("no launcher was written")
	}

	h = newPickerHarness(t)
	h.onErr(h.declarePrefix("element"), "declare: refused", 78)
	h.script.gum = []string{"YES"}
	r = h.pass(t)
	if res, err := r.openElement(context.Background(), "alpha"); err != nil || res != refused {
		t.Fatalf("%v %v", res, err)
	}
	if len(h.ops.calls) != 0 || !strings.Contains(h.errOut.String(), "declare: refused\n") {
		t.Fatalf("ops %v stderr %q", h.ops.calls, h.errOut.String())
	}
}

func TestElementSetUpCancelledEndsWith130(t *testing.T) {
	h := newPickerHarness(t)
	h.script.gum = []string{"CANCEL"}
	r := h.pass(t)
	_, err := r.openElement(context.Background(), "alpha")
	if code, ok := exitCodeOf(err); !ok || code != term.ExitCancelled {
		t.Fatalf("%v", err)
	}
}
