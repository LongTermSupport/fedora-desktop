package teamsync

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// joinOf is the path substring that selects the join of the reports room.
func (w *world) joinOf() string { return url.PathEscape(w.ids.Reports) + "/join" }

// reportsJoins are the joins of the reports room fakehs received.
func (w *world) reportsJoins() int {
	n := 0
	for _, r := range w.srv.Requests() {
		if r.Method == http.MethodPost && (strings.Contains(r.Path, w.joinOf()) || strings.Contains(r.Path, w.ids.Reports+"/join")) {
			n++
		}
	}
	return n
}

// joinsExceptReports are the joins fakehs received for any room but the reports room, which the
// team's eager join (RP1) makes at a time of its own.
func (w *world) joinsExceptReports() int { return w.requests("/join") - w.reportsJoins() }

// watchReports is the stream of states the team put the reports room in.
func watchReports(tm *Team) chan team.ReportsRoom {
	ch := make(chan team.ReportsRoom, 32)
	tm.onReports = func(rr team.ReportsRoom) { ch <- rr }
	return ch
}

// nextReports is the next state the reports room was put in.
func nextReports(t *testing.T, ch <-chan team.ReportsRoom) team.ReportsRoom {
	t.Helper()
	select {
	case rr := <-ch:
		return rr
	case <-t.Context().Done():
		t.Fatal("the reports room's state did not change")
	}
	return team.ReportsRoom{}
}

// forbid answers the next join of the reports room 403, times times.
func (w *world) forbidJoin(times int) {
	w.srv.Inject(fakehs.Fault{
		Method: http.MethodPost, Path: w.joinOf(), Status: http.StatusForbidden,
		Body: []byte(`{"errcode":"M_FORBIDDEN","error":"x"}`), Times: times,
	})
}

// RP1, n3f-D12: the reports room is joined at the team's start, by the restricted rule, while
// the team goes on: main's long-poll is pending and its items are delivered.
func TestTheReportsRoomIsJoinedAtTheStart(t *testing.T) {
	w := newWorld(t)
	tm := w.newTeam()
	rooms := watchReports(tm)
	r := w.startTeam(tm)
	r.poll(t)
	w.batch()
	if rr := nextReports(t, rooms); rr != (team.ReportsRoom{State: team.ReportsJoined}) {
		t.Fatalf("the reports room is %+v, want joined", rr)
	}
	if n := w.reportsJoins(); n != 1 {
		t.Errorf("%d joins of the reports room, want 1", n)
	}
	if got := tm.ReportsRoom(); got.State != team.ReportsJoined {
		t.Errorf("ReportsRoom() = %+v", got)
	}
	w.agentText(peer, "after the join", user(me))
	if b := w.batch(); len(b.Items) != 1 {
		t.Errorf("main's item was not delivered: %+v", b)
	}
}

// RP1: before its join is answered the room is joining, never joined.
func TestTheReportsRoomIsJoiningBeforeItsJoinIsAnswered(t *testing.T) {
	w := newWorld(t)
	tm := w.newTeam()
	if got := tm.ReportsRoom(); got != (team.ReportsRoom{State: team.ReportsJoining}) {
		t.Errorf("a team not yet run shows %+v, want joining", got)
	}
}

// RP1: a member whose reports join is refused delivers main's items unchanged, and its room
// is shown refused with the errcode; the join is not retried by a timer.
func TestARefusedReportsJoinIsShownAndMainGoesOn(t *testing.T) {
	w := newWorld(t)
	w.forbidJoin(1)
	tm := w.newTeam()
	rooms := watchReports(tm)
	r := w.startTeam(tm)
	r.poll(t)
	w.batch()
	want := team.ReportsRoom{State: team.ReportsRefused, Check: "M_FORBIDDEN"}
	if rr := nextReports(t, rooms); rr != want {
		t.Fatalf("the reports room is %+v, want %+v", rr, want)
	}
	if got := tm.ReportsRoom(); got != want {
		t.Errorf("ReportsRoom() = %+v", got)
	}
	w.agentText(peer, "main goes on", user(me))
	if b := w.batch(); len(b.Items) != 1 {
		t.Errorf("main's item was not delivered: %+v", b)
	}
	if n := w.reportsJoins(); n != 1 {
		t.Errorf("a refused join was tried %d times without a start, a roster change or a report", n)
	}
}

// RP1: a join that fails transiently twice then succeeds shows joined, the team going on and
// the retries SY8's backoff.
func TestATransientReportsJoinFailureIsRetriedWithBackoff(t *testing.T) {
	w := newWorld(t)
	w.srv.Inject(fakehs.Fault{Method: http.MethodPost, Path: w.joinOf(), Status: http.StatusBadGateway, Body: []byte(`{}`), Times: 2})
	tm := w.newTeam()
	rooms := watchReports(tm)
	r := w.startTeam(tm)
	r.poll(t)
	w.batch()
	for _, want := range []time.Duration{time.Second, 2 * time.Second} {
		if d := r.wait(t); d != want {
			t.Fatalf("the join waited %v, want %v", d, want)
		}
		w.agentText(peer, "main goes on", user(me))
		if b := w.batch(); len(b.Items) != 1 {
			t.Fatalf("main's item was not delivered while the join failed: %+v", b)
		}
		w.clk.Advance(want)
	}
	if rr := nextReports(t, rooms); rr.State != team.ReportsJoined {
		t.Errorf("the reports room is %+v after the third try", rr)
	}
	if n := w.reportsJoins(); n != 3 {
		t.Errorf("%d joins, want 3", n)
	}
}

// RP1: a 403 for a member already in the room is joined: membership is read from the joined
// rooms, never from the error's text.
func TestAForbiddenJoinOfAMemberAlreadyInIsJoined(t *testing.T) {
	w := newWorld(t)
	if _, err := w.client(me).Join(t.Context(), w.ids.Reports); err != nil {
		t.Fatalf("join: %v", err)
	}
	w.forbidJoin(1)
	tm := w.newTeam()
	rooms := watchReports(tm)
	w.startTeam(tm)
	if rr := nextReports(t, rooms); rr.State != team.ReportsJoined {
		t.Errorf("the reports room is %+v, want joined", rr)
	}
}

// RP1: a refused join is tried again at the next roster change and at the next report.
func TestARefusedReportsJoinIsTriedAgainAtARosterChangeAndAReport(t *testing.T) {
	w := newWorld(t)
	w.forbidJoin(2)
	tm := w.newTeam()
	rooms := watchReports(tm)
	r := w.startTeam(tm)
	r.poll(t)
	if rr := nextReports(t, rooms); rr.State != team.ReportsRefused {
		t.Fatalf("the reports room is %+v, want refused", rr)
	}
	next := w.roster
	next.Name = "Alpha again"
	w.writeRoster(next)
	r.poll(t)
	w.batch()
	if n := w.reportsJoins(); n != 2 {
		t.Fatalf("%d joins after the roster change, want 2", n)
	}
	if rr := nextReports(t, rooms); rr.State != team.ReportsRefused {
		t.Fatalf("the reports room is %+v after the second refusal", rr)
	}
	err := tm.VerifyReports(t.Context())
	if err != nil {
		t.Fatalf("VerifyReports: %v", err)
	}
	if got := tm.ReportsRoom(); got.State != team.ReportsJoined {
		t.Errorf("after a report's join the room is %+v", got)
	}
	if n := w.reportsJoins(); n != 3 {
		t.Errorf("%d joins, want 3", n)
	}
}

// RP4: a room that verifies: nil, joined.
func TestVerifyReportsOfAGoodRoom(t *testing.T) {
	w := newWorld(t)
	tm := w.newTeam()
	rooms := watchReports(tm)
	r := w.startTeam(tm)
	r.poll(t)
	nextReports(t, rooms)
	if err := tm.VerifyReports(t.Context()); err != nil {
		t.Fatalf("VerifyReports: %v", err)
	}
	if got := tm.ReportsRoom(); got != (team.ReportsRoom{State: team.ReportsJoined}) {
		t.Errorf("ReportsRoom() = %+v", got)
	}
}

// RP4: each failed check is 22 untrusted naming the reports room and the check, and the room
// shows untrusted with the check; main is left as it was.
func TestVerifyReportsRefusesEachFailedCheck(t *testing.T) {
	cases := map[string]struct {
		breakIt func(w *world)
		check   string
	}{
		"join rule": {func(w *world) {
			put(w, proto.JoinRulesType, `{"join_rule":"invite"}`)
		}, "join rule"},
		"link": {func(w *world) {
			put(w, proto.LinkType, `{"v":1,"team":"other","purpose":"reports","main":"!x:`+server+`"}`)
		}, "link"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			tm := w.newTeam()
			rooms := watchReports(tm)
			r := w.startTeam(tm)
			r.poll(t)
			w.batch()
			nextReports(t, rooms)
			tc.breakIt(w)
			err := tm.VerifyReports(t.Context())
			xe, ok := errors.AsType[*exits.Error](err)
			if !ok || xe.Code != exits.CodeUntrusted {
				t.Fatalf("VerifyReports = %v, want 22 untrusted", err)
			}
			if !strings.Contains(xe.Message, "reports") || !strings.Contains(xe.Message, tc.check) {
				t.Errorf("the message %q names neither the reports room nor %q", xe.Message, tc.check)
			}
			if got := tm.ReportsRoom(); got != (team.ReportsRoom{State: team.ReportsUntrusted, Check: tc.check}) {
				t.Errorf("ReportsRoom() = %+v", got)
			}
			w.agentText(peer, "main goes on", user(me))
			if b := w.batch(); len(b.Items) != 1 {
				t.Errorf("main's item was not delivered after the reports failure: %+v", b)
			}
			if h := w.eng.lastHealth(); h.Stopped != "" {
				t.Errorf("main was stopped %q by a reports room's failed check", h.Stopped)
			}
		})
	}
}

// put writes a state event in the reports room as the admin does.
func put(w *world, typ, content string) {
	w.t.Helper()
	if _, err := w.admin().PutState(w.t.Context(), w.ids.Reports, typ, "", json.RawMessage(content)); err != nil {
		w.t.Fatalf("put %s: %v", typ, err)
	}
}

// RP4: a refused join at a report is 22 forbidden naming the reports room, and the room is
// refused; a 403 on the state read is 22 forbidden.
func TestVerifyReportsForbidden(t *testing.T) {
	t.Run("the join", func(t *testing.T) {
		w := newWorld(t)
		w.forbidJoin(2)
		tm := w.newTeam()
		r := w.startTeam(tm)
		r.poll(t)
		err := tm.VerifyReports(t.Context())
		xe, ok := errors.AsType[*exits.Error](err)
		if !ok || xe.Code != exits.CodeForbidden || !strings.Contains(xe.Message, "reports") {
			t.Fatalf("VerifyReports = %v, want 22 forbidden naming the reports room", err)
		}
		if got := tm.ReportsRoom(); got.State != team.ReportsRefused {
			t.Errorf("ReportsRoom() = %+v", got)
		}
	})
	t.Run("the state read", func(t *testing.T) {
		w := newWorld(t)
		tm := w.newTeam()
		rooms := watchReports(tm)
		r := w.startTeam(tm)
		r.poll(t)
		nextReports(t, rooms)
		w.srv.Inject(fakehs.Fault{Method: http.MethodGet, Path: url.PathEscape(w.ids.Reports) + "/state", Status: http.StatusForbidden, Body: []byte(`{"errcode":"M_FORBIDDEN","error":"x"}`)})
		err := tm.VerifyReports(t.Context())
		if xe, ok := errors.AsType[*exits.Error](err); !ok || xe.Code != exits.CodeForbidden {
			t.Fatalf("VerifyReports = %v, want 22 forbidden", err)
		}
	})
}

// RP4: a transient failure of the state read is no verdict: the error is not an exits error, so
// the engine queues the report, and the room's state is as it was.
func TestVerifyReportsTransient(t *testing.T) {
	w := newWorld(t)
	tm := w.newTeam()
	rooms := watchReports(tm)
	r := w.startTeam(tm)
	r.poll(t)
	nextReports(t, rooms)
	w.srv.Inject(fakehs.Fault{Method: http.MethodGet, Path: url.PathEscape(w.ids.Reports) + "/state", Status: http.StatusBadGateway, Body: []byte(`{}`), Times: 10})
	err := tm.VerifyReports(t.Context())
	if err == nil {
		t.Fatal("VerifyReports answered nil over a state read that failed")
	}
	if errors.As(err, new(*exits.Error)) {
		t.Errorf("a transient failure was refused: %v", err)
	}
	if got := tm.ReportsRoom(); got.State != team.ReportsJoined {
		t.Errorf("the room's state changed to %+v by a transient failure", got)
	}
}

// RP4: before main is verified a report cannot be judged: not a refusal.
func TestVerifyReportsBeforeMainIsVerifiedIsTransient(t *testing.T) {
	w := newWorld(t)
	tm := w.newTeam()
	err := tm.VerifyReports(t.Context())
	if err == nil {
		t.Fatal("VerifyReports answered nil before main was verified")
	}
	if errors.As(err, new(*exits.Error)) {
		t.Errorf("an unverified team refused: %v", err)
	}
}

// reportEvent is a roster agent's report as the homeserver holds it in the reports room.
func (w *world) reportEvent(local, text string) (string, json.RawMessage) {
	w.t.Helper()
	c := w.client(local)
	if _, err := c.Join(w.t.Context(), w.ids.Reports); err != nil {
		w.t.Fatalf("%s joins the reports room: %v", local, err)
	}
	out, err := proto.Composer{Server: server, Self: user(peer), Token: w.tokens[peer], Roster: w.roster, Reports: &proto.Window{}, Gate: &proto.SendGate{}, NowMs: startMS}.Report(text)
	check(w.t, err)
	id, err := c.Send(w.t.Context(), w.ids.Reports, proto.MessageType, "", out.Content.Compact())
	check(w.t, err)
	raw, err := c.GetEvent(w.t.Context(), w.ids.Reports, id)
	check(w.t, err)
	return id, raw
}

// RP2: a reports event that reaches a judge anyway is a fault: one line with its id, once, and
// no item; a sender off the roster is dropped `sender` first. (The sync filter names no reports
// room, so the server sends none: the answer is forged.)
func TestAReportsEventInAnAnswerIsAFaultAndNoItem(t *testing.T) {
	w := newWorld(t)
	srvUser := "mallory"
	w.srv.AddUser(srvUser, "pw", false)
	token, err := w.srv.MintToken(user(srvUser))
	check(t, err)
	w.tokens[srvUser] = token
	check(t, w.admin().Invite(t.Context(), w.main, user(srvUser)))
	if _, err := w.client(srvUser).Join(t.Context(), w.main); err != nil {
		t.Fatalf("join main: %v", err)
	}
	good, goodRaw := w.reportEvent(peer, "a report\nwith a body")
	_, strangerRaw := w.reportEvent(srvUser, "a stranger's report")
	tm := w.newTeam()
	s := &sess{t: tm, c: w.client(me), a: proto.Anchors{Server: server, Team: teamName, MainRoom: w.main, Self: user(me)}, roster: w.roster}
	forged := matrix.SyncResponse{NextBatch: "s1", Join: map[string]matrix.RoomUpdate{
		w.ids.Reports: {Timeline: matrix.Timeline{Events: []json.RawMessage{goodRaw, strangerRaw, goodRaw}}},
	}}
	for range 2 {
		if err := s.handle(t.Context(), "", answer{resp: forged}); err != nil {
			t.Fatalf("handle: %v", err)
		}
	}
	if n := w.log.count("fault " + good); n != 1 {
		t.Errorf("%d fault lines for the report, want 1: %q", n, w.log.String())
	}
	if n := w.log.count("dropped"); n < 1 || !strings.Contains(w.log.String(), ": sender") {
		t.Errorf("the stranger's report was not dropped `sender`: %q", w.log.String())
	}
	w.noBatch()
}
