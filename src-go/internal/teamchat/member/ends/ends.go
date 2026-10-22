// Package ends is a run's lifecycle notices (Plan 00209 SPEC-n3 EN1-EN4, RS1-RS2; B 2.11):
// the goodbye a session's end posts in every team of this run, the closing of a store's
// previous run at a take, `back` after a disconnect, and the leave of a released seat. Each is
// posted as the member, on its team goroutine's client (team.Notices), from A's composer, and
// a run's end is recorded in its store before it is sent (attempted, by a `tc` transaction id),
// so a crash mid-send is closed by the next take from the room, never sent twice blind.
//
// Presence is the link's: it sets each team offline after End and before Leave. A seat whose
// roster role is probe posts nothing.
package ends

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/failpoint"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/notice"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// restartSwitch is RS2's one switch: on, a `restart` end posts nothing and the next run carries
// on (n3-D2); off, it is decided as an exit (RS1). RS3's commit (Task 4.2) turns it on.
const restartSwitch = false

// HowKeeperDied is the end the link makes itself when its keeper's control socket closes (LN1).
const HowKeeperDied = "keeper-died"

// The left reasons the ends post besides a recorded SessionEnd reason (A 7.4).
const (
	reasonOther    = "other"
	reasonReleased = "released"
)

// Config is what the ends are built from: the engine their run's ends are recorded through,
// this link's run, the clock whose wall time stamps an end, and the link's log.
type Config struct {
	Engine team.Engine
	Run    paths.RunID
	Clock  clock.Clock
	Log    io.Writer

	// switchOverride replaces restartSwitch in tests.
	switchOverride *bool
}

// New is the ends of cfg.
func New(cfg Config) team.Ends { return &ends{cfg: cfg} }

type ends struct {
	cfg   Config
	logMu sync.Mutex

	// turns are the teams' turns: one team's lifecycle notices posted one step at a time, each
	// step deciding from main and posting within its turn, so no other post of the run comes
	// between. ended are the teams whose run's end or leave has begun, marked before it waits
	// for its turn, so a back after it posts nothing even when the end gave up waiting.
	turnsMu sync.Mutex
	turns   map[paths.TeamID]chan struct{}
	ended   map[paths.TeamID]bool

	// turnWait, when set, is called when a turn must wait for another (tests).
	turnWait func()
}

var errUnknownEnd = errors.New("ends: an end of no known kind")

// End is EN1 for every team of this run, in parallel, each in its team's turn, after which the
// team posts no back; a team whose store's run is another's has its previous run still to
// close, so this run posts and records nothing for it.
func (e *ends) End(ctx context.Context, how team.End, teams []team.EndTeam) []error {
	errs := make([]error, len(teams))
	d, err := e.decide(how)
	if err != nil {
		for i := range errs {
			errs[i] = err
		}
		return errs
	}
	var wg sync.WaitGroup
	for i, et := range teams {
		e.markEnded(et.Team)
		wg.Go(func() {
			errs[i] = e.turn(ctx, et.Team, func() error {
				if et.Run.ID != e.cfg.Run {
					return nil
				}
				return e.end(ctx, et, d)
			})
		})
	}
	wg.Wait()
	return errs
}

// Close is EN3: the store's previous run closed at a take, judged from the room.
func (e *ends) Close(ctx context.Context, t paths.TeamID, n team.Notices, prev state.StoreRun) error {
	return e.turn(ctx, t, func() error {
		ended := prev.Ended
		switch {
		case ended == nil:
			return e.disconnected(ctx, t, n, prev)
		case ended.Notice == state.NoticeNone || ended.State == state.StateSent:
			return nil
		}
		nt, err := noticeOf(n, prev)
		if err != nil {
			return err
		}
		event, found, err := notice.LatestOwn(ctx, n, nt)
		if err != nil {
			return err
		}
		if found && (event == proto.EventLeft || event == proto.EventDisconnected) {
			return nil
		}
		return e.send(ctx, t, n, *ended, true)
	})
}

// Back is EN4: `back` when the member's latest own lifecycle notice is `disconnected`, read and
// posted in the team's turn, and nothing once the run's end or a leave has begun for the team.
func (e *ends) Back(ctx context.Context, t paths.TeamID, n team.Notices, run state.StoreRun) error {
	return e.turn(ctx, t, func() error {
		if e.hasEnded(t) {
			return nil
		}
		nt, err := noticeOf(n, state.StoreRun{ID: run.ID})
		if err != nil {
			return err
		}
		event, found, err := notice.LatestOwn(ctx, n, nt)
		if err != nil || !found || event != proto.EventDisconnected {
			return err
		}
		_, err = post(ctx, n, proto.EventBack, "", backTxn(run.ID))
		return err
	})
}

// Leave is LN8's `left` with reason released, by the keeper's txn, in the team's turn, after
// which the team posts no back.
func (e *ends) Leave(ctx context.Context, t paths.TeamID, n team.Notices, txn string) error {
	e.markEnded(t)
	return e.turn(ctx, t, func() error {
		_, err := post(ctx, n, proto.EventLeft, reasonReleased, txn)
		return err
	})
}

// turn runs fn as team t's one lifecycle step, once any step it holds has finished, or is ctx's
// error when ctx ends first.
func (e *ends) turn(ctx context.Context, t paths.TeamID, fn func() error) error {
	e.turnsMu.Lock()
	if e.turns == nil {
		e.turns = map[paths.TeamID]chan struct{}{}
	}
	held, ok := e.turns[t]
	if !ok {
		held = make(chan struct{}, 1)
		e.turns[t] = held
	}
	e.turnsMu.Unlock()
	select {
	case held <- struct{}{}:
	default:
		if e.turnWait != nil {
			e.turnWait()
		}
		select {
		case held <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	defer func() { <-held }()
	return fn()
}

// markEnded records that t's run's end or leave has begun.
func (e *ends) markEnded(t paths.TeamID) {
	e.turnsMu.Lock()
	defer e.turnsMu.Unlock()
	if e.ended == nil {
		e.ended = map[paths.TeamID]bool{}
	}
	e.ended[t] = true
}

// hasEnded is whether t's run's end or leave has begun.
func (e *ends) hasEnded(t paths.TeamID) bool {
	e.turnsMu.Lock()
	defer e.turnsMu.Unlock()
	return e.ended[t]
}

func (e *ends) switchOn() bool {
	if e.cfg.switchOverride != nil {
		return *e.cfg.switchOverride
	}
	return restartSwitch
}

// decision is what an end posts: a lifecycle event and its reason, or, with none set, nothing
// and the reason the run records.
type decision struct {
	event, reason, none string
}

// decide is B 2.11's table (EN1).
func (e *ends) decide(how team.End) (decision, error) {
	switch {
	case how.How == wire.EndRestart && e.switchOn():
		return decision{none: state.ReasonRestart}, nil
	case how.How == wire.EndLaunchFailed:
		return decision{none: state.ReasonLaunchFailed}, nil
	case !slices.Contains([]string{wire.EndRestart, wire.EndStop, wire.EndExit, wire.EndKilled, HowKeeperDied}, how.How):
		return decision{}, fmt.Errorf("%w: %q", errUnknownEnd, how.How)
	case how.SessionEndReason != nil:
		return decision{event: proto.EventLeft, reason: *how.SessionEndReason}, nil
	case how.How == wire.EndStop:
		return decision{event: proto.EventLeft, reason: reasonOther}, nil
	}
	return decision{event: proto.EventDisconnected}, nil
}

func (e *ends) end(ctx context.Context, et team.EndTeam, d decision) error {
	at := e.cfg.Clock.WallMS()
	if d.event == "" {
		return e.cfg.Engine.SetEnded(ctx, et.Team, state.RunEnd{Notice: state.NoticeNone, Reason: &d.none, State: state.StateSkipped, AtMS: at})
	}
	txn, err := newTxn()
	if err != nil {
		return err
	}
	end := state.RunEnd{Notice: d.event, State: state.StateAttempted, Txn: &txn, AtMS: at}
	if d.reason != "" {
		end.Reason = &d.reason
	}
	return e.send(ctx, et.Team, et.Notices, end, false)
}

// send records end attempted unless the store holds it so already, posts it by its txn, and
// records it sent.
func (e *ends) send(ctx context.Context, t paths.TeamID, n team.Notices, end state.RunEnd, recorded bool) error {
	end.State = state.StateAttempted
	if !recorded {
		if err := e.cfg.Engine.SetEnded(ctx, t, end); err != nil {
			return err
		}
	}
	reason := ""
	if end.Reason != nil {
		reason = *end.Reason
	}
	posted, err := post(ctx, n, end.Notice, reason, *end.Txn)
	if err != nil || !posted {
		return err
	}
	failpoint.Hit(failpoint.LinkMidEndSends)
	end.State = state.StateSent
	return e.cfg.Engine.SetEnded(ctx, t, end)
}

// disconnected closes a run that ended unseen: notice.Disconnected, as the host's call does,
// so the two post once between them.
func (e *ends) disconnected(ctx context.Context, t paths.TeamID, n team.Notices, prev state.StoreRun) error {
	nt, err := noticeOf(n, prev)
	if err != nil {
		return err
	}
	out, err := notice.Disconnected(ctx, n, nt)
	if err != nil {
		return err
	}
	if out.Why != "" {
		e.logf("ends: %s: run %s's disconnected is not possible: %s", t, prev.ID, out.Why)
	}
	return nil
}

func (e *ends) logf(format string, args ...any) {
	e.logMu.Lock()
	defer e.logMu.Unlock()
	// link.log is the log; a line it cannot take has nowhere else to go.
	if _, err := fmt.Fprintf(e.cfg.Log, format+"\n", args...); err != nil {
		return
	}
}

// post sends the member's own lifecycle notice event by txn and records it as an own send;
// false, with no error, for a probe seat, which posts nothing.
func post(ctx context.Context, n team.Notices, event, reason, txn string) (bool, error) {
	a := n.Anchors()
	handle, ok := proto.Localpart(a.Self, a.Server)
	if !ok {
		return false, errNotOurs
	}
	roster, verified := n.Roster()
	if !verified {
		r, err := notice.Roster(ctx, n, a)
		if err != nil {
			return false, err
		}
		roster = r
	}
	if roster.Members[a.Self].Role == proto.RoleProbe {
		return false, nil
	}
	to := notice.Addressees(roster, a.Self)
	out, err := proto.Composer{Server: a.Server, Self: a.Self, Token: n.Token(), Roster: roster}.Notice(
		proto.Notice{Event: event, Subject: a.Self, Reason: reason, To: to}, handle+": "+glosses[event])
	if err != nil {
		return false, err
	}
	id, err := n.Send(ctx, a.MainRoom, proto.MessageType, txn, out.Content.Compact())
	if err != nil {
		return false, err
	}
	n.RecordOwnSend(id, proto.Related{Sender: a.Self, RoomID: a.MainRoom, AddressedToRosterAgent: slices.ContainsFunc(to, roster.HumanReader)})
	return true, nil
}

var errNotOurs = errors.New("ends: the seat's user is not of the team's server")

// glosses are A 7.4's, a notice's body after the member's handle.
var glosses = map[string]string{
	proto.EventLeft:         "the member's session ended and said goodbye",
	proto.EventDisconnected: "the member's session ended without a goodbye",
	proto.EventBack:         "the member's session is running again after a disconnect",
}

// noticeOf is the notice package's view of n's member and run.
func noticeOf(n team.Notices, run state.StoreRun) (notice.Notice, error) {
	a := n.Anchors()
	handle, ok := proto.Localpart(a.Self, a.Server)
	if !ok {
		return notice.Notice{}, errNotOurs
	}
	return notice.Notice{Server: a.Server, Team: a.Team, MainRoom: a.MainRoom, Handle: handle, Token: n.Token(), Run: run.ID, SinceMS: run.StartedMS}, nil
}

// newTxn is a run end's transaction id, the store's grammar: `tc` and 32 hex from crypto/rand
// (n3c-D18).
func newTxn() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("ends: draw a transaction id: %w", err)
	}
	return "tc" + hex.EncodeToString(b), nil
}

// backTxn is a run's `back` transaction id: one per run, so a retry posts it once.
func backTxn(run paths.RunID) string { return "tc-back-" + run.String() }
