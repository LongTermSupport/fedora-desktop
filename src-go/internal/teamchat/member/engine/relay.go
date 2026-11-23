package engine

import (
	"fmt"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// queueRelays is the batch's relays queued in `sending` in its write (A 10.6), each once: a
// transaction ID already there is a batch fetched again. It says whether one was queued. A full
// `sending` refuses the whole batch, as a full pending list does, so no relay is lost and the
// batch is fetched again once the sends drain.
func queueRelays(s *state.Store, b team.Batch, now int64) (bool, error) {
	queued := false
	for _, r := range b.Relays {
		if slices.ContainsFunc(s.Outbox.Sending, func(x state.Sending) bool { return x.Txn == r.Txn }) {
			continue
		}
		x := state.Sending{Txn: r.Txn, Room: proto.PurposeMain, Kind: state.SendText, SinceMS: now, Type: proto.MessageType, Content: r.Content}
		if err := s.AddSending(x); err != nil {
			return false, fmt.Errorf("engine: team %s: the relay %s is not queued: %w", b.Team, r.Txn, err)
		}
		queued = true
	}
	return queued, nil
}
