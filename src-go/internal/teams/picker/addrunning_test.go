package picker

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/hot"
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

// The help describes only what is built: the add lines, and not the restart of Task 3.11.
func TestHelpDescribesTheAddLinesAndNoRestart(t *testing.T) {
	for _, want := range []string{"Add a running session to <team>", "Add this\nsession to another team"} {
		if !strings.Contains(helpText, want) {
			t.Fatalf("help lacks %q", want)
		}
	}
	for _, unbuilt := range []string{"ccy-teams restart", "Restart the team"} {
		if strings.Contains(helpText, unbuilt) {
			t.Fatalf("help describes %q, which is not built", unbuilt)
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
	h.withBeta()
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
