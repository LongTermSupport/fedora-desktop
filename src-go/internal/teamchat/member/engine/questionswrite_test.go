package engine

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// questionsKey is a `questions` key holding an index at the head, token tok.
func questionsKey(tok string) *proto.QuestionsState {
	idx := proto.NewQuestionIndex("example.test", 0).State()
	last := evID(900)
	return &proto.QuestionsState{Index: &idx, Token: &tok, Last: &last}
}

// QX5 (e), n3f-D19: a batch that does not hold the questions room leaves the key byte for byte as
// it was; one that does writes it, through the engine's one store writer.
func TestPersistWritesTheQuestionsKeyOnlyWhenTheBatchHoldsIt(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.held(ops)
	key := questionsKey("t1")
	w.persistBatch(team.Batch{Team: ops, NextBatch: "s1", Questions: key})
	before, err := json.Marshal(w.store(ops).Questions)
	check(t, err)
	want, err := json.Marshal(key)
	check(t, err)
	if string(before) != string(want) {
		t.Fatalf("the key written = %s, want %s", before, want)
	}
	w.persistBatch(team.Batch{Team: ops, NextBatch: "s2", Items: []team.Item{humanItem(ops, 1, 10, "main")}})
	after, err := json.Marshal(w.store(ops).Questions)
	check(t, err)
	if string(after) != string(before) {
		t.Fatalf("a batch that did not hold the room changed the key:\n%s\n%s", before, after)
	}
	next := questionsKey("t3")
	w.persistBatch(team.Batch{Team: ops, NextBatch: "t3", Questions: next})
	if got := w.store(ops).Questions; got == nil || *got.Token != "t3" || w.store(ops).Sync.NextBatch != "t3" {
		t.Fatalf("the key after a second write = %+v", got)
	}
}

// QS9 (the engine's half): a demotion's write drops the questions room's pending and handed
// items, the index, the reminder state and the three outbox keys, and a questions post in
// `sending`; main's items and the questions floor stay, and an item awaiting proof stays too.
func TestPersistDropsTheQuestionsRoomForADemotion(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	s := w.newStore(ops)
	unechoed := evID(77)
	handed := "0a1b2c3d"
	s.Pending = []state.Pending{
		{ID: evID(1), Room: proto.PurposeMain, Item: records.KindHuman, SentMS: 10, From: alice(ops)},
		{ID: evID(2), Room: proto.PurposeQuestions, Item: records.KindHuman, SentMS: 20, From: alice(ops)},
		{ID: evID(3), Room: proto.PurposeQuestions, Item: records.KindHuman, SentMS: 30, From: alice(ops), Handed: &handed},
		{ID: evID(4), Room: proto.PurposeQuestions, Item: records.KindHuman, SentMS: 40, From: alice(ops), Handed: &handed},
	}
	s.Awaiting = []state.Awaiting{{Nonce: handed, IDs: []string{evID(4)}, Transcript: state.TranscriptMark{Path: "/t.jsonl", Offset: 1}, AtMS: 50, Written: state.WrittenYes}}
	s.Sync = state.Sync{NextBatch: "s5", RestoreEpoch: 0, FloorMS: map[string]int64{proto.PurposeMain: 9, proto.PurposeQuestions: 19}}
	s.Questions = questionsKey("s5")
	s.RemindedMS, s.RemindLate = 123, []int64{2}
	s.Outbox.QuestionsUnechoed, s.Outbox.QuestionsUncounted, s.Outbox.QuestionsHandover = &unechoed, state.Uncounted{Count: 2, Said: 1}, true
	s.Outbox.Sending = []state.Sending{
		{Txn: "tc" + strings.Repeat("a", 32), Room: proto.PurposeQuestions, Kind: state.SendText, SinceMS: 5, Type: proto.MessageType, Content: json.RawMessage(`{}`)},
		{Txn: "tc" + strings.Repeat("b", 32), Room: proto.PurposeMain, Kind: state.SendText, SinceMS: 5, Type: proto.MessageType, Content: json.RawMessage(`{}`)},
	}
	w.add(ops, encodeStore(t, s))
	w.persistBatch(team.Batch{Team: ops, NextBatch: "s6", DropQuestions: true})
	got := w.store(ops)
	ids := make([]string, 0, len(got.Pending))
	for _, p := range got.Pending {
		ids = append(ids, p.ID)
	}
	if want := []string{evID(1), evID(4)}; !slices.Equal(ids, want) {
		t.Errorf("pending %v, want %v (main's, and the item awaiting proof)", ids, want)
	}
	if got.Questions != nil || got.RemindedMS != 0 || len(got.RemindLate) != 0 {
		t.Errorf("the index or reminder state is left: %+v %d %v", got.Questions, got.RemindedMS, got.RemindLate)
	}
	if got.Outbox.QuestionsUnechoed != nil || got.Outbox.QuestionsUncounted != (state.Uncounted{}) || got.Outbox.QuestionsHandover {
		t.Errorf("the outbox's questions keys are left: %+v", got.Outbox)
	}
	if len(got.Outbox.Sending) != 1 || got.Outbox.Sending[0].Room != proto.PurposeMain {
		t.Errorf("sending %+v, want main's alone", got.Outbox.Sending)
	}
	if got.Sync.FloorMS[proto.PurposeQuestions] == 0 {
		t.Errorf("the questions floor was dropped: %+v", got.Sync.FloorMS)
	}
	var kept bool
	w.local(func() { _, kept = w.e.teams[ops].content[evID(2)] })
	if kept {
		t.Error("a dropped item's content is still in memory")
	}
}
