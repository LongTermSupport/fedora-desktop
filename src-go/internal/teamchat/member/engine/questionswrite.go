package engine

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// questionsDrop is what a write did of the questions room beside its key: what a demotion took
// out of the store, for the log (the ids of the questions items and the transaction ids of the
// questions posts in `sending`, never a body), and whether it queued a relay (A 10.6).
type questionsDrop struct {
	items, sends []string
	relayed      bool
}

// afterQuestions is a written batch's questions effects in memory: the dropped items forgotten
// and logged, and the sender woken for a queued relay.
func (e *Engine) afterQuestions(h *held, t paths.TeamID, d questionsDrop) {
	for _, id := range d.items {
		delete(h.content, id)
		e.logf("team %s: the questions item %s is dropped with the questions room", t, id)
	}
	for _, txn := range d.sends {
		e.logf("team %s: the questions post sent as %s is dropped with the questions room; it may have landed", t, txn)
	}
	if d.relayed {
		h.pokeSender()
	}
}

// applyQuestions is the batch's writes of the questions room's keys in s (QX5, QS9, n3f-D19).
// The engine's one store writer is the only place the `questions` key is written: a batch with
// Questions sets it, a batch with DropQuestions takes the room out, and any other batch leaves
// it as it was.
func applyQuestions(s *state.Store, b team.Batch) (questionsDrop, error) {
	var out questionsDrop
	if b.Questions != nil {
		data, err := json.Marshal(b.Questions)
		if err != nil {
			return out, fmt.Errorf("engine: team %s: copy the questions key: %w", b.Team, err)
		}
		var key proto.QuestionsState
		if err := json.Unmarshal(data, &key); err != nil {
			return out, fmt.Errorf("engine: team %s: copy the questions key: %w", b.Team, err)
		}
		s.Questions = &key
		s.Outbox.Folded(b.QuestionsFolded, int64(len(b.QuestionsUncounted)))
	}
	if b.Handover {
		s.Outbox.QuestionsHandover = true
	}
	if !b.DropQuestions {
		return out, nil
	}
	awaiting := awaitingIDs(s)
	s.Pending = slices.DeleteFunc(s.Pending, func(p state.Pending) bool {
		drop := p.Room == proto.PurposeQuestions && !awaiting[p.ID]
		if drop {
			out.items = append(out.items, p.ID)
		}
		return drop
	})
	s.Outbox.Sending = slices.DeleteFunc(s.Outbox.Sending, func(x state.Sending) bool {
		drop := x.Room == proto.PurposeQuestions
		if drop {
			out.sends = append(out.sends, x.Txn)
		}
		return drop
	})
	s.Questions, s.RemindedMS, s.RemindLate = nil, 0, []int64{}
	s.Outbox.QuestionsUnechoed, s.Outbox.QuestionsUncounted, s.Outbox.QuestionsHandover = nil, state.Uncounted{}, false
	return out, nil
}
