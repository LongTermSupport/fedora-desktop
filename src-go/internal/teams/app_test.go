package teams

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/config"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// harness is the fake machine and the App wired over it.
type harness struct {
	*teamtest.World

	app *App
}

func newHarness(t *testing.T) harness {
	t.Helper()
	w := teamtest.New(t)
	return harness{World: w, app: Wire(w.Host)}
}

// world is the machine with team alpha declared, built and up.
func world(t *testing.T) harness {
	t.Helper()
	w := teamtest.Populated(t)
	return harness{World: w, app: Wire(w.Host)}
}

func TestHelpPrintsTheUsageToStdout(t *testing.T) {
	for _, flag := range []string{"-h", "--help"} {
		h := newHarness(t)
		if code := h.app.Run([]string{flag}); code != term.ExitOK {
			t.Fatalf("%s: exit %d", flag, code)
		}
		if !strings.Contains(h.Out.String(), "ccy-teams -l|--list") || strings.Contains(h.Out.String(), "set -euo") {
			t.Fatalf("%s: usage %q", flag, h.Out.String())
		}
	}
}

func TestUsageErrors(t *testing.T) {
	cases := map[string][]string{
		"ccy-teams: unknown option '--nope'. See ccy-teams --help.\n":             {"--nope"},
		"ccy-teams: '--list' takes no further arguments. See ccy-teams --help.\n": {"--list", "x"},
		"ccy-teams: '-l' takes no further arguments. See ccy-teams --help.\n":     {"-l", "x"},
	}
	for want, args := range cases {
		h := newHarness(t)
		if code := h.app.Run(args); code != term.ExitUsage {
			t.Fatalf("%v: exit %d", args, code)
		}
		if h.ErrOut.String() != want {
			t.Fatalf("%v: stderr %q", args, h.ErrOut.String())
		}
	}
}

func TestMissingCcyLibRefusesBeforeAnySubcommand(t *testing.T) {
	h := newHarness(t)
	h.app.CcyLib = filepath.Join(t.TempDir(), "absent")
	if code := h.app.Run([]string{"--list"}); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.ErrOut.String(), "CCY is not installed here ("+h.app.CcyLib+"/tmux-session.bash missing)") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
	if len(h.Fake.Calls()) != 0 {
		t.Fatalf("nothing runs without CCY: %v", h.Fake.Calls())
	}
}

func TestDispatchWithNoArgumentsIsThePicker(t *testing.T) {
	h := newHarness(t)
	if code := h.app.Run(nil); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.ErrOut.String(), "ccy-teams needs a terminal") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
	if h.Out.String() != "" {
		t.Fatalf("stdout %q", h.Out.String())
	}
}

func TestDispatchListIsTheBoard(t *testing.T) {
	h := world(t)
	if code := h.app.Run([]string{"--list"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if !strings.HasPrefix(h.Out.String(), "TEAM ") || !strings.Contains(h.Out.String(), "up to date") {
		t.Fatalf("stdout %q", h.Out.String())
	}
}

func TestSubcommandsReachTheirRealFlowsWithTheirUsageErrors(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"create", "--bogus"}, "ccy-teams: unknown create option '--bogus'. See ccy-teams --help."},
		{[]string{"apply"}, "ccy-teams: 'apply' takes one team, or --all. See ccy-teams --help."},
		{[]string{"remove"}, "ccy-teams: 'remove' takes one team (and no --yes: it always asks). See ccy-teams --help."},
		{[]string{"backup"}, "ccy-teams: 'backup' takes one team. See ccy-teams --help."},
		{[]string{"restore", "--nope"}, "unknown restore option '--nope'"},
		{[]string{"restore"}, "ccy-teams: "},
	}
	for _, c := range cases {
		h := newHarness(t)
		if code := h.app.Run(c.args); code != term.ExitUsage {
			t.Fatalf("%v: exit %d, stderr %q", c.args, code, h.ErrOut.String())
		}
		if !strings.Contains(h.ErrOut.String(), c.want) {
			t.Fatalf("%v: stderr %q, want %q", c.args, h.ErrOut.String(), c.want)
		}
	}
}

func TestCreateApplyAndRemoveRefuseWhenTheCheckoutHasGone(t *testing.T) {
	for _, sub := range []string{"create", "apply", "remove"} {
		h := newHarness(t)
		if err := os.Remove(filepath.Join(h.Root, "helpers", "agent_bus", "declare.py")); err != nil {
			t.Fatal(err)
		}
		args := []string{sub, "alpha"}
		if sub == "create" {
			args = []string{sub, "--yes"}
		}
		if code := h.app.Run(args); code != term.ExitFailure {
			t.Fatalf("%s: exit %d", sub, code)
		}
		if !strings.Contains(h.ErrOut.String(), "has gone") {
			t.Fatalf("%s: stderr %q", sub, h.ErrOut.String())
		}
	}
}

func TestNewAppIsTheRealSystemOverTheConfig(t *testing.T) {
	a := NewApp(config.Config{RootDir: "/checkout"})
	if a.Runner == nil || a.Board == nil || a.Ops == nil || a.Login == nil || a.Seats == nil || a.Create == nil || a.Picker == nil {
		t.Fatalf("app %+v", a)
	}
	if a.RootDir != "/checkout" {
		t.Fatalf("config %+v", a.Config)
	}
}
