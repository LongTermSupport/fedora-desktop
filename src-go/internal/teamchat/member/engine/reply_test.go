package engine

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// The ledger items the replies answer: alice's text, repo.2's text, repo.3's review ping,
// repo.2's done, a carried item, a question, and the probe's check.
const (
	humanEv = iota + 1
	peerEv
	reviewEv
	doneEv
	carriedEv
	questionEv
	checkEv
	unknownEv
)

func ledgerOf(id string, room, item, from string, verb *string) state.Ledger {
	return state.Ledger{ID: id, Room: room, Item: item, From: from, AtMS: startWall, Verb: verb}
}

// replyStore is a new store whose ledger holds the items the replies answer.
func (w *world) replyStore(id paths.TeamID) state.Store {
	s := w.newStore(id)
	s.Ledger = []state.Ledger{
		ledgerOf(evID(humanEv), proto.PurposeMain, records.KindHuman, alice(id), nil),
		ledgerOf(evID(peerEv), proto.PurposeMain, records.KindPeer, agent(id, 2), nil),
		ledgerOf(evID(reviewEv), proto.PurposeMain, records.KindPing, agent(id, 3), new("review")),
		ledgerOf(evID(doneEv), proto.PurposeMain, records.KindPing, agent(id, 2), new("done")),
		ledgerOf(evID(carriedEv), proto.PurposeMain, records.KindNotice, alice(id), nil),
		ledgerOf(evID(questionEv), proto.PurposeQuestions, records.KindHuman, alice(id), nil),
		ledgerOf(evID(checkEv), proto.PurposeMain, records.KindPing, agent(id, 4), new("check")),
	}
	return s
}

func (w *world) reply(a wire.ReplyArgs, stdin *string) answer {
	w.t.Helper()
	return w.request(w.t.Context(), wire.OpReply, a, stdin)
}

func verbReply(n int, verb string) wire.ReplyArgs {
	return wire.ReplyArgs{ID: evID(n), Verb: &verb}
}

// carriedVerdict is the verdict of a carried item of kind from from.
func carriedVerdict(id paths.TeamID, n int, kind, from string) proto.Verdict {
	c := proto.Carried{Kind: kind, From: from, SentMs: 1, To: agent(id, 1), Text: "carried"}
	if kind == proto.CarriedPing {
		c.Verb, c.Ref = "review", reviewRef
	}
	return proto.Verdict{
		Outcome: proto.Deliver, Kind: proto.KindNotice, Carried: true,
		Event:   proto.Event{ID: evID(n), Sender: alice(id), OriginTs: startWall},
		Content: proto.Content{Key: proto.KeyCarried, Carried: c},
	}
}

// SD1-1: a reply to a human item is a reply in main to the human, which a worker copies to
// the coordinator (P21); --to-coordinator adds the coordinator to a reply to a peer.
func TestSD1_1AReplyToAHumanItem(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.sendingWith(ops, w.replyStore(ops))
	posted(t, w.reply(wire.ReplyArgs{ID: evID(humanEv)}, new("noted")))
	c := contentOf(t, lastContent(t, f))
	if c.Key != proto.KeyText || c.Text.Text != "noted" || c.Relation != proto.ReplyTo(evID(humanEv)) {
		t.Fatalf("the reply = %+v", c)
	}
	if want := []string{alice(ops), agent(ops, 3)}; !slices.Equal(c.Text.To, want) {
		t.Errorf("the reply is to %v, want %v", c.Text.To, want)
	}

	posted(t, w.reply(wire.ReplyArgs{ID: evID(peerEv), ToCoordinator: true}, new("seen")))
	if c := contentOf(t, lastContent(t, f)); !slices.Equal(c.Text.To, []string{agent(ops, 2), agent(ops, 3)}) {
		t.Errorf("--to-coordinator is to %v", c.Text.To)
	}
	posted(t, w.reply(wire.ReplyArgs{ID: evID(reviewEv), ToCoordinator: true}, new("on it")))
	if c := contentOf(t, lastContent(t, f)); !slices.Equal(c.Text.To, []string{agent(ops, 3)}) {
		t.Errorf("--to-coordinator to the coordinator itself is to %v", c.Text.To)
	}
	if f.refetchedIDs() != nil {
		t.Errorf("a ledger reply fetched %v", f.refetchedIDs())
	}

	refusedCode(t, "no text", firstOf(w.reply(wire.ReplyArgs{ID: evID(humanEv)}, nil)), exits.CodeUsage)
	refusedCode(t, "an empty text", firstOf(w.reply(wire.ReplyArgs{ID: evID(humanEv)}, new(""))), exits.CodeEmpty)
	refusedCode(t, "--ref without --verb", firstOf(w.reply(wire.ReplyArgs{ID: evID(humanEv), Ref: new(reviewRef)}, new("x"))), exits.CodeUsage)
	refusedCode(t, "a question", firstOf(w.reply(wire.ReplyArgs{ID: evID(questionEv)}, new("x"))), exits.CodeInternal)
	if d := w.done(wire.OpReply, map[string]any{"nope": 1}); d.Error == nil || d.Error.Code != exits.CodeSchema {
		t.Fatalf("reply with bad arguments = %+v", d)
	}

	r := sendRoster(ops)
	delete(r.Members, agent(ops, 3))
	f.notices.set(r)
	refusedCode(t, "--to-coordinator with none", firstOf(w.reply(wire.ReplyArgs{ID: evID(peerEv), ToCoordinator: true}, new("x"))), exits.CodeUnknownMember)
}

// SD1-2: --verb answers a ping that expects one, in its thread to its sender, a reference
// forge-checked; on any other item it is 10 re.
func TestSD1_2AReplyWithAVerbAnswersAPing(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.sendingWith(ops, w.replyStore(ops))
	posted(t, w.reply(verbReply(reviewEv, proto.VerbAck), nil))
	c := contentOf(t, lastContent(t, f))
	if c.Key != proto.KeyPing || c.Ping.Verb != proto.VerbAck || c.Relation != proto.AnswerTo(evID(reviewEv)) {
		t.Fatalf("the answer = %+v", c)
	}
	if !slices.Equal(c.Ping.To, []string{agent(ops, 3)}) {
		t.Errorf("the answer is to %v", c.Ping.To)
	}

	calls := w.forge.count()
	done := verbReply(reviewEv, proto.VerbDone)
	done.Ref = new(reviewRef)
	posted(t, w.reply(done, nil))
	if w.forge.count() != calls+1 {
		t.Error("done --ref was not forge-checked")
	}
	if c := contentOf(t, lastContent(t, f)); c.Ping.Ref != reviewRef || c.Relation.Root != evID(reviewEv) {
		t.Errorf("done --ref = %+v", c)
	}
	w.forge.set(proto.ReasonProvenance, nil)
	blocked := verbReply(reviewEv, proto.VerbBlocked)
	blocked.Ref = new(reviewRef)
	refusedCode(t, "a reference failing provenance", firstOf(w.reply(blocked, nil)), exits.CodeProvenance)

	for name, n := range map[string]int{"a peer item": peerEv, "an answer item": doneEv, "a human item": humanEv} {
		refusedCode(t, "--verb on "+name, firstOf(w.reply(verbReply(n, proto.VerbAck), nil)), exits.CodeRe)
	}
	toCoord := verbReply(reviewEv, proto.VerbAck)
	toCoord.ToCoordinator = true
	refusedCode(t, "--verb with --to-coordinator", firstOf(w.reply(toCoord, nil)), exits.CodeUsage)
	refusedCode(t, "--verb with stdin", firstOf(w.reply(verbReply(reviewEv, proto.VerbAck), new("x"))), exits.CodeUsage)
	refusedCode(t, "--verb review", firstOf(w.reply(verbReply(reviewEv, "review"), nil)), exits.CodeVerb)
}

// SD1-3: a carried ping is answered to its original sender; one whose sender is no longer a
// member of the roster is 10 target; only a carried ping takes --verb.
func TestSD1_3ACarriedPingIsAnsweredToItsSender(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.sendingWith(ops, w.replyStore(ops))
	f.setVerdict(evID(carriedEv), carriedVerdict(ops, carriedEv, proto.CarriedPing, agent(ops, 3)))
	posted(t, w.reply(verbReply(carriedEv, proto.VerbAck), nil))
	c := contentOf(t, lastContent(t, f))
	if c.Relation != proto.AnswerTo(evID(carriedEv)) || !slices.Equal(c.Ping.To, []string{agent(ops, 3)}) {
		t.Fatalf("the carried answer = %+v", c)
	}
	if got := f.refetchedIDs(); !slices.Equal(got, []string{evID(carriedEv)}) {
		t.Errorf("fetched %v", got)
	}
	posted(t, w.reply(wire.ReplyArgs{ID: evID(carriedEv)}, new("thanks")))
	if c := contentOf(t, lastContent(t, f)); c.Relation != proto.ReplyTo(evID(carriedEv)) || !slices.Equal(c.Text.To, []string{agent(ops, 3)}) {
		t.Errorf("a text to a carried item = %+v", c)
	}

	gone := "@repo.9+site.podman:" + paths.ServerName(ops)
	f.setVerdict(evID(carriedEv), carriedVerdict(ops, carriedEv, proto.CarriedPing, gone))
	refusedCode(t, "a carried ping from a former member", firstOf(w.reply(verbReply(carriedEv, proto.VerbAck), nil)), exits.CodeTarget)
	f.setVerdict(evID(carriedEv), carriedVerdict(ops, carriedEv, "text", agent(ops, 3)))
	refusedCode(t, "--verb on a carried text", firstOf(w.reply(verbReply(carriedEv, proto.VerbAck), nil)), exits.CodeRe)

	notice := proto.Verdict{
		Outcome: proto.Deliver, Kind: proto.KindNotice,
		Event:   proto.Event{ID: evID(carriedEv), Sender: agent(ops, 2), OriginTs: startWall},
		Content: proto.Content{Key: proto.KeyNotice, Notice: proto.Notice{Event: proto.EventBack, Subject: agent(ops, 2)}},
	}
	f.setVerdict(evID(carriedEv), notice)
	refusedCode(t, "--verb on a notice", firstOf(w.reply(verbReply(carriedEv, proto.VerbAck), nil)), exits.CodeRe)
	posted(t, w.reply(wire.ReplyArgs{ID: evID(carriedEv)}, new("welcome back")))
	if c := contentOf(t, lastContent(t, f)); !slices.Equal(c.Text.To, []string{agent(ops, 2)}) {
		t.Errorf("a text to a notice is to %v", c.Text.To)
	}
}

// SD1-4: an id in no ledger is fetched from --team or the only team; 404 or no item is 23; two
// teams need --team; an own event and a TIMEOUT's key are 64; no answer is 20.
func TestSD1_4AnUnknownIDIsFetched(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.sending(ops)
	f.setVerdict(evID(unknownEv), proto.Verdict{
		Outcome: proto.Deliver, Kind: proto.KindPing, Event: proto.Event{ID: evID(unknownEv), Sender: agent(ops, 3), OriginTs: startWall},
		Content: proto.Content{Key: proto.KeyPing, Ping: proto.Ping{Verb: "review", To: []string{agent(ops, 1)}, Ref: reviewRef}},
	})
	posted(t, w.reply(verbReply(unknownEv, proto.VerbAck), nil))
	if c := contentOf(t, lastContent(t, f)); c.Relation != proto.AnswerTo(evID(unknownEv)) || !slices.Equal(c.Ping.To, []string{agent(ops, 3)}) {
		t.Fatalf("the answer to a fetched ping = %+v", c)
	}
	f.setVerdict(evID(peerEv), proto.Verdict{Outcome: proto.Deliver, Kind: proto.KindPeer, Event: proto.Event{ID: evID(peerEv), Sender: agent(ops, 2)}})
	posted(t, w.reply(wire.ReplyArgs{ID: evID(peerEv)}, new("fetched")))
	f.setVerdict(evID(humanEv), proto.Verdict{Outcome: proto.Deliver, Kind: proto.KindHuman, Event: proto.Event{ID: evID(humanEv), Sender: alice(ops)}})
	posted(t, w.reply(wire.ReplyArgs{ID: evID(humanEv)}, new("hello alice")))

	refusedCode(t, "no item", firstOf(w.reply(wire.ReplyArgs{ID: evID(99)}, new("x"))), exits.CodeNotFound)
	f.setVerdict(evID(98), proto.Verdict{Outcome: proto.Drop, Event: proto.Event{ID: evID(98), Sender: agent(ops, 2)}})
	refusedCode(t, "a dropped event", firstOf(w.reply(wire.ReplyArgs{ID: evID(98)}, new("x"))), exits.CodeNotFound)
	f.setVerdict(evID(97), proto.Verdict{Outcome: proto.Ignore, Event: proto.Event{ID: evID(97), Sender: agent(ops, 1)}})
	refusedCode(t, "an own event", firstOf(w.reply(wire.ReplyArgs{ID: evID(97)}, new("x"))), exits.CodeUsage)
	refusedCode(t, "a TIMEOUT's key", firstOf(w.reply(wire.ReplyArgs{ID: proto.TimeoutKey(evID(1), "repo.2")}, new("x"))), exits.CodeUsage)

	f.setRefetchErr(matrix.ErrNotFound)
	refusedCode(t, "a 404", firstOf(w.reply(wire.ReplyArgs{ID: evID(unknownEv)}, new("x"))), exits.CodeNotFound)
	f.setRefetchErr(errors.New("connection refused"))
	refusedCode(t, "no answer", firstOf(w.reply(wire.ReplyArgs{ID: evID(unknownEv)}, new("x"))), exits.CodeUnreachable)
	f.setRefetchErr(exits.New(exits.CodeUntrusted, &ops, "not authentic"))
	refusedCode(t, "an unauthentic event", firstOf(w.reply(wire.ReplyArgs{ID: evID(unknownEv)}, new("x"))), exits.CodeUntrusted)

	w.sending(tid(t, "dev"))
	refusedCode(t, "two teams", firstOf(w.reply(wire.ReplyArgs{ID: evID(unknownEv)}, new("x"))), exits.CodeAmbiguousTeam)
	f.setRefetchErr(nil)
	posted(t, w.reply(wire.ReplyArgs{Team: &ops, ID: evID(peerEv)}, new("with --team")))
}

// SD1-5: a worker answers the probe's check from the ledger alone.
func TestSD1_5AWorkerAnswersTheProbesCheck(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.sendingWith(ops, w.replyStore(ops))
	r := sendRoster(ops)
	r.Members[agent(ops, 4)] = proto.RosterMember{Role: proto.RoleProbe}
	f.notices.set(r)
	posted(t, w.reply(verbReply(checkEv, proto.VerbAck), nil))
	if c := contentOf(t, lastContent(t, f)); c.Relation != proto.AnswerTo(evID(checkEv)) || !slices.Equal(c.Ping.To, []string{agent(ops, 4)}) {
		t.Fatalf("the answer to the check = %+v", c)
	}
	if f.refetchedIDs() != nil {
		t.Errorf("the check's answer fetched %v", f.refetchedIDs())
	}
}

// SD1-6 (offline): a reply to a ledger item while the team is down is queued, exit 0.
func TestSD1_6AReplyWhileDownIsQueued(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.downWith(ops, w.replyStore(ops))
	queued(t, w.reply(wire.ReplyArgs{ID: evID(humanEv)}, new("later")))
	queued(t, w.reply(verbReply(reviewEv, proto.VerbAck), nil))
	q := w.queuedRecords()
	if len(q) != 2 || q[0].Send != state.SendText || q[1].Verb == nil || *q[1].Verb != proto.VerbAck {
		t.Fatalf("queued = %+v", q)
	}
}

// A pending item answers from the store as a ledger one does.
func TestAReplyToAPendingItemNeedsNoFetch(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	s := w.newStore(ops)
	s.Pending = []state.Pending{{ID: evID(reviewEv), Room: proto.PurposeMain, Item: records.KindPing, SentMS: startWall, From: agent(ops, 3), Verb: new("review")}}
	f := w.sendingWith(ops, s)
	posted(t, w.reply(verbReply(reviewEv, proto.VerbAck), nil))
	if f.refetchedIDs() != nil || !strings.Contains(string(lastContent(t, f)), evID(reviewEv)) {
		t.Fatalf("fetched %v", f.refetchedIDs())
	}
}

func (f *fakeTeam) refetchedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.refetched)
}

func (f *fakeTeam) setRefetchErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refetchErr = err
}
