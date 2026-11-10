package picker

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/hot"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Plan 00203 SPEC-team-add-and-restart.md A1-A9: a running session is added to a team from the
// team's own menu and from a running member's row, through the same seat and role questions as
// the Running sessions view's Add to a team.

const (
	addRunningAlpha = "Add a running session to alpha"
	addThisSession  = "Add this session to another team"
)

// other is a second running session, in the rig's checkout and on no team.
func (h rig) other() hot.Session {
	s := h.plain()
	s.Name = "ccy-other"
	return s
}

// index is where line is among labels, failing the test when it is not there.
func index(t *testing.T, labels []string, line string) int {
	t.Helper()
	at := slices.Index(labels, line)
	if at < 0 {
		t.Fatalf("menu %q lacks %q", labels, line)
	}
	return at
}

// A1, A2: a visible line, with the add actions, after the LXC one and before every destructive one.
func TestATeamsMenuOffersAddingARunningSessionWithTheAddActions(t *testing.T) {
	h := newRig(t)
	r := h.run1(t)
	labels := h.labels(r, h.row(t, r.snap, board.NoDescription))
	at := index(t, labels, addRunningAlpha)
	if lxc := index(t, labels, "Add an LXC container's agent to alpha"); at != lxc+1 {
		t.Fatalf("the add follows the LXC add: %q", labels)
	}
	if shutdown := index(t, labels, "Shut down every session of alpha"); at > shutdown {
		t.Fatalf("destructive lines stay last: %q", labels)
	}
}

// A3, A5: the running sessions are listed, one in the team refused and said; the chosen one goes
// on to the seat and the role, and `session add` runs with the words a person would type.
func TestAddARunningSessionFromTheTeamsMenuRunsSessionAdd(t *testing.T) {
	h := newRig(t)
	h.seatsOfAlpha()
	h.ses.list = []hot.Session{h.seated(), h.other()}
	h.Script.Fzf = []string{"|ccy-proj", "|ccy-other", "|lowest free", "|worker", "|ABORT"}
	r := h.run1(t)
	if _, err := r.do(context.Background(), actAddRunning, h.row(t, r.snap, board.NoDescription)); err != nil {
		t.Fatal(err)
	}
	h.wantRuns(t, []string{"add", "ccy-other", "alpha"})
	h.wantErr(t, "ccy-proj is already in alpha as proj.1+local.podman: one seat per team per session.")
	sessions := untagged(h.Script.Rows[0])
	if !slices.ContainsFunc(sessions, func(l string) bool {
		return strings.HasPrefix(l, "ccy-proj ") && strings.Contains(l, "in alpha as proj.1+local.podman (worker)")
	}) || !slices.ContainsFunc(sessions, func(l string) bool { return strings.HasPrefix(l, "ccy-other ") && strings.Contains(l, "in no team") }) {
		t.Fatalf("session rows %q", sessions)
	}
	for i, want := range []string{
		"Teams › alpha › Add a running session · step 1/3 Session\nteam alpha",
		"Teams › alpha › Add a running session · step 1/3 Session\nteam alpha",
		"Teams › alpha › Add a running session · step 2/3 Seat\nteam alpha · session ccy-other",
		"Teams › alpha › Add a running session · step 3/3 Role\nteam alpha · session ccy-other · the lowest free seat",
	} {
		h.Script.HeadingAt(i, want)
	}
}

// A6: with no running session, the line explains itself and changes nothing.
func TestAddARunningSessionWithNoneRunningSaysSo(t *testing.T) {
	h := newRig(t)
	h.Script.Fzf = []string{"|ABORT"}
	r := h.run1(t)
	if _, err := r.do(context.Background(), actAddRunning, h.row(t, r.snap, board.NoDescription)); err != nil {
		t.Fatal(err)
	}
	h.wantErr(t, "No session is running, so none can be added to alpha. Start a ccy session in alpha, or start one with ccy and add it here.")
	h.wantRuns(t)
}

// A6: every running session already in the team, or not changeable while it runs, is said.
func TestAddARunningSessionWhenNoneCanJoinSaysWhyForEach(t *testing.T) {
	h := newRig(t)
	stuck := h.other()
	stuck.Controllable = false
	stuck.State = "old or --no-bus: relaunch it to change it while it runs"
	h.ses.list = []hot.Session{h.seated(), stuck}
	r := h.run1(t)
	if _, err := r.do(context.Background(), actAddRunning, h.row(t, r.snap, board.NoDescription)); err != nil {
		t.Fatal(err)
	}
	h.wantErr(t, "No running session can be added to alpha:",
		"ccy-proj is already in alpha as proj.1+local.podman",
		"ccy-other cannot be changed while it runs (old or --no-bus: relaunch it to change it while it runs)")
	h.wantRuns(t)
	if len(h.Script.Rows) != 0 {
		t.Fatalf("nothing to choose is not a question: %q", h.Script.Rows)
	}
}

// A3: a team that cannot be joined (homeserver down, or not installed) is refused before the
// sessions are listed, in teamOptions' own words.
func TestAddARunningSessionToATeamThatCannotBeJoinedRefusesBeforeListing(t *testing.T) {
	for name, tc := range map[string]struct{ units, want string }{
		"homeserver down": {
			"agent-bus-hs@alpha.service loaded failed failed Homeserver alpha\n",
			"alpha's homeserver is down, so nothing can join it: systemctl status agent-bus-hs@alpha",
		},
		"not installed": {"", "alpha is not installed: ccy-teams apply alpha builds it."},
	} {
		t.Run(name, func(t *testing.T) {
			h := newRig(t)
			h.On("systemctl list-units", tc.units, 0)
			h.ses.list = []hot.Session{h.other()}
			r := h.run1(t)
			if _, err := r.do(context.Background(), actAddRunning, h.row(t, r.snap, board.NoDescription)); err != nil {
				t.Fatal(err)
			}
			h.wantErr(t, tc.want)
			if h.ses.listed != 0 || len(h.Script.Rows) != 0 {
				t.Fatalf("refused before listing: listed %d, questions %q", h.ses.listed, h.Script.Rows)
			}
			h.wantRuns(t)
		})
	}
}

// The help describes the add lines and the team restart (B1).
func TestHelpDescribesTheAddLinesAndTheRestart(t *testing.T) {
	for _, want := range []string{
		"Add a running session to <team>", "Add this\nsession to another team", "Add a running session to this seat",
		"Restart the team's desktop sessions", "`ccy-teams restart <team>`", "LXC members",
		"Wind down the team's sessions", "`ccy-teams wind-down <team>`", "/goal clear",
	} {
		if !strings.Contains(helpText, want) {
			t.Fatalf("help lacks %q", want)
		}
	}
}

// A7, A8: a running member's seat row offers adding its session to another team, with the add
// actions, never last; it is the sessions view's Add to a team, its own team refused.
func TestASeatRowOffersAddingItsSessionToAnotherTeam(t *testing.T) {
	h := newRig(t)
	r := h.run1(t)
	labels := h.labels(r, h.row(t, r.snap, "1"))
	at := index(t, labels, addThisSession)
	if lxc := index(t, labels, "Add an LXC container's agent to alpha"); at != lxc+1 {
		t.Fatalf("the add follows the LXC add: %q", labels)
	}
	if remove := slices.IndexFunc(labels, func(l string) bool { return strings.HasPrefix(l, "Remove seat ") }); remove < 0 || at > remove {
		t.Fatalf("destructive lines stay last: %q", labels)
	}
	if slices.Contains(h.labels(r, h.row(t, r.snap, "dev")), addThisSession) {
		t.Fatal("a seat with no running session has no session to add")
	}
}

func TestAddThisSessionFromASeatRowRunsSessionAdd(t *testing.T) {
	h := newRig(t)
	h.withBeta(t)
	h.On("agent-bus seat choose beta --type=podman", "CHOICE\t1\tnew\tproj.1+local.podman\tw\nDEFAULT\t1\n", 0)
	h.ses.list = []hot.Session{h.seated()}
	h.Script.Fzf = []string{"|alpha", "|beta", "|lowest free", "|worker", "|ABORT"}
	r := h.run1(t)
	if _, err := r.do(context.Background(), actAddThis, h.row(t, r.snap, "1")); err != nil {
		t.Fatal(err)
	}
	h.wantRuns(t, []string{"add", proj, "beta"})
	h.wantErr(t, "ccy-proj is already in alpha as proj.1+local.podman: one seat per team per session.")
	h.Script.HeadingAt(0, "Teams › Running sessions › ccy-proj › Add to a team\n· step 1/3 Team\nsession ccy-proj")
}

// A9: a seat whose session cannot be changed while it runs says why, and asks nothing.
func TestAddThisSessionThatCannotBeChangedSaysWhy(t *testing.T) {
	h := newRig(t)
	s := h.seated()
	s.Controllable, s.State = false, "old or --no-bus: relaunch it to change it while it runs"
	h.ses.list = []hot.Session{s}
	r := h.run1(t)
	if _, err := r.do(context.Background(), actAddThis, h.row(t, r.snap, "1")); err != nil {
		t.Fatal(err)
	}
	h.wantErr(t, "ccy-proj cannot be changed while it runs (old or --no-bus: relaunch it to change it while it runs).")
	h.wantRuns(t)
}

// A11-A15: a seat with no running session takes one, its team and seat fixed.

const addToSeat = "Add a running session to this seat"

// devRecord writes a stopped session record that names alpha's seat dev, restored or not.
func (h rig) devRecord(t *testing.T, restore string) {
	t.Helper()
	teamtest.MustWrite(t, filepath.Join(h.Root, "home/.local/state/ccy/sessions/ccy-old"),
		"ccy-session-record 1\nname=ccy-old\ndir="+h.Checkout()+"\nlauncher=/x/ccy\nprefix=ccy\nrestore="+restore+"\narg=--teams\narg=dev@alpha\n")
}

// A11: the line is on a seat row whose session is not running, with the add actions, before
// Remove seat; a running seat has Add this session to another team instead.
func TestASeatWithNoRunningSessionOffersAddingARunningSessionToIt(t *testing.T) {
	h := newRig(t)
	r := h.run1(t)
	labels := h.labels(r, h.row(t, r.snap, "dev"))
	at := index(t, labels, addToSeat)
	if lxc := index(t, labels, "Add an LXC container's agent to alpha"); at != lxc+1 {
		t.Fatalf("the add follows the LXC add: %q", labels)
	}
	if remove := slices.IndexFunc(labels, func(l string) bool { return strings.HasPrefix(l, "Remove seat ") }); remove < 0 || at > remove {
		t.Fatalf("destructive lines stay last: %q", labels)
	}
	if slices.Contains(h.labels(r, h.row(t, r.snap, "1")), addToSeat) {
		t.Fatal("a seat whose session runs has Add this session to another team, not this")
	}
}

// A12, A13: the sessions are listed with A4's refusals and those of the seat (another
// checkout, another kind); the chosen one goes on to the role only, and `session add` names
// the seat.
func TestAddARunningSessionToASeatAsksTheSessionAndTheRoleOnly(t *testing.T) {
	h := newRig(t)
	elsewhere := h.other()
	elsewhere.Name, elsewhere.Checkout = "ccy-far", filepath.Join(h.Root, "far")
	cc := h.other()
	cc.Name, cc.Kind = "cc-proj", "cc"
	h.ses.list = []hot.Session{h.seated(), elsewhere, cc, h.other()}
	h.Script.Fzf = []string{"|ccy-far", "|cc-proj", "|ccy-other", "|worker", "|ABORT"}
	r := h.run1(t)
	if _, err := r.do(context.Background(), actAddToSeat, h.row(t, r.snap, "dev")); err != nil {
		t.Fatal(err)
	}
	h.wantRuns(t, []string{"add", "ccy-other", "alpha/dev", "--role=worker"})
	h.wantErr(t,
		"ccy-far runs in "+h.p.Kit.Abbrev(elsewhere.Checkout)+", and seat dev is a seat of "+h.p.Kit.Abbrev(h.Checkout())+".",
		"cc-proj is a cc session, and seat dev is a ccy seat.")
	for i, want := range []string{
		"Teams › alpha › proj.dev+local.podman › Add a running session\n· step 1/2 Session\nteam alpha · seat dev",
		"Teams › alpha › proj.dev+local.podman › Add a running session\n· step 1/2 Session\nteam alpha · seat dev",
		"Teams › alpha › proj.dev+local.podman › Add a running session\n· step 1/2 Session\nteam alpha · seat dev",
		"Teams › alpha › proj.dev+local.podman › Add a running session\n· step 2/2 Role\nteam alpha · session ccy-other · seat dev",
	} {
		h.Script.HeadingAt(i, want)
	}
}

// A14: with none that can take the seat, each one's reason is said and nothing is asked.
func TestAddARunningSessionToASeatWhenNoneCanTakeItSaysWhyForEach(t *testing.T) {
	h := newRig(t)
	stuck := h.other()
	stuck.Controllable, stuck.State = false, "old or --no-bus: relaunch it to change it while it runs"
	cc := h.other()
	cc.Name, cc.Kind = "cc-proj", "cc"
	h.ses.list = []hot.Session{h.seated(), stuck, cc}
	r := h.run1(t)
	if _, err := r.do(context.Background(), actAddToSeat, h.row(t, r.snap, "dev")); err != nil {
		t.Fatal(err)
	}
	h.wantErr(t, "No running session can be added to seat dev of alpha:",
		"ccy-proj is already in alpha", "ccy-other cannot be changed while it runs",
		"cc-proj is a cc session, and seat dev is a ccy seat")
	h.wantRuns(t)
	if len(h.Script.Rows) != 0 {
		t.Fatalf("nothing to choose is not a question: %q", h.Script.Rows)
	}
}

// A15: a seat a process holds cannot take a running session: the line stays, says why, and
// choosing it asks nothing. A seat only a stopped session's record names is free, restored or
// not: a restored one is taken over (A16).
func TestASeatAProcessHoldsSaysWhyItCannotTakeARunningSession(t *testing.T) {
	h := newRig(t)
	h.HoldSeat(t, h.Checkout(), "alpha", "dev", true)
	h.ses.list = []hot.Session{h.other()}
	r := h.run1(t)
	row := h.row(t, r.snap, "dev")
	if !slices.Contains(h.labels(r, row), addToSeat+": unavailable, another process holds it") {
		t.Fatalf("menu %q", h.labels(r, row))
	}
	if _, err := r.do(context.Background(), actAddToSeat, row); err != nil {
		t.Fatal(err)
	}
	h.wantErr(t, "Seat dev of alpha cannot take a running session: another process holds it.")
	if h.ses.listed != 0 || len(h.Script.Rows) != 0 {
		t.Fatalf("refused before listing: listed %d, questions %q", h.ses.listed, h.Script.Rows)
	}
	h.wantRuns(t)
	for _, restore := range []string{"yes", "no"} {
		h := newRig(t)
		h.devRecord(t, restore)
		r := h.run1(t)
		if !slices.Contains(h.labels(r, h.row(t, r.snap, "dev")), addToSeat) {
			t.Fatalf("restore=%s: a stopped record leaves the line available: %q", restore, h.labels(r, h.row(t, r.snap, "dev")))
		}
	}
}

// oldRestores is whether the stopped session ccy-old, which names seat dev, is started again
// after a reboot.
func (h rig) oldRestores(t *testing.T) bool {
	t.Helper()
	store, err := h.p.Seats.Registry()
	if err != nil {
		t.Fatal(err)
	}
	rec, err := store.Get("ccy-old")
	if err != nil {
		t.Fatal(err)
	}
	return rec.Restore
}

const takeOverQuestion = "ccy-old is set to restart into this seat after a reboot; attaching ccy-other stops that"

// takeOver is the seat add on seat dev, which the stopped session ccy-old's record restores,
// answered with answers.
func (h rig) takeOver(t *testing.T, answers ...string) {
	t.Helper()
	h.devRecord(t, "yes")
	h.ses.list = []hot.Session{h.other()}
	h.Script.Fzf = answers
	r := h.run1(t)
	if _, err := r.do(context.Background(), actAddToSeat, h.row(t, r.snap, "dev")); err != nil {
		t.Fatal(err)
	}
}

// A16: a seat a stopped, restored record names is taken over in one step: after the session and
// the role, a confirm naming the old session; yes turns the old record's restore off (the
// seat menu's Stop starting its session again after a reboot), then the add runs.
func TestAddingToASeatARestoredRecordNamesTakesItOverAfterAConfirm(t *testing.T) {
	h := newRig(t)
	h.takeOver(t, "|ccy-other", "|worker", "|YES")
	h.wantRuns(t, []string{"add", "ccy-other", "alpha/dev", "--role=worker"})
	if h.oldRestores(t) {
		t.Fatal("the old session still restarts into the seat")
	}
	if !strings.Contains(h.HeaderOf(2), takeOverQuestion) {
		t.Fatalf("header %q", h.HeaderOf(2))
	}
	h.wantErr(t, "Session ccy-old will not be started again after a reboot.")
	for i, want := range []string{
		"Teams › alpha › proj.dev+local.podman › Add a running session\n· step 1/3 Session\nteam alpha · seat dev",
		"Teams › alpha › proj.dev+local.podman › Add a running session\n· step 2/3 Role\nteam alpha · session ccy-other · seat dev",
		"Teams › alpha › proj.dev+local.podman › Add a running session\n· step 3/3 Take over\nseat dev of alpha · session ccy-other · ccy-old restarts into it",
	} {
		h.Script.HeadingAt(i, want)
	}
}

// A16: cancelling the confirm, by No or Esc, changes nothing.
func TestCancellingTheTakeoverChangesNothing(t *testing.T) {
	for _, answer := range []string{"|EXIT", "|ABORT"} {
		h := newRig(t)
		h.takeOver(t, "|ccy-other", "|worker", answer)
		h.wantRuns(t)
		if !h.oldRestores(t) {
			t.Fatalf("%s: the old session's restore was turned off", answer)
		}
	}
}

// A16: an add that fails after the takeover puts the old session's restore back, so a failure
// leaves the seat as it was.
func TestATakeoverWhoseAddFailsPutsTheRestoreBack(t *testing.T) {
	h := newRig(t)
	h.ses.code = term.ExitFailure
	h.takeOver(t, "|ccy-other", "|worker", "|YES")
	h.wantRuns(t, []string{"add", "ccy-other", "alpha/dev", "--role=worker"})
	if !h.oldRestores(t) {
		t.Fatal("the failed add left the old session's restore off")
	}
	h.wantErr(t, "The add did not finish, so session ccy-old will be started again after a reboot, as before.")
}

// A16: when the old record's restore cannot be turned off, nothing is added.
func TestATakeoverWhoseRestoreCannotBeTurnedOffAddsNothing(t *testing.T) {
	h := newRig(t)
	h.p.Seats.Registry = func() (*registry.Store, error) { return nil, &registry.RelativeBaseError{Value: "x"} }
	h.takeOver(t, "|ccy-other", "|worker", "|YES")
	h.wantRuns(t)
	h.wantErr(t, "ERROR: session registry base 'x' is not an absolute path.", "Nothing was changed.")
}

// A14: the seat add says why the sessions cannot be listed, and says when none runs.
func TestAddARunningSessionToASeatWithNoListOrNoneRunningSaysSo(t *testing.T) {
	h := newRig(t)
	h.ses.err = errors.New("tmux is gone")
	r := h.run1(t)
	if _, err := r.do(context.Background(), actAddToSeat, h.row(t, r.snap, "dev")); err != nil {
		t.Fatal(err)
	}
	h.wantErr(t, "The running sessions cannot be listed: tmux is gone.")
	h.ses.err = nil
	if _, err := r.do(context.Background(), actAddToSeat, h.row(t, r.snap, "dev")); err != nil {
		t.Fatal(err)
	}
	h.wantErr(t, "No session is running, so none can be added to seat dev of alpha.")
	h.wantRuns(t)
}

// Leaving the seat add's session or role question changes nothing.
func TestLeavingTheSeatAddChangesNothing(t *testing.T) {
	for name, answers := range map[string][]string{
		"session": {"|ABORT"},
		"role":    {"|ccy-other", "|ABORT"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newRig(t)
			h.ses.list = []hot.Session{h.other()}
			h.Script.Fzf = answers
			r := h.run1(t)
			if _, err := r.do(context.Background(), actAddToSeat, h.row(t, r.snap, "dev")); err != nil {
				t.Fatal(err)
			}
			h.wantRuns(t)
		})
	}
}

// A8: a seat whose session has ended since the list was drawn, or whose sessions cannot be
// listed, says so and asks nothing.
func TestAddThisSessionThatIsNoLongerListedSaysWhy(t *testing.T) {
	h := newRig(t)
	h.ses.list = []hot.Session{h.other()}
	r := h.run1(t)
	if _, err := r.do(context.Background(), actAddThis, h.row(t, r.snap, "1")); err != nil {
		t.Fatal(err)
	}
	h.wantErr(t, "ccy-proj is not among the running sessions ccy-teams can change")
	h.ses.err = errors.New("tmux is gone")
	if _, err := r.do(context.Background(), actAddThis, h.row(t, r.snap, "1")); err != nil {
		t.Fatal(err)
	}
	h.wantErr(t, "The running sessions cannot be listed: tmux is gone.")
	h.wantRuns(t)
}

// A16: when the restore cannot be turned on again after a failed add, the error says how to.
func TestATakeoverWhoseRestoreCannotBePutBackSaysHow(t *testing.T) {
	h := newRig(t)
	h.ses.code = term.ExitFailure
	open, calls := h.p.Seats.Registry, 0
	h.p.Seats.Registry = func() (*registry.Store, error) {
		calls++
		if calls > 1 {
			return nil, &registry.RelativeBaseError{Value: "x"}
		}
		return open()
	}
	h.takeOver(t, "|ccy-other", "|worker", "|YES")
	h.wantRuns(t, []string{"add", "ccy-other", "alpha/dev", "--role=worker"})
	h.wantErr(t, "the add did not finish, and session ccy-old's restore could not be turned on again",
		"Start its session again after a reboot, on the seat's menu, turns it on.")
}
