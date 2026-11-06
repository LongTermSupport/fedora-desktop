package teamsync

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// Builder is QX1's IndexBuilder: the question index built from the room's start, or caught up
// from its token, by a goroutine of its own beside the team's sync, never on its delivery path.
// One job runs at a time; Begin ends the one running. Each page's fold is judged by the team's
// own judging code under the team's judging hold, so the sync and the builder never judge at
// once, and is written in one store write with the page's items (P3).
type Builder struct {
	mu   sync.Mutex
	stop context.CancelFunc
	done chan struct{}
}

// NewBuilder is a builder with no job.
func NewBuilder() *Builder { return &Builder{} }

// Begin ends the job running and starts job, which must come from the team (qbegin).
func (b *Builder) Begin(ctx context.Context, job IndexJob) {
	b.Stop()
	if job.s == nil {
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	b.mu.Lock()
	b.stop, b.done = cancel, done
	b.mu.Unlock()
	go b.run(ctx, job, done)
}

// Stop ends the running job and waits for its goroutine.
func (b *Builder) Stop() {
	b.mu.Lock()
	stop, done := b.stop, b.done
	b.stop, b.done = nil, nil
	b.mu.Unlock()
	if stop != nil {
		stop()
		<-done
	}
}

func (b *Builder) run(ctx context.Context, job IndexJob, done chan struct{}) {
	defer close(done)
	job.s.build(ctx, job)
}

// The phases of a job: the room verified (a build), read backwards to its start (a build),
// read forwards to the head.
const (
	phaseVerify = iota
	phaseBack
	phaseForward
)

// buildRun is one job's progress, kept across a page's failures (QX1: the pages already folded
// are kept). rec is the build record, nil for a catch-up; idx is nil until it is loaded or made
// under the judging hold; stored is the job's key's state, loaded then.
type buildRun struct {
	room     string
	floor    int64
	stored   *proto.IndexState
	idx      *proto.QuestionIndex
	token    string
	last     string
	rec      *proto.BuildRecord
	handover bool
	phase    int
	resume   bool
	from     string
	oldest   []json.RawMessage
	start    string
	fresh    bool
	done     bool
}

// newRun reads the job's key (QX5's load): none is a build from the room's start, owing the
// ready line; a build record owed (no start) a build from the start with its `after` and
// `late_from`; one under way (a start) the build resumed forwards from the key's token; an
// index with a token a catch-up.
func newRun(job IndexJob) *buildRun {
	r := &buildRun{room: job.RoomID, floor: job.Floor}
	k := job.Key
	switch {
	case k == nil:
		r.rec, r.handover = &proto.BuildRecord{}, true
	case k.Build == nil && k.Index != nil && k.Token != nil && k.Last != nil:
		r.stored, r.token, r.last, r.phase, r.fresh = k.Index, *k.Token, *k.Last, phaseForward, true
	case k.Build != nil && k.Build.Start != nil && k.Index != nil && k.Token != nil && k.Last != nil:
		r.rec = &proto.BuildRecord{Start: k.Build.Start, After: k.Build.After, LateFrom: k.Build.LateFrom}
		r.stored, r.token, r.last, r.resume = k.Index, *k.Token, *k.Last, true
	case k.Build != nil:
		r.rec = &proto.BuildRecord{After: k.Build.After, LateFrom: k.Build.LateFrom}
	default:
		r.rec = &proto.BuildRecord{After: k.Last}
	}
	return r
}

// build is one job, page by page, until the head, a demotion or the team's end (ctx), or the
// room's own failure (QS8). A transient failure is tried again under SY8's backoff, a page at a
// time, the pages folded kept.
func (s *sess) build(ctx context.Context, job IndexJob) {
	t := s.t
	defer t.onBuildEnd()
	r := newRun(job)
	delay := firstBackoff
	for !r.done {
		err := s.buildStep(ctx, r)
		switch {
		case err == nil:
			delay = firstBackoff
			continue
		case ctx.Err() != nil || errors.Is(err, team.ErrRemoved):
			return
		case errors.Is(err, matrix.ErrForbidden) || errors.Is(err, matrix.ErrNotFound):
			s.quntrusted(r.room, "state")
			return
		case r.refused(err):
			t.log.Printf("the questions index's token was refused: the index is built again")
			err = s.rebuild(ctx, r)
			if err == nil {
				continue
			}
		}
		t.log.Printf("a page of the questions index was not taken; it is tried again: %s", describe(err))
		d := delay - t.cfg.Jitter(delay)
		delay = min(2*delay, maxBackoff)
		if !s.buildWait(ctx, d) {
			return
		}
	}
}

func describe(err error) string {
	if e, ok := errors.AsType[*matrix.Error](err); ok {
		return matrix.Class(e)
	}
	return err.Error()
}

// buildWait is one backoff wait; false when the job ends first.
func (s *sess) buildWait(ctx context.Context, d time.Duration) bool {
	ch, stop := s.t.cfg.Clock.Hold(d)
	defer stop()
	s.t.onBuildWait(d)
	select {
	case <-ch:
		return true
	case <-ctx.Done():
		return false
	}
}

// refused is a catch-up's first read refused by the server for good: its token is one the server
// no longer takes, so the index is built again (QX1).
func (r *buildRun) refused(err error) bool {
	e, ok := errors.AsType[*matrix.Error](err)
	return r.rec == nil && r.fresh && ok && errors.Is(err, matrix.ErrRefused) && !e.Transient()
}

// rebuild turns a refused catch-up into a build that replaces its index: items only past the
// replaced index's last folded event, and its next number kept for the reminders (QX5).
func (s *sess) rebuild(ctx context.Context, r *buildRun) error {
	return s.t.judgingHold(ctx, func() error {
		idx, err := r.index(s)
		if err != nil {
			return err
		}
		after, next := r.last, idx.Next()
		*r = buildRun{room: r.room, floor: r.floor, rec: &proto.BuildRecord{After: &after, LateFrom: &next}}
		return nil
	})
}

// buildStep is one request of the job and, for a page that ends a read, its write.
func (s *sess) buildStep(ctx context.Context, r *buildRun) error {
	switch r.phase {
	case phaseVerify:
		return s.buildVerify(ctx, r)
	case phaseBack:
		return s.buildBack(ctx, r)
	}
	return s.buildForward(ctx, r)
}

// buildVerify is a build's VerifySide over the room's state (QX1); a check that fails is QS8's.
func (s *sess) buildVerify(ctx context.Context, r *buildRun) error {
	raw, err := s.c.RoomState(ctx, r.room)
	if err != nil {
		return err
	}
	check := ""
	if herr := s.t.judgingHold(ctx, func() error {
		if verr := proto.VerifySide(proto.PurposeQuestions, r.room, proto.StateEvents(raw), s.roster, s.a); verr != nil {
			check = sideCheck(verr)
		}
		return nil
	}); herr != nil {
		return herr
	}
	if check != "" {
		s.quntrusted(r.room, check)
		r.done = true
		return nil
	}
	if r.resume {
		r.phase = phaseForward
		return nil
	}
	store, err := s.t.cfg.Engine.Load(s.t.cfg.Team, s.a.Self)
	if err != nil {
		return err
	}
	r.phase, r.from = phaseBack, store.Sync.NextBatch
	return nil
}

// sideCheck is the check a side room's VerifySide failed.
func sideCheck(verr error) string {
	if te, ok := errors.AsType[*proto.TrustError](verr); ok {
		return te.Check
	}
	return "verify"
}

// buildBack is one backward page from the team's position, keeping only the last page that held
// events; at the room's start (a page with no `end`) that page is folded oldest first.
func (s *sess) buildBack(ctx context.Context, r *buildRun) error {
	page, err := s.c.Messages(ctx, r.room, matrix.MessagesRequest{From: r.from, Dir: matrix.Backward, Limit: pageSize})
	if err != nil {
		return err
	}
	if len(page.Chunk) > 0 {
		if r.rec.Start == nil {
			start := eventID(page.Chunk[0])
			r.rec.Start = &start
		}
		r.oldest, r.start = page.Chunk, page.Start
	}
	if page.End != "" && len(page.Chunk) > 0 {
		r.from = page.End
		return nil
	}
	if len(r.oldest) == 0 {
		return errors.New("teamsync: the questions room's history holds no event")
	}
	oldestFirst := slices.Clone(r.oldest)
	slices.Reverse(oldestFirst)
	if err := s.buildPage(ctx, r, oldestFirst, r.start, false); err != nil {
		return err
	}
	r.phase, r.oldest = phaseForward, nil
	return nil
}

// buildForward is one forward page from the index's token: its events folded and written with
// the page's `end`, or, at the head (a page with no events), the last write and the index handed
// to the team.
func (s *sess) buildForward(ctx context.Context, r *buildRun) error {
	page, err := s.c.Messages(ctx, r.room, matrix.MessagesRequest{From: r.token, Dir: matrix.Forward, Limit: pageSize})
	if err != nil {
		return err
	}
	r.fresh = false
	if len(page.Chunk) == 0 {
		token := r.token
		if page.End != "" {
			token = page.End
		}
		return s.buildPage(ctx, r, nil, token, true)
	}
	if page.End == "" {
		return errors.New("teamsync: a page of the questions room's events has no end")
	}
	return s.buildPage(ctx, r, page.Chunk, page.End, false)
}

func eventID(raw json.RawMessage) string {
	v, err := proto.Parse(raw)
	if err != nil {
		return ""
	}
	return str(v, "event_id")
}

// buildPage is one page's write under the team's judging hold: the page's events judged by the
// team's judging code and folded into a copy of the index (QX2), the human posts the room owes
// taken as items (QS6), and the items, the index's state, token, last event and build record
// written in one store write; only then is the copy the run's. At the head the build record is
// null and the index is handed to the team.
func (s *sess) buildPage(ctx context.Context, r *buildRun, events []json.RawMessage, token string, head bool) error {
	return s.t.judgingHold(ctx, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		t := s.t
		idx, err := r.index(s)
		if err != nil {
			return err
		}
		j := judged{flood: copyWindow(t.flood), budget: copyWindow(t.budget), now: t.cfg.Clock.WallMS()}
		jr, err := s.judgeRoom(ctx, &j, roomJudge{room: r.room, purpose: proto.PurposeQuestions, events: events, res: t.qres, floor: r.floor})
		if err != nil {
			return err
		}
		owes, passed := r.owed(events)
		s.qtake(&j, jr, idx, r.floor, owes)
		key := r.key(idx, events, token, passed, head)
		if err := key.Check(s.a.Server); err != nil {
			return err
		}
		b := j.batch
		b.Team, b.Page, b.Questions, b.Handover = t.cfg.Team, true, &key, r.handover
		if _, err := t.cfg.Engine.Persist(ctx, b); err != nil {
			return err
		}
		s.commitJudged(j)
		s.paged(r, b, idx, passed, head)
		return nil
	})
}

// paged is a written page made the run's: its index, token and last event, `after` passed, the
// questions floor raised; at the head the index handed to the team.
func (s *sess) paged(r *buildRun, b team.Batch, idx *proto.QuestionIndex, passed, head bool) {
	r.idx, r.token, r.last, r.handover = idx, *b.Questions.Token, *b.Questions.Last, false
	if passed && r.rec != nil {
		r.rec.After = nil
	}
	if fl, ok := b.Floors[proto.PurposeQuestions]; ok && fl > r.floor {
		r.floor = fl
		s.t.qset(r.room, func(q *qstate) { q.floor = max(q.floor, fl) })
	}
	if head {
		r.done = true
		s.t.IndexAtHead(idx, r.last)
	}
}

// index is a copy of the run's index to fold a page into: the job's stored state loaded, or a
// new index for a build from the room's start; under the judging hold, which the roster needs.
func (r *buildRun) index(s *sess) (*proto.QuestionIndex, error) {
	if r.idx == nil && r.stored != nil {
		idx, err := proto.LoadQuestionIndex(s.a.Server, s.roster.QuestionFloor, *r.stored)
		if err != nil {
			return nil, err
		}
		r.idx, r.stored = idx, nil
	}
	if r.idx == nil {
		r.idx = proto.NewQuestionIndex(s.a.Server, s.roster.QuestionFloor)
	}
	return r.idx.Copy(), nil
}

// owed is which of a page's events the room owes as items, and whether the page passes the
// replaced index's last folded event (the build record's `after`, QX5): every event once it is
// passed, none before; a catch-up and a build begun with no index owe every one.
func (r *buildRun) owed(events []json.RawMessage) (func(string) bool, bool) {
	if r.rec == nil || r.rec.After == nil {
		return func(string) bool { return true }, false
	}
	after := *r.rec.After
	i := slices.IndexFunc(events, func(raw json.RawMessage) bool { return eventID(raw) == after })
	if i < 0 {
		return func(string) bool { return false }, false
	}
	owed := map[string]bool{}
	for _, raw := range events[i+1:] {
		owed[eventID(raw)] = true
	}
	return func(id string) bool { return owed[id] }, true
}

// key is the `questions` key a page's write carries: idx's state, the page's token, its newest
// event (the run's last folded one for a page with none) and the build record.
func (r *buildRun) key(idx *proto.QuestionIndex, events []json.RawMessage, token string, passed, head bool) proto.QuestionsState {
	last := r.last
	for _, raw := range slices.Backward(events) {
		if id := eventID(raw); id != "" {
			last = id
			break
		}
	}
	st := idx.State()
	return proto.QuestionsState{Index: &st, Token: &token, Last: &last, Build: r.record(passed, head)}
}

// record is the build record a page's write carries: none for a catch-up or at the head, else
// the run's, its `after` null once a page has passed it.
func (r *buildRun) record(passed, head bool) *proto.BuildRecord {
	if r.rec == nil || head {
		return nil
	}
	rec := *r.rec
	if passed {
		rec.After = nil
	}
	return &rec
}

// judgingHold runs f while the team's judging is held: the sync's answers and the builder's pages
// are judged, written and committed one at a time, so the flood window and the delivery budget
// they share are never judged twice at once. A hold that ctx ends first is ctx's error.
func (t *Team) judgingHold(ctx context.Context, f func() error) error {
	select {
	case t.judging <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-t.judging }()
	return f()
}
