package chooser

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/playlock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
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
	// seatTrails is the heading each seat question was given, as its trail.
	seatTrails []string
	seatLeft   map[string]bool
	seatCode   term.ExitCode
	created    string
	createCode term.ExitCode
	creates    int
	applied    [][]string
	applyErr   error
	// locks is each hold of the play lock asked for; inLock is whether one is held now, and
	// lockErr, when set, is a lock not taken (fn is not run).
	locks   []string
	inLock  bool
	lockErr error
	// createWrites is the host_vars entry the wizard adds when it creates a team, so the reads
	// after it see the new team.
	createWrites string
}

// The teams are host_vars entries whose standing to example-org/proj is the one each is named for.
const (
	alphaCovers  = "  - team: alpha\n    description: Builds the site\n    repos:\n      - repo: example-org/proj\n"
	betaWiden    = "  - team: beta\n    org: other-org\n    description: Reviews the docs\n"
	gammaExclude = "  - team: gamma\n    org: example-org\n    exclude: [\"proj\"]\n"
)

// teamsVars is a host_vars declaring the entries.
func teamsVars(entries string) string {
	return "agent_bus_address: 192.0.2.10\nagent_bus_teams:\n" + entries
}

// newRig is the rig with the entries (alphaCovers, betaWiden...) declared in the host_vars.
func newRig(t *testing.T, entries string) *rig {
	t.Helper()
	w := teamtest.New(t)
	console := teamtest.Interactive(t, w)
	w.On("git rev-parse --is-inside-work-tree", "true\n", 0)
	w.On("git rev-parse --show-toplevel", w.Checkout()+"\n", 0)
	w.On("git remote get-url origin", "git@github.com:Example-Org/proj.git\n", 0)
	w.WriteHostVars(t, teamsVars(entries))
	for _, team := range []string{"alpha", "beta", "gamma", "delta", "fresh"} {
		w.On("systemctl is-active agent-bus-hs@"+team+".service", "active\n", 0)
	}
	r := &rig{World: w, Console: console, payload: &bytes.Buffer{}, seatLeft: map[string]bool{}}
	r.c = New(w.Host,
		func(_ context.Context, h ui.Heading, team, memberType string) (string, term.ExitCode) {
			r.seatsAsked = append(r.seatsAsked, team+"/"+memberType)
			r.seatTrails = append(r.seatTrails, h.Trail())
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
			if r.createWrites != "" && r.createCode == term.ExitOK {
				w.WriteHostVars(t, teamsVars(entries+r.createWrites))
			}
			return r.created, r.createCode
		},
		func(_ context.Context, teams []string) error {
			r.applied = append(r.applied, teams)
			return r.applyErr
		},
		func(ctx context.Context, purpose string, fn func(context.Context) error) error {
			r.locks = append(r.locks, purpose)
			if r.lockErr != nil {
				return r.lockErr
			}
			r.inLock = true
			defer func() { r.inLock = false }()
			return fn(ctx)
		})
	r.c.Payload = r.payload
	return r
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
	// The standings are the host_vars' own: read in-process, so no process ran for them.
	if calls := r.Spawned(); calls["declare.py choices"] != 0 {
		t.Fatalf("coverage came from a process: %v", calls)
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
	r := newRig(t, alphaCovers+"  - team: delta\n    repos: !vault x\n    description: !vault x\n")
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
	if !r.Fake.Called("env PYTHONPATH=" + r.Host.RootDir + " /usr/bin/python3 -P -m helpers.agent_bus.declare add-repo --file " + r.Host.HostVars + " --team beta --repo " + repo) {
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

// The repository is added to the scope and the team applied in one hold of the play lock.
func TestTheAddAndItsApplyAreInOneHoldOfThePlayLock(t *testing.T) {
	r := newRig(t, alphaCovers+betaWiden)
	addInLock := false
	r.Fake.OnFunc(strings.Fields(r.DeclarePrefix("add-repo")), func(execx.Cmd) (execx.Result, error) {
		addInLock = r.inLock
		return execx.Result{Stdout: []byte("CHANGED\tteam beta trusts " + repo + "\n")}, nil
	})
	r.Script.Fzf = []string{"|beta"}
	r.Script.Gum = []string{"CHOOSE:Add " + repo + " to beta's scope"}
	exit(t, r.run("--type=podman"), term.ExitOK, r)
	if want := []string{"add " + repo + " to the scope of beta"}; !slices.Equal(r.locks, want) || !addInLock {
		t.Fatalf("holds %q, add in the hold %v", r.locks, addInLock)
	}
}

// The owner's defect: an add while another play run holds the lock, and the wait does not end in
// it. Nothing is added to the scope, nothing is applied or started.
func TestAPlayLockNotTakenAddsNothingAndStartsNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		code term.ExitCode
	}{
		{"gave up", fmt.Errorf("gave up: %w", playlock.ErrNotTaken), term.ExitFailure},
		{"stopped", fmt.Errorf("stopped: %w %w", playlock.ErrNotTaken, playlock.ErrStopped), term.ExitCancelled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, alphaCovers+betaWiden)
			r.OnDeclare("add-repo", "CHANGED\tteam beta trusts "+repo+"\n")
			r.lockErr = tc.err
			r.Script.Fzf = []string{"|beta"}
			r.Script.Gum = []string{"CHOOSE:Add " + repo + " to beta's scope"}
			exit(t, r.run("--type=podman"), tc.code, r)
			if r.Fake.Called(r.DeclarePrefix("add-repo")) || len(r.applied) != 0 || r.seatsAsked != nil {
				t.Fatalf("added, applied %v or asked seats %v without the lock", r.applied, r.seatsAsked)
			}
			contains(t, "stderr", r.stderr(), "ccy-teams: nothing was added to the scope of beta and nothing was started, "+
				"as the play lock was not free (above). Once the other play run has finished, run this again.")
			r.lacksOutput(t)
		})
	}
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
	if r.Fake.Called("env PYTHONPATH=" + r.Host.RootDir + " /usr/bin/python3 -P -m helpers.agent_bus.declare add-repo") {
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
	r := newRig(t, "  - team: alpha\n    port: 6167\n    description: Builds the site\n"+
		"  - team: beta\n    port: 6168\n  - team: gone\n    port: 6169\n    state: absent\n")
	r.OnNotARepository()
	r.Script.Fzf = []string{"|beta"}
	exit(t, r.run("--type=podman"), term.ExitOK, r)
	rows := strings.Join(r.Script.Rows[0], "\n")
	contains(t, "rows", rows, "alpha", "not a GitHub checkout", "Builds the site", "beta", "(no description)", "+ Create a team")
	if strings.Contains(rows, "gone") || strings.Contains(rows, "would add") {
		t.Fatalf("rows %s", rows)
	}
	// No repository, so no standing is asked: no row says what a team would add.
	if r.payload.String() != "1@beta\n" || len(r.Script.GumLog) != 0 {
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

// S-B11: creating a team from the list. The checkout's standing is read again afterwards, from
// the host_vars the wizard wrote fresh's entry into.
func (r *rig) choicesAfterCreate(fresh string) {
	r.createWrites = fresh
}

const (
	freshCovers = "  - team: fresh\n    description: New\n    repos:\n      - repo: example-org/proj\n"
	freshWidens = "  - team: fresh\n    org: other-org\n"
)

func TestCreatingATeamFromTheListTakesIt(t *testing.T) {
	r := newRig(t, alphaCovers+betaWiden)
	r.choicesAfterCreate(freshCovers)
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
	r.choicesAfterCreate(freshWidens)
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
	// A host_vars the reader refuses: a coverage check would have failed the run.
	r.WriteHostVars(t, "agent_bus_teams:\n  - team: beta\n    port: port\n")
	r.payload.Reset()
	exit(t, r.run("--type=podman", "beta"), term.ExitOK, r)
	if r.payload.String() != "1@beta\n" {
		t.Fatalf("a named team needs no coverage check: payload %q", r.payload.String())
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
	if want := []string{"New session › Team 2 of 3: beta"}; !slices.Equal(r.seatTrails, want) {
		t.Fatalf("seat headings %q, want %q", r.seatTrails, want)
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
	r.WriteHostVars(t, teamsVars(alphaCovers+"    port: port\n"))
	exit(t, r.run("--type=podman"), 78, r)
	contains(t, "stderr", r.stderr(), "declare: line 7: the port is not a number")
	r.lacksOutput(t)
}

func TestNoTeamAtAllStillOffersToCreateOne(t *testing.T) {
	r := newRig(t, "")
	r.choicesAfterCreate("  - team: fresh\n    repos:\n      - repo: example-org/proj\n")
	r.created = "fresh"
	r.Script.Fzf = []string{"|+ Create a team"}
	exit(t, r.run("--type=podman"), term.ExitOK, r)
	if r.payload.String() != "1@fresh\n" {
		t.Fatalf("payload %q", r.payload.String())
	}
	contains(t, "stderr", r.stderr(), "No team on this machine works on "+repo)
}
