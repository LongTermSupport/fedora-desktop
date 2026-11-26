package deliver_test

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/deliver"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/testfiles"
)

var update = flag.Bool("update", false, "rewrite testdata/ from the Go cases")

func teamID(s string) paths.TeamID {
	t, err := paths.ParseTeamID(s)
	if err != nil {
		panic(err)
	}
	return t
}

var (
	team       = teamID("myteam")
	teamA      = teamID("ateam")
	teamB      = teamID("bteam")
	reviewVerb = "review"
)

const (
	self   = "myrepo.3+desk.lxc"
	nonce  = "3f9c2a71"
	sentMS = int64(1_792_587_731_000) // 2026-10-21T13:02:11Z
	alice  = "@alice:example.org"
	bob    = "@bob:example.org"
	coord  = "@myrepo.1+desk.podman:example.org"
	worker = "@myrepo.2+desk.podman:example.org"
	probe  = "@probe.1+host.podman:example.org"
	admin  = "@admin:example.org"
)

func id(c byte) string { return "$" + strings.Repeat(string(c), 43) }

// names is the verified roster's view of each sender: a human by its name with no role, an
// agent by its handle and role, the admin as `admin`.
func names(userID string) (string, string) {
	switch userID {
	case alice:
		return "alice", ""
	case bob:
		return "bob", ""
	case coord, proto.UserID("myrepo.1+desk.podman", paths.ServerName(team)):
		// The coordinator, also as a restored relayed item names it: on the team's server.
		return "myrepo.1+desk.podman", proto.RoleCoordinator
	case worker:
		return "myrepo.2+desk.podman", proto.RoleWorker
	case probe:
		return "probe.1+host.podman", proto.RoleProbe
	case admin:
		return proto.AdminLocalpart, proto.RoleAdmin
	}
	return "Not A Name", ""
}

func fixedNonce() (string, error) { return nonce, nil }

func pending(c byte, kind, from string, at int64) state.Pending {
	return state.Pending{ID: id(c), Room: proto.PurposeMain, Item: kind, SentMS: at, From: from}
}

func human(c byte, at int64, body string) deliver.Item {
	return deliver.Item{Pending: pending(c, "human", alice, at), Verdict: &proto.Verdict{Human: proto.Human{Body: body}}}
}

func peer(c byte, at int64, body string) deliver.Item {
	return deliver.Item{
		Pending: pending(c, "peer", worker, at),
		Verdict: &proto.Verdict{Content: proto.Content{Text: proto.Text{To: []string{coord}, Text: body}}},
	}
}

func ping(c byte, at int64) deliver.Item {
	return deliver.Item{
		Pending: pending(c, "ping", coord, at),
		Verdict: &proto.Verdict{Content: proto.Content{Ping: proto.Ping{Verb: proto.VerbAck, To: []string{worker}}}},
	}
}

func timeout(c byte) deliver.Item {
	p := pending(c, "timeout", "", 0)
	p.ID = proto.TimeoutKey(id(c), "myrepo.2+desk.podman")
	return deliver.Item{Pending: p, Timeout: &deliver.Timeout{Ping: state.Ping{ID: id(c), Verb: proto.VerbAck}, Silent: "myrepo.2+desk.podman"}}
}

func notice(c byte, at int64) deliver.Item {
	return deliver.Item{
		Pending: pending(c, "notice", worker, at),
		Verdict: &proto.Verdict{Content: proto.Content{Notice: proto.Notice{Event: proto.EventLeft, Subject: worker, Reason: "exit"}}},
	}
}

func build(t *testing.T, items []deliver.Item, f deliver.Facts) deliver.Built {
	t.Helper()
	b, err := deliver.Build(team, self, items, f, names, fixedNonce)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func pin(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".txt")
	if *update {
		if err := os.WriteFile(path, []byte(got+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, found := testfiles.Find(t, "testdata", name+".txt")
	if !found {
		t.Fatalf("%s is missing (go test -update writes it)", path)
	}
	if !bytes.Equal([]byte(got+"\n"), want) {
		t.Errorf("%s differs (go test -update rewrites it):\n got %s\nwant %s", path, got, want)
	}
}

// TestMappingGoldens pins DV2's mapping: one delivery of each kind, rendered whole.
func TestMappingGoldens(t *testing.T) {
	commit := "commit:example-org/myrepo@0123456789abcdef0123456789abcdef01234567"
	parsed, ok := proto.ParseRef(commit)
	if !ok {
		t.Fatal("the ref does not parse")
	}
	questions := human('h', sentMS, "Which licence do we use?")
	questions.Pending.Room = proto.PurposeQuestions
	questions.Question = &proto.EntryQuestion{N: 7, Headline: "Which licence?"}
	questions.Verdict.Human.ReplyTo = id('r')
	questions.Verdict.Human.Revises = id('o')
	copied := peer('e', sentMS, "pushed")
	copied.Verdict.Content.Text.To = []string{alice, bob, coord}
	copied.Verdict.Content.Relation = proto.ReplyTo(id('r'))
	copied.Pending.Repeat = true
	pinged := ping('p', sentMS)
	pinged.Verdict.Content.Ping = proto.Ping{Verb: reviewVerb, To: []string{worker}, Ref: commit, Branch: "feat/x"}
	pinged.Verdict.Forge = &proto.ForgeCheck{Ref: parsed, Class: proto.RefListed}
	answer := ping('a', sentMS)
	answer.Verdict.Content.Ping = proto.Ping{Verb: proto.VerbDone, To: []string{worker}}
	answer.Verdict.Answer = &proto.Answer{Ping: id('q'), Verb: proto.VerbDone}
	check := deliver.Item{
		Pending: pending('k', "ping", probe, sentMS),
		Verdict: &proto.Verdict{Content: proto.Content{Ping: proto.Ping{Verb: proto.VerbCheck, To: []string{worker}}}},
	}
	added := deliver.Item{
		Pending: pending('n', "notice", admin, sentMS),
		Verdict: &proto.Verdict{Content: proto.Content{Notice: proto.Notice{Event: proto.EventAdded, Subject: worker, Role: proto.RoleWorker}}},
	}
	moved := deliver.Item{
		Pending: pending('m', "notice", admin, sentMS),
		Verdict: &proto.Verdict{Content: proto.Content{Notice: proto.Notice{Event: proto.EventMoved}}},
	}
	carriedPing := deliver.Item{
		Pending: pending('c', "notice", admin, sentMS),
		Verdict: &proto.Verdict{Carried: true, Content: proto.Content{Carried: proto.Carried{
			Kind: proto.CarriedPing, Verb: reviewVerb, Ref: "issue:example-org/myrepo#12", From: "myrepo.1+desk.podman",
			SentMs: sentMS - 60_000, Text: "please review the branch", PossibleDuplicate: true,
		}}},
	}
	carriedText := deliver.Item{
		Pending: pending('d', "notice", admin, sentMS),
		Verdict: &proto.Verdict{Carried: true, Content: proto.Content{Carried: proto.Carried{
			Kind: "text", From: "alice", SentMs: -1, Text: "moved over",
		}}},
	}
	withRef := timeout('t')
	withRef.Timeout.Ping.Ref = new(commit)
	reminder := deliver.Item{
		Pending:  state.Pending{ID: proto.ReminderKey(id('q')), Room: proto.PurposeQuestions, Item: "timeout"},
		Timeout:  &deliver.Timeout{Silent: proto.HumansWord},
		Question: &proto.EntryQuestion{N: 4, Headline: "Which licence?"},
	}
	restored := human('h', sentMS, "queued before the restart")
	restored.Pending.Restored = true
	restored.Pending.Repeat = true
	// A relayed post (A 10.6): its question is the relay's claim, and the coordinator is named.
	relayed := human('h', sentMS, "Use port 8080.")
	relayed.Pending.Room = proto.PurposeQuestions
	relayed.Verdict.Relayed = &proto.Relayed{By: coord, Q: 4}
	// Restored, its verdict is the post fetched again by ID, with no relay account: the store's
	// item gives back its question and its relayed-by line.
	relayedRestored := human('h', sentMS, "Use port 8080.")
	relayedRestored.Pending.Room, relayedRestored.Pending.Restored = proto.PurposeQuestions, true
	relayedRestored.Pending.RelayedBy, relayedRestored.Pending.Q = "myrepo.1+desk.podman", 4
	// Judged on replay past its age limit, an item is delivered marked late (Plan 00267 Task 3.2).
	late := human('h', sentMS, "are you still on the refactor?")
	late.Pending.Late = true
	peerBody := peer('e', sentMS, "")
	peerBody.Verdict.Content.Body = "the body, when the text is empty"
	for name, item := range map[string]deliver.Item{
		"human":           human('h', sentMS, "Please stop the refactor, commit what you have, and push the branch."),
		"human-questions": questions, "peer-copy": copied, "peer-body": peerBody, "ping": pinged, "ping-answer": answer,
		"ping-check": check, "notice-left": notice('n', sentMS), "notice-added": added, "notice-moved": moved,
		"carried-ping": carriedPing, "carried-text": carriedText, "timeout": timeout('t'), "timeout-ref": withRef,
		"timeout-reminder": reminder, "restored": restored, "human-relayed": relayed,
		"human-relayed-restored": relayedRestored, "late": late,
	} {
		t.Run(name, func(t *testing.T) {
			b := build(t, []deliver.Item{item}, deliver.Facts{})
			if len(b.IDs) != 1 || b.IDs[0] != item.Pending.ID || len(b.Failed) != 0 || len(b.Left) != 0 {
				t.Fatalf("built %+v", b)
			}
			pin(t, name, b.Text)
			if b.Delivery.Nonce != nonce || b.Delivery.Team != team.String() {
				t.Fatalf("delivery %+v", b.Delivery)
			}
		})
	}
}

func TestTrailingFactsGolden(t *testing.T) {
	b := build(t, []deliver.Item{peer('e', sentMS, "pushed")}, deliver.Facts{
		Answered: []proto.Answered{{From: "myrepo.2+desk.podman", Answer: proto.VerbDone, Verb: reviewVerb, PingID: id('p')}},
		HeldBack: []proto.HeldBack{{From: "myrepo.1+desk.podman", Count: 12}},
		Gap:      true, Stopped: true,
	})
	pin(t, "trailing", b.Text)
}

func TestATrailingOnlyDelivery(t *testing.T) {
	b := build(t, nil, deliver.Facts{Gap: true})
	if len(b.IDs) != 0 || !strings.Contains(b.Text, "0 messages on myteam") || !strings.Contains(b.Text, "\ngap: ") {
		t.Fatalf("built %+v", b)
	}
}

func TestNothingToSendDrawsNoNonce(t *testing.T) {
	b, err := deliver.Build(team, self, nil, deliver.Facts{}, names, func() (string, error) {
		t.Fatal("a nonce was drawn for nothing")
		return "", nil
	})
	if err != nil || b.Text != "" {
		t.Fatalf("built %+v, %v", b, err)
	}
}

func TestAFailedHumanSlotIsNotShownAndDropped(t *testing.T) {
	bad := human('h', sentMS, "hello")
	bad.Pending.From = "@x:example.org" // the namer gives a name that fails `<human>`
	b := build(t, []deliver.Item{bad, peer('e', sentMS+1, "ok")}, deliver.Facts{})
	if !slices.Equal(b.Failed, []string{id('h')}) || !slices.Equal(b.IDs, []string{id('e')}) {
		t.Fatalf("built %+v", b)
	}
	if !strings.Contains(b.Text, "not shown: a message from a team human could not be shown; read it with teamchat verify '"+id('h')+"'") {
		t.Fatalf("no not-shown line:\n%s", b.Text)
	}
	pin(t, "not-shown", b.Text)
}

func TestAFailedAgentSlotIsDroppedWithoutALine(t *testing.T) {
	bad := peer('e', sentMS, "x")
	bad.Pending.From = "@x:example.org"
	unknown := peer('u', sentMS, "x")
	unknown.Pending.Item = "fact"
	b := build(t, []deliver.Item{bad, unknown}, deliver.Facts{})
	if !slices.Equal(b.Failed, []string{id('e'), id('u')}) || b.Text != "" {
		t.Fatalf("built %+v", b)
	}
}

func TestARestoredItemNotYetFetchedAgainIsLeft(t *testing.T) {
	waiting := deliver.Item{Pending: pending('h', "human", alice, sentMS)}
	waiting.Pending.Restored = true
	b := build(t, []deliver.Item{waiting, peer('e', sentMS+1, "ok")}, deliver.Facts{})
	if !slices.Equal(b.Left, []string{id('h')}) || !slices.Equal(b.IDs, []string{id('e')}) || b.Delivery.Pending != 1 {
		t.Fatalf("built %+v", b)
	}
}

func TestEntriesFollowTheDeliveryOrder(t *testing.T) {
	items := []deliver.Item{
		notice('n', sentMS-50),
		timeout('t'),
		peer('e', sentMS+2, "later"),
		ping('p', sentMS+1),
		human('h', sentMS+3, "behind the agents"),
		human('i', sentMS, "first"),
		timeout('u'),
	}
	b := build(t, items, deliver.Facts{})
	want := []string{id('i'), id('h'), id('p'), id('e'), items[1].Pending.ID, items[6].Pending.ID, id('n')}
	if !slices.Equal(b.IDs, want) {
		t.Fatalf("ids %v, want %v", b.IDs, want)
	}
}

// memberNotice is a notice from worker about itself.
func memberNotice(c byte, at int64, event, reason string) deliver.Item {
	it := notice(c, at)
	it.Verdict.Content.Notice = proto.Notice{Event: event, Subject: worker, Reason: reason}
	return it
}

// Plan 00267 Task 7.3: notices from one sender about one member pending together fold into one
// entry, the latest, its earlier ones on `earlier:` lines; every folded item is in the text. A
// notice from another sender about the same member, and a carried item, stay entries of their own.
func TestNoticesAboutOneMemberFoldIntoOneEntry(t *testing.T) {
	added := deliver.Item{
		Pending: pending('m', "notice", admin, sentMS+1_000),
		Verdict: &proto.Verdict{Content: proto.Content{Notice: proto.Notice{Event: proto.EventRole, Subject: worker, Role: proto.RoleWorker}}},
	}
	items := []deliver.Item{
		memberNotice('o', sentMS+5_000, proto.EventBack, ""),
		peer('e', sentMS+9_000, "after the notices"),
		memberNotice('n', sentMS, proto.EventLeft, "exit"),
		added,
		memberNotice('q', sentMS+2_000, proto.EventDisconnected, ""),
	}
	b := build(t, items, deliver.Facts{})
	want := []string{id('e'), id('m'), id('n'), id('q'), id('o')}
	if !slices.Equal(b.IDs, want) || len(b.Failed) != 0 || len(b.Left) != 0 {
		t.Fatalf("built ids %v failed %v left %v, want ids %v", b.IDs, b.Failed, b.Left, want)
	}
	if len(b.Delivery.Entries) != 3 {
		t.Fatalf("%d entries, want 3 (the peer, the admin's, the member's folded):\n%s", len(b.Delivery.Entries), b.Text)
	}
	pin(t, "notice-folded", b.Text)
	// A restored notice and a live one stay apart: the restored line is the entry's own.
	restored := memberNotice('r', sentMS+3_000, proto.EventBack, "")
	restored.Pending.Restored = true
	b = build(t, []deliver.Item{memberNotice('n', sentMS, proto.EventLeft, "exit"), restored}, deliver.Facts{})
	if len(b.Delivery.Entries) != 2 {
		t.Errorf("a restored notice was folded into a live one:\n%s", b.Text)
	}
}

// Plan 00267 Task 7.3: one entry folds at most deliver.MaxFold notices, so a member that flaps
// cannot make one entry unbounded; the rest fold into the next.
func TestAFoldIsBounded(t *testing.T) {
	var items []deliver.Item
	for i := range deliver.MaxFold + 2 {
		event, reason := proto.EventLeft, "exit"
		if i%2 == 1 {
			event, reason = proto.EventBack, ""
		}
		items = append(items, memberNotice(byte('a'+i), sentMS+int64(i)*1_000, event, reason))
	}
	b := build(t, items, deliver.Facts{})
	if len(b.Delivery.Entries) != 2 || len(b.IDs) != len(items) {
		t.Fatalf("%d entries for %d ids:\n%s", len(b.Delivery.Entries), len(b.IDs), b.Text)
	}
	if got := len(b.Delivery.Entries[0].Earlier); got != deliver.MaxFold-1 {
		t.Errorf("the first fold holds %d earlier notices, want %d", got, deliver.MaxFold-1)
	}
}

// TestNoCountOfEntriesLimitsADelivery is Plan 00267 Task 7.6: nine small items go in one
// delivery, and of many more the most that fit the bytes go, the rest counted pending.
func TestNoCountOfEntriesLimitsADelivery(t *testing.T) {
	const ids = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	items := make([]deliver.Item, 0, len(ids))
	for i := range len(ids) {
		items = append(items, peer(ids[i], sentMS+int64(i), "n"))
	}
	b := build(t, items[:9], deliver.Facts{})
	if len(b.IDs) != 9 || len(b.Left) != 0 || strings.Contains(b.Text, "more pending") {
		t.Fatalf("nine: ids %d left %v\n%s", len(b.IDs), b.Left, b.Text)
	}
	b = build(t, items, deliver.Facts{})
	n := len(b.IDs)
	if n <= 9 || n+len(b.Left) != len(items) || !slices.Equal(b.Left, idsOf(items[n:])) ||
		!strings.HasSuffix(b.Text, fmt.Sprintf("\n%d more pending: they follow in the next delivery", len(b.Left))) ||
		proto.WireBytes(b.Text) > proto.MaxDeliveryBytes {
		t.Fatalf("%d items: ids %d left %d, %d bytes", len(items), n, len(b.Left), proto.WireBytes(b.Text))
	}
}

func idsOf(items []deliver.Item) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Pending.ID)
	}
	return out
}

// TestTwelveKiBExactlyAndOneByteOver finds the last body that brings the user line's text to
// exactly the cap, and shows one byte more leaves that entry for the next delivery.
func TestTwelveKiBExactlyAndOneByteOver(t *testing.T) {
	items := func(n int) []deliver.Item {
		var out []deliver.Item
		for i := range 5 {
			out = append(out, peer(byte('a'+i), sentMS+int64(i), strings.Repeat("x", 1800)))
		}
		return append(out, peer('f', sentMS+5, strings.Repeat("y", n)))
	}
	for n := 1; n <= 2000; n++ {
		b := build(t, items(n), deliver.Facts{})
		if len(b.IDs) < 6 {
			t.Fatalf("no body length reaches exactly %d bytes", proto.MaxDeliveryBytes)
		}
		if proto.WireBytes(b.Text) != proto.MaxDeliveryBytes {
			continue
		}
		over := build(t, items(n+1), deliver.Facts{})
		if len(over.IDs) != 5 || !slices.Equal(over.Left, []string{id('f')}) || proto.WireBytes(over.Text) > proto.MaxDeliveryBytes {
			t.Fatalf("one byte over: ids %d left %v, %d bytes", len(over.IDs), over.Left, proto.WireBytes(over.Text))
		}
		return
	}
	t.Fatal("never reached the cap")
}

func TestASingleItemOverTheByteCapIsSentAlone(t *testing.T) {
	big := human('h', sentMS, strings.Repeat("é", 20_000))
	f := deliver.Facts{Gap: true, Stopped: true}
	for i := range 20 {
		f.HeldBack = append(f.HeldBack, proto.HeldBack{From: fmt.Sprintf("myrepo.%d+desk.podman", i+1), Count: 1})
	}
	// Each body is cut at 2048 bytes, which is six times that on the ASCII-escaped wire.
	b := build(t, []deliver.Item{big, human('i', sentMS+1, strings.Repeat("é", 20_000))}, f)
	if !slices.Equal(b.IDs, []string{id('h')}) || !slices.Equal(b.Left, []string{id('i')}) || !strings.Contains(b.Text, "\ntruncated: ") || proto.WireBytes(b.Text) > proto.MaxDeliveryBytes {
		t.Fatalf("ids %v, %d bytes", b.IDs, proto.WireBytes(b.Text))
	}
}

func TestBuildFailsFast(t *testing.T) {
	if _, err := deliver.Build(team, self, []deliver.Item{peer('e', sentMS, "x")}, deliver.Facts{}, names, func() (string, error) {
		return "", errors.New("no entropy")
	}); err == nil || !strings.Contains(err.Error(), "no entropy") {
		t.Fatalf("nonce failure: %v", err)
	}
	bad := deliver.Facts{Answered: []proto.Answered{{From: "Not A Name", Answer: "ack", Verb: "ack", PingID: id('p')}}}
	if _, err := deliver.Build(team, self, nil, bad, names, fixedNonce); err == nil {
		t.Fatal("a bad answered fact rendered")
	}
	if _, err := deliver.Build(team, self, nil, deliver.Facts{Gap: true}, names, func() (string, error) { return "XYZ", nil }); err == nil {
		t.Fatal("a bad nonce rendered")
	}
}

func TestChoose(t *testing.T) {
	agent := pending('a', "peer", worker, 10)
	humanLater := pending('h', "human", alice, 20)
	handedHuman := pending('x', "human", alice, 1)
	handed := "n"
	handedHuman.Handed = &handed
	heldHuman := pending('y', "human", alice, 2)
	heldHuman.Held = true
	cases := []struct {
		name  string
		teams map[paths.TeamID][]state.Pending
		want  paths.TeamID
		ok    bool
	}{
		{"none", map[paths.TeamID][]state.Pending{teamA: nil}, paths.TeamID{}, false},
		{"humans first", map[paths.TeamID][]state.Pending{teamA: {agent}, teamB: {humanLater}}, teamB, true},
		{"else the oldest", map[paths.TeamID][]state.Pending{teamA: {pending('a', "peer", worker, 30)}, teamB: {pending('b', "ping", coord, 5)}}, teamB, true},
		{"ties by team id", map[paths.TeamID][]state.Pending{teamB: {humanLater}, teamA: {humanLater}}, teamA, true},
		{"handed and held skipped", map[paths.TeamID][]state.Pending{teamA: {handedHuman, heldHuman, agent}, teamB: {humanLater}}, teamB, true},
		{"only handed", map[paths.TeamID][]state.Pending{teamA: {handedHuman, heldHuman}}, paths.TeamID{}, false},
	}
	for _, c := range cases {
		got, ok := deliver.Choose(c.teams)
		if got != c.want || ok != c.ok {
			t.Fatalf("%s: got %q %v, want %q %v", c.name, got, ok, c.want, c.ok)
		}
	}
}
