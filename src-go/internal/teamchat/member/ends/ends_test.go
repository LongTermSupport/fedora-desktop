package ends

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/notice"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

var txnRe = regexp.MustCompile(`^tc[0-9a-f]{32}$`)

// RS1: the restart switch ships off; RS3's commit turns it on.
func TestTheRestartSwitchShipsOff(t *testing.T) {
	if restartSwitch {
		t.Fatal("restartSwitch is on")
	}
	if newEnds(t, nil).e.switchOn() {
		t.Fatal("the ends' switch is on with no override")
	}
}

type endCase struct {
	name     string
	how      string
	reason   *string
	switchOn *bool
	event    string // "" posts nothing
	left     string
	none     string // the reason of a run that posts nothing
}

func endCases() []endCase {
	on, off := true, false
	return []endCase{
		{name: "restart, switch on", how: wire.EndRestart, switchOn: &on, none: state.ReasonRestart},
		{name: "restart, switch on, a reason", how: wire.EndRestart, reason: new("exit"), switchOn: &on, none: state.ReasonRestart},
		{name: "restart, switch off, after /exit", how: wire.EndRestart, reason: new("exit"), switchOn: &off, event: proto.EventLeft, left: "exit"},
		{name: "restart, switch off, no reason", how: wire.EndRestart, switchOn: &off, event: proto.EventDisconnected},
		{name: "exit with exit", how: wire.EndExit, reason: new("exit"), event: proto.EventLeft, left: "exit"},
		{name: "exit with logout", how: wire.EndExit, reason: new("logout"), event: proto.EventLeft, left: "logout"},
		{name: "killed with other", how: wire.EndKilled, reason: new("other"), event: proto.EventLeft, left: "other"},
		{name: "stop, no reason", how: wire.EndStop, event: proto.EventLeft, left: "other"},
		{name: "stop with logout", how: wire.EndStop, reason: new("logout"), event: proto.EventLeft, left: "logout"},
		{name: "exit, no reason", how: wire.EndExit, event: proto.EventDisconnected},
		{name: "killed, no reason", how: wire.EndKilled, event: proto.EventDisconnected},
		{name: "the keeper died", how: HowKeeperDied, event: proto.EventDisconnected},
		{name: "launch failed", how: wire.EndLaunchFailed, none: state.ReasonLaunchFailed},
	}
}

// EN1: B 2.11's table, the switch in both positions.
func TestTheEndTable(t *testing.T) {
	for _, tc := range endCases() {
		t.Run(tc.name, func(t *testing.T) { checkEnd(t, tc) })
	}
}

func checkEnd(t *testing.T, tc endCase) {
	t.Helper()
	w := newWorld(t)
	f := newEnds(t, tc.switchOn)
	n := w.notices(t, worker, false)
	alpha := teamOf(t, teamID)
	errs := f.e.End(t.Context(), team.End{How: tc.how, SessionEndReason: tc.reason, Kind: "ccy"},
		[]team.EndTeam{{Team: alpha, Notices: n, Run: state.StoreRun{ID: runID(t, thisRun), StartedMS: 1}}})
	if len(errs) != 1 || errs[0] != nil {
		t.Fatalf("End: %v", errs)
	}
	got, ended := w.lifecycles(t), f.eng.of(alpha)
	if tc.event == "" {
		checkNothingPosted(t, tc, got, ended)
		return
	}
	if len(got) != 1 || got[0] != (lifecycle{user(worker), tc.event, tc.left}) {
		t.Fatalf("posted %v", got)
	}
	txn := checkSentEnd(t, tc, ended)
	if sent := w.sentTxns(); len(sent) != 1 || sent[0] != txn {
		t.Fatalf("sent %v, recorded %s", sent, txn)
	}
	if own := n.owns(); len(own) != 1 {
		t.Fatalf("own sends %v", own)
	}
}

// checkSentEnd checks a posted end was recorded attempted, then sent, by one txn; it is the txn.
func checkSentEnd(t *testing.T, tc endCase, ended []state.RunEnd) string {
	t.Helper()
	if len(ended) != 2 || ended[0].State != state.StateAttempted || ended[1].State != state.StateSent {
		t.Fatalf("recorded %+v", ended)
	}
	txn := *ended[0].Txn
	if !txnRe.MatchString(txn) || *ended[1].Txn != txn || ended[0].Notice != tc.event {
		t.Fatalf("recorded %+v", ended)
	}
	if tc.left != "" && *ended[0].Reason != tc.left || tc.left == "" && ended[0].Reason != nil {
		t.Fatalf("recorded reason %v", ended[0].Reason)
	}
	return txn
}

func checkNothingPosted(t *testing.T, tc endCase, got []lifecycle, ended []state.RunEnd) {
	t.Helper()
	if len(got) != 0 || len(ended) != 1 {
		t.Fatalf("posted %v, recorded %+v", got, ended)
	}
	end := ended[0]
	if end.Notice != state.NoticeNone || end.State != state.StateSkipped || end.Txn != nil || *end.Reason != tc.none || end.AtMS != 1_770_000_000_000 {
		t.Fatalf("recorded %+v", end)
	}
}

// EN1: every team in parallel; the own send is recorded with the room and its addressees; a team
// whose run is not this run's is neither posted for nor recorded.
func TestEndPostsForEveryTeamOfThisRun(t *testing.T) {
	w := newWorld(t)
	f := newEnds(t, nil)
	a, b, c := teamOf(t, "alpha"), teamOf(t, "beta"), teamOf(t, "gamma")
	na, nb, nc := w.notices(t, worker, true), w.notices(t, coord, false), w.notices(t, other, true)
	errs := f.e.End(t.Context(), team.End{How: wire.EndExit}, []team.EndTeam{
		{Team: a, Notices: na, Run: state.StoreRun{ID: runID(t, thisRun)}},
		{Team: b, Notices: nb, Run: state.StoreRun{ID: runID(t, thisRun)}},
		{Team: c, Notices: nc, Run: state.StoreRun{ID: runID(t, priorRun)}},
	})
	if len(errs) != 3 || errors.Join(errs...) != nil {
		t.Fatalf("End: %v", errs)
	}
	if got := w.lifecycles(t); len(got) != 2 {
		t.Fatalf("posted %v", got)
	}
	if len(f.eng.of(c)) != 0 || nc.sent != 0 {
		t.Fatal("another run's team was ended")
	}
	for id, r := range na.owns() {
		if r.Sender != user(worker) || r.RoomID != w.main || !r.AddressedToRosterAgent || id == "" {
			t.Fatalf("own send %s %+v", id, r)
		}
	}
	for _, r := range nb.owns() {
		if r.Sender != user(coord) || r.AddressedToRosterAgent {
			t.Fatalf("the coordinator's own send %+v", r)
		}
	}
}

// EN1: a probe seat posts nothing, and its end stays attempted for the next take to judge.
func TestAProbePostsNothing(t *testing.T) {
	w := newWorld(t)
	f := newEnds(t, nil)
	n := w.notices(t, worker, true)
	n.roster.Members = map[string]proto.RosterMember{user(worker): {Role: proto.RoleProbe}}
	alpha := teamOf(t, teamID)
	if errs := f.e.End(t.Context(), team.End{How: wire.EndExit}, []team.EndTeam{{Team: alpha, Notices: n, Run: state.StoreRun{ID: runID(t, thisRun)}}}); errs[0] != nil {
		t.Fatalf("End: %v", errs)
	}
	if n.sent != 0 || len(f.eng.of(alpha)) != 1 {
		t.Fatalf("sent %d, recorded %+v", n.sent, f.eng.of(alpha))
	}
	if err := f.e.Leave(t.Context(), alpha, n, "tc"+strings.Repeat("a", 32)); err != nil || n.sent != 0 {
		t.Fatalf("Leave: %v, sent %d", err, n.sent)
	}
	if err := f.e.Back(t.Context(), alpha, n, state.StoreRun{ID: runID(t, thisRun)}); err != nil || n.sent != 0 {
		t.Fatalf("Back: %v, sent %d", err, n.sent)
	}
}

// EN1: a failed send or record is that team's error; the end stays attempted.
func TestEndFailures(t *testing.T) {
	w := newWorld(t)
	f := newEnds(t, nil)
	alpha := teamOf(t, teamID)
	this := state.StoreRun{ID: runID(t, thisRun)}
	n := w.notices(t, worker, true)
	n.sendErr = errors.New("down")
	errs := f.e.End(t.Context(), team.End{How: wire.EndExit}, []team.EndTeam{{Team: alpha, Notices: n, Run: this}})
	if !errors.Is(errs[0], n.sendErr) {
		t.Fatalf("End: %v", errs)
	}
	if ended := f.eng.of(alpha); len(ended) != 1 || ended[0].State != state.StateAttempted {
		t.Fatalf("recorded %+v", ended)
	}
	f.eng.err = errors.New("store")
	errs = f.e.End(t.Context(), team.End{How: wire.EndLaunchFailed}, []team.EndTeam{{Team: alpha, Notices: n, Run: this}})
	if !errors.Is(errs[0], f.eng.err) {
		t.Fatalf("launch failed: %v", errs)
	}
	errs = f.e.End(t.Context(), team.End{How: wire.EndExit}, []team.EndTeam{{Team: alpha, Notices: w.notices(t, worker, true), Run: this}})
	if !errors.Is(errs[0], f.eng.err) || len(w.lifecycles(t)) != 0 {
		t.Fatalf("an unrecorded attempt was sent: %v", errs)
	}
	errs = f.e.End(t.Context(), team.End{How: "lost"}, []team.EndTeam{{Team: alpha, Notices: n, Run: this}})
	if errs[0] == nil {
		t.Fatal("an unknown end kind is no error")
	}
}

// EN1: main that does not verify posts nothing; a handle on another server composes nothing.
func TestEndUntrusted(t *testing.T) {
	w := newWorld(t)
	f := newEnds(t, nil)
	alpha := teamOf(t, teamID)
	this := state.StoreRun{ID: runID(t, thisRun)}
	n := w.notices(t, worker, false)
	n.anchors.Team = "beta"
	if errs := f.e.End(t.Context(), team.End{How: wire.EndExit}, []team.EndTeam{{Team: alpha, Notices: n, Run: this}}); errs[0] == nil || n.sent != 0 {
		t.Fatalf("End over an unverified main: %v, sent %d", errs, n.sent)
	}
	n = w.notices(t, worker, true)
	n.anchors.Self = "@" + worker + ":elsewhere.example.com"
	if errs := f.e.End(t.Context(), team.End{How: wire.EndExit}, []team.EndTeam{{Team: alpha, Notices: n, Run: this}}); errs[0] == nil || n.sent != 0 {
		t.Fatalf("End as another server's user: %v, sent %d", errs, n.sent)
	}
}

// EN3: an unseen end (ended null) is closed by tc-disc-<ended run>, once with the host's call.
func TestCloseAnUnseenEnd(t *testing.T) {
	w := newWorld(t)
	e := newEnds(t, nil).e
	alpha := teamOf(t, teamID)
	n := w.notices(t, worker, false)
	w.clock.set(5_000)
	prev := state.StoreRun{ID: runID(t, priorRun), StartedMS: 4_000}
	if err := e.Close(t.Context(), alpha, n, prev); err != nil {
		t.Fatalf("Close: %v", err)
	}
	host := notice.Notice{Server: server, Team: teamID, MainRoom: w.main, Handle: worker, Token: w.tokens[worker], Run: prev.ID, SinceMS: 4_000}
	if out, err := notice.Disconnected(t.Context(), w.client(t, worker), host); err != nil || !out.Found {
		t.Fatalf("the host's call: %+v %v", out, err)
	}
	if err := e.Close(t.Context(), alpha, n, prev); err != nil {
		t.Fatalf("Close again: %v", err)
	}
	if got := w.lifecycles(t); len(got) != 1 || got[0].event != proto.EventDisconnected {
		t.Fatalf("posted %v", got)
	}
	if sent := w.sentTxns(); len(sent) != 1 || sent[0] != notice.TxnID(prev.ID) {
		t.Fatalf("sent %v", sent)
	}
}

// EN3: a run that posted nothing (a restart, a launch failure) or whose end was sent is closed
// with no post.
func TestCloseWithNothingOwed(t *testing.T) {
	w := newWorld(t)
	f := newEnds(t, nil)
	alpha := teamOf(t, teamID)
	n := w.notices(t, worker, true)
	for _, ended := range []*state.RunEnd{
		{Notice: state.NoticeNone, Reason: new(state.ReasonRestart), State: state.StateSkipped},
		{Notice: state.NoticeNone, Reason: new(state.ReasonLaunchFailed), State: state.StateSkipped},
		{Notice: state.NoticeLeft, Reason: new("exit"), State: state.StateSent, Txn: new("tc" + strings.Repeat("b", 32))},
	} {
		if err := f.e.Close(t.Context(), alpha, n, state.StoreRun{ID: runID(t, priorRun), Ended: ended}); err != nil {
			t.Fatalf("Close %+v: %v", ended, err)
		}
	}
	if n.sent != 0 || len(f.eng.of(alpha)) != 0 {
		t.Fatalf("sent %d, recorded %+v", n.sent, f.eng.of(alpha))
	}
}

// EN3, n3c-D19: an attempted end is judged from the room first: re-sent by its recorded txn
// when no goodbye since the run started is there, nothing when one is.
func TestCloseAnAttemptedEnd(t *testing.T) {
	w := newWorld(t)
	f := newEnds(t, nil)
	alpha := teamOf(t, teamID)
	n := w.notices(t, worker, true)
	txn := "tc" + strings.Repeat("c", 32)
	w.clock.set(5_000)
	prev := state.StoreRun{
		ID: runID(t, priorRun), StartedMS: 4_000,
		Ended: &state.RunEnd{Notice: state.NoticeLeft, Reason: new("logout"), State: state.StateAttempted, Txn: &txn, AtMS: 4_500},
	}
	if err := f.e.Close(t.Context(), alpha, n, prev); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := w.lifecycles(t); len(got) != 1 || got[0] != (lifecycle{user(worker), proto.EventLeft, "logout"}) {
		t.Fatalf("posted %v", got)
	}
	if sent := w.sentTxns(); len(sent) != 1 || sent[0] != txn {
		t.Fatalf("sent %v", sent)
	}
	if ended := f.eng.of(alpha); len(ended) != 1 || ended[0].State != state.StateSent || *ended[0].Txn != txn || ended[0].AtMS != 4_500 {
		t.Fatalf("recorded %+v", ended)
	}
	if err := f.e.Close(t.Context(), alpha, n, prev); err != nil {
		t.Fatalf("Close again: %v", err)
	}
	if got := w.lifecycles(t); len(got) != 1 {
		t.Fatalf("a found goodbye was re-sent: %v", got)
	}
}

// EN3: a transient failure is the team's error, so it closes the run again later.
func TestCloseTransient(t *testing.T) {
	w := newWorld(t)
	e := newEnds(t, nil).e
	n := w.notices(t, worker, true)
	n.messagesErr = errors.New("down")
	n.stateErr = n.messagesErr
	if err := e.Close(t.Context(), teamOf(t, teamID), n, state.StoreRun{ID: runID(t, priorRun)}); err == nil {
		t.Fatal("an unreachable server closed the run")
	}
	txn := "tc" + strings.Repeat("d", 32)
	prev := state.StoreRun{ID: runID(t, priorRun), Ended: &state.RunEnd{Notice: state.NoticeDisconnected, State: state.StateAttempted, Txn: &txn}}
	if err := e.Close(t.Context(), teamOf(t, teamID), n, prev); !errors.Is(err, n.messagesErr) {
		t.Fatalf("an unreachable server and an attempted run: %v", err)
	}
	if err := e.Back(t.Context(), teamOf(t, teamID), n, state.StoreRun{ID: runID(t, thisRun)}); !errors.Is(err, n.messagesErr) {
		t.Fatalf("Back on an unreachable server: %v", err)
	}
}

// EN4: back only when the latest own lifecycle notice is disconnected; nothing on a link restart
// in the same run, whose latest is its own back.
func TestBack(t *testing.T) {
	w := newWorld(t)
	f := newEnds(t, nil)
	alpha := teamOf(t, teamID)
	n := w.notices(t, worker, false)
	run := state.StoreRun{ID: runID(t, thisRun)}
	if err := f.e.Back(t.Context(), alpha, n, run); err != nil || n.sent != 0 {
		t.Fatalf("Back with nothing before: %v, sent %d", err, n.sent)
	}
	w.clock.set(2_000)
	if out, err := notice.Disconnected(t.Context(), w.client(t, worker), notice.Notice{Server: server, Team: teamID, MainRoom: w.main, Handle: worker, Token: w.tokens[worker], Run: runID(t, priorRun)}); err != nil || !out.Posted {
		t.Fatalf("disconnected: %+v %v", out, err)
	}
	w.clock.set(3_000)
	for range 2 {
		if err := f.e.Back(t.Context(), alpha, n, run); err != nil {
			t.Fatalf("Back: %v", err)
		}
	}
	if got := w.lifecycles(t); len(got) != 2 || got[0].event != proto.EventBack {
		t.Fatalf("posted %v", got)
	}
	if sent := w.sentTxns(); sent[len(sent)-1] != "tc-back-"+thisRun {
		t.Fatalf("sent %v", sent)
	}
	if f.log.Len() != 0 {
		t.Fatalf("log %q", f.log.String())
	}
}

// LN8: left with reason released, by the keeper's txn.
func TestLeave(t *testing.T) {
	w := newWorld(t)
	f := newEnds(t, nil)
	n := w.notices(t, worker, true)
	txn := "tc" + strings.Repeat("e", 32)
	if err := f.e.Leave(t.Context(), teamOf(t, teamID), n, txn); err != nil {
		t.Fatalf("Leave: %v", err)
	}
	if got := w.lifecycles(t); len(got) != 1 || got[0] != (lifecycle{user(worker), proto.EventLeft, "released"}) {
		t.Fatalf("posted %v", got)
	}
	if sent := w.sentTxns(); len(sent) != 1 || sent[0] != txn {
		t.Fatalf("sent %v", sent)
	}
	if len(f.eng.of(teamOf(t, teamID))) != 0 {
		t.Fatal("a leave recorded an end")
	}
}

// EN3: a run main cannot close for good (it does not verify) is logged and closed; a seat user
// of another server closes nothing.
func TestCloseUntrusted(t *testing.T) {
	w := newWorld(t)
	f := newEnds(t, nil)
	alpha := teamOf(t, teamID)
	prev := state.StoreRun{ID: runID(t, priorRun)}
	n := w.notices(t, worker, false)
	n.anchors.Team = "beta"
	if err := f.e.Close(t.Context(), alpha, n, prev); err != nil || n.sent != 0 {
		t.Fatalf("Close over an unverified main: %v, sent %d", err, n.sent)
	}
	if !strings.Contains(f.log.String(), "does not verify") {
		t.Fatalf("log %q", f.log.String())
	}
	n = w.notices(t, worker, true)
	n.anchors.Self = "@" + worker + ":elsewhere.example.com"
	attempted := prev
	attempted.Ended = &state.RunEnd{Notice: state.NoticeDisconnected, State: state.StateAttempted, Txn: new("tc" + strings.Repeat("f", 32))}
	for _, run := range []state.StoreRun{prev, attempted} {
		if err := f.e.Close(t.Context(), alpha, n, run); !errors.Is(err, errNotOurs) {
			t.Fatalf("Close as another server's user: %v", err)
		}
	}
	if err := f.e.Back(t.Context(), alpha, n, prev); !errors.Is(err, errNotOurs) {
		t.Fatalf("Back as another server's user: %v", err)
	}
}
