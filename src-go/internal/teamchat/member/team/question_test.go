package team

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

const (
	qServer  = "ops.example.org"
	qAskText = "Which licence for the docs?\nContext: the site goes public.\nQuestion: MIT or CC-BY?\nWhy a human: decision on the licence"
)

func qEvent(c byte) string { return "$" + strings.Repeat(string(c), 43) }

// indexWithQ1 is an index holding Q1, asked as event qEvent('a'), and a post attached to none.
func indexWithQ1(t *testing.T) *proto.QuestionIndex {
	t.Helper()
	coord := "@ops.coord.c1:" + qServer
	r := proto.Roster{Members: map[string]proto.RosterMember{coord: {Role: proto.RoleCoordinator}}}
	x := proto.NewQuestionIndex(qServer, 0)
	out, err := proto.Composer{Server: qServer, Self: coord, Roster: r, Questions: x}.Question(proto.QuestionPost{Text: qAskText})
	if err != nil {
		t.Fatal(err)
	}
	dc, reason := proto.DecodeContent(out.Content, proto.QuestionKeys)
	if reason != "" {
		t.Fatal(reason)
	}
	if !x.Feed(proto.Verdict{Outcome: proto.Index, Event: proto.Event{ID: qEvent('a'), Sender: coord, OriginTs: 1}, Content: dc}) {
		t.Fatal("Q1 was not counted")
	}
	x.Attach(proto.Event{ID: qEvent('u'), Sender: "@alice:" + qServer, OriginTs: 2}, proto.Human{Body: "unrelated"})
	return x
}

// R34: InQuestionOf names the question an event is in, in the forms the envelope and the
// records show: Q<n> and its headline, `none` for a post attached to none, nothing for an event
// the index cannot place or with no index.
func TestInQuestionOf(t *testing.T) {
	x := indexWithQ1(t)
	in := InQuestionOf(x, qEvent('h'), proto.Human{Body: "MIT", ThreadRoot: qEvent('a')})
	if e := in.Entry(); e == nil || e.N != 1 || e.Headline != "Which licence for the docs?" {
		t.Errorf("entry %+v, want Q1 and its headline", e)
	}
	if n, h := in.Name(), in.HeadlineName(); n == nil || *n != "Q1" || h == nil || *h != "Which licence for the docs?" {
		t.Errorf("record names %v %v, want Q1 and its headline", n, h)
	}
	none := InQuestionOf(x, qEvent('u'), proto.Human{Body: "unrelated"})
	if n := none.Name(); none.Entry() != nil || n == nil || *n != "none" || none.HeadlineName() != nil {
		t.Errorf("a post attached to none: entry %+v name %v", none.Entry(), n)
	}
	for name, unknown := range map[string]InQuestion{
		"no index":          InQuestionOf(nil, qEvent('h'), proto.Human{Body: "MIT", ThreadRoot: qEvent('a')}),
		"an unplaced event": InQuestionOf(x, qEvent('w'), proto.Human{Body: "hello"}),
	} {
		if unknown.Entry() != nil || unknown.Name() != nil || unknown.HeadlineName() != nil || unknown.Known() {
			t.Errorf("%s: %+v, want nothing named", name, unknown)
		}
	}
	if q := QuestionNamed(4, ""); q.Name() == nil || *q.Name() != "Q4" || q.HeadlineName() != nil {
		t.Errorf("QuestionNamed(4, \"\") = %+v", q)
	}
}
