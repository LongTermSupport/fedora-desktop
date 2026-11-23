package chooser

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// v2Team is a team live on teamchat in the rig's checkout.
const v2Team = "lts-v2"

// errPool is a pool that cannot be read.
var errPool = errors.New("the pool cannot be read")

// teamchatRig is the rig with teamchat's side: v2Team live, with seats, and the pool's teams.
type teamchatRig struct {
	*rig

	seats   map[string][]string
	liveErr error
	asked   []string // each team Live was asked about
}

func newTeamchatRig(t *testing.T, entries string, seats map[string][]string) *teamchatRig {
	t.Helper()
	r := &teamchatRig{rig: newRig(t, entries), seats: seats}
	r.c.Teamchat = &Teamchat{
		Live: func(_ context.Context, team string) (bool, error) {
			r.asked = append(r.asked, team)
			_, ok := r.seats[team]
			return ok, r.liveErr
		},
		Seats: func(team string) ([]string, error) { return r.seats[team], nil },
		Teams: func() ([]string, error) {
			var teams []string
			for team, s := range r.seats {
				if len(s) > 0 {
					teams = append(teams, team)
				}
			}
			slices.Sort(teams)
			return teams, nil
		},
	}
	return r
}

func TestATeamchatTeamNamedAloneTakesItsOnePoolSeat(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2"}})
	exit(t, r.run("--type=podman", v2Team), term.ExitOK, r.rig)
	if r.payload.String() != "2@"+v2Team+"\n" {
		t.Fatalf("payload %q", r.payload.String())
	}
	if r.seatsAsked != nil {
		t.Fatalf("agent-bus's seat chooser was asked: %v", r.seatsAsked)
	}
	contains(t, "stderr", r.stderr(), "Team "+v2Team+" is on teamchat: seat 2, this checkout's one seat of it.")
}

func TestATeamchatTeamWithSeveralSeatsAsksWhichOne(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2", "3"}})
	r.Script.Gum = []string{"CHOOSE:3"}
	exit(t, r.run("--type=podman", v2Team), term.ExitOK, r.rig)
	if r.payload.String() != "3@"+v2Team+"\n" {
		t.Fatalf("payload %q", r.payload.String())
	}
}

func TestLeavingTheTeamchatSeatQuestionStartsNothing(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2", "3"}})
	r.Script.Gum = []string{"CANCEL"}
	exit(t, r.run("--type=podman", v2Team), term.ExitOK, r.rig)
	contains(t, "stderr", r.stderr(), nothingChosen)
	r.lacksOutput(t)
}

func TestATeamchatTeamWithNoSeatInThisCheckoutIsRefused(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: nil})
	exit(t, r.run("--type=podman", v2Team), term.ExitConfig, r.rig)
	contains(t, "stderr", r.stderr(), "Team "+v2Team+" is on teamchat, and this checkout has no seat of it",
		"ask its coordinator for a seat issued to this checkout")
	r.lacksOutput(t)
}

func TestATeamchatTeamThatCannotBeDecidedIsAFailure(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2"}})
	r.liveErr = errPool
	exit(t, r.run("--type=podman", v2Team), term.ExitFailure, r.rig)
	contains(t, "stderr", r.stderr(), errPool.Error())
	r.lacksOutput(t)
}

// Task 5.1 step 6: a host launch (cc) takes a teamchat team's pool seat as a podman one does,
// and the owner's Matrix consent is asked for it as for an agent-bus host seat.
func TestAHostLaunchTakesATeamchatPoolSeat(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2"}})
	r.consentRig()
	r.Script.Gum = []string{"YES"}
	exit(t, r.run("--type=host", v2Team), term.ExitOK, r.rig)
	if r.payload.String() != "2@"+v2Team+"\n" || !slices.Equal(r.asked, []string{v2Team}) {
		t.Fatalf("payload %q, asked %v", r.payload.String(), r.asked)
	}
	if r.seatsAsked != nil {
		t.Fatalf("agent-bus's seat chooser was asked: %v", r.seatsAsked)
	}
	if !strings.Contains(strings.Join(r.Script.Headings, "\n"), "Matrix consent") {
		t.Fatalf("the host seat's Matrix consent was not asked: %q", r.Script.Headings)
	}
}

func TestAnAgentBusTeamNamedAloneStillGoesToItsSeatChooser(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2"}})
	exit(t, r.run("--type=podman", "alpha"), term.ExitOK, r.rig)
	if r.payload.String() != "1@alpha\n" || !slices.Equal(r.seatsAsked, []string{"alpha/podman"}) {
		t.Fatalf("payload %q seats %v", r.payload.String(), r.seatsAsked)
	}
}

func TestTheListShowsThisCheckoutsTeamchatTeams(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers+betaWiden, map[string][]string{v2Team: {"2"}})
	r.Script.Fzf = []string{"|" + v2Team}
	exit(t, r.run("--type=podman"), term.ExitOK, r.rig)
	rows := r.Script.Rows[0]
	if len(rows) != 4 {
		t.Fatalf("rows %q", rows)
	}
	contains(t, "row", rows[0], v2Team, "on teamchat, 1 seat here")
	contains(t, "row", rows[1], "alpha", "covers "+repo)
	if r.payload.String() != "2@"+v2Team+"\n" {
		t.Fatalf("payload %q", r.payload.String())
	}
	if r.seatsAsked != nil {
		t.Fatalf("agent-bus's seat chooser was asked: %v", r.seatsAsked)
	}
	if calls := r.Spawned(); calls["systemctl is-active agent-bus-hs@"+v2Team+".service"] != 0 {
		t.Fatalf("a teamchat team's homeserver was asked of systemd: %v", calls)
	}
}

func TestTheOnlyTeamBeingATeamchatOneIsTakenWithoutAList(t *testing.T) {
	r := newTeamchatRig(t, "", map[string][]string{v2Team: {"2"}})
	exit(t, r.run("--type=podman"), term.ExitOK, r.rig)
	if r.payload.String() != "2@"+v2Team+"\n" || len(r.Script.Rows) != 0 {
		t.Fatalf("payload %q rows %v", r.payload.String(), r.Script.Rows)
	}
}

func TestTeamsOnBothSystemsInOneLaunchAreRefused(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers+betaWiden, map[string][]string{v2Team: {"2"}})
	r.Script.Fzf = []string{"|" + v2Team + "&&alpha"}
	exit(t, r.run("--type=podman"), term.ExitUsage, r.rig)
	contains(t, "stderr", r.stderr(), "one session joins one system", "agent-bus: alpha", "teamchat: "+v2Team)
	r.lacksOutput(t)
}

func TestAHostListShowsThisCheckoutsTeamchatTeams(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers+betaWiden, map[string][]string{v2Team: {"2"}})
	r.Script.Fzf = []string{"|ABORT"}
	exit(t, r.run("--type=host"), term.ExitOK, r.rig)
	if !slices.ContainsFunc(r.Script.Rows[0], func(row string) bool { return strings.Contains(row, v2Team) }) {
		t.Fatalf("a host list did not offer %s: %q", v2Team, r.Script.Rows[0])
	}
}
