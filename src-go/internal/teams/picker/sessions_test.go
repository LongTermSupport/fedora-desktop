package picker

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/hot"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Plan 00203 SPEC-hot-control.md M5 (sections 5.1 to 5.3): the picker's Running sessions view,
// its menus and flows. Every change is made by running the `ccy-teams session` command a
// person would type, so the steps, the questions, the locks and the rollback are the M4 ones.

const proj = "ccy-proj"

// seated is ccy-proj on seat 1 of alpha, as the rig's board has it.
func (h rig) seated() hot.Session {
	return hot.Session{
		Name: proj, Kind: "ccy", Checkout: h.Checkout(), State: "ok", Controllable: true,
		Teams: []hot.Cell{{Team: "alpha", Seat: "1", Role: "worker"}},
	}
}

// plain is ccy-proj on no team.
func (h rig) plain() hot.Session {
	s := h.seated()
	s.Teams = nil
	return s
}

// coordinatorOfAlpha makes handle alpha's coordinator in the roster the board reads.
func (h rig) coordinatorOfAlpha(handle string) {
	list := strings.ReplaceAll(teamtest.BusList, handle+"\tdev\t", handle+"\tcoordinator\t")
	list = strings.ReplaceAll(list, handle+"\tops\t", handle+"\tcoordinator\t")
	h.On(teamtest.SudoBus+"list alpha", list, 0)
}

// seatsOfAlpha scripts agent-bus seat choose for alpha in the checkout.
func (h rig) seatsOfAlpha() {
	h.On("agent-bus seat choose alpha --type=podman",
		"CHOICE\t1\theld\tproj.1+local.podman\tw\nCHOICE\tdev\tfree\tproj.dev+local.podman\tw\nCHOICE\t3\tnew\tproj.3+local.podman\tw\nDEFAULT\tdev\n", 0)
}

func (h rig) wantRuns(t *testing.T, want ...[]string) {
	t.Helper()
	if !slices.EqualFunc(h.ses.runs, want, slices.Equal) {
		t.Fatalf("ccy-teams session runs %q, want %q\nstderr: %s", h.ses.runs, want, h.ErrOut.String())
	}
}

func (h rig) wantErr(t *testing.T, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(h.ErrOut.String(), w) {
			t.Fatalf("stderr lacks %q:\n%s", w, h.ErrOut.String())
		}
	}
}

func (h rig) argsOf(n int) []string { return h.Script.FzfArgs[n] }

func TestTabShowsTheRunningSessionsAndTabAgainTheTeams(t *testing.T) {
	h := newRig(t)
	h.ses.list = []hot.Session{h.seated()}
	h.Script.Fzf = []string{"tab|alpha", "tab|ccy-proj", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if !slices.Contains(h.argsOf(1), "--border-label= Running sessions ") ||
		!slices.Contains(h.argsOf(1), "--expect=tab,ctrl-a,ctrl-x,ctrl-o,?") {
		t.Fatalf("args %q", h.argsOf(1))
	}
	if !slices.ContainsFunc(h.Script.Rows[1], func(r string) bool {
		return strings.HasPrefix(r, "ccy-proj ") && strings.Contains(r, "alpha (1, worker)") && strings.Contains(r, "  ok")
	}) {
		t.Fatalf("rows %q", h.Script.Rows[1])
	}
	for _, want := range []string{
		"Enter menu   Ctrl-A add to a team   Ctrl-X remove from a team   Ctrl-O role   Tab teams   ? help   Esc back",
		hot.OnlyTmuxLine, "SESSION ", "TEAMS (seat, role)",
	} {
		if !strings.Contains(h.HeaderOf(1), want) {
			t.Fatalf("header %q lacks %q", h.HeaderOf(1), want)
		}
	}
	if !slices.Contains(h.argsOf(2), "--border-label= Agent teams ") {
		t.Fatalf("Tab again shows the teams: %q", h.argsOf(2))
	}
	h.wantRuns(t)
}

func TestEscInTheRunningSessionsGoesBackToTheTeams(t *testing.T) {
	h := newRig(t)
	h.Script.Fzf = []string{"tab|alpha", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if len(h.Script.Rows) != 3 || h.Script.Rows[1][len(h.Script.Rows[1])-1] != "Back to the teams" {
		t.Fatalf("teams, no sessions, teams again: %q", h.Script.Rows)
	}
}

func TestSessionsThatCannotBeListedSayWhyAndTheTeamsComeBack(t *testing.T) {
	h := newRig(t)
	h.ses.err = errors.New("podman could not list the sessions' containers: boom")
	h.Script.Fzf = []string{"tab|alpha", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	h.wantErr(t, "The running sessions cannot be listed: podman could not list the sessions' containers: boom.\n")
	if !slices.Contains(h.argsOf(1), "--border-label= Agent teams ") {
		t.Fatalf("back on the teams: %q", h.argsOf(1))
	}
}

// 5.2: Ctrl-A, a team, the lowest free seat and worker run `session add <s> <team>`, which
// asks its own question and prints its steps; the list waits for Enter after.
func TestCtrlAAddsTheSessionToATeamThroughSessionAdd(t *testing.T) {
	h := newRig(t)
	h.seatsOfAlpha()
	h.ses.list = []hot.Session{h.plain()}
	h.Script.Fzf = []string{"tab|alpha", "ctrl-a|ccy-proj", "|alpha", "|lowest free", "|worker", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	h.wantRuns(t, []string{"add", proj, "alpha"})
	if !slices.ContainsFunc(h.Script.Rows[2], func(r string) bool {
		return strings.HasPrefix(r, "alpha ") && strings.Contains(r, "up") && strings.Contains(r, "3 agents")
	}) {
		t.Fatalf("team rows %q", h.Script.Rows[2])
	}
	seats := h.Script.Rows[3]
	if !strings.HasPrefix(seats[0], "The lowest free seat") || !slices.ContainsFunc(seats, func(r string) bool { return strings.HasPrefix(r, "seat dev (free, proj.dev+local.podman)") }) ||
		!slices.ContainsFunc(seats, func(r string) bool { return strings.HasPrefix(r, "seat 3 (new, proj.3+local.podman)") }) ||
		slices.ContainsFunc(seats, func(r string) bool { return strings.HasPrefix(r, "seat 1 ") }) {
		t.Fatalf("seat rows %q", seats)
	}
	h.wantErr(t, "Press Enter to go back to the list.\n")
}

func TestAddOfAChosenSeatAsCoordinatorDemotesTheCurrentOne(t *testing.T) {
	h := newRig(t)
	h.seatsOfAlpha()
	h.coordinatorOfAlpha("proj.box+role.lxc")
	h.ses.list = []hot.Session{h.plain()}
	h.Script.Fzf = []string{"tab|alpha", "ctrl-a|ccy-proj", "|alpha", "|seat dev", "|Replace the coordinator", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	wantTakeoverRows(t, h.Script.Rows[4], "proj.box+role.lxc")
	h.wantRuns(t, []string{"add", proj, "alpha/dev", "--role=coordinator", "--demote-current"})
}

// wantTakeoverRows is a role question on a team whose coordinator is holder: worker first, then
// the takeover in its own words, never a plain "coordinator" row.
func wantTakeoverRows(t *testing.T, rows []string, holder string) {
	t.Helper()
	if !strings.HasPrefix(rows[0], "worker") ||
		!slices.ContainsFunc(rows, func(r string) bool { return strings.HasPrefix(r, "Replace the coordinator, "+holder+" (") }) ||
		slices.ContainsFunc(rows, func(r string) bool { return strings.HasPrefix(r, "coordinator") }) {
		t.Fatalf("role rows %q", rows)
	}
}

func TestAddAsCoordinatorOfATeamWithNoneIsOfferedPlainly(t *testing.T) {
	h := newRig(t)
	h.seatsOfAlpha()
	h.ses.list = []hot.Session{h.plain()}
	h.Script.Fzf = []string{"tab|alpha", "ctrl-a|ccy-proj", "|alpha", "|seat dev", "|alpha has none", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if rows := h.Script.Rows[4]; !strings.HasPrefix(rows[0], "worker") || !slices.ContainsFunc(rows, func(r string) bool { return strings.HasPrefix(r, "Make it the coordinator (alpha has none)") }) {
		t.Fatalf("role rows %q", rows)
	}
	h.wantRuns(t, []string{"add", proj, "alpha/dev", "--role=coordinator"})
}

func TestAddAsWorkerOfAnExistingSeatSaysTheRole(t *testing.T) {
	h := newRig(t)
	h.seatsOfAlpha()
	h.ses.list = []hot.Session{h.plain()}
	h.Script.Fzf = []string{"tab|alpha", "ctrl-a|ccy-proj", "|alpha", "|seat dev", "|worker", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	h.wantRuns(t, []string{"add", proj, "alpha/dev", "--role=worker"})
}

// InteractiveScripts.md: a team that cannot be joined is said, and asked again, three times at
// most; nothing is run.
func TestAddOfATeamThatCannotBeJoinedAsksAgainThenGivesUp(t *testing.T) {
	h := newRig(t)
	h.ses.list = []hot.Session{h.seated()}
	h.Script.Fzf = []string{"tab|alpha", "ctrl-a|ccy-proj", "|alpha", "|alpha", "|alpha", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	h.wantRuns(t)
	h.wantErr(t, "ccy-proj is already in alpha as seat 1: one seat per team per session.\n",
		"Nothing was changed after 3 tries.\n", "Press Enter to go back to the list.\n")
	if strings.Count(h.ErrOut.String(), "already in alpha") != 3 {
		t.Fatalf("asked three times: %s", h.ErrOut.String())
	}
}

func TestAddOfATeamWhoseHomeserverIsDownIsRefusedAndAskedAgain(t *testing.T) {
	h := newRig(t)
	h.On("systemctl list-units", "agent-bus-hs@alpha.service loaded failed failed Homeserver alpha\n", 0)
	h.ses.list = []hot.Session{h.plain()}
	h.Script.Fzf = []string{"tab|alpha", "ctrl-a|ccy-proj", "|alpha", "|ABORT", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	h.wantRuns(t)
	h.wantErr(t, "alpha's homeserver is down, so nothing can join it: systemctl status agent-bus-hs@alpha\n")
	if len(h.Script.Rows) != 6 {
		t.Fatalf("the team question again after the refusal: %d fzf calls", len(h.Script.Rows))
	}
}

// 5.3: removing the team's coordinator asks what becomes of the role first.
func TestRemoveOfTheCoordinatorAsksWhereTheRoleGoes(t *testing.T) {
	for answer, want := range map[string]string{
		"|goes to no one":               "--coordinator=demote",
		"|Hand it to proj.box+role.lxc": "--coordinator=hand-to=proj.box+role.lxc",
	} {
		h := newRig(t)
		h.coordinatorOfAlpha("proj.1+local.podman")
		h.ses.list = []hot.Session{h.seated()}
		h.Script.Fzf = []string{"tab|alpha", "ctrl-x|ccy-proj", answer, "|ABORT", "|ABORT"}
		if code := h.run(); code != term.ExitOK {
			t.Fatalf("exit %d: %s", code, h.ErrOut.String())
		}
		if !strings.Contains(h.HeaderOf(2), "Its seat 1 is alpha's coordinator. After it leaves:") ||
			slices.ContainsFunc(h.Script.Rows[2], func(r string) bool { return strings.Contains(r, "proj.2+local.podman") }) {
			t.Fatalf("header %q rows %q (a parked member is not offered)", h.HeaderOf(2), h.Script.Rows[2])
		}
		h.wantRuns(t, []string{"remove", proj, "alpha", want})
	}
}

func TestRemoveOfAWorkerAsksNothingMore(t *testing.T) {
	h := newRig(t)
	h.ses.list = []hot.Session{h.seated()}
	h.ses.code = term.ExitFailure
	h.Script.Fzf = []string{"tab|alpha", "ctrl-x|ccy-proj", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	h.wantRuns(t, []string{"remove", proj, "alpha"})
	h.wantErr(t, "(ccy-teams session remove ccy-proj alpha failed, exit 1: see above.)\n")
}

func TestRoleOfTheSessionInATeam(t *testing.T) {
	h := newRig(t)
	h.coordinatorOfAlpha("proj.box+role.lxc")
	h.ses.list = []hot.Session{h.seated()}
	h.Script.Fzf = []string{"tab|alpha", "ctrl-o|ccy-proj", "|Replace the coordinator", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if !slices.ContainsFunc(h.Script.Rows[2], func(r string) bool { return strings.HasPrefix(r, "worker (now)") }) {
		t.Fatalf("role rows %q", h.Script.Rows[2])
	}
	wantTakeoverRows(t, h.Script.Rows[2], "proj.box+role.lxc")
	h.wantRuns(t, []string{"role", proj, "alpha", "coordinator", "--demote-current"})
}

func TestTheMenuOfAnUnguardedSessionOffersRepair(t *testing.T) {
	h := newRig(t)
	s := h.seated()
	s.Unguarded, s.State = true, "unguarded: ccy-teams session repair "+proj
	h.ses.list = []hot.Session{s}
	h.Script.Fzf = []string{"tab|alpha", "|ccy-proj", "|Repair the seat keeper", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	menu := h.Script.Rows[2]
	for _, want := range []string{
		"Add to a team...   (Ctrl-A)", "Remove from a team...   (Ctrl-X)", "Move to another team...",
		"Change its role in a team...   (Ctrl-O)", "Attach this terminal to it", "Repair the seat keeper (it has stopped)",
	} {
		if !slices.ContainsFunc(menu, func(r string) bool { return strings.HasPrefix(r, want) }) {
			t.Fatalf("menu %q lacks %q", menu, want)
		}
	}
	h.wantRuns(t, []string{"repair", proj})
}

func TestASessionThatCannotBeChangedIsOnlyAttached(t *testing.T) {
	h := newRig(t)
	h.ses.list = []hot.Session{{Name: proj, Kind: "ccy", Checkout: "-", State: "old or --no-bus: relaunch it to change it while it runs"}}
	h.Script.Fzf = []string{"tab|alpha", "|ccy-proj", "|ABORT", "ctrl-a|ccy-proj", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if menu := h.Script.Rows[2]; len(menu) != 2 || !strings.HasPrefix(menu[0], "Attach this terminal to it") {
		t.Fatalf("menu %q", menu)
	}
	h.wantRuns(t)
	h.wantErr(t, "ccy-proj cannot be changed while it runs (old or --no-bus: relaunch it to change it while it runs). "+
		"Relaunch it from ccy's tmux to change it.\n")
}

func TestMoveFromTheMenuRunsSessionMove(t *testing.T) {
	h := newRig(t)
	h.OnDeclare("declared", teamtest.DeclaredAlpha+"TEAM\tbeta\t6168\tpresent\tbeta.agent-bus.internal\thttp://192.0.2.10:6168\t-\n")
	h.OnDeclare("status", "STATUS\talpha\tcurrent\t-\nSTATUS\tbeta\tcurrent\t-\n")
	h.On("systemctl list-units", teamtest.UnitAlpha+"agent-bus-hs@beta.service loaded active running Homeserver beta\n", 0)
	h.On(teamtest.SudoBus+"list beta", "MEMBER\tother.1+local.podman\tworker\tjoin\tactive\n", 0)
	h.ses.list = []hot.Session{h.seated()}
	h.Script.Fzf = []string{"tab|alpha", "|ccy-proj", "|Move to another team", "|beta", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if slices.ContainsFunc(h.Script.Rows[3], func(r string) bool { return strings.HasPrefix(r, "alpha ") && !strings.Contains(r, "(cannot") }) {
		t.Fatalf("the team it leaves is not a team to move to: %q", h.Script.Rows[3])
	}
	h.wantRuns(t, []string{"move", proj, "alpha", "beta"})
}

// Ctrl-O on a team with no coordinator: worker (now), then the plain offer, which passes no
// --demote-current.
func TestRoleOnATeamWithNoCoordinatorIsOfferedPlainly(t *testing.T) {
	h := newRig(t)
	h.ses.list = []hot.Session{h.seated()}
	h.Script.Fzf = []string{"tab|alpha", "ctrl-o|ccy-proj", "|alpha has none", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	rows := h.Script.Rows[2]
	if !strings.HasPrefix(rows[0], "worker (now)") ||
		!slices.ContainsFunc(rows, func(r string) bool { return strings.HasPrefix(r, "Make it the coordinator (alpha has none)") }) {
		t.Fatalf("role rows %q", rows)
	}
	h.wantRuns(t, []string{"role", proj, "alpha", "coordinator"})
}

// Fable's N1: a chosen seat whose handle already holds the role is not offered as a team with
// none: it is the coordinator now.
func TestAddOfASeatWhoseHandleIsTheCoordinatorSaysItIsOneNow(t *testing.T) {
	h := newRig(t)
	h.On("agent-bus seat choose alpha --type=podman", "CHOICE\t2\tfree\tproj.2+local.podman\tw\nDEFAULT\t2\n", 0)
	h.coordinatorOfAlpha("proj.2+local.podman")
	h.ses.list = []hot.Session{h.plain()}
	h.Script.Fzf = []string{"tab|alpha", "ctrl-a|ccy-proj", "|alpha", "|seat 2", "|coordinator (now)", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	rows := h.Script.Rows[4]
	if !slices.ContainsFunc(rows, func(r string) bool { return strings.HasPrefix(r, "coordinator (now)") }) ||
		slices.ContainsFunc(rows, func(r string) bool { return strings.Contains(r, "has none") || strings.HasPrefix(r, "Replace") }) {
		t.Fatalf("role rows %q", rows)
	}
	h.wantRuns(t, []string{"add", proj, "alpha/2", "--role=coordinator"})
}
