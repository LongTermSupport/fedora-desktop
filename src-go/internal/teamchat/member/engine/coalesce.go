package engine

import (
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// The coalesce windows (Plan 00267 Tasks 7.1 and 7.5; B 2.7.3), on the boot clock. A team's
// pending items are held until they are quiet, so a burst costs the session one turn, not one per
// pace slot:
//
//   - agent items: until coalesceQuiet has passed since the newest, at most coalesceCeiling after
//     the oldest. 2 s is the pace a burst was already split by (paceWait), so a burst that
//     arrived within it before now arrives whole; 10 s bounds how late any item can be.
//   - HUMAN items skip that window for their own: until humanSettle has passed since the newest
//     HUMAN item, at most humanCeiling after the oldest. 3 s covers a person sending one thought
//     in two or three short posts; 10 s bounds how late the first can be. What else is pending
//     goes with them.
//   - a halt ping skips both: the team's delivery goes at once, with whatever is pending.
const (
	coalesceQuiet   = 2 * time.Second
	coalesceCeiling = 10 * time.Second
	humanSettle     = 3 * time.Second
	humanCeiling    = 10 * time.Second
)

// window is the hold a delivery waits out: quiet and ceiling for agent items, settle and
// settleCeiling for HUMAN ones. The zero window holds nothing.
type window struct {
	quiet, ceiling, settle, settleCeiling time.Duration
}

// coalesce is the window every engine holds by.
var coalesce = window{quiet: coalesceQuiet, ceiling: coalesceCeiling, settle: humanSettle, settleCeiling: humanCeiling}

// arrival is one deliverable item as the window weighs it: when the link first saw it pending,
// on the boot clock, and whether it is a HUMAN item or a halt ping.
type arrival struct {
	at          time.Duration
	human, halt bool
}

// arrivalOf is p as the window weighs it, first seen at at.
func arrivalOf(p state.Pending, at time.Duration) arrival {
	return arrival{
		at:    at,
		human: p.Item == records.KindHuman,
		halt:  p.Item == records.KindPing && p.Verb != nil && *p.Verb == proto.VerbHalt,
	}
}

// dueAt is when a team's delivery of items may go: 0 (now) for none or a halt ping; else, with
// a HUMAN item, the human settle over the HUMAN items; else the agent window over them all.
//
// Every HUMAN item restarts the settle, whichever human sent it: a delivery goes once each
// human's posts are quiet, which is the same instant as the last HUMAN item's settle, and the
// ceiling from the first HUMAN item still bounds it.
func (w window) dueAt(items []arrival) time.Duration {
	var (
		all, human     span
		quiet, ceiling = w.quiet, w.ceiling
	)
	for _, it := range items {
		if it.halt {
			return 0
		}
		all.add(it.at)
		if it.human {
			human.add(it.at)
		}
	}
	s := all
	if human.set {
		s, quiet, ceiling = human, w.settle, w.settleCeiling
	}
	if !s.set {
		return 0
	}
	return min(s.last+quiet, s.first+ceiling)
}

// span is the first and last of a set of arrival times.
type span struct {
	first, last time.Duration
	set         bool
}

func (s *span) add(at time.Duration) {
	if !s.set {
		*s = span{first: at, last: at, set: true}
		return
	}
	s.first, s.last = min(s.first, at), max(s.last, at)
}

// arrivals is the team's deliverable items as the window weighs them, each at the time the
// link first saw it pending; items no longer deliverable are forgotten. On the state goroutine.
func (d *deliveryState) arrivals(items []state.Pending, now time.Duration) []arrival {
	seen := make(map[string]time.Duration, len(items))
	out := make([]arrival, 0, len(items))
	for _, p := range items {
		at, ok := d.seen[p.ID]
		if !ok {
			at = now
		}
		seen[p.ID] = at
		out = append(out, arrivalOf(p, at))
	}
	d.seen = seen
	return out
}
