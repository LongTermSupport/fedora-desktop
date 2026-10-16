package picker

import (
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/coordinator"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/hot"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
)

// The members of every kind a team can have (Plan 00203 Task 3.8): a ccy seat of this checkout,
// a ccy member of another checkout, a cc session, an LXC container's agent, a docker and a vm
// member. The owner's rule: every kind is offered the same role actions, in the same words.
const (
	kindSeatHere   = "proj.1+local.podman"
	kindCcyThere   = "other.4+local.podman"
	kindCc         = "proj.2+desk.host"
	kindLxc        = "proj.1+worker.lxc"
	kindDocker     = "proj.1+box.docker"
	kindVM         = "proj.1+lab.vm"
	everyKindRoles = "HUMAN\towner\tjoin\nHUMAN\tguest\tinvite\n"
)

var everyKind = []string{kindSeatHere, kindCcyThere, kindCc, kindLxc, kindDocker, kindVM}

// everyKindRig is the machine with one member of each kind; coordinator holds the role ("" none).
func everyKindRig(t *testing.T, holder string) rig {
	t.Helper()
	h := newRig(t)
	var list strings.Builder
	list.WriteString(everyKindRoles)
	for _, handle := range everyKind {
		role := agentbus.RoleWorker
		if handle == holder {
			role = agentbus.RoleCoordinator
		}
		list.WriteString("MEMBER\t" + handle + "\t" + role + "\tjoin\tactive\n")
	}
	h.On(teamtest.SudoBus+"list alpha", list.String(), 0)
	return h
}

// memberRow is the row of handle: a seat row of this checkout shows the seat, any other the handle.
func memberRow(t *testing.T, snap *board.Snapshot, handle string) board.Row {
	t.Helper()
	for _, row := range snap.Rows {
		if row.Handle == handle {
			return row
		}
	}
	t.Fatalf("no row for %s", handle)
	return board.Row{}
}

// destructiveLine is whether a menu line parks, removes, takes down, shuts down or demotes, read
// from its words, independently of how the menu classes its actions.
func destructiveLine(line string) bool {
	for _, start := range []string{"Park ", "Remove ", "Take ", "Shut down ", "Replace the coordinator"} {
		if strings.HasPrefix(line, start) {
			return true
		}
	}
	return strings.Contains(line, " a worker (")
}

// checkOrder fails unless the first line is not destructive and every destructive line comes
// after every other one.
func checkOrder(t *testing.T, what string, lines []string) {
	t.Helper()
	if len(lines) == 0 {
		t.Fatalf("%s: an empty menu", what)
	}
	if destructiveLine(lines[0]) {
		t.Fatalf("%s: a destructive action is first, where the cursor starts: %q", what, lines)
	}
	seen := false
	for _, line := range lines {
		if destructiveLine(line) {
			seen = true
		} else if seen {
			t.Fatalf("%s: %q comes after a destructive action: %q", what, line, lines)
		}
	}
}

func TestNoMenuOfAnyRowStartsOnADestructiveActionAndEachComesLast(t *testing.T) {
	for _, holder := range append([]string{""}, everyKind...) {
		h := everyKindRig(t, holder)
		r := h.run1(t)
		for _, row := range r.snap.Rows {
			checkOrder(t, "holder "+holder+", row "+row.Member, h.labels(r, row))
		}
	}
}

func TestEveryMemberKindIsOfferedTheSameRoleActionInTheSameWords(t *testing.T) {
	for _, tc := range []struct {
		name, holder string
		// role is the role line a member other than the holder gets; holderLine the holder's.
		role       func(handle string) string
		holderLine func(handle string) string
	}{
		{"no coordinator", "", func(handle string) string { return coordinator.MakeChoice(handle, "alpha") }, nil},
		{
			"an LXC coordinator", kindLxc,
			func(string) string {
				return coordinator.TakeoverChoice(coordinator.Holder{Handle: kindLxc, Running: coordinator.Unknown})
			},
			func(handle string) string { return coordinator.WorkerChoice(handle, "alpha") },
		},
		{
			"a ccy coordinator", kindSeatHere,
			func(string) string {
				return coordinator.TakeoverChoice(coordinator.Holder{Handle: kindSeatHere, Running: coordinator.Active})
			},
			func(handle string) string { return coordinator.WorkerChoice(handle, "alpha") },
		},
	} {
		h := everyKindRig(t, tc.holder)
		r := h.run1(t)
		shared := make([][]string, 0, len(everyKind))
		for _, handle := range everyKind {
			labels := h.labels(r, memberRow(t, r.snap, handle))
			want := tc.role(handle)
			if handle == tc.holder {
				want = tc.holderLine(handle)
			}
			if !slices.Contains(labels, want) {
				t.Fatalf("%s: %s is not offered %q: %q", tc.name, handle, want, labels)
			}
			shared = append(shared, sharedShape(labels, handle, want))
		}
		for i := range shared {
			if !slices.Equal(shared[i], shared[0]) {
				t.Fatalf("%s: %s is offered %q, %s %q", tc.name, everyKind[i], shared[i], everyKind[0], shared[0])
			}
		}
	}
}

// sharedShape is a member menu's lines with its role line written ROLE, the member's own handle
// and seat as one word, and the ccy seat's own lines (attach, restore) left out: what every kind
// shares, in its order.
func sharedShape(labels []string, handle, roleLine string) []string {
	var shape []string
	for _, l := range labels {
		if strings.HasPrefix(l, "Attach ") || strings.Contains(l, "after a reboot") {
			continue
		}
		if l == roleLine {
			l = "ROLE"
		}
		l = strings.ReplaceAll(l, handle, "MEMBER")
		l = strings.Replace(l, "Remove seat 1 from alpha", "Park MEMBER", 1)
		shape = append(shape, l)
	}
	return shape
}

func TestASessionsMenuListsRemovingItFromATeamLast(t *testing.T) {
	h := newRig(t)
	r := h.run1(t)
	s := hot.Session{Name: "ccy-proj", Controllable: true, Unguarded: true, Teams: []hot.Cell{
		{Team: "alpha", Seat: "1", Role: agentbus.RoleCoordinator}, {Team: "beta", Seat: "2", Role: agentbus.RoleWorker},
	}}
	cs := r.sessionChoices(s)
	lines := make([]string, len(cs))
	for i, c := range cs {
		lines[i] = c.line
	}
	checkOrder(t, "session menu", lines)
}

func TestTheRoleQuestionOfACoordinatorStartsOnTheRoleItHas(t *testing.T) {
	opts := roleOptions("alpha", roleCoordinator, coordinator.Holder{})
	if opts[0].value != roleCoordinator || !strings.HasSuffix(opts[0].line, "(now)") {
		t.Fatalf("the cursor starts on making the coordinator a worker: %+v", opts)
	}
}
