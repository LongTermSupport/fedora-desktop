package teamsync

import "github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"

// budgeted is whether B 2.7.3's delivery budget counts v: an agent's PEER, PING or NOTICE.
// HUMAN, TIMEOUT, the admin's and carried items are never held back (15b, 15c).
func budgeted(v proto.Verdict) bool {
	switch v.Class {
	case proto.SenderCoordinator, proto.SenderWorker, proto.SenderProbe:
	case proto.SenderNone, proto.SenderAdmin, proto.SenderHuman:
		return false
	}
	return !v.Carried && (v.Kind == proto.KindPeer || v.Kind == proto.KindPing || v.Kind == proto.KindNotice)
}

// admit is the delivery budget applied as v would enter pending: deliver_per_sender items per
// (team, sender) per DeliveryWindowMs. It is when the item may be shown: the budget window's next
// free slot for an item over it, which enters pending held until then and is never dropped (Plan
// 00267 C4). The window lives in memory only, so a link restart resets it.
func (s *sess) admit(j *judged, v proto.Verdict) int64 {
	if !budgeted(v) {
		return j.now
	}
	key := proto.DeliveryKey(s.a.Team, v.Event.Sender)
	return j.budget.Reserve(key, s.roster.Limits.DeliverPerSender, proto.DeliveryWindowMs, j.now)
}
