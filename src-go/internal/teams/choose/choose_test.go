package choose

import (
	"errors"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/coordinator"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/ops"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/seats"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	seatChoose = "agent-bus seat choose alpha"
	setRole    = teamtest.SudoBus + "set-role alpha "
	handleOne  = "proj.1+local.podman"
	handleDev  = "proj.dev+local.podman"
	handleNew  = "proj.3+local.podman"
)

// rig is the fake machine on a terminal, and the chooser over it.
type rig struct {
	*teamtest.World
	*teamtest.Console

	c *Choose
}

// newRig offers seat 1 (free), dev (held) and 3 (new); the team's members are seat 1 and dev.
func newRig(t *testing.T) rig {
	t.Helper()
	w := teamtest.Populated(t)
	console := teamtest.Interactive(t, w)
	kit := flow.New(w.Host)
	b := board.New(w.Host)
	o := ops.New(w.Host, b.Statuses, func() { b.ForgetDevices("") })
	w.On(seatChoose, "CHOICE\t1\tfree\t"+handleOne+"\ta ccy seat\n"+
		"CHOICE\tdev\theld\t"+handleDev+"\ta ccy seat\n"+
		"CHOICE\t3\tnew\t"+handleNew+"\ta ccy seat\n"+
		"DEFAULT\t1\n", 0)
	w.On(teamtest.SudoBus+"list alpha", roster("worker", "worker"), 0)
	w.On(w.DeclarePrefix("declared"), teamtest.DeclaredAlpha, 0)
	w.On("sudo -n -v", "", 0)
	w.On(setRole, "", 0)
	return rig{World: w, Console: console, c: New(w.Host, kit, seats.New(w.Host, kit, o))}
}

func roster(oneIs, devIs string) string {
	return "HUMAN\towner\tjoin\n" +
		"MEMBER\t" + handleOne + "\t" + oneIs + "\tjoin\tactive\n" +
		"MEMBER\t" + handleDev + "\t" + devIs + "\tjoin\tactive\n"
}

func (h rig) seatRow(seat, state, handle string) string {
	return row(agentbus.Choice{Seat: seat, State: state, Handle: handle, Words: "a ccy seat"})
}

func (h rig) pickSeatOne() string { return "CHOOSE:" + h.seatRow("1", "free", handleOne) }

func (h rig) stdout() string { return h.Out.String() }

// gumHeader is the command line of gum call n, which carries the question it asked.
func (h rig) gumHeader(n int) string {
	if n >= len(h.Script.GumLog) {
		return ""
	}
	return h.Script.GumLog[n]
}

func TestChoosingASeatKeepingItsRolePrintsTheSeatAndChangesNothing(t *testing.T) {
	h := newRig(t)
	h.Script.Gum = []string{h.pickSeatOne(), "CHOOSE:" + keepRole("worker")}
	if code := h.c.Seat(t.Context(), []string{"alpha"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if h.stdout() != "1@alpha\n" {
		t.Fatalf("stdout %q", h.stdout())
	}
	if h.Fake.Called("sudo -n -v") || h.Fake.Called(setRole) {
		t.Fatalf("sudo ran though the role was kept: %v", h.Fake.Calls())
	}
	if !strings.Contains(h.ErrOut.String(), "Taking seat 1@alpha: "+handleOne+" (a ccy seat).") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestTheSeatAndRoleQuestionsCarryTheHeadingTheyAreGiven(t *testing.T) {
	h := newRig(t)
	h.Script.Gum = []string{h.pickSeatOne(), "CHOOSE:" + keepRole("worker")}
	if ref, code := h.c.PickSeat(t.Context(), "alpha", "", "Team 2 of 3: alpha"); ref != "1@alpha" || code != term.ExitOK {
		t.Fatalf("%q %d: %s", ref, code, h.ErrOut.String())
	}
	if !strings.Contains(h.gumHeader(0), "--header Team 2 of 3: alpha · seat: which seat does this session take?") ||
		!strings.Contains(h.gumHeader(1), "--header Team 2 of 3: alpha · role of seat 1: ") {
		t.Fatalf("gum %q", h.Script.GumLog)
	}
	if !strings.HasPrefix(h.ErrOut.String(), "Team 2 of 3: alpha\n") {
		t.Fatalf("the team's lines are not under its heading: %q", h.ErrOut.String())
	}
}

func TestTheDefaultSeatIsTheOneTheWrapperNames(t *testing.T) {
	h := newRig(t)
	h.Script.Gum = []string{h.pickSeatOne(), "CHOOSE:" + keepRole("worker")}
	h.c.Seat(t.Context(), []string{"alpha"})
	if len(h.Script.GumLog) == 0 || !strings.Contains(h.Script.GumLog[0], "--selected "+h.seatRow("1", "free", handleOne)) {
		t.Fatalf("gum %q", h.Script.GumLog)
	}
}

func TestChoosingMakeCoordinatorChangesTheRoleThroughTheSharedPath(t *testing.T) {
	h := newRig(t)
	h.Script.Gum = []string{h.pickSeatOne(), "CHOOSE:" + coordinator.NoneChoice("alpha")}
	h.Script.Fzf = []string{"|YES"}
	if code := h.c.Seat(t.Context(), []string{"alpha"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if !h.Fake.Called(setRole + handleOne + " --role=coordinator") {
		t.Fatalf("calls %v", h.Fake.Calls())
	}
	if h.stdout() != "1@alpha\n" {
		t.Fatalf("stdout %q", h.stdout())
	}
}

func TestMakeCoordinatorNamesTheHolderWhoBecomesAWorker(t *testing.T) {
	h := newRig(t)
	h.On(teamtest.SudoBus+"list alpha", roster("worker", "coordinator"), 0)
	holder := coordinator.Holder{Handle: handleDev, Running: coordinator.Active}
	h.Script.Gum = []string{h.pickSeatOne(), "CHOOSE:" + coordinator.TakeoverChoice(holder)}
	h.Script.Fzf = []string{"|YES"}
	if code := h.c.Seat(t.Context(), []string{"alpha"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if !strings.Contains(h.gumHeader(1), "alpha's coordinator is "+handleDev+" (running)") {
		t.Fatalf("the role question does not name the holder and whether it runs: %q", h.gumHeader(1))
	}
	takeover := "alpha's coordinator is " + handleDev + " (running). A team has one coordinator. Remove " + handleDev +
		" as coordinator and make " + handleOne + " the coordinator instead?"
	if !strings.Contains(h.HeaderOf(0), takeover) || !strings.Contains(strings.Join(h.Script.FzfArgs[0], " "), "--bind=start:last") {
		t.Fatalf("the confirmation is not the takeover, defaulting to no: %q", h.HeaderOf(0))
	}
	if !h.Fake.Called(setRole+handleDev+" --role=worker") || !h.Fake.Called(setRole+handleOne+" --role=coordinator") {
		t.Fatalf("calls %v", h.Fake.Calls())
	}
}

func TestTheRoleChoicesKeepWorkerFirstAndNeverOfferAPlainCoordinator(t *testing.T) {
	for _, tc := range []struct {
		name, roster, want string
	}{
		{"none", roster("worker", "worker"), coordinator.NoneChoice("alpha")},
		{"takeover", roster("worker", "coordinator"), coordinator.TakeoverChoice(coordinator.Holder{Handle: handleDev, Running: coordinator.Active})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newRig(t)
			h.On(teamtest.SudoBus+"list alpha", tc.roster, 0)
			h.Script.Gum = []string{h.pickSeatOne(), "CHOOSE:" + keepRole("worker")}
			if code := h.c.Seat(t.Context(), []string{"alpha"}); code != term.ExitOK {
				t.Fatalf("exit %d: %s", code, h.ErrOut.String())
			}
			q := h.gumHeader(1)
			if !strings.Contains(q, "--selected "+keepRole("worker")) || !strings.HasSuffix(q, keepRole("worker")+" "+tc.want) {
				t.Fatalf("question %q, want the worker first and selected, then %q", q, tc.want)
			}
		})
	}
}

func TestTheCoordinatorCanStopBeingOne(t *testing.T) {
	h := newRig(t)
	h.On(teamtest.SudoBus+"list alpha", roster("coordinator", "worker"), 0)
	h.Script.Gum = []string{h.pickSeatOne(), "CHOOSE:" + stopRole}
	h.Script.Fzf = []string{"|YES"}
	if code := h.c.Seat(t.Context(), []string{"alpha"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if !h.Fake.Called(setRole + handleOne + " --role=worker") {
		t.Fatalf("calls %v", h.Fake.Calls())
	}
}

func TestEscAtTheRoleQuestionLeavesTheRoleAndStillTakesTheSeat(t *testing.T) {
	h := newRig(t)
	h.Script.Gum = []string{h.pickSeatOne(), "CANCEL"}
	if code := h.c.Seat(t.Context(), []string{"alpha"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if h.stdout() != "1@alpha\n" || h.Fake.Called("sudo -n -v") || h.Fake.Called(setRole) {
		t.Fatalf("stdout %q calls %v", h.stdout(), h.Fake.Calls())
	}
	if !strings.Contains(h.ErrOut.String(), "Its role is left as it is.") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestDecliningTheConfirmationLeavesTheRole(t *testing.T) {
	h := newRig(t)
	h.Script.Gum = []string{h.pickSeatOne(), "CHOOSE:" + coordinator.NoneChoice("alpha")}
	h.Script.Fzf = []string{"|EXIT"}
	if code := h.c.Seat(t.Context(), []string{"alpha"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if h.stdout() != "1@alpha\n" || h.Fake.Called(setRole) {
		t.Fatalf("stdout %q calls %v", h.stdout(), h.Fake.Calls())
	}
}

func TestAChangeThatFailsFailsTheLaunchAndPrintsNoSeat(t *testing.T) {
	h := newRig(t)
	h.OnErr(setRole, "agent-bus: refused", 70)
	h.Script.Gum = []string{h.pickSeatOne(), "CHOOSE:" + coordinator.NoneChoice("alpha")}
	h.Script.Fzf = []string{"|YES"}
	if code := h.c.Seat(t.Context(), []string{"alpha"}); code != term.ExitFailure {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if h.stdout() != "" {
		t.Fatalf("a failed launch printed a seat: %q", h.stdout())
	}
}

func TestANewSeatIsOfferedNoRoleAndSaysWhy(t *testing.T) {
	h := newRig(t)
	h.Script.Gum = []string{"CHOOSE:" + h.seatRow("3", "new", handleNew)}
	if code := h.c.Seat(t.Context(), []string{"alpha"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if h.stdout() != "3@alpha\n" || len(h.Script.GumLog) != 1 {
		t.Fatalf("stdout %q gum %q", h.stdout(), h.Script.GumLog)
	}
	if !strings.Contains(h.ErrOut.String(), "joins as a worker") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
	if h.Fake.Called(teamtest.SudoBus + "list") {
		t.Fatal("read the team for a seat that is not in it")
	}
}

func TestLeavingTheSeatChooserStartsNothing(t *testing.T) {
	h := newRig(t)
	h.Script.Gum = []string{"CANCEL"}
	if code := h.c.Seat(t.Context(), []string{"alpha"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if h.stdout() != "" || !strings.Contains(h.ErrOut.String(), "No seat chosen; nothing started.") {
		t.Fatalf("stdout %q stderr %q", h.stdout(), h.ErrOut.String())
	}
}

func TestWithoutGumItNamesTheSeatToTypeInstead(t *testing.T) {
	h := newRig(t)
	h.c.Kit.LookPath = func(name string) (string, error) {
		if name == "gum" {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + name, nil
	}
	if code := h.c.Seat(t.Context(), []string{"alpha"}); code != 78 {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"gum is not installed", "--teams 1@alpha"} {
		if !strings.Contains(h.ErrOut.String(), want) {
			t.Fatalf("stderr %q lacks %q", h.ErrOut.String(), want)
		}
	}
}

func TestTheWrappersRefusalIsTheExitCode(t *testing.T) {
	h := newRig(t)
	h.OnErr(seatChoose, "agent-bus: team alpha is not running here", 78)
	if code := h.c.Seat(t.Context(), []string{"alpha"}); code != 78 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.ErrOut.String(), "the seats of team alpha could not be listed (agent-bus seat choose exited 78") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestAnUnreadableTeamFailsBeforeAnyRoleIsOffered(t *testing.T) {
	h := newRig(t)
	h.OnErr(teamtest.SudoBus+"list alpha", "sudo: a password is required", 1)
	h.Script.Gum = []string{h.pickSeatOne()}
	if code := h.c.Seat(t.Context(), []string{"alpha"}); code != term.ExitFailure {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if h.stdout() != "" {
		t.Fatalf("stdout %q", h.stdout())
	}
}

func TestSeatTakesExactlyOneTeam(t *testing.T) {
	h := newRig(t)
	for _, args := range [][]string{nil, {"a", "b"}} {
		if code := h.c.Seat(t.Context(), args); code != term.ExitUsage {
			t.Fatalf("%v exit %d", args, code)
		}
	}
}

func TestDescribeNamesTheSeatAndItsRole(t *testing.T) {
	h := newRig(t)
	h.On("agent-bus seat list", "SEAT\talpha\t1\theld\tself\t"+handleOne+"\nSEAT\talpha\tdev\tfree\t-\t"+handleDev+"\n", 0)
	h.On(teamtest.SudoBus+"list alpha", roster("coordinator", "worker"), 0)
	if code := h.c.Describe(t.Context(), []string{"1@alpha,dev@alpha"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if want := "1@alpha (coordinator), dev@alpha (worker)\n"; h.stdout() != want {
		t.Fatalf("stdout %q, want %q", h.stdout(), want)
	}
	if h.Fake.Called("sudo -n -v") {
		t.Fatal("asked for a password to describe a launch")
	}
}

func TestDescribeFollowsEachSeatWithItsTeamsDescription(t *testing.T) {
	h := newRig(t)
	h.On(h.DeclarePrefix("declared"), "TEAM\talpha\t6167\tpresent\talpha.agent-bus.internal\thttp://192.0.2.10:6167\t\"Builds the example site\"\n"+
		"TEAM\tbeta\t6168\tpresent\tbeta.agent-bus.internal\t-\t-\n", 0)
	h.On("agent-bus seat list", "SEAT\talpha\t1\theld\tself\t"+handleOne+"\n", 0)
	h.On(teamtest.SudoBus+"list alpha", roster("worker", "worker"), 0)
	if code := h.c.Describe(t.Context(), []string{"1@alpha,3@beta"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	want := "1@alpha (worker): Builds the example site, 3@beta (new seat: it joins as a worker)\n"
	if h.stdout() != want {
		t.Fatalf("stdout %q, want %q", h.stdout(), want)
	}
}

func TestDescribeStillNamesTheSeatsWhenTheDescriptionsCannotBeRead(t *testing.T) {
	h := newRig(t)
	h.OnErr(h.DeclarePrefix("declared"), "declare: cannot read", 78)
	h.On("agent-bus seat list", "SEAT\talpha\t1\theld\tself\t"+handleOne+"\n", 0)
	if code := h.c.Describe(t.Context(), []string{"1@alpha"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if !strings.HasPrefix(h.stdout(), "1@alpha (") || strings.Contains(h.stdout(), ": ") || !strings.Contains(h.ErrOut.String(), "declare: cannot read") {
		t.Fatalf("stdout %q stderr %q", h.stdout(), h.ErrOut.String())
	}
}

func TestDescribeReadsEachTeamOnce(t *testing.T) {
	h := newRig(t)
	h.On("agent-bus seat list", "SEAT\talpha\t1\theld\tself\t"+handleOne+"\nSEAT\talpha\tdev\tfree\t-\t"+handleDev+"\n", 0)
	h.Spawned()
	h.c.Describe(t.Context(), []string{"1@alpha,dev@alpha"})
	if n := h.Spawned()["agent-bus list"]; n != 1 {
		t.Fatalf("read the team %d times", n)
	}
}

func TestDescribeSaysWhyARoleIsUnknown(t *testing.T) {
	h := newRig(t)
	h.On("agent-bus seat list", "SEAT\talpha\t1\theld\tself\t"+handleOne+"\n", 0)
	h.OnErr(teamtest.SudoBus+"list alpha", "sudo: a password is required", 1)
	if code := h.c.Describe(t.Context(), []string{"1@alpha"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if !strings.HasPrefix(h.stdout(), "1@alpha (role unknown: ") {
		t.Fatalf("stdout %q", h.stdout())
	}
}

func TestDescribeAnUnusedSeatIsNew(t *testing.T) {
	h := newRig(t)
	h.On("agent-bus seat list", "", 0)
	h.c.Describe(t.Context(), []string{"3@alpha"})
	if want := "3@alpha (new seat: it joins as a worker)\n"; h.stdout() != want {
		t.Fatalf("stdout %q, want %q", h.stdout(), want)
	}
}

func TestDescribeWritesThePrefixFirstSoTheLauncherNeedsNoEcho(t *testing.T) {
	h := newRig(t)
	h.On("agent-bus seat list", "SEAT\talpha\t1\theld\tself\t"+handleOne+"\n", 0)
	h.On(teamtest.SudoBus+"list alpha", roster("coordinator", "worker"), 0)
	if code := h.c.Describe(t.Context(), []string{"--prefix", "  Team:    ", "1@alpha"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if want := "  Team:    1@alpha (coordinator)\n"; h.stdout() != want {
		t.Fatalf("stdout %q, want %q", h.stdout(), want)
	}
	h = newRig(t)
	h.c.Describe(t.Context(), []string{"--prefix", "  Team:    ", ""})
	if want := "  Team:    none\n"; h.stdout() != want {
		t.Fatalf("stdout %q, want %q", h.stdout(), want)
	}
	h = newRig(t)
	if code := h.c.Describe(t.Context(), []string{"--prefix"}); code != term.ExitUsage {
		t.Fatalf("exit %d", code)
	}
}

// S-C4: the launch block names every team of the session with its seat, role and description.
func TestDescribeBlockNamesEveryTeamWithSeatRoleAndDescription(t *testing.T) {
	h := newRig(t)
	h.On(h.DeclarePrefix("declared"), "TEAM\talpha\t6167\tpresent\talpha.agent-bus.internal\thttp://192.0.2.10:6167\t\"Builds the example site\"\n"+
		"TEAM\tbeta\t6168\tpresent\tbeta.agent-bus.internal\thttp://192.0.2.10:6168\t-\n", 0)
	h.On("agent-bus seat list", "SEAT\talpha\t1\theld\tself\t"+handleOne+"\nSEAT\talpha\tdev\tfree\t-\t"+handleDev+"\n", 0)
	h.On(teamtest.SudoBus+"list alpha", roster("coordinator", "worker"), 0)
	if code := h.c.Describe(t.Context(), []string{"--block", "1@alpha,3@beta"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	want := "This session is on 2 teams:\n" +
		"  alpha  seat 1 (coordinator)  Builds the example site\n" +
		"  beta   seat 3 (new seat: it joins as a worker)  (no description)\n" +
		"Any human listed in these teams can direct this session as you can.\n"
	if h.stdout() != want {
		t.Fatalf("stdout %q, want %q", h.stdout(), want)
	}
}

func TestDescribeBlockOfOneTeamSaysSo(t *testing.T) {
	h := newRig(t)
	h.On("agent-bus seat list", "", 0)
	if code := h.c.Describe(t.Context(), []string{"--block", "1@alpha"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if !strings.HasPrefix(h.stdout(), "This session is on 1 team:\n  alpha  seat 1 ") ||
		!strings.HasSuffix(h.stdout(), "\nAny human listed in this team can direct this session as you can.\n") {
		t.Fatalf("stdout %q", h.stdout())
	}
}

func TestDescribeBlockNeedsAListOfSeatsAndIsNotCombinedWithPrefix(t *testing.T) {
	for _, args := range [][]string{{"--block"}, {"--block", ""}, {"--block", "alpha"}, {"--block", "--prefix", "x", "1@a"}} {
		h := newRig(t)
		if code := h.c.Describe(t.Context(), args); code != term.ExitUsage {
			t.Fatalf("%v exit %d", args, code)
		}
		if h.stdout() != "" {
			t.Fatalf("%v stdout %q", args, h.stdout())
		}
	}
}

func TestDescribeNothingAndATeamAlone(t *testing.T) {
	h := newRig(t)
	h.c.Describe(t.Context(), []string{""})
	if h.stdout() != "none\n" {
		t.Fatalf("stdout %q", h.stdout())
	}
	h = newRig(t)
	h.c.Describe(t.Context(), []string{"alpha"})
	if h.stdout() != "alpha\n" {
		t.Fatalf("stdout %q", h.stdout())
	}
}

const (
	handleTwo   = "proj.2+local.host"
	handleThree = "proj.3+local.host"
	hostSeats   = "SEAT\talpha\t1\theld\tself\t" + handleOne + "\nSEAT\tbeta\t2\theld\tself\t" + handleTwo + "\n" +
		"SEAT\tgamma\t3\theld\tself\t" + handleThree + "\n"
)

// S-E5: the sentence a host session is told about itself names every team and the handle in it.
func TestDescribeSentenceNamesEveryHandleInItsTeam(t *testing.T) {
	for list, want := range map[string]string{
		"1@alpha":                "You are " + handleOne + " in alpha.\n",
		"1@alpha,2@beta":         "You are " + handleOne + " in alpha and " + handleTwo + " in beta.\n",
		"1@alpha,2@beta,3@gamma": "You are " + handleOne + " in alpha, " + handleTwo + " in beta and " + handleThree + " in gamma.\n",
	} {
		h := newRig(t)
		h.On("agent-bus seat list", hostSeats, 0)
		if code := h.c.Describe(t.Context(), []string{"--sentence", list}); code != term.ExitOK {
			t.Fatalf("%s: exit %d: %s", list, code, h.ErrOut.String())
		}
		if h.stdout() != want {
			t.Fatalf("%s: stdout %q, want %q", list, h.stdout(), want)
		}
	}
}

func TestDescribeSentenceOfASeatThatDoesNotExistFailsAndPrintsNothing(t *testing.T) {
	h := newRig(t)
	h.On("agent-bus seat list", hostSeats, 0)
	if code := h.c.Describe(t.Context(), []string{"--sentence", "1@alpha,9@beta"}); code != exitConfig {
		t.Fatalf("exit %d", code)
	}
	if h.stdout() != "" || !strings.Contains(h.ErrOut.String(), "9@beta") {
		t.Fatalf("stdout %q stderr %q", h.stdout(), h.ErrOut.String())
	}
}

func TestDescribeSentenceNeedsAListOfSeats(t *testing.T) {
	for _, args := range [][]string{{"--sentence"}, {"--sentence", ""}, {"--sentence", "alpha"}} {
		h := newRig(t)
		if code := h.c.Describe(t.Context(), args); code != term.ExitUsage {
			t.Fatalf("%v exit %d", args, code)
		}
		if h.stdout() != "" {
			t.Fatalf("%v stdout %q", args, h.stdout())
		}
	}
}
