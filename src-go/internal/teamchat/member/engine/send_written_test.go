package engine

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
)

// n3d-D1, A 11.2: a send whose content is within A's 16384 bytes but whose entry, as the store
// writes it, is over a send in flight's 20 KiB is refused at the verb as an over-size send (10
// size), before anything is charged; a text of the same length that the store writes as it is
// still goes.
func TestASendTooLargeAsWrittenIsRefusedAsSize(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.sending(ops)
	before := w.store(ops).Outbox
	d := firstOf(w.say(&ops, strings.Repeat("<", maxText), "repo.2"))
	refusedCode(t, "a text of 4096 `<`", d, exits.CodeSize)
	if !strings.Contains(d.Error.Message, "(size)") {
		t.Errorf("the refusal says %q", d.Error.Message)
	}
	if got := w.store(ops).Outbox; got.Gate.Credit != before.Gate.Credit || len(got.Sending) != 0 || len(got.Gate.Recent) != 0 {
		t.Errorf("a refused send was charged: %+v", got)
	}
	if len(f.notices.sent()) != 0 {
		t.Errorf("a refused send was sent: %v", f.notices.sent())
	}
	sentOf(t, w.say(&ops, strings.Repeat("a", maxText), "repo.2"))
}
