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
	case coord:
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
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (go test -update writes it)", err)
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
	peerBody := peer('e', sentMS, "")
	peerBody.Verdict.Content.Body = "the body, when the text is empty"
	for name, item := range map[string]deliver.Item{
		"human":           human('h', sentMS, "Please stop the refactor, commit what you have, and push the branch."),
		"human-questions": questions, "peer-copy": copied, "peer-body": peerBody, "ping": pinged, "ping-answer": answer,
		"ping-check": check, "notice-left": notice('n', sentMS), "notice-added": added, "notice-moved": moved,
		"carried-ping": carriedPing, "carried-text": carriedText, "timeout": timeout('t'), "timeout-ref": withRef,
		"timeout-reminder": reminder, "restored": restored,
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

func TestEightEntriesAndANinth(t *testing.T) {
	items := make([]deliver.Item, 0, 9)
	for i := range 9 {
		items = append(items, peer(byte('a'+i), sentMS+int64(i), "n"))
	}
	b := build(t, items[:8], deliver.Facts{})
	if len(b.IDs) != 8 || len(b.Left) != 0 || strings.Contains(b.Text, "more pending") {
		t.Fatalf("eight: %+v", b)
	}
	b = build(t, items, deliver.Facts{})
	if len(b.IDs) != 8 || !slices.Equal(b.Left, []string{id('i')}) || !strings.HasSuffix(b.Text, "\n1 more pending: they follow in the next delivery") {
		t.Fatalf("nine: ids %d left %v\n%s", len(b.IDs), b.Left, b.Text)
	}
}

// TestTwelveKiBExactlyAndOneByteOver finds the last body that brings the user line's text to
// exactly the cap, and shows one byte more leaves that entry for the next delivery.
func TestTwelveKiBExactlyAndOneByteOver(t *testing.T) {
	items := func(n int) []deliver.Item {
		var out []deliver.Item
		for i := range 5 {
			out = append(out, peer(byte('a'+i), sentMS+int64(i), strings.Repeat("x", 1900)))
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
