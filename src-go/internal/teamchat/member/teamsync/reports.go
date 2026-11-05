package teamsync

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// reportsJoinLimit is RP1's deadline for one try of the join.
const reportsJoinLimit = 30 * time.Second

const (
	msgReportsForbidden exits.Template = "the homeserver forbids this member the reports room of team {}; tell a human"
	msgReportsUntrusted exits.Template = "the reports room of team {} fails its {} check; tell a human"
)

// errNoRoster is a reports check made before main was verified: there is no verified roster to
// judge the room against, which is not a verdict on the room.
var errNoRoster = errors.New("teamsync: main has not been verified yet")

// reportsState is the reports room as the team last found it, with the room and the roster
// revision it was found under: a state of another room or revision is not the room's.
type reportsState struct {
	room  string
	rev   int64
	state string
	check string
	// force is a start's kick, which tries a refused join again whatever the revision.
	force bool
}

// refusedJoinError is a join the homeserver refused, which membership read from the joined rooms
// did not make right (RP1).
type refusedJoinError struct {
	cause error
}

func (e *refusedJoinError) Error() string {
	return "teamsync: the reports join was refused: " + e.cause.Error()
}
func (e *refusedJoinError) Unwrap() error { return e.cause }

// reportsSess is what a try at the reports room works with: the team's client as the pair reads
// now, its anchors, and the verified roster.
type reportsSess struct {
	c      *matrix.Client
	a      proto.Anchors
	roster proto.Roster
}

// ReportsRoom is the reports room's state as last found (team.Team).
func (t *Team) ReportsRoom() team.ReportsRoom {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.verified || t.reports.state == "" || t.reports.room != t.roster.Rooms.Reports {
		return team.ReportsRoom{State: team.ReportsJoining}
	}
	return team.ReportsRoom{State: t.reports.state, Check: t.reports.check}
}

// setReports records the room's state and tells the test seam.
func (t *Team) setReports(s reportsSess, state, check string) {
	t.mu.Lock()
	t.reports.room, t.reports.rev, t.reports.state, t.reports.check = s.roster.Rooms.Reports, s.roster.Rev, state, check
	t.mu.Unlock()
	t.onReports(team.ReportsRoom{State: state, Check: check})
}

// kickReports asks the reports loop to look at the room again; force is a start's kick.
func (t *Team) kickReports(force bool) {
	t.mu.Lock()
	t.reports.force = t.reports.force || force
	t.mu.Unlock()
	select {
	case t.reportsKick <- struct{}{}:
	default:
	}
}

// reportsLoop is RP1's eager join, on the team's own goroutine so that it never holds the
// sync: each kick (a start, a roster change) tries the join, a transient failure again on SY8's
// backoff, a refusal not again until the next kick (or a report).
func (t *Team) reportsLoop(ctx context.Context) {
	for {
		select {
		case <-t.reportsKick:
		case <-ctx.Done():
			return
		}
		t.joinEager(ctx)
	}
}

func (t *Team) joinEager(ctx context.Context) {
	delay := firstBackoff
	for ctx.Err() == nil {
		if !t.reportsJoinDue() {
			return
		}
		if _, err := t.joinReports(ctx); err == nil || !transient(err) {
			return
		}
		d := delay - t.cfg.Jitter(delay)
		delay = min(2*delay, maxBackoff)
		ch, stop := t.cfg.Clock.Hold(d)
		t.onWait(d)
		select {
		case <-ch:
		case <-t.reportsKick:
		case <-ctx.Done():
		}
		stop()
	}
}

// reportsJoinDue is whether the join is to be tried: main is verified and the room is not
// joined, and either it is not refused, or it is refused under another roster revision, or a
// start forced it.
func (t *Team) reportsJoinDue() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	force := t.reports.force
	t.reports.force = false
	r := &t.reports
	switch {
	case !t.verified || t.roster.Rooms.Reports == "":
		return false
	case r.room != t.roster.Rooms.Reports || r.state == "" || r.state == team.ReportsJoining:
		return true
	case r.state == team.ReportsRefused:
		return force || r.rev != t.roster.Rev
	}
	return false
}

// transient is whether err may pass: anything but the homeserver's own refusal.
func transient(err error) bool {
	if errors.As(err, new(*refusedJoinError)) {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if me, ok := errors.AsType[*matrix.Error](err); ok {
		return me.Transient()
	}
	return true
}

// reportsSession is the client, anchors and verified roster of a try at the reports room. The
// client is the session's own, as its start read the pair: a second reading here would change
// the pair the session's 401 rule (n3c-D12) compares with.
func (t *Team) reportsSession() (reportsSess, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.verified || t.roster.Rooms.Reports == "" || t.client == nil {
		return reportsSess{}, errNoRoster
	}
	return reportsSess{c: t.client, a: anchorsOf(t.cfg.Team, t.pair), roster: t.roster}, nil
}

// bounded is ctx ended by the injected clock after d.
func (t *Team) bounded(ctx context.Context, d time.Duration) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)
	stop := t.cfg.Clock.AfterFunc(d, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}

// joinReports is one try of RP1's join, one at a time: the session when the member is in the
// room, a *refusedJoinError when the homeserver refuses it and the joined rooms do not hold it, any
// other error for a failure that may pass. The join is the restricted rule's (A 4.3, 6.3): a
// member of main joins by its own request.
func (t *Team) joinReports(ctx context.Context) (reportsSess, error) {
	select {
	case t.reportsTry <- struct{}{}:
		defer func() { <-t.reportsTry }()
	case <-ctx.Done():
		return reportsSess{}, ctx.Err()
	}
	s, err := t.reportsSession()
	if err != nil {
		return reportsSess{}, err
	}
	t.mu.Lock()
	r := t.reports
	joined := r.room == s.roster.Rooms.Reports && (r.state == team.ReportsJoined || r.state == team.ReportsUntrusted)
	t.mu.Unlock()
	if joined {
		return s, nil
	}
	jctx, done := t.bounded(ctx, reportsJoinLimit)
	defer done()
	_, err = s.c.Join(jctx, s.roster.Rooms.Reports)
	if err == nil {
		t.setReports(s, team.ReportsJoined, "")
		return s, nil
	}
	if transient(err) {
		return reportsSess{}, err
	}
	in, readErr := memberOf(jctx, s.c, s.roster.Rooms.Reports)
	switch {
	case readErr != nil:
		return reportsSess{}, readErr
	case in:
		t.setReports(s, team.ReportsJoined, "")
		return s, nil
	}
	t.setReports(s, team.ReportsRefused, errcodeOf(err))
	return reportsSess{}, &refusedJoinError{cause: err}
}

// memberOf is whether the member is in room, read from the joined rooms, never from an error's
// text.
func memberOf(ctx context.Context, c *matrix.Client, room string) (bool, error) {
	rooms, err := c.JoinedRooms(ctx)
	if err != nil {
		return false, err
	}
	return slices.Contains(rooms, room), nil
}

func errcodeOf(err error) string {
	if me, ok := errors.AsType[*matrix.Error](err); ok {
		return me.Errcode
	}
	return ""
}

// VerifyReports is RP4 (team.Team): the join first, then the room's state judged against main's
// verified roster. A refusal is an *exits.Error and leaves main as it was.
func (t *Team) VerifyReports(ctx context.Context) error {
	forbidden := exits.New(exits.CodeForbidden, &t.cfg.Team, msgReportsForbidden, exits.Team(t.cfg.Team))
	s, err := t.joinReports(ctx)
	if errors.As(err, new(*refusedJoinError)) {
		return forbidden
	}
	if err != nil {
		return err
	}
	room := s.roster.Rooms.Reports
	raw, err := s.c.RoomState(ctx, room)
	switch {
	case errors.Is(err, matrix.ErrForbidden):
		t.setReports(s, team.ReportsRefused, errcodeOf(err))
		return forbidden
	case errors.Is(err, matrix.ErrNotFound):
		raw = nil
	case err != nil:
		return err
	}
	if verr := proto.VerifySide(proto.PurposeReports, room, proto.StateEvents(raw), s.roster, s.a); verr != nil {
		check := "state"
		if trust, ok := errors.AsType[*proto.TrustError](verr); ok {
			check = trust.Check
		}
		t.setReports(s, team.ReportsUntrusted, check)
		return exits.New(exits.CodeUntrusted, &t.cfg.Team, msgReportsUntrusted, exits.Team(t.cfg.Team), exits.Words(check))
	}
	t.setReports(s, team.ReportsJoined, "")
	return nil
}

// faults is RP2: a reports event in an answer, which no filter names, is judged as A 7.6 does
// there. A roster agent's (or a human's) is a fault, a stranger's is dropped `sender` first;
// each is one line with its id, once, and never an item.
func (s *sess) faults(join map[string]matrix.RoomUpdate) {
	t := s.t
	room := s.roster.Rooms.Reports
	up, ok := join[room]
	if !ok || room == "" {
		return
	}
	judge := proto.Judge{
		Server: s.a.Server, Self: s.a.Self, RoomID: room, Purpose: proto.PurposeReports,
		Roster: s.roster, NowMs: t.cfg.Clock.WallMS(), Lookup: &memLookup{judged: map[string]bool{}}, Flood: &proto.Window{},
	}
	for _, raw := range up.Timeline.Events {
		v, err := proto.Parse(raw)
		if err != nil {
			continue
		}
		id := str(v, "event_id")
		verdict := t.judge(judge, v)
		if verdict.Outcome != proto.Fault && verdict.Outcome != proto.Drop || id == "" || t.faulted.has(id) {
			continue
		}
		t.faulted.put(id, struct{}{})
		if verdict.Outcome == proto.Fault {
			t.log.Printf("fault %s: an event in the reports room, which no filter names", id)
			continue
		}
		t.log.Printf("dropped %s: %s", id, verdict.Reason)
	}
}
