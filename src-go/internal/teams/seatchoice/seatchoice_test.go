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

// nowMS is the tests' clock: a seat that last ran two hours before it.
const nowMS = int64(1_800_000_000_000)

const twoHours = int64(2 * 60 * 60 * 1000)

// S2 (Plan 00265 R90): the default is the free seat that last ran here, the newest; with none,
// the lowest free seat; a held seat never is.
func TestDefaultIsTheFreeSeatThatLastRan(t *testing.T) {
	got, ok := Default(seats())
	if !ok || got.Name != "3" {
		t.Fatalf("default %+v, %v; want seat 3", got, ok)
	}
	used := seats()
	used[0].LastRanMS = nowMS - twoHours
	used[3].LastRanMS = nowMS - 2*twoHours
	if got, ok := Default(used); !ok || got.Name != "10" {
		t.Fatalf("default %+v, %v; want seat 10, the newest that ran", got, ok)
	}
	allHeld := []Seat{{Name: "1", HeldBy: "ccy-a"}, {Name: "2", HeldBy: "ccy-b"}}
	if got, ok := Default(allHeld); ok {
		t.Fatalf("every seat held has no default, got %+v", got)
	}
	if _, ok := Default(nil); ok {
		t.Fatal("no seat has no default")
	}
}

// S9 (Plan 00265 R90): Pick takes, without asking, the one free seat with a conversation, or a
// free seat when none was ever used and none is held (they are the same); two free seats with a
// conversation, or a held one beside the free ones, are a genuine choice.
func TestPickTakesTheSeatThisCheckoutWorkedInAndAsksOnlyAGenuineChoice(t *testing.T) {
	free := []Seat{{Name: "1", Handle: "p.1+s.podman"}, {Name: "2", Handle: "p.2+s.podman"}}
	if got, ok := Pick(free); !ok || got.Name != "1" {
		t.Errorf("never used: %+v %v", got, ok)
	}
	oneUsed := slices.Clone(free)
	oneUsed[1].LastRanMS = nowMS
	if got, ok := Pick(oneUsed); !ok || got.Name != "2" {
		t.Errorf("one with a conversation: %+v %v", got, ok)
	}
	twoUsed := slices.Clone(oneUsed)
	twoUsed[0].LastRanMS = nowMS - twoHours
	if _, ok := Pick(twoUsed); ok {
		t.Error("two seats with a conversation were decided without asking")
	}
	withHeld := append(slices.Clone(free), Seat{Name: "3", HeldBy: "ccy-proj"})
	if _, ok := Pick(withHeld); ok {
		t.Error("a held seat beside free ones was decided without asking")
	}
	if _, ok := Pick(nil); ok {
		t.Error("no seat was decided")
	}
}

// S3 (Plan 00265 R90): a row says a seat in words, never its name or handle: when it last ran
// here, or that it never did, or the session holding it with the re-attach.
func TestRowsSayASeatInWords(t *testing.T) {
	used := Row(Seat{Name: "3", Handle: "proj.3+site.podman", LastRanMS: nowMS - twoHours}, team, nowMS)
	never := Row(Seat{Name: "4", Handle: "proj.4+site.podman"}, team, nowMS)
	held := Row(Seat{Name: "2", Handle: "proj.2+site.podman", HeldBy: "ccy-proj"}, team, nowMS)
	if used != "A seat of "+team+": last ran 2 hours ago" {
		t.Errorf("used row %q", used)
	}
	if never != "A free seat of "+team {
		t.Errorf("never-used row %q", never)
	}
	if !strings.Contains(held, "Running now in session ccy-proj") || !strings.Contains(held, "re-attach") {
		t.Errorf("held row %q", held)
	}
	for _, row := range []string{used, never, held} {
		if strings.Contains(row, "proj.") || strings.Contains(row, "seat 3") || strings.Contains(row, "seat 2") {
			t.Errorf("a row names its seat: %q", row)
		}
	}
}

// S3 (Plan 00265 R90, Q5): a row names the seat by its role in the team, and a seat that ran by
// its conversation's first prompt too.
func TestRowsNameTheRoleAndTheFirstPrompt(t *testing.T) {
	coordinator := Row(Seat{Name: "1", Handle: "proj.1+site.podman", Role: "coordinator", LastRanMS: nowMS - twoHours, Prompt: "Fix the restore"}, team, nowMS)
	if want := "The coordinator of " + team + `: last ran 2 hours ago, "Fix the restore"`; coordinator != want {
		t.Errorf("coordinator row %q, want %q", coordinator, want)
	}
	worker := Row(Seat{Name: "3", Handle: "proj.3+site.podman", Role: "worker", LastRanMS: nowMS - twoHours}, team, nowMS)
	if want := "A worker of " + team + ": last ran 2 hours ago"; worker != want {
		t.Errorf("worker row %q, want %q", worker, want)
	}
	free := Row(Seat{Name: "4", Handle: "proj.4+site.podman", Role: "worker"}, team, nowMS)
	if want := "A free worker seat of " + team; free != want {
		t.Errorf("free worker row %q, want %q", free, want)
	}
	held := Row(Seat{Name: "2", Handle: "proj.2+site.podman", Role: "coordinator", HeldBy: "ccy-proj"}, team, nowMS)
	if want := "Running now in session ccy-proj (the coordinator of " + team + "): re-attach it"; held != want {
		t.Errorf("held row %q, want %q", held, want)
	}
	if got := Describe(Seat{Role: "probe"}, team); got != "A probe of "+team {
		t.Errorf("Describe of another role = %q", got)
	}
}

// S3: how long ago a seat ran, in words.
func TestAgoSaysHowLongAgoInWords(t *testing.T) {
	for _, c := range []struct {
		ago  int64
		want string
	}{
		{0, "just now"},
		{90_000, "1 minute ago"},
		{5 * 60_000, "5 minutes ago"},
		{twoHours / 2, "1 hour ago"},
		{twoHours, "2 hours ago"},
		{13 * twoHours, "yesterday"},
		{36 * twoHours, "3 days ago"},
	} {
		if got := Ago(nowMS, nowMS-c.ago); got != c.want {
			t.Errorf("Ago(%d ms) = %q, want %q", c.ago, got, c.want)
		}
	}
}

// S4: the ways on name every seat in words, which are held, and the command for each: the plain
// launch by team, which asks, a held seat's re-attach, and a new seat; never a seat to type.
func TestWaysOnNameEverySeatAndTheCommandForEach(t *testing.T) {
	text := strings.Join(WaysOn(KindPodman, team, "~/proj", seats(), nowMS), "\n")
	for _, want := range []string{
		"Seats of " + team + " in this checkout (~/proj):",
		"Running now in session ccy-proj",
		"take a free seat: run ccy --teams " + team + " at a terminal",
		"re-attach session ccy-proj: ccy-sessions",
		"ccy-sessions restore ccy-proj",
		"a new seat: ccy-teams seats add " + team + ", then ccy --teams " + team,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the ways on lack %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "@"+team) || strings.Contains(text, "<seat>") {
		t.Errorf("the ways on name a seat to type:\n%s", text)
	}
	host := strings.Join(WaysOn(KindHost, team, "~/proj", seats(), nowMS), "\n")
	if !strings.Contains(host, "run cc --teams "+team) || !strings.Contains(host, "a new seat: ccy-teams seats add "+team+" --host, then cc --teams "+team) {
		t.Errorf("a host launch's ways on do not name cc and a host seat's add:\n%s", host)
	}
}

// S5: with no seat at all, the ways on still name the way to have one, of the launch's kind.
func TestWaysOnWithNoSeatNameTheWayToHaveOne(t *testing.T) {
	text := strings.Join(WaysOn(KindPodman, team, "~/proj", nil, nowMS), "\n")
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
}

// S8 (register R55): a seat named for a launch of the other kind is refused naming its kind, the
// launcher it belongs to, and the add of a seat of the launch's own kind.
func TestASeatOfTheOtherKindIsRefusedNamingIt(t *testing.T) {
	got := WrongKind("2", team, "proj.2+site.podman", KindHost)
	for _, want := range []string{
		"2@" + team + " is a podman seat (proj.2+site.podman)", "this is a cc (host) launch",
		"launch it with ccy --teams " + team, "ccy-teams seats add " + team + " --host, then cc --teams " + team,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the refusal lacks %q: %s", want, got)
		}
	}
	if got := WrongKind("4", team, "proj.4+site.host", KindPodman); !strings.Contains(got, "launch it with cc --teams "+team) || !strings.Contains(got, "a ccy (podman) launch") {
		t.Errorf("podman launch refusal %s", got)
	}
}

// S8: a seat of a kind neither launcher takes (an LXC member's) is refused naming its kind, and
// names no launcher for it.
func TestASeatOfAKindNoLauncherTakesIsRefusedNamingIt(t *testing.T) {
	got := WrongKind("5", team, "proj.5+site.lxc", KindPodman)
	for _, want := range []string{
		"5@" + team + " is a lxc seat (proj.5+site.lxc)", "neither ccy nor cc launches it", "this is a ccy (podman) launch",
		"ccy-teams seats add " + team + ", then ccy --teams " + team,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the refusal lacks %q: %s", want, got)
		}
	}
	if strings.Contains(got, "launch it with") {
		t.Errorf("the refusal names a launcher for an lxc seat: %s", got)
	}
}

// S7: AnyKind is every seat with a handle, whatever its kind.
func TestAnyKindIsEverySeat(t *testing.T) {
	mixed := []Seat{{Name: "1", Handle: "proj.1+site.podman"}, {Name: "2", Handle: "proj.2+site.lxc"}, {Name: "x", Handle: "not a handle"}}
	if got := OfKind(mixed, AnyKind); len(got) != 2 || got[1].Name != "2" {
		t.Errorf("OfKind(AnyKind) %+v", got)
	}
	if !IsKind("proj.2+site.lxc", AnyKind) || IsKind("not a handle", AnyKind) || IsKind("proj.2+site.lxc", KindHost) {
		t.Error("IsKind does not match a handle's kind, or AnyKind every handle")
	}
}
