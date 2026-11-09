package proto

import (
	"errors"
	"fmt"
	"slices"
)

// ErrNotPlaced is a provisional entry (QX3) that cannot be placed: the number is not the next,
// the question it answers is not indexed, or its transaction is already placed.
var ErrNotPlaced = errors.New("proto: the own post cannot be placed in the question index")

// PlacedThread is an own thread post of the coordinator, placed provisionally: a post in
// question N's thread (Close when it closes it), or, with N 0, a plain reply to the human post
// ReplyTo (A 10.3).
type PlacedThread struct {
	N       int64
	Close   bool
	ReplyTo string
}

// provisional is an own post placed when its send is recorded and replaced, in stream order,
// when its echo is folded (QX3). It is in no stored state: a restart places it again.
type provisional struct {
	txn, sender string
	isQuestion  bool
	q           IndexedQuestion
	thread      PlacedThread
}

func (p provisional) clone() provisional {
	p.q = p.q.clone()
	return p
}

// echoes is whether the folded content is this entry's own post, which the sender and the
// content's shape tell: sends are one at a time per team and in order, so the first entry of a
// shape is the one an echo of that shape belongs to.
func (p provisional) echoes(c Content) bool {
	if p.isQuestion {
		return c.Key == KeyQuestion && c.Question.N == p.q.N
	}
	t := c.Thread
	return c.Key == KeyThread && t.N == p.thread.N && t.Close == p.thread.Close &&
		(t.N != 0 || c.Relation.InReplyTo == p.thread.ReplyTo)
}

func (x *QuestionIndex) placed(txn string) bool {
	return slices.ContainsFunc(x.prov, func(p provisional) bool { return p.txn == txn })
}

// PlaceQuestion places the coordinator's own question at q.N, which must be Next(), as a
// provisional entry (QX3): the next ask numbers above it, a duplicate of it is seen, and it
// counts as open, before its echo is folded.
func (x *QuestionIndex) PlaceQuestion(txn, sender string, q Question, body string) error {
	if q.N != x.Next() || x.placed(txn) {
		return fmt.Errorf("%w: a question at Q%d, transaction %q, with Q%d next", ErrNotPlaced, q.N, txn, x.Next())
	}
	headline, section, err := QuestionForm(q.N, body)
	if err != nil {
		return err
	}
	x.prov = append(x.prov, provisional{txn: txn, sender: sender, isQuestion: true, q: IndexedQuestion{
		N: q.N, Headline: headline, Hash: sectionHash(section), AskedBy: x.indexUsers(q.AskedBy),
		Re: slices.Concat([]string{}, q.Re), RemindAfterS: q.RemindAfterS, Unanswered: []string{}, Provisional: true,
	}})
	return nil
}

// PlaceThread places the coordinator's own thread post provisionally (QX3): in a question's
// thread it is applied to that question (a closing post closes it, any post leaves no human post
// unanswered), and a reply to a human post takes that post out of Unattached.
func (x *QuestionIndex) PlaceThread(txn, sender string, t PlacedThread) error {
	if t.N > 0 {
		if q, held := x.Question(t.N); !held || q.Redacted() {
			return fmt.Errorf("%w: a thread post in Q%d, which is not indexed", ErrNotPlaced, t.N)
		}
	}
	if t.N == 0 && t.ReplyTo == "" || x.placed(txn) {
		return fmt.Errorf("%w: a thread post, transaction %q, naming no question and no post", ErrNotPlaced, txn)
	}
	x.prov = append(x.prov, provisional{txn: txn, sender: sender, thread: t})
	return nil
}

// TakeDropped is the transactions of the provisional entries whose echo was folded and not
// counted (an admin's question took the number first, QX3), which the link logs; it empties
// the list.
func (x *QuestionIndex) TakeDropped() []string {
	dropped := x.dropped
	x.dropped = nil
	return dropped
}

// echoed replaces the provisional entry the folded event is the echo of. An event that was not
// counted drops the entry and is reported by TakeDropped; one already folded is left alone.
func (x *QuestionIndex) echoed(v Verdict, r foldResult) {
	if r == repeated {
		return
	}
	i := slices.IndexFunc(x.prov, func(p provisional) bool { return p.sender == v.Event.Sender && p.echoes(v.Content) })
	if i < 0 {
		return
	}
	if r == notCounted {
		x.dropped = append(x.dropped, x.prov[i].txn)
	}
	x.prov = slices.Delete(x.prov, i, i+1)
}

// overlay applies the coordinator's own placed thread posts to a question's view.
func (x *QuestionIndex) overlay(q IndexedQuestion) IndexedQuestion {
	for _, p := range x.prov {
		if p.isQuestion || p.thread.N != q.N {
			continue
		}
		q.Closed = q.Closed || p.thread.Close
		q.Unanswered, q.UnansweredMore = []string{}, 0
	}
	return q
}

func (x *QuestionIndex) repliedProvisionally(post string) bool {
	return slices.ContainsFunc(x.prov, func(p provisional) bool {
		return !p.isQuestion && p.thread.N == 0 && p.thread.ReplyTo == post
	})
}
