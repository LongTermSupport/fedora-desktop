package teamsync

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/bundle"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// LN6: Verify re-fetches an event and judges whose it is, never whom it addresses.
func TestVerify(t *testing.T) {
	w := newWorld(t)
	tm := w.newTeam()
	hid := w.say(alice, human("not for me"))
	got, err := tm.Verify(t.Context(), hid)
	check(t, err)
	sum := sha256.Sum256([]byte("not for me"))
	if got.Item != records.KindHuman || got.From != user(alice) || got.Room != proto.PurposeMain || got.Text != "not for me" ||
		got.BodySHA256 != hex.EncodeToString(sum[:]) || got.ID != hid || got.Team != w.team || got.SentMS == 0 {
		t.Errorf("verified = %+v", got)
	}
	pid := w.agentText(peer, "to coord", user(coord))
	if got, err := tm.Verify(t.Context(), pid); err != nil || got.Item != records.KindPeer || got.Text != "to coord" {
		t.Errorf("peer = %+v, %v", got, err)
	}
	if _, err := tm.Verify(t.Context(), "$"+"nosucheventanywhere_aaaaaaaaaaaaaaaaaaaaaa"); !isCode(err, exits.CodeNotFound) {
		t.Errorf("a missing event: %v", err)
	}
	emote := w.say(alice, map[string]any{"msgtype": "m.emote", "body": "waves"})
	if _, err := tm.Verify(t.Context(), emote); !isCode(err, exits.CodeUntrusted) {
		t.Errorf("an emote: %v", err)
	}
}

// LN6: History reads main backwards and gives it oldest first, in and out.
func TestHistory(t *testing.T) {
	w := newWorld(t)
	tm := w.newTeam()
	w.say(alice, human("one"))
	w.agentText(me, "two", user(coord))
	got, err := tm.History(t.Context(), proto.PurposeMain, 2)
	check(t, err)
	if len(got) != 2 {
		t.Fatalf("history = %+v", got)
	}
	if h, ok := got[0].Item.(records.Human); !ok || got[0].Dir != "in" || h.Text != "one" || h.FromID != user(alice) {
		t.Errorf("first = %+v", got[0])
	}
	if p, ok := got[1].Item.(records.Peer); !ok || got[1].Dir != "out" || p.Text != "two" {
		t.Errorf("second = %+v", got[1])
	}
}

// LN6: History gives a ping (its reference never trusted) and a notice; a room this member does
// not read is a usage error, and the questions room is the roster's.
func TestHistoryOfPingsNoticesAndRooms(t *testing.T) {
	w := newWorld(t)
	tm := w.newTeam()
	out, err := w.composer(peer).Ping(proto.Ping{Verb: proto.VerbDone, To: []string{user(coord)}, Ref: "commit:example-org/myrepo@0123456789abcdef0123456789abcdef01234567"}, "", "")
	w.post(peer, out, err)
	out, err = w.composer(me).Notice(proto.Notice{Event: proto.EventLeft, Subject: user(me), To: []string{user(coord)}, Reason: proto.LeftReasons[0]}, "")
	w.post(me, out, err)
	w.say(peer, human("not teamchat"))
	// A limit under 1 is 1: the newest event, which does not read as its sender's.
	got, err := tm.History(t.Context(), proto.PurposeMain, 0)
	check(t, err)
	if len(got) != 0 {
		t.Fatalf("history %+v", got)
	}
	got, err = tm.History(t.Context(), proto.PurposeMain, 3)
	check(t, err)
	if len(got) != 2 {
		t.Fatalf("history %+v", got)
	}
	if p, ok := got[0].Item.(records.Ping); !ok || p.Trust != proto.TrustExternal || p.Ref == nil || p.From != peer || got[0].Dir != "in" {
		t.Errorf("ping %+v", got[0].Item)
	}
	if n, ok := got[1].Item.(records.Notice); !ok || n.Event != proto.EventLeft || n.Reason == nil || got[1].Dir != "out" {
		t.Errorf("notice %+v", got[1].Item)
	}
	if _, err := tm.History(t.Context(), proto.PurposeReports, 1); !isCode(err, exits.CodeUsage) {
		t.Errorf("reports: %v", err)
	}
	if _, err := tm.History(t.Context(), proto.PurposeQuestions, 1); err == nil {
		t.Error("the questions room was read by a member not in it")
	}
}

// LN6: Refetch judges an event again, with no flood or seen of its own; only main is read.
func TestRefetch(t *testing.T) {
	w := newWorld(t)
	tm := w.newTeam()
	root := w.agentText(peer, "peer's say", user(coord))
	id := w.say(alice, reply("for me", root, user(me)))
	for range 2 {
		v, err := tm.Refetch(t.Context(), proto.PurposeMain, id)
		check(t, err)
		if v.Outcome != proto.Deliver || v.Event.ID != id {
			t.Errorf("verdict %+v", v)
		}
	}
	if _, err := tm.Refetch(t.Context(), proto.PurposeQuestions, id); !isCode(err, exits.CodeUsage) {
		t.Errorf("questions: %v", err)
	}
	if _, err := tm.Refetch(t.Context(), proto.PurposeMain, "$"+"nosucheventanywhere_aaaaaaaaaaaaaaaaaaaaaaa"); err == nil {
		t.Error("a missing event was judged")
	}
}

// The ends' calls work before Run, on the pair as it reads; with no pair they fail, and the
// anchors and token are empty.
func TestNotices(t *testing.T) {
	w := newWorld(t)
	tm := w.newTeam()
	n := tm.Notices()
	if _, ok := n.Roster(); ok {
		t.Error("a roster before any verification")
	}
	if n.Token() != w.tokens[me] {
		t.Error("the token is not the pair's")
	}
	state, err := n.RoomState(t.Context(), w.main)
	if err != nil || len(state) == 0 {
		t.Errorf("room state %d, %v", len(state), err)
	}
	out, err := w.composer(me).Text(proto.Text{To: []string{user(coord)}, Text: "via notices"}, "", "via notices")
	check(t, err)
	id, err := n.Send(t.Context(), w.main, proto.MessageType, "", out.Content.Compact())
	check(t, err)
	page, err := n.Messages(t.Context(), w.main, matrix.MessagesRequest{Dir: matrix.Backward, Limit: 1})
	if err != nil || len(page.Chunk) != 1 || !strings.Contains(string(page.Chunk[0]), id) {
		t.Errorf("messages %+v, %v", page, err)
	}
	check(t, n.SetPresence(t.Context(), user(me), "online"))
	if _, err := tm.Verify(t.Context(), id); err != nil {
		t.Errorf("verify %v", err)
	}
	if _, ok := n.Roster(); !ok {
		t.Error("no roster after a verification")
	}

	tm.readPair = func(*homefs.Root, paths.Seat) (bundle.Pair, error) { return bundle.Pair{}, bundle.ErrTorn }
	if n.Token() != "" || n.Anchors() != (proto.Anchors{}) {
		t.Error("a torn pair gave a token or anchors")
	}
	for _, err := range []error{
		func() error { _, err := n.RoomState(t.Context(), w.main); return err }(),
		func() error { _, err := n.Messages(t.Context(), w.main, matrix.MessagesRequest{}); return err }(),
		func() error { _, err := n.Send(t.Context(), w.main, proto.MessageType, "", nil); return err }(),
		n.SetPresence(t.Context(), user(me), "online"),
		func() error { _, err := tm.Verify(t.Context(), id); return err }(),
		func() error { _, err := tm.History(t.Context(), proto.PurposeMain, 1); return err }(),
		func() error { _, err := tm.Refetch(t.Context(), proto.PurposeMain, id); return err }(),
		tm.FlushFloor(t.Context()),
	} {
		if !errors.Is(err, bundle.ErrTorn) {
			t.Errorf("with a torn pair: %v", err)
		}
	}
}

func isCode(err error, code string) bool {
	e, ok := errors.AsType[*exits.Error](err)
	return ok && e.Code == code
}
