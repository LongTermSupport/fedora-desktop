package keeper

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// RC1: a desired file that gains a seat makes the keeper take it and add it to the link.
func TestHotAdd(t *testing.T) {
	w := newWorld(t, claudeLaunch...)
	alpha := w.seat("alpha")
	w.writeDesired(desiredJSON(1, state.InboundAccept))
	l, c, res := w.running(0)
	w.writeDesired(desiredJSON(2, state.InboundAccept, "alpha/1"))
	w.changed()
	if d := l.expect(t, wire.KindDesired); d.Desired.Generation != 2 || d.Desired.State != state.DesiredApplied {
		t.Fatalf("desired %+v", d.Desired)
	}
	if a := l.expect(t, wire.KindAdd); a.Add.Team.String() != "alpha" || a.Add.Seat != "1" {
		t.Fatalf("add %+v", a.Add)
	}
	if !w.held(alpha) {
		t.Fatal("the added seat is not held")
	}
	c.code(0)
	l.expect(t, wire.KindEnd)
	l.code(0)
	exitOf(t, res, term.ExitOK)
}
