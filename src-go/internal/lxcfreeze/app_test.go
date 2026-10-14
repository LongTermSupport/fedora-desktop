package lxcfreeze

// The command line end to end (spec 2 and 9.3): the bash suite ran no tool as a command line, so
// every case here is new. The harness runs App.RunContext over an execx.Fake primed with the
// fixture machine, a scripted chooser or a scripted /dev/tty, and buffers for the two streams, so
// the bytes on stdout and stderr and the exit status are all asserted.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/freeze"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

type harness struct {
	t    *testing.T
	app  *App
	fake *execx.Fake
	out  *bytes.Buffer
	errb *bytes.Buffer
}

// newHarness is a host (no container markers), no terminal, LXC and sudo installed, fzf absent,
// and the fixture machine behind the fake, every freeze, unfreeze and renewal succeeding.
// Tests override what they exercise.
func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, fake: scripted(fixtureBoxes()), out: &bytes.Buffer{}, errb: &bytes.Buffer{}}
	dir := t.TempDir()
	env := freeze.NewEnv(h.out, h.errb, h.fake)
	env.ContainerEnvPath = filepath.Join(dir, "containerenv")
	env.DockerEnvPath = filepath.Join(dir, "dockerenv")
	env.Getenv = func(string) string { return "" }
	env.LookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	env.IsTerminal = func(any) bool { return false }
	env.OpenTTY = func() (io.ReadCloser, error) { return nil, errors.New("no tty") }
	h.app = &App{Env: env, Stdin: strings.NewReader("")}
	h.fake.OnError([]string{"fzf"}, &execx.NotInstalledError{Name: "fzf"})
	for _, x := range fixtureBoxes() {
		h.fake.On([]string{"sudo", "-n", "lxc-freeze", "-n", x.name}, execx.Result{})
		h.fake.On([]string{"sudo", "-n", "lxc-unfreeze", "-n", x.name}, execx.Result{})
		h.fake.On([]string{"sudo", "-n", "lxc-attach", "-n", x.name, "--", "nmcli", "-t", "-f"}, execx.Result{Stdout: []byte("eth0:ethernet\n")})
		h.fake.On([]string{"sudo", "-n", "lxc-attach", "-n", x.name, "--", "nmcli", "device", "connect"}, execx.Result{})
	}
	return h
}

// withTTY gives the run a terminal whose /dev/tty reads the given replies.
func (h *harness) withTTY(replies string) {
	h.app.Env.IsTerminal = func(any) bool { return true }
	h.app.Env.OpenTTY = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(replies)), nil }
}

func (h *harness) run(args ...string) term.ExitCode {
	h.t.Helper()
	return h.app.RunContext(context.Background(), args)
}

func mustExit(t *testing.T, h *harness, want term.ExitCode, args ...string) {
	t.Helper()
	if got := h.run(args...); got != want {
		t.Fatalf("%v: exit %d, want %d\nstdout: %s\nstderr: %s", args, got, want, h.out, h.errb)
	}
}

// acts are the freeze and unfreeze commands, in order, as "freeze NAME" / "unfreeze NAME".
func (h *harness) acts() []string {
	var acts []string
	for _, c := range h.fake.Calls() {
		if len(c.Args) >= 3 && (c.Args[1] == "lxc-freeze" || c.Args[1] == "lxc-unfreeze") {
			acts = append(acts, strings.TrimPrefix(c.Args[1], "lxc-")+" "+c.Args[3])
		}
	}
	return acts
}

// count is how many recorded commands have exactly this line.
func (h *harness) count(line string) int {
	n := 0
	for _, c := range h.fake.Calls() {
		if c.Line() == line {
			n++
		}
	}
	return n
}

func tableRow(name, state, bridge, addr string) string {
	return fmt.Sprintf("  %-34s %-8s %-18s%-18s\n", name, state, bridge, addr)
}

var tableHead = tableRow("NAME", "STATE", "BRIDGE", "IPV4")

func menuLabel(desc, effect string) string { return fmt.Sprintf("%-38s %s", desc, effect) }

// ---- usage and --help ----

func TestHelpPrintsTheUsageToStdoutOnlyAndRunsNothing(t *testing.T) {
	for _, flag := range []string{"-h", "--help"} {
		h := newHarness(t)
		mustExit(t, h, 0, flag)
		if h.out.String() != usageText || h.errb.Len() != 0 {
			t.Fatalf("%s: stdout %q stderr %q", flag, h.out, h.errb)
		}
		if len(h.fake.Calls()) != 0 {
			t.Fatalf("--help runs no command (and asks for no password): %v", h.fake.Calls())
		}
	}
}

func TestHelpWorksInsideAContainerBecauseParsingPrecedesTheGuard(t *testing.T) {
	h := newHarness(t)
	h.app.Env.Getenv = func(k string) string {
		if k == "container" {
			return "lxc"
		}
		return ""
	}
	mustExit(t, h, 0, "--help")
	if h.out.String() != usageText {
		t.Fatal("the usage text is printed")
	}
}

func TestHelpMidLineStopsParsingButAnEarlierMistakeStillDies(t *testing.T) {
	h := newHarness(t)
	mustExit(t, h, 0, "--all", "--help", "--bridge")
	if h.out.String() != usageText {
		t.Fatal("-h mid-line prints usage")
	}
	h = newHarness(t)
	mustExit(t, h, 1, "--bogus", "--help")
	if h.out.Len() != 0 {
		t.Fatal("an earlier bad option means no usage")
	}
}

func TestAUsageThatCannotBeWrittenIsAFailure(t *testing.T) {
	h := newHarness(t)
	h.app.Env.Out = failWriter{}
	mustExit(t, h, 1, "--help")
	if !strings.Contains(h.errb.String(), "write usage") {
		t.Fatalf("got %q", h.errb)
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

// ---- usage mistakes: exit 1 (bug 10, kept), nothing run, message on stderr ----

func TestUsageMistakesExitOneWithTheMessageAndRunNothing(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"--bogus"}, "lxcfreeze: unknown option '--bogus' — see 'lxcfreeze --help'.\n"},
		{[]string{"--bridge"}, "lxcfreeze: --bridge needs a bridge name — see 'lxcfreeze --help'.\n"},
		{[]string{"list", "thaw"}, "lxcfreeze: only one verb at a time — already have 'list'.\n"},
		{[]string{"--all", "--bridge", "br"}, "lxcfreeze: choose one target only — '--all' and '--bridge' are mutually exclusive.\n"},
		{[]string{"--all", "--all"}, "lxcfreeze: choose one target only — '--all' and '--all' are mutually exclusive.\n"},
		{[]string{"--all", "foo"}, "lxcfreeze: choose one target only — '--all' and 'names' are mutually exclusive.\n"},
		// podfreeze's flags are unknown options here.
		{[]string{"--ccy"}, "lxcfreeze: unknown option '--ccy' — see 'lxcfreeze --help'.\n"},
		{[]string{"--network", "n"}, "lxcfreeze: unknown option '--network' — see 'lxcfreeze --help'.\n"},
		{[]string{"-nh"}, "lxcfreeze: unknown option '-nh' — see 'lxcfreeze --help'.\n"},
		// Bug 22: `list` with a target is refused before ANY external command: the bash asked for
		// a sudo password first.
		{[]string{"list", "--all"}, "lxcfreeze: 'list' takes no target — it reports everything.\n"},
		{[]string{"list", "alpha"}, "lxcfreeze: 'list' takes no target — it reports everything.\n"},
		{[]string{"--bridge", "lxcbr0", "list"}, "lxcfreeze: 'list' takes no target — it reports everything.\n"},
	}
	for _, tc := range cases {
		h := newHarness(t)
		mustExit(t, h, 1, tc.args...)
		if h.errb.String() != tc.want || h.out.Len() != 0 {
			t.Errorf("%v: stderr %q stdout %q", tc.args, h.errb, h.out)
		}
		if len(h.fake.Calls()) != 0 {
			t.Errorf("%v ran commands: %v", tc.args, h.fake.Calls())
		}
	}
}

// Bug 15, kept: a flag's value is the next token verbatim, and the result is a clean error after.
func TestAFlagsValueIsTheNextTokenVerbatim(t *testing.T) {
	h := newHarness(t)
	mustExit(t, h, 1, "--bridge", "-n")
	want := "lxcfreeze: no running or frozen container is on bridge '-n'.\n  In use right now: lxcbr0 virbr1\n"
	if h.errb.String() != want || len(h.acts()) != 0 {
		t.Fatalf("got %q", h.errb)
	}
}

// ---- the host guard and preflight ----

func TestRefusesInsideAContainerBeforeAnyCommand(t *testing.T) {
	want := "lxcfreeze: this is a container — run lxcfreeze on the HOST.\n" +
		"  The host's LXC is not reachable from in here, so every container would look\n" +
		"  absent and this tool would report a machine with nothing on it.\n"
	for name, arrange := range map[string]func(*harness){
		"containerenv": func(h *harness) { writeMarker(t, h.app.Env.ContainerEnvPath) },
		"dockerenv":    func(h *harness) { writeMarker(t, h.app.Env.DockerEnvPath) },
		"env": func(h *harness) {
			h.app.Env.Getenv = func(k string) string {
				if k == "container" {
					return "oci"
				}
				return ""
			}
		},
	} {
		h := newHarness(t)
		arrange(h)
		mustExit(t, h, 1, "--all")
		if h.errb.String() != want || len(h.fake.Calls()) != 0 {
			t.Errorf("%s: stderr %q calls %v", name, h.errb, h.fake.Calls())
		}
	}
}

func writeMarker(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAMarkerWeCannotReadIsAFailureNotAHost(t *testing.T) {
	h := newHarness(t)
	h.app.Env.Stat = func(string) (os.FileInfo, error) { return nil, fs.ErrPermission }
	mustExit(t, h, 1, "--all")
	if !strings.Contains(h.errb.String(), "cannot tell whether") || len(h.fake.Calls()) != 0 {
		t.Fatalf("got %q", h.errb)
	}
}

func TestPreflightFailureNamesTheMissingToolsAndAsksForNoPassword(t *testing.T) {
	h := newHarness(t)
	h.app.Env.LookPath = func(name string) (string, error) {
		if name == "lxc-info" {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + name, nil
	}
	mustExit(t, h, 1, "list")
	if !strings.HasPrefix(h.errb.String(), "lxcfreeze: LXC is not installed on this host — missing: lxc-info\n") || len(h.fake.Calls()) != 0 {
		t.Fatalf("got %q, calls %v", h.errb, h.fake.Calls())
	}
}

func TestNoSudoIsNamedBeforeAnyCommand(t *testing.T) {
	h := newHarness(t)
	h.app.Env.LookPath = func(name string) (string, error) {
		if name == "sudo" {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + name, nil
	}
	mustExit(t, h, 1, "list")
	if h.errb.String() != "lxcfreeze: no sudo on this host, and every LXC query needs root.\n" || len(h.fake.Calls()) != 0 {
		t.Fatalf("got %q", h.errb)
	}
}

// ---- root: cached, prompted, refused ----

func TestACachedSudoCredentialAsksNothing(t *testing.T) {
	h := newHarness(t)
	h.withTTY("")
	mustExit(t, h, 0, "list")
	if strings.Contains(h.errb.String(), "password") {
		t.Fatalf("a cached credential asks nothing: %q", h.errb)
	}
}

func TestAnExpiredCredentialIsExplainedThenAskedForOnTheTerminal(t *testing.T) {
	h := newHarness(t)
	h.withTTY("")
	h.fake.On([]string{"sudo", "-n", "-v"}, execx.Result{Stderr: []byte("sudo: a password is required\n"), ExitCode: 1})
	h.fake.On([]string{"sudo", "-v"}, execx.Result{})
	mustExit(t, h, 0, "list")
	text := h.errb.String()
	for _, want := range []string{
		"LXC here is rootful: /var/lib/lxc is root-only, so even listing containers needs root.\n",
		"sudo asks for your login password.\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %q", want, text)
		}
	}
	if !h.fake.Called("sudo -v") || !strings.HasPrefix(h.out.String(), "=== frozen") {
		t.Fatalf("the prompt ran, then the list: %q", h.out)
	}
}

// A refusal is "the question was never asked", never "no containers".
func TestARefusedSudoIsNotAnEmptyMachineAndChangesNothing(t *testing.T) {
	h := newHarness(t)
	h.fake.On([]string{"sudo", "-n", "-v"}, execx.Result{Stderr: []byte("sudo: a password is required\n"), ExitCode: 1})
	mustExit(t, h, 1, "freeze", "--all")
	text := h.errb.String()
	for _, want := range []string{"sudo was refused, so no LXC state could be read.", "This is NOT 'no containers found'"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %q", want, text)
		}
	}
	if h.out.Len() != 0 || len(h.acts()) != 0 || h.fake.Called("sudo -n lxc-ls") {
		t.Fatalf("stdout %q acts %v", h.out, h.acts())
	}
}

// ---- list (spec 3.6) ----

func TestListShowsFrozenThenRunningOnStdoutAfterOneLoad(t *testing.T) {
	h := newHarness(t)
	mustExit(t, h, 0, "list")
	want := "=== frozen (3) ===\n" + tableHead +
		tableRow("zulu", "FROZEN", "lxcbr0", "192.0.2.11") +
		tableRow("mike", "FROZEN", "virbr1", "") +
		tableRow("kilo", "FROZEN", bridgeNone, "") +
		"\n  Thaw them all with: lxcfreeze thaw --all\n" +
		"\n=== running (2) ===\n" + tableHead +
		tableRow("alpha", "RUNNING", "lxcbr0", "192.0.2.12") +
		tableRow("bravo", "RUNNING", "virbr1", "198.51.100.5")
	if h.out.String() != want {
		t.Fatalf("got:\n%q\nwant:\n%q", h.out, want)
	}
	if h.errb.Len() != 0 {
		t.Fatalf("list writes only stdout: %q", h.errb)
	}
	// Bug 26: one load. One ready, one lxc-ls, three probes per container.
	if h.count("sudo -n -v") != 1 || h.count("sudo -n lxc-ls -1") != 1 {
		t.Fatalf("calls %d", len(h.fake.Calls()))
	}
}

func TestListOfAMachineWithNoContainersSaysLXCIsInstalledAndEmpty(t *testing.T) {
	h := newHarness(t)
	script(h.fake, nil)
	mustExit(t, h, 0, "list")
	want := "=== frozen (0) ===\n  (none)\n\n=== running (0) ===\n  (none)\n" +
		"\n  LXC is installed and has no running or frozen containers.\n" +
		"  (A STOPPED container is not counted — see 'sudo lxc-ls -f'.)\n"
	if h.out.String() != want {
		t.Fatalf("got %q", h.out)
	}
}

// A list that could not be read is not an empty machine: the footer is withheld and the note
// says which containers, once.
func TestListWithAnUnreadableContainerNamesItOnceAndClaimsNothingAboutEmptiness(t *testing.T) {
	h := newHarness(t)
	script(h.fake, []box{{name: "ghost", info: stateLine("RUNNING"), infoFail: true}})
	mustExit(t, h, 0, "list")
	if strings.Contains(h.out.String(), "has no running or frozen containers") {
		t.Fatalf("got %q", h.out)
	}
	if n := strings.Count(h.errb.String(), "NOTE — 1 container(s) could not be read"); n != 1 {
		t.Fatalf("%d notes in %q", n, h.errb)
	}
}

func TestListIgnoresDryRun(t *testing.T) {
	h := newHarness(t)
	mustExit(t, h, 0, "list", "-n")
	if !strings.HasPrefix(h.out.String(), "=== frozen (3) ===") {
		t.Fatalf("got %q", h.out)
	}
}

func TestAFailedLxcLsIsFatalNotAnEmptyMachine(t *testing.T) {
	h := newHarness(t)
	h.fake.On([]string{"sudo", "-n", "lxc-ls", "-1"}, execx.Result{Stderr: []byte("lxc-ls: cannot open /var/lib/lxc\n"), ExitCode: 1})
	mustExit(t, h, 1, "list")
	if !strings.Contains(h.errb.String(), "lxcfreeze: lxc-ls failed, so the container list is unknown: lxc-ls: cannot open /var/lib/lxc") || h.out.Len() != 0 {
		t.Fatalf("got %q", h.errb)
	}
}

// ---- one-shot flows (spec 2.3 step 7) ----

func TestFreezeAllActsOnTheRunningOnesSaysWhatItSkippedAndOffersTheUndo(t *testing.T) {
	h := newHarness(t)
	mustExit(t, h, 0, "freeze", "--all")
	want := "Skipped — not currently RUNNING: zulu mike kilo\n" +
		"2 container(s) being FROZEN:\n" + tableHead +
		tableRow("alpha", "RUNNING", "lxcbr0", "192.0.2.12") +
		tableRow("bravo", "RUNNING", "virbr1", "198.51.100.5") +
		"  ✓ alpha\n  ✓ bravo\n" +
		"\n  Thaw them with: lxcfreeze thaw alpha bravo\n" +
		"\n" + Config().FreezeNote + "\n"
	if h.errb.String() != want || h.out.Len() != 0 {
		t.Fatalf("stderr %q stdout %q", h.errb, h.out)
	}
	eqStrings(t, "acts", h.acts(), []string{"freeze alpha", "freeze bravo"})
}

// Bug 6: root is readied before the load AND before the act batch, so a menu left idle past
// sudo's timestamp is asked about with an explanation, never silently.
func TestSudoIsReadiedBeforeTheLoadAndBeforeTheActBatch(t *testing.T) {
	h := newHarness(t)
	mustExit(t, h, 0, "freeze", "--all")
	if n := h.count("sudo -n -v"); n != 2 {
		t.Fatalf("%d readiness checks, want 2 (load, act batch)", n)
	}
	for _, c := range h.fake.Calls() {
		if c.Name != "sudo" || c.Args[0] != "-n" {
			t.Fatalf("every command is sudo -n: %s", c.Line())
		}
	}
}

func TestThawAllUnfreezesThenRenewsTheLeaseOfEachAndOffersNoHint(t *testing.T) {
	h := newHarness(t)
	mustExit(t, h, 0, "thaw", "--all")
	eqStrings(t, "acts", h.acts(), []string{"unfreeze zulu", "unfreeze mike", "unfreeze kilo"})
	if h.count("sudo -n lxc-attach -n kilo -- nmcli device connect eth0") != 1 {
		t.Fatalf("each thaw renews the lease: %v", h.fake.Calls())
	}
	if strings.Contains(h.errb.String(), "Thaw them with") {
		t.Fatal("a thaw prints no hint (bug 7)")
	}
}

func TestADerivedVerbThawsASetWithNothingRunning(t *testing.T) {
	h := newHarness(t)
	mustExit(t, h, 0, "mike", "kilo")
	eqStrings(t, "acts", h.acts(), []string{"unfreeze mike", "unfreeze kilo"})
	h = newHarness(t)
	mustExit(t, h, 0, "mike", "alpha")
	eqStrings(t, "any running one means freeze", h.acts(), []string{"freeze alpha"})
}

func TestBridgeTargetActsOnItsMembersOnly(t *testing.T) {
	h := newHarness(t)
	mustExit(t, h, 0, "thaw", "--bridge", "virbr1")
	eqStrings(t, "acts", h.acts(), []string{"unfreeze mike"})
	h = newHarness(t)
	mustExit(t, h, 0, "freeze", "--bridge", "lxcbr0")
	eqStrings(t, "acts", h.acts(), []string{"freeze alpha"})
}

func TestDryRunPrintsTheResolvedSetOnStdoutAndChangesNothing(t *testing.T) {
	h := newHarness(t)
	mustExit(t, h, 0, "freeze", "--bridge", "lxcbr0", "-n")
	want := "DRY RUN — 1 container(s) would be FROZEN:\n" + tableHead + tableRow("alpha", "RUNNING", "lxcbr0", "192.0.2.12")
	if h.out.String() != want {
		t.Fatalf("stdout %q", h.out)
	}
	if len(h.acts()) != 0 || h.count("sudo -n -v") != 1 {
		t.Fatalf("a dry run acts on nothing and readies root only to read: %v", h.acts())
	}
	if !strings.Contains(h.errb.String(), "Skipped — not currently RUNNING: zulu") {
		t.Fatalf("the skipped notes still go to stderr: %q", h.errb)
	}
}

func TestNamesAreActedOnOnceEvenWhenGivenTwice(t *testing.T) {
	h := newHarness(t)
	mustExit(t, h, 0, "freeze", "alpha", "alpha")
	eqStrings(t, "bug 4: one freeze, not two", h.acts(), []string{"freeze alpha"})
}

func TestAnUnknownNameIsFatalAndActsOnNothing(t *testing.T) {
	h := newHarness(t)
	mustExit(t, h, 1, "freeze", "alpha", "nosuch")
	want := "lxcfreeze: not a running or frozen container: nosuch\n  Run 'lxcfreeze list' to see what is available.\n" +
		"  A STOPPED container is not listed: it can be neither frozen nor thawed.\n"
	if h.errb.String() != want || len(h.acts()) != 0 {
		t.Fatalf("stderr %q acts %v", h.errb, h.acts())
	}
}

func TestNothingToDoIsSuccess(t *testing.T) {
	h := newHarness(t)
	mustExit(t, h, 0, "thaw", "alpha")
	want := "Skipped — not currently FROZEN: alpha\nNothing to do — every selected container is already in the requested state.\n"
	if h.errb.String() != want || len(h.acts()) != 0 {
		t.Fatalf("stderr %q", h.errb)
	}
	if h.count("sudo -n -v") != 1 {
		t.Fatal("nothing to do asks for no root beyond the load")
	}
}

func TestAVanishedBridgeEndsAOneShotRunWithTheMenusWords(t *testing.T) {
	h := newHarness(t)
	mustExit(t, h, 1, "freeze", "--bridge", "nosuchbr")
	want := "lxcfreeze: no running or frozen container is on bridge 'nosuchbr'.\n  In use right now: lxcbr0 virbr1\n"
	if h.errb.String() != want || len(h.acts()) != 0 {
		t.Fatalf("got %q", h.errb)
	}
	h = newHarness(t)
	script(h.fake, []box{{name: "x", info: stateLine("RUNNING"), config: "lxc.uts.name = x\n"}})
	mustExit(t, h, 1, "--bridge", "lxcbr0")
	if h.errb.String() != "lxcfreeze: no running or frozen container is on any bridge.\n" {
		t.Fatalf("got %q", h.errb)
	}
}

func TestAFailedContainerIsNamedWithLXCsWordsAndExitsOne(t *testing.T) {
	h := newHarness(t)
	h.fake.On([]string{"sudo", "-n", "lxc-unfreeze", "-n", "mike"}, execx.Result{Stderr: []byte("lxc-unfreeze: mike: cgroup freezer unavailable\n"), ExitCode: 1})
	mustExit(t, h, 1, "thaw", "--all")
	text := h.errb.String()
	for _, want := range []string{
		"  ✓ zulu\n", "  ✗ mike — lxc-unfreeze: mike: cgroup freezer unavailable\n", "  ✓ kilo\n",
		"lxcfreeze: 1 of 3 container(s) failed — see above.\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %q", want, text)
		}
	}
	if h.count("sudo -n lxc-attach -n mike -- nmcli device connect eth0") != 0 {
		t.Fatal("no renewal for a container that did not thaw")
	}
}

// Decision D6, kept: a thaw whose DHCP renewal fails reports a failure and exits 1, though the
// container IS thawed.
func TestAThawWhoseRenewalFailedCountsAsAFailureAndSaysItIsThawed(t *testing.T) {
	h := newHarness(t)
	h.fake.On([]string{"sudo", "-n", "lxc-attach", "-n", "kilo", "--", "nmcli", "-t", "-f"}, execx.Result{Stdout: []byte("lo:loopback\n")})
	mustExit(t, h, 1, "thaw", "--all")
	want := "  ✗ kilo — thawed, but the DHCP lease was not renewed — NetworkManager manages no ethernet device\n"
	if !strings.Contains(h.errb.String(), want) || !strings.Contains(h.errb.String(), "1 of 3 container(s) failed") {
		t.Fatalf("got %q", h.errb)
	}
}

// Bug 7: a freeze where every target failed must not print a thaw hint or the reconnect note.
func TestAFreezeThatFailedEverywhereHintsNothing(t *testing.T) {
	h := newHarness(t)
	h.fake.On([]string{"sudo", "-n", "lxc-freeze", "-n", "alpha"}, execx.Result{Stderr: []byte("boom\n"), ExitCode: 1})
	mustExit(t, h, 1, "freeze", "alpha")
	if strings.Contains(h.errb.String(), "Thaw them with") || strings.Contains(h.errb.String(), "Reconnect") {
		t.Fatalf("got %q", h.errb)
	}
}

func TestAnEmptySelectionIsAnErrorWithTheNextStep(t *testing.T) {
	h := newHarness(t)
	script(h.fake, nil)
	mustExit(t, h, 1, "--all")
	want := "lxcfreeze: nothing matched — the selection is empty.\n  Run 'lxcfreeze list' to see what is running and what is frozen.\n"
	if h.errb.String() != want {
		t.Fatalf("got %q", h.errb)
	}
}

// A sudo credential that lapsed between the load and the act batch stops the batch before any
// container is touched, and says so.
func TestASudoThatLapsedBeforeTheActBatchTouchesNothing(t *testing.T) {
	h := newHarness(t)
	var probes int
	h.fake.OnFunc([]string{"sudo", "-n", "-v"}, func(execx.Cmd) (execx.Result, error) {
		probes++
		if probes == 1 {
			return execx.Result{}, nil
		}
		return execx.Result{Stderr: []byte("sudo: a password is required\n"), ExitCode: 1}, nil
	})
	mustExit(t, h, 1, "freeze", "--all")
	if len(h.acts()) != 0 || !strings.Contains(h.errb.String(), "sudo was refused, so nothing was frozen or thawed.") {
		t.Fatalf("acts %v, stderr %q", h.acts(), h.errb)
	}
}

// ---- the interactive session ----

// scriptedChooser answers the menu without a terminal: a list of group keys ("" quits) and the
// member choices for the groups opened.
type scriptedChooser struct {
	keys    []string
	members []freeze.Choice
	labels  [][]string
	screens []freeze.MemberScreen
}

func (s *scriptedChooser) Group(_ context.Context, keys, labels []string) (string, error) {
	s.labels = append(s.labels, labels)
	if len(s.keys) == 0 || s.keys[0] == "" {
		return "", freeze.ErrQuit
	}
	key := s.keys[0]
	s.keys = s.keys[1:]
	if !slices.Contains(keys, key) && !strings.HasSuffix(key, ":gone") {
		return "", fmt.Errorf("scripted chooser: %q is not a row of %v", key, keys)
	}
	return key, nil
}

func (s *scriptedChooser) Members(_ context.Context, screen freeze.MemberScreen) (freeze.Choice, error) {
	s.screens = append(s.screens, screen)
	if len(s.members) == 0 {
		return freeze.Choice{}, freeze.ErrBack
	}
	c := s.members[0]
	s.members = s.members[1:]
	return c, nil
}

func TestTheMenuLoadsOnceForItsFirstPassAndQuitsCleanly(t *testing.T) {
	h := newHarness(t)
	h.app.Chooser = &scriptedChooser{}
	mustExit(t, h, 0)
	// Bug 26: the bash loaded twice before the first menu.
	if h.count("sudo -n lxc-ls -1") != 1 {
		t.Fatalf("one load: %d lxc-ls", h.count("sudo -n lxc-ls -1"))
	}
}

// Bug 25: the bash announced a real unreadable container twice on start.
func TestTheUnreadableNoteIsAnnouncedOncePerLoadNotTwiceOnStart(t *testing.T) {
	h := newHarness(t)
	boxes := fixtureBoxes()
	boxes[1].infoFail = true
	script(h.fake, boxes)
	h.app.Chooser = &scriptedChooser{}
	mustExit(t, h, 0)
	if n := strings.Count(h.errb.String(), "NOTE — 1 container(s) could not be read"); n != 1 {
		t.Fatalf("%d notes in %q", n, h.errb)
	}
	// ...and an act that sends the menu round again loads again, so the machine is described
	// afresh: a second note, because the second load found it again.
	h = newHarness(t)
	script(h.fake, boxes)
	h.app.Chooser = &scriptedChooser{keys: []string{"all", ""}, members: []freeze.Choice{{All: true}}}
	mustExit(t, h, 0)
	if n := strings.Count(h.errb.String(), "NOTE — 1 container(s) could not be read"); n != 2 {
		t.Fatalf("%d notes in %q", n, h.errb)
	}
}

func TestTheMenuActsOnAGroupThenRepromptsWithAFreshLoadAndFreshRoot(t *testing.T) {
	h := newHarness(t)
	h.app.Chooser = &scriptedChooser{keys: []string{"bridge:lxcbr0", ""}, members: []freeze.Choice{{All: true}}}
	mustExit(t, h, 0)
	eqStrings(t, "acts", h.acts(), []string{"freeze alpha"})
	// load, act batch, load: root is readied before each.
	if h.count("sudo -n -v") != 3 || h.count("sudo -n lxc-ls -1") != 2 {
		t.Fatalf("readied %d times, listed %d", h.count("sudo -n -v"), h.count("sudo -n lxc-ls -1"))
	}
	if !strings.HasSuffix(h.errb.String(), "\n\n") {
		t.Fatalf("a blank line separates passes: %q", h.errb)
	}
}

func TestTheMenuLabelsSayWhatChoosingEachRowDoes(t *testing.T) {
	h := newHarness(t)
	sc := &scriptedChooser{}
	h.app.Chooser = sc
	mustExit(t, h, 0)
	want := []string{
		menuLabel("all LXC containers", "FREEZE 2"),
		menuLabel("bridge: lxcbr0", "FREEZE 1"),
		menuLabel("bridge: virbr1", "FREEZE 1"),
	}
	eqStrings(t, "labels", sc.labels[0], want)
}

func TestAnExplicitVerbDrivesEveryMenuLabel(t *testing.T) {
	h := newHarness(t)
	sc := &scriptedChooser{}
	h.app.Chooser = sc
	mustExit(t, h, 0, "thaw")
	want := []string{
		menuLabel("all LXC containers", "THAW   3"),
		menuLabel("bridge: lxcbr0", "THAW   1"),
		menuLabel("bridge: virbr1", "THAW   1"),
	}
	eqStrings(t, "labels", sc.labels[0], want)
}

func TestDryRunIntoTheMenuPreviewsEachPick(t *testing.T) {
	// Bug 21, kept: -n with no target enters the menu and previews rather than acts.
	h := newHarness(t)
	h.app.Chooser = &scriptedChooser{keys: []string{"all", ""}, members: []freeze.Choice{{All: true}}}
	mustExit(t, h, 0, "-n")
	if !strings.HasPrefix(h.out.String(), "DRY RUN — 2 container(s) would be FROZEN:") || len(h.acts()) != 0 {
		t.Fatalf("stdout %q acts %v", h.out, h.acts())
	}
}

func TestAnEmptyMachineEndsTheMenuWithAMessage(t *testing.T) {
	h := newHarness(t)
	script(h.fake, nil)
	h.app.Chooser = &scriptedChooser{}
	mustExit(t, h, 0)
	if h.errb.String() != "No running or frozen containers.\n" {
		t.Fatalf("got %q", h.errb)
	}
}

func TestAFailedActInTheMenuIsCarriedToTheExitStatus(t *testing.T) {
	h := newHarness(t)
	h.fake.On([]string{"sudo", "-n", "lxc-freeze", "-n", "alpha"}, execx.Result{Stderr: []byte("boom\n"), ExitCode: 1})
	h.app.Chooser = &scriptedChooser{keys: []string{"all", ""}, members: []freeze.Choice{{Names: []string{"alpha"}}}}
	mustExit(t, h, 1)
}

// Bug 5 (here for the bridge axis): a bridge that vanished between the menu being drawn and the
// row chosen re-prompts, with the reason said.
func TestABridgeThatVanishedBetweenDrawAndChoiceRepromptsInsteadOfEnding(t *testing.T) {
	h := newHarness(t)
	h.app.Chooser = &scriptedChooser{keys: []string{"bridge:gone", ""}}
	mustExit(t, h, 0)
	want := "lxcfreeze: no running or frozen container is on bridge 'gone'.\n  In use right now: lxcbr0 virbr1\n\n"
	if h.errb.String() != want {
		t.Fatalf("got %q", h.errb)
	}
}

// ---- the real chooser: numbered prompts on /dev/tty, no fzf ----

func TestNumberedMenuQuitsWithQ(t *testing.T) {
	h := newHarness(t)
	h.withTTY("q\n")
	mustExit(t, h, 0)
	text := h.errb.String()
	for _, want := range []string{
		"What to act on:\n",
		"   1) " + menuLabel("all LXC containers", "FREEZE 2") + "\n",
		"   3) " + menuLabel("bridge: virbr1", "FREEZE 1") + "\n",
		"Choice [1-3, q to quit]: ",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %q", want, text)
		}
	}
}

func TestNumberedMenuActsOnTheChosenGroupAndThenTheWholeOfIt(t *testing.T) {
	h := newHarness(t)
	h.withTTY("3\n\nq\n") // row 3 is "bridge: virbr1"; ENTER acts on all of it; q quits
	mustExit(t, h, 0)
	eqStrings(t, "acts", h.acts(), []string{"freeze bravo"})
	if !strings.Contains(h.errb.String(), "In this group:\n") || !strings.Contains(h.errb.String(), "Choice [ENTER or 1 for all, numbers for specific, b to go back]: ") {
		t.Fatalf("got %q", h.errb)
	}
}

func TestNumberedMenuDrillsDownToSpecificContainers(t *testing.T) {
	h := newHarness(t)
	h.withTTY("1\n3,4\nq\n")
	mustExit(t, h, 0)
	// The drill-down of "all": 1 is the all row, 2 zulu (frozen), 3 mike (frozen), 4 alpha
	// (running), 5 kilo, 6 bravo. Picking 3 and 4: alpha is running, so the verb is freeze, and
	// mike (not running) is skipped.
	eqStrings(t, "acts", h.acts(), []string{"freeze alpha"})
	mustContain(t, "mike is skipped", h.errb.String(), "Skipped — not currently RUNNING: mike")
}

func TestNumberedMenuGivesUpAfterThreeBadRepliesAndChangesNothing(t *testing.T) {
	h := newHarness(t)
	h.withTTY("x\n0\n99\n")
	mustExit(t, h, 1)
	text := h.errb.String()
	if strings.Count(text, "Not one of the options — enter a number between 1 and 3, or q.") != 3 ||
		!strings.HasSuffix(text, "lxcfreeze: giving up after 3 attempts — nothing changed.\n") || len(h.acts()) != 0 {
		t.Fatalf("got %q", text)
	}
}

func TestNumberedMenuEOFIsAQuit(t *testing.T) {
	h := newHarness(t)
	h.withTTY("")
	mustExit(t, h, 0)
	if !strings.HasSuffix(h.errb.String(), "Choice [1-3, q to quit]: \n") {
		t.Fatalf("EOF ends the prompt's line: %q", h.errb)
	}
}

func TestNoTerminalIsANamedFailureAndNeverAGuess(t *testing.T) {
	want := "lxcfreeze: no terminal to ask on — name a target explicitly: NAME..., --bridge BR,\n" +
		"  or --all.\n  See 'lxcfreeze --help'.\n"
	h := newHarness(t) // stderr is not a terminal
	mustExit(t, h, 1)
	if h.errb.String() != want || len(h.acts()) != 0 {
		t.Fatalf("got %q", h.errb)
	}
	// Bug 12: a terminal on stderr but no controlling /dev/tty is the same named failure, not a
	// silent exit 0.
	h = newHarness(t)
	h.app.Env.IsTerminal = func(any) bool { return true }
	mustExit(t, h, 1)
	if h.errb.String() != want {
		t.Fatalf("got %q", h.errb)
	}
}

func TestCtrlCAtANumberedPromptExits130(t *testing.T) {
	h := newHarness(t)
	reader, writer := io.Pipe()
	t.Cleanup(func() {
		if err := errors.Join(reader.Close(), writer.Close()); err != nil {
			t.Error(err)
		}
	})
	h.app.Env.IsTerminal = func(any) bool { return true }
	h.app.Env.OpenTTY = func() (io.ReadCloser, error) { return reader, nil }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan term.ExitCode, 1)
	go func() { done <- h.app.RunContext(ctx, nil) }()
	time.AfterFunc(50*time.Millisecond, cancel)
	select {
	case code := <-done:
		if code != 130 {
			t.Fatalf("exit %d, want 130; stderr %q", code, h.errb)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Ctrl-C did not reach the prompt")
	}
}
