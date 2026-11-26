package chooser

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

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
	// dead is why each team whose bus does not answer does not (Answers); answersErr fails it.
	// Answers is asked from one goroutine per team, so these are only read.
	dead       map[string]string
	answersErr error
	// lastRan is when each seat part last ran in the checkout (Plan 00265 R90); absent is never.
	lastRan map[string]int64
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
	r := &teamchatRig{rig: newRig(t, entries), seats: seats, held: map[string]string{}, dead: map[string]string{}, lastRan: map[string]int64{}}
	r.c.Teamchat = &Teamchat{
		LastRan: func(_, _, seat string) (int64, error) { return r.lastRan[seat], nil },
		Answers: func(_ context.Context, team string) (string, error) { return r.dead[team], r.answersErr },
		Pool:    func(team, kind string) ([]seatchoice.Seat, error) { return r.ofKind(team, kind), r.poolErr },
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
	contains(t, "stderr", r.stderr(), "Team "+v2Team+" is on teamchat: this checkout's one podman seat of it.")
}

// Plan 00265 R90: of several free seats, the one this checkout last worked in is taken and
// resumed without a question, and said in words; with none ever run, one is taken (they are the
// same); two with a conversation are a genuine choice, asked in words, the newest the default.
func TestTheSeatThisCheckoutWorkedInIsTakenWithoutAQuestion(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2", "3", "4"}})
	r.lastRan["3"] = r.c.Now().UnixMilli() - 2*60*60*1000
	exit(t, r.run("--type=podman", v2Team), term.ExitOK, r.rig)
	if r.payload.String() != "3@"+v2Team+"\n" || len(r.Script.GumLog) != 0 {
		t.Fatalf("payload %q, questions %v", r.payload.String(), r.Script.GumLog)
	}
	contains(t, "stderr", r.stderr(), "resuming this checkout's seat of it that last ran 2 hours ago")

	r = newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2", "3"}})
	exit(t, r.run("--type=podman", v2Team), term.ExitOK, r.rig)
	if r.payload.String() != "2@"+v2Team+"\n" || len(r.Script.GumLog) != 0 {
		t.Fatalf("none ever ran: payload %q, questions %v", r.payload.String(), r.Script.GumLog)
	}

	r = newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2", "3"}})
	r.lastRan["2"] = r.c.Now().UnixMilli() - 3*24*60*60*1000
	r.lastRan["3"] = r.c.Now().UnixMilli() - 2*60*60*1000
	r.Script.Gum = []string{"CHOOSE:" + r.row("2")}
	exit(t, r.run("--type=podman", v2Team), term.ExitOK, r.rig)
	if r.payload.String() != "2@"+v2Team+"\n" {
		t.Fatalf("a genuine choice: payload %q", r.payload.String())
	}
	if got := r.gumDefault(t); !strings.HasPrefix(got, "A seat of "+v2Team+": last ran 2 hours ago") {
		t.Fatalf("the default is not the newest: %s", got)
	}
	if log := r.Script.GumLog[0]; strings.Contains(log, "proj.") || strings.Contains(log, "seat 2") {
		t.Fatalf("the question names a seat: %s", log)
	}
}

// row is the chooser's row of a podman seat of the rig.
func (r *teamchatRig) row(seat string) string { return r.rowOf(seat, seatchoice.KindPodman) }

// rowOf is the chooser's row of a seat of kind of the rig.
func (r *teamchatRig) rowOf(seat, kind string) string {
	return seatchoice.Row(seatchoice.Seat{Name: seat, Handle: "proj." + seat + "+local." + kind, HeldBy: r.held[seat], LastRanMS: r.lastRan[seat]}, v2Team, r.c.Now().UnixMilli())
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
	r.lastRan["2"] = r.c.Now().UnixMilli() - 60*60*1000
	r.lastRan["3"] = r.c.Now().UnixMilli() - 3*60*60*1000
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
		"re-attach session ccy-proj: ccy-sessions", "a new seat: ccy-teams seats add "+v2Team)

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

// Issue 423, v1's floor: every seat is listed, free and held, in seat order whatever order the
// pool lists them in; the default is the free seat that last ran here (Plan 00265 R90).
func TestTheSeatsAreListedWithTheLastRunAsDefault(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"5", "2", "3"}})
	r.held["2"] = "ccy-proj"
	r.lastRan["3"] = r.c.Now().UnixMilli() - 60*60*1000
	r.lastRan["5"] = r.c.Now().UnixMilli() - 3*60*60*1000
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
		"take a free seat: run cc --teams "+v2Team+" at a terminal", "re-attach session cc-proj: ccy-sessions")
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
	contains(t, "stderr", r.stderr(), "to take is a choice, and there is no terminal", "take a free seat: run ccy --teams "+v2Team+" at a terminal",
		"re-attach session ccy-proj")
}

func TestLeavingTheTeamchatSeatQuestionStartsNothing(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2", "3"}})
	r.lastRan["2"] = r.c.Now().UnixMilli() - 60*60*1000
	r.lastRan["3"] = r.c.Now().UnixMilli() - 3*60*60*1000
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
	// Seats in two teams, so the bare list is shown (one would be taken without it).
	r := newTeamchatRig(t, alphaCovers+betaWiden, map[string][]string{v2Team: {"2"}, "lts-other": {"1", "3"}})
	r.Script.Fzf = []string{"|" + v2Team}
	exit(t, r.run("--type=podman"), term.ExitOK, r.rig)
	rows := r.Script.Rows[0]
	if len(rows) != 5 {
		t.Fatalf("rows %q", rows)
	}
	i := slices.IndexFunc(rows, func(row string) bool { return strings.HasPrefix(row, v2Team) })
	if i < 0 {
		t.Fatalf("no row for %s: %q", v2Team, rows)
	}
	contains(t, "row", rows[i], v2Team, "on teamchat, 1 podman seat here")
	contains(t, "row", rows[2], "alpha", "covers "+repo)
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

// deadTeam is a team with a seat in the rig's checkout whose bus does not answer: retired, its
// server gone, or its bus stopped (issue 430).
const deadTeam = "lts-retired"

// deadWhy is what the rig's Answers says of deadTeam.
const deadWhy = "its bus does not answer: its homeserver at https://127.0.0.1:8448 refused the probe (connection refused)"

// Issue 430 (the owner: "when selecting team I'm presented with lists of dead teams - this MUST
// be fixed"): a teamchat team whose bus does not answer is never a row of the list, so it cannot
// be chosen; it is named above the list in words, with why.
func TestATeamchatTeamWhoseBusDoesNotAnswerIsNotOffered(t *testing.T) {
	// Two live teams with seats here, so the list is shown (one alone is taken without it).
	r := newTeamchatRig(t, alphaCovers+betaWiden, map[string][]string{v2Team: {"2"}, "lts-other": {"1"}, deadTeam: {"1"}})
	r.dead[deadTeam] = deadWhy
	r.Script.Fzf = []string{"|ABORT"}
	exit(t, r.run("--type=podman"), term.ExitOK, r.rig)
	rows := r.Script.Rows[0]
	if slices.ContainsFunc(rows, func(row string) bool { return strings.Contains(row, deadTeam) }) {
		t.Fatalf("a dead team was offered: %q", rows)
	}
	if !slices.ContainsFunc(rows, func(row string) bool { return strings.Contains(row, v2Team) }) {
		t.Fatalf("the live teamchat team was not offered: %q", rows)
	}
	contains(t, "stderr", r.stderr(), "Not offered: team "+deadTeam+": "+deadWhy+".")
	r.lacksOutput(t)
}

// Issue 430 review: the teams' buses are asked at once, so the list waits for the slowest probe,
// not their sum. Each probe blocks until every team has been asked: probes run one after another
// would never all start, and the test would hit its bound.
func TestEveryTeamsBusIsAskedAtOnce(t *testing.T) {
	teams := map[string][]string{v2Team: {"2"}, deadTeam: {"1"}, "lts-stopped": {"1"}}
	r := newTeamchatRig(t, "", teams)
	var started sync.WaitGroup
	started.Add(len(teams))
	allAsked := make(chan struct{})
	go func() {
		started.Wait()
		close(allAsked)
	}()
	r.c.Teamchat.Answers = func(ctx context.Context, team string) (string, error) {
		started.Done()
		select {
		case <-allAsked:
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(10 * time.Second):
			return "", errors.New("team " + team + " was asked alone: the probes did not run at once")
		}
		if team == v2Team {
			return "", nil
		}
		return deadWhy, nil
	}
	exit(t, r.run("--type=podman"), term.ExitOK, r.rig)
	if r.payload.String() != "2@"+v2Team+"\n" {
		t.Fatalf("payload %q", r.payload.String())
	}
}

// Issue 430 review: a team with no pool seat here is named as having none, not as a dead bus.
func TestATeamWithNoSeatHereIsNotCalledDead(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2"}, deadTeam: {"1"}})
	r.dead[deadTeam] = "no pool seat of it was found in this checkout, so its bus was not asked"
	r.Script.Fzf = []string{"|ABORT"}
	exit(t, r.run("--type=podman"), term.ExitOK, r.rig)
	contains(t, "stderr", r.stderr(), "Not offered: team "+deadTeam+": no pool seat of it was found")
	if strings.Contains(r.stderr(), "does not answer") {
		t.Fatalf("a team with no seat was called dead: %s", r.stderr())
	}
}

// Issue 430: with the dead teams left out, a checkout whose one live team is a teamchat one takes
// it without a list, as when the dead ones were never there.
func TestTheOneLiveTeamAmongDeadOnesIsTakenWithoutAList(t *testing.T) {
	r := newTeamchatRig(t, "", map[string][]string{v2Team: {"2"}, deadTeam: {"1"}, "lts-stopped": {"1"}})
	r.dead[deadTeam] = deadWhy
	r.dead["lts-stopped"] = "its homeserver at https://127.0.0.1:8449 gave no answer within 5s"
	exit(t, r.run("--type=podman"), term.ExitOK, r.rig)
	if r.payload.String() != "2@"+v2Team+"\n" || len(r.Script.Rows) != 0 {
		t.Fatalf("payload %q rows %v", r.payload.String(), r.Script.Rows)
	}
	contains(t, "stderr", r.stderr(), "Not offered: team "+deadTeam, "Not offered: team lts-stopped", "gave no answer within 5s")
}

// Issue 430: whether a bus answers that cannot be judged stops the list, and with no probe wired
// the list refuses: a teamchat team is never offered unjudged.
func TestATeamchatTeamWhoseBusCannotBeJudgedIsNotOffered(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2"}})
	r.answersErr = errors.New("the seat.json cannot be read")
	exit(t, r.run("--type=podman"), term.ExitFailure, r.rig)
	contains(t, "stderr", r.stderr(), "the seat.json cannot be read")
	r.lacksOutput(t)

	r = newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2"}})
	r.c.Teamchat.Answers = nil
	exit(t, r.run("--type=podman"), term.ExitFailure, r.rig)
	contains(t, "stderr", r.stderr(), "no probe of a team's bus is wired")
	r.lacksOutput(t)
}

func TestTheOnlyTeamBeingATeamchatOneIsTakenWithoutAList(t *testing.T) {
	r := newTeamchatRig(t, "", map[string][]string{v2Team: {"2"}})
	exit(t, r.run("--type=podman"), term.ExitOK, r.rig)
	if r.payload.String() != "2@"+v2Team+"\n" || len(r.Script.Rows) != 0 {
		t.Fatalf("payload %q rows %v", r.payload.String(), r.Script.Rows)
	}
}

// Plan 00265 R90 (design section 2): a bare --teams in a checkout whose pool holds seats of
// exactly one team takes that team without the list, beside other teams and teams with no seat
// here, and says so; with seats in two teams the list is shown.
func TestABareListTakesTheOneTeamThisCheckoutHasSeatsIn(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers+betaWiden, map[string][]string{v2Team: {"2"}, "lts-other": nil})
	exit(t, r.run("--type=podman"), term.ExitOK, r.rig)
	if r.payload.String() != "2@"+v2Team+"\n" || len(r.Script.Rows) != 0 {
		t.Fatalf("payload %q rows %v", r.payload.String(), r.Script.Rows)
	}
	contains(t, "stderr", r.stderr(), "This checkout has seats in "+v2Team+" alone: taking it.")

	r = newTeamchatRig(t, "", map[string][]string{v2Team: {"2"}, "lts-other": {"1"}})
	r.Script.Fzf = []string{"|ABORT"}
	exit(t, r.run("--type=podman"), term.ExitOK, r.rig)
	if len(r.Script.Rows) == 0 {
		t.Fatalf("seats in two teams took one without the list: payload %q", r.payload.String())
	}
}

func TestTeamsOnBothSystemsInOneLaunchAreRefused(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers+betaWiden, map[string][]string{v2Team: {"2"}, "lts-other": {"1"}})
	r.Script.Fzf = []string{"|" + v2Team + "&&alpha"}
	exit(t, r.run("--type=podman"), term.ExitUsage, r.rig)
	contains(t, "stderr", r.stderr(), "one session joins one system", "agent-bus: alpha", "teamchat: "+v2Team)
	r.lacksOutput(t)
}

func TestAHostListShowsThisCheckoutsTeamchatTeams(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers+betaWiden, map[string][]string{v2Team: {"2"}, "lts-other": {"1"}})
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
	contains(t, "stderr", r.stderr(), "this checkout's one podman seat of it")
}

// Register R55: a seat named for cc that is a podman seat is refused naming its kind and ccy,
// before anything is asked.
func TestAHostLaunchRefusesANamedPodmanSeat(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2:podman", "3:host"}})
	r.consentRig()
	exit(t, r.run("--type=host", "2@"+v2Team), term.ExitConfig, r.rig)
	r.lacksOutput(t)
	contains(t, "stderr", r.stderr(), "2@"+v2Team+" is a podman seat (proj.2+local.podman)", "launch it with ccy --teams "+v2Team)

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
// Plan 00265 R90: with no terminal (a restore, a restart) the seat a launch would take without
// asking is taken; only a genuine choice, or no seat at all, is refused, in words.
func TestNoTerminalTakesTheSeatThisCheckoutWorkedIn(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2", "3"}})
	r.lastRan["3"] = r.c.Now().UnixMilli() - 2*60*60*1000
	r.Host.IsTerminal = func(any) bool { return false }
	exit(t, r.run("--type=podman", "1@alpha,"+v2Team), term.ExitOK, r.rig)
	if r.payload.String() != "1@alpha,3@"+v2Team+"\n" || len(r.Script.GumLog) != 0 {
		t.Fatalf("payload %q, questions %v", r.payload.String(), r.Script.GumLog)
	}
	contains(t, "stderr", r.stderr(), "resuming this checkout's seat of it that last ran 2 hours ago")
}

func TestNoTerminalRefusesAGenuineChoiceInWords(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2", "3"}})
	r.lastRan["2"] = r.c.Now().UnixMilli() - 3*24*60*60*1000
	r.lastRan["3"] = r.c.Now().UnixMilli() - 2*60*60*1000
	r.Host.IsTerminal = func(any) bool { return false }
	exit(t, r.run("--type=podman", v2Team), term.ExitUsage, r.rig)
	r.lacksOutput(t)
	contains(t, "stderr", r.stderr(), "Which seat of "+v2Team+" to take is a choice", "A seat of "+v2Team+": last ran 2 hours ago",
		"run ccy --teams "+v2Team+" at a terminal")
	if strings.Contains(r.stderr(), "2@") || strings.Contains(r.stderr(), "proj.") {
		t.Fatalf("the refusal names a seat:\n%s", r.stderr())
	}
}

// A restore or a restart runs on a terminal nobody watches: the launcher says so with
// --attended=false, and nothing is asked there either.
func TestTheLauncherSaysNobodyIsThereToAsk(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2", "3"}})
	r.lastRan["2"] = r.c.Now().UnixMilli() - 3*24*60*60*1000
	r.lastRan["3"] = r.c.Now().UnixMilli() - 2*60*60*1000
	exit(t, r.run("--type=podman", "--attended=false", v2Team), term.ExitUsage, r.rig)
	if len(r.Script.GumLog) != 0 {
		t.Fatalf("asked on a terminal nobody watches: %v", r.Script.GumLog)
	}
	r.lastRan["2"] = 0
	exit(t, r.run("--type=podman", "--attended=false", v2Team), term.ExitOK, r.rig)
	if r.payload.String() != "3@"+v2Team+"\n" {
		t.Fatalf("payload %q", r.payload.String())
	}
	exit(t, r.run("--type=podman", "--attended=maybe", v2Team), term.ExitUsage, r.rig)
}

func TestNoTerminalAddsNoSeat(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2:host"}})
	added := r.wireNewSeat("9", term.ExitOK)
	r.Host.IsTerminal = func(any) bool { return false }
	exit(t, r.run("--type=podman", v2Team), term.ExitUsage, r.rig)
	r.lacksOutput(t)
	if len(*added) != 0 {
		t.Fatalf("a seat was added with no terminal: %v", *added)
	}
	contains(t, "stderr", r.stderr(), "has no podman seat of "+v2Team, "ccy-teams seats add "+v2Team)
}

func TestNoTerminalNamesOnlyTheLaunchKindsSeats(t *testing.T) {
	r := newTeamchatRig(t, alphaCovers, map[string][]string{v2Team: {"2:podman", "3", "4"}})
	r.lastRan["3"] = r.c.Now().UnixMilli() - 2*60*60*1000
	r.lastRan["4"] = r.c.Now().UnixMilli() - 3*60*60*1000
	r.Host.IsTerminal = func(any) bool { return false }
	exit(t, r.run("--type=host", v2Team), term.ExitUsage, r.rig)
	contains(t, "stderr", r.stderr(), "take a free seat: run cc --teams "+v2Team, "ccy-teams seats add "+v2Team+" --host")
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
