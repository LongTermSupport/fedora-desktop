package board

import (
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

func TestWordColour(t *testing.T) {
	cases := map[string]string{
		"free": cYellow, "invited": cYellow, "no": cYellow, "parked": cYellow,
		"never signed in": cYellow, "alpha-1(stopped)": cYellow,
		"up": cGreen, "active": cGreen, "held": cGreen, "in room": cGreen, "yes": cGreen, "2 signed in": cGreen,
		"out of date": cYellow, "out of date: config": cYellow, "not built": cYellow,
		"up to date":      cGreen,
		"down (inactive)": cRed, "not installed": cRed, "not in room": cRed, "unknown": cRed,
		"need sudo": cRed, "decide: absent": cRed, "unreadable": cRed,
		"removed": "", "ccy-proj": "", "-": "",
	}
	for word, want := range cases {
		if got := wordColour(word); got != want {
			t.Errorf("wordColour(%q) = %q, want %q", word, got, want)
		}
	}
}

// sample is a team with a seat of a long handle that is its coordinator, a worker seat of
// another checkout with the same seat number, a member with no checkout, and a person.
func sample() []Row {
	return []Row{
		{Team: "alpha", Member: "Builds the site", Kind: KindTeam, State: "up", Applied: "up to date", Session: "-", Restore: "-", Where: "port 6167", Handle: "-", Dir: "-"},
		{
			Team: "alpha", Member: "1", Kind: KindCcy, State: "active", Applied: "-", Session: "ccy-proj", Restore: "yes", Where: "~/proj",
			Handle: "a-rather-long-repository-name.1+build-host.podman", Dir: "/proj", Role: "coordinator",
		},
		{
			Team: "alpha", Member: "1", Kind: KindCcy, State: "unknown", Applied: "-", Session: "x(stopped)", Restore: "no", Where: "~/other",
			Handle: "other.1+local.podman", Dir: "/other", Role: "worker",
		},
		{
			Team: "alpha", Member: "proj.2+local.podman", Kind: KindCcy, State: "parked", Applied: "-", Session: "-", Restore: "-", Where: "worker",
			Handle: "proj.2+local.podman", Dir: "-", Role: "worker",
		},
		{Team: "alpha", Member: "owner", Kind: KindHuman, State: "in room", Applied: "-", Session: "never signed in", Restore: "-", Where: "Element", Handle: "-", Dir: "-"},
	}
}

// starts is where each heading begins on the line.
func starts(t *testing.T, heading string) []int {
	t.Helper()
	at := make([]int, 0, len(headings))
	for _, h := range headings {
		i := strings.Index(heading, h)
		if i < 0 {
			t.Fatalf("heading %q lacks %s", heading, h)
		}
		at = append(at, utf8.RuneCountInString(heading[:i]))
	}
	return at
}

func TestEverySeatShowsItsWholeHandleAndTheCoordinatorItsRole(t *testing.T) {
	rows := sample()
	tb := Layout(rows, 0)
	for i, want := range []string{
		"a-rather-long-repository-name.1+build-host.podman (coordinator)", "other.1+local.podman  ",
		"proj.2+local.podman  ", "owner  ",
	} {
		if got := tb.Text(rows[i+1]); !strings.Contains(got, "alpha  "+want) {
			t.Errorf("row %d %q lacks %q", i+1, got, want)
		}
	}
	if strings.Contains(tb.Text(rows[2]), "coordinator") || strings.Contains(tb.Text(rows[3]), "(worker)") {
		t.Fatal("a worker is marked")
	}
	if strings.Contains(tb.Text(rows[1]), "…") {
		t.Fatal("a handle is cut")
	}
}

func TestEachColumnIsAsWideAsItsLongestValueSoNoRowShifts(t *testing.T) {
	rows := sample()
	tb := Layout(rows, 0)
	at := starts(t, tb.Heading())
	for _, r := range rows {
		line := tb.Text(r)
		for i, c := range r.cells() {
			if i == colMember && r.Kind == KindTeam {
				continue
			}
			got := []rune(line)
			n := utf8.RuneCountInString(c)
			if at[i]+n > len(got) || string(got[at[i]:at[i]+n]) != c {
				t.Errorf("column %s of %q does not start under its heading at %d", headings[i], line, at[i])
			}
		}
		if painted := ansi.ReplaceAllString(tb.Painted(r), ""); painted != line {
			t.Errorf("painted %q strips to %q, want %q", tb.Painted(r), painted, line)
		}
	}
	if !strings.HasPrefix(tb.Heading(), "TEAM   MEMBER") {
		t.Fatalf("TEAM is as wide as its longest value: %q", tb.Heading())
	}
}

// The owner's rule: nothing is cut short. A long description widens the MEMBER column like any
// value; on a terminal too narrow for that, it moves off the row with the other columns, whole.
func TestADescriptionIsNeverCut(t *testing.T) {
	long := "Coordinates the release of every example service across all regions"
	rows := sample()
	rows[0].Member = long
	for _, width := range []int{0, 500} {
		tb := Layout(rows, width)
		if tb.Narrow() || !strings.Contains(tb.Text(rows[0]), long+"  team ") || strings.Contains(tb.Text(rows[0]), "…") {
			t.Fatalf("width %d: the description is whole on its row and the column fits it: %q", width, tb.Text(rows[0]))
		}
		at := starts(t, tb.Heading())
		if got := []rune(tb.Text(rows[1])); string(got[at[colKind]:at[colKind]+3]) != "ccy" {
			t.Fatalf("width %d: a seat row's KIND does not line up under its heading: %q", width, tb.Text(rows[1]))
		}
	}
	tb := Layout(rows, 100)
	if !tb.Narrow() || strings.Contains(tb.Text(rows[0]), "Coordinates") || !strings.HasPrefix(tb.Rest(rows[0]), "ABOUT "+long+"   APPLIED ") {
		t.Fatalf("too narrow: the description moves off the row, whole: %q / %q", tb.Text(rows[0]), tb.Rest(rows[0]))
	}
	if strings.Contains(tb.Rest(rows[1]), "ABOUT") {
		t.Fatalf("only a team row has a description: %q", tb.Rest(rows[1]))
	}
}

func TestColumnsAreMeasuredInTerminalCells(t *testing.T) {
	wide := string([]rune{0x65e5, 0x672c, 0x8a9e}) // three characters, six cells
	rows := sample()
	rows[0].Member = wide
	tb := Layout(rows, 0)
	heading := tb.Heading()
	kindAt := strings.Index(heading, "KIND")
	// MEMBER is as wide as its longest value in cells: the seat row's KIND starts at the same
	// byte offset as the heading's, the team row's after the wide text's extra bytes.
	if line := tb.Text(rows[1]); line[kindAt:kindAt+3] != "ccy" {
		t.Fatalf("seat row %q misaligned against %q", line, heading)
	}
	team := tb.Text(rows[0])
	if cells := strings.Index(team, "team") - len(wide) + 6; cells != kindAt {
		t.Fatalf("team row %q: KIND at cell %d, heading at %d", team, cells, kindAt)
	}
}

func TestTheTeamRowIsBoldAndItsWordsColoured(t *testing.T) {
	rows := sample()
	tb := Layout(rows, 0)
	if !strings.Contains(tb.Painted(rows[0]), term.Bold+"alpha") || !strings.Contains(tb.Painted(rows[0]), cGreen+"up ") {
		t.Fatalf("the team row is bold and its words coloured: %q", tb.Painted(rows[0]))
	}
	if strings.Contains(tb.Painted(rows[1]), term.Bold) {
		t.Fatalf("only a team row is bold: %q", tb.Painted(rows[1]))
	}
}

func TestATableTooWideForTheTerminalKeepsTheRowWholeAndMovesTheRestOff(t *testing.T) {
	rows := sample()
	full := Layout(rows, 0)
	width := len([]rune(full.Text(rows[1]))) - 1
	tb := Layout(rows, width)
	if !tb.Narrow() || full.Narrow() || Layout(rows, width+60).Narrow() {
		t.Fatal("only a table wider than the terminal is narrow")
	}
	heading := tb.Heading()
	if heading != strings.TrimRight(heading, " ") || strings.Contains(heading, "APPLIED") || !strings.Contains(heading, "STATE") {
		t.Fatalf("the narrow heading is TEAM MEMBER KIND STATE: %q", heading)
	}
	for _, r := range rows {
		line := tb.Text(r)
		if len([]rune(line)) > width {
			t.Errorf("a narrow row is wider than %d: %q", width, line)
		}
		if ansi.ReplaceAllString(tb.Painted(r), "") != line {
			t.Errorf("painted %q is not %q", tb.Painted(r), line)
		}
		rest := tb.Rest(r)
		for _, want := range []string{"APPLIED " + r.Applied, "SESSION " + r.Session, "RESTORE " + r.Restore, "WHERE " + r.Where} {
			if !strings.Contains(rest, want) {
				t.Errorf("the rest of %q lacks %q: %q", line, want, rest)
			}
		}
	}
	if !strings.Contains(tb.Text(rows[1]), "a-rather-long-repository-name.1+build-host.podman (coordinator)") {
		t.Fatalf("the handle stays whole: %q", tb.Text(rows[1]))
	}
	if full.Rest(rows[1]) != "" {
		t.Fatal("a table that fits moves nothing off its rows")
	}
}

func TestAHandleWiderThanEvenTheNarrowTableIsStillWhole(t *testing.T) {
	rows := sample()
	tb := Layout(rows, 20)
	if !tb.Narrow() || !strings.Contains(tb.Text(rows[1]), "a-rather-long-repository-name.1+build-host.podman (coordinator)") {
		t.Fatalf("got %q", tb.Text(rows[1]))
	}
}
