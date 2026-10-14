package podfreeze

// The command line end to end (spec 2 and 9.3): neither bash suite ran a tool as a command line,
// so every case here is new. The harness runs App.RunContext over an execx.Fake primed with
// the fixture's podman answers, a scripted chooser or a scripted /dev/tty, and buffers for the
// two streams, so the bytes on stdout and stderr and the exit status are all asserted.

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
)

type harness struct {
	t    *testing.T
	app  *App
	fake *execx.Fake
	out  *bytes.Buffer
	errb *bytes.Buffer
}

// newHarness is a host (no container markers), no terminal, podman installed, fzf absent, and
// the fixture machine behind the fake. Tests override what they exercise.
func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, fake: scripted(fixturePS, fixtureLabels, fixtureNetworks), out: &bytes.Buffer{}, errb: &bytes.Buffer{}}
	dir := t.TempDir()
	env := freeze.NewEnv(h.out, h.errb, h.fake)
	env.ContainerEnvPath = filepath.Join(dir, "containerenv")
	env.DockerEnvPath = filepath.Join(dir, "dockerenv")
	env.Getenv = func(string) string { return "" }
	env.LookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	env.IsTerminal = func(any) bool { return false }
	env.OpenTTY = func() (io.ReadCloser, error) { return nil, errors.New("no tty") }
	h.app = &App{Env: env}
	h.fake.OnError([]string{"fzf"}, &execx.NotInstalledError{Name: "fzf"})
	for _, c := range fixtureContainers() {
		h.fake.On([]string{"podman", "pause", c}, execx.Result{Stdout: []byte(c + "\n")})
		h.fake.On([]string{"podman", "unpause", c}, execx.Result{Stdout: []byte(c + "\n")})
	}
	return h
}

func fixtureContainers() []string {
	return []string{"zulu_yolo", "mike", "alpha_yolo_2", "kilo", "bravo_browser", "echo_yolo_3"}
}

// withTTY gives the run a terminal whose /dev/tty reads the given replies.
func (h *harness) withTTY(replies string) {
	h.app.Env.IsTerminal = func(any) bool { return true }
	h.app.Env.OpenTTY = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(replies)), nil }
}

func (h *harness) run(args ...string) int {
	h.t.Helper()
	return h.app.RunContext(context.Background(), args)
}

// podmanCalls are the podman invocations, without the trailing format arguments.
func (h *harness) podmanCalls() []string {
	var lines []string
	for _, c := range h.fake.Calls() {
		if c.Name == "podman" {
			lines = append(lines, strings.Join(c.Args[:min(len(c.Args), 3)], " "))
		}
	}
	return lines
}

func (h *harness) acts() []string {
	var acts []string
	for _, c := range h.fake.Calls() {
		if c.Name == "podman" && (c.Args[0] == "pause" || c.Args[0] == "unpause") {
			acts = append(acts, c.Args[0]+" "+c.Args[1])
		}
	}
	return acts
}

func mustExit(t *testing.T, h *harness, want int, args ...string) {
	t.Helper()
	if got := h.run(args...); got != want {
		t.Fatalf("%v: exit %d, want %d\nstdout: %s\nstderr: %s", args, got, want, h.out, h.errb)
	}
}

func tableRow(name, state, ccy, nets string) string {
	return fmt.Sprintf("  %-34s %-8s %-4s %-22s\n", name, state, ccy, nets)
}

const tableHead = "  NAME                               STATE    CCY  NETWORKS              \n"

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
			t.Fatalf("--help runs no command: %v", h.fake.Calls())
		}
	}
}

func TestHelpWorksInsideAContainerBecauseParsingPrecedesTheGuard(t *testing.T) {
	h := newHarness(t)
	h.app.Env.Getenv = func(k string) string {
		if k == "container" {
			return "podman"
		}
		return ""
	}
	mustExit(t, h, 0, "--help")
	if h.out.String() != usageText {
		t.Fatal("the usage text is printed")
	}
}

func TestNewHelpMidLineStopsParsingButAnEarlierMistakeStillDies(t *testing.T) {
	h := newHarness(t)
	mustExit(t, h, 0, "--ccy", "--help", "--all")
	if h.out.String() != usageText {
		t.Fatal("-h mid-line prints usage")
	}
	h = newHarness(t)
	mustExit(t, h, 1, "--bogus", "--help")
	if h.out.Len() != 0 {
		t.Fatal("an earlier bad option means no usage")
	}
}

// ---- usage mistakes: exit 1 (bug 10, kept), nothing run, message on stderr ----

func TestUsageMistakesExitOneWithTheMessageAndRunNothing(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"--bogus"}, "podfreeze: unknown option '--bogus' — see 'podfreeze --help'.\n"},
		{[]string{"--network"}, "podfreeze: --network needs a network name — see 'podfreeze --help'.\n"},
		{[]string{"--github"}, "podfreeze: --github needs a value — see 'podfreeze --help'.\n"},
		{[]string{"list", "thaw"}, "podfreeze: only one verb at a time — already have 'list'.\n"},
		{[]string{"--ccy", "--all"}, "podfreeze: choose one target only — '--ccy' and '--all' are mutually exclusive.\n"},
		{[]string{"--ccy", "foo"}, "podfreeze: choose one target only — '--ccy' and 'names' are mutually exclusive.\n"},
		{[]string{"--bridge", "br"}, "podfreeze: unknown option '--bridge' — see 'podfreeze --help'.\n"},
		// Bug 22: `list` with a target is refused before ANY external command (the bash loaded
		// the whole inventory first).
		{[]string{"list", "--all"}, "podfreeze: 'list' takes no target — it reports everything.\n"},
		{[]string{"list", "mike"}, "podfreeze: 'list' takes no target — it reports everything.\n"},
		{[]string{"--network", "appnet", "list"}, "podfreeze: 'list' takes no target — it reports everything.\n"},
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

// ---- the host guard and preflight ----

func TestRefusesInsideAContainerBeforeAnyCommand(t *testing.T) {
	want := "podfreeze: this is a container — run podfreeze on the HOST.\n" +
		"  The host's podman is not reachable from in here, so every container would\n" +
		"  look absent. Freezing your own session would also be unrecoverable.\n"
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

func TestNewPreflightFailureSaysHowToInstallPodmanAndRunsNothing(t *testing.T) {
	h := newHarness(t)
	h.app.Env.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	mustExit(t, h, 1, "list")
	if !strings.HasPrefix(h.errb.String(), "podfreeze: podman is not installed.\n") || len(h.fake.Calls()) != 0 {
		t.Fatalf("got %q, calls %v", h.errb, h.fake.Calls())
	}
}

// ---- list (spec 3.6) ----

func TestListShowsFrozenThenRunningOnStdoutAfterOneLoad(t *testing.T) {
	h := newHarness(t)
	mustExit(t, h, 0, "list")
	want := "=== frozen (3) ===\n" + tableHead +
		tableRow("zulu_yolo", "paused", "CCY", "podman") +
		tableRow("alpha_yolo_2", "paused", "CCY", "podman, appnet") +
		tableRow("echo_yolo_3", "paused", "CCY", "none") +
		"\n  Thaw them all with: podfreeze thaw --all\n" +
		"\n=== running (3) ===\n" + tableHead +
		tableRow("mike", "running", "-", "none") +
		tableRow("kilo", "running", "-", "appnet") +
		tableRow("bravo_browser", "running", "CCY", "podman")
	if h.out.String() != want {
		t.Fatalf("got:\n%q\nwant:\n%q", h.out, want)
	}
	if h.errb.Len() != 0 {
		t.Fatalf("list writes only stdout: %q", h.errb)
	}
	// Bug 26: one load, two podman queries; nothing else.
	eqStrings(t, "podman", h.podmanCalls(), []string{"ps --all --format", "ps --all --filter"})
}

func TestListOfAnEmptyMachine(t *testing.T) {
	h := newHarness(t)
	h.fake.On(psPrefix, execx.Result{})
	h.fake.On(labelPrefix, execx.Result{})
	mustExit(t, h, 0, "list")
	want := "=== frozen (0) ===\n  (none)\n\n=== running (0) ===\n  (none)\n"
	if h.out.String() != want {
		t.Fatalf("got %q", h.out)
	}
}

func TestListWithOnlyRunningContainersHasNoThawHint(t *testing.T) {
	h := newHarness(t)
	h.fake.On(psPrefix, execx.Result{Stdout: []byte("mike|running|none\n")})
	h.fake.On(labelPrefix, execx.Result{})
	mustExit(t, h, 0, "list")
	want := "=== frozen (0) ===\n  (none)\n\n=== running (1) ===\n" + tableHead + tableRow("mike", "running", "-", "none")
	if h.out.String() != want {
		t.Fatalf("got %q", h.out)
	}
}

func TestNewListIgnoresDryRun(t *testing.T) {
	h := newHarness(t)
	mustExit(t, h, 0, "list", "-n")
	if !strings.HasPrefix(h.out.String(), "=== frozen (3) ===") {
		t.Fatalf("got %q", h.out)
	}
}

// ---- one-shot flows (spec 2.3 step 7) ----

func TestFreezeCCYActsOnTheRunningSessionAndSaysWhatItSkipped(t *testing.T) {
	h := newHarness(t)
	mustExit(t, h, 0, "freeze", "--ccy")
	want := "Skipped — not currently running: zulu_yolo alpha_yolo_2 echo_yolo_3\n" +
		"1 container(s) being FROZEN:\n" + tableHead + tableRow("bravo_browser", "running", "CCY", "podman") +
		"  ✓ bravo_browser\n" +
		"\n  Thaw them with: podfreeze thaw bravo_browser\n"
	if h.errb.String() != want || h.out.Len() != 0 {
		t.Fatalf("stderr %q stdout %q", h.errb, h.out)
	}
	eqStrings(t, "acts", h.acts(), []string{"pause bravo_browser"})
}

func TestTheDerivedVerbTogglesAndThawActsOnEveryPausedContainer(t *testing.T) {
	h := newHarness(t)
	mustExit(t, h, 0, "thaw", "--all")
	eqStrings(t, "acts", h.acts(), []string{"unpause zulu_yolo", "unpause alpha_yolo_2", "unpause echo_yolo_3"})
	if strings.Contains(h.errb.String(), "Thaw them with") {
		t.Fatal("a thaw prints no hint (bug 7)")
	}
	h = newHarness(t)
	h.fake.On(psPrefix, execx.Result{Stdout: []byte("zulu_yolo|paused|podman\n")})
	h.fake.On(labelPrefix, execx.Result{})
	mustExit(t, h, 0, "--all")
	eqStrings(t, "an all-paused set derives thaw", h.acts(), []string{"unpause zulu_yolo"})
}

func TestDryRunPrintsTheResolvedSetOnStdoutAndChangesNothing(t *testing.T) {
	h := newHarness(t)
	mustExit(t, h, 0, "freeze", "--network", "appnet", "-n")
	want := "DRY RUN — 1 container(s) would be FROZEN:\n" + tableHead + tableRow("kilo", "running", "-", "appnet")
	if h.out.String() != want {
		t.Fatalf("stdout %q", h.out)
	}
	if len(h.acts()) != 0 {
		t.Fatalf("a dry run acts on nothing: %v", h.acts())
	}
	if !strings.Contains(h.errb.String(), "Skipped — not currently running: alpha_yolo_2") {
		t.Fatalf("the skipped notes still go to stderr: %q", h.errb)
	}
}

func TestNamesAreActedOnOnceEvenWhenGivenTwice(t *testing.T) {
	h := newHarness(t)
	mustExit(t, h, 0, "freeze", "mike", "mike")
	eqStrings(t, "bug 4: one pause, not two", h.acts(), []string{"pause mike"})
}

func TestAnUnknownNameIsFatalAndActsOnNothing(t *testing.T) {
	h := newHarness(t)
	mustExit(t, h, 1, "freeze", "mike", "nosuch")
	want := "podfreeze: not a running or frozen container: nosuch\n  Run 'podfreeze list' to see what is available.\n"
	if h.errb.String() != want || len(h.acts()) != 0 {
		t.Fatalf("stderr %q acts %v", h.errb, h.acts())
	}
}

func TestNothingToDoIsSuccess(t *testing.T) {
	h := newHarness(t)
	mustExit(t, h, 0, "thaw", "mike")
	want := "Skipped — not currently paused: mike\nNothing to do — every selected container is already in the requested state.\n"
	if h.errb.String() != want || len(h.acts()) != 0 {
		t.Fatalf("stderr %q", h.errb)
	}
}

func TestAFailedContainerIsNamedWithPodmansWordsAndExitsOne(t *testing.T) {
	h := newHarness(t)
	h.fake.On([]string{"podman", "unpause", "alpha_yolo_2"}, execx.Result{Stderr: []byte("Error: cgroup freezer unavailable\n"), ExitCode: 125})
	mustExit(t, h, 1, "thaw", "--all")
	text := h.errb.String()
	for _, want := range []string{
		"  ✓ zulu_yolo\n", "  ✗ alpha_yolo_2 — Error: cgroup freezer unavailable\n", "  ✓ echo_yolo_3\n",
		"podfreeze: 1 of 3 container(s) failed — see above.\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %q", want, text)
		}
	}
}

// Bug 7: a freeze where every target failed must not print a thaw hint for containers that are
// not frozen.
func TestNewAFreezeThatFailedEverywhereHintsNothing(t *testing.T) {
	h := newHarness(t)
	h.fake.On([]string{"podman", "pause", "mike"}, execx.Result{Stderr: []byte("boom\n"), ExitCode: 125})
	mustExit(t, h, 1, "freeze", "mike")
	if strings.Contains(h.errb.String(), "Thaw them with") {
		t.Fatalf("got %q", h.errb)
	}
}

func TestAnEmptySelectionIsAnErrorWithTheNextStep(t *testing.T) {
	h := newHarness(t)
	h.fake.On(psPrefix, execx.Result{Stdout: []byte("mike|running|none\n")})
	h.fake.On(labelPrefix, execx.Result{})
	mustExit(t, h, 1, "--ccy")
	want := "podfreeze: nothing matched — the selection is empty.\n  Run 'podfreeze list' to see what is running and what is frozen.\n"
	if h.errb.String() != want {
		t.Fatalf("got %q", h.errb)
	}
}

func TestNetworkAndIdentityTargetsEndToEnd(t *testing.T) {
	h := newHarness(t)
	mustExit(t, h, 0, "thaw", "--network", "podman")
	eqStrings(t, "network", h.acts(), []string{"unpause zulu_yolo", "unpause alpha_yolo_2"})

	h = newHarness(t)
	mustExit(t, h, 0, "thaw", "--github", "octo")
	eqStrings(t, "github", h.acts(), []string{"unpause alpha_yolo_2"})
	if !strings.HasPrefix(h.errb.String(), "podfreeze: NOTE — 1 CCY session(s) carry no identity labels,") {
		t.Fatalf("the blind-spot note comes before the action: %q", h.errb)
	}

	h = newHarness(t)
	mustExit(t, h, 0, "freeze", "--ssh-key", "key_shared")
	eqStrings(t, "ssh-key", h.acts(), []string{"pause bravo_browser"})
}

func TestAnUnknownNetworkOrIdentityValueEndsTheRunBeforeActing(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"--network", "nosuchnet"}, "podfreeze: no such network: 'nosuchnet'\n  Known networks:\n    podman\n    appnet\n    bridge\n"},
		{[]string{"--github", "ghost"}, "podfreeze: no running or frozen CCY session has --github 'ghost'\n  Known values:\n    hubot\n    octo\n"},
		// Bug 15, kept: a flag's value is the next token verbatim.
		{[]string{"--network", "-n"}, "podfreeze: no such network: '-n'\n  Known networks:\n    podman\n    appnet\n    bridge\n"},
	}
	for _, tc := range cases {
		h := newHarness(t)
		mustExit(t, h, 1, tc.args...)
		if h.errb.String() != tc.want || len(h.acts()) != 0 {
			t.Errorf("%v: stderr %q acts %v", tc.args, h.errb, h.acts())
		}
	}
}

func TestAFailedQueryIsFatalNotAnEmptyMachine(t *testing.T) {
	h := newHarness(t)
	h.fake.On(psPrefix, execx.Result{Stderr: []byte("Cannot connect to Podman socket\n"), ExitCode: 125})
	mustExit(t, h, 1, "list")
	if !strings.Contains(h.errb.String(), "podfreeze: podman ps failed: Cannot connect to Podman socket") || h.out.Len() != 0 {
		t.Fatalf("got %q", h.errb)
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
	if !slices.Contains(keys, key) {
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
	eqStrings(t, "podman", h.podmanCalls(), []string{"ps --all --format", "ps --all --filter"})
}

func TestTheMenuActsOnAGroupThenRepromptsWithAFreshLoad(t *testing.T) {
	h := newHarness(t)
	h.app.Chooser = &scriptedChooser{keys: []string{"ccy", ""}, members: []freeze.Choice{{All: true}}}
	mustExit(t, h, 0)
	eqStrings(t, "acts", h.acts(), []string{"pause bravo_browser"})
	eqStrings(t, "two passes, two loads", h.podmanCalls()[:4], []string{"ps --all --format", "ps --all --filter", "pause bravo_browser", "ps --all --format"})
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
		menuLabel("all CCY (Claude) containers", "FREEZE 1"),
		menuLabel("all containers", "FREEZE 3"),
		menuLabel("network: appnet", "FREEZE 1"),
		menuLabel("network: podman", "FREEZE 1"),
		menuLabel("github: hubot", "FREEZE 1"),
		menuLabel("github: octo", "THAW   1"),
		menuLabel("token: work", "FREEZE 1"),
		menuLabel("ssh-key: key_alpha", "THAW   1"),
		menuLabel("ssh-key: key_shared", "FREEZE 1"),
	}
	eqStrings(t, "labels", sc.labels[0], want)
}

func TestAnExplicitVerbDrivesEveryMenuLabel(t *testing.T) {
	h := newHarness(t)
	sc := &scriptedChooser{}
	h.app.Chooser = sc
	mustExit(t, h, 0, "thaw")
	if got := sc.labels[0][0]; got != menuLabel("all CCY (Claude) containers", "THAW   3") {
		t.Fatalf("got %q", got)
	}
	if got := sc.labels[0][5]; got != menuLabel("github: octo", "THAW   1") {
		t.Fatalf("got %q", got)
	}
}

func TestDryRunIntoTheMenuPreviewsEachPick(t *testing.T) {
	// Bug 21, kept: -n with no target enters the menu and previews rather than acts.
	h := newHarness(t)
	h.app.Chooser = &scriptedChooser{keys: []string{"all", ""}, members: []freeze.Choice{{All: true}}}
	mustExit(t, h, 0, "-n")
	if !strings.HasPrefix(h.out.String(), "DRY RUN — 3 container(s) would be FROZEN:") || len(h.acts()) != 0 {
		t.Fatalf("stdout %q acts %v", h.out, h.acts())
	}
}

func TestAnEmptyMachineEndsTheMenuWithAMessage(t *testing.T) {
	h := newHarness(t)
	h.fake.On(psPrefix, execx.Result{})
	h.fake.On(labelPrefix, execx.Result{})
	h.app.Chooser = &scriptedChooser{}
	mustExit(t, h, 0)
	if h.errb.String() != "No running or frozen containers.\n" {
		t.Fatalf("got %q", h.errb)
	}
}

func TestAFailedActInTheMenuIsCarriedToTheExitStatus(t *testing.T) {
	h := newHarness(t)
	h.fake.On([]string{"podman", "pause", "mike"}, execx.Result{Stderr: []byte("boom\n"), ExitCode: 125})
	h.app.Chooser = &scriptedChooser{keys: []string{"all", ""}, members: []freeze.Choice{{Names: []string{"mike"}}}}
	mustExit(t, h, 1)
}

// Bug 5: a vanished network OR identity value re-prompts from the menu.
func TestAGroupThatVanishedBetweenDrawAndChoiceRepromptsInsteadOfEnding(t *testing.T) {
	for _, key := range []string{"net:gone", "id:github:gone"} {
		h := newHarness(t)
		// A row that existed when the menu was drawn and is gone when it is chosen: the chooser
		// answers with a key the engine can no longer resolve.
		h.app.Chooser = &anyKeyChooser{keys: []string{key, ""}}
		mustExit(t, h, 0)
		if !strings.Contains(h.errb.String(), "podfreeze: no ") {
			t.Errorf("%s: the reason is said: %q", key, h.errb)
		}
	}
}

type anyKeyChooser struct{ keys []string }

func (a *anyKeyChooser) Group(context.Context, []string, []string) (string, error) {
	if a.keys[0] == "" {
		return "", freeze.ErrQuit
	}
	key := a.keys[0]
	a.keys = a.keys[1:]
	return key, nil
}

func (a *anyKeyChooser) Members(context.Context, freeze.MemberScreen) (freeze.Choice, error) {
	return freeze.Choice{}, freeze.ErrBack
}

// ---- the real chooser: numbered prompts on /dev/tty, no fzf ----

func TestNumberedMenuQuitsWithQ(t *testing.T) {
	h := newHarness(t)
	h.withTTY("q\n")
	mustExit(t, h, 0)
	text := h.errb.String()
	for _, want := range []string{
		"What to act on:\n",
		"   1) " + menuLabel("all CCY (Claude) containers", "FREEZE 1") + "\n",
		"   9) " + menuLabel("ssh-key: key_shared", "FREEZE 1") + "\n",
		"Choice [1-9, q to quit]: ",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %q", want, text)
		}
	}
}

func TestNumberedMenuActsOnTheChosenGroupAndThenTheWholeOfIt(t *testing.T) {
	h := newHarness(t)
	h.withTTY("3\n\nq\n") // row 3 is "network: appnet"; ENTER acts on all of it; q quits
	mustExit(t, h, 0)
	eqStrings(t, "acts", h.acts(), []string{"pause kilo"})
	if !strings.Contains(h.errb.String(), "In this group:\n") || !strings.Contains(h.errb.String(), "Choice [ENTER or 1 for all, numbers for specific, b to go back]: ") {
		t.Fatalf("got %q", h.errb)
	}
}

func TestNumberedMenuDrillsDownToSpecificContainers(t *testing.T) {
	h := newHarness(t)
	h.withTTY("2\n2,4\nq\n")
	mustExit(t, h, 0)
	// The drill-down of "all containers": 1 is the all row, 2 zulu_yolo (paused), 3 mike,
	// 4 alpha_yolo_2 (paused), 5 kilo, 6 bravo_browser, 7 echo_yolo_3. Picking 2 and 4: nothing
	// in {zulu, alpha} is running, so the derived verb is thaw.
	eqStrings(t, "acts", h.acts(), []string{"unpause zulu_yolo", "unpause alpha_yolo_2"})
}

func TestNumberedMenuGivesUpAfterThreeBadRepliesAndChangesNothing(t *testing.T) {
	h := newHarness(t)
	h.withTTY("x\n0\n99\n")
	mustExit(t, h, 1)
	text := h.errb.String()
	if strings.Count(text, "Not one of the options — enter a number between 1 and 9, or q.") != 3 ||
		!strings.HasSuffix(text, "podfreeze: giving up after 3 attempts — nothing changed.\n") || len(h.acts()) != 0 {
		t.Fatalf("got %q", text)
	}
}

func TestNumberedMenuEOFIsAQuit(t *testing.T) {
	h := newHarness(t)
	h.withTTY("")
	mustExit(t, h, 0)
	if !strings.HasSuffix(h.errb.String(), "Choice [1-9, q to quit]: \n") {
		t.Fatalf("EOF ends the prompt's line: %q", h.errb)
	}
}

func TestNoTerminalIsANamedFailureAndNeverAGuess(t *testing.T) {
	want := "podfreeze: no terminal to ask on — name a target explicitly: NAME..., --ccy,\n" +
		"  --network NET, or --all.\n  See 'podfreeze --help'.\n"
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

func TestNewAUsageThatCannotBeWrittenIsAFailure(t *testing.T) {
	h := newHarness(t)
	h.app.Env.Out = brokenPipe{}
	mustExit(t, h, 1, "--help")
	if !strings.Contains(h.errb.String(), "write usage") {
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
	done := make(chan int, 1)
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
