package teamsync

import (
	"encoding/json"
	"maps"
	"net/http"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// SY11: the coordinator accepts the admin's invite to the questions room, and verifies again.
func TestTheCoordinatorJoinsTheQuestionsRoom(t *testing.T) {
	w := newWorld(t)
	w.as(coord)
	joins := w.requests("/join")
	w.start()
	w.batch()
	if n := w.requests("/join"); n != joins+1 {
		t.Errorf("%d joins, want %d", n, joins+1)
	}
	if n := w.requests("/state"); n < 3 {
		t.Errorf("main was verified %d times", n)
	}
}

// SY11: a failed join of an accepted invite is one line and holds nothing.
func TestAFailedJoinHoldsNothing(t *testing.T) {
	w := newWorld(t)
	w.as(coord)
	w.srv.Inject(fakehs.Fault{Method: http.MethodPost, Path: "/join", Status: http.StatusBadGateway, Body: []byte(`{}`)})
	r := w.start()
	w.batch()
	r.poll(t)
	if n := w.log.count("not joined"); n != 1 {
		t.Errorf("log %q", w.log.String())
	}
}

// invite is an invite's stripped state: the room created by creator, self invited by inviter.
func invite(creator, inviter, self string) matrix.InvitedRoom {
	return matrix.InvitedRoom{InviteState: []json.RawMessage{
		json.RawMessage(`{"type":"m.room.create","state_key":"","sender":"` + creator + `","content":{}}`),
		json.RawMessage(`{"type":"m.room.member","state_key":"` + self + `","sender":"` + inviter + `","content":{"membership":"invite"}}`),
		json.RawMessage(`not json`),
	}}
}

// SY11: an admin's invite to a room the roster does not name stays pending, said once; another's
// invite is rejected, and a failed leave is one line. (The sync filter names only the rooms
// this member reads, so the server sends no other invite: the steps are driven directly.)
func TestInvitesPendingAndRejected(t *testing.T) {
	w := newWorld(t)
	tm := w.newTeam()
	admin := proto.AdminID(server)
	s := &sess{t: tm, c: w.client(me), a: proto.Anchors{Server: server, Team: teamName, MainRoom: w.main, Self: user(me)}, roster: w.roster}
	pending := "!pendingroom:" + server
	ins := map[string]matrix.InvitedRoom{pending: invite(admin, admin, user(me)), "!fromapeer:" + server: invite(user(peer), user(peer), user(me))}
	for range 2 {
		if s.invites(t.Context(), ins) {
			t.Error("a room was joined")
		}
	}
	if n := w.log.count("left pending"); n != 1 {
		t.Errorf("log %q", w.log.String())
	}
	if n := w.log.count("not rejected"); n != 2 {
		t.Errorf("log %q", w.log.String())
	}
}

// SY14 and SY11: a role change in the roster reaches the health; a roster without this member
// stops the team untrusted.
func TestTheRosterIsVerifiedAgain(t *testing.T) {
	w := newWorld(t)
	r := w.start()
	w.batch()
	r.poll(t)
	next := w.roster
	next.Members = maps.Clone(w.roster.Members)
	next.Members[user(me)], next.Members[user(coord)] = w.roster.Members[user(coord)], w.roster.Members[user(me)]
	w.writeRoster(next)
	w.waitHealth(func(h team.Health) bool { return h.Role == proto.RoleCoordinator })
	gone := next
	gone.Members = map[string]proto.RosterMember{}
	for id, m := range next.Members {
		if id != user(me) {
			gone.Members[id] = m
		}
	}
	gone.Members[user(peer)] = proto.RosterMember{Role: proto.RoleCoordinator, HumanText: true}
	w.writeRoster(gone)
	w.waitHealth(stoppedWith(exits.CodeUntrusted))
}

// Verify's 403: a member kicked from main, and not invited, tries one join and stops forbidden.
func TestAKickedMemberStopsForbidden(t *testing.T) {
	w := newWorld(t)
	check(t, w.admin().Kick(t.Context(), w.main, user(me), ""))
	w.start()
	w.waitHealth(stoppedWith(exits.CodeForbidden))
}

// SY14's triggers, one by one. (fakehs does not model a sync's leave section, so main left is
// judged here.)
func TestTrustTriggers(t *testing.T) {
	s := &sess{a: proto.Anchors{MainRoom: "!main:" + server}}
	msg := json.RawMessage(`{"type":"m.room.message","content":{}}`)
	roster := json.RawMessage(`{"type":"` + proto.RosterType + `","state_key":"","content":{}}`)
	tl := func(limited bool, evs ...json.RawMessage) matrix.RoomUpdate {
		return matrix.RoomUpdate{Timeline: matrix.Timeline{Limited: limited, Events: evs}}
	}
	left := matrix.SyncResponse{Leave: map[string]matrix.RoomUpdate{s.a.MainRoom: {}}}
	for _, c := range []struct {
		name   string
		a      answer
		main   matrix.RoomUpdate
		joined bool
		want   bool
	}{
		{"a message", answer{}, tl(false, msg), false, false},
		{"a join", answer{}, tl(false), true, true},
		{"main left", answer{resp: left}, tl(false), false, true},
		{"limited", answer{}, tl(true), false, true},
		{"a state section", answer{}, matrix.RoomUpdate{State: []json.RawMessage{roster}}, false, true},
		{"SY1's state section", answer{first: true}, matrix.RoomUpdate{State: []json.RawMessage{roster}}, false, false},
		{"a roster in the timeline", answer{}, tl(false, json.RawMessage(`not json`), roster), false, true},
	} {
		if got := s.trustTriggered(c.a, c.main, c.joined); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}
}

// Verify's 403: a member kicked and invited again joins main once and goes on.
func TestAnInvitedMemberJoinsMain(t *testing.T) {
	w := newWorld(t)
	check(t, w.admin().Kick(t.Context(), w.main, user(me), ""))
	check(t, w.admin().Invite(t.Context(), w.main, user(me)))
	w.start()
	if b := w.batch(); b.NextBatch == "" {
		t.Errorf("batch %+v", b)
	}
}
