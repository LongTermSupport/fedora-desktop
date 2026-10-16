package coordinator

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
)

const (
	team    = "alpha"
	current = "other.5+local.podman"
	who     = "ccy-proj"
)

func roster(members ...agentbus.Member) agentbus.Roster { return agentbus.Roster{Members: members} }

func TestOfIsTheOtherMemberThatHoldsTheRole(t *testing.T) {
	r := roster(agentbus.Member{Handle: "proj.3+local.podman", Role: agentbus.RoleWorker},
		agentbus.Member{Handle: current, Role: agentbus.RoleCoordinator})
	if m, held := Of(r, "proj.3+local.podman"); !held || m.Handle != current {
		t.Fatalf("held %v by %q", held, m.Handle)
	}
	if _, held := Of(r, current); held {
		t.Fatal("the coordinator itself is no other holder")
	}
	if _, held := Of(roster(agentbus.Member{Handle: current, Role: agentbus.RoleWorker}), "x"); held {
		t.Fatal("a team of workers has no coordinator")
	}
}

func TestTheTwoPathsAreWordedApart(t *testing.T) {
	if got, want := OfferNone(team, who), "alpha has no coordinator. Make ccy-proj its coordinator?"; got != want {
		t.Fatalf("no coordinator:\n got %q\nwant %q", got, want)
	}
	h := Holder{Handle: current, Running: Active}
	want := "alpha's coordinator is other.5+local.podman (running). A team has one coordinator. " +
		"Remove other.5+local.podman as coordinator and make ccy-proj the coordinator instead?"
	if got := OfferTakeover(team, who, h); got != want {
		t.Fatalf("takeover:\n got %q\nwant %q", got, want)
	}
}

func TestTheHolderSaysWhetherItIsActive(t *testing.T) {
	for _, tc := range []struct {
		running Running
		want    string
	}{
		{Active, current + " (running)"},
		{Stopped, current + " (not running)"},
		{Parked, current + " (parked)"},
		{Unknown, current + " (whether it is running is not known here)"},
	} {
		if got := (Holder{Handle: current, Running: tc.running}).Named(); got != tc.want {
			t.Errorf("got %q, want %q", got, tc.want)
		}
	}
}

func TestTheChoicesNeverReadAsAPlainCoordinator(t *testing.T) {
	if got, want := NoneChoice(team), "Make it the coordinator (alpha has none)"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	want := "Replace the coordinator, other.5+local.podman (not running): it becomes a worker..."
	if got := TakeoverChoice(Holder{Handle: current, Running: Stopped}); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAMembersMenuNamesTheMemberInBothRoleLines(t *testing.T) {
	if got, want := MakeChoice("proj.1+worker.lxc", team), "Make proj.1+worker.lxc the coordinator (alpha has none)"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got, want := WorkerChoice("proj.1+worker.lxc", team), "Make proj.1+worker.lxc a worker (alpha then has no coordinator)"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestTheRefusalNamesTheCurrentCoordinatorAndTheFlag(t *testing.T) {
	want := "alpha's coordinator is other.5+local.podman (running). A team has one coordinator: to remove " +
		"other.5+local.podman as coordinator and make ccy-proj the coordinator instead, add --demote-current. Nothing was changed."
	if got := Refusal(team, who, Holder{Handle: current, Running: Active}, "--demote-current"); got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestRunningFromSeats(t *testing.T) {
	seats := []agentbus.Seat{{Team: team, Seat: "5", Held: true, Handle: current}, {Team: team, Seat: "6", Handle: "free.6+local.podman"}}
	look := FromSeats(seats)
	for _, tc := range []struct {
		m    agentbus.Member
		want Running
	}{
		{agentbus.Member{Handle: current}, Active},
		{agentbus.Member{Handle: "free.6+local.podman"}, Stopped},
		{agentbus.Member{Handle: current, Parked: true}, Parked},
		{agentbus.Member{Handle: "lxc.1+box.lxc"}, Unknown},
	} {
		if got := look(tc.m); got != tc.want {
			t.Errorf("%+v: got %v, want %v", tc.m, got, tc.want)
		}
	}
	if got := Holding(roster(agentbus.Member{Handle: current, Role: agentbus.RoleCoordinator}), "me", look); got.Running != Active || got.Handle != current {
		t.Fatalf("holding %+v", got)
	}
	if got := Holding(roster(), "me", nil); got.Handle != "" {
		t.Fatalf("no holder %+v", got)
	}
}
