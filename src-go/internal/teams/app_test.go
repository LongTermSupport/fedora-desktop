package teams

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
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

// Plan 00203 M5: the picker's Running sessions view changes a session through the very
// `ccy-teams session` command the App dispatches, so the picker and the command line cannot
// differ.
func TestThePickersRunningSessionsAreTheSessionCommand(t *testing.T) {
	h := newHarness(t)
	if h.app.Picker.Session != h.app.Session || h.app.Session == nil {
		t.Fatalf("picker hot control %v, session command %v", h.app.Picker.Session, h.app.Session)
	}
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
	h.On("git rev-parse --show-toplevel", h.Checkout()+"\n", 0)
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
	if code := h.app.Run([]string{"record-seats", "one", "two", "three"}); code != term.ExitUsage {
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
	h.RemoveSeat(t, h.Checkout(), "alpha", "dev")
	h.On("git rev-parse --show-toplevel", h.Checkout()+"\n", 0)
	h.On(teamtest.SudoBus+"list alpha", "MEMBER\tproj.1+local.podman\tcoordinator\tjoin\tactive\n", 0)
	if code := h.app.Run([]string{"describe-seats", "1@alpha"}); code != term.ExitOK {
		t.Fatalf("describe-seats: exit %d: %s", code, h.ErrOut.String())
	}
	if h.Out.String() != "1@alpha (coordinator)\n" {
		t.Fatalf("stdout %q", h.Out.String())
	}
}

func TestDispatchInternalMembershipInitIsTheMembershipCommand(t *testing.T) {
	h := newHarness(t)
	runtime := t.TempDir()
	h.Env["XDG_RUNTIME_DIR"] = runtime
	if code := h.app.Run([]string{"internal", "membership-init", "ccy-proj", "/c", "1@alpha"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if want := filepath.Join(runtime, "ccy", "membership", "ccy-proj") + "\n"; h.Out.String() != want {
		t.Fatalf("stdout %q, want %q", h.Out.String(), want)
	}
}

func TestDispatchInternalRestartArgsPassesThroughForAnUnnamedLaunch(t *testing.T) {
	h := newHarness(t)
	h.app.Stdin = strings.NewReader("--no-ssh\x00")
	if code := h.app.Run([]string{"internal", "restart-args", ""}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if h.Out.String() != "--no-ssh\x00" {
		t.Fatalf("stdout %q", h.Out.String())
	}
}

func TestDispatchInternalClaimContainerNameIsTheClaimCommand(t *testing.T) {
	h := newHarness(t)
	h.Env["XDG_RUNTIME_DIR"] = t.TempDir()
	h.On("podman ps -a --format {{.Names}}", "proj_yolo\n", 0)
	if code := h.app.Run([]string{"internal", "claim-container-name", "proj", "yolo", strconv.Itoa(os.Getpid())}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if h.Out.String() != "proj_yolo_1\n" {
		t.Fatalf("stdout %q", h.Out.String())
	}
}

func TestDispatchSessionIsTheHotCommand(t *testing.T) {
	h := newHarness(t)
	if code := h.app.Run([]string{"session", "frob"}); code != term.ExitUsage ||
		!strings.Contains(h.ErrOut.String(), "unknown session command 'frob'") {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	h = newHarness(t)
	if code := h.app.Run([]string{"session", "--help"}); code != term.ExitOK ||
		!strings.Contains(h.Out.String(), "Only sessions started through ccy's tmux") {
		t.Fatalf("help: exit %d: %s", code, h.Out.String())
	}
}

func TestInternalNeedsOneOfItsCommands(t *testing.T) {
	for _, args := range [][]string{{"internal"}, {"internal", "nope"}} {
		h := newHarness(t)
		if code := h.app.Run(args); code != term.ExitUsage {
			t.Fatalf("%v: exit %d", args, code)
		}
		if !strings.Contains(h.ErrOut.String(), "internal membership-init") ||
			!strings.Contains(h.ErrOut.String(), "internal claim-container-name") {
			t.Fatalf("%v: stderr %q", args, h.ErrOut.String())
		}
	}
}

func TestUsageTextNamesTheLauncherSubcommandsAndTheCoordinator(t *testing.T) {
	h := newHarness(t)
	h.app.Run([]string{"--help"})
	wants := []string{
		"choose-seat", "describe-seats", "coordinator", "ccy-teams shutdown <team> [--yes]", "ccy-teams restart <team> [--yes]",
		"ccy-teams wind-down <team> [--yes]",
		"ccy-teams internal membership-init <session> <checkout> <seats>",
		"ccy-teams internal restart-args <session>", "ccy-teams session list",
		"ccy-teams internal claim-container-name <project> <suffix> <owner-pid>",
		"ccy-teams session add <session> <team>[/<seat>] [--role=worker|coordinator] [--demote-current] [--yes]",
		"ccy-teams session remove <session> <team> [--coordinator=demote|hand-to=<seat>] [--park] [--yes]",
		"ccy-teams session move <session> <from-team> <to-team>[/<seat>]",
		"ccy-teams session role <session>|<handle> <team> worker|coordinator [--demote-current] [--yes]",
		"ccy-teams session repair <session> [--yes]",
		"Only sessions started through ccy's tmux (listed by ccy-sessions), and cc sessions started with",
	}
	for _, want := range wants {
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
		{[]string{"shutdown"}, "ccy-teams: 'shutdown' needs a team. See ccy-teams --help."},
		{[]string{"shutdown", "alpha", "--now"}, "ccy-teams: unknown shutdown option '--now'. See ccy-teams --help."},
		{[]string{"restart"}, "ccy-teams: 'restart' needs a team. See ccy-teams --help."},
		{[]string{"restart", "alpha", "--now"}, "ccy-teams: unknown restart option '--now'. See ccy-teams --help."},
		{[]string{"wind-down"}, "ccy-teams: 'wind-down' needs a team. See ccy-teams --help."},
		{[]string{"wind-down", "alpha", "--now"}, "ccy-teams: unknown wind-down option '--now'. See ccy-teams --help."},
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

// Security audit H1: a launcher starts an agent's session on the terminal ccy-teams ran on, so
// a run that made sudo ready drops the credential before it ends.
func TestARunThatMadeSudoReadyDropsTheCredentialLast(t *testing.T) {
	h := world(t)
	h.On("sudo -n -v", "", 0)
	h.On("sudo -n /usr/local/sbin/agent-bus-install backup-now --team alpha", "backup 1 written\n", 0)
	if code := h.app.Run([]string{"backup", "alpha"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	calls := h.Fake.Calls()
	if calls[len(calls)-1].Line() != "sudo -k" {
		t.Fatalf("the last command is not sudo -k: %v", calls)
	}
}

// Review L-A: Ctrl-C after sudo was made ready must not kill ccy-teams before it drops the
// credential. Run takes its context from Interrupted; a run cut short through it still ends
// with sudo -k.
func TestARunInterruptedAfterSudoWasMadeReadyStillDropsTheCredential(t *testing.T) {
	h := world(t)
	var interrupt context.CancelFunc
	h.app.Interrupted = func(parent context.Context) (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(parent)
		interrupt = cancel
		return ctx, cancel
	}
	h.On("sudo -n -v", "", 0)
	h.Fake.OnFunc([]string{"sudo", "-n", "/usr/local/sbin/agent-bus-install", "backup-now"},
		func(execx.Cmd) (execx.Result, error) {
			interrupt()
			return execx.Result{ExitCode: 130}, nil
		})
	if code := h.app.Run([]string{"backup", "alpha"}); code == term.ExitOK {
		t.Fatal("an interrupted backup exited 0")
	}
	calls := h.Fake.Calls()
	if calls[len(calls)-1].Line() != "sudo -k" {
		t.Fatalf("the last command is not sudo -k: %v", calls)
	}
}

// The default Interrupted catches SIGINT as a cancelled context instead of letting it end the
// process. Without it this SIGINT ends the test binary, which is the red.
func TestTheDefaultInterruptedTurnsCtrlCIntoACancelledContext(t *testing.T) {
	h := newHarness(t)
	ctx, stop := h.app.Interrupted(context.Background())
	defer stop()
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	<-ctx.Done()
}

func TestARunThatNeverMadeSudoReadyLeavesItAlone(t *testing.T) {
	h := world(t)
	if code := h.app.Run([]string{"--list"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if h.Fake.Called("sudo -k") {
		t.Fatalf("sudo -k ran: %v", h.Fake.Calls())
	}
}

func TestACredentialThatCannotBeDroppedFailsTheRun(t *testing.T) {
	h := world(t)
	h.On("sudo -n -v", "", 0)
	h.On("sudo -n /usr/local/sbin/agent-bus-install backup-now --team alpha", "", 0)
	h.On("sudo -k", "", 1)
	if code := h.app.Run([]string{"backup", "alpha"}); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.ErrOut.String(), "the sudo credential could not be dropped") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}
