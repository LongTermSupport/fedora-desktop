package teamsync

import (
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// joinRule sets the questions room's join rule as the admin would, a trust-bearing change.
func (w *world) joinRule(rule string) {
	w.t.Helper()
	if _, err := w.admin().PutState(w.t.Context(), w.qroom(), proto.JoinRulesType, "", []byte(`{"join_rule":"`+rule+`"}`)); err != nil {
		w.t.Fatalf("join rule: %v", err)
	}
}

// mainItemDelivered is QS8's "main goes on": an agent's text to the coordinator in main is
// persisted as an item, within the next four batches.
func (w *world) mainItemDelivered() {
	w.t.Helper()
	id := w.agentText(peer, "status?", user(coord))
	w.batchWith(func(b team.Batch) bool { return slices.Contains(itemIDs(b), id) })
}

func refusal(status int, code string) fakehs.Fault {
	return fakehs.Fault{Status: status, Body: []byte(`{"errcode":"` + code + `","error":"x"}`)}
}

// QS8: a questions room whose state read is refused (403, 404) or that fails VerifySide at the
// start is untrusted alone, with the check named: the filter is the worker form, and main's
// items are delivered meanwhile.
func TestQS8AFailedCheckAtTheStartIsTheRoomsAlone(t *testing.T) {
	cases := map[string]struct {
		prep  func(w *world)
		check string
	}{
		"state refused 403": {func(w *world) {
			f := refusal(http.StatusForbidden, "M_FORBIDDEN")
			f.Method, f.Path, f.Times = http.MethodGet, url.PathEscape(w.qroom())+"/state", 2
			w.srv.Inject(f)
		}, "state"},
		"state missing 404": {func(w *world) {
			f := refusal(http.StatusNotFound, "M_NOT_FOUND")
			f.Method, f.Path = http.MethodGet, url.PathEscape(w.qroom())+"/state"
			w.srv.Inject(f)
		}, "state"},
		"join rule": {func(w *world) { w.joinRule("public") }, "join rule"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.asCoordinator()
			c.prep(w)
			tm := w.newTeam(withBuilder(&fakeBuilder{}))
			filters := filtersOf(tm)
			r := w.startTeam(tm)
			w.batch()
			r.poll(t)
			if st := tm.QuestionsStatus(); st.State != team.QuestionsUntrusted || st.Check != c.check {
				t.Errorf("status %+v, want untrusted with the check %q", st, c.check)
			}
			if got := nextFilter(t, filters); got != w.filterFor(false) {
				t.Errorf("filter:\n%s\nwant the worker form", got)
			}
			w.mainItemDelivered()
		})
	}
}

// QS8 (A 6.5): a trust-bearing event in the questions room's own timeline re-verifies it; a
// check it now fails makes the room untrusted, out of the filter, and main goes on.
func TestQS8ATrustTriggerInTheRoomReVerifies(t *testing.T) {
	w := newWorld(t)
	rt := w.readyWith(w.main, nil)
	reads := w.calls(http.MethodGet, "/state", w.qroom())
	rt.drainPolls()
	w.joinRule("public")
	w.mainItemDelivered()
	if n := w.calls(http.MethodGet, "/state", w.qroom()); n <= reads {
		t.Errorf("%d state reads of the room after the trigger, %d before: it was not verified again", n, reads)
	}
	if st := rt.tm.QuestionsStatus(); st.State != team.QuestionsUntrusted || st.Check != "join rule" {
		t.Errorf("status %+v, want untrusted with the check join rule", st)
	}
	// The filters drained before the trigger: the first after it is the request after its answer.
	if got := nextFilter(t, rt.filters); got != w.filterFor(false) {
		t.Errorf("filter after the failed check:\n%s\nwant the worker form", got)
	}
}

// QS8: a refused history read of the room (QS7's fill) is the check `state`; main's item in
// the same answer is delivered, and the key is left as it was.
func TestQS8ARefusedHistoryReadIsUntrusted(t *testing.T) {
	w := newWorld(t)
	w.inQuestions(alice, false)
	var g *filterGate
	var release func()
	rt := w.readyWith(w.main, func(tm *Team) {
		g = gateFilter(t, tm, w.filterFor(true))
		release = g.armed()
	})
	g.isHeld()
	w.questionPosts()
	mainID := w.agentText(peer, "status?", user(coord))
	f := refusal(http.StatusForbidden, "M_FORBIDDEN")
	f.Method, f.Path = http.MethodGet, "/messages"
	w.srv.Inject(f)
	rt.drainPolls()
	release()
	b := w.persistedBefore(rt.r)
	if got := itemIDs(b); !slices.Equal(got, []string{mainID}) {
		t.Errorf("items %v, want main's item alone", got)
	}
	if b.Questions != nil {
		t.Errorf("the key %+v written by an answer whose room was refused", b.Questions)
	}
	if st := rt.tm.QuestionsStatus(); st.State != team.QuestionsUntrusted || st.Check != "state" {
		t.Errorf("status %+v, want untrusted with the check state", st)
	}
}

// QS8: an untrusted room that passes its check again at the next roster change is caught up
// from the store's key (QX1) with its floor, so what was posted while it was untrusted is the
// catch-up's to take; the catch-up itself is n3f-c2's builder.
func TestQS8RecoveryCatchesTheRoomUpFromItsToken(t *testing.T) {
	w := newWorld(t)
	w.inQuestions(alice, false)
	rt := w.readyWith(w.main, nil)
	folded := w.sayIn(w.qroom(), alice, human("before"))
	w.batchWith(lastIs(folded))
	w.joinRule("public")
	w.mainItemDelivered()
	if st := rt.tm.QuestionsStatus(); st.State != team.QuestionsUntrusted {
		t.Fatalf("status %+v, want untrusted", st)
	}
	w.clk.StepWall(1000)
	during := w.sayIn(w.qroom(), alice, human("while untrusted"))
	w.mainItemDelivered()
	store, err := w.eng.Load(w.team, user(coord))
	check(t, err)
	w.joinRule("invite")
	jobs := len(rt.fb.jobs())
	w.writeRoster(w.readRoster())
	w.mainItemDelivered()
	got := rt.fb.jobs()[jobs:]
	if len(got) != 1 || got[0].RoomID != w.qroom() || !reflect.DeepEqual(got[0].Key, store.Questions) {
		t.Fatalf("builder jobs %+v after the recovery, want one catch-up from the store's key %+v", got, store.Questions)
	}
	ev, err := w.admin().GetEvent(t.Context(), w.qroom(), during)
	check(t, err)
	v, err := proto.Parse(ev)
	check(t, err)
	if ts := originTs(v); got[0].Floor >= ts {
		t.Errorf("the catch-up's floor %d is not before the post made while untrusted (%d)", got[0].Floor, ts)
	}
	if st := rt.tm.QuestionsStatus(); st.State != team.QuestionsBuilding {
		t.Errorf("status %+v, want building", st)
	}
}

// QS8: a kick seen while the roster still names this member is `left`, never a trust failure;
// the join is tried again, and QS2's 300 s clock starts again at the kick.
func TestQS8AKickWhileNamedIsLeftAndRestartsTheClock(t *testing.T) {
	w := newWorld(t)
	w.asCoordinator()
	tm := w.newTeam(withBuilder(&fakeBuilder{}))
	s := w.directSess(tm, w.readRoster())
	store, err := w.eng.Load(w.team, user(coord))
	check(t, err)
	check(t, s.qstart(t.Context(), store))
	if st := tm.QuestionsStatus(); st.State != team.QuestionsBuilding {
		t.Fatalf("status %+v at the start, want building (joined)", st)
	}
	// 10 h into the term, QS2's clock started at the start; no team runs, so no request is
	// overdue and nothing reads as a freeze (SY12).
	w.clk.Advance(10 * time.Hour)
	joins := w.joinsOf(w.qroom())
	check(t, w.admin().Kick(t.Context(), w.qroom(), user(coord), "kicked"))
	// fakehs sends no `leave` section, so the kick's answer is made here.
	resp := matrix.SyncResponse{NextBatch: "n", Leave: map[string]matrix.RoomUpdate{w.qroom(): {}}}
	check(t, s.handle(t.Context(), "", answer{resp: resp}))
	if st := tm.QuestionsStatus(); st.State != team.QuestionsLeft {
		t.Errorf("status %+v just after the kick, want left", st)
	}
	if n := w.joinsOf(w.qroom()); n <= joins {
		t.Error("the join was not tried again after the kick")
	}
	w.clk.Advance(299 * time.Second)
	if st := tm.QuestionsStatus(); st.State != team.QuestionsLeft {
		t.Errorf("at 299 s after the kick: %+v, want left", st)
	}
	w.clk.Advance(2 * time.Second)
	if st := tm.QuestionsStatus(); st.State != team.QuestionsUntrusted || st.Check != team.CheckInvite {
		t.Errorf("at 301 s after the kick: %+v, want untrusted with the check invite", st)
	}
}
