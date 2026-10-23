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
// (team, sender) per DeliveryWindowMs. An item over it never enters pending and is one more in
// the batch's HeldBack for its sender (n3e-D1). The window lives in memory only, so a link
// restart resets it.
func (s *sess) admit(j *judged, v proto.Verdict) bool {
	if !budgeted(v) {
		return true
	}
	key := proto.DeliveryKey(s.a.Team, v.Event.Sender)
	if j.budget.Admit(key, s.roster.Limits.DeliverPerSender, proto.DeliveryWindowMs, j.now) {
		return true
	}
	if j.batch.HeldBack == nil {
		j.batch.HeldBack = map[string]int64{}
	}
	j.batch.HeldBack[v.Event.Sender]++
	return false
}
