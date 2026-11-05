package engine

import (
	"cmp"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// answerNote is one answer to an own ping as Persist read it, kept in memory for status's
// outbox records (n3d-D8): the addressee's index in the ping's to, its verb and event, and
// whether it came after the deadline, which does not count (A 8.2).
type answerNote struct {
	index    int
	verb, id string
	late     bool
}

// findPing is s's outbox ping id, nil when the outbox holds none.
func findPing(s *state.Store, id string) *state.Ping {
	for i := range s.Outbox.Pings {
		if s.Outbox.Pings[i].ID == id {
			return &s.Outbox.Pings[i]
		}
	}
	return nil
}

// addIndex is xs, ascending, with i in it once.
func addIndex(xs []int, i int) []int {
	at, found := slices.BinarySearch(xs, i)
	if found {
		return xs
	}
	return slices.Insert(xs, at, i)
}

// applyOutbox is a batch's echoes and answers in a copy of its store (SY4, n3d-D8, n3d-D9):
// an echo moves its ping's deadline to the server's time; an answer from an addressee counts
// when its origin is at or before the ping's due_ms, however late it is read. It is the answers
// to keep in memory once the copy is written, by ping, and the pings answered that the outbox
// does not hold.
func applyOutbox(s *state.Store, b team.Batch) (map[string][]answerNote, []string) {
	for _, ec := range b.Echoes {
		if p := findPing(s, ec.ID); p != nil {
			timeout := p.DueMS - p.SentMS
			p.SentMS, p.DueMS = ec.OriginMS, ec.OriginMS+timeout
		}
	}
	notes := map[string][]answerNote{}
	var unknown []string
	for _, a := range b.Answered {
		p := findPing(s, a.Ping)
		if p == nil {
			unknown = append(unknown, a.Ping)
			continue
		}
		// proto makes no Answer from a sender the ping did not address; this is the engine's own
		// guard.
		i := slices.Index(p.To, a.From)
		if i < 0 {
			continue
		}
		late := a.OriginMS > p.DueMS
		if !late {
			p.Answered = addIndex(p.Answered, i)
		}
		notes[a.Ping] = append(notes[a.Ping], answerNote{index: i, verb: a.Verb, id: a.ID, late: late})
	}
	return notes, unknown
}

// answeredLines are the `answered:` lines a batch's answers owe the next delivery (A 8.4,
// n3d-D17), read from s once applyOutbox has written them: every ack, and an on-time answer to
// an addressee whose TIMEOUT was already made, marked so. From is the sender's user id, named
// when a delivery takes it.
func answeredLines(s *state.Store, b team.Batch) []proto.Answered {
	var out []proto.Answered
	for _, a := range b.Answered {
		p := findPing(s, a.Ping)
		if p == nil {
			continue
		}
		i := slices.Index(p.To, a.From)
		if i < 0 {
			continue
		}
		after := a.OriginMS <= p.DueMS && slices.Contains(p.Reported, i)
		if a.Verb == proto.VerbAck || after {
			out = append(out, proto.Answered{From: a.From, Answer: a.Verb, Verb: p.Verb, PingID: a.Ping, AfterTimeout: after})
		}
	}
	return out
}

// settled is whether every addressee of p has answered or been reported.
func settled(p state.Ping) bool {
	for i := range p.To {
		if !slices.Contains(p.Answered, i) && !slices.Contains(p.Reported, i) {
			return false
		}
	}
	return true
}

// expired is whether p is past the outbox's window at now: settled, and its deadline plus its
// own timeout reached ("answered in the last ack_timeout_s", B 2.2.2; n3d-D6).
func expired(p state.Ping, now int64) bool {
	return settled(p) && now >= p.DueMS+(p.DueMS-p.SentMS)
}

// timeoutKeyed is the pings a TIMEOUT item still pending or awaiting is keyed on, which its
// record reads for the ping's verb and ref.
func timeoutKeyed(s *state.Store) map[string]bool {
	keyed := map[string]bool{}
	mark := func(id string) {
		if event, _, ok := strings.Cut(id, "~"); ok {
			keyed[event] = true
		}
	}
	for _, p := range s.Pending {
		mark(p.ID)
	}
	for _, a := range s.Awaiting {
		for _, id := range a.IDs {
			mark(id)
		}
	}
	return keyed
}

// pruneOutbox drops every expired ping no TIMEOUT item still names, in a copy of the store at
// one of its outbox writes; it is the pings dropped.
func pruneOutbox(s *state.Store, now int64) []string {
	keyed := timeoutKeyed(s)
	var pruned []string
	s.Outbox.Pings = slices.DeleteFunc(s.Outbox.Pings, func(p state.Ping) bool {
		if expired(p, now) && !keyed[p.ID] {
			pruned = append(pruned, p.ID)
			return true
		}
		return false
	})
	return pruned
}

// note keeps a written batch's answers in memory, and forgets the pings pruned.
func (h *held) note(notes map[string][]answerNote, pruned []string) {
	for id, ns := range notes {
		h.answers[id] = append(h.answers[id], ns...)
	}
	h.forget(pruned)
}

func (h *held) forget(pruned []string) {
	for _, id := range pruned {
		delete(h.answers, id)
	}
}

// shownPings are the outbox pings status shows at now, oldest send first, with their answers
// in memory: every ping not yet due to leave the outbox (LN5).
func shownPings(h *held, now int64) ([]state.Ping, map[string][]answerNote) {
	if h.store == nil {
		return nil, nil
	}
	keyed := timeoutKeyed(h.store)
	var pings []state.Ping
	answers := map[string][]answerNote{}
	for _, p := range h.store.Outbox.Pings {
		if expired(p, now) && !keyed[p.ID] {
			continue
		}
		pings = append(pings, p)
		answers[p.ID] = slices.Clone(h.answers[p.ID])
	}
	slices.SortStableFunc(pings, func(a, b state.Ping) int { return cmp.Compare(a.SentMS, b.SentMS) })
	return pings, answers
}

// outboxRecords are a team's outbox records (B 2.2.2): names as short as n gives, one answer
// per addressee answered on time, its verb and id from memory or null when the answer was
// recorded before this link started (n3d-D8), then each late answer this link read.
func outboxRecords(t paths.TeamID, pings []state.Ping, answers map[string][]answerNote, n namer) []records.Record {
	out := make([]records.Record, 0, len(pings))
	for _, p := range pings {
		r := records.Outbox{
			Team: t, ID: p.ID, Verb: p.Verb, Ref: p.Ref, To: names(n, p.To, nil), DueMS: p.DueMS,
			Answers: records.List[records.Answer]{}, TimedOut: names(n, p.To, p.Reported),
		}
		notes := answers[p.ID]
		for _, i := range p.Answered {
			a := records.Answer{From: n.name(p.To[i])}
			if k := slices.IndexFunc(notes, func(x answerNote) bool { return x.index == i && !x.late }); k >= 0 {
				a.Verb, a.ID = new(notes[k].verb), new(notes[k].id)
			}
			r.Answers = append(r.Answers, a)
		}
		for _, x := range notes {
			if x.late {
				r.Answers = append(r.Answers, records.Answer{From: n.name(p.To[x.index]), Verb: new(x.verb), ID: new(x.id), Late: true})
			}
		}
		out = append(out, r)
	}
	return out
}

// names are the short names of ids, or of only the ids at the given indexes when some are given.
func names(n namer, ids []string, only []int) records.List[string] {
	out := records.List[string]{}
	for i, id := range ids {
		if only == nil || slices.Contains(only, i) {
			out = append(out, n.name(id))
		}
	}
	return out
}
