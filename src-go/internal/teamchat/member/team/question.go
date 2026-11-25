package team

import (
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// InQuestion is which question an event of the questions room is in (Plan 00265 R34), in the one
// form every path that shows it takes: the delivery's entry, the recv, history and verify
// records. The zero value names nothing (main's events, or one no index here can place).
type InQuestion struct {
	n        int64
	headline string
	known    bool
}

// InQuestionOf is the question the index x places event id, of human content h, in
// (proto.QuestionIndex.QuestionIn, the one resolver), with its headline; nothing with no index.
func InQuestionOf(x *proto.QuestionIndex, id string, h proto.Human) InQuestion {
	if x == nil {
		return InQuestion{}
	}
	n, known := x.QuestionIn(id, h)
	in := InQuestion{n: n, known: known}
	if q, ok := x.Question(n); n != 0 && ok && !q.Redacted() {
		in.headline = q.Headline
	}
	return in
}

// QuestionNamed is question n with its headline ("" for none), as a source other than the
// index names it: a relay's account, or verify's read of a question root.
func QuestionNamed(n int64, headline string) InQuestion {
	return InQuestion{n: n, headline: headline, known: true}
}

// Known is whether the question (or none) is named at all.
func (q InQuestion) Known() bool { return q.known }

// Entry is the delivery's `question:` slot: nil for none or unknown (`question: none`).
func (q InQuestion) Entry() *proto.EntryQuestion {
	if q.n == 0 {
		return nil
	}
	return &proto.EntryQuestion{N: int(q.n), Headline: q.headline}
}

// Name is a record's `question`: `Q<n>`, records.QuestionNone, or nil for unknown.
func (q InQuestion) Name() *string {
	switch {
	case !q.known:
		return nil
	case q.n == 0:
		return new(records.QuestionNone)
	}
	return new(proto.QuestionName(int(q.n)))
}

// HeadlineName is a record's `headline`: the question's, nil when no question or no headline.
func (q InQuestion) HeadlineName() *string {
	if q.n == 0 || q.headline == "" {
		return nil
	}
	return new(q.headline)
}
