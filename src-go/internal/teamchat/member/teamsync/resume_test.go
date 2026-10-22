package teamsync

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// ts is the origin time fakehs gave the event id.
func (w *world) ts(id string) int64 {
	w.t.Helper()
	raw, err := w.client(alice).GetEvent(w.t.Context(), w.main, id)
	check(w.t, err)
	v, err := proto.Parse(raw)
	check(w.t, err)
	return originTs(v)
}

// putMarker writes the member's floor marker as an earlier link did.
func (w *world) putMarker(content json.RawMessage) {
	w.t.Helper()
	check(w.t, w.client(w.self).PutRoomAccountData(w.t.Context(), user(w.self), w.main, paths.FloorAccountData, content))
}

// later posts a human's mention of me a second after the last post.
func (w *world) later(body string) string {
	w.t.Helper()
	w.clk.StepWall(1000)
	return w.say(alice, human(body, user(me)))
}

// quiet posts a human message that mentions no one: it moves the batch's high-water and owes
// nothing.
func (w *world) quiet(body string) string {
	w.t.Helper()
	w.clk.StepWall(1000)
	return w.say(alice, human(body))
}

func itemIDs(b team.Batch) []string {
	out := make([]string, 0, len(b.Items))
	for _, it := range b.Items {
		out = append(out, it.Pending.ID)
	}
	return out
}

// SY1 (b): the marker's floor is the bound; what is at or before it is not judged again.
func TestResumesAfterTheMarker(t *testing.T) {
	w := newWorld(t)
	before := w.later("before")
	after := w.later("after")
	w.putMarker(markerContent(w.ts(before), w.roster.RestoreEpoch))
	w.start()
	if got := itemIDs(w.batch()); len(got) != 1 || got[0] != after {
		t.Errorf("items %v, want [%s]; log %s", got, after, w.log.String())
	}
}

// SY1 (b): a marker that does not decode is none, said once; the bound is the member's own join.
func TestAnUndecodableMarkerIsNone(t *testing.T) {
	for _, content := range []string{`{"v":2,"floor_ms":1,"restore_epoch":0}`, `{"v":1}`, `{"v":1,"floor_ms":-1,"restore_epoch":0}`} {
		w := newWorld(t)
		one, two := w.later("one"), w.later("two")
		w.putMarker(json.RawMessage(content))
		w.start()
		if got := itemIDs(w.batch()); len(got) != 2 || got[0] != one || got[1] != two {
			t.Errorf("%s: items %v", content, got)
		}
		if n := w.log.count("does not decode"); n != 1 {
			t.Errorf("%s: said %d times", content, n)
		}
	}
}

// SY1 (b): a held seat's release time bounds the resume too, and marks what came before it
// restored.
func TestTheReleaseTimeMarksRestored(t *testing.T) {
	w := newWorld(t)
	before := w.later("before the release")
	released := w.ts(before)
	after := w.later("after the release")
	w.place(w.tokens[me], &released)
	w.start()
	b := w.batch()
	if got := itemIDs(b); len(got) != 2 || got[0] != before || got[1] != after {
		t.Fatalf("items %v", got)
	}
	if !b.Items[0].Pending.Restored || b.Items[1].Pending.Restored {
		t.Errorf("restored = %v, %v", b.Items[0].Pending.Restored, b.Items[1].Pending.Restored)
	}
}

// SY1 (b): with a marker and a release time, the earlier bound decides what is judged.
func TestTheEarlierBoundDecides(t *testing.T) {
	w := newWorld(t)
	first := w.later("first")
	second := w.later("second")
	third := w.later("third")
	w.putMarker(markerContent(w.ts(second), w.roster.RestoreEpoch))
	released := w.ts(first) - 1
	w.place(w.tokens[me], &released)
	w.start()
	if got := itemIDs(w.batch()); len(got) != 3 || got[0] != first || got[2] != third {
		t.Errorf("items %v", got)
	}
}

// SY1 (b): what came before the member's own latest goodbye (a `left` notice) is restored.
func TestAGoodbyeMarksRestored(t *testing.T) {
	w := newWorld(t)
	before := w.later("before the goodbye")
	w.clk.StepWall(1000)
	out, err := proto.Composer{Server: server, Self: user(me), Token: w.tokens[me], Roster: w.roster}.Notice(proto.Notice{Event: proto.EventLeft, Subject: user(me), To: []string{user(coord)}, Reason: proto.LeftReasons[0]}, "")
	check(t, err)
	_, err = w.client(me).Send(t.Context(), w.main, proto.MessageType, "", out.Content.Compact())
	check(t, err)
	after := w.later("after the goodbye")
	w.start()
	b := w.batch()
	if got := itemIDs(b); len(got) != 2 || got[0] != before || got[1] != after {
		t.Fatalf("items %v", got)
	}
	if !b.Items[0].Pending.Restored || b.Items[1].Pending.Restored {
		t.Errorf("restored = %v, %v", b.Items[0].Pending.Restored, b.Items[1].Pending.Restored)
	}
}

// posts sends n human messages addressed to no one, after one mention of me, and gives the
// mention's id.
func (w *world) posts(n int) string {
	w.t.Helper()
	first := w.later("the oldest")
	for i := range n {
		w.say(alice, human(fmt.Sprintf("chatter %d", i)))
	}
	return first
}

// SY1 (b): a timeline the first sync gives limited is filled backwards to the member's join.
func TestTheFirstSyncIsFilledBack(t *testing.T) {
	w := newWorld(t)
	first := w.posts(105)
	w.start()
	b := w.batch()
	if got := itemIDs(b); len(got) != 1 || got[0] != first || b.Gap {
		t.Errorf("items %v, gap %v", got, b.Gap)
	}
	if w.requests("/messages") == 0 {
		t.Error("no backwards read")
	}
}

// SY1 (b) and SY3: past the page bound the read stops, and the batch says there is a gap.
func TestPastThePageBoundIsAGap(t *testing.T) {
	w := newWorld(t)
	first := w.posts(105)
	tm := w.newTeam()
	tm.pages = pageBound{size: 2, max: 2}
	w.startTeam(tm)
	b := w.batch()
	if !b.Gap || b.Counts.Gaps != 1 {
		t.Errorf("gap %v, counts %+v", b.Gap, b.Counts)
	}
	for _, id := range itemIDs(b) {
		if id == first {
			t.Error("the event past the gap was delivered")
		}
	}
}

// SY3: a limited long-poll answer is filled back to the position it was asked from.
func TestALimitedAnswerIsFilledToSince(t *testing.T) {
	for _, small := range []bool{false, true} {
		w := newWorld(t)
		tm := w.newTeam()
		if small {
			tm.pages = pageBound{size: 2, max: 2}
		}
		// The first long-poll waits until the posts are made, so its answer is limited.
		_, release := w.held(tm)
		first := w.posts(105)
		release()
		b := w.batch()
		got := itemIDs(b)
		switch {
		case small && (!b.Gap || len(got) != 0):
			t.Errorf("small pages: gap %v, items %v", b.Gap, got)
		case !small && (b.Gap || len(got) != 1 || got[0] != first):
			t.Errorf("items %v, gap %v", got, b.Gap)
		}
	}
}
