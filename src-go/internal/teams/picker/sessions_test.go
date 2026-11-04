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
// Every action is a named choice in a list (CLAUDE/InteractiveScripts.md): the view is a row
// of the teams, and what can be done to a session is its menu.

const proj = "ccy-proj"

// toSessions is the answer that chooses the Running sessions row of the teams.
const toSessions = "|Running sessions"

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

// withBeta adds a second team, beta, up and with one member.
func (h rig) withBeta() {
	h.OnDeclare("declared", teamtest.DeclaredAlpha+"TEAM\tbeta\t6168\tpresent\tbeta.agent-bus.internal\thttp://192.0.2.10:6168\t-\n")
	h.OnDeclare("status", "STATUS\talpha\tcurrent\t-\nSTATUS\tbeta\tcurrent\t-\n")
	h.On("systemctl list-units", teamtest.UnitAlpha+"agent-bus-hs@beta.service loaded active running Homeserver beta\n", 0)
	h.On(teamtest.SudoBus+"list beta", "MEMBER\tother.1+local.podman\tworker\tjoin\tactive\n", 0)
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

// untagged is rows without their hidden tags.
func untagged(rows []string) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = strings.SplitN(r, "\t", 2)[0]
	}
	return out
}

func TestRunningSessionsIsAChoiceInTheTeamsList(t *testing.T) {
	h := newRig(t)
	h.ses.list = []hot.Session{h.seated()}
	h.Script.Fzf = []string{toSessions, "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	teams := untagged(h.Script.Rows[0])
	if n := len(teams); n < 2 || teams[n-1] != "Exit" ||
		teams[n-2] != "Running sessions: change a session's teams while it runs   (Tab)" {
		t.Fatalf("the teams end with Running sessions, then Exit: %q", teams)
	}
	if !slices.Contains(h.argsOf(1), "--border-label= Running sessions ") || !slices.Contains(h.argsOf(1), "--expect=?") {
		t.Fatalf("args %q", h.argsOf(1))
	}
	if !slices.ContainsFunc(h.Script.Rows[1], func(r string) bool {
		return strings.HasPrefix(r, "ccy-proj ") && strings.Contains(r, "alpha (1, worker)") && strings.Contains(r, "  ok")
	}) {
		t.Fatalf("rows %q", h.Script.Rows[1])
	}
	sessions := untagged(h.Script.Rows[1])
	if n := len(sessions); sessions[n-1] != "Back to the teams" || sessions[n-2] != "What this list shows   (?)" {
		t.Fatalf("help and the way back are rows too: %q", sessions)
	}
	for _, want := range []string{
		"Enter lists what can be done to the session   ? help   Esc back to the teams",
		hot.OnlyTmuxLine, "SESSION ", "TEAMS (seat, role)",
	} {
		if !strings.Contains(h.HeaderOf(1), want) {
			t.Fatalf("header %q lacks %q", h.HeaderOf(1), want)
		}
	}
	if !slices.Contains(h.argsOf(2), "--border-label= Agent teams ") {
		t.Fatalf("Esc shows the teams again: %q", h.argsOf(2))
	}
	h.wantRuns(t)
}

// Tab is the Running sessions row's shortcut, shown beside it and in the header; it is never
// the only way there.
func TestTabIsTheShortcutShownBesideRunningSessions(t *testing.T) {
	h := newRig(t)
	h.Script.Fzf = []string{"tab|alpha", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if !slices.Contains(h.argsOf(1), "--border-label= Running sessions ") {
		t.Fatalf("Tab shows the running sessions: %q", h.argsOf(1))
	}
	if len(h.Script.Rows) != 3 || h.Script.Rows[1][len(h.Script.Rows[1])-1] != "Back to the teams" {
		t.Fatalf("teams, no sessions, teams again: %q", h.Script.Rows)
	}
}

func TestTheRunningSessionsHelpRowShowsTheHelp(t *testing.T) {
	h := newRig(t)
	h.ses.list = []hot.Session{h.seated()}
	h.Script.Fzf = []string{toSessions, "|What this list shows", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	h.wantErr(t, "Running sessions, the last row of the teams (or Tab), lists every session ccy's tmux started",
		"  Remove from <team>        for the team's coordinator", "Press Enter to go back to the list.\n")
	if !slices.Contains(h.argsOf(2), "--border-label= Running sessions ") {
		t.Fatalf("back on the sessions after the help: %q", h.argsOf(2))
	}
	h.wantRuns(t)
}

func TestSessionsThatCannotBeListedSayWhyAndTheTeamsComeBack(t *testing.T) {
	h := newRig(t)
	h.ses.err = errors.New("podman could not list the sessions' containers: boom")
	h.Script.Fzf = []string{toSessions, "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	h.wantErr(t, "The running sessions cannot be listed: podman could not list the sessions' containers: boom.\n")
	if !slices.Contains(h.argsOf(1), "--border-label= Agent teams ") {
		t.Fatalf("back on the teams: %q", h.argsOf(1))
	}
}

// Every action is a line of the session's menu, in words, naming the team it acts on.
func TestTheSessionMenuNamesEveryActionInWords(t *testing.T) {
	h := newRig(t)
	s := h.seated()
	s.Teams = append(s.Teams, hot.Cell{Team: "beta", Seat: "2", Role: "coordinator"})
	s.Unguarded, s.State = true, "unguarded: ccy-teams session repair "+proj
	h.ses.list = []hot.Session{s}
	h.Script.Fzf = []string{toSessions, "|ccy-proj", "|ABORT", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	want := []string{
		"Add to a team", "Remove from alpha", "Remove from beta",
		"Change role in alpha (now worker)", "Change role in beta (now coordinator)",
		"Move from alpha to another team", "Move from beta to another team",
		"Attach", "Repair (its seat keeper has stopped)", "Back to the list",
	}
	if menu := untagged(h.Script.Rows[2]); !slices.Equal(menu, want) {
		t.Fatalf("menu %q, want %q", menu, want)
	}
	if slices.ContainsFunc(h.argsOf(2), func(a string) bool { return strings.HasPrefix(a, "--expect") }) {
		t.Fatalf("the menu takes no keys besides Enter: %q", h.argsOf(2))
	}
	h.wantRuns(t)
}

func TestTheMenuOfASessionInOneTeamMovesItToAnotherTeam(t *testing.T) {
	h := newRig(t)
	h.ses.list = []hot.Session{h.seated()}
	h.Script.Fzf = []string{toSessions, "|ccy-proj", "|ABORT", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	want := []string{"Add to a team", "Remove from alpha", "Change role in alpha (now worker)", "Move to another team", "Attach", "Back to the list"}
	if menu := untagged(h.Script.Rows[2]); !slices.Equal(menu, want) {
		t.Fatalf("menu %q, want %q", menu, want)
	}
}

// 5.2: Add to a team, a team, the lowest free seat and worker run `session add <s> <team>`,
// which asks its own question and prints its steps; the list waits for Enter after.
func TestAddToATeamRunsSessionAdd(t *testing.T) {
	h := newRig(t)
	h.seatsOfAlpha()
	h.ses.list = []hot.Session{h.plain()}
	h.Script.Fzf = []string{toSessions, "|ccy-proj", "|Add to a team", "|alpha", "|lowest free", "|worker", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	h.wantRuns(t, []string{"add", proj, "alpha"})
	// Every screen names where it is; the wizard's steps carry the team chosen so far, so the
	// seat and the role are never asked without saying which team they are in.
	for i, want := range []string{
		"Teams\n1 team",
		"Teams › Running sessions\n1 session running",
		"Teams › Running sessions › ccy-proj\nsession ccy-proj · ok · in no team",
		"Teams › Running sessions › ccy-proj › Add to a team · step 1/3 Team\nsession ccy-proj",
		"Teams › Running sessions › ccy-proj › Add to a team · step 2/3 Seat\nsession ccy-proj · team alpha",
		"Teams › Running sessions › ccy-proj › Add to a team · step 3/3 Role\nsession ccy-proj · team alpha · the lowest free seat",
	} {
		h.Script.HeadingAt(i, want)
	}
	if !slices.ContainsFunc(h.Script.Rows[3], func(r string) bool {
		return strings.HasPrefix(r, "alpha ") && strings.Contains(r, "up") && strings.Contains(r, "3 agents")
	}) {
		t.Fatalf("team rows %q", h.Script.Rows[3])
	}
	seats := h.Script.Rows[4]
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
	h.Script.Fzf = []string{toSessions, "|ccy-proj", "|Add to a team", "|alpha", "|seat dev", "|Replace the coordinator", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	wantTakeoverRows(t, h.Script.Rows[5], "proj.box+role.lxc")
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
	h.Script.Fzf = []string{toSessions, "|ccy-proj", "|Add to a team", "|alpha", "|seat dev", "|alpha has none", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if rows := h.Script.Rows[5]; !strings.HasPrefix(rows[0], "worker") || !slices.ContainsFunc(rows, func(r string) bool { return strings.HasPrefix(r, "Make it the coordinator (alpha has none)") }) {
		t.Fatalf("role rows %q", rows)
	}
	h.wantRuns(t, []string{"add", proj, "alpha/dev", "--role=coordinator"})
}

func TestAddAsWorkerOfAnExistingSeatSaysTheRole(t *testing.T) {
	h := newRig(t)
	h.seatsOfAlpha()
	h.ses.list = []hot.Session{h.plain()}
	h.Script.Fzf = []string{toSessions, "|ccy-proj", "|Add to a team", "|alpha", "|seat dev", "|worker", "|ABORT", "|ABORT"}
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
	h.Script.Fzf = []string{toSessions, "|ccy-proj", "|Add to a team", "|alpha", "|alpha", "|alpha", "|ABORT", "|ABORT"}
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
	h.Script.Fzf = []string{toSessions, "|ccy-proj", "|Add to a team", "|alpha", "|ABORT", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	h.wantRuns(t)
	h.wantErr(t, "alpha's homeserver is down, so nothing can join it: systemctl status agent-bus-hs@alpha\n")
	if len(h.Script.Rows) != 7 {
		t.Fatalf("the team question again after the refusal: %d fzf calls", len(h.Script.Rows))
	}
}

// Esc in a question changes nothing and counts no refusal.
func TestLeavingAQuestionChangesNothing(t *testing.T) {
	h := newRig(t)
	h.ses.list = []hot.Session{h.plain()}
	h.Script.Fzf = []string{toSessions, "|ccy-proj", "|Add to a team", "|ABORT", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	h.wantRuns(t)
	if strings.Contains(h.ErrOut.String(), "Press Enter") {
		t.Fatalf("leaving says nothing: %s", h.ErrOut.String())
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
		h.Script.Fzf = []string{toSessions, "|ccy-proj", "|Remove from alpha", answer, "|ABORT", "|ABORT"}
		if code := h.run(); code != term.ExitOK {
			t.Fatalf("exit %d: %s", code, h.ErrOut.String())
		}
		if !strings.Contains(h.HeaderOf(3), "Its seat 1 is alpha's coordinator. After it leaves:") ||
			slices.ContainsFunc(h.Script.Rows[3], func(r string) bool { return strings.Contains(r, "proj.2+local.podman") }) {
			t.Fatalf("header %q rows %q (a parked member is not offered)", h.HeaderOf(3), h.Script.Rows[3])
		}
		h.wantRuns(t, []string{"remove", proj, "alpha", want})
	}
}

func TestRemoveOfAWorkerAsksNothingMore(t *testing.T) {
	h := newRig(t)
	h.ses.list = []hot.Session{h.seated()}
	h.ses.code = term.ExitFailure
	h.Script.Fzf = []string{toSessions, "|ccy-proj", "|Remove from alpha", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	h.wantRuns(t, []string{"remove", proj, "alpha"})
	h.wantErr(t, "(ccy-teams session remove ccy-proj alpha failed, exit 1: see above.)\n")
}

func TestChangeRoleInATeamRunsSessionRole(t *testing.T) {
	h := newRig(t)
	h.coordinatorOfAlpha("proj.box+role.lxc")
	h.ses.list = []hot.Session{h.seated()}
	h.Script.Fzf = []string{toSessions, "|ccy-proj", "|Change role in alpha", "|Replace the coordinator", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if !slices.ContainsFunc(h.Script.Rows[3], func(r string) bool { return strings.HasPrefix(r, "worker (now)") }) {
		t.Fatalf("role rows %q", h.Script.Rows[3])
	}
	wantTakeoverRows(t, h.Script.Rows[3], "proj.box+role.lxc")
	h.wantRuns(t, []string{"role", proj, "alpha", "coordinator", "--demote-current"})
	h.Script.HeadingAt(3, "Teams › Running sessions › ccy-proj › Role in alpha\nsession ccy-proj · seat 1 in team alpha · now worker")
}

func TestRepairIsInTheMenuOfAnUnguardedSession(t *testing.T) {
	h := newRig(t)
	s := h.seated()
	s.Unguarded, s.State = true, "unguarded: ccy-teams session repair "+proj
	h.ses.list = []hot.Session{s}
	h.Script.Fzf = []string{toSessions, "|ccy-proj", "|Repair", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	h.wantRuns(t, []string{"repair", proj})
}

func TestASessionThatCannotBeChangedIsOnlyAttachedAndTheMenuSaysWhy(t *testing.T) {
	h := newRig(t)
	h.ses.list = []hot.Session{{Name: proj, Kind: "ccy", Checkout: "-", State: "old or --no-bus: relaunch it to change it while it runs"}}
	h.Script.Fzf = []string{toSessions, "|ccy-proj", "|ABORT", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if menu := untagged(h.Script.Rows[2]); !slices.Equal(menu, []string{"Attach", "Back to the list"}) {
		t.Fatalf("menu %q", menu)
	}
	if !strings.Contains(h.HeaderOf(2), "ccy-proj cannot be changed while it runs. Relaunch it from ccy's tmux to change it.") {
		t.Fatalf("header %q", h.HeaderOf(2))
	}
	h.wantRuns(t)
}

func TestMoveFromTheMenuRunsSessionMove(t *testing.T) {
	h := newRig(t)
	h.withBeta()
	h.ses.list = []hot.Session{h.seated()}
	h.Script.Fzf = []string{toSessions, "|ccy-proj", "|Move to another team", "|beta", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if slices.ContainsFunc(h.Script.Rows[3], func(r string) bool { return strings.HasPrefix(r, "alpha ") }) {
		t.Fatalf("the team it leaves is not a team to move to: %q", h.Script.Rows[3])
	}
	h.wantRuns(t, []string{"move", proj, "alpha", "beta"})
}

// Moving the team's coordinator asks what becomes of its role, as removing it does.
func TestMoveOfTheCoordinatorAsksWhereTheRoleGoes(t *testing.T) {
	h := newRig(t)
	h.withBeta()
	h.coordinatorOfAlpha("proj.1+local.podman")
	h.ses.list = []hot.Session{h.seated()}
	h.Script.Fzf = []string{toSessions, "|ccy-proj", "|Move to another team", "|beta", "|goes to no one", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	h.wantRuns(t, []string{"move", proj, "alpha", "beta", "--coordinator=demote"})
	h.Script.HeadingAt(3, "Teams › Running sessions › ccy-proj › Move from alpha\nsession ccy-proj · seat 1 in team alpha")
	h.Script.HeadingAt(4, "Teams › Running sessions › ccy-proj › Move from alpha › Coordinator role\nsession ccy-proj · from team alpha to team beta")
}

// Change role on a team with no coordinator: worker (now), then the plain offer, which passes
// no --demote-current.
func TestRoleOnATeamWithNoCoordinatorIsOfferedPlainly(t *testing.T) {
	h := newRig(t)
	h.ses.list = []hot.Session{h.seated()}
	h.Script.Fzf = []string{toSessions, "|ccy-proj", "|Change role in alpha", "|alpha has none", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	rows := h.Script.Rows[3]
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
	h.Script.Fzf = []string{toSessions, "|ccy-proj", "|Add to a team", "|alpha", "|seat 2", "|coordinator (now)", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	rows := h.Script.Rows[5]
	if !slices.ContainsFunc(rows, func(r string) bool { return strings.HasPrefix(r, "coordinator (now)") }) ||
		slices.ContainsFunc(rows, func(r string) bool { return strings.Contains(r, "has none") || strings.HasPrefix(r, "Replace") }) {
		t.Fatalf("role rows %q", rows)
	}
	h.wantRuns(t, []string{"add", proj, "alpha/2", "--role=coordinator"})
}
