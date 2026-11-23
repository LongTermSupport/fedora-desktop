package engine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// A 10.6: the relays a batch owes are queued in `sending` in its one write, each under its
// derived transaction ID, and a batch fetched again queues none twice.
func TestABatchsRelaysAreQueuedOnce(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.add(ops, encodeStore(t, w.newStore(ops)))
	post := "$" + strings.Repeat("h", 43)
	txn := proto.RelayTxn(post, "@w:example.org")
	content := json.RawMessage(`{"msgtype":"m.notice","body":"x"}`)
	b := team.Batch{Team: ops, NextBatch: "s1", Relays: []team.Relay{{Txn: txn, Content: content}}}
	w.persistBatch(b)
	b.NextBatch = "s2"
	w.persistBatch(b)
	var queued []state.Sending
	for _, x := range w.store(ops).Outbox.Sending {
		if x.Txn == txn {
			queued = append(queued, x)
		}
	}
	if len(queued) != 1 {
		t.Fatalf("the relay queued %d times, want once", len(queued))
	}
	if x := queued[0]; x.Room != proto.PurposeMain || x.Kind != state.SendText || x.Type != proto.MessageType || string(x.Content) != string(content) {
		t.Errorf("queued %+v", x)
	}
}
