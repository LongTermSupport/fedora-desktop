package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// The tests of SPEC-n3f G2: the seven keys the questions room adds to the seat store (12.6).

// questionsKey is a small `questions` key that holds each part the store keeps: an open question
// with a reminder and an unanswered thread post, a number held by a redacted event, a post, an
// agent's thread post and an unattached human post, an index at the head.
func questionsKey() *proto.QuestionsState {
	st := proto.IndexState{
		Highest: 2,
		Questions: []proto.IndexedQuestion{
			{
				N: 1, EventID: eventID(21), Latest: eventID(22), Headline: "Which licence?", Hash: strings.Repeat("ab", 32),
				AskedBy: []string{agentID}, Re: []string{}, AskedMs: 1_760_000_000_000, RemindAfterS: 900,
				Attached: 1, Unanswered: []string{eventID(23)},
			},
			{N: 2, Closed: true, AskedBy: []string{}, Re: []string{}, Unanswered: []string{}},
		},
		Posts:      []proto.PostEntry{{ID: eventID(23), Q: 1, T: 1}},
		AgentPosts: []proto.AgentPost{{ID: eventID(22), Q: 1}},
		Unattached: []proto.UnattachedPost{{ID: eventID(24), From: aliceID, SentMs: 1_760_000_001_000}},
	}
	return &proto.QuestionsState{Index: &st, Token: new("s72594_4483_1934_0_1"), Last: new(eventID(24))}
}

// questionsStore is the sample store with each of the seven keys set to a value that is not the
// fresh store's.
func questionsStore(t *testing.T) Store {
	t.Helper()
	s := sampleStore(t)
	s.Outbox.Reports = []int64{1_760_000_000_000, 1_760_000_000_500}
	s.Outbox.QuestionsUnechoed = new(eventID(25))
	s.Outbox.QuestionsUncounted = Uncounted{Count: 2, Said: 1}
	s.Outbox.QuestionsHandover = true
	s.Questions = questionsKey()
	s.RemindedMS = 1_760_000_900_000
	s.RemindLate = []int64{1, 7}
	return s
}

func decodeInvalid(t *testing.T, data []byte) *InvalidError {
	t.Helper()
	_, err := DecodeStore(data)
	inv, ok := errors.AsType[*InvalidError](err)
	if !ok {
		t.Fatalf("DecodeStore = %v, want an *InvalidError", err)
	}
	return inv
}

// A fresh store writes all seven keys, each in its place (12.6): in outbox after gate, and at the
// top level after outbox and before run.
func TestFreshStoreWritesTheSevenKeys(t *testing.T) {
	s := NewStore(teamID(t), agentID, StoreRun{ID: runID(t), StartedMS: 1}, Gate{})
	data, err := s.encode()
	check(t, err)
	text := string(data)
	ordered := []string{
		`"gate":{`, `"reports":[]`, `"questions_unechoed":null`, `"questions_uncounted":{"count":0,"said":0}`,
		`"questions_handover":false`, `"questions":null`, `"reminded_ms":0`, `"remind_late":[]`, `"run":{`,
	}
	at := -1
	for _, want := range ordered {
		i := strings.Index(text, want)
		if i < 0 {
			t.Fatalf("a fresh store has no %s:\n%s", want, text)
		}
		if i < at {
			t.Errorf("%s is before the key that precedes it in 12.6", want)
		}
		at = i
	}
	if !strings.Contains(text, `"questions_handover":false},"questions":null,"reminded_ms":0,"remind_late":[],"run":{`) {
		t.Errorf("questions is not the first key after outbox:\n%s", text)
	}
	back, err := DecodeStore(data)
	check(t, err)
	if back.Questions != nil || back.Outbox.Reports == nil || back.RemindLate == nil || back.Outbox.QuestionsUncounted != (Uncounted{}) {
		t.Fatalf("a fresh store read back as %+v", back)
	}
}

func TestStoreHoldsTheSevenKeysAndRoundTripsThem(t *testing.T) {
	s := questionsStore(t)
	data, err := s.encode()
	check(t, err)
	back, err := DecodeStore(data)
	check(t, err)
	again, err := back.encode()
	check(t, err)
	if string(again) != string(data) {
		t.Fatalf("the round trip changed the store:\n%s\n%s", data, again)
	}
	for name, pair := range map[string][2]any{
		"reports":             {back.Outbox.Reports, s.Outbox.Reports},
		"questions_unechoed":  {*back.Outbox.QuestionsUnechoed, *s.Outbox.QuestionsUnechoed},
		"questions_uncounted": {back.Outbox.QuestionsUncounted, Uncounted{Count: 2, Said: 1}},
		"questions_handover":  {back.Outbox.QuestionsHandover, true},
		"questions":           {back.Questions, s.Questions},
		"reminded_ms":         {back.RemindedMS, int64(1_760_000_900_000)},
		"remind_late":         {back.RemindLate, []int64{1, 7}},
	} {
		if !reflect.DeepEqual(pair[0], pair[1]) {
			t.Errorf("%s read back as %+v, want %+v", name, pair[0], pair[1])
		}
	}
}

// The key holds a build owed and a build under way as the codec does (QX5).
func TestStoreHoldsEachShapeOfTheQuestionsKey(t *testing.T) {
	owed := &proto.QuestionsState{Build: &proto.BuildRecord{After: new(eventID(30)), LateFrom: new(int64(3))}}
	under := questionsKey()
	under.Build = &proto.BuildRecord{Start: new(eventID(31)), After: nil, LateFrom: nil}
	for name, key := range map[string]*proto.QuestionsState{"none": nil, "head": questionsKey(), "owed": owed, "under way": under} {
		s := questionsStore(t)
		s.Questions = key
		data, err := s.encode()
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		back, err := DecodeStore(data)
		check(t, err)
		if (back.Questions == nil) != (key == nil) {
			t.Errorf("%s: read back as %+v", name, back.Questions)
		}
	}
}

// without is data with the member at path removed, the rest of its value as it was.
func without(t *testing.T, data []byte, path ...string) []byte {
	t.Helper()
	var doc map[string]json.RawMessage
	check(t, json.Unmarshal(data, &doc))
	if len(path) == 1 {
		if _, ok := doc[path[0]]; !ok {
			t.Fatalf("no key %s to remove", path[0])
		}
		delete(doc, path[0])
	} else {
		doc[path[0]] = without(t, doc[path[0]], path[1:]...)
	}
	out, err := json.Marshal(doc)
	check(t, err)
	return out
}

// Every one of the seven keys is always present in an encoded store, and the decoder refuses a
// store without one (MS1): a store written before the keys has none, and Version stays 1 because
// no store exists outside tests (n3d), so it is refused, never read with the key defaulted.
func TestEveryKeyOfTheQuestionsRoomIsRequired(t *testing.T) {
	full := questionsStore(t)
	data, err := full.encode()
	check(t, err)
	keys := [][]string{
		{"outbox", "reports"},
		{"outbox", "questions_unechoed"},
		{"outbox", "questions_uncounted"},
		{"outbox", "questions_handover"},
		{"questions"},
		{"reminded_ms"},
		{"remind_late"},
	}
	old := data
	for _, path := range keys {
		inv := decodeInvalid(t, without(t, data, path...))
		if inv.Code != CodeSchema || !strings.Contains(inv.Rule, "a key missing") {
			t.Errorf("a store without %v = %+v, want the missing-key refusal", path, inv)
		}
		old = without(t, old, path...)
	}
	if inv := decodeInvalid(t, old); !strings.Contains(inv.Rule, "a key missing") {
		t.Errorf("a store from before the questions room = %+v", inv)
	}
	// The same refusal for a key in another case, a key it does not have, and null for a value.
	for name, edit := range map[string][2]string{
		"upper key":          {`"reminded_ms"`, `"REMINDED_MS"`},
		"unknown outbox key": {`"questions_handover":true`, `"questions_handover":true,"questions_extra":1`},
		"null uncounted":     {`"questions_uncounted":{"count":2,"said":1}`, `"questions_uncounted":null`},
		"null handover":      {`"questions_handover":true`, `"questions_handover":null`},
		"null reports":       {`"reports":[1760000000000,1760000000500]`, `"reports":null`},
		"null remind_late":   {`"remind_late":[1,7]`, `"remind_late":null`},
		"empty questions":    {`"questions":{`, `"questions":{},"x":{`},
		"unknown nested key": {`"token":"s72594_4483_1934_0_1"`, `"token":"s72594_4483_1934_0_1","spare":1`},
		"float report":       {`"reports":[1760000000000,`, `"reports":[1760000000000.5,`},
	} {
		bad := strings.Replace(string(data), edit[0], edit[1], 1)
		if bad == string(data) {
			t.Fatalf("%s: the edit did not apply", name)
		}
		if _, err := DecodeStore([]byte(bad)); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: DecodeStore = %v, want ErrInvalid", name, err)
		}
	}
}

// Each rule of the new keys refuses by its own rule, never by another one that fires first.
func TestStoreRulesOfTheQuestionsRoom(t *testing.T) {
	for name, c := range map[string]struct {
		mutate func(*Store)
		rule   string
	}{
		"null reports":                {func(s *Store) { s.Outbox.Reports = nil }, "an outbox list is not a list"},
		"61 reports":                  {func(s *Store) { s.Outbox.Reports = times(capReports + 1) }, "reports holds more than 60 entries"},
		"negative report":             {func(s *Store) { s.Outbox.Reports[0] = -1 }, "a report time is not a time"},
		"report past the last time":   {func(s *Store) { s.Outbox.Reports[0] = proto.MaxSentMs + 1 }, "a report time is not a time"},
		"unechoed not an id":          {func(s *Store) { s.Outbox.QuestionsUnechoed = new("$short") }, "questions_unechoed is not an event id"},
		"unechoed a timeout key":      {func(s *Store) { s.Outbox.QuestionsUnechoed = new(eventID(1) + "~alice") }, "questions_unechoed is not an event id"},
		"negative count":              {func(s *Store) { s.Outbox.QuestionsUncounted = Uncounted{Count: -1, Said: -1} }, "questions_uncounted is not"},
		"negative said":               {func(s *Store) { s.Outbox.QuestionsUncounted = Uncounted{Count: 1, Said: -1} }, "questions_uncounted is not"},
		"said past count":             {func(s *Store) { s.Outbox.QuestionsUncounted = Uncounted{Count: 1, Said: 2} }, "questions_uncounted is not"},
		"null remind_late":            {func(s *Store) { s.RemindLate = nil }, "remind_late is not a list"},
		"65 remind_late":              {func(s *Store) { s.RemindLate = numbers(capRemindLate + 1) }, "remind_late holds more than 64 entries"},
		"remind_late zero":            {func(s *Store) { s.RemindLate = []int64{0} }, "remind_late is not distinct question numbers"},
		"remind_late negative":        {func(s *Store) { s.RemindLate = []int64{-3} }, "remind_late is not distinct question numbers"},
		"remind_late twice":           {func(s *Store) { s.RemindLate = []int64{4, 4} }, "remind_late is not distinct question numbers"},
		"negative reminded_ms":        {func(s *Store) { s.RemindedMS = -1 }, "reminded_ms is not a time"},
		"questions an empty state":    {func(s *Store) { s.Questions = &proto.QuestionsState{} }, "questions: an index at the head needs"},
		"questions no last event":     {func(s *Store) { s.Questions.Last = nil }, "questions: an index at the head needs"},
		"questions a space in token":  {func(s *Store) { s.Questions.Token = new("s1 2") }, "questions: token has a character"},
		"questions a null list":       {func(s *Store) { s.Questions.Index.Questions = nil }, "questions: questions is null"},
		"questions an owed with data": {func(s *Store) { s.Questions.Build = &proto.BuildRecord{After: new(eventID(1))} }, "questions: a build owed holds no state"},
		"questions another server": {func(s *Store) {
			s.Questions.Index.Unattached[0].From = "@alice:beta.teamchat.internal"
		}, "questions: an unattached post's from_id is not a user id of the server"},
	} {
		s := questionsStore(t)
		c.mutate(&s)
		_, err := s.encode()
		inv, ok := errors.AsType[*InvalidError](err)
		if !ok || !strings.Contains(inv.Rule, c.rule) {
			t.Errorf("%s: encode = %v, want the rule %q", name, err, c.rule)
		}
	}
	// The counts and the times may be as large as strict JSON holds, and are refused past it by
	// the decoder itself (no rule of the store bounds them again).
	s := questionsStore(t)
	s.Outbox.QuestionsUncounted, s.RemindedMS, s.RemindLate = Uncounted{Count: maxJSONInt, Said: maxJSONInt}, maxJSONInt, []int64{maxJSONInt}
	if _, err := s.encode(); err != nil {
		t.Errorf("the largest counts and times: %v", err)
	}
	for name, mutate := range map[string]func(*Store){
		"count":       func(s *Store) { s.Outbox.QuestionsUncounted.Count = maxJSONInt + 1 },
		"reminded_ms": func(s *Store) { s.RemindedMS = maxJSONInt + 1 },
		"remind_late": func(s *Store) { s.RemindLate = []int64{maxJSONInt + 1} },
		"report":      func(s *Store) { s.Outbox.Reports = []int64{maxJSONInt + 1} },
		"report in A": func(s *Store) { s.Outbox.Reports = []int64{proto.MaxSentMs + 1} },
		"both counts": func(s *Store) { s.Outbox.QuestionsUncounted = Uncounted{Count: maxJSONInt + 1, Said: maxJSONInt + 1} },
	} {
		past := questionsStore(t)
		mutate(&past)
		if _, err := past.encode(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s past its bound: encode = %v, want ErrInvalid", name, err)
		}
	}
}

// maxJSONInt is the largest integer strict JSON holds (proto.Parse): 2^53 - 1.
const maxJSONInt = 1<<53 - 1

// times is n report times, oldest first.
func times(n int) []int64 {
	out := make([]int64, n)
	for i := range out {
		out[i] = 1_760_000_000_000 + int64(i)
	}
	return out
}

// numbers is n distinct question numbers.
func numbers(n int) []int64 {
	out := make([]int64, n)
	for i := range out {
		out[i] = int64(i + 1)
	}
	return out
}

// The caps of the two lists hold exactly: 60 report times and 64 numbers are kept, one more is
// refused (n3f-D6, n3f-D11).
func TestStoreHoldsTheQuestionsRoomListsAtTheirCaps(t *testing.T) {
	s := questionsStore(t)
	s.Outbox.Reports, s.RemindLate = times(capReports), numbers(capRemindLate)
	data, err := s.encode()
	check(t, err)
	back, err := DecodeStore(data)
	check(t, err)
	if len(back.Outbox.Reports) != 60 || len(back.RemindLate) != 64 {
		t.Fatalf("read back %d report times and %d numbers", len(back.Outbox.Reports), len(back.RemindLate))
	}
	if capReports != 60 || capRemindLate != proto.MaxOpenQuestions {
		t.Errorf("caps: reports %d, remind_late %d; n3f-D6 says 60 and n3f-D11 the cap on questions not closed", capReports, capRemindLate)
	}
}

// G2: Pending.Room takes questions for HUMAN and TIMEOUT items. A TIMEOUT in questions is a
// reminder, keyed `<question event id>~humans` (A 10.5, proto.ReminderKey).
func TestPendingRoomQuestionsForHumanAndTimeout(t *testing.T) {
	s := questionsStore(t)
	human := Pending{ID: eventID(40), Room: proto.PurposeQuestions, Item: records.KindHuman, SentMS: 11, From: aliceID}
	reminder := Pending{ID: proto.ReminderKey(eventID(21)), Room: proto.PurposeQuestions, Item: records.KindTimeout, SentMS: 12, From: agentID}
	check(t, s.AddPending(human))
	check(t, s.AddPending(reminder))
	data, err := s.encode()
	check(t, err)
	back, err := DecodeStore(data)
	if err != nil {
		t.Fatalf("a HUMAN item and a reminder TIMEOUT in the questions room: %v", err)
	}
	got := back.Pending[len(back.Pending)-2:]
	if got[0].Room != proto.PurposeQuestions || got[1].Room != proto.PurposeQuestions || got[1].ID != eventID(21)+"~humans" {
		t.Errorf("read back %+v", got)
	}
	// The same reminder, delivered, is a ledger entry; and its id is an item id of an awaiting entry.
	s.AddLedger(Ledger{ID: reminder.ID, Room: proto.PurposeQuestions, Item: records.KindTimeout, From: agentID, AtMS: 13})
	s.Awaiting = []Awaiting{{Nonce: "3f9c2a71", IDs: []string{reminder.ID}, Transcript: TranscriptMark{Path: "/home/u/.claude/projects/p/x.jsonl"}, AtMS: 13, Written: WrittenYes}}
	if _, err := s.encode(); err != nil {
		t.Errorf("a delivered reminder: %v", err)
	}
}

// Only a reminder, a TIMEOUT in the questions room, is keyed `~humans`: in pending and in the
// ledger, by the reminder's own rule. (A pending item that is not a TIMEOUT is refused by
// pending's older rule that a `~` key is a TIMEOUT's, before this one.)
func TestOnlyAReminderIsKeyedHumans(t *testing.T) {
	id := proto.ReminderKey(eventID(21))
	for name, c := range map[string]struct{ room, item string }{
		"in main":      {proto.PurposeMain, records.KindTimeout},
		"in reports":   {proto.PurposeReports, records.KindTimeout},
		"not a timout": {proto.PurposeQuestions, records.KindPeer},
	} {
		inLedger := questionsStore(t)
		inLedger.Ledger = append(inLedger.Ledger, Ledger{ID: id, Room: c.room, Item: c.item, From: agentID, AtMS: 13})
		inPending := questionsStore(t)
		inPending.Pending = append(inPending.Pending, Pending{ID: id, Room: c.room, Item: c.item, SentMS: 12, From: agentID})
		for where, s := range map[string]Store{"ledger": inLedger, "pending": inPending} {
			_, err := s.encode()
			inv, ok := errors.AsType[*InvalidError](err)
			switch {
			case !ok:
				t.Errorf("a `~humans` key %s in %s: encode = %v, want ErrInvalid", name, where, err)
			case (c.item == records.KindTimeout || where == "ledger") && inv.Rule != reminderRule:
				t.Errorf("a `~humans` key %s in %s: refused by %q, want the reminder rule", name, where, inv.Rule)
			}
		}
	}
}

// A `~humans` key is the one name beyond an agent's handle and a human's name that an item key
// takes; the reserved words and no name at all are still refused.
func TestItemKeyGrammarTakesTheReminderWord(t *testing.T) {
	for suffix, want := range map[string]bool{
		"~alice": true, "~repo.3+site.lxc": true, "~humans": true,
		"~Humans": false, "~admin": false, "~teamchat": false, "~": false, "~humans~humans": false,
	} {
		if got := validItemID(eventID(1) + suffix); got != want {
			t.Errorf("validItemID(<event>%s) = %v, want %v", suffix, got, want)
		}
	}
}

// RM2: a reminder takes pending's reserved share, so a flood of agent items never holds it back.
func TestReminderTakesTheReserveOfPending(t *testing.T) {
	full := NewStore(teamID(t), agentID, StoreRun{ID: runID(t)}, Gate{})
	for i := range capPending - pendingReserve {
		check(t, full.AddPending(Pending{ID: eventID(i), Room: proto.PurposeQuestions, Item: records.KindPeer}))
	}
	if err := full.AddPending(Pending{ID: eventID(9000), Room: proto.PurposeQuestions, Item: records.KindPeer}); !errors.Is(err, ErrFull) {
		t.Fatalf("an agent item past the agent cap = %v", err)
	}
	check(t, full.AddPending(Pending{ID: proto.ReminderKey(eventID(9001)), Room: proto.PurposeQuestions, Item: records.KindTimeout}))
}

// worstQuestionsKey is the `questions` key with every cap full at its widest (QX6): 64 questions
// not closed and 128 closed, each with 16 `asked_by` of 128 bytes, 16 `re`, 16 unanswered, a
// headline of 120 code points that is written as 6 bytes each, and the integers at their
// widest; 1024 posts, 256 agent posts, 64 unattached posts, the longest token and a build under way.
func worstQuestionsKey() *proto.QuestionsState {
	const wide = 3_000_000_000_000_000
	longUser := func(n int) string {
		return fmt.Sprintf("@%s.%d+%s.podman:%s", strings.Repeat("r", 40), 999_000+n, strings.Repeat("s", 48), server)
	}
	ids := func(from, n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = eventID(from + i)
		}
		return out
	}
	st := proto.IndexState{Highest: wide, Slots: wide, Evicted: wide}
	for i := range proto.MaxOpenQuestions + proto.MaxClosedQuestions {
		asked := make([]string, proto.MaxAskedBy)
		for j := range asked {
			asked[j] = longUser(j)
		}
		st.Questions = append(st.Questions, proto.IndexedQuestion{
			N: wide - 191 + int64(i), EventID: eventID(100_000 + i), Latest: eventID(200_000 + i),
			Headline: strings.Repeat("<", proto.MaxHeadline), Hash: strings.Repeat("ab", 32),
			AskedBy: asked, Re: ids(300_000+i*16, proto.MaxQuestionRe), AskedMs: wide, RemindAfterS: proto.MaxRemindS,
			Closed: i >= proto.MaxOpenQuestions, Attached: wide, Unanswered: ids(400_000+i*16, proto.MaxUnanswered), UnansweredMore: wide - proto.MaxUnanswered,
		})
	}
	for i := range proto.MaxAttachments {
		st.Posts = append(st.Posts, proto.PostEntry{ID: eventID(500_000 + i), Q: wide, T: 1})
	}
	for i := range proto.MaxAgentPosts {
		st.AgentPosts = append(st.AgentPosts, proto.AgentPost{ID: eventID(800_000 + i), Q: wide})
	}
	for i := range proto.MaxUnattached {
		st.Unattached = append(st.Unattached, proto.UnattachedPost{ID: eventID(600_000 + i), From: longUser(i), SentMs: wide})
	}
	return &proto.QuestionsState{
		Index: &st, Token: new(strings.Repeat("~", maxNextBatch)), Last: new(eventID(700_000)),
		Build: &proto.BuildRecord{Start: new(eventID(700_001)), After: new(eventID(700_002)), LateFrom: new(int64(wide))},
	}
}

// worstListsFull is whether the key's lists and token are at their caps: 192 questions, 1024
// posts, 256 agent posts, 64 unattached posts and a 1024 byte token.
func worstListsFull(k *proto.QuestionsState) bool {
	ix := k.Index
	return len(ix.Questions) == 192 && len(ix.Posts) == 1024 && len(ix.AgentPosts) == 256 && len(ix.Unattached) == 64 && len(*k.Token) == 1024
}

// QX6's worst case is the worst case: every cap full, and the widest entry of each list.
func TestWorstQuestionsKeyIsFull(t *testing.T) {
	k := worstQuestionsKey()
	open := 0
	for _, q := range k.Index.Questions {
		if !q.Closed {
			open++
		}
	}
	q := k.Index.Questions[0]
	if open != 64 || !worstListsFull(k) ||
		len(q.AskedBy) != 16 || len(q.AskedBy[0]) != maxUserID || len(q.Re) != 16 || len(q.Unanswered) != 16 || q.UnansweredMore == 0 {
		t.Fatalf("a cap is not full: %d open of %d, %d posts, %d agent posts, %d unattached",
			open, len(k.Index.Questions), len(k.Index.Posts), len(k.Index.AgentPosts), len(k.Index.Unattached))
	}
	data, err := json.Marshal(q.Headline)
	check(t, err)
	if string(data) != `"`+strings.Repeat(`\u003c`, proto.MaxHeadline)+`"` {
		t.Errorf("the headline is not 120 code points of 6 written bytes each")
	}
	if err := k.Check(server); err != nil {
		t.Fatalf("the worst key breaks its own rules: %v", err)
	}
}
