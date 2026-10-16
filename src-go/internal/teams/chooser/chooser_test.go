package chooser

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const repo = "example-org/proj"

// rig is the fake machine on a terminal, in a GitHub checkout of example-org/proj, with the
// seat chooser, the wizard and the apply replaced by recorders.
type rig struct {
	*teamtest.World
	*teamtest.Console

	c       *Chooser
	payload *bytes.Buffer

	seatsAsked []string // "<team>/<type>"
	headings   []string // the heading each seat question was given
	seatLeft   map[string]bool
	seatCode   term.ExitCode
	created    string
	createCode term.ExitCode
	creates    int
	applied    [][]string
	applyErr   error
}

const (
	alphaCovers  = "CHOICE\talpha\tcovers\t\"Builds the site\"\n"
	betaWiden    = "CHOICE\tbeta\twiden\t\"Reviews the docs\"\n"
	gammaExclude = "CHOICE\tgamma\texcluded\t-\n"
)

func newRig(t *testing.T, choices string) *rig {
	t.Helper()
	w := teamtest.New(t)
	console := teamtest.Interactive(t, w)
	w.On("git rev-parse --is-inside-work-tree", "true\n", 0)
	w.On("git rev-parse --show-toplevel", w.Checkout()+"\n", 0)
	w.On("git remote get-url origin", "git@example.com:example-org/proj.git\n", 0)
	w.OnDeclare("github-remote", repo+"\n")
	w.OnDeclare("choices", choices)
	for _, team := range []string{"alpha", "beta", "gamma", "delta", "fresh"} {
		w.On("systemctl is-active agent-bus-hs@"+team+".service", "active\n", 0)
	}
	r := &rig{World: w, Console: console, payload: &bytes.Buffer{}, seatLeft: map[string]bool{}}
	r.c = New(w.Host,
		func(_ context.Context, team, memberType, heading string) (string, term.ExitCode) {
			r.seatsAsked = append(r.seatsAsked, team+"/"+memberType)
			r.headings = append(r.headings, heading)
			r.say(heading + " (seat chosen)")
			if r.seatLeft[team] {
				return "", term.ExitOK
			}
			if r.seatCode != term.ExitOK {
				return "", r.seatCode
			}
			return "1@" + team, term.ExitOK
		},
		func(context.Context) (string, term.ExitCode) {
			r.creates++
			return r.created, r.createCode
		},
		func(_ context.Context, teams []string) error {
			r.applied = append(r.applied, teams)
			return r.applyErr
		})
	r.c.Payload = r.payload
	return r
}

// say is a line on stderr, as the seat chooser prints its own.
func (r *rig) say(line string) {
	r.Host.Print(r.Host.Stderr, line+"\n")
}

func (r *rig) run(args ...string) term.ExitCode {
	return r.c.Run(context.Background(), args)
}

func (r *rig) stderr() string { return r.ErrOut.String() }

func (r *rig) lacksOutput(t *testing.T) {
	t.Helper()
	if r.payload.Len() != 0 {
		t.Fatalf("payload %q, want none", r.payload.String())
	}
	if r.Out.Len() != 0 {
		t.Fatalf("a flow wrote on stdout: %q", r.Out.String())
	}
}

func contains(t *testing.T, what, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("%s lacks %q\n---\n%s", what, w, got)
		}
	}
}

func exit(t *testing.T, got, want term.ExitCode, r *rig) {
	t.Helper()
	if got != want {
		t.Fatalf("exit %d, want %d\nstderr:\n%s", got, want, r.stderr())
	}
}

// S-B1: attended only.
func TestWithoutATerminalItAsksNothingAndRefuses(t *testing.T) {
	r := newRig(t, alphaCovers)
	r.Host.IsTerminal = func(any) bool { return false }
	exit(t, r.run("--type=podman"), term.ExitUsage, r)
	contains(t, "stderr", r.stderr(), "terminal")
	r.lacksOutput(t)
}

func TestTheMemberTypeIsRequiredAndKnown(t *testing.T) {
	r := newRig(t, alphaCovers)
	for _, args := range [][]string{{}, {"--type=lxc"}, {"--type"}, {"--type=podman", "a", "b"}, {"--bogus"}} {
		exit(t, r.run(args...), term.ExitUsage, r)
	}
	r.lacksOutput(t)
}

// S-B2: one team, covering the checkout and up: taken without a list.
func TestOneTeamCoveringTheCheckoutIsTakenWithoutAList(t *testing.T) {
	r := newRig(t, alphaCovers)
	exit(t, r.run("--type=podman"), term.ExitOK, r)
	if r.payload.String() != "1@alpha\n" {
		t.Fatalf("payload %q", r.payload.String())
	}
	contains(t, "stderr", r.stderr(), "Team alpha works on "+repo+" (this checkout): Builds the site.")
	if len(r.Script.Rows) != 0 {
		t.Fatalf("a list was shown: %v", r.Script.Rows)
	}
	if !slices.Equal(r.seatsAsked, []string{"alpha/podman"}) {
		t.Fatalf("seats %v", r.seatsAsked)
	}
	if r.Out.Len() != 0 {
		t.Fatalf("stdout %q", r.Out.String())
	}
}

// S-B3 and S-B4: every team, covering first, with the standing and the description.
func TestTheListShowsEveryTeamCoveringFirstAndCreating(t *testing.T) {
	r := newRig(t, betaWiden+gammaExclude+alphaCovers)
	r.Script.Fzf = []string{"|alpha"}
	exit(t, r.run("--type=podman"), term.ExitOK, r)
	rows := r.Script.Rows[0]
	if len(rows) != 4 {
		t.Fatalf("rows %q", rows)
	}
	want := [][]string{
		{"alpha", "covers " + repo, "Builds the site"},
		{"beta", "would add " + repo, "Reviews the docs"},
		{"gamma", "excludes " + repo, "(no description)"},
		{"+ Create a team for " + repo},
	}
	for i, parts := range want {
		contains(t, "row", rows[i], parts...)
	}
	contains(t, "header", r.HeaderOf(0), "Tab marks several", "Esc leaves")
	if r.payload.String() != "1@alpha\n" {
		t.Fatalf("payload %q", r.payload.String())
	}
	if !r.Fake.Called("env PYTHONPATH=" + r.Host.RootDir + " python3 -P -m helpers.agent_bus.declare choices") {
		t.Fatal("coverage did not come from declare.py choices")
	}
}

func TestACoveringTeamAlongsideOthersIsStillListed(t *testing.T) {
	r := newRig(t, alphaCovers+betaWiden)
	r.Script.Fzf = []string{"|alpha"}
	exit(t, r.run("--type=podman"), term.ExitOK, r)
	if len(r.Script.Rows) != 1 {
		t.Fatalf("with two teams the list is always shown: %v", r.Script.Rows)
	}
}

func TestADownTeamIsMarkedAndCannotBeTaken(t *testing.T) {
	r := newRig(t, alphaCovers+betaWiden)
	r.On("systemctl is-active agent-bus-hs@alpha.service", "inactive\n", 3)
	r.Script.Fzf = []string{"|alpha", "|ABORT"}
	exit(t, r.run("--type=podman"), term.ExitOK, r)
	contains(t, "row", r.Script.Rows[0][0], "alpha", "(down)")
	contains(t, "stderr", r.stderr(), "alpha's homeserver is down: ccy-teams apply alpha.")
	if len(r.Script.Rows) != 2 {
		t.Fatalf("the list was not shown again: %v", r.Script.Rows)
	}
	r.lacksOutput(t)
}

// S-B6: an excluded team is explained and the list comes back, three rounds at most.
func TestAnExcludedTeamIsExplainedAndTheListComesBackThreeTimesThenFails(t *testing.T) {
	r := newRig(t, gammaExclude+alphaCovers+betaWiden)
	r.Script.Fzf = []string{"|gamma", "|gamma", "|gamma"}
	exit(t, r.run("--type=podman"), term.ExitFailure, r)
	contains(t, "stderr", r.stderr(),
		"gamma excludes "+repo+" on purpose: edit its exclude in the host_vars to change that.")
	if len(r.Script.Rows) != 3 || r.seatsAsked != nil {
		t.Fatalf("rounds %d seats %v", len(r.Script.Rows), r.seatsAsked)
	}
	r.lacksOutput(t)
}

func TestATeamWhoseScopeCannotBeReadIsExplained(t *testing.T) {
	r := newRig(t, alphaCovers+"CHOICE\tdelta\tunreadable\t!vaulted\n")
	r.Script.Fzf = []string{"|delta", "|alpha"}
	exit(t, r.run("--type=podman"), term.ExitOK, r)
	contains(t, "stderr", r.stderr(), "delta's scope cannot be read")
	contains(t, "row", strings.Join(r.Script.Rows[0], "\n"), "(description vaulted)")
}

// S-B7 and S-B9: taking a team that does not cover: say what Add does, add, apply once, then go on.
func TestAddingTheRepositoryAppliesOnceAndThenStarts(t *testing.T) {
	r := newRig(t, alphaCovers+betaWiden)
	r.OnDeclare("add-repo", "CHANGED\tteam beta trusts "+repo+"\n")
	r.Script.Fzf = []string{"|beta"}
	r.Script.Gum = []string{"CHOOSE:Add " + repo + " to beta's scope"}
	exit(t, r.run("--type=podman"), term.ExitOK, r)
	if r.payload.String() != "1@beta\n" {
		t.Fatalf("payload %q", r.payload.String())
	}
	if len(r.applied) != 1 || !slices.Equal(r.applied[0], []string{"beta"}) {
		t.Fatalf("applied %v", r.applied)
	}
	if !r.Fake.Called("env PYTHONPATH=" + r.Host.RootDir + " python3 -P -m helpers.agent_bus.declare add-repo --file " + r.Host.HostVars + " --team beta --repo " + repo) {
		t.Fatal("the scope was not edited by declare.py add-repo")
	}
	contains(t, "stderr", r.stderr(), "host_vars", "play-agent-bus.yml", "sudo", "no homeserver restart", "trusts "+repo)
	if len(r.Script.GumLog) != 1 {
		t.Fatalf("gum %v", r.Script.GumLog)
	}
	contains(t, "question", r.Script.GumLog[0], "beta", repo, "(any branch)",
		"--selected Add "+repo+" to beta's scope",
		"Join without adding (beta will treat "+repo+" references as external)", "Leave beta out")
}

func TestAnApplyThatFailsStartsNothingAndNamesTheCommand(t *testing.T) {
	r := newRig(t, alphaCovers+betaWiden)
	r.OnDeclare("add-repo", "CHANGED\tteam beta trusts "+repo+"\n")
	r.applyErr = errors.New("play failed")
	r.Script.Fzf = []string{"|beta"}
	r.Script.Gum = []string{"CHOOSE:Add " + repo + " to beta's scope"}
	exit(t, r.run("--type=podman"), term.ExitFailure, r)
	contains(t, "stderr", r.stderr(), "ccy-teams apply beta", "nothing was started")
	if r.seatsAsked != nil {
		t.Fatalf("seats were asked: %v", r.seatsAsked)
	}
	r.lacksOutput(t)
}

func TestAnAddThatDeclareRefusesFailsAndStartsNothing(t *testing.T) {
	r := newRig(t, alphaCovers+betaWiden)
	r.OnErr(r.DeclarePrefix("add-repo"), "declare: team beta already lists 32 repos\n", 78)
	r.Script.Fzf = []string{"|beta"}
	r.Script.Gum = []string{"CHOOSE:Add " + repo + " to beta's scope"}
	exit(t, r.run("--type=podman"), 78, r)
	contains(t, "stderr", r.stderr(), "already lists 32 repos")
	if len(r.applied) != 0 || r.seatsAsked != nil {
		t.Fatalf("applied %v seats %v", r.applied, r.seatsAsked)
	}
	r.lacksOutput(t)
}

// S-B10: joining without adding is a named choice, and leaving a team out drops it.
func TestJoiningWithoutAddingKeepsTheTeamAndSaysItsReferencesAreExternal(t *testing.T) {
	r := newRig(t, alphaCovers+betaWiden)
	r.Script.Fzf = []string{"|beta"}
	r.Script.Gum = []string{"CHOOSE:Join without adding (beta will treat " + repo + " references as external)"}
	exit(t, r.run("--type=podman"), term.ExitOK, r)
	if r.payload.String() != "1@beta\n" || len(r.applied) != 0 {
		t.Fatalf("payload %q applied %v", r.payload.String(), r.applied)
	}
	contains(t, "stderr", r.stderr(), "beta", repo+" references external")
	if r.Fake.Called("env PYTHONPATH=" + r.Host.RootDir + " python3 -P -m helpers.agent_bus.declare add-repo") {
		t.Fatal("the scope was edited")
	}
}

func TestLeavingATeamOutShowsTheListAgain(t *testing.T) {
	r := newRig(t, alphaCovers+betaWiden)
	r.Script.Fzf = []string{"|beta", "|alpha"}
	r.Script.Gum = []string{"CHOOSE:Leave beta out"}
	exit(t, r.run("--type=podman"), term.ExitOK, r)
	if len(r.Script.Rows) != 2 || r.payload.String() != "1@alpha\n" {
		t.Fatalf("rounds %d payload %q", len(r.Script.Rows), r.payload.String())
	}
}

func TestEscOnTheScopeQuestionLeavesWithNothingChosen(t *testing.T) {
	r := newRig(t, alphaCovers+betaWiden)
	r.Script.Fzf = []string{"|beta"}
	r.Script.Gum = []string{"CANCEL"}
	exit(t, r.run("--type=podman"), term.ExitOK, r)
	contains(t, "stderr", r.stderr(), "No team chosen; nothing started.")
	r.lacksOutput(t)
	if len(r.applied) != 0 {
		t.Fatalf("applied %v", r.applied)
	}
}

// S-B5: not a GitHub checkout: the whole list, joining without widening.
func TestOutsideAGitHubCheckoutEveryTeamIsListedAndJoinedWithoutWidening(t *testing.T) {
	r := newRig(t, "")
	r.On("git rev-parse --is-inside-work-tree", "", 128)
	r.On(r.DeclarePrefix("declared"), "TEAM\talpha\t6167\tpresent\talpha.agent-bus.internal\thttp://192.0.2.10:6167\t\"Builds the site\"\n"+
		"TEAM\tbeta\t6168\tpresent\tbeta.agent-bus.internal\thttp://192.0.2.10:6168\t-\n"+
		"TEAM\tgone\t6169\tabsent\tgone.agent-bus.internal\t-\t-\n", 0)
	r.Script.Fzf = []string{"|beta"}
	exit(t, r.run("--type=podman"), term.ExitOK, r)
	rows := strings.Join(r.Script.Rows[0], "\n")
	contains(t, "rows", rows, "alpha", "not a GitHub checkout", "Builds the site", "beta", "(no description)", "+ Create a team")
	if strings.Contains(rows, "gone") || strings.Contains(rows, "would add") {
		t.Fatalf("rows %s", rows)
	}
	if r.payload.String() != "1@beta\n" || len(r.Script.GumLog) != 0 || r.Fake.Called("env PYTHONPATH="+r.Host.RootDir+" python3 -P -m helpers.agent_bus.declare choices") {
		t.Fatalf("payload %q gum %v", r.payload.String(), r.Script.GumLog)
	}
}

// A session is never chosen for in an LXC container's checkout seen from the host, whichever
// way the seats are given: it would run on the container's files as the host user.
func TestNoSessionIsChosenForInAContainersCheckout(t *testing.T) {
	for _, args := range [][]string{{"--type=podman"}, {"--type=host", "1@alpha"}, {"--type=host", "--bus"}} {
		r := newRig(t, alphaCovers+betaWiden)
		top := r.InLxcCheckout()
		exit(t, r.run(args...), term.ExitFailure, r)
		contains(t, "stderr", r.stderr(), top+" is LXC container "+teamtest.LxcContainer+"'s checkout", "ccy-teams lxc-add")
		r.lacksOutput(t)
		if len(r.Script.Rows) != 0 || len(r.seatsAsked) != 0 {
			t.Fatalf("%v: rows %q seats %v", args, r.Script.Rows, r.seatsAsked)
		}
	}
}

// S-B11: creating a team from the list. The checkout's standing is read again afterwards.
func (r *rig) choicesAfterCreate(fresh string) {
	calls := 0
	r.Fake.OnFunc(strings.Fields(r.DeclarePrefix("choices")), func(execx.Cmd) (execx.Result, error) {
		calls++
		out := alphaCovers + betaWiden
		if calls > 1 {
			out += fresh
		}
		return execx.Result{Stdout: []byte(out)}, nil
	})
}

func TestCreatingATeamFromTheListTakesIt(t *testing.T) {
	r := newRig(t, alphaCovers+betaWiden)
	r.choicesAfterCreate("CHOICE\tfresh\tcovers\t\"New\"\n")
	r.created = "fresh"
	r.Script.Fzf = []string{"|+ Create a team"}
	exit(t, r.run("--type=podman"), term.ExitOK, r)
	if r.creates != 1 || r.payload.String() != "1@fresh\n" {
		t.Fatalf("creates %d payload %q", r.creates, r.payload.String())
	}
	if len(r.Script.GumLog) != 0 {
		t.Fatalf("a covering new team asks no scope question: %v", r.Script.GumLog)
	}
}

func TestACreatedTeamThatDoesNotCoverTheCheckoutFailsFast(t *testing.T) {
	r := newRig(t, alphaCovers+betaWiden)
	r.choicesAfterCreate("CHOICE\tfresh\twiden\t-\n")
	r.created = "fresh"
	r.Script.Fzf = []string{"|+ Create a team"}
	exit(t, r.run("--type=podman"), term.ExitFailure, r)
	contains(t, "stderr", r.stderr(), "fresh", "does not cover "+repo, "nothing was started")
	if r.seatsAsked != nil {
		t.Fatalf("seats %v", r.seatsAsked)
	}
	r.lacksOutput(t)
}

func TestCancellingTheWizardReturnsToTheList(t *testing.T) {
	r := newRig(t, alphaCovers+betaWiden)
	r.createCode = term.ExitCancelled
	r.Script.Fzf = []string{"|+ Create a team", "|alpha"}
	exit(t, r.run("--type=podman"), term.ExitOK, r)
	if len(r.Script.Rows) != 2 || r.payload.String() != "1@alpha\n" {
		t.Fatalf("rounds %d payload %q", len(r.Script.Rows), r.payload.String())
	}
}

func TestAWizardThatFailsEndsTheRunWithItsCode(t *testing.T) {
	r := newRig(t, alphaCovers+betaWiden)
	r.createCode = term.ExitFailure
	r.Script.Fzf = []string{"|+ Create a team"}
	exit(t, r.run("--type=podman"), term.ExitFailure, r)
	r.lacksOutput(t)
}

// S-B13: Esc.
func TestEscOnTheListPrintsNothingAndSucceeds(t *testing.T) {
	r := newRig(t, alphaCovers+betaWiden)
	r.Script.Fzf = []string{"|ABORT"}
	exit(t, r.run("--type=podman"), term.ExitOK, r)
	contains(t, "stderr", r.stderr(), "No team chosen; nothing started.")
	r.lacksOutput(t)
	if r.seatsAsked != nil {
		t.Fatalf("seats %v", r.seatsAsked)
	}
}

// S-B12: the seat chooser, and leaving it.
func TestLeavingTheSeatChooserPrintsNothingAndSucceeds(t *testing.T) {
	r := newRig(t, alphaCovers+betaWiden)
	r.seatLeft["alpha"] = true
	r.Script.Fzf = []string{"|alpha"}
	exit(t, r.run("--type=host"), term.ExitOK, r)
	r.lacksOutput(t)
	if !slices.Equal(r.seatsAsked, []string{"alpha/host"}) {
		t.Fatalf("seats %v", r.seatsAsked)
	}
}

func TestASeatChooserFailureIsTheRunsExit(t *testing.T) {
	r := newRig(t, alphaCovers)
	r.seatCode = 78
	exit(t, r.run("--type=podman"), 78, r)
	r.lacksOutput(t)
}

// S-B12: a team alone in the given list goes straight to the seat chooser.
func TestATeamAloneInTheListSkipsTheListAndAsksOnlyForItsSeat(t *testing.T) {
	r := newRig(t, alphaCovers+betaWiden)
	exit(t, r.run("--type=podman", "beta"), term.ExitOK, r)
	if r.payload.String() != "1@beta\n" || len(r.Script.Rows) != 0 {
		t.Fatalf("payload %q rows %v", r.payload.String(), r.Script.Rows)
	}
	if r.Fake.Called("env PYTHONPATH=" + r.Host.RootDir + " python3 -P -m helpers.agent_bus.declare choices") {
		t.Fatal("a named team needs no coverage check")
	}
}

func TestSeatsInTheListAreKeptAndTeamsAloneAreResolvedInOrder(t *testing.T) {
	r := newRig(t, alphaCovers)
	exit(t, r.run("--type=podman", "3@alpha, beta,qa@gamma"), term.ExitOK, r)
	if r.payload.String() != "3@alpha,1@beta,qa@gamma\n" {
		t.Fatalf("payload %q", r.payload.String())
	}
	if !slices.Equal(r.seatsAsked, []string{"beta/podman"}) {
		t.Fatalf("seats %v", r.seatsAsked)
	}
}

func TestAListWithNothingToAskIsPrintedAsGiven(t *testing.T) {
	r := newRig(t, alphaCovers)
	exit(t, r.run("--type=podman", "3@alpha"), term.ExitOK, r)
	if r.payload.String() != "3@alpha\n" || r.seatsAsked != nil {
		t.Fatalf("payload %q seats %v", r.payload.String(), r.seatsAsked)
	}
}

func TestLeavingTheSeatChooserOfAListedTeamStartsNothing(t *testing.T) {
	r := newRig(t, alphaCovers)
	r.seatLeft["beta"] = true
	exit(t, r.run("--type=podman", "3@alpha,beta"), term.ExitOK, r)
	r.lacksOutput(t)
}

func TestAnEmptyItemInTheListIsRefused(t *testing.T) {
	r := newRig(t, alphaCovers)
	exit(t, r.run("--type=podman", "3@alpha,,beta"), term.ExitUsage, r)
	contains(t, "stderr", r.stderr(), "empty")
	r.lacksOutput(t)
}

// fzf and gum are needed to ask; without them the person is told which play installs them.
func TestWithoutFzfTheListCannotBeShown(t *testing.T) {
	r := newRig(t, alphaCovers+betaWiden)
	r.Host.LookPath = func(name string) (string, error) {
		if name == "fzf" {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + name, nil
	}
	exit(t, r.run("--type=podman"), term.ExitFailure, r)
	contains(t, "stderr", r.stderr(), "fzf", "play-claude-yolo.yml")
	r.lacksOutput(t)
}

func TestAFailureToReadTheCheckoutIsReportedNotHidden(t *testing.T) {
	r := newRig(t, alphaCovers)
	r.OnErr(r.DeclarePrefix("choices"), "declare: boom\n", 78)
	exit(t, r.run("--type=podman"), 78, r)
	contains(t, "stderr", r.stderr(), "declare: boom")
	r.lacksOutput(t)
}

func TestNoTeamAtAllStillOffersToCreateOne(t *testing.T) {
	r := newRig(t, "")
	calls := 0
	r.Fake.OnFunc(strings.Fields(r.DeclarePrefix("choices")), func(execx.Cmd) (execx.Result, error) {
		calls++
		if calls == 1 {
			return execx.Result{}, nil
		}
		return execx.Result{Stdout: []byte("CHOICE\tfresh\tcovers\t-\n")}, nil
	})
	r.created = "fresh"
	r.Script.Fzf = []string{"|+ Create a team"}
	exit(t, r.run("--type=podman"), term.ExitOK, r)
	if r.payload.String() != "1@fresh\n" {
		t.Fatalf("payload %q", r.payload.String())
	}
	contains(t, "stderr", r.stderr(), "No team on this machine works on "+repo)
}
