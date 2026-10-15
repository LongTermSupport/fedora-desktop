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

func TestDispatchForCheckoutIsTheFindFlow(t *testing.T) {
	h := world(t)
	h.On("git rev-parse --is-inside-work-tree", "true\n", 0)
	h.On("git remote get-url origin", "git@example.com:example-org/proj.git\n", 0)
	h.OnDeclare("github-remote", "example-org/proj\n")
	h.OnDeclare("teams-for", "TEAM\talpha\n")
	if code := h.app.Run([]string{"for-checkout"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if h.Out.String() != "alpha\n" {
		t.Fatalf("stdout %q", h.Out.String())
	}
}

func TestDispatchChooseTeamsIsTheChooserAndItsPayloadIsTheSeatList(t *testing.T) {
	h := world(t)
	teamtest.Interactive(t, h.World)
	if code := h.app.Run([]string{"choose-teams", "--type=podman", "2@alpha"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if h.Out.String() != "2@alpha\n" {
		t.Fatalf("stdout %q", h.Out.String())
	}
}

func TestChooseTeamsHasNoForCheckoutChooseAnyMore(t *testing.T) {
	h := world(t)
	if code := h.app.Run([]string{"for-checkout", "--choose"}); code != term.ExitUsage {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
}

func TestAChooseTeamsAppKeepsStdoutForThePayloadAlone(t *testing.T) {
	a := NewAppFor(config.Config{RootDir: "/checkout"}, []string{"choose-teams", "--type=podman"})
	if a.Payload != os.Stdout || a.Stdout != os.Stderr || a.Ops.Stdout != os.Stderr {
		t.Fatalf("payload %v stdout %v ops %v", a.Payload, a.Stdout, a.Ops.Stdout)
	}
	b := NewAppFor(config.Config{RootDir: "/checkout"}, []string{"--list"})
	if b.Payload != os.Stdout || b.Stdout != os.Stdout {
		t.Fatalf("a command whose stdout is its output keeps it: payload %v stdout %v", b.Payload, b.Stdout)
	}
}

func TestDispatchRecordSeatsIsTheFindFlow(t *testing.T) {
	h := world(t)
	if code := h.app.Run([]string{"record-seats", "one", "two"}); code != term.ExitUsage {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if !strings.Contains(h.ErrOut.String(), "record-seats") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestDispatchChooseSeatAndDescribeSeatsAreTheChooseFlow(t *testing.T) {
	h := world(t)
	if code := h.app.Run([]string{"choose-seat"}); code != term.ExitUsage ||
		!strings.Contains(h.ErrOut.String(), "'choose-seat' takes one team") {
		t.Fatalf("choose-seat: exit %d: %s", code, h.ErrOut.String())
	}
	h = world(t)
	h.On("agent-bus seat list", "SEAT\talpha\t1\theld\tself\tproj.1+local.podman\n", 0)
	h.On(teamtest.SudoBus+"list alpha", "MEMBER\tproj.1+local.podman\tcoordinator\tjoin\tactive\n", 0)
	if code := h.app.Run([]string{"describe-seats", "1@alpha"}); code != term.ExitOK {
		t.Fatalf("describe-seats: exit %d: %s", code, h.ErrOut.String())
	}
	if h.Out.String() != "1@alpha (coordinator)\n" {
		t.Fatalf("stdout %q", h.Out.String())
	}
}

func TestUsageTextNamesTheLauncherSubcommandsAndTheCoordinator(t *testing.T) {
	h := newHarness(t)
	h.app.Run([]string{"--help"})
	for _, want := range []string{"choose-seat", "describe-seats", "coordinator"} {
		if !strings.Contains(h.Out.String(), want) {
			t.Fatalf("usage lacks %q", want)
		}
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

func TestDispatchLxcAddIsTheAdderAndTheUsageNamesIt(t *testing.T) {
	h := world(t)
	if code := h.app.Run([]string{"lxc-add"}); code != term.ExitUsage ||
		!strings.Contains(h.ErrOut.String(), "'lxc-add' takes one team: ccy-teams lxc-add <team>") {
		t.Fatalf("no team: exit %d: %s", code, h.ErrOut.String())
	}
	h = world(t)
	if code := h.app.Run([]string{"lxc-add", "alpha"}); code != term.ExitUsage ||
		!strings.Contains(h.ErrOut.String(), "asks its questions on a terminal") {
		t.Fatalf("no terminal: exit %d: %s", code, h.ErrOut.String())
	}
	h = newHarness(t)
	h.app.Run([]string{"--help"})
	for _, want := range []string{"ccy-teams lxc-add <team>", "agent_bus_lxc_members", "Add an LXC container's agent"} {
		if !strings.Contains(h.Out.String(), want) {
			t.Fatalf("usage lacks %q", want)
		}
	}
}

func TestLxcAddRefusesWhenTheCheckoutHasGone(t *testing.T) {
	h := newHarness(t)
	if err := os.Remove(filepath.Join(h.Root, "helpers", "agent_bus", "declare.py")); err != nil {
		t.Fatal(err)
	}
	if code := h.app.Run([]string{"lxc-add", "alpha"}); code != term.ExitFailure || !strings.Contains(h.ErrOut.String(), "has gone") {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
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
