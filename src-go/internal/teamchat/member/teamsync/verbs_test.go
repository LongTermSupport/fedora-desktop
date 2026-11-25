package teamsync

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/bundle"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// LN6: Verify re-fetches an event and judges whose it is, never whom it addresses.
func TestVerify(t *testing.T) {
	w := newWorld(t)
	tm := w.newTeam()
	hid := w.say(alice, human("not for me"))
	got, err := tm.Verify(t.Context(), hid, nil)
	check(t, err)
	sum := sha256.Sum256([]byte("not for me"))
	if got.Item != records.KindHuman || got.From != user(alice) || got.Room != proto.PurposeMain || got.Text != "not for me" ||
		got.BodySHA256 != hex.EncodeToString(sum[:]) || got.ID != hid || got.Team != w.team || got.SentMS == 0 {
		t.Errorf("verified = %+v", got)
	}
	pid := w.agentText(peer, "to coord", user(coord))
	if got, err := tm.Verify(t.Context(), pid, nil); err != nil || got.Item != records.KindPeer || got.Text != "to coord" {
		t.Errorf("peer = %+v, %v", got, err)
	}
	if _, err := tm.Verify(t.Context(), "$"+"nosucheventanywhere_aaaaaaaaaaaaaaaaaaaaaa", nil); !isCode(err, exits.CodeNotFound) {
		t.Errorf("a missing event: %v", err)
	}
	emote := w.say(alice, map[string]any{"msgtype": "m.emote", "body": "waves"})
	if _, err := tm.Verify(t.Context(), emote, nil); !isCode(err, exits.CodeUntrusted) {
		t.Errorf("an emote: %v", err)
	}
}

// R10 (#369, DESIGN-questions-v2 2.8): verify reads the questions room by --room, or after main
// answers 404; the record says whether the sender is a team human and, from the event's own
// relation, the question it names; a human post in main names none (null).
func TestVerifyInTheQuestionsRoom(t *testing.T) {
	w := newWorld(t)
	w.asCoordinator()
	w.inQuestions(coord, false)
	w.inQuestions(alice, false)
	q := w.adminQuestion(3)
	answer := w.sayIn(w.qroom(), alice, inThread("port 8080", q))
	tm := w.newTeam()
	for name, room := range map[string]*string{"--room questions": new(proto.PurposeQuestions), "main first": nil} {
		got, err := tm.Verify(t.Context(), answer, room)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got.Room != proto.PurposeQuestions || got.Item != records.KindHuman || !got.Human || got.Text != "port 8080" {
			t.Errorf("%s: verified %+v", name, got)
		}
		if got.Question == nil || *got.Question != "Q3" || got.Headline == nil || *got.Headline != "Which port does the deploy use?" {
			t.Errorf("%s: question %v headline %v, want Q3 and its headline", name, got.Question, got.Headline)
		}
	}
	if _, err := tm.Verify(t.Context(), answer, new(proto.PurposeMain)); !isCode(err, exits.CodeNotFound) {
		t.Errorf("--room main for a questions post: %v, want not-found", err)
	}
	hid := w.say(alice, human("in main"))
	got, err := tm.Verify(t.Context(), hid, nil)
	check(t, err)
	if !got.Human || got.Question != nil || got.Headline != nil || got.Room != proto.PurposeMain {
		t.Errorf("a main post verified %+v, want human and no question", got)
	}
}

// R10 and A 4.1: a worker the room holds no invite for cannot read it: its verify of a questions
// post is 23 not-found. Invited, as every non-parked member is, its link joins the room at the
// refused read and reads the post (DESIGN-questions-v2 section 4), never syncing the room. The
// refusal is Tuwunel's 404 for an event a non-member cannot see (Plan 00265 R12); the skill's
// `--room questions` reads it as main-first does.
func TestAWorkersVerifyOfAQuestionsPost(t *testing.T) {
	w := newWorld(t)
	w.inQuestions(alice, false)
	q := w.adminQuestion(3)
	answer := w.sayIn(w.qroom(), alice, inThread("port 8080", q))
	w.uninvited(me)
	before := w.joinsOf(w.qroom())
	tm := w.newTeam()
	if _, err := tm.Verify(t.Context(), answer, nil); !isCode(err, exits.CodeNotFound) {
		t.Fatalf("a worker with no invite: %v, want not-found", err)
	}
	w.invited(me)
	got, err := tm.Verify(t.Context(), answer, nil)
	check(t, err)
	if !got.Human || got.Question == nil || *got.Question != "Q3" {
		t.Errorf("an invited worker verified %+v, want the human's post on Q3", got)
	}
	if n := w.joinsOf(w.qroom()) - before; n != 2 {
		t.Errorf("%d joins of questions, want 2 (the refused one and the one that read)", n)
	}
	if got, err := tm.Verify(t.Context(), answer, new(proto.PurposeQuestions)); err != nil || !got.Human || got.Room != proto.PurposeQuestions {
		t.Errorf("--room questions verified %+v, %v; want the human's post", got, err)
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

// LN6: Refetch judges an event again, with no flood or seen of its own; a room it does not know
// is refused.
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
	if _, err := tm.Refetch(t.Context(), proto.PurposeReports, id); !isCode(err, exits.CodeUsage) {
		t.Errorf("reports: %v", err)
	}
	if _, err := tm.Refetch(t.Context(), proto.PurposeMain, "$"+"nosucheventanywhere_aaaaaaaaaaaaaaaaaaaaaaa"); err == nil {
		t.Error("a missing event was judged")
	}
}

// B 2.7.3, QS6: a restored questions item is fetched from the questions room and judged as that
// room's (addressed with no mention, its item in `room: questions`); main's id is not looked for
// there.
func TestRefetchOfAQuestionsItem(t *testing.T) {
	w := newWorld(t)
	w.inQuestions(alice, false)
	w.asCoordinator()
	w.inQuestions(coord, true)
	tm := w.newTeam()
	id := w.sayIn(w.qroom(), alice, human("is the build green?"))
	v, err := tm.Refetch(t.Context(), proto.PurposeQuestions, id)
	check(t, err)
	// Unmentioned, it is delivered only when judged as the questions room's (QS6).
	if v.Outcome != proto.Deliver || v.Event.ID != id || v.Kind != proto.KindHuman {
		t.Errorf("verdict %+v, want the post delivered as the questions room's", v)
	}
	if _, err := tm.Refetch(t.Context(), proto.PurposeMain, id); !errors.Is(err, team.ErrItemGone) {
		t.Errorf("a questions event refetched as main's: %v", err)
	}
}

// project_room_default: the room decisions name main and questions each, and refuse any other
// room rather than give it main's path.
func TestTheRoomDecisionsRefuseAnotherRoom(t *testing.T) {
	w := newWorld(t)
	tm := w.newTeam()
	if m, ok := readRoomMessage(nil, proto.Roster{}, "", proto.PurposeReports); ok {
		t.Errorf("a reports event was read as %+v", m)
	}
	if _, err := tm.getEvent(t.Context(), nil, proto.PurposeReports, "!r", "$e", proto.Roster{}, ""); !isCode(err, exits.CodeUsage) {
		t.Errorf("a reports event was fetched: %v", err)
	}
}

// A restored item the server will never serve again (404, 403, a room other than main) is
// team.ErrItemGone, which the engine drops; a transient failure is not (A1).
func TestARefetchOfAnEventThatIsGoneSaysSo(t *testing.T) {
	w := newWorld(t)
	tm := w.newTeam()
	id := w.say(alice, human("for me", user(me)))
	missing := "$" + "nosucheventanywhere_aaaaaaaaaaaaaaaaaaaaaaa"
	refetch := func(room, id string) error { _, err := tm.Refetch(t.Context(), room, id); return err }
	if err := refetch(proto.PurposeMain, missing); !errors.Is(err, team.ErrItemGone) {
		t.Errorf("404: %v", err)
	}
	if err := refetch("elsewhere", id); !errors.Is(err, team.ErrItemGone) || !isCode(err, exits.CodeUsage) {
		t.Errorf("a room it does not know: %v", err)
	}
	// A worker is not in the questions room: the server refuses the read, and the item is gone.
	if err := refetch(proto.PurposeQuestions, id); !errors.Is(err, team.ErrItemGone) {
		t.Errorf("questions as a worker: %v", err)
	}
	w.srv.Inject(fakehs.Fault{Method: http.MethodGet, Path: "/event/", Status: http.StatusForbidden, Body: []byte(`{"errcode":"M_FORBIDDEN","error":"x"}`)})
	if err := refetch(proto.PurposeMain, id); !errors.Is(err, team.ErrItemGone) {
		t.Errorf("403: %v", err)
	}
	w.srv.Inject(fakehs.Fault{Method: http.MethodGet, Path: "/event/", Status: http.StatusBadGateway, Body: []byte(`{}`), Times: 10})
	if err := refetch(proto.PurposeMain, id); err == nil || errors.Is(err, team.ErrItemGone) {
		t.Errorf("502: %v", err)
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
	if _, err := tm.Verify(t.Context(), id, nil); err != nil {
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
		func() error { _, err := tm.Verify(t.Context(), id, nil); return err }(),
		func() error { _, err := tm.History(t.Context(), proto.PurposeMain, 1); return err }(),
		func() error { _, err := tm.Refetch(t.Context(), proto.PurposeMain, id); return err }(),
		tm.FlushFloor(t.Context()),
	} {
		if !errors.Is(err, bundle.ErrTorn) {
			t.Errorf("with a torn pair: %v", err)
		}
	}
}

// B 2.2.4's exit table: a verb's homeserver failure is its own exit, never internal: no usable
// answer is 20 unreachable, a refused token 21, a forbidden room 22, a rate limit 14.
func TestLN6ServerFailuresAreTheirExits(t *testing.T) {
	limited := []byte(`{"errcode":"M_LIMIT_EXCEEDED","error":"x","retry_after_ms":0}`)
	for name, tc := range map[string]struct {
		fault fakehs.Fault
		code  string
	}{
		"dropped":      {fakehs.Fault{Drop: true, Times: 10}, exits.CodeUnreachable},
		"bad gateway":  {fakehs.Fault{Status: http.StatusBadGateway, Body: []byte(`{}`), Times: 10}, exits.CodeUnreachable},
		"token":        {fakehs.Fault{Status: http.StatusUnauthorized, Body: []byte(`{"errcode":"M_UNKNOWN_TOKEN","error":"x"}`)}, exits.CodeToken},
		"forbidden":    {fakehs.Fault{Status: http.StatusForbidden, Body: []byte(`{"errcode":"M_FORBIDDEN","error":"x"}`)}, exits.CodeForbidden},
		"rate limited": {fakehs.Fault{Status: http.StatusTooManyRequests, Body: limited, Times: 10}, exits.CodeServerRate},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			tm := w.newTeam()
			hid := w.say(alice, human("one"))
			messages, event := tc.fault, tc.fault
			messages.Method, messages.Path = http.MethodGet, "/messages"
			event.Method, event.Path = http.MethodGet, "/event/"
			w.srv.Inject(messages)
			w.srv.Inject(event)
			if _, err := tm.History(t.Context(), proto.PurposeMain, 2); !isCode(err, tc.code) {
				t.Errorf("History: %v", err)
			}
			if _, err := tm.Verify(t.Context(), hid, nil); !isCode(err, tc.code) {
				t.Errorf("Verify: %v", err)
			}
		})
	}
}

func isCode(err error, code string) bool {
	e, ok := errors.AsType[*exits.Error](err)
	return ok && e.Code == code
}
