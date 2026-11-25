package seatchoice

import (
	"slices"
	"strings"
	"testing"
)

const team = "lts-v2"

func seats() []Seat {
	return []Seat{
		{Name: "10", Handle: "proj.10+site.podman"},
		{Name: "lead", Handle: "proj.lead+site.podman"},
		{Name: "2", Handle: "proj.2+site.podman", HeldBy: "ccy-proj"},
		{Name: "3", Handle: "proj.3+site.podman"},
	}
}

// S1: numbered seats first, by number, then named ones by name: v1's seat order
// (helpers/agent_bus/checkout.py _seat_order).
func TestOrderIsNumberedSeatsByNumberThenNamedOnes(t *testing.T) {
	var got []string
	for _, s := range Order(seats()) {
		got = append(got, s.Name)
	}
	if want := []string{"2", "3", "10", "lead"}; !slices.Equal(got, want) {
		t.Fatalf("order %v, want %v", got, want)
	}
}

// S2: the default is the lowest free seat (v1's seat_choices); a held seat never is.
func TestDefaultIsTheLowestFreeSeat(t *testing.T) {
	got, ok := Default(seats())
	if !ok || got.Name != "3" {
		t.Fatalf("default %+v, %v; want seat 3", got, ok)
	}
	allHeld := []Seat{{Name: "1", HeldBy: "ccy-a"}, {Name: "2", HeldBy: "ccy-b"}}
	if got, ok := Default(allHeld); ok {
		t.Fatalf("every seat held has no default, got %+v", got)
	}
	if _, ok := Default(nil); ok {
		t.Fatal("no seat has no default")
	}
}

// S3: a row names the seat, its handle and whether a running session holds it; a held row
// offers the re-attach, never a launch.
func TestRowsSayFreeOrHeldAndOfferTheReattach(t *testing.T) {
	free := Row(Seat{Name: "3", Handle: "proj.3+site.podman"})
	if !strings.Contains(free, "seat 3") || !strings.Contains(free, "proj.3+site.podman") || !strings.HasSuffix(free, "free") {
		t.Errorf("free row %q", free)
	}
	held := Row(Seat{Name: "2", Handle: "proj.2+site.podman", HeldBy: "ccy-proj"})
	if !strings.Contains(held, "held by session ccy-proj") || !strings.Contains(held, "re-attach") {
		t.Errorf("held row %q", held)
	}
}

// S4: the ways on name every seat, which are held, and the command for each: a free seat's
// launch, a held seat's re-attach, and another seat issued to this checkout.
func TestWaysOnNameEverySeatAndTheCommandForEach(t *testing.T) {
	text := strings.Join(WaysOn("ccy", team, "~/proj", seats()), "\n")
	for _, want := range []string{
		"Seats of " + team + " in this checkout (~/proj):",
		"seat 2", "held by session ccy-proj",
		"ccy --teams 3@" + team, "ccy --teams 10@" + team, "ccy --teams lead@" + team,
		"re-attach session ccy-proj (seat 2): ccy-sessions",
		"ccy-sessions restore ccy-proj",
		"teamchat seat place-issued --checkout ~/proj",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the ways on lack %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "ccy --teams 2@"+team) {
		t.Errorf("a held seat is offered as a launch:\n%s", text)
	}
	if !strings.Contains(strings.Join(WaysOn("cc", team, "~/proj", seats()), "\n"), "cc --teams 3@"+team) {
		t.Error("the launcher's own name is not the one in the command")
	}
}

// S5: with no seat at all, the ways on still name the way to have one.
func TestWaysOnWithNoSeatNameTheWayToHaveOne(t *testing.T) {
	text := strings.Join(WaysOn("ccy", team, "~/proj", nil), "\n")
	if !strings.Contains(text, "has no seat of "+team) || !strings.Contains(text, "place-issued") {
		t.Errorf("no seat:\n%s", text)
	}
}
