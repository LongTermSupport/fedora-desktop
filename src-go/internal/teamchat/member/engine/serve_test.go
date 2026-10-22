package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

func TestVerifyResolvesItsTeamAndAsksIt(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops, dev := tid(t, "ops"), tid(t, "dev")
	w.held(ops)
	f := w.held(dev)
	w.persist(dev, humanItem(dev, 5, 5, "x"))
	f.mu.Lock()
	f.verified = records.Verified{Team: dev, ID: evID(5)}
	f.mu.Unlock()
	// The store that knows the event id names the team.
	d, out := w.serve(t.Context(), wire.OpVerify, wire.VerifyArgs{ID: evID(5)})
	if d != (wire.Done{}) || out.kinds() != "verified" || out.flushes != 1 {
		t.Fatalf("verify = %+v %s", d, out.kinds())
	}
	// Two teams and no clue is a usage error.
	if d := w.done(wire.OpVerify, wire.VerifyArgs{ID: evID(6)}); d.Exit != int(exits.ExitUsage) {
		t.Fatalf("verify of an unknown id = %+v", d)
	}
	f.mu.Lock()
	f.verifyErr = context.DeadlineExceeded
	f.mu.Unlock()
	if d := w.done(wire.OpVerify, wire.VerifyArgs{Team: &dev, ID: evID(5)}); d.Error == nil || d.Error.Code != exits.CodeTimedOut {
		t.Fatalf("verify past its deadline = %+v", d)
	}
	f.mu.Lock()
	f.verifyErr = exits.New(exits.CodeNotFound, &dev, "no event {}", exits.Name("x"))
	f.mu.Unlock()
	if d := w.done(wire.OpVerify, wire.VerifyArgs{Team: &dev, ID: evID(5)}); d.Error == nil || d.Error.Code != exits.CodeNotFound {
		t.Fatalf("verify of a missing event = %+v", d)
	}
	f.mu.Lock()
	f.verifyErr = nil
	f.mu.Unlock()
	for name, out := range map[string]*recorder{"record": {err: errors.New("x")}, "flush": {flushErr: errors.New("x")}} {
		raw := []byte(`{"team":"dev","id":"` + evID(5) + `"}`)
		if d := w.e.Serve(t.Context(), wire.Request{Op: wire.OpVerify, Args: raw}, out); d.Exit != 1 {
			t.Fatalf("%s: verify on a closed connection = %+v", name, d)
		}
	}
	other := tid(t, "qa")
	if d := w.done(wire.OpVerify, wire.VerifyArgs{Team: &other, ID: evID(5)}); d.Exit != int(exits.ExitUsage) {
		t.Fatalf("verify --team of a team not held = %+v", d)
	}
}

func TestHistoryReadsTheTeamsRoom(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.held(ops)
	f.mu.Lock()
	f.history = []records.History{
		{Team: ops, Dir: "back", Item: records.Peer{Team: ops, ID: evID(1)}},
		{Team: ops, Dir: "back", Item: records.Peer{Team: ops, ID: evID(2)}},
	}
	f.mu.Unlock()
	d, out := w.serve(t.Context(), wire.OpHistory, wire.HistoryArgs{Room: proto.PurposeMain, Limit: 2})
	if d != (wire.Done{}) || out.kinds() != "history,history" || out.flushes != 1 {
		t.Fatalf("history = %+v %s", d, out.kinds())
	}
	for _, limit := range []int{0, 101} {
		if d := w.done(wire.OpHistory, wire.HistoryArgs{Room: proto.PurposeMain, Limit: limit}); d.Exit != int(exits.ExitUsage) {
			t.Fatalf("history --limit %d = %+v", limit, d)
		}
	}
	for name, out := range map[string]*recorder{"record": {err: errors.New("x")}, "flush": {flushErr: errors.New("x")}} {
		raw := []byte(`{"team":null,"room":"main","limit":5}`)
		if d := w.e.Serve(t.Context(), wire.Request{Op: wire.OpHistory, Args: raw}, out); d.Exit != 1 {
			t.Fatalf("%s: history on a closed connection = %+v", name, d)
		}
	}
	f.mu.Lock()
	f.historyErr = context.Canceled
	f.mu.Unlock()
	if d := w.done(wire.OpHistory, wire.HistoryArgs{Room: proto.PurposeMain, Limit: 5}); d.Error == nil || d.Error.Code != exits.CodeTimedOut {
		t.Fatalf("history past its deadline = %+v", d)
	}
	check(t, w.e.Shutdown(t.Context()))
	if d := w.done(wire.OpHistory, wire.HistoryArgs{Room: proto.PurposeMain, Limit: 5}); d.Exit != int(exits.ExitUsage) {
		t.Fatalf("history with no team = %+v", d)
	}
}

func TestServeRefusesBadArgumentsAndUnbuiltOps(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	for _, op := range []string{wire.OpRecv, wire.OpWait, wire.OpVerify, wire.OpHistory} {
		d := w.e.Serve(t.Context(), wire.Request{Op: op, Args: []byte(`{"nope":1}`)}, &recorder{})
		if d.Error == nil || d.Error.Code != exits.CodeSchema {
			t.Fatalf("%s with bad arguments = %+v", op, d)
		}
	}
	if d := w.e.Serve(t.Context(), wire.Request{Op: wire.OpQuestions}, &recorder{}); d.Error == nil || d.Error.Code != exits.CodeInternal {
		t.Fatalf("an unbuilt op = %+v", d)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if d := w.e.Serve(ctx, wire.Request{Op: wire.OpVerify, Args: []byte(`{"team":null,"id":"x"}`)}, &recorder{}); d.Exit != 1 {
		t.Fatalf("verify on an ended context = %+v", d)
	}
	if d := w.e.Serve(ctx, wire.Request{Op: wire.OpWait, Args: []byte(`{"team":null,"timeout_s":1}`)}, &recorder{}); d != (wire.Done{}) {
		t.Fatalf("wait on an ended context = %+v", d)
	}
	if d := w.e.Serve(ctx, wire.Request{Op: wire.OpInbox, Args: []byte(`{"team":null}`)}, &recorder{}); d.Exit != 1 {
		t.Fatalf("inbox on an ended context = %+v", d)
	}
	if d := w.e.Serve(ctx, wire.Request{Op: wire.OpStatus, Args: []byte(`{"team":null}`)}, &recorder{}); d.Exit != 1 {
		t.Fatalf("status on an ended context = %+v", d)
	}
}

func TestPersistAddsEachItemOnceAppliesRedactionsAndFloors(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.held(ops)
	w.persist(ops, humanItem(ops, 1, 1, "a"), humanItem(ops, 2, 2, "b"))
	d := w.done(wire.OpRecv, wire.TeamsArgs{})
	check(t, w.e.Ack(t.Context(), d.Ack))
	check(t, w.e.Persist(t.Context(), team.Batch{
		Team: ops, NextBatch: "s2", RestoreEpoch: 3, Floors: map[string]int64{proto.PurposeQuestions: 9},
		Items:   []team.Item{humanItem(ops, 1, 1, "again"), humanItem(ops, 3, 3, "c"), humanItem(ops, 4, 4, "d"), humanItem(ops, 4, 4, "d")},
		Redacts: []string{evID(3)},
	}))
	s := w.store(ops)
	if len(s.Pending) != 1 || s.Pending[0].ID != evID(4) || s.Sync.NextBatch != "s2" || s.Sync.RestoreEpoch != 3 ||
		s.Sync.FloorMS[proto.PurposeQuestions] != 9 {
		t.Fatalf("the store = %+v %+v", s.Pending, s.Sync)
	}
	var known bool
	w.local(func() { _, known = w.e.teams[ops].content[evID(3)] })
	if known {
		t.Fatal("a redacted item's content is still in memory")
	}
}

func TestAFullPendingListRefusesTheWholeBatch(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.held(ops)
	batch := make([]team.Item, 0, 1793)
	for i := 1; i <= 1793; i++ {
		it := humanItem(ops, i, int64(i), "")
		it.Pending.Item, it.Verdict.Content.Body = records.KindPeer, "x"
		it.Pending.From = agent(ops, 2)
		batch = append(batch, it)
	}
	err := w.e.Persist(t.Context(), team.Batch{Team: ops, Items: batch, Counts: team.Counts{Ignored: 1}})
	if err == nil {
		t.Fatal("a batch past the cap was persisted")
	}
	if len(w.store(ops).Pending) != 0 {
		t.Fatal("part of a refused batch was kept")
	}
	var counts team.Counts
	w.local(func() { counts = w.e.teams[ops].counts })
	if counts.Ignored != 0 {
		t.Fatal("a refused batch's counts were kept")
	}
}

func testNamer(ops paths.TeamID) namer {
	server := paths.ServerName(ops)
	return namer{server: server, verified: true, roster: proto.Roster{Members: map[string]proto.RosterMember{
		agent(ops, 1): {}, "@repo.1+other.podman:" + server: {}, agent(ops, 3): {},
	}}}
}

func TestTheShortFromIsUniqueInTheVerifiedRoster(t *testing.T) {
	ops := tid(t, "ops")
	server := paths.ServerName(ops)
	n := testNamer(ops)
	for id, want := range map[string]string{
		agent(ops, 3):      "repo.3",
		agent(ops, 1):      "repo.1+site.podman",
		"@admin:" + server: "admin",
		"@x:elsewhere":     "@x:elsewhere",
	} {
		if got := n.name(id); got != want {
			t.Errorf("name(%s) = %q, want %q", id, got, want)
		}
	}
	if got := (namer{server: server}).name(agent(ops, 3)); got != "repo.3+site.podman" {
		t.Fatalf("an unverified roster's agent = %q", got)
	}
}

func TestAPeerRendersItsTextOrBody(t *testing.T) {
	ops := tid(t, "ops")
	p := state.Pending{ID: evID(1), Room: proto.PurposeMain, Item: records.KindPeer, SentMS: 1, From: agent(ops, 3), Restored: true}
	peer := proto.Verdict{Content: proto.Content{Body: "body"}}
	if r, ok := itemRecord(ops, p, peer, testNamer(ops), nil).(records.Peer); !ok || r.Text != "body" || !r.Restored {
		t.Fatalf("peer = %+v", r)
	}
	peer.Content.Text.Text = "text"
	if r, ok := itemRecord(ops, p, peer, testNamer(ops), nil).(records.Peer); !ok || r.Text != "text" {
		t.Fatalf("peer with a text = %+v", r)
	}
}

func TestAPingRendersItsTrustAndAnswer(t *testing.T) {
	ops := tid(t, "ops")
	p := state.Pending{ID: evID(1), Room: proto.PurposeMain, Item: records.KindPing, SentMS: 1, From: agent(ops, 3)}
	ping := proto.Verdict{Content: proto.Content{Ping: proto.Ping{Verb: "review", Ref: "pr:1"}}}
	if r, ok := itemRecord(ops, p, ping, testNamer(ops), nil).(records.Ping); !ok || r.Trust != "-" || r.Re != nil || *r.Ref != "pr:1" {
		t.Fatalf("ping = %+v", r)
	}
	ping.Forge, ping.Answer = &proto.ForgeCheck{}, &proto.Answer{Ping: evID(9)}
	if r, ok := itemRecord(ops, p, ping, testNamer(ops), nil).(records.Ping); !ok || r.Trust != proto.TrustTrusted || *r.Re != evID(9) {
		t.Fatalf("an answer = %+v", r)
	}
}

func TestANoticeRendersWhatItCarries(t *testing.T) {
	ops := tid(t, "ops")
	p := state.Pending{ID: evID(1), Room: proto.PurposeMain, Item: records.KindNotice, SentMS: 1, From: agent(ops, 3)}
	notice := proto.Verdict{Carried: true, Content: proto.Content{
		Notice:  proto.Notice{Event: "left", Subject: agent(ops, 3), Reason: "bye"},
		Carried: proto.Carried{Kind: "ping", Verb: "review", From: "x", Text: "t"},
	}}
	r, ok := itemRecord(ops, p, notice, testNamer(ops), nil).(records.Notice)
	if !ok || r.Subject != "repo.3" || r.Carried == nil || *r.Carried.Verb != "review" {
		t.Fatalf("notice = %+v", r)
	}
}

func TestATimeoutRendersFromItsKeyAndTheOutbox(t *testing.T) {
	ops := tid(t, "ops")
	ref := "pr:2"
	p := state.Pending{ID: evID(7) + "~repo.3+site.podman", Item: records.KindTimeout}
	pings := []state.Ping{{ID: evID(6), Verb: "x"}, {ID: evID(7), Verb: "review", Ref: &ref}}
	if r, ok := itemRecord(ops, p, proto.Verdict{}, testNamer(ops), pings).(records.Timeout); !ok || r.Silent != "repo.3+site.podman" || r.Verb != "review" || r.Ref != &ref {
		t.Fatalf("timeout = %+v", r)
	}
	p.ID = evID(7)
	if r, ok := itemRecord(ops, p, proto.Verdict{}, testNamer(ops), pings).(records.Timeout); !ok || r.Silent != "" {
		t.Fatalf("an unkeyed timeout = %+v", r)
	}
}
