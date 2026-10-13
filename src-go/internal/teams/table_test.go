package teams

import (
	"regexp"
	"strings"
	"testing"
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
	r := Row{Team: "alpha", Member: "-", Kind: "team", State: "up", Applied: "up to date", Session: "-", Restore: "-", Where: "port 6167"}
	want := "alpha" + strings.Repeat(" ", 19) + "  -" + strings.Repeat(" ", 35) + "  team    up             up to date          -" +
		strings.Repeat(" ", 25) + "  -        port 6167"
	if got := r.Text(); got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestPaintedRowTextEqualsThePlainRow(t *testing.T) {
	rows := []Row{
		{Team: "alpha", Member: "-", Kind: "team", State: "up", Applied: "up to date", Session: "-", Restore: "-", Where: "port 6167"},
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
	if !strings.Contains(rows[0].Painted(), cBold+"alpha") || !strings.Contains(rows[0].Painted(), cGreen+"up ") {
		t.Fatalf("the team row is bold and its words coloured: %q", rows[0].Painted())
	}
	if strings.Contains(rows[1].Painted(), cBold) {
		t.Fatalf("only a team row is bold: %q", rows[1].Painted())
	}
}

func TestHeading(t *testing.T) {
	h := headingText()
	for _, w := range []string{"TEAM", "MEMBER", "KIND", "STATE", "APPLIED", "SESSION", "RESTORE", "WHERE"} {
		if !strings.Contains(h, w) {
			t.Fatalf("heading %q lacks %s", h, w)
		}
	}
}
