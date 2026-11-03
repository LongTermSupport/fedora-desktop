package chooser

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

const (
	betaCovers = "  - team: beta\n    description: Reviews the docs\n    repos:\n      - repo: example-org/proj\n"

	launchAnswer = "CHOOSE:Launch with these choices"
	changeAlpha  = "CHOOSE:Change team alpha (seat 1, worker)"
)

// summaryRig is a rig whose probe reads taken and roles; a seat of a team is taken when
// taken["<seat>@<team>"] is set.
type summaryRig struct {
	*rig

	taken      map[string]bool
	roles      map[string]string
	probeErr   map[string]error
	probedWith []string
}

func newSummaryRig(t *testing.T, entries string) *summaryRig {
	t.Helper()
	s := &summaryRig{rig: newRig(t, entries), taken: map[string]bool{}, roles: map[string]string{}, probeErr: map[string]error{}}
	s.c.Probe = func(_ context.Context, team, seat, memberType string) (bool, string, error) {
		s.probedWith = append(s.probedWith, seat+"@"+team+"/"+memberType)
		if err := s.probeErr[team]; err != nil {
			return false, "", err
		}
		role := s.roles[team]
		if role == "" {
			role = "worker"
		}
		return s.taken[seat+"@"+team], role, nil
	}
	return s
}

func (s *summaryRig) summary(args ...string) term.ExitCode {
	return s.c.Summary(context.Background(), args)
}

func (s *summaryRig) screen(n int) string { return s.Script.GumLog[n] }

// Accept-all: Enter on the first row launches with every default, no other screen and no question.
func TestAcceptingTheSummaryAsksNothingMore(t *testing.T) {
	s := newSummaryRig(t, alphaCovers+betaCovers)
	s.Script.Gum = []string{launchAnswer}
	exit(t, s.summary("--type=podman", "--token=main", "--ssh=/k/a", "--network=net", "--teams=1@alpha,1@beta"), term.ExitOK, s.rig)
	want := "TEAMS\t1@alpha,1@beta\nROLES\talpha=worker,beta=worker\nASK\tnone\n"
	if s.payload.String() != want {
		t.Fatalf("payload %q, want %q", s.payload.String(), want)
	}
	if len(s.Script.GumLog) != 1 || len(s.seatsAsked) != 0 {
		t.Fatalf("screens %d, seat questions %v", len(s.Script.GumLog), s.seatsAsked)
	}
	contains(t, "screen", s.screen(0), "Change token (main)", "Change SSH key (/k/a)", "Change network (net)",
		"Change team alpha (seat 1, worker)", "Change team beta (seat 1, worker)")
	if strings.Contains(s.screen(0), " - NEEDS AN ANSWER") {
		t.Fatalf("a line is marked though nothing is wrong: %s", s.screen(0))
	}
	if !strings.Contains(s.screen(0), "--selected Launch with these choices") {
		t.Fatalf("Enter is not on the launch row: %s", s.screen(0))
	}
	s.Script.HeadingAt(0, "New session › Launch summary")
	s.lacksStdout(t)
}

func (s *summaryRig) lacksStdout(t *testing.T) {
	t.Helper()
	if s.Out.Len() != 0 {
		t.Fatalf("a flow wrote on stdout: %q", s.Out.String())
	}
}

// A seat taken since the last launch is marked, Enter lands on it, and only that line is asked.
func TestATakenSeatIsMarkedAndOnlyThatLineIsAnswered(t *testing.T) {
	s := newSummaryRig(t, alphaCovers+betaCovers)
	s.taken["2@beta"] = true
	taken := "Change team beta (seat 2, worker) - NEEDS AN ANSWER: seat 2 is held by a running session"
	s.Script.Gum = []string{"CHOOSE:" + taken, launchAnswer}
	exit(t, s.summary("--type=podman", "--token=main", "--ssh=", "--network=", "--teams=1@alpha,2@beta"), term.ExitOK, s.rig)
	contains(t, "first screen", s.screen(0), taken, "--selected "+taken)
	if !slices.Equal(s.seatsAsked, []string{"beta/podman"}) {
		t.Fatalf("seats asked %v: only the marked team is asked", s.seatsAsked)
	}
	if !strings.HasPrefix(s.payload.String(), "TEAMS\t1@alpha,1@beta\n") {
		t.Fatalf("payload %q", s.payload.String())
	}
	if strings.Contains(s.screen(1), " - NEEDS AN ANSWER") {
		t.Fatalf("the answered line is still marked: %s", s.screen(1))
	}
}

// Launching with a marked line is refused in words, and the summary is shown again.
func TestLaunchingWithAMarkedLineIsRefusedAndShownAgain(t *testing.T) {
	s := newSummaryRig(t, alphaCovers)
	s.taken["1@alpha"] = true
	s.Script.Gum = []string{launchAnswer, "CHOOSE:Change team alpha (seat 1, worker) - NEEDS AN ANSWER: seat 1 is held by a running session", launchAnswer}
	s.taken["1@alpha"] = true
	s.c.Seat = func(context.Context, ui.Heading, string, string) (string, term.ExitCode) {
		return "2@alpha", term.ExitOK
	}
	exit(t, s.summary("--type=podman", "--token=t", "--ssh=", "--network=", "--teams=1@alpha"), term.ExitOK, s.rig)
	contains(t, "stderr", s.stderr(), "alpha needs an answer before the launch")
	if !strings.HasPrefix(s.payload.String(), "TEAMS\t2@alpha\n") {
		t.Fatalf("payload %q", s.payload.String())
	}
}

// A team whose scope does not cover this repo is marked; the scope question is the one asked, then the
// seat; the owner's answer is not asked again in the same launch.
func TestAScopeNoLongerCoveringTheRepoIsMarkedAndAnswered(t *testing.T) {
	s := newSummaryRig(t, alphaCovers+betaWiden)
	marked := "Change team beta (seat 1, worker) - NEEDS AN ANSWER: beta does not cover " + repo
	s.Script.Gum = []string{"CHOOSE:" + marked, joinBeta, launchAnswer}
	exit(t, s.summary("--type=podman", "--token=t", "--ssh=", "--network=", "--teams=1@alpha,1@beta"), term.ExitOK, s.rig)
	contains(t, "first screen", s.screen(0), marked)
	if !slices.Equal(s.seatsAsked, []string{"beta/podman"}) {
		t.Fatalf("seats asked %v", s.seatsAsked)
	}
	if strings.Contains(s.screen(2), " - NEEDS AN ANSWER") {
		t.Fatalf("an answered scope is asked again: %s", s.screen(2))
	}
}

// Leaving a team out at the scope question drops it from the launch.
func TestLeavingATeamOutAtTheScopeQuestionDropsIt(t *testing.T) {
	s := newSummaryRig(t, alphaCovers+betaWiden)
	s.Script.Gum = []string{"CHOOSE:Change team beta (seat 1, worker) - NEEDS AN ANSWER: beta does not cover " + repo, "CHOOSE:Leave beta out", launchAnswer}
	exit(t, s.summary("--type=podman", "--token=t", "--ssh=", "--network=", "--teams=1@alpha,1@beta"), term.ExitOK, s.rig)
	if !strings.HasPrefix(s.payload.String(), "TEAMS\t1@alpha\nROLES\talpha=worker\n") || len(s.seatsAsked) != 0 {
		t.Fatalf("payload %q seats %v", s.payload.String(), s.seatsAsked)
	}
}

// A remembered role that is not the member's now is marked.
func TestARoleThatChangedSinceIsMarked(t *testing.T) {
	s := newSummaryRig(t, alphaCovers)
	s.roles["alpha"] = "worker"
	s.Script.Gum = []string{"CHOOSE:Change team alpha (seat 1, worker) - NEEDS AN ANSWER: its role is now worker (was coordinator)", launchAnswer}
	exit(t, s.summary("--type=podman", "--token=t", "--ssh=", "--network=", "--teams=1@alpha", "--roles=alpha=coordinator"), term.ExitOK, s.rig)
	if !slices.Equal(s.seatsAsked, []string{"alpha/podman"}) || !strings.Contains(s.payload.String(), "ROLES\talpha=worker\n") {
		t.Fatalf("seats %v payload %q", s.seatsAsked, s.payload.String())
	}
}

// A team that excludes the repo is left out and said.
func TestATeamThatExcludesTheRepoIsLeftOutAndSaid(t *testing.T) {
	s := newSummaryRig(t, alphaCovers+gammaExclude)
	s.Script.Gum = []string{"CHOOSE:Change team gamma (seat 1, role unknown) - NEEDS AN ANSWER: gamma excludes " + repo + " on purpose", launchAnswer}
	exit(t, s.summary("--type=podman", "--token=t", "--ssh=", "--network=", "--teams=1@alpha,1@gamma"), term.ExitOK, s.rig)
	contains(t, "stderr", s.stderr(), "gamma excludes "+repo+" on purpose", "left out of this launch")
	if !strings.HasPrefix(s.payload.String(), "TEAMS\t1@alpha\n") {
		t.Fatalf("payload %q", s.payload.String())
	}
}

// A seat that cannot be checked is marked with the reason.
func TestASeatThatCannotBeCheckedIsMarkedWithTheReason(t *testing.T) {
	s := newSummaryRig(t, alphaCovers)
	s.probeErr["alpha"] = errors.New("the bus said no")
	s.c.Seat = func(context.Context, ui.Heading, string, string) (string, term.ExitCode) {
		delete(s.probeErr, "alpha")
		s.seatsAsked = append(s.seatsAsked, "alpha/podman")
		return "1@alpha", term.ExitOK
	}
	s.Script.Gum = []string{"CHOOSE:Change team alpha (seat 1, role unknown) - NEEDS AN ANSWER: seat 1 could not be checked: the bus said no", launchAnswer}
	exit(t, s.summary("--type=podman", "--token=t", "--ssh=", "--network=", "--teams=1@alpha"), term.ExitOK, s.rig)
	if !slices.Equal(s.seatsAsked, []string{"alpha/podman"}) {
		t.Fatalf("seats %v", s.seatsAsked)
	}
}

// Change one of token, SSH key, network: the launcher asks that one question, so the summary
// ends naming it and keeps the teams as they stand.
func TestChangingTheTokenEndsTheSummaryNamingIt(t *testing.T) {
	for _, c := range []struct{ row, ask string }{
		{"Change token (main)", "token"}, {"Change SSH key (none)", "ssh"}, {"Change network (none)", "network"},
	} {
		s := newSummaryRig(t, alphaCovers)
		s.Script.Gum = []string{"CHOOSE:" + c.row}
		exit(t, s.summary("--type=podman", "--token=main", "--ssh=", "--network=", "--teams=1@alpha"), term.ExitOK, s.rig)
		if want := "TEAMS\t1@alpha\nROLES\talpha=worker\nASK\t" + c.ask + "\n"; s.payload.String() != want {
			t.Errorf("%s: payload %q, want %q", c.row, s.payload.String(), want)
		}
	}
}

// Changing a single team line from a summary of several asks that team only.
func TestChangingOneOfSeveralTeamsAsksThatTeamOnly(t *testing.T) {
	s := newSummaryRig(t, alphaCovers+betaCovers)
	s.Script.Gum = []string{changeAlpha, launchAnswer}
	exit(t, s.summary("--type=podman", "--token=t", "--ssh=", "--network=", "--teams=1@alpha,1@beta"), term.ExitOK, s.rig)
	if !slices.Equal(s.seatsAsked, []string{"alpha/podman"}) {
		t.Fatalf("seats %v", s.seatsAsked)
	}
	s.Script.HeadingAt(0, "New session › Launch summary")
}

// Leaving the summary starts nothing.
func TestLeavingTheSummaryStartsNothing(t *testing.T) {
	s := newSummaryRig(t, alphaCovers)
	s.Script.Gum = []string{"ESC"}
	exit(t, s.summary("--type=podman", "--token=t", "--ssh=", "--network=", "--teams=1@alpha"), term.ExitOK, s.rig)
	s.lacksOutput(t)
	contains(t, "stderr", s.stderr(), "Nothing started")
}

// A launch without teams, and one where --teams was given, show no team lines.
func TestNoTeamsAndSkippedTeamsShowNoTeamLines(t *testing.T) {
	s := newSummaryRig(t, alphaCovers)
	s.Script.Gum = []string{launchAnswer}
	exit(t, s.summary("--type=podman", "--token=t", "--ssh=", "--network=", "--skip-teams"), term.ExitOK, s.rig)
	if strings.Contains(s.screen(0), "Change team") || s.payload.String() != "TEAMS\t\nROLES\t\nASK\tnone\n" {
		t.Fatalf("screen %q payload %q", s.screen(0), s.payload.String())
	}
}

func TestSummaryNeedsATerminalAndKnownArguments(t *testing.T) {
	s := newSummaryRig(t, alphaCovers)
	s.Host.IsTerminal = func(any) bool { return false }
	exit(t, s.summary("--type=podman", "--token=t"), term.ExitUsage, s.rig)
	s = newSummaryRig(t, alphaCovers)
	for _, args := range [][]string{{}, {"--type=lxc"}, {"--type=podman", "--bogus"}, {"--type=podman", "--teams=alpha"}} {
		exit(t, s.summary(args...), term.ExitUsage, s.rig)
	}
	s.lacksOutput(t)
}
