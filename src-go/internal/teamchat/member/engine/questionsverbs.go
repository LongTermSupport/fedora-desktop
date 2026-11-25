package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

const (
	msgQNotCoordinator  exits.Template = "this member is not the coordinator of team {}, so it posts nothing in questions; ask the coordinator with teamchat say --to-coordinator"
	msgQNoneCoordinated exits.Template = "this member coordinates no team held, so it keeps no questions; run status"
	msgAskNoText        exits.Template = "ask-human reads the question on stdin; teamchat ask-human --skeleton prints its form"
	msgThreadNoText     exits.Template = "thread reads its post on stdin"
	msgRetitleNoText    exits.Template = "retitle reads the new headline on stdin"
	msgAskRemind        exits.Template = "--remind-after is 600 to 604800 seconds"
	msgAskTooMany       exits.Template = "--for and --re name at most 16 each"
	msgAskRe            exits.Template = "--re names event ids"
	msgAskForHuman      exits.Template = "{} is a human; --for names the seats that asked"
	msgQForm            exits.Template = "the question does not follow the form (a headline, then Context: and Question: and Why a human: sections, then Options: numbered 1. to 9.); teamchat ask-human --skeleton prints it"
	msgQWhy             exits.Template = "the Why a human: section starts with one of decision, physical, credential, preference"
	msgQDuplicate       exits.Template = "{} is open with the same question; post in its thread with teamchat thread"
	msgQOpenLimit       exits.Template = "team {} has 64 questions open; close some first"
	msgQBlank           exits.Template = "a thread post that does not close its question has text"
	msgQHeadline        exits.Template = "the headline is one line of 1 to 120 characters"
	msgQName            exits.Template = "{} is not a question name; name one as Q<n>"
	msgQNotKept         exits.Template = "team {} keeps no question {}; run teamchat questions --all"
	msgQClosed          exits.Template = "{} is closed already"
	msgQOtherSeat       exits.Template = "{} was asked by another seat; only its sender can edit it"
	msgQUnposted        exits.Template = "{} is not posted yet; retitle it once it is"
	msgQNotHuman        exits.Template = "team {} has no human post {} in questions"
	msgQBumpUsage       exits.Template = "bump takes question names or --waiting, and a --min-age of 0 or more"
	msgQPartial         exits.Template = "questions answered for some teams only; read the team-failed records"
)

// digestCap is the digest's bound in bytes (DESIGN-questions-v2 2.6).
const digestCap = 1500

// questionsTeam is the team a questions verb acts on (2.x): resolved as a report's, and only a
// team this member coordinates (11 role otherwise).
func (e *Engine) questionsTeam(ctx context.Context, flag *paths.TeamID) (sendTeam, error) {
	st, err := e.reportingTeam(ctx, flag)
	if err != nil {
		return sendTeam{}, err
	}
	if c, ok := st.roster.Coordinator(); !ok || c != st.self {
		return sendTeam{}, exits.New(exits.CodeRole, &st.id, msgQNotCoordinator, exits.Team(st.id))
	}
	return st, nil
}

// qcompose builds one questions post from the composer and the index's view.
type qcompose func(c proto.Composer, v team.QuestionsView) (proto.Outgoing, error)

// qsent is a questions post as the sender answered it: its event id, nil when queued, and the
// charge time.
type qsent struct {
	id    *string
	since int64
}

// qpost is one questions post (QX4): composed under the team's compose hold, sized, charged
// in one store write and placed on the index before the hold is let go, then posted or
// queued by the deadline. With check, nothing is charged or sent.
func (e *Engine) qpost(ctx context.Context, st sendTeam, kind string, check bool, compose qcompose) (qsent, team.QuestionsView, error) {
	var (
		lk    storeLookup
		lkErr error
	)
	if err := e.do(ctx, func() { lk, lkErr = e.sendable(st.id) }); err != nil {
		return qsent{}, team.QuestionsView{}, exits.New(exits.CodeTimedOut, &st.id, msgTimedOut, exits.Team(st.id))
	}
	if lkErr != nil {
		return qsent{}, team.QuestionsView{}, lkErr
	}
	var (
		entry     state.Sending
		wait      chan sendResult
		queuedNow bool
		checked   team.QuestionsView
	)
	view, err := st.tm.Questions(ctx, func(v team.QuestionsView) ([]team.Placement, error) {
		c := proto.Composer{
			Server: st.anchors.Server, Self: st.self, Token: st.tm.Notices().Token(), Roster: st.roster,
			Lookup: lk.with(st.roster), Questions: v.Index, NowMs: e.cfg.Clock.WallMS(),
		}
		o, err := compose(c, v)
		if err != nil {
			return nil, refusal(st.id, err)
		}
		ent, d, ok := fitEntry(st, kind, o)
		if !ok {
			return nil, d.Error
		}
		if check {
			checked = v
			return nil, nil
		}
		var chargeErr error
		if err := e.do(ctx, func() { wait, queuedNow, chargeErr = e.charge(st, c, o, &ent, false) }); err != nil {
			return nil, err
		}
		if chargeErr != nil {
			return nil, e.chargeFailure(st.id, chargeErr)
		}
		entry = ent
		if p, ok := team.PlacementOf(ent.Txn, ent.Content); ok {
			return []team.Placement{p}, nil
		}
		return nil, nil
	})
	switch {
	case err != nil && ended(err):
		return qsent{}, team.QuestionsView{}, exits.New(exits.CodeTimedOut, &st.id, msgTimedOut, exits.Team(st.id))
	case err != nil:
		return qsent{}, team.QuestionsView{}, err
	case check:
		return qsent{}, checked, nil
	case queuedNow:
		return qsent{since: entry.SinceMS}, view, nil
	}
	r, ok := e.awaitResult(ctx, st.id, entry.Txn, wait)
	switch {
	case !ok:
		return qsent{since: entry.SinceMS}, view, nil
	case r.err != nil:
		return qsent{}, team.QuestionsView{}, r.err
	}
	return qsent{id: &r.id, since: entry.SinceMS}, view, nil
}

// chargeFailure is a charge's refusal as the op's error: its own, else the store's.
func (e *Engine) chargeFailure(t paths.TeamID, err error) error {
	if xe, ok := errors.AsType[*exits.Error](err); ok {
		return xe
	}
	e.logf("team %s: a questions post was not charged: %v", t, err)
	return exits.New(exits.CodeStore, &t, msgStoreWrite, exits.Team(t))
}

// questionN is a Q<n> argument's number, 64 for a malformed one.
func questionN(t *paths.TeamID, name string) (int64, error) {
	n, ok := proto.ParseQuestion(name)
	if !ok {
		return 0, exits.New(exits.CodeUsage, t, msgQName, exits.Name(name))
	}
	return int64(n), nil
}

// keptQuestion is Q<n> as the index keeps it: 23 for none or a redacted one.
func keptQuestion(st sendTeam, v team.QuestionsView, n int64) (proto.IndexedQuestion, error) {
	q, ok := v.Index.Question(n)
	if !ok || q.Redacted() {
		return proto.IndexedQuestion{}, exits.New(exits.CodeNotFound, &st.id, msgQNotKept, exits.Team(st.id), exits.Name(proto.QuestionName(int(n))))
	}
	return q, nil
}

// questionRecord is the question record of q (QL1, 2.6).
func questionRecord(st sendTeam, q proto.IndexedQuestion, id *string) records.Question {
	n := q.N
	if q.Redacted() {
		return records.Question{Team: st.id, N: &n, State: proto.QuestionClosed, Redacted: true}
	}
	nm := st.namer()
	r := records.Question{
		Team: st.id, N: &n, State: q.State(), ID: id, Headline: q.Headline,
		AskedBy: names(nm, q.AskedBy, nil), For: names(nm, q.AskedBy, nil), Re: records.List[string]{},
		Unanswered: records.List[string]{}, UnansweredMore: q.UnansweredMore, Why: optional(q.Why),
	}
	r.Re = append(r.Re, q.Re...)
	r.Unanswered = append(r.Unanswered, q.Unanswered...)
	if r.ID == nil {
		r.ID = optional(q.EventID)
	}
	if q.AskedMs > 0 {
		r.AskedMS = &q.AskedMs
	}
	if q.RemindAfterS > 0 {
		r.RemindAfterS = &q.RemindAfterS
	}
	if q.Waiting() {
		r.WaitingSinceMS = &q.WaitingMs
	}
	return r
}

// questionAfter is Q<n>'s record after an own post: the index's view, the post's id when it is
// the question, and the charge time as the waiting time of a post that waits on a human (the
// provisional overlay does not set it).
func questionAfter(st sendTeam, v team.QuestionsView, n int64, s qsent, root, waits bool) records.Question {
	q, ok := v.Index.Question(n)
	if !ok {
		return records.Question{Team: st.id, N: &n, State: proto.QuestionOpen, AskedBy: records.List[string]{}, Re: records.List[string]{}, Unanswered: records.List[string]{}, For: records.List[string]{}}
	}
	var id *string
	if root {
		id = s.id
	}
	r := questionRecord(st, q, id)
	if waits && !q.Closed {
		since := s.since
		r.WaitingSinceMS = &since
	}
	return r
}

// writeRecords writes rs and flushes; the op's done.
func writeRecords(out RecordWriter, rs ...records.Record) wire.Done {
	for _, r := range rs {
		if err := out.Record(r); err != nil {
			return internal(err)
		}
	}
	if err := out.Flush(); err != nil {
		return internal(err)
	}
	return wire.Done{}
}

func sentRecord(t paths.TeamID, s qsent) records.Sent {
	return records.Sent{Team: t, ID: s.id, Room: proto.PurposeQuestions, Queued: s.id == nil}
}

// askHuman is 2.1: the coordinator's question, numbered by the index.
func (e *Engine) askHuman(ctx context.Context, a wire.AskArgs, stdin *string, whole int64, out RecordWriter) wire.Done {
	if err := askUsage(a); err != nil {
		return doneOf(err)
	}
	st, err := e.questionsTeam(ctx, a.Team)
	if err != nil {
		return doneOf(err)
	}
	if stdin == nil {
		return doneOf(exits.New(exits.CodeUsage, &st.id, msgAskNoText))
	}
	text := *stdin
	asked, err := askedBy(st, a.For)
	if err != nil {
		return doneOf(err)
	}
	post := proto.QuestionPost{Text: text, AskedBy: asked, Re: a.Re}
	if a.RemindAfterS != nil {
		post.RemindAfterS = *a.RemindAfterS
	}
	var n int64
	s, v, err := e.qpost(ctx, st, state.SendQuestion, a.Check, func(c proto.Composer, v team.QuestionsView) (proto.Outgoing, error) {
		n = v.Index.Next()
		o, err := c.Question(post)
		if err != nil {
			return o, askError(st, n, text, whole, v, err)
		}
		if v.Index.Open() >= proto.MaxOpenQuestions {
			return o, exits.New(exits.CodeOpenLimit, &st.id, msgQOpenLimit, exits.Team(st.id))
		}
		return o, nil
	})
	if err != nil {
		return doneOf(err)
	}
	if a.Check {
		return writeRecords(out, checkedQuestion(st, n, text, asked))
	}
	return writeRecords(out, sentRecord(st.id, s), questionAfter(st, v, n, s, true, true))
}

// askUsage is ask-human's arguments checked before any team is read.
func askUsage(a wire.AskArgs) error {
	switch {
	case a.RemindAfterS != nil && (*a.RemindAfterS < proto.MinRemindS || *a.RemindAfterS > proto.MaxRemindS):
		return exits.New(exits.CodeUsage, a.Team, msgAskRemind)
	case len(a.For) > proto.MaxAskedBy || len(a.Re) > proto.MaxAskedBy:
		return exits.New(exits.CodeUsage, a.Team, msgAskTooMany)
	case slices.ContainsFunc(a.Re, func(id string) bool { return !proto.ValidEventID(id) }):
		return exits.New(exits.CodeUsage, a.Team, msgAskRe)
	}
	return nil
}

// askedBy is --for's seats as user ids: each one agent of the roster.
func askedBy(st sendTeam, seats []string) ([]string, error) {
	ids := []string{}
	for _, name := range seats {
		found := members(st.roster, st.anchors.Server, name)
		switch {
		case len(found) == 0:
			return nil, exits.New(exits.CodeUnknownMember, &st.id, msgUnknownMember, exits.Name(name))
		case len(found) > 1:
			return nil, exits.New(exits.CodeAmbiguousMember, &st.id, msgAmbiguousMember, exits.Name(name), exits.Team(st.id))
		case st.roster.IsHuman(found[0]):
			return nil, exits.New(exits.CodeUnknownMember, &st.id, msgAskForHuman, exits.Name(name))
		}
		if !slices.Contains(ids, found[0]) {
			ids = append(ids, found[0])
		}
	}
	return ids, nil
}

// askError is a question's refusal as the exit it is (2.1, #365): a size names both sizes, the
// form and the why say which rule, a duplicate names the open question.
func askError(st sendTeam, n int64, text string, whole int64, v team.QuestionsView, err error) error {
	re, ok := errors.AsType[*proto.RefusalError](err)
	if !ok {
		return err
	}
	prefix := int64(len(proto.QuestionName(int(n)) + " "))
	switch {
	case re.Reason == proto.ReasonBody && prefix+int64(len(text)) > proto.MaxQuestionBody:
		return exits.TooLarge(exits.CodeBody, &st.id, prefix+max(whole, int64(len(text))), proto.MaxQuestionBody)
	case re.Reason == proto.ReasonBody && strings.Contains(re.Detail, proto.SectionWhy):
		return exits.New(exits.CodeBody, &st.id, msgQWhy)
	case re.Reason == proto.ReasonBody:
		return exits.New(exits.CodeBody, &st.id, msgQForm)
	case re.Reason == proto.ReasonDuplicate:
		if open, dup := openDuplicate(v, n, text); dup {
			return exits.New(exits.CodeDuplicate, &st.id, msgQDuplicate, exits.Name(proto.QuestionName(int(open))))
		}
	}
	return err
}

// openDuplicate is the open question that asks what text asks.
func openDuplicate(v team.QuestionsView, n int64, text string) (int64, bool) {
	_, section, err := proto.QuestionForm(n, proto.QuestionName(int(n))+" "+text)
	if err != nil {
		return 0, false
	}
	return v.Index.OpenDuplicate(section)
}

// checkedQuestion is --check's record: the question as it would be posted, n null.
func checkedQuestion(st sendTeam, n int64, text string, asked []string) records.Question {
	nm := st.namer()
	r := records.Question{
		Team: st.id, State: proto.QuestionOpen, AskedBy: names(nm, asked, nil), For: names(nm, asked, nil),
		Re: records.List[string]{}, Unanswered: records.List[string]{},
	}
	if headline, _, err := proto.QuestionForm(n, proto.QuestionName(int(n))+" "+text); err == nil {
		r.Headline = headline
	}
	if why, err := proto.WhyOf(text); err == nil {
		r.Why = &why
	}
	return r
}

// thread is 2.2: a post in Q<n>'s thread, notifying unless --note.
func (e *Engine) thread(ctx context.Context, a wire.ThreadArgs, stdin *string, whole int64, out RecordWriter) wire.Done {
	n, err := questionN(a.Team, a.Q)
	if err != nil {
		return doneOf(err)
	}
	st, err := e.questionsTeam(ctx, a.Team)
	if err != nil {
		return doneOf(err)
	}
	if stdin == nil {
		return doneOf(exits.New(exits.CodeUsage, &st.id, msgThreadNoText))
	}
	text := *stdin
	s, v, err := e.qpost(ctx, st, state.SendThread, false, func(c proto.Composer, v team.QuestionsView) (proto.Outgoing, error) {
		if _, err := keptQuestion(st, v, n); err != nil {
			return proto.Outgoing{}, err
		}
		o, err := c.Thread(proto.ThreadPost{N: n, Text: text, Note: a.Note})
		return o, threadError(st, text, whole, err)
	})
	if err != nil {
		return doneOf(err)
	}
	return writeRecords(out, sentRecord(st.id, s), questionAfter(st, v, n, s, false, !a.Note))
}

// threadError is a thread post's refusal: a size names both sizes, a blank post says so.
func threadError(st sendTeam, text string, whole int64, err error) error {
	re, ok := errors.AsType[*proto.RefusalError](err)
	switch {
	case !ok || re.Reason != proto.ReasonBody:
		return err
	case len(text) > proto.MaxThreadBody:
		return exits.TooLarge(exits.CodeBody, &st.id, max(whole, int64(len(text))), proto.MaxThreadBody)
	}
	return exits.New(exits.CodeBody, &st.id, msgQBlank)
}

// replyQuestions is QR1 with the link-back (2.3): a human post attached to Q<n> is answered in
// Q<n>'s thread, then, when the post is not in that thread, linked back to at the top level; an
// unattached post is answered in its own thread, or in a new one rooted at it.
func (e *Engine) replyQuestions(ctx context.Context, st sendTeam, id, text string, out RecordWriter) wire.Done {
	v, err := st.tm.Refetch(ctx, proto.PurposeQuestions, id)
	if err != nil {
		return doneOf(fetchErr(ctx, st.id, id, err))
	}
	// An edit answers as the post it revises (R10): the homeserver refuses a thread rooted at
	// an edit, and the human's thread is the original's (A 7.5 accepts an edit only of a post
	// of the same sender in this room).
	if original := v.Human.Revises; v.Class == proto.SenderHuman && original != "" {
		id = original
		if v, err = st.tm.Refetch(ctx, proto.PurposeQuestions, id); err != nil {
			return doneOf(fetchErr(ctx, st.id, id, err))
		}
	}
	if v.Class != proto.SenderHuman || v.Event.Sender == "" {
		return doneOf(exits.New(exits.CodeNotFound, &st.id, msgQNotHuman, exits.Team(st.id), exits.Name(id)))
	}
	human := v.Event.Sender
	var (
		n        int64
		linkBack bool
	)
	s, view, err := e.qpost(ctx, st, state.SendThread, false, func(c proto.Composer, qv team.QuestionsView) (proto.Outgoing, error) {
		attached, known := qv.Index.Attachment(id)
		if !known {
			return proto.Outgoing{}, exits.New(exits.CodeNotFound, &st.id, msgQNotHuman, exits.Team(st.id), exits.Name(id))
		}
		n = attached
		post := proto.ThreadPost{N: attached, Answers: id, To: human, Text: text}
		if attached == 0 {
			post.Root = v.Human.ThreadRoot
		} else if q, ok := qv.Index.Question(attached); ok {
			linkBack = v.Human.ThreadRoot != q.EventID
		}
		o, err := c.Thread(post)
		return o, threadError(st, text, int64(len(text)), err)
	})
	if err != nil {
		return doneOf(err)
	}
	recs := []records.Record{sentRecord(st.id, s)}
	if !linkBack || s.id == nil {
		return writeRecords(out, recs...)
	}
	answer := *s.id
	link, _, err := e.qpost(ctx, st, state.SendThread, false, func(c proto.Composer, _ team.QuestionsView) (proto.Outgoing, error) {
		return c.LinkBack(proto.LinkPost{N: n, Post: id, Link: answer, To: human, Permalink: proto.Permalink(view.Room, answer)})
	})
	if err != nil {
		e.logf("team %s: the link-back of %s was not sent: %v", st.id, answer, err)
		return doneOf(err)
	}
	return writeRecords(out, append(recs, sentRecord(st.id, link))...)
}

// closeQuestions is 2.4: each named question closed by a quiet post, then retitled when this
// seat posted its root. Every name is judged before any send.
func (e *Engine) closeQuestions(ctx context.Context, a wire.CloseArgs, stdin *string, out RecordWriter) wire.Done {
	ns := make([]int64, 0, len(a.Qs))
	for _, name := range a.Qs {
		n, err := questionN(a.Team, name)
		if err != nil {
			return doneOf(err)
		}
		ns = append(ns, n)
	}
	st, err := e.questionsTeam(ctx, a.Team)
	if err != nil {
		return doneOf(err)
	}
	note := ""
	if stdin != nil {
		note = *stdin
	}
	if _, err := st.tm.Questions(ctx, func(v team.QuestionsView) ([]team.Placement, error) {
		for _, n := range ns {
			q, err := keptQuestion(st, v, n)
			if err != nil {
				return nil, err
			}
			if q.Closed {
				return nil, exits.New(exits.CodeTarget, &st.id, msgQClosed, exits.Name(proto.QuestionName(int(n))))
			}
		}
		return nil, nil
	}); err != nil {
		return doneOf(err)
	}
	var recs []records.Record
	for _, n := range ns {
		rs, err := e.closeOne(ctx, st, n, note)
		if err != nil {
			return doneOf(err)
		}
		recs = append(recs, rs...)
	}
	return writeRecords(out, recs...)
}

// closeOne is one question's closing post, its retitle when this seat posted its root, and
// its question record.
func (e *Engine) closeOne(ctx context.Context, st sendTeam, n int64, note string) ([]records.Record, error) {
	s, v, err := e.qpost(ctx, st, state.SendThread, false, func(c proto.Composer, _ team.QuestionsView) (proto.Outgoing, error) {
		o, err := c.Thread(proto.ThreadPost{N: n, Text: note, Close: true})
		return o, threadError(st, note, int64(len(note)), err)
	})
	if err != nil {
		return nil, err
	}
	recs := []records.Record{sentRecord(st.id, s)}
	retitled := false
	if q, ok := v.Index.Question(n); ok && q.From == st.self && q.EventID != "" {
		rs, rv, err := e.retitlePost(ctx, st, n, q.Headline)
		if err != nil {
			return nil, err
		}
		recs = append(recs, sentRecord(st.id, rs))
		v, retitled = rv, true
	}
	r := questionAfter(st, v, n, s, false, false)
	r.Retitled = &retitled
	return append(recs, r), nil
}

// retitlePost fetches Q<n>'s root and posts its retitle edit under headline.
func (e *Engine) retitlePost(ctx context.Context, st sendTeam, n int64, headline string) (qsent, team.QuestionsView, error) {
	var root string
	if _, err := st.tm.Questions(ctx, func(v team.QuestionsView) ([]team.Placement, error) {
		q, err := keptQuestion(st, v, n)
		switch {
		case err != nil:
			return nil, err
		case q.From != st.self:
			return nil, exits.New(exits.CodeTarget, &st.id, msgQOtherSeat, exits.Name(proto.QuestionName(int(n))))
		case q.EventID == "":
			return nil, exits.New(exits.CodeNotReady, &st.id, msgQUnposted, exits.Name(proto.QuestionName(int(n))))
		}
		root = q.EventID
		return nil, nil
	}); err != nil {
		return qsent{}, team.QuestionsView{}, err
	}
	raw, err := st.tm.FetchEvent(ctx, root)
	if err != nil {
		return qsent{}, team.QuestionsView{}, fetchErr(ctx, st.id, root, err)
	}
	parsed, err := proto.Parse(raw)
	if err != nil {
		return qsent{}, team.QuestionsView{}, exits.New(exits.CodeUntrusted, &st.id, msgQNotKept, exits.Team(st.id), exits.Name(proto.QuestionName(int(n))))
	}
	original, ok := proto.DecodeEvent(parsed)
	if !ok {
		return qsent{}, team.QuestionsView{}, exits.New(exits.CodeUntrusted, &st.id, msgQNotKept, exits.Team(st.id), exits.Name(proto.QuestionName(int(n))))
	}
	return e.qpost(ctx, st, state.SendRetitle, false, func(c proto.Composer, _ team.QuestionsView) (proto.Outgoing, error) {
		o, err := c.Retitle(proto.RetitlePost{N: n, Headline: headline, Original: original})
		if re, ok := errors.AsType[*proto.RefusalError](err); ok && re.Reason == proto.ReasonBody {
			return o, exits.New(exits.CodeBody, &st.id, msgQHeadline)
		}
		return o, err
	})
}

// retitle is 2.5: an own question's new headline, from stdin.
func (e *Engine) retitle(ctx context.Context, a wire.RetitleArgs, stdin *string, out RecordWriter) wire.Done {
	n, err := questionN(a.Team, a.Q)
	if err != nil {
		return doneOf(err)
	}
	st, err := e.questionsTeam(ctx, a.Team)
	if err != nil {
		return doneOf(err)
	}
	if stdin == nil {
		return doneOf(exits.New(exits.CodeUsage, &st.id, msgRetitleNoText))
	}
	s, v, err := e.retitlePost(ctx, st, n, *stdin)
	if err != nil {
		return doneOf(err)
	}
	r := questionAfter(st, v, n, s, false, false)
	retitled := true
	r.Retitled = &retitled
	return writeRecords(out, sentRecord(st.id, s), r)
}

// bumpTarget is one question bump judged: why it is skipped, "" for a bump.
type bumpTarget struct {
	n        int64
	headline string
	why      string
}

// bump is 2.7: a notifying thread post for each named, or every waiting, question that has
// waited at least --min-age; the others are skipped with why. The sent records come first,
// then every bump record in question order.
func (e *Engine) bump(ctx context.Context, a wire.BumpArgs, out RecordWriter) wire.Done {
	if (len(a.Qs) > 0) == a.Waiting || a.MinAgeS < 0 {
		return doneOf(exits.New(exits.CodeUsage, a.Team, msgQBumpUsage))
	}
	ns := make([]int64, 0, len(a.Qs))
	for _, name := range a.Qs {
		n, err := questionN(a.Team, name)
		if err != nil {
			return doneOf(err)
		}
		ns = append(ns, n)
	}
	st, err := e.questionsTeam(ctx, a.Team)
	if err != nil {
		return doneOf(err)
	}
	targets, err := e.bumpTargets(ctx, st, ns, a)
	if err != nil {
		return doneOf(err)
	}
	var sent, bumps []records.Record
	for _, j := range targets {
		name := proto.QuestionName(int(j.n))
		if j.why != "" {
			bumps = append(bumps, records.Bump{Team: st.id, Q: name, Why: &j.why})
			continue
		}
		text := name + " still waiting: " + j.headline
		if a.Note != nil && *a.Note != "" {
			text += "\n\n" + *a.Note
		}
		s, _, err := e.qpost(ctx, st, state.SendThread, false, func(c proto.Composer, _ team.QuestionsView) (proto.Outgoing, error) {
			o, err := c.Thread(proto.ThreadPost{N: j.n, Text: text})
			return o, threadError(st, text, int64(len(text)), err)
		})
		if err != nil {
			return doneOf(err)
		}
		sent = append(sent, sentRecord(st.id, s))
		bumps = append(bumps, records.Bump{Team: st.id, Q: name, Bumped: true})
	}
	return writeRecords(out, append(sent, bumps...)...)
}

// bumpTargets judges bump's questions under one compose hold: every name kept (23 otherwise).
func (e *Engine) bumpTargets(ctx context.Context, st sendTeam, ns []int64, a wire.BumpArgs) ([]bumpTarget, error) {
	var targets []bumpTarget
	now := e.cfg.Clock.WallMS()
	_, err := st.tm.Questions(ctx, func(v team.QuestionsView) ([]team.Placement, error) {
		if a.Waiting {
			ns = waitingNumbers(v.Index)
		}
		for _, n := range ns {
			q, err := keptQuestion(st, v, n)
			if err != nil {
				return nil, err
			}
			j := bumpTarget{n: n, headline: q.Headline}
			switch {
			case !q.Waiting():
				j.why = records.BumpAnswered
			case now-q.WaitingMs < a.MinAgeS*1000:
				j.why = records.BumpYoung
			}
			targets = append(targets, j)
		}
		return nil, nil
	})
	return targets, err
}

// waitingNumbers are the index's waiting questions, oldest waiting first.
func waitingNumbers(x *proto.QuestionIndex) []int64 {
	var qs []proto.IndexedQuestion
	for n := int64(1); n < x.Next(); n++ {
		if q, ok := x.Question(n); ok && !q.Redacted() && q.Waiting() {
			qs = append(qs, q)
		}
	}
	slices.SortStableFunc(qs, func(a, b proto.IndexedQuestion) int { return cmpInt64(a.WaitingMs, b.WaitingMs) })
	ns := make([]int64, 0, len(qs))
	for _, q := range qs {
		ns = append(ns, q.N)
	}
	return ns
}

func cmpInt64(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// questions is QL1 with 2.6's --waiting, --for and --digest: the questions of each team this
// member coordinates (the flag's, or every one held).
func (e *Engine) questions(ctx context.Context, a wire.QuestionsArgs, out RecordWriter) wire.Done {
	if a.Digest {
		return e.digest(ctx, a.Team, out)
	}
	teams, err := e.coordinated(ctx, a.Team)
	if err != nil {
		return doneOf(err)
	}
	var (
		recs   []records.Record
		failed []error
	)
	for _, st := range teams {
		rs, err := e.teamQuestions(ctx, st, a)
		if err != nil {
			failed = append(failed, err)
			recs = append(recs, records.TeamFailedOf(st.id, err))
			continue
		}
		recs = append(recs, rs...)
	}
	switch {
	case len(failed) == len(teams):
		return doneOf(failed[0])
	case len(failed) > 0:
		if d := writeRecords(out, recs...); d.Error != nil {
			return d
		}
		return doneOf(exits.New(exits.CodePartial, nil, msgQPartial))
	}
	return writeRecords(out, recs...)
}

// coordinated are the held teams this member coordinates by their verified rosters: with the
// flag that team (11 when it does not), else every one (11 for none).
func (e *Engine) coordinated(ctx context.Context, flag *paths.TeamID) ([]sendTeam, error) {
	if flag != nil {
		st, err := e.questionsTeam(ctx, flag)
		if err != nil {
			return nil, err
		}
		return []sendTeam{st}, nil
	}
	cands, err := e.candidates(ctx, nil)
	if err != nil {
		return nil, err
	}
	var teams []sendTeam
	for _, c := range cands {
		r, ok, verified := c.roster()
		if coord, has := r.Coordinator(); ok && verified && has && coord == c.self {
			teams = append(teams, sendTeam{id: c.id, tm: c.tm, roster: r, self: c.self, anchors: c.anchors})
		}
	}
	if len(teams) == 0 {
		return nil, exits.New(exits.CodeRole, nil, msgQNoneCoordinated)
	}
	return teams, nil
}

// unattachedMs is how far back questions lists unattached human posts (QL1): 14 days.
const unattachedMs = 14 * 24 * 3600 * 1000

// maxListed is QL1's bound on the records of one team, after which a more record follows.
const maxListed = 256

// indexView is the team's questions index at the head, read under its compose hold.
func indexView(ctx context.Context, st sendTeam) (team.QuestionsView, error) {
	var view team.QuestionsView
	_, err := st.tm.Questions(ctx, func(v team.QuestionsView) ([]team.Placement, error) {
		view = v
		return nil, nil
	})
	return view, err
}

// teamQuestions are one team's records.
func (e *Engine) teamQuestions(ctx context.Context, st sendTeam, a wire.QuestionsArgs) ([]records.Record, error) {
	view, err := indexView(ctx, st)
	if err != nil {
		return nil, err
	}
	forID := ""
	if a.For != nil {
		ids, err := askedBy(st, []string{*a.For})
		if err != nil {
			return nil, err
		}
		forID = ids[0]
	}
	x := view.Index
	var ns []int64
	if a.Waiting {
		ns = waitingNumbers(x)
	} else {
		for n := int64(1); n < x.Next(); n++ {
			ns = append(ns, n)
		}
	}
	var recs []records.Record
	for _, n := range ns {
		q, ok := x.Question(n)
		switch {
		case !ok, !a.All && !a.Waiting && q.Closed, forID != "" && !slices.Contains(q.AskedBy, forID):
			continue
		}
		recs = append(recs, questionRecord(st, q, nil))
	}
	if !a.Waiting && forID == "" {
		since := e.cfg.Clock.WallMS() - unattachedMs
		for _, u := range x.Unattached() {
			if u.SentMs >= since {
				recs = append(recs, records.Unattached{Team: st.id, ID: u.ID, From: st.namer().name(u.From), SentMS: u.SentMs})
			}
		}
	}
	if len(recs) > maxListed {
		recs = append(recs[:maxListed], records.More{Pending: int64(len(recs) - maxListed)})
	}
	return recs, nil
}

// digest is 2.6's --digest: one quiet report of what waits on the humans, posted to the reports
// room; nothing waiting posts nothing.
func (e *Engine) digest(ctx context.Context, flag *paths.TeamID, out RecordWriter) wire.Done {
	st, err := e.questionsTeam(ctx, flag)
	if err != nil {
		return doneOf(err)
	}
	view, err := indexView(ctx, st)
	if err != nil {
		return doneOf(err)
	}
	text, ok := digestText(st, view.Index, e.cfg.Clock.WallMS())
	if !ok {
		return writeRecords(out)
	}
	return e.send(ctx, st, state.SendReport, func(c proto.Composer) (proto.Outgoing, error) { return c.Report(text) }, out)
}

// digestText is the digest's report: the headline, then a line per waiting question, cut at
// digestCap bytes with `and K more`; false when nothing waits.
func digestText(st sendTeam, x *proto.QuestionIndex, now int64) (string, bool) {
	ns := waitingNumbers(x)
	if len(ns) == 0 {
		return "", false
	}
	noun := "questions"
	if len(ns) == 1 {
		noun = "question"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Waiting on you: %d %s", len(ns), noun)
	for i, n := range ns {
		q, ok := x.Question(n)
		if !ok {
			continue
		}
		line := "\n" + proto.QuestionName(int(n)) + " (" + age(now-q.WaitingMs)
		if len(q.AskedBy) > 0 {
			line += ", for " + strings.Join(names(st.namer(), q.AskedBy, nil), ", ")
		}
		line += "): " + q.Headline
		rest := fmt.Sprintf("\nand %d more", len(ns)-i)
		if b.Len()+len(line)+len(rest) > digestCap {
			b.WriteString(rest)
			break
		}
		b.WriteString(line)
	}
	return b.String(), true
}

// age is a duration in ms as the digest writes it: minutes, hours, then days.
func age(ms int64) string {
	switch m := ms / 60_000; {
	case m < 60:
		return fmt.Sprintf("%dm", max(m, 0))
	case m < 48*60:
		return fmt.Sprintf("%dh", m/60)
	default:
		return fmt.Sprintf("%dd", m/(24*60))
	}
}
