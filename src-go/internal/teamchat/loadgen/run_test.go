package loadgen

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/traffic"
)

// The replay's round trip: the probe's counts of what a replay made are the counts replayed.
func TestReplayRoundTripsThroughTheProbe(t *testing.T) {
	c := newStepClock()
	team := labTeam(t, c, 3)
	g := newGen(t, team, c, Options{})
	doc := traffic.Counts{Window: traffic.Window{SinceMS: startMS, UntilMS: startMS + 2*traffic.HourMS}}
	rows := []traffic.Row{
		msg(proto.PurposeMain, 0, proto.KeyPing, "", 3), msg(proto.PurposeMain, 0, proto.KeyPing, traffic.RelThread, 2),
		msg(proto.PurposeMain, 0, proto.KeyText, "", 2), msg(proto.PurposeMain, 1, proto.KeyText, traffic.RelReply, 1),
		msg(proto.PurposeMain, 1, proto.KeyNotice, "", 2), msg(proto.PurposeMain, 1, "", "", 2),
		msg(proto.PurposeReports, 0, proto.KeyReport, "", 2), msg(proto.PurposeReports, 1, proto.KeyReceipt, "", 1),
		msg(proto.PurposeQuestions, 0, proto.KeyQuestion, "", 1), msg(proto.PurposeQuestions, 1, "", traffic.RelThread, 2),
		msg(proto.PurposeQuestions, 1, "", traffic.RelEdit, 1), msg(proto.PurposeQuestions, 1, proto.KeyThread, traffic.RelThread, 3),
	}
	want := map[traffic.Key]int64{}
	for _, r := range rows {
		r.HourMS = startMS + (r.HourMS - hourAt(0))
		doc.Rows = append(doc.Rows, r)
		want[r.Key] = r.Count
	}
	s, err := Replay(doc, 1, false, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if err := Run(t.Context(), g, s, c); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := map[traffic.Key]int64{}
	for _, r := range probe(t, team, doc.Window).Rows {
		if r.Type == proto.MessageType {
			got[r.Key] = r.Count
		}
	}
	if !maps.Equal(got, want) {
		t.Fatalf("the probe counted\n%v\nwant\n%v", got, want)
	}
	if tally := g.Tally(); len(tally.Extra) != 0 || tally.Made[KindHumanMain] != 2 || tally.Made[KindRead] != 2 {
		t.Fatalf("tally %+v", tally)
	}
}

func TestRunWaitsForEachStep(t *testing.T) {
	c := newStepClock()
	team := labTeam(t, c, 2)
	g := newGen(t, team, c, Options{})
	s, err := Rates(map[string]int{PatternReports: 6}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := Run(t.Context(), g, s, c); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if c.slept != 50*time.Minute || g.Tally().Units[PatternReports] != 6 {
		t.Fatalf("slept %v, tally %+v", c.slept, g.Tally())
	}
}

func TestRunStopsAtTheFirstFailure(t *testing.T) {
	c := newStepClock()
	team := labTeam(t, c, 2)
	g := newGen(t, team, c, Options{})
	if err := Run(t.Context(), g, &listed{steps: []Step{{Kind: KindPing}, {Kind: "nothing"}, {Kind: KindPing}}}, c); err == nil {
		t.Fatal("Run passed a step it cannot make")
	}
	if g.Tally().Made[KindPing] != 1 {
		t.Fatalf("a step after the failure ran: %+v", g.Tally())
	}
}

func TestRunEndsWithItsContext(t *testing.T) {
	c := newStepClock()
	team := labTeam(t, c, 2)
	g := newGen(t, team, c, Options{})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := Run(ctx, g, &listed{steps: []Step{{AtMS: 1000, Kind: KindPing}}}, c); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run after the interrupt: %v", err)
	}
}
