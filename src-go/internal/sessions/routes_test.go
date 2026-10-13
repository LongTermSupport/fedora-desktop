package sessions

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// containerRoute scripts the ccy session ccy-c (pane pid 500) whose process tree holds a podman client
// for the container c_yolo.
func containerRoute(h *harness) {
	h.tmuxOut("list-panes", "ccy-c 500\n")
	h.fake.On([]string{"ps"}, execx.Result{Stdout: []byte("500 1 bash\n600 500 podman run --rm --name c_yolo claude-yolo:latest\n")})
}

func TestCheckProjectsGroupsSessionsByProjectInListingOrder(t *testing.T) {
	h := newHarness(t)
	a, b := h.project("a", true), h.project("b", true)
	h.okCLI(a)
	h.okCLI(b)
	h.sessions("cc-a 1 "+a, "cc-a-2 0 "+a, "cc-b 0 "+b)
	h.tmuxOut("list-panes", "")
	h.fake.On([]string{"ps"}, execx.Result{})
	projects, err := h.app.checkProjects(context.Background(), "reboot-warning")
	if err != nil || len(projects) != 2 {
		t.Fatalf("%v %v", projects, err)
	}
	if projects[0].Dir != a || strings.Join(projects[0].Sessions, ",") != "cc-a,cc-a-2" || projects[1].Dir != b {
		t.Fatalf("projects %+v", projects)
	}
	if h.count(cli(a)+" signal --help") != 1 || h.count(cli(b)+" signal --help") != 1 {
		t.Fatalf("each CLI is asked for signal --help once: %v", h.ran())
	}
}

func TestCheckProjectsWithNoSessionsIsNoProjects(t *testing.T) {
	h := newHarness(t)
	h.sessions()
	projects, err := h.app.checkProjects(context.Background(), "reboot-warning")
	if err != nil || projects != nil {
		t.Fatalf("%v %v", projects, err)
	}
	if h.count("ps") != 0 {
		t.Fatal("no sessions means no process table is read")
	}
}

func TestCheckProjectsRefusesAListThatCannotBeRead(t *testing.T) {
	h := newHarness(t)
	h.tmuxFail("list-sessions", "lost server")
	_, err := h.app.checkProjects(context.Background(), "reboot-warning")
	if !errors.Is(err, errReported) {
		t.Fatalf("got %v", err)
	}
	contains(t, "list failure", h.stderr(), "tmux list-sessions failed",
		"the live session list could not be read, so nothing is signalled and nothing may reboot.")
}

func TestCheckProjectsRefusesWhenContainersCannotBeMatched(t *testing.T) {
	h := newHarness(t)
	h.sessions("cc-a 1 /srv/a")
	h.tmuxFail("list-panes", "lost server")
	if _, err := h.app.checkProjects(context.Background(), "reboot-warning"); !errors.Is(err, errReported) {
		t.Fatalf("panes: %v", err)
	}
	contains(t, "panes", h.stderr(), "tmux list-panes failed",
		"the live sessions could not be matched to their containers, so nothing is signalled and nothing may reboot.")

	h = newHarness(t)
	h.sessions("cc-a 1 /srv/a")
	h.tmuxOut("list-panes", "")
	h.fake.On([]string{"ps"}, execx.Result{ExitCode: 1, Stderr: []byte("ps: gone")})
	if _, err := h.app.checkProjects(context.Background(), "reboot-warning"); !errors.Is(err, errReported) {
		t.Fatalf("ps: %v", err)
	}
	contains(t, "ps", h.stderr(), "the process table could not be read, so no session can be matched to its container: ps: gone")

	h = newHarness(t)
	h.sessions("cc-a 1 /srv/a")
	h.tmuxOut("list-panes", "")
	h.fake.On([]string{"ps"}, execx.Result{Stdout: []byte("not a table line\n")})
	if _, err := h.app.checkProjects(context.Background(), "reboot-warning"); !errors.Is(err, errReported) {
		t.Fatalf("garbled ps: %v", err)
	}
}

func TestACcySessionWithNoContainerIsLeftOutNotRefused(t *testing.T) {
	h := newHarness(t)
	a, broken := h.project("a", true), h.project("broken", true)
	h.okCLI(a)
	h.sessions("cc-a 1 "+a, "ccy-s 0 "+broken)
	h.tmuxOut("list-panes", "")
	h.fake.On([]string{"ps"}, execx.Result{})
	projects, err := h.app.checkProjects(context.Background(), "reboot-warning")
	if err != nil || len(projects) != 1 || projects[0].Dir != a {
		t.Fatalf("%+v %v", projects, err)
	}
	contains(t, "left out", h.stderr(), "Left out: no container was found in these ccy sessions' process trees",
		"  ccy-s  in "+broken)
	if h.count(cli(broken)) != 0 {
		t.Fatal("a left-out session's CLI is never asked anything")
	}
}

func TestAProjectWithACcySessionIsReachedInItsContainer(t *testing.T) {
	h := newHarness(t)
	a, ccyOnly := h.project("a", true), h.project("ccy-only", true)
	h.okCLI(a)
	h.sessions("cc-a 1 "+a, "ccy-c 0 "+ccyOnly)
	containerRoute(h)
	h.fake.On([]string{"podman", "exec"}, execx.Result{})
	projects, err := h.app.checkProjects(context.Background(), "reboot-warning")
	if err != nil || len(projects) != 2 {
		t.Fatalf("%+v %v", projects, err)
	}
	r := projects[1].route()
	if strings.Join(r.cmd, " ") != "podman exec c_yolo /workspace/.claude/hooks-daemon/bin/hooks-daemon" ||
		r.root != "/workspace" || r.via != " (in container c_yolo)" {
		t.Fatalf("route %+v", r)
	}
	if h.count("podman exec c_yolo /workspace/.claude/hooks-daemon/bin/hooks-daemon signal --help") != 1 {
		t.Fatalf("the probe goes through the container: %v", h.ran())
	}
	hostRoute := projects[0].route()
	if hostRoute.cmd[0] != cli(a) || hostRoute.root != a || hostRoute.via != "" {
		t.Fatalf("host route %+v", hostRoute)
	}
}

func TestMissingAndBrokenCLIsAreListedTogetherWithWhatToDo(t *testing.T) {
	h := newHarness(t)
	a, none, none2, broken := h.project("a", true), h.project("none", false), h.project("none2", false), h.project("broken", true)
	h.okCLI(a)
	h.onCLI(broken, func(execx.Cmd) execx.Result {
		return execx.Result{ExitCode: 1, Stderr: []byte("resolve_venv: no usable venv found\nsecond line\n")}
	})
	h.sessions("cc-a 1 "+a, "cc-x 0 "+none, "cc-x-2 0 "+none, "cc-z 0 "+none2, "cc-y 0 "+broken)
	h.tmuxOut("list-panes", "")
	h.fake.On([]string{"ps"}, execx.Result{})
	if _, err := h.app.checkProjects(context.Background(), "reboot-warning"); !errors.Is(err, errReported) {
		t.Fatalf("got %v", err)
	}
	got := h.stderr()
	contains(t, "report", got,
		"ERROR: these live sessions cannot be warned: their project has no hooks-daemon CLI.\n",
		"  cc-x  in "+none+"  (expected "+cli(none)+")\n",
		"  cc-x-2  in "+none+"  (expected "+cli(none)+")\n",
		"  cc-z  in "+none2+"  (expected "+cli(none2)+")\n",
		"ERROR: these live sessions cannot be warned: their project's hooks-daemon CLI cannot run.\n",
		"  cc-y  in "+broken+": its hooks-daemon CLI cannot run:\n      resolve_venv: no usable venv found\n      second line\n",
		"For each session above, either end it (run ccy-sessions and press Ctrl-X on it), or install or repair the daemon in its project (the hooks-daemon skill, 'install'). Nothing was signalled.\n")
	lacks(t, "report", got, "  cc-a ")
	if h.count(cli(a)+" signal reboot") != 0 {
		t.Fatal("nothing is signalled")
	}
}

func TestABrokenContainerCLIIsNamedWithItsContainer(t *testing.T) {
	h := newHarness(t)
	ccyOnly := h.project("ccy-only", true)
	h.sessions("ccy-c 0 " + ccyOnly)
	containerRoute(h)
	h.fake.On([]string{"podman", "exec"}, execx.Result{ExitCode: 1, Stderr: []byte("exec: no usable venv in the container\n")})
	if _, err := h.app.checkProjects(context.Background(), "reboot-warning"); !errors.Is(err, errReported) {
		t.Fatalf("got %v", err)
	}
	contains(t, "container", h.stderr(), "  ccy-c  in "+ccyOnly+" (in container c_yolo): its hooks-daemon CLI cannot run:",
		"      exec: no usable venv in the container")
}

func TestAnEngineThatCannotStartIsABrokenCLI(t *testing.T) {
	h := newHarness(t)
	ccyOnly := h.project("ccy-only", true)
	h.sessions("ccy-c 0 " + ccyOnly)
	containerRoute(h)
	h.fake.OnError([]string{"podman", "exec"}, &execx.NotInstalledError{Name: "podman"})
	if _, err := h.app.checkProjects(context.Background(), "reboot-warning"); !errors.Is(err, errReported) {
		t.Fatalf("got %v", err)
	}
	contains(t, "engine", h.stderr(), "      podman is not installed")
}

func TestAWithdrawalDoesNotAskTheCLIAnything(t *testing.T) {
	h := newHarness(t)
	a, none := h.project("a", true), h.project("none", false)
	h.sessions("cc-a 1 "+a, "cc-x 0 "+none)
	h.tmuxOut("list-panes", "")
	h.fake.On([]string{"ps"}, execx.Result{})
	if _, err := h.app.checkProjects(context.Background(), "reboot-cancelled"); !errors.Is(err, errReported) {
		t.Fatalf("a missing CLI still refuses a withdrawal's check: %v", err)
	}
	if h.count(cli(a)) != 0 {
		t.Fatal("a withdrawal skips the signal --help probe")
	}
}

func TestDeliverSignalOrdersArgvAndCountsProjects(t *testing.T) {
	h := newHarness(t)
	a, b := h.project("a", true), h.project("b", true)
	var seen []string
	rec := func(dir string) {
		h.onCLI(dir, func(c execx.Cmd) execx.Result {
			seen = append(seen, dir+" "+strings.Join(c.Args, " "))
			return execx.Result{Stdout: []byte("daemon says hello\n")}
		})
	}
	rec(a)
	rec(b)
	projects := []projectInfo{{Dir: a}, {Dir: b}}
	n, err := h.app.deliverSignal(context.Background(), projects, "reboot-warning", 5, false)
	if err != nil || n != 2 {
		t.Fatalf("%d %v", n, err)
	}
	want := []string{
		a + " signal reboot-warning --minutes 5 --all-sessions --project-root " + a,
		b + " signal reboot-warning --minutes 5 --all-sessions --project-root " + b,
	}
	if strings.Join(seen, "|") != strings.Join(want, "|") {
		t.Fatalf("seen %v", seen)
	}
	contains(t, "progress", h.stderr(), "signalled reboot-warning (5 min) to "+a+"\n", "signalled reboot-warning (5 min) to "+b+"\n")
	// Spec bug 9.3: the daemon CLI's own stdout must not mix into the tool's payload.
	if h.stdout() != "" {
		t.Fatalf("stdout %q", h.stdout())
	}
	contains(t, "child output", h.stderr(), "daemon says hello")
}

func TestDeliverSignalDryRunSignalsNothing(t *testing.T) {
	h := newHarness(t)
	projects := []projectInfo{{Dir: "/srv/a"}, {Dir: "/srv/b", Engine: "podman", Container: "c_yolo"}}
	n, err := h.app.deliverSignal(context.Background(), projects, "reboot-warning", 2, true)
	if err != nil || n != 2 || len(h.ran()) != 0 {
		t.Fatalf("%d %v %v", n, err, h.ran())
	}
	contains(t, "dry run", h.stderr(), "would signal reboot-warning --minutes 2 to /srv/a\n",
		"would signal reboot-warning --minutes 2 to /srv/b (in container c_yolo)\n")
}

func TestDeliverSignalWithNoProjectsSaysSoAndCountsZero(t *testing.T) {
	h := newHarness(t)
	n, err := h.app.deliverSignal(context.Background(), nil, "reboot-warning", 5, false)
	if err != nil || n != 0 {
		t.Fatalf("%d %v", n, err)
	}
	contains(t, "none", h.stderr(), "No CCY or cc sessions need warning; nothing to signal.\n")
}

func TestDeliverSignalStopsAtTheFirstRefusedWarningButNotAWithdrawal(t *testing.T) {
	h := newHarness(t)
	a, b := h.project("a", true), h.project("b", true)
	h.onCLI(a, func(execx.Cmd) execx.Result { return execx.Result{ExitCode: 1} })
	h.okCLI(b)
	projects := []projectInfo{{Dir: a}, {Dir: b}}
	if _, err := h.app.deliverSignal(context.Background(), projects, "reboot-warning", 3, false); !errors.Is(err, errReported) {
		t.Fatalf("got %v", err)
	}
	contains(t, "warning", h.stderr(), "ERROR: signalling reboot-warning to "+a+" failed.")
	if h.count(cli(b)) != 0 {
		t.Fatal("a warning stops at the first project that refuses")
	}
	if _, err := h.app.deliverSignal(context.Background(), projects, "reboot-cancelled", 0, false); !errors.Is(err, errReported) {
		t.Fatalf("a withdrawal that failed anywhere still fails: %v", err)
	}
	if h.count(cli(b)+" signal reboot-cancelled --all-sessions --project-root "+b) != 1 {
		t.Fatalf("a withdrawal reaches the next project: %v", h.ran())
	}
}

func TestSignalProjectsChecksBeforeItSignals(t *testing.T) {
	h := newHarness(t)
	a, none := h.project("a", true), h.project("none", false)
	h.okCLI(a)
	h.sessions("cc-a 1 "+a, "cc-x 0 "+none)
	h.tmuxOut("list-panes", "")
	h.fake.On([]string{"ps"}, execx.Result{})
	if _, err := h.app.signalProjects(context.Background(), "reboot-warning", 5, false); !errors.Is(err, errReported) {
		t.Fatalf("got %v", err)
	}
	if h.count(cli(a)+" signal reboot") != 0 {
		t.Fatal("no project is signalled when another refuses")
	}
	h = newHarness(t)
	h.okCLI(a)
	h.sessions("cc-a 1 " + a)
	h.tmuxOut("list-panes", "")
	h.fake.On([]string{"ps"}, execx.Result{})
	n, err := h.app.signalProjects(context.Background(), "reboot-warning", 5, false)
	if err != nil || n != 1 {
		t.Fatalf("%d %v", n, err)
	}
}
