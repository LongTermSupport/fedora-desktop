package loadgen

import (
	"bytes"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/traffic"
)

func steps(s Schedule) []Step {
	var out []Step
	for {
		st, ok := s.Next()
		if !ok {
			return out
		}
		out = append(out, st)
	}
}

func TestRatesSpreadEachPatternsUnitsEvenly(t *testing.T) {
	s, err := Rates(map[string]int{PatternReports: 4, PatternPings: 2}, time.Hour)
	if err != nil {
		t.Fatalf("Rates: %v", err)
	}
	want := []Step{
		{AtMS: 0, Pattern: PatternPings},
		{AtMS: 0, Pattern: PatternReports},
		{AtMS: 900_000, Pattern: PatternReports},
		{AtMS: 1_800_000, Pattern: PatternPings},
		{AtMS: 1_800_000, Pattern: PatternReports},
		{AtMS: 2_700_000, Pattern: PatternReports},
	}
	if got := steps(s); !slices.Equal(got, want) {
		t.Fatalf("steps\n%v\nwant\n%v", got, want)
	}
}

func TestRatesOffIsLeftOut(t *testing.T) {
	s, err := Rates(map[string]int{PatternReports: 0, PatternBig: MaxRate}, time.Second)
	if err != nil {
		t.Fatalf("Rates: %v", err)
	}
	got := steps(s)
	if len(got) != 10 || got[9].AtMS != 900 || got[0].Pattern != PatternBig {
		t.Fatalf("the most a second at the top rate: %v", got)
	}
}

func TestRatesRefuseWhatTheyCannotRun(t *testing.T) {
	cases := []struct {
		rates  map[string]int
		length time.Duration
		says   string
	}{
		{map[string]int{}, time.Hour, "no pattern"},
		{map[string]int{PatternPings: 0}, time.Hour, "no pattern"},
		{map[string]int{"floods": 1}, time.Hour, `no pattern "floods"`},
		{map[string]int{PatternPings: -1}, time.Hour, "--pings"},
		{map[string]int{PatternPings: MaxRate + 1}, time.Hour, "--pings"},
		{map[string]int{PatternPings: 1}, time.Second - 1, "--for"},
		{map[string]int{PatternPings: 1}, MaxLength + 1, "--for"},
	}
	for _, c := range cases {
		if _, err := Rates(c.rates, c.length); err == nil || !strings.Contains(err.Error(), c.says) {
			t.Errorf("Rates(%v, %v) = %v, want refused saying %q", c.rates, c.length, err, c.says)
		}
	}
}

// hourAt is the hour h of the replayed window, from 2026-11-25T16:00Z.
func hourAt(h int64) int64 { return 1_795_622_400_000 + h*traffic.HourMS }

func counts(rows ...traffic.Row) traffic.Counts {
	c := traffic.Counts{Window: traffic.Window{SinceMS: hourAt(0) + 60_000, UntilMS: hourAt(2)}}
	events := map[string]int64{}
	for _, r := range rows {
		events[r.Room] += r.Count
	}
	for _, room := range []string{proto.PurposeMain, proto.PurposeReports, proto.PurposeQuestions} {
		c.Rooms = append(c.Rooms, traffic.RoomRead{Room: room, Events: events[room], Pages: 1})
	}
	c.Rows = rows
	return c
}

func row(room string, hour int64, typ, typed, rel string, n int64) traffic.Row {
	k := traffic.Key{Room: room, HourMS: hourAt(hour), Type: typ, Typed: typed, Rel: rel}
	if typ == "m.room.member" {
		k.Membership = "join"
	}
	return traffic.Row{Key: k, Count: n}
}

func msg(room string, hour int64, typed, rel string, n int64) traffic.Row {
	return row(room, hour, proto.MessageType, typed, rel, n)
}

func TestReplayMakesEachRowsKind(t *testing.T) {
	cases := []struct {
		row  traffic.Row
		kind string
	}{
		{msg(proto.PurposeMain, 0, proto.KeyPing, "", 1), KindPing},
		{msg(proto.PurposeMain, 0, proto.KeyPing, traffic.RelThread, 1), KindAnswer},
		{msg(proto.PurposeMain, 0, proto.KeyText, "", 1), KindText},
		{msg(proto.PurposeMain, 0, proto.KeyText, traffic.RelReply, 1), KindReply},
		{msg(proto.PurposeMain, 0, proto.KeyNotice, "", 1), KindNotice},
		{msg(proto.PurposeMain, 0, "", "", 1), KindHumanMain},
		{msg(proto.PurposeReports, 0, proto.KeyReport, "", 1), KindReport},
		{msg(proto.PurposeReports, 0, proto.KeyReceipt, "", 1), KindReceiptEvent},
		{msg(proto.PurposeQuestions, 0, proto.KeyQuestion, "", 1), KindQuestion},
		{msg(proto.PurposeQuestions, 0, "", traffic.RelEdit, 1), KindRetitle},
		{msg(proto.PurposeQuestions, 0, proto.KeyThread, traffic.RelThread, 1), KindThread},
		{msg(proto.PurposeQuestions, 0, "", traffic.RelThread, 1), KindHumanThread},
	}
	for _, c := range cases {
		s, err := Replay(counts(c.row), 1, false, &bytes.Buffer{})
		if err != nil {
			t.Fatalf("Replay of %+v: %v", c.row.Key, err)
		}
		if got := steps(s); len(got) != 1 || got[0].Kind != c.kind {
			t.Errorf("row %+v made %v, want %s", c.row.Key, got, c.kind)
		}
	}
}

func TestReplaySpreadsEachHourInOrder(t *testing.T) {
	c := counts(
		msg(proto.PurposeMain, 0, proto.KeyPing, "", 2),
		msg(proto.PurposeMain, 1, proto.KeyText, "", 1),
		msg(proto.PurposeReports, 0, proto.KeyReport, "", 4),
	)
	s, err := Replay(c, 1, false, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	want := []Step{
		{AtMS: 0, Kind: KindPing},
		{AtMS: 0, Kind: KindReport},
		{AtMS: 900_000, Kind: KindReport},
		{AtMS: 1_800_000, Kind: KindPing},
		{AtMS: 1_800_000, Kind: KindReport},
		{AtMS: 2_700_000, Kind: KindReport},
		{AtMS: 3_600_000, Kind: KindText},
	}
	if got := steps(s); !slices.Equal(got, want) {
		t.Fatalf("steps\n%v\nwant\n%v", got, want)
	}
	fast, err := Replay(c, 60, false, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if got := steps(fast); got[6].AtMS != 60_000 || got[2].AtMS != 15_000 {
		t.Fatalf("at speed 60: %v", got)
	}
}

func TestReplayRefusesRowsItCannotMake(t *testing.T) {
	c := counts(
		msg(proto.PurposeMain, 0, proto.KeyPing, "", 1),
		row(proto.PurposeMain, 0, "m.room.member", "", "", 2),
		msg(proto.PurposeMain, 0, proto.KeyText, traffic.RelEdit, 3),
		row(proto.PurposeQuestions, 1, "m.reaction", "", "", 1),
	)
	_, err := Replay(c, 1, false, &bytes.Buffer{})
	if err == nil {
		t.Fatal("Replay made rows it cannot")
	}
	for _, want := range []string{"3 rows", "m.room.member", "teamchat.text edit", "m.reaction", "--skip-unmade"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q does not name %q", err, want)
		}
	}
	var warn bytes.Buffer
	s, err := Replay(c, 1, true, &warn)
	if err != nil {
		t.Fatalf("Replay --skip-unmade: %v", err)
	}
	if got := steps(s); len(got) != 1 || got[0].Kind != KindPing {
		t.Fatalf("with the rows left out: %v", got)
	}
	if lines := strings.Count(warn.String(), "left out"); lines != 3 {
		t.Fatalf("left-out lines %d, want 3:\n%s", lines, warn.String())
	}
}

func TestReplayRefusesBadSettings(t *testing.T) {
	c := counts(msg(proto.PurposeMain, 0, proto.KeyPing, "", 1))
	for _, speed := range []int{0, MaxSpeed + 1} {
		if _, err := Replay(c, speed, false, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "--speed") {
			t.Errorf("speed %d: %v", speed, err)
		}
	}
	if _, err := Replay(counts(), 1, false, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "nothing to replay") {
		t.Errorf("an empty replay: %v", err)
	}
	big := counts(msg(proto.PurposeMain, 0, proto.KeyPing, "", MaxReplayEvents+1))
	if _, err := Replay(big, 1, false, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "more than") {
		t.Errorf("a replay past the bound: %v", err)
	}
}
