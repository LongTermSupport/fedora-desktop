package login

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func (h rig) withElementProfile(t *testing.T) {
	t.Helper()
	teamtest.MustWrite(t, h.elementLauncher(), "[Desktop Entry]\n")
	h.On("systemd-run", "", 0)
	// the people have each signed in, so no password is offered
	h.On(teamtest.SudoBus+"human devices alpha owner", "DEVICE\ta\t-\n", 0)
}

// ownerDeviceReads is how many times the owner's Element sessions were read.
func (h rig) ownerDeviceReads() int {
	n := 0
	for _, c := range h.Fake.Calls() {
		if strings.HasPrefix(c.Line(), teamtest.SudoBus+"human devices alpha owner") {
			n++
		}
	}
	return n
}

func TestElementOpensTheProfileInItsOwnScopeAndReturnsToTheList(t *testing.T) {
	h := newRig(t)
	h.withElementProfile(t)
	snap := h.pass(t)
	reads := h.ownerDeviceReads()
	res, err := h.l.OpenElement(context.Background(), snap, "alpha")
	if err != nil || res != flow.Done {
		t.Fatalf("%v %v: %s", res, err, h.ErrOut.String())
	}
	h.pass(t)
	if h.ownerDeviceReads() != reads+1 {
		t.Fatalf("the list reads the team's Element sessions again after Element is started: %d then %d", reads, h.ownerDeviceReads())
	}
	var run execx.Cmd
	for _, c := range h.Fake.Calls() {
		if c.Name == "systemd-run" {
			run = c
		}
	}
	line := run.Line()
	if !strings.HasPrefix(line, "systemd-run --user --scope --quiet --collect --unit ccy-teams-element-alpha-") ||
		!strings.HasSuffix(line, "setsid -w gio launch "+h.elementLauncher()) {
		t.Fatalf("systemd-run %q", line)
	}
	logPath := filepath.Join(h.l.StateDir, "element-alpha.log")
	for _, want := range []string{"Opening alpha in Element.\n", "Element is starting; its own messages go to " + term.AbbrevHome(logPath, h.l.Home) + ".\n"} {
		if !strings.Contains(h.ErrOut.String(), want) {
			t.Fatalf("stderr %q lacks %q", h.ErrOut.String(), want)
		}
	}
	if h.Fake.Called(teamtest.SudoBus + "human password") {
		t.Fatal("a person who has signed in is offered no password")
	}
}

func TestElementFailedLaunchIsExplicitlyAFailureWithTheLogsTail(t *testing.T) {
	h := newRig(t)
	h.withElementProfile(t)
	logPath := filepath.Join(h.l.StateDir, "element-alpha.log")
	teamtest.MustWrite(t, logPath, "electron chatter on stderr\n")
	h.OnErr("systemd-run", "gio: failed", 1)
	snap := h.pass(t)
	_, err := h.l.OpenElement(context.Background(), snap, "alpha")
	if code, ok := flow.ExitCodeOf(err); !ok || code != term.ExitFailure {
		t.Fatalf("%v", err)
	}
	for _, want := range []string{"ERROR: Element did not start; its messages are in ", "electron chatter on stderr\n"} {
		if !strings.Contains(h.ErrOut.String(), want) {
			t.Fatalf("stderr %q lacks %q", h.ErrOut.String(), want)
		}
	}
}

func TestElementLaunchFailureThatIsNotAnExitIsReportedToo(t *testing.T) {
	h := newRig(t)
	h.withElementProfile(t)
	// the log's directory is a file, so the launch cannot even open its log
	teamtest.MustWrite(t, h.l.StateDir, "")
	snap := h.pass(t)
	_, err := h.l.OpenElement(context.Background(), snap, "alpha")
	if code, ok := flow.ExitCodeOf(err); !ok || code != term.ExitFailure {
		t.Fatalf("%v", err)
	}
	if !strings.Contains(h.ErrOut.String(), "ERROR: ") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestElementOffersNeverSignedInPeopleANewPassword(t *testing.T) {
	h := newRig(t)
	h.withElementProfile(t)
	h.On(teamtest.SudoBus+"human devices alpha owner", "", 0)
	h.passwordable()
	h.Script.Fzf = []string{"|YES"}
	snap := h.pass(t)
	if _, err := h.l.OpenElement(context.Background(), snap, "alpha"); err != nil {
		t.Fatalf("%v", err)
	}
	for _, want := range []string{
		"owner has never signed in to alpha.\n", "a-new-password", afterFirstPassword, "Opening alpha in Element.\n",
	} {
		if !strings.Contains(h.ErrOut.String(), want) {
			t.Fatalf("stderr %q lacks %q", h.ErrOut.String(), want)
		}
	}
}

// afterFirstPassword is what Open in Element says once a first password is shown: true whether
// it went to the clipboard, was not offered for it, or was declined for it.
const afterFirstPassword = "Press Enter to open Element, then sign in there as owner with the password shown above.\n"

// Someone who has never signed in has no Element session for a new password to sign out, and
// cannot sign in without one: Enter sets it.
func TestElementFirstSignInDefaultsToSettingAPassword(t *testing.T) {
	h := newRig(t)
	h.withElementProfile(t)
	h.On(teamtest.SudoBus+"human devices alpha owner", "", 0)
	h.passwordable()
	h.Script.Fzf = []string{"|YES"}
	snap := h.pass(t)
	if _, err := h.l.OpenElement(context.Background(), snap, "alpha"); err != nil {
		t.Fatalf("%v", err)
	}
	if !strings.Contains(h.HeaderOf(0), "owner has no Element session to sign out.") {
		t.Fatalf("header %q", h.HeaderOf(0))
	}
	if slices.Contains(h.Script.FzfArgs[0], "--bind=start:last") {
		t.Fatal("a first password defaults to Yes")
	}
}

// After a first password, nothing tells the person to copy it: with Clipboard Indicator on it
// is not offered for the clipboard and is typed from the screen.
func TestElementAfterAFirstPasswordNeverSaysCopyIt(t *testing.T) {
	cases := map[string]func(h rig){
		"Clipboard Indicator on": func(h rig) {
			h.On("gsettings get org.gnome.shell enabled-extensions", "['clipboard-indicator@tudmotu.com']\n", 0)
			h.Script.Fzf = []string{"|YES"}
		},
		"Clipboard Indicator off, copied": func(h rig) {
			h.withoutClipboardHistory()
			h.On("wl-copy", "", 0)
			h.Script.Fzf = []string{"|YES", "|YES"}
		},
		"Clipboard Indicator off, not copied": func(h rig) {
			h.withoutClipboardHistory()
			h.Script.Fzf = []string{"|YES", "|EXIT"}
		},
		"no gsettings": func(h rig) {
			h.l.Kit.LookPath = func(name string) (string, error) {
				if name == "gsettings" {
					return "", errors.New("not found")
				}
				return "/usr/bin/" + name, nil
			}
			h.On("wl-copy", "", 0)
			h.Script.Fzf = []string{"|YES", "|YES"}
		},
	}
	for name, setUp := range cases {
		t.Run(name, func(t *testing.T) {
			h := newRig(t)
			h.withElementProfile(t)
			h.On(teamtest.SudoBus+"human devices alpha owner", "", 0)
			h.passwordable()
			h.Env["WAYLAND_DISPLAY"] = "wayland-test"
			setUp(h)
			snap := h.pass(t)
			if _, err := h.l.OpenElement(context.Background(), snap, "alpha"); err != nil {
				t.Fatalf("%v", err)
			}
			out := h.ErrOut.String()
			if strings.Contains(out, "Copy it") || !strings.Contains(out, afterFirstPassword) {
				t.Fatalf("stderr %q", out)
			}
		})
	}
}

func TestElementNeverSignedInDeclinedStillOpens(t *testing.T) {
	h := newRig(t)
	h.withElementProfile(t)
	h.On(teamtest.SudoBus+"human devices alpha owner", "", 0)
	h.Script.Fzf = []string{"|EXIT"}
	snap := h.pass(t)
	if _, err := h.l.OpenElement(context.Background(), snap, "alpha"); err != nil {
		t.Fatalf("%v", err)
	}
	if h.Fake.Called(teamtest.SudoBus+"human password") || !h.Fake.Called("systemd-run") {
		t.Fatalf("calls %v", h.Fake.Calls())
	}
	if strings.Contains(h.ErrOut.String(), "password shown above") {
		t.Fatal("nothing to copy when no password was set")
	}
}

func TestElementWithNoProfileOffersToSetItUp(t *testing.T) {
	h := newRig(t)
	h.Script.Gum = []string{"NO"}
	snap := h.pass(t)
	res, err := h.l.OpenElement(context.Background(), snap, "alpha")
	if err != nil || res != flow.Refused {
		t.Fatalf("%v %v", res, err)
	}
	for _, want := range []string{
		"alpha has no Element profile on this desktop yet.\n", "Nothing set up. Opening the team in Element offers it again.\nPress Enter to go back to the list.\n",
	} {
		if !strings.Contains(h.ErrOut.String(), want) {
			t.Fatalf("stderr %q lacks %q", h.ErrOut.String(), want)
		}
	}
	if strings.Contains(h.ErrOut.String(), "agent_bus_element_teams") {
		t.Fatal("the host_vars key is never named")
	}
	if want := "gum confirm --default=true Set one up now? It installs Element Desktop if needed (sudo asks for your password)."; h.Script.GumLog[0] != want {
		t.Fatalf("gum %q", h.Script.GumLog)
	}
	if h.elementPlays() != 0 {
		t.Fatal("the Element play was run")
	}
}

func TestElementSetUpDeclaresThenRunsThePlayThenOpens(t *testing.T) {
	h := newRig(t)
	h.OnDeclare("element", "CHANGED\tteam alpha\n")
	h.On("systemd-run", "", 0)
	h.On(teamtest.SudoBus+"human devices alpha owner", "DEVICE\ta\t-\n", 0)
	h.Script.Gum = []string{"YES"}
	// the play writes the launcher
	h.Fake.OnFunc(strings.Fields(h.Host.ElementPlay), func(execx.Cmd) (execx.Result, error) {
		teamtest.MustWrite(t, h.elementLauncher(), "")
		return execx.Result{}, nil
	})
	snap := h.pass(t)
	if _, err := h.l.OpenElement(context.Background(), snap, "alpha"); err != nil {
		t.Fatalf("%v: %s", err, h.ErrOut.String())
	}
	if h.elementPlays() != 1 {
		t.Fatalf("the Element play ran %d times", h.elementPlays())
	}
}

// The Element profile is declared and its play run in one hold of the play lock; a wait for it
// that is stopped declares nothing.
func TestElementSetUpIsInOneHoldOfThePlayLock(t *testing.T) {
	h := newRig(t)
	heldAtDeclare := false
	h.Fake.OnFunc(strings.Fields(h.DeclarePrefix("element")), func(execx.Cmd) (execx.Result, error) {
		heldAtDeclare = h.PlayLockHeld(t)
		return execx.Result{Stdout: []byte("CHANGED\tteam alpha\n")}, nil
	})
	h.On("systemd-run", "", 0)
	h.On(teamtest.SudoBus+"human devices alpha owner", "DEVICE\ta\t-\n", 0)
	h.Script.Gum = []string{"YES"}
	h.Fake.OnFunc(strings.Fields(h.Host.ElementPlay), func(execx.Cmd) (execx.Result, error) {
		teamtest.MustWrite(t, h.elementLauncher(), "")
		return execx.Result{}, nil
	})
	if _, err := h.l.OpenElement(context.Background(), h.pass(t), "alpha"); err != nil {
		t.Fatalf("%v: %s", err, h.ErrOut.String())
	}
	if !heldAtDeclare || h.PlayLockHeld(t) {
		t.Fatalf("held at the declare %v, held after %v", heldAtDeclare, h.PlayLockHeld(t))
	}
}

func TestStoppingTheWaitForThePlayLockSetsNothingUpInElement(t *testing.T) {
	h := newRig(t)
	h.OnDeclare("element", "CHANGED\tteam alpha\n")
	h.HoldPlayLock(t)
	h.Script.Gum = []string{"YES"}
	h.Script.Fzf = []string{"|EXIT"}
	res, err := h.l.OpenElement(context.Background(), h.pass(t), "alpha")
	if err != nil || res != flow.Refused {
		t.Fatalf("%v %v", res, err)
	}
	if h.Fake.Called(h.DeclarePrefix("element")) || h.elementPlays() != 0 {
		t.Fatal("declared, or the play ran, without the lock")
	}
	if !strings.Contains(h.ErrOut.String(), "Nothing set up: the play lock was not free (above). Opening the team in Element offers it again.") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestElementSetUpFailureIsSaidAndKeepsThePicker(t *testing.T) {
	h := newRig(t)
	h.OnDeclare("element", "CHANGED\tx\n")
	h.Script.Gum = []string{"YES"}
	h.OnErr(h.Host.ElementPlay, "play failed", 1)
	snap := h.pass(t)
	res, err := h.l.OpenElement(context.Background(), snap, "alpha")
	if err != nil || res != flow.Refused {
		t.Fatalf("%v %v", res, err)
	}
	if !strings.Contains(h.ErrOut.String(), "alpha was not set up in Element, for the reason above.\nPress Enter to go back to the list.\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
	if _, err := os.Stat(h.elementLauncher()); err == nil {
		t.Fatal("no launcher was written")
	}

	h = newRig(t)
	h.OnErr(h.DeclarePrefix("element"), "declare: refused", 78)
	h.Script.Gum = []string{"YES"}
	snap = h.pass(t)
	if res, err := h.l.OpenElement(context.Background(), snap, "alpha"); err != nil || res != flow.Refused {
		t.Fatalf("%v %v", res, err)
	}
	if h.elementPlays() != 0 || !strings.Contains(h.ErrOut.String(), "declare: refused\n") {
		t.Fatalf("plays %d stderr %q", h.elementPlays(), h.ErrOut.String())
	}
}

func TestOpenEveryTeamLaunchesEachTeamWithAProfileInItsOwnWindow(t *testing.T) {
	h := newRig(t)
	h.withElementProfile(t)
	snap := h.pass(t)
	teamtest.MustWrite(t, filepath.Join(h.l.Home, ".local/share/applications/agent-bus-beta-element.desktop"), "[Desktop Entry]\n")
	snap.Teams = append(snap.Teams, "beta", "gamma")
	res, err := h.l.OpenEveryTeam(context.Background(), snap)
	if err != nil || res != flow.Done {
		t.Fatalf("%v %v: %s", res, err, h.ErrOut.String())
	}
	var launched []string
	for _, c := range h.Fake.Calls() {
		if c.Name == "systemd-run" {
			launched = append(launched, c.Line())
		}
	}
	if len(launched) != 2 || !strings.Contains(launched[0], "ccy-teams-element-alpha-") || !strings.Contains(launched[1], "ccy-teams-element-beta-") {
		t.Fatalf("launched %q", launched)
	}
	for _, want := range []string{
		"Opening alpha, beta in Element: one window each (each team is its own Matrix server, and an Element window holds one account).\n",
		"gamma has no Element profile on this desktop yet: Open gamma in Element (Ctrl-E on its row) sets one up.\n",
	} {
		if !strings.Contains(h.ErrOut.String(), want) {
			t.Fatalf("stderr %q lacks %q", h.ErrOut.String(), want)
		}
	}
	if h.Fake.Called(teamtest.SudoBus + "human password") {
		t.Fatal("opening every team sets no password")
	}
}

func TestOpenEveryTeamWithNoProfileSaysSoAndLaunchesNothing(t *testing.T) {
	h := newRig(t)
	snap := h.pass(t)
	res, err := h.l.OpenEveryTeam(context.Background(), snap)
	if err != nil || res != flow.Done {
		t.Fatalf("%v %v: %s", res, err, h.ErrOut.String())
	}
	if h.Fake.Called("systemd-run") {
		t.Fatal("Element was started with no profile")
	}
	if want := "alpha has no Element profile on this desktop yet: Open alpha in Element (Ctrl-E on its row) sets one up.\n"; !strings.Contains(h.ErrOut.String(), want) {
		t.Fatalf("stderr %q lacks %q", h.ErrOut.String(), want)
	}
}

func TestElementSetUpCancelledEndsWith130(t *testing.T) {
	h := newRig(t)
	h.Script.Gum = []string{"CANCEL"}
	snap := h.pass(t)
	_, err := h.l.OpenElement(context.Background(), snap, "alpha")
	if code, ok := flow.ExitCodeOf(err); !ok || code != term.ExitCancelled {
		t.Fatalf("%v", err)
	}
}
