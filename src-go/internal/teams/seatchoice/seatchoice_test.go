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
	text := strings.Join(WaysOn(KindPodman, team, "~/proj", seats()), "\n")
	for _, want := range []string{
		"Seats of " + team + " in this checkout (~/proj):",
		"seat 2", "held by session ccy-proj",
		"ccy --teams 3@" + team, "ccy --teams 10@" + team, "ccy --teams lead@" + team,
		"re-attach session ccy-proj (seat 2): ccy-sessions",
		"ccy-sessions restore ccy-proj",
		"a new seat: ccy-teams seats add " + team + ", then ccy --teams <seat>@" + team,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the ways on lack %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "ccy --teams 2@"+team) {
		t.Errorf("a held seat is offered as a launch:\n%s", text)
	}
	host := strings.Join(WaysOn(KindHost, team, "~/proj", seats()), "\n")
	if !strings.Contains(host, "cc --teams 3@"+team) || !strings.Contains(host, "a new seat: ccy-teams seats add "+team+" --host, then cc --teams") {
		t.Errorf("a host launch's ways on do not name cc and a host seat's add:\n%s", host)
	}
}

// S5: with no seat at all, the ways on still name the way to have one, of the launch's kind.
func TestWaysOnWithNoSeatNameTheWayToHaveOne(t *testing.T) {
	text := strings.Join(WaysOn(KindPodman, team, "~/proj", nil), "\n")
	if !strings.Contains(text, "has no podman seat of "+team) || !strings.Contains(text, AddCommand(team, KindPodman)) {
		t.Errorf("no seat:\n%s", text)
	}
}

// S6: the new-seat row names the command that adds a seat of the launch's kind.
func TestTheNewSeatRowNamesItsCommand(t *testing.T) {
	if got := NewRow(team, KindPodman); !strings.Contains(got, "ccy-teams seats add "+team+")") {
		t.Errorf("new row %q", got)
	}
	if got := NewRow(team, KindHost); !strings.Contains(got, "new host seat") || !strings.Contains(got, "ccy-teams seats add "+team+" --host") {
		t.Errorf("host new row %q", got)
	}
}

// S7 (register R55): a seat's kind is its handle's type; a launch is offered only the seats of
// its own kind, and its launcher is ccy for podman, cc for host.
func TestOnlyTheLaunchKindsSeatsAreOffered(t *testing.T) {
	mixed := append(seats(), Seat{Name: "4", Handle: "proj.4+site.host"}, Seat{Name: "x", Handle: "not a handle"})
	var podman, host []string
	for _, s := range OfKind(mixed, KindPodman) {
		podman = append(podman, s.Name)
	}
	for _, s := range OfKind(mixed, KindHost) {
		host = append(host, s.Name)
	}
	if want := []string{"10", "lead", "2", "3"}; !slices.Equal(podman, want) {
		t.Errorf("podman seats %v, want %v", podman, want)
	}
	if want := []string{"4"}; !slices.Equal(host, want) {
		t.Errorf("host seats %v, want %v", host, want)
	}
	if KindOf("proj.4+site.host") != KindHost || KindOf("not a handle") != "" {
		t.Error("KindOf does not read the handle's type")
	}
	if Launcher(KindHost) != "cc" || Launcher(KindPodman) != "ccy" {
		t.Error("the launcher of a kind is wrong")
	}
	if Other(KindHost) != KindPodman || Other(KindPodman) != KindHost {
		t.Error("the other kind is wrong")
	}
}

// S8 (register R55): a seat named for a launch of the other kind is refused naming its kind, the
// launcher it belongs to, and the add of a seat of the launch's own kind.
func TestASeatOfTheOtherKindIsRefusedNamingIt(t *testing.T) {
	got := WrongKind("2", team, "proj.2+site.podman", KindHost)
	for _, want := range []string{
		"2@" + team + " is a podman seat (proj.2+site.podman)", "this is a cc (host) launch",
		"ccy --teams 2@" + team, "ccy-teams seats add " + team + " --host",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the refusal lacks %q: %s", want, got)
		}
	}
	if got := WrongKind("4", team, "proj.4+site.host", KindPodman); !strings.Contains(got, "cc --teams 4@"+team) || !strings.Contains(got, "a ccy (podman) launch") {
		t.Errorf("podman launch refusal %s", got)
	}
}
