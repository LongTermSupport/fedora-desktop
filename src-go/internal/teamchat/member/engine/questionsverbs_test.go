package engine

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// The events of the questions room the verbs' tests fold: questions, human posts, and the
// roots fetched for a retitle.
const (
	qOwnEv = 500 + iota
	qOtherEv
	qHumanTopEv
	qHumanThreadEv
	qLooseEv
	qLooseThreadEv
)

// coordinating is team ops held with this member (agent 1) its coordinator and an empty
// questions index; posts are human items of the questions room in its ledger, as delivered.
func (w *world) coordinating(ops paths.TeamID, posts ...int) *fakeTeam {
	w.t.Helper()
	s := w.newStore(ops)
	for _, n := range posts {
		s.Ledger = append(s.Ledger, ledgerItem(evID(n), proto.PurposeQuestions, records.KindHuman, alice(ops), nil))
	}
	f := w.sendingUnder(ops, s, coordinatorRoster(ops))
	f.q.mu.Lock()
	f.q.idx, f.q.self, f.q.events = proto.NewQuestionIndex(paths.ServerName(ops), 0), agent(ops, 1), map[string]json.RawMessage{}
	f.q.mu.Unlock()
	return f
}

// countQuestion folds a question by sender (agent 1 under coordinatorRoster, agent 3 under
// sendRoster) as the event id, posted at ts; the fake keeps its raw event for a fetch.
func countQuestion(t *testing.T, f *fakeTeam, ops paths.TeamID, id, sender string, ts int64) {
	t.Helper()
	r := coordinatorRoster(ops)
	if sender != agent(ops, 1) {
		r = sendRoster(ops)
	}
	f.q.mu.Lock()
	defer f.q.mu.Unlock()
	out, err := proto.Composer{Server: paths.ServerName(ops), Self: sender, Roster: r, Questions: f.q.idx}.Question(proto.QuestionPost{Text: strings.Replace(askBody, "MIT or CC-BY?", "MIT or CC-BY for "+id+"?", 1)})
	check(t, err)
	dc, reason := proto.DecodeContent(out.Content, proto.QuestionKeys)
	if reason != "" {
		t.Fatal(reason)
	}
	ev := proto.Event{ID: id, Type: proto.MessageType, Sender: sender, OriginTs: ts}
	if !f.q.idx.Feed(proto.Verdict{Outcome: proto.Index, Event: ev, Content: dc}) {
		t.Fatal("the question was not counted")
	}
	raw, err := json.Marshal(map[string]any{
		"event_id": id, "sender": sender, "type": proto.MessageType, "origin_server_ts": ts, "room_id": qRoom,
		"content": json.RawMessage(out.Content.Compact()),
	})
	check(t, err)
	f.q.events[id] = raw
}

// humanPost attaches alice's post id (root: its thread, "" for none) and gives the fake's refetch
// its verdict.
func humanPost(t *testing.T, f *fakeTeam, ops paths.TeamID, id, body, root string, ts int64) {
	t.Helper()
	ev := proto.Event{ID: id, Type: proto.MessageType, Sender: alice(ops), OriginTs: ts}
	h := proto.Human{Body: body, ThreadRoot: root}
	f.q.mu.Lock()
	f.q.idx.Attach(ev, h)
	f.q.mu.Unlock()
	f.setVerdict(id, proto.Verdict{Outcome: proto.Deliver, Kind: proto.KindHuman, Class: proto.SenderHuman, Event: ev, Human: h})
}

// sentEv is a sent content as the verbs' tests read it.
type sentEv struct {
	MsgType  string `json:"msgtype"`
	Body     string `json:"body"`
	Mentions struct {
		UserIDs []string `json:"user_ids"`
	} `json:"m.mentions"`
	Rel struct {
		RelType     string `json:"rel_type"`
		EventID     string `json:"event_id"`
		FallingBack *bool  `json:"is_falling_back"`
		InReplyTo   struct {
			EventID string `json:"event_id"`
		} `json:"m.in_reply_to"`
	} `json:"m.relates_to"`
	Question struct {
		Why string `json:"why"`
	} `json:"teamchat.question"`
	Thread struct {
		Wait bool   `json:"wait"`
		Link string `json:"link"`
	} `json:"teamchat.thread"`
	NewContent struct {
		Body string `json:"body"`
	} `json:"m.new_content"`
}

func (s sentEv) mentioned() []string {
	if s.Mentions.UserIDs == nil {
		return []string{}
	}
	return s.Mentions.UserIDs
}

func (s sentEv) fallingBack() bool { return s.Rel.FallingBack != nil && *s.Rel.FallingBack }

// sentContent is the i-th content the team's homeserver was sent.
func sentContent(t *testing.T, f *fakeTeam, i int) sentEv {
	t.Helper()
	f.notices.mu.Lock()
	defer f.notices.mu.Unlock()
	if i >= len(f.notices.contents) {
		t.Fatalf("%d sends, want at least %d", len(f.notices.contents), i+1)
	}
	var s sentEv
	check(t, json.Unmarshal(f.notices.contents[i], &s))
	return s
}

func recordsOf[R records.Record](a answer) []R {
	var out []R
	for _, r := range a.out.recs {
		if x, ok := r.(R); ok {
			out = append(out, x)
		}
	}
	return out
}

func (w *world) q(op string, args any, stdin *string) answer {
	w.t.Helper()
	return w.request(w.t.Context(), op, args, stdin)
}

// ask-human (2.1): posted to questions as m.text mentioning the humans, with why and the --for
// seats; the records are sent, then the question with its why, for and waiting time.
func TestAskHumanPostsANotifyingQuestion(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.coordinating(ops)
	a := w.q(wire.OpAskHuman, wire.AskArgs{For: []string{"repo.3"}}, new(askBody))
	if a.d.Error != nil || a.out.kinds() != "sent,question" {
		t.Fatalf("ask-human = %+v %v, records %s", a.d, a.d.Error, a.out.kinds())
	}
	c := sentContent(t, f, 0)
	if c.MsgType != proto.MsgText || !slices.Equal(c.mentioned(), []string{alice(ops)}) || c.Question.Why != "decision" {
		t.Errorf("the question = %v", c)
	}
	q := recordsOf[records.Question](a)[0]
	if q.N == nil || *q.N != 1 || q.Why == nil || *q.Why != "decision" || !slices.Equal(q.For, []string{"repo.3"}) || q.WaitingSinceMS == nil {
		t.Errorf("the question record = %+v", q)
	}
}

// ask-human's refusals: the form (10 body), the why (10 body), a size (10, both sizes), the
// --for seat (unknown-member), a duplicate (15), and a member that does not coordinate (11).
func TestAskHumanRefusals(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.coordinating(ops)
	noWhy := strings.Replace(askBody, "\nWhy a human: decision on the licence", "", 1)
	refusedCode(t, "no why", firstOf(w.q(wire.OpAskHuman, wire.AskArgs{}, &noWhy)), exits.CodeBody)
	badWhy := strings.Replace(askBody, "Why a human: decision", "Why a human: whim", 1)
	refusedCode(t, "a bad why", firstOf(w.q(wire.OpAskHuman, wire.AskArgs{}, &badWhy)), exits.CodeBody)
	noContext := "Which licence?\nQuestion: MIT?\nWhy a human: decision"
	refusedCode(t, "no context", firstOf(w.q(wire.OpAskHuman, wire.AskArgs{}, &noContext)), exits.CodeBody)
	big := askBody + "\n" + strings.Repeat("a", 8000)
	d := firstOf(w.q(wire.OpAskHuman, wire.AskArgs{}, &big))
	refusedCode(t, "too big", d, exits.CodeBody)
	if !strings.Contains(d.Error.Message, "8000-byte limit") {
		t.Errorf("the size refusal does not name the limit: %q", d.Error.Message)
	}
	refusedCode(t, "unknown seat", firstOf(w.q(wire.OpAskHuman, wire.AskArgs{For: []string{"nobody"}}, new(askBody))), exits.CodeUnknownMember)
	refusedCode(t, "a human seat", firstOf(w.q(wire.OpAskHuman, wire.AskArgs{For: []string{"alice"}}, new(askBody))), exits.CodeUnknownMember)
	refusedCode(t, "remind", firstOf(w.q(wire.OpAskHuman, wire.AskArgs{RemindAfterS: new(int64(5))}, new(askBody))), exits.CodeUsage)
	refusedCode(t, "no stdin", firstOf(w.q(wire.OpAskHuman, wire.AskArgs{}, nil)), exits.CodeUsage)
	if a := w.q(wire.OpAskHuman, wire.AskArgs{}, new(askBody)); a.d.Error != nil {
		t.Fatalf("the first ask = %v", a.d.Error)
	}
	refusedCode(t, "the same question", firstOf(w.q(wire.OpAskHuman, wire.AskArgs{}, new(askBody))), exits.CodeDuplicate)

	w2 := newWorld(t, state.DeliverySocket)
	w2.sendingUnder(ops, w2.newStore(ops), sendRoster(ops))
	refusedCode(t, "a worker", firstOf(w2.q(wire.OpAskHuman, wire.AskArgs{}, new(askBody))), exits.CodeRole)
}

// ask-human --check sends nothing and prints the question record with n null.
func TestAskHumanCheckSendsNothing(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.coordinating(ops)
	a := w.q(wire.OpAskHuman, wire.AskArgs{Check: true}, new(askBody))
	if a.d.Error != nil || a.out.kinds() != "question" || len(f.notices.sent()) != 0 {
		t.Fatalf("--check = %v, records %s, sent %v", a.d.Error, a.out.kinds(), f.notices.sent())
	}
	if q := recordsOf[records.Question](a)[0]; q.N != nil || q.Headline != "Which licence for the docs?" {
		t.Errorf("the checked record = %+v", q)
	}
}

// thread (2.2): notify by default (m.text, the humans, wait), quiet with --note; a question not
// kept is 23.
func TestThreadNotifiesByDefaultAndNoteIsQuiet(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.coordinating(ops)
	countQuestion(t, f, ops, evID(qOwnEv), agent(ops, 1), startWall)
	if a := w.q(wire.OpThread, wire.ThreadArgs{Q: "Q1"}, new("Any view?")); a.d.Error != nil || a.out.kinds() != "sent,question" {
		t.Fatalf("thread = %v, records %s", a.d.Error, a.out.kinds())
	}
	c := sentContent(t, f, 0)
	if c.MsgType != proto.MsgText || !c.Thread.Wait || !slices.Equal(c.mentioned(), []string{alice(ops)}) || c.Rel.EventID != evID(qOwnEv) {
		t.Errorf("a default thread post = %v", c)
	}
	if a := w.q(wire.OpThread, wire.ThreadArgs{Q: "Q1", Note: true}, new("Relayed to repo.3.")); a.d.Error != nil {
		t.Fatalf("thread --note = %v", a.d.Error)
	}
	if c := sentContent(t, f, 1); c.MsgType != proto.MsgNotice || len(c.mentioned()) != 0 {
		t.Errorf("a note = %v", c)
	}
	refusedCode(t, "Q9", firstOf(w.q(wire.OpThread, wire.ThreadArgs{Q: "Q9"}, new("x"))), exits.CodeNotFound)
	refusedCode(t, "blank", firstOf(w.q(wire.OpThread, wire.ThreadArgs{Q: "Q1"}, new("  "))), exits.CodeBody)
}

// reply to a human post attached to Q<n> at the top level: the answer in Q<n>'s thread, then the
// link-back, a top-level reply naming the answer's permalink and mentioning the human.
func TestReplyToATopLevelAttachedPostLinksBack(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.coordinating(ops, qHumanTopEv)
	countQuestion(t, f, ops, evID(qOwnEv), agent(ops, 1), startWall)
	humanPost(t, f, ops, evID(qHumanTopEv), "Q1: MIT", "", startWall+1)
	a := w.reply(wire.ReplyArgs{ID: evID(qHumanTopEv)}, new("MIT it is."))
	if a.d.Error != nil || a.out.kinds() != "sent,sent" {
		t.Fatalf("reply = %v, records %s", a.d.Error, a.out.kinds())
	}
	answer, link := sentContent(t, f, 0), sentContent(t, f, 1)
	if answer.Rel.EventID != evID(qOwnEv) || answer.Rel.InReplyTo.EventID != evID(qHumanTopEv) || !slices.Equal(answer.mentioned(), []string{alice(ops)}) {
		t.Errorf("the answer = %v", answer)
	}
	answerID := *recordsOf[records.Sent](a)[0].ID
	if link.Rel.InReplyTo.EventID != evID(qHumanTopEv) || link.Rel.RelType != "" || link.Thread.Link != answerID ||
		!strings.Contains(link.Body, proto.Permalink(qRoom, answerID)) || !slices.Equal(link.mentioned(), []string{alice(ops)}) {
		t.Errorf("the link-back = %v", link)
	}
}

// reply to a post in Q<n>'s thread: the answer alone, no link-back.
func TestReplyInTheThreadSendsNoLinkBack(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.coordinating(ops, qHumanThreadEv)
	countQuestion(t, f, ops, evID(qOwnEv), agent(ops, 1), startWall)
	humanPost(t, f, ops, evID(qHumanThreadEv), "MIT", evID(qOwnEv), startWall+1)
	if a := w.reply(wire.ReplyArgs{ID: evID(qHumanThreadEv)}, new("Done.")); a.d.Error != nil || a.out.kinds() != "sent" {
		t.Fatalf("reply = %v, records %s", a.d.Error, a.out.kinds())
	}
}

// The coordinator's input to OD-Q3: a reply to an unattached post starts a thread at a top-level
// post, and stays in the thread of a post a human posted in a thread.
func TestReplyToAnUnattachedPostIsAThreadPost(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.coordinating(ops, qLooseEv, qLooseThreadEv)
	humanPost(t, f, ops, evID(qLooseEv), "about the docs", "", startWall)
	humanPost(t, f, ops, evID(qLooseThreadEv), "the licence part", evID(qLooseEv), startWall+1)
	if a := w.reply(wire.ReplyArgs{ID: evID(qLooseEv)}, new("Which question?")); a.d.Error != nil {
		t.Fatalf("reply = %v", a.d.Error)
	}
	top := sentContent(t, f, 0).Rel
	if top.RelType != "m.thread" || top.EventID != evID(qLooseEv) || top.FallingBack == nil || !*top.FallingBack || top.InReplyTo.EventID != evID(qLooseEv) {
		t.Errorf("the reply to a top-level post = %+v", top)
	}
	if a := w.reply(wire.ReplyArgs{ID: evID(qLooseThreadEv)}, new("Which one?")); a.d.Error != nil {
		t.Fatalf("reply in a thread = %v", a.d.Error)
	}
	in := sentContent(t, f, 1)
	if in.Rel.RelType != "m.thread" || in.Rel.EventID != evID(qLooseEv) || in.Rel.FallingBack == nil || in.fallingBack() || in.Rel.InReplyTo.EventID != evID(qLooseThreadEv) {
		t.Errorf("the reply in a human's thread = %+v", in.Rel)
	}
}

// close (2.4): a quiet closing post, then the retitle of an own root; another seat's root is
// not edited and the record says retitled false. A closed question is 10 target.
func TestCloseRetitlesOnlyItsOwnRoot(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.coordinating(ops)
	countQuestion(t, f, ops, evID(qOtherEv), agent(ops, 3), startWall)
	countQuestion(t, f, ops, evID(qOwnEv), agent(ops, 1), startWall+1)
	a := w.q(wire.OpClose, wire.CloseArgs{Qs: []string{"Q1", "Q2"}}, nil)
	if a.d.Error != nil || a.out.kinds() != "sent,question,sent,sent,question" {
		t.Fatalf("close = %v, records %s", a.d.Error, a.out.kinds())
	}
	closing := sentContent(t, f, 0)
	if closing.MsgType != proto.MsgNotice || closing.Body != "Q1 closed." {
		t.Errorf("the closing post = %v", closing)
	}
	edit := sentContent(t, f, 2)
	if edit.Rel.RelType != "m.replace" || edit.Rel.EventID != evID(qOwnEv) || !strings.HasPrefix(edit.NewContent.Body, "Q2 "+proto.ClosedTitle) {
		t.Errorf("the retitle = %v", edit)
	}
	qs := recordsOf[records.Question](a)
	if qs[0].Retitled == nil || *qs[0].Retitled || qs[1].Retitled == nil || !*qs[1].Retitled || qs[0].State != proto.QuestionClosed {
		t.Errorf("the records = %+v", qs)
	}
	refusedCode(t, "closed", firstOf(w.q(wire.OpClose, wire.CloseArgs{Qs: []string{"Q1"}}, nil)), exits.CodeTarget)
	sends := len(f.notices.sent())
	refusedCode(t, "one unknown", firstOf(w.q(wire.OpClose, wire.CloseArgs{Qs: []string{"Q3", "Q9"}}, nil)), exits.CodeNotFound)
	if len(f.notices.sent()) != sends {
		t.Error("close sent before every name was judged")
	}
}

// retitle (2.5): the new headline on an own root; another seat's root is 10 target.
func TestRetitle(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.coordinating(ops)
	countQuestion(t, f, ops, evID(qOtherEv), agent(ops, 3), startWall)
	countQuestion(t, f, ops, evID(qOwnEv), agent(ops, 1), startWall+1)
	refusedCode(t, "another seat's", firstOf(w.q(wire.OpRetitle, wire.RetitleArgs{Q: "Q1"}, new("New"))), exits.CodeTarget)
	a := w.q(wire.OpRetitle, wire.RetitleArgs{Q: "Q2"}, new("Docs licence, decided by Friday"))
	if a.d.Error != nil || a.out.kinds() != "sent,question" {
		t.Fatalf("retitle = %v, records %s", a.d.Error, a.out.kinds())
	}
	edit := sentContent(t, f, 0)
	if edit.MsgType != proto.MsgNotice || !strings.HasPrefix(edit.NewContent.Body, "Q2 Docs licence, decided by Friday\n") {
		t.Errorf("the edit = %v", edit)
	}
	refusedCode(t, "Q9", firstOf(w.q(wire.OpRetitle, wire.RetitleArgs{Q: "Q9"}, new("x"))), exits.CodeNotFound)
}

// bump (2.7): a waiting question older than --min-age is bumped with a notifying post; an
// answered one and a young one are skipped with their reason.
func TestBumpSkipsAnsweredAndYoung(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.coordinating(ops)
	now := w.clk.WallMS()
	countQuestion(t, f, ops, evID(qOtherEv), agent(ops, 3), now-7_200_000)
	countQuestion(t, f, ops, evID(qOwnEv), agent(ops, 1), now-60_000)
	countQuestion(t, f, ops, evID(qOwnEv+100), agent(ops, 1), now-7_200_000)
	humanPost(t, f, ops, evID(qHumanThreadEv), "MIT", evID(qOwnEv+100), now-1000)
	a := w.q(wire.OpBump, wire.BumpArgs{Qs: []string{"Q1", "Q2", "Q3"}, MinAgeS: 3600}, nil)
	if a.d.Error != nil || a.out.kinds() != "sent,bump,bump,bump" {
		t.Fatalf("bump = %v, records %s", a.d.Error, a.out.kinds())
	}
	bs := recordsOf[records.Bump](a)
	if !bs[0].Bumped || bs[1].Why == nil || *bs[1].Why != records.BumpYoung || bs[2].Why == nil || *bs[2].Why != records.BumpAnswered {
		t.Errorf("the bump records = %+v", bs)
	}
	c := sentContent(t, f, 0)
	if c.MsgType != proto.MsgText || !strings.HasPrefix(c.Body, "Q1 still waiting: ") || c.Rel.EventID != evID(qOtherEv) {
		t.Errorf("the bump = %v", c)
	}
	w.clk.Advance(proto.DuplicateWindowMs * time.Millisecond)
	if a := w.q(wire.OpBump, wire.BumpArgs{Waiting: true, MinAgeS: 3600}, nil); a.out.kinds() != "sent,bump,bump" {
		t.Errorf("bump --waiting = %v, records %s", a.d.Error, a.out.kinds())
	}
}

// questions --waiting lists the waiting questions oldest first; --for the seat's; --digest
// posts one report to the reports room.
func TestQuestionsWaitingForAndDigest(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.coordinating(ops)
	now := w.clk.WallMS()
	countQuestion(t, f, ops, evID(qOwnEv), agent(ops, 1), now-60_000)
	countQuestion(t, f, ops, evID(qOwnEv+100), agent(ops, 1), now-7_200_000)
	countQuestion(t, f, ops, evID(qOwnEv+200), agent(ops, 1), now-1000)
	humanPost(t, f, ops, evID(qHumanThreadEv), "MIT", evID(qOwnEv+200), now-500)
	a := w.q(wire.OpQuestions, wire.QuestionsArgs{Waiting: true}, nil)
	qs := recordsOf[records.Question](a)
	if a.d.Error != nil || len(qs) != 2 || *qs[0].N != 2 || *qs[1].N != 1 {
		t.Fatalf("--waiting = %v, records %+v", a.d.Error, qs)
	}
	all := recordsOf[records.Question](w.q(wire.OpQuestions, wire.QuestionsArgs{}, nil))
	if len(all) != 3 {
		t.Errorf("questions = %+v", all)
	}
	if got := recordsOf[records.Question](w.q(wire.OpQuestions, wire.QuestionsArgs{For: new("repo.3")}, nil)); len(got) != 0 {
		t.Errorf("--for a seat no question names = %+v", got)
	}
	d := w.q(wire.OpQuestions, wire.QuestionsArgs{Digest: true}, nil)
	if d.d.Error != nil || d.out.kinds() != "sent" {
		t.Fatalf("--digest = %v, records %s", d.d.Error, d.out.kinds())
	}
	body := sentContent(t, f, 0).Body
	if !strings.HasPrefix(body, "Waiting on you: 2 questions\n") || !strings.Contains(body, "Q2 (2h") || f.notices.sentRooms()[0] == qRoom {
		t.Errorf("the digest = %q to %v", body, f.notices.sentRooms())
	}
}
