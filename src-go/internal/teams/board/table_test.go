package board

import (
	"regexp"
	"strings"
	"testing"

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

func TestRowTextUsesTheColumnWidths(t *testing.T) {
	r := Row{Team: "alpha", Member: "-", Kind: KindTeam, State: "up", Applied: "up to date", Session: "-", Restore: "-", Where: "port 6167"}
	want := "alpha" + strings.Repeat(" ", 19) + "  -" + strings.Repeat(" ", 35) + "  team    up             up to date          -" +
		strings.Repeat(" ", 25) + "  -        port 6167"
	if got := r.Text(); got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestALongDescriptionIsCutOnATerminalAndWholeWhenPiped(t *testing.T) {
	long := "Coordinates the release of every example service across all regions"
	r := Row{Team: "alpha", Member: long, Kind: KindTeam, State: "up", Applied: "up to date", Session: "-", Restore: "-", Where: "port 6167"}
	if !strings.Contains(r.Text(), long) {
		t.Fatalf("the plain row (--list piped) keeps it whole: %q", r.Text())
	}
	painted := ansi.ReplaceAllString(r.Painted(), "")
	cut := long[:35] + "…"
	if !strings.Contains(painted, cut+"  team ") || strings.Contains(painted, long) {
		t.Fatalf("the painted row (a terminal, the picker) cuts it to 36 characters with an ellipsis: %q", painted)
	}
	r.Member = "Short one"
	if ansi.ReplaceAllString(r.Painted(), "") != r.Text() {
		t.Fatal("a description that fits is not cut")
	}
}

func TestPaintedRowTextEqualsThePlainRow(t *testing.T) {
	rows := []Row{
		{Team: "alpha", Member: "-", Kind: KindTeam, State: "up", Applied: "up to date", Session: "-", Restore: "-", Where: "port 6167"},
		{Team: "alpha", Member: "1", Kind: "ccy", State: "active", Applied: "-", Session: "ccy-proj", Restore: "yes", Where: "~/proj"},
		{Team: "alpha", Member: "owner", Kind: "human", State: "in room", Applied: "-", Session: "never signed in", Restore: "-", Where: "Element"},
		{Team: "alpha", Member: "1", Kind: "ccy", State: "unknown", Applied: "-", Session: "x(stopped)", Restore: "no", Where: "~/p"},
	}
	for _, r := range rows {
		painted := r.Painted()
		if got := ansi.ReplaceAllString(painted, ""); got != r.Text() {
			t.Errorf("painted %q strips to %q, want %q", painted, got, r.Text())
		}
	}
	if !strings.Contains(rows[0].Painted(), term.Bold+"alpha") || !strings.Contains(rows[0].Painted(), cGreen+"up ") {
		t.Fatalf("the team row is bold and its words coloured: %q", rows[0].Painted())
	}
	if strings.Contains(rows[1].Painted(), term.Bold) {
		t.Fatalf("only a team row is bold: %q", rows[1].Painted())
	}
}

func TestTheCoordinatorsSeatRowSaysSoAndPaintsTheSame(t *testing.T) {
	r := Row{
		Team: "alpha", Member: "1", Kind: "ccy", State: "active", Applied: "-", Session: "ccy-proj", Restore: "yes",
		Where: "~/proj", Dir: "/proj", Role: "coordinator",
	}
	if !strings.Contains(r.Text(), "1 (coordinator)") {
		t.Fatalf("got %q", r.Text())
	}
	if got := ansi.ReplaceAllString(r.Painted(), ""); got != r.Text() {
		t.Fatalf("painted %q strips to %q, want %q", r.Painted(), got, r.Text())
	}
	r.Role = "worker"
	if strings.Contains(r.Text(), "coordinator") {
		t.Fatalf("a worker's row says coordinator: %q", r.Text())
	}
}

func TestAMemberWithNoCheckoutKeepsItsHandleAndSaysTheRoleInWhere(t *testing.T) {
	r := Row{
		Team: "alpha", Member: "proj.2+local.podman", Kind: "ccy", State: "active", Applied: "-", Session: "-",
		Restore: "-", Where: "coordinator", Dir: "-", Role: "coordinator",
	}
	if strings.Contains(r.Text(), "(coordinator)") {
		t.Fatalf("a long handle must not be widened: %q", r.Text())
	}
}

func TestHeading(t *testing.T) {
	h := HeadingText()
	for _, w := range []string{"TEAM", "MEMBER", "KIND", "STATE", "APPLIED", "SESSION", "RESTORE", "WHERE"} {
		if !strings.Contains(h, w) {
			t.Fatalf("heading %q lacks %s", h, w)
		}
	}
}

func TestACrumbNamesASeatAsASeatAndAnyoneElseByName(t *testing.T) {
	for _, c := range []struct {
		row  Row
		want string
	}{
		{Row{Kind: KindCcy, Member: "1", Dir: "/srv/proj"}, "seat 1"},
		{Row{Kind: KindCcy, Member: "proj.1+local.podman", Dir: "-"}, "proj.1+local.podman"},
		{Row{Kind: KindHuman, Member: "owner"}, "owner"},
		{Row{Kind: "lxc", Member: "proj.box+role.lxc"}, "proj.box+role.lxc"},
	} {
		if got := c.row.Crumb(); got != c.want {
			t.Errorf("%+v: crumb %q, want %q", c.row, got, c.want)
		}
	}
}
