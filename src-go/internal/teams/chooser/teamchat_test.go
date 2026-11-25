package chooser

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/seatchoice"
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
	// held is the session holding each seat part, for every team; a seat not in it is free.
	held map[string]string
	// poolErr fails Pool; reattachErr fails Reattach, which records each session it is asked for.
	poolErr     error
	reattachErr error
	reattached  []string
}

// ofKind is team's seats of kind, as system.Cmd answers them: a rig seat "N" is of every kind
// asked (the launch's own), "N:<kind>" of that kind alone.
func (r *teamchatRig) ofKind(team, kind string) []seatchoice.Seat {
	var pool []seatchoice.Seat
	for _, s := range r.seats[team] {
		name, fixed, ok := strings.Cut(s, ":")
		if ok && fixed != kind && kind != seatchoice.AnyKind {
			continue
		}
		// A seat of no fixed kind is of whichever kind is asked (podman when any is).
		of := kind
		if ok {
			of = fixed
		} else if kind == seatchoice.AnyKind {
			of = seatchoice.KindPodman
		}
		pool = append(pool, seatchoice.Seat{Name: name, Handle: "proj." + name + "+local." + of, HeldBy: r.held[name]})
	}
	return pool
}

func newTeamchatRig(t *testing.T, entries string, seats map[string][]string) *teamchatRig {
	t.Helper()
	r := &teamchatRig{rig: newRig(t, entries), seats: seats, held: map[string]string{}}
	r.c.Teamchat = &Teamchat{
		Pool: func(team, kind string) ([]seatchoice.Seat, error) { return r.ofKind(team, kind), r.poolErr },
		Reattach: func(_ context.Context, session string) error {
			r.reattached = append(r.reattached, session)
			return r.reattachErr
		},
		Live: func(_ context.Context, team string) (bool, error) {
			r.asked = append(r.asked, team)
			_, ok := r.seats[team]
			return ok, r.liveErr
		},
		SeatCount: func(team, kind string) (int, error) { return len(r.ofKind(team, kind)), nil },
		Handles: func(team, kind string) ([]string, error) {
			var handles []string
			for _, s := range r.ofKind(team, kind) {
				handles = append(handles, s.Handle)
			}
			return handles, nil
		},
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
	contains(t, "stderr", r.stderr(), "Team "+v2Team+" is on teamchat: seat 2, this checkout's one podman seat of it.")
}

// row is the chooser's row of a podman seat of the rig.
func (r *teamchatRig) row(seat string) string { return r.rowOf(seat, seatchoice.KindPodman) }

// rowOf is the chooser's row of a seat of kind of the rig.
func (r *teamchatRig) rowOf(seat, kind string) string {
	return seatchoice.Row(seatchoice.Seat{Name: seat, Handle: "proj." + seat + "+local." + kind, HeldBy: r.held[seat]})
}

// gumDefault is the default the last gum question offered (its --selected).
func (r *teamchatRig) gumDefault(t *testing.T) string {
	t.Helper()
	if len(r.Script.GumLog) == 0 {
		t.Fatal("no gum question was asked")
	}
	last := r.Script.GumLog[len(r.Script.GumLog)-1]
	_, after, ok := strings.Cut(last, "--selected ")
	if !ok {
		t.Fatalf("the question offered no default: %s", last)
	}
	return after
}

func TestATeamchatTeamWithSeveralSeatsAsksWhichOne(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2", "3"}})
	r.Script.Gum = []string{"CHOOSE:" + r.row("3")}
	exit(t, r.run("--type=podman", v2Team), term.ExitOK, r.rig)
	if r.payload.String() != "3@"+v2Team+"\n" {
		t.Fatalf("payload %q", r.payload.String())
	}
}

// Issue 423: the owner's `cc --teams <team>` in a checkout whose one seat a running session
// holds took that seat, and the launch exited 75. A held seat is never taken: it is offered as
// the re-attach of its session, and the refusal names the seats and every way on.
func TestATeamchatTeamsOneSeatHeldIsNeverTaken(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2"}})
	r.held["2"] = "ccy-proj"
	r.Script.Gum = []string{"CANCEL"}
	exit(t, r.run("--type=podman", v2Team), term.ExitOK, r.rig)
	r.lacksOutput(t)
	contains(t, "stderr", r.stderr(), "Every seat of "+v2Team+" in this checkout is held by a running session")
	if strings.Contains(r.stderr(), "this checkout's one podman seat of it") {
		t.Fatalf("the held seat was taken:\n%s", r.stderr())
	}
	if got := r.gumDefault(t); !strings.HasPrefix(got, seatchoice.NewRow(v2Team, seatchoice.KindPodman)) {
		t.Fatalf("with no seat free the default is a new seat: %s", got)
	}
	contains(t, "the question", r.Script.GumLog[0], r.row("2"))
}

// The "no free seat" outcome is one seam (Teamchat.NewSeat): unwired, the new-seat row says the
// command that adds the seat and every way on, and ends the launch (78); wired (issue 416's
// seat add), its seat is the launch's.
func TestTheNewSeatRowGoesThroughTheNewSeatSeam(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2"}})
	r.held["2"] = "ccy-proj"
	r.Script.Gum = []string{"CHOOSE:" + seatchoice.NewRow(v2Team, seatchoice.KindPodman)}
	exit(t, r.run("--type=podman", v2Team), term.ExitConfig, r.rig)
	r.lacksOutput(t)
	contains(t, "stderr", r.stderr(), "a new podman seat of "+v2Team+" is added by ccy-teams seats add "+v2Team,
		"re-attach session ccy-proj (seat 2): ccy-sessions", "a new seat: ccy-teams seats add "+v2Team)

	r = newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2"}})
	r.held["2"] = "ccy-proj"
	var asked []string
	r.c.Teamchat.NewSeat = func(_ context.Context, team, memberType string) (string, term.ExitCode) {
		asked = append(asked, team+"/"+memberType)
		return "4@" + team, term.ExitOK
	}
	r.Script.Gum = []string{"CHOOSE:" + seatchoice.NewRow(v2Team, seatchoice.KindPodman)}
	exit(t, r.run("--type=podman", v2Team), term.ExitOK, r.rig)
	if r.payload.String() != "4@"+v2Team+"\n" || !slices.Equal(asked, []string{v2Team + "/podman"}) {
		t.Fatalf("payload %q, asked %v", r.payload.String(), asked)
	}
}

// Issue 423, v1's floor: every seat is listed, free and held, and the default is the lowest
// free seat, whatever order the pool lists them in.
func TestTheSeatsAreListedWithTheLowestFreeAsDefault(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"5", "2", "3"}})
	r.held["2"] = "ccy-proj"
	r.Script.Gum = []string{"CHOOSE:" + r.row("3")}
	exit(t, r.run("--type=podman", v2Team), term.ExitOK, r.rig)
	if r.payload.String() != "3@"+v2Team+"\n" {
		t.Fatalf("payload %q", r.payload.String())
	}
	if got := r.gumDefault(t); !strings.HasPrefix(got, r.row("3")) {
		t.Fatalf("the default is not the lowest free seat, 3: %s", got)
	}
	log := r.Script.GumLog[len(r.Script.GumLog)-1]
	for _, seat := range []string{"2", "3", "5"} {
		contains(t, "the question", log, r.row(seat))
	}
	if strings.Index(log, r.row("2")) > strings.Index(log, r.row("5")) {
		t.Fatalf("the seats are not in seat order: %s", log)
	}
}

// Issue 423: choosing a held seat re-attaches the session holding it and starts nothing new.
func TestChoosingAHeldSeatReattachesItsSession(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2", "3"}})
	r.held["2"] = "ccy-proj"
	r.Script.Gum = []string{"CHOOSE:" + r.row("2")}
	exit(t, r.run("--type=podman", v2Team), term.ExitOK, r.rig)
	r.lacksOutput(t)
	if !slices.Equal(r.reattached, []string{"ccy-proj"}) {
		t.Fatalf("re-attached %v, want [ccy-proj]", r.reattached)
	}
	contains(t, "stderr", r.stderr(), "re-attaching it; nothing new is started")
}

// A re-attach that cannot be made (open in another terminal, not running) is refused as held
// (75), naming the seats and each way on, with the host launcher's own command for cc.
func TestAReattachThatCannotBeMadeNamesTheWaysOn(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2", "3"}})
	r.held["2"] = "cc-proj"
	r.reattachErr = errors.New("'cc-proj' is open in another terminal")
	r.consentRig()
	r.Script.Gum = []string{"CHOOSE:" + r.rowOf("2", seatchoice.KindHost)}
	exit(t, r.run("--type=host", v2Team), term.ExitBusy, r.rig)
	r.lacksOutput(t)
	contains(t, "stderr", r.stderr(), "'cc-proj' is open in another terminal",
		"take free seat 3: cc --teams 3@"+v2Team, "re-attach session cc-proj (seat 2): ccy-sessions")
}

// A pool whose holders cannot be read is a failure, never a seat taken blind.
func TestAPoolThatCannotBeReadTakesNoSeat(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2"}})
	r.poolErr = errPool
	exit(t, r.run("--type=podman", v2Team), term.ExitFailure, r.rig)
	contains(t, "stderr", r.stderr(), errPool.Error())
	r.lacksOutput(t)
}

// With no person to ask, a teamchat team named alone prompts nothing: the refusal names its
// seats, which are held, and the command for each way on.
func TestNoTerminalNamesTheWaysOnForATeamchatTeamAlone(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2", "3"}})
	r.held["2"] = "ccy-proj"
	r.Host.IsTerminal = func(any) bool { return false }
	exit(t, r.run("--type=podman", v2Team), term.ExitUsage, r.rig)
	r.lacksOutput(t)
	if len(r.Script.GumLog) != 0 {
		t.Fatalf("a question was asked with no terminal: %v", r.Script.GumLog)
	}
	contains(t, "stderr", r.stderr(), "name the seats", "take free seat 3: ccy --teams 3@"+v2Team,
		"re-attach session ccy-proj (seat 2)")
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
	contains(t, "stderr", r.stderr(), "Team "+v2Team+" is on teamchat, and this checkout has no podman seat of it",
		"ccy-teams seats add "+v2Team)
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
	contains(t, "stderr", r.stderr(), "proj.2+local.host joins its team")
}

// A host seat named in full on a teamchat team takes its handle from this checkout's pool; a
// seat the pool does not have is refused (78) before anything is asked.
func TestAHostTeamchatSeatNamedInFullTakesItsHandleFromThePool(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2", "3"}})
	r.consentRig()
	r.Script.Gum = []string{"YES"}
	exit(t, r.run("--type=host", "3@"+v2Team), term.ExitOK, r.rig)
	contains(t, "stderr", r.stderr(), "proj.3+local.host joins its team")
	if r.seatsAsked != nil {
		t.Fatalf("agent-bus's seat chooser was asked: %v", r.seatsAsked)
	}

	r = newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2"}})
	r.consentRig()
	exit(t, r.run("--type=host", "9@"+v2Team), term.ExitConfig, r.rig)
	contains(t, "stderr", r.stderr(), "9@"+v2Team+" is not one of this checkout's teamchat host seats of "+v2Team)
	r.lacksOutput(t)
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
	contains(t, "row", rows[0], v2Team, "on teamchat, 1 podman seat here")
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

// wireNewSeat wires a NewSeat that records each ask and answers seat (or code when seat is "").
func (r *teamchatRig) wireNewSeat(seat string, code term.ExitCode) *[]string {
	var asked []string
	r.c.Teamchat.NewSeat = func(_ context.Context, team, memberType string) (string, term.ExitCode) {
		asked = append(asked, team+"/"+memberType)
		if seat == "" {
			return "", code
		}
		return seat + "@" + team, term.ExitOK
	}
	return &asked
}

// Register R5: a bare `ccy --teams <team>` with no seat in this checkout adds one (the seat add,
// wired as NewSeat) and launches it, saying so on stderr; it no longer ends at 78.
func TestABareTeamWithNoSeatAddsOne(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: nil})
	asked := r.wireNewSeat("1", term.ExitOK)
	exit(t, r.run("--type=podman", v2Team), term.ExitOK, r.rig)
	if r.payload.String() != "1@"+v2Team+"\n" || !slices.Equal(*asked, []string{v2Team + "/podman"}) {
		t.Fatalf("payload %q, asked %v", r.payload.String(), *asked)
	}
	contains(t, "stderr", r.stderr(), "this checkout has no podman seat of it",
		"Adding a new podman seat of "+v2Team+" to this checkout (ccy-teams seats add "+v2Team+")")
}

// Register R5: a seat add that fails ends the launch with its code; nothing is launched.
func TestASeatAddThatFailsEndsTheLaunch(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: nil})
	r.wireNewSeat("", term.ExitConfig)
	exit(t, r.run("--type=podman", v2Team), term.ExitConfig, r.rig)
	r.lacksOutput(t)
}

// Register R55: a cc launch is offered the host seats only. A team whose only seat here is a
// podman one has no seat for cc: the way on is a host seat's add, and the podman seat is never
// taken; a ccy launch beside a host seat takes its podman one.
func TestALaunchIsOfferedOnlyItsOwnKindsSeats(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2:podman"}})
	r.consentRig()
	exit(t, r.run("--type=host", v2Team), term.ExitConfig, r.rig)
	r.lacksOutput(t)
	contains(t, "stderr", r.stderr(), "this checkout has no host seat of it", "ccy-teams seats add "+v2Team+" --host")

	r = newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2:host", "3"}})
	exit(t, r.run("--type=podman", v2Team), term.ExitOK, r.rig)
	if r.payload.String() != "3@"+v2Team+"\n" {
		t.Fatalf("payload %q", r.payload.String())
	}
	contains(t, "stderr", r.stderr(), "seat 3, this checkout's one podman seat of it")
}

// Register R55: a seat named for cc that is a podman seat is refused naming its kind and ccy,
// before anything is asked.
func TestAHostLaunchRefusesANamedPodmanSeat(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2:podman", "3:host"}})
	r.consentRig()
	exit(t, r.run("--type=host", "2@"+v2Team), term.ExitConfig, r.rig)
	r.lacksOutput(t)
	contains(t, "stderr", r.stderr(), "2@"+v2Team+" is a podman seat (proj.2+local.podman)", "ccy --teams 2@"+v2Team)

	r = newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2:podman"}})
	r.consentRig()
	exit(t, r.run("--type=host", "9@"+v2Team), term.ExitConfig, r.rig)
	contains(t, "stderr", r.stderr(), "9@"+v2Team+" is not one of this checkout's teamchat host seats of "+v2Team+" (it has: none)")

	// A seat of a kind neither launcher takes is refused naming that kind, not as an unknown seat.
	r = newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"4:lxc", "3:host"}})
	r.consentRig()
	exit(t, r.run("--type=host", "4@"+v2Team), term.ExitConfig, r.rig)
	r.lacksOutput(t)
	contains(t, "stderr", r.stderr(), "4@"+v2Team+" is a lxc seat (proj.4+local.lxc)", "neither ccy nor cc launches it")
}

// Register R55: with nobody to ask, the ways on name the launch's own kind's seats only.
func TestNoTerminalNamesOnlyTheLaunchKindsSeats(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2:podman", "3"}})
	r.Host.IsTerminal = func(any) bool { return false }
	exit(t, r.run("--type=host", v2Team), term.ExitUsage, r.rig)
	contains(t, "stderr", r.stderr(), "take free seat 3: cc --teams 3@"+v2Team, "ccy-teams seats add "+v2Team+" --host")
	if strings.Contains(r.stderr(), "2@"+v2Team) {
		t.Fatalf("a podman seat was offered to cc:\n%s", r.stderr())
	}
}

// Register R55: the list counts the launch's own kind's seats, and a team with none of them is
// still a teamchat team (never agent-bus's).
func TestTheListCountsTheLaunchKindsSeats(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2:podman"}})
	r.Script.Fzf = []string{"|ABORT"}
	exit(t, r.run("--type=host"), term.ExitOK, r.rig)
	if !slices.ContainsFunc(r.Script.Rows[0], func(row string) bool {
		return strings.Contains(row, v2Team) && strings.Contains(row, "on teamchat, no host seat here")
	}) {
		t.Fatalf("rows %q", r.Script.Rows[0])
	}
}
