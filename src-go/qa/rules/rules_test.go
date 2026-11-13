package rules

import (
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"
)

// Each analyzer is proven alone against a fixture: every line the rule must flag carries a
// want comment, and the clean constructs beside them carry none, so a rule that fires too
// much or too little fails here. The fixtures reproduce the defects the tools actually hit.

func TestSeamExec(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), SeamExec, "fix/internal/execx", "fix/qa/internal/proc", "fix/internal/badexec")
}

func TestSeamCommands(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), SeamCommands,
		"fix/internal/execx", "fix/internal/sudo", "fix/internal/badcmd", "fix/other/internal/tmuxccy",
		"fix/internal/podfreeze", "fix/internal/lxcfreeze", "fix/internal/sessprobe", "fix/internal/badrun")
}

func TestSeamCommandsDynamicNames(t *testing.T) {
	allow := []allowance{
		{pkg: "internal/dynamic", fn: "Launch", reason: "the program is chosen at run time"},
		{pkg: "internal/dynamic", fn: "Runner.Run", reason: "the program is chosen at run time"},
	}
	analysistest.Run(t, analysistest.TestData(), newSeamCommands(allow, nil), "fix/internal/dynamic")
}

// lxccmd is the second owner of lxc-ls, lxc-info and lxc-attach. A dynamicNames row lets its
// function hand a run-time inner program to lxc-attach; without the row the finding stays, and a
// wrapper that is not inner is not excused by it.
func TestSeamCommandsInnerDynamic(t *testing.T) {
	allow := []allowance{
		{pkg: "internal/teamchat/host/lxc/lxccmd", fn: "LXC.Guest", reason: "the host-written kit binary"},
		{pkg: "internal/teamchat/host/lxc/lxccmd", fn: "LXC.Inside", reason: "the host-written kit binary"},
		{pkg: "internal/teamchat/host/lxc/lxccmd", fn: "LXC.Env", reason: "the host-written kit binary"},
	}
	analysistest.Run(t, analysistest.TestData(), newSeamCommands(allow, nil), "fix/internal/teamchat/host/lxc/lxccmd")
}

// A shellStrings row excuses its function's shell string and nothing else: the neighbour is
// flagged, and an owned program in the excused function still is.
func TestSeamCommandsShellStrings(t *testing.T) {
	allow := []allowance{
		{pkg: "internal/shellstring", fn: "Configured", reason: "a command line the user configured"},
		{pkg: "internal/shellstring", fn: "Runner.Run", reason: "a command line the user configured"},
		{pkg: "internal/shellstring", fn: "OwnedInside", reason: "a command line the user configured"},
	}
	analysistest.Run(t, analysistest.TestData(), newSeamCommands(nil, allow), "fix/internal/shellstring")
}

func TestEveryShellStringHasAReason(t *testing.T) {
	for _, a := range shellStrings {
		if a.reason == "" || a.pkg == "" || a.fn == "" {
			t.Errorf("allowance %+v needs a package, a function and a reason", a)
		}
	}
}

func TestSubmitUnchecked(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), SubmitUnchecked, "fix/sub/internal/tmuxccy", "fix/sub/badsubmit")
}

func TestTmuxTargets(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), TmuxTargets, "fix/internal/tmuxccy", "fix/internal/term")
}

func TestTestClock(t *testing.T) {
	allow := []clockAllowance{{pkg: "internal/allowedclock", fn: "TestRealWait", reason: "proves the real sleep ran"}}
	analysistest.Run(t, analysistest.TestData(), newTestClock(allow), "fix/internal/badclock", "fix/internal/allowedclock")
}

func TestHomeAbbrev(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), HomeAbbrev, "fix/internal/term", "fix/qa/rules", "fix/internal/badhome")
}

func TestSelfErasingClipboard(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), SelfErasingClipboard, "fix/qa/rules", "fix/internal/badclip")
}

func TestFakeClockCount(t *testing.T) {
	allow := []allowance{{pkg: "internal/countallowed", fn: "TestNothingWaits", reason: "claims nothing at all is armed"}}
	analysistest.Run(t, analysistest.TestData(), newFakeClockCount(allow), "fix/internal/fakeclock", "fix/internal/othertimers",
		"fix/internal/badcount", "fix/internal/countallowed")
}

func TestGoroutineEnd(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), GoroutineEnd, "fix/internal/badgo", "fix/internal/checkedgo")
}

func TestLeakcheckOnce(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), LeakcheckOnce, "fix/internal/twicecheck")
}

func TestSelfChildStderr(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), SelfChildStderr, "fix/internal/selfchild", "fix/internal/argschild", "fix/internal/fileschild", "fix/internal/otherchild")
}

func TestTestSpin(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), TestSpin, "fix/internal/spin")
}

// A condition handed to leakcheck.Poll or Drive is read twice around the idle reading, so it must
// not act: the fixture has the clock moves, sends and closes in it (a select's, a nested
// function literal's) and the reads, receives, steps and conditions held in a variable beside them.
func TestFakeRestart(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), FakeRestart, "fix/internal/badrestart")
}

func TestPollPure(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), PollPure, "fix/internal/pollpure")
}

func TestSemgrepTestRunner(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), SemgrepTestRunner, "fix/internal/badsemgreptest")
}

func TestWaitDelay(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), WaitDelay, "fix/internal/badwait")
}

func TestCommandFailureDropped(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), CommandFailureDropped, "fix/internal/execx", "fix/internal/wrapper", "fix/internal/badfailure")
}

func TestRefusalKind(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), RefusalKind, "fix/internal/declare", "fix/internal/badrefusal")
}

func TestHiddenBindings(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), HiddenBindings, "fix/internal/ui", "fix/internal/ui/notab", "fix/internal/badbind", "fix/qa/rules",
		"fix/qa/hiddenbindings", "fix/internal/badtea", "fix/internal/ui/frame")
}

// A package with no test file is flagged; a test file of its own package or of an external test
// package (x_test) satisfies the rule.
func TestPackageTests(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), PackageTests, "fix/internal/notests", "fix/internal/withtests", "fix/internal/externaltests")
}

// A test that gives a file to another user fails for an unprivileged user: the fixture has each
// way of writing the call, and the clean forms (the caller's own uid, -1, a branch or a skip on
// the user being root) beside them.
func TestTestChown(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), TestChown, "fix/internal/badchown", "fix/internal/guardedchown")
}

// A test that asserts the order of a directory listing nothing sorted fails on a runner whose
// filesystem lists the names another way: the fixture has the listing calls (a function of the
// package, a wrapper, a file's own methods, a test helper, another package's function through the
// exported fact) compared in order, and the sorted and order-free forms beside them.
func TestListingOrder(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), ListingOrder, "fix/internal/badlisting", "fix/internal/badlistingx")
}

// A directory walk lists named pipes, sockets and devices as well as files, and opening a named
// pipe blocks for ever: the fixture opens walked entries every way the sweep for #195 found (a
// listing, a glob, each walk's callback, a helper of the package, a gathered slice, a closure,
// ParseFile, an fs.FS), with the checks that do not guard (a suffix, !IsDir, a warning that falls
// through, a check of another name, a check joined by ||) and the ones that do beside them.
func TestWalkOpen(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), WalkOpen, "fix/internal/badwalk")
}

func TestZombieExit(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), ZombieExit, "fix/internal/badzombie")
}

func TestRowKind(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), RowKind, "fix/internal/teams/board", "fix/internal/badrowkind")
}

func TestChildExit(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), ChildExit, "fix/internal/term", "fix/internal/badexit")
}

// A seat session asked as Kind == KindCcy misses a cc seat: the fixture compares both ways
// round and with !=, beside IsSession, another kind and a plain string; an IsSession method,
// which defines the question, is not judged.
func TestSessionKind(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), SessionKind, "fix/internal/badsessionkind")
}

// A seat's member type fixed in the code offers a cc session ccy's seats: the fixture passes a
// literal, a named constant, "" and a folded constant, beside a type computed from the session's
// kind and one passed on; the agentbus client's own default-taking wrapper is not judged.
func TestSeatMemberType(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), SeatMemberType, "fix/internal/agentbus", "fix/internal/badmembertype")
}

// A recorded session's start decided outside conversation.StartPlan (fedora-desktop#270): the
// restore's planner as it was, a team restart's resume-only start, a builder held as a value and
// a lookup error held in a variable; the planner itself, the registry's own use, a plain lookup
// and test files are not judged, and a second function in the conversation package is.
func TestStartDecision(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), StartDecision, "fix/internal/registry", "fix/internal/conversation", "fix/internal/badstart")
}

func TestAllIsValid(t *testing.T) {
	if err := analysis.Validate(All()); err != nil {
		t.Fatal(err)
	}
	if got := len(All()); got != 48 {
		t.Fatalf("want 48 rules, got %d", got)
	}
}

func TestEveryExpectBuilderHasAReason(t *testing.T) {
	if len(expectBuilders) == 0 {
		t.Fatal("no function may build --expect: the ui seam has nowhere to declare its keys")
	}
	for _, a := range expectBuilders {
		if a.reason == "" || a.pkg == "" || a.fn == "" {
			t.Errorf("allowance %+v needs a package, a function and a reason", a)
		}
	}
}

func TestEveryDynamicNameHasAReason(t *testing.T) {
	for _, a := range dynamicNames {
		if a.reason == "" || a.pkg == "" || a.fn == "" {
			t.Errorf("allowance %+v needs a package, a function and a reason", a)
		}
	}
}

func TestEveryClockAllowanceHasAReason(t *testing.T) {
	for _, a := range clockAllowances {
		if a.reason == "" || a.pkg == "" || a.fn == "" {
			t.Errorf("allowance %+v needs a package, a function and a reason", a)
		}
	}
}

func TestOwnedBy(t *testing.T) {
	cases := []struct {
		pkg, owner string
		want       bool
	}{
		{"github.com/x/src-go/internal/execx", "internal/execx", true},
		{"internal/execx", "internal/execx", true},
		{"github.com/x/src-go/internal/execx_test", "internal/execx", true},
		{"github.com/x/src-go/internal/execx [github.com/x/src-go/internal/execx.test]", "internal/execx", true},
		{"github.com/x/src-go/internal/notexecx", "internal/execx", false},
		{"github.com/x/src-go/internal/execx/sub", "internal/execx", false},
	}
	for _, c := range cases {
		if got := ownedBy(c.pkg, c.owner); got != c.want {
			t.Errorf("ownedBy(%q, %q) = %v, want %v", c.pkg, c.owner, got, c.want)
		}
	}
}

func TestRawConnProbe(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), RawConnProbe, "fix/internal/badraw")
}

func TestContainerPidsLimit(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), ContainerPidsLimit, "fix/internal/badpids")
}
