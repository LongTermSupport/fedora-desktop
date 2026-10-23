//go:build busfailpoints

package link

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/failpoint"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/lock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

func eventID(n int) string { return fmt.Sprintf("$%043d", n) }

// P11: a link killed by SIGKILL after a hand-out's done reached the client, before its ack,
// loses no item: the store still owes both, and the next link hands them out again.
func TestKilledWhileHanded(t *testing.T) {
	w, linkEnd := newKeeperWorld(t)
	a := tid(t, "alpha")
	from := "@alice:" + paths.ServerName(a)
	l := w.seatHolding(a, []state.Pending{
		{ID: eventID(1), Room: proto.PurposeMain, Item: records.KindHuman, SentMS: startWall, From: from},
		{ID: eventID(2), Room: proto.PurposeMain, Item: records.KindHuman, SentMS: startWall + 1, From: from},
	})
	w.startProcess(linkEnd, string(failpoint.LinkWhileHanded))
	w.launch(1)
	w.addHeld(a, l)
	w.next(wire.KindReady)
	first := w.request(wire.OpRecv, boot()+5*time.Second)
	if !strings.Contains(first, eventID(1)) || !strings.Contains(first, `"ack"`) {
		t.Fatalf("recv %q", first)
	}
	// The kill closes the link's end of the control socket.
	for g := range w.in {
		if g.err != nil {
			break
		}
	}
	data, err := w.root.Read(w.layout.Seat(a).Store(), 1<<20)
	check(t, err)
	var st state.Store
	check(t, json.Unmarshal(data.Data, &st))
	if len(st.Pending) != 2 {
		t.Fatalf("after the kill the store owes %+v", st.Pending)
	}

	done := released(w.startProcess(w.connect(), ""))
	w.launch(1)
	w.addHeld(a, l)
	w.next(wire.KindReady)
	if again := w.request(wire.OpRecv, boot()+5*time.Second); !strings.Contains(again, eventID(1)) || !strings.Contains(again, eventID(2)) {
		t.Fatalf("the restarted link's recv %q", again)
	}
	w.send(wire.Msg{Kind: wire.KindEnd, End: &wire.End{How: wire.EndStop}}, nil)
	w.await(done)
}

// addHeld is id's add with a copy of l, a seat the keeper already holds.
func (w *linkWorld) addHeld(id paths.TeamID, l *lock.SeatLock) {
	w.t.Helper()
	fd, err := l.Dup()
	check(w.t, err)
	w.send(addOf(id), fd)
	check(w.t, fd.Close())
}
