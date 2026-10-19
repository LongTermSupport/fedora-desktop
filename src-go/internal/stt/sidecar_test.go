package stt

import (
	"strings"
	"testing"
	"time"
)

func TestClock(t *testing.T) {
	for secs, want := range map[float64]string{0: "00:00:00", 9.9: "00:00:09", 61: "00:01:01", 3725: "01:02:05"} {
		if got := clock(secs); got != want {
			t.Errorf("clock(%v) = %q, want %q", secs, got, want)
		}
	}
}

func TestParagraphsBreakAtAPauseOfThreeSeconds(t *testing.T) {
	segments := []segment{{0, 2, "One."}, {4.9, 6, "Two."}, {9, 10, "Three."}}
	got := paragraphs(segments)
	want := "**[00:00:00]** One. Two.\n\n**[00:00:09]** Three.\n"
	if got != want {
		t.Fatalf("paragraphs:\n%q\nwant\n%q", got, want)
	}
}

// Steady dictation seldom pauses 3 s, so a paragraph that has run a minute ends at the next
// sentence end, and the times stay close enough to find a passage by.
func TestALongParagraphEndsAtASentenceAfterAMinute(t *testing.T) {
	segments := []segment{
		{0, 30, "First part of a long run"},
		{30.5, 59, "still going."},
		{59.5, 61, "Past a minute"},
		{61.2, 62, "mid-sentence"},
		{62.1, 70, "ends here."},
		{70.2, 80, "New paragraph."},
	}
	got := paragraphs(segments)
	want := "**[00:00:00]** First part of a long run still going. Past a minute mid-sentence ends here.\n\n" +
		"**[00:01:10]** New paragraph.\n"
	if got != want {
		t.Fatalf("paragraphs:\n%q\nwant\n%q", got, want)
	}
}

func TestFrontMatterQuotesNamesThatYAMLWouldMisread(t *testing.T) {
	r := recording{path: "/r/memo: notes #1.m4a", parts: []string{"x"}}
	got := render(r, reply{Duration: 1, Language: "en", Model: "m"}, nil, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	if !strings.Contains(got, `source: "memo: notes #1.m4a"`) {
		t.Fatalf("front matter:\n%s", got)
	}
}
