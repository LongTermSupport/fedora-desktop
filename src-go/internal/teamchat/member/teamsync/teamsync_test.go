package teamsync

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// The clock is the matrix client's timers, and fakehs holds a sync on the same clock.
var (
	_ matrix.Timers = clock.Clock(nil)
	_ team.Team     = (*Team)(nil)
	_               = fakehs.Options{Hold: (*clock.Fake)(nil).Hold}
)

// SY1 (b) with no store and no marker: the first answer is persisted fresh: false from the
// member's own join, then the long-poll from its next_batch delivers a human's mention.
func TestDeliversAHumanMention(t *testing.T) {
	w := newWorld(t)
	r := w.start()
	first := w.batch()
	if first.Fresh || first.NextBatch == "" || len(first.Items) != 0 || first.RestoreEpoch != w.roster.RestoreEpoch {
		t.Fatalf("first batch = %+v", first)
	}
	if since := r.poll(t); since != first.NextBatch {
		t.Fatalf("long-poll since %q, want %q", since, first.NextBatch)
	}
	id := w.say(alice, human("hello", user(me)))
	b := w.batch()
	if len(b.Items) != 1 {
		t.Fatalf("batch = %+v", b)
	}
	p := b.Items[0].Pending
	if p.ID != id || p.Room != proto.PurposeMain || p.Item != records.KindHuman || p.From != user(alice) || p.Restored || p.SentMS == 0 {
		t.Errorf("pending = %+v", p)
	}
	if b.Floors[proto.PurposeMain] != p.SentMS || b.Fresh {
		t.Errorf("floors = %v, fresh %v", b.Floors, b.Fresh)
	}
	if h := w.eng.lastHealth(); h.Role != proto.RoleWorker || h.Down || h.Stopped != "" || h.LastSyncOKMS == 0 {
		t.Errorf("health = %+v", h)
	}
	if got := r.tm.Notices().Anchors(); got.MainRoom != w.main || got.Self != user(me) || got.Team != teamName {
		t.Errorf("anchors = %+v", got)
	}
}
