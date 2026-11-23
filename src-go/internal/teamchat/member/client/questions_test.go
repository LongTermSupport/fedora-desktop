package client

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// DESIGN-questions-v2 2.1-2.7: each questions verb's op, args and stdin, on the send deadline
// (questions on its own 60 s).
func TestQuestionsVerbsRequestBytes(t *testing.T) {
	for _, tc := range []struct {
		args     []string
		in       string
		stdin    *string
		op       string
		want     string
		deadline time.Duration
	}{
		{
			[]string{"ask-human", "--for", "repo.2,repo.3", "--re", eventID, "--remind-after", "600", "--check"},
			"the question\n", new("the question"),
			wire.OpAskHuman, `{"check":true,"for":["repo.2","repo.3"],"re":["` + eventID + `"],"remind_after_s":600,"team":null}`, 30 * time.Second,
		},
		{
			[]string{"ask-human", "--team", "alpha"},
			"q", new("q"),
			wire.OpAskHuman, `{"check":false,"for":[],"re":[],"remind_after_s":null,"team":"alpha"}`, 30 * time.Second,
		},
		{[]string{"thread", "Q4", "--note"}, "more\n", new("more"), wire.OpThread, `{"note":true,"q":"Q4","team":null}`, 30 * time.Second},
		{[]string{"close", "Q4", "Q5"}, "", nil, wire.OpClose, `{"qs":["Q4","Q5"],"team":null}`, 30 * time.Second},
		{[]string{"close", "Q4"}, "settled\n", new("settled"), wire.OpClose, `{"qs":["Q4"],"team":null}`, 30 * time.Second},
		{[]string{"retitle", "Q4"}, "New headline\n", new("New headline"), wire.OpRetitle, `{"q":"Q4","team":null}`, 30 * time.Second},
		{[]string{"bump", "--waiting"}, "", nil, wire.OpBump, `{"min_age_s":3600,"note":null,"qs":[],"team":null,"waiting":true}`, 30 * time.Second},
		{[]string{"bump", "Q2", "Q3", "--note", "still?", "--min-age", "0"}, "", nil, wire.OpBump, `{"min_age_s":0,"note":"still?","qs":["Q2","Q3"],"team":null,"waiting":false}`, 30 * time.Second},
		{[]string{"questions"}, "", nil, wire.OpQuestions, `{"all":false,"digest":false,"for":null,"team":null,"waiting":false}`, time.Minute},
		{[]string{"questions", "--waiting", "--for", "repo.2", "--digest", "--all"}, "", nil, wire.OpQuestions, `{"all":true,"digest":true,"for":"repo.2","team":null,"waiting":true}`, time.Minute},
	} {
		w := newWorld(t)
		w.stdin = strings.NewReader(tc.in)
		l := w.listen(doneOK)
		if code := w.do(tc.args...); code != term.ExitOK {
			t.Fatalf("%v: exit %d %q", tc.args, code, w.stderr.String())
		}
		req := l.request(t)
		got, err := json.Marshal(args(t, req))
		if err != nil {
			t.Fatal(err)
		}
		if req.Op != tc.op || string(got) != tc.want {
			t.Fatalf("%v: %s %s", tc.args, req.Op, got)
		}
		if req.DeadlineMS != (boot0 + tc.deadline).Milliseconds() {
			t.Fatalf("%v: deadline %d", tc.args, req.DeadlineMS)
		}
		if (req.Stdin == nil) != (tc.stdin == nil) || req.Stdin != nil && *req.Stdin != *tc.stdin {
			t.Fatalf("%v: stdin %v", tc.args, req.Stdin)
		}
	}
}

// bump names its questions or says --waiting, exactly one; --min-age is a whole number. Each
// refusal is 64 before anything is sent.
func TestBumpTakesNamesOrWaiting(t *testing.T) {
	for _, args := range [][]string{{"bump"}, {"bump", "Q4", "--waiting"}} {
		w := newWorld(t)
		if code := w.do(args...); code != exits.ExitUsage {
			t.Fatalf("%v: exit %d", args, code)
		}
		if e := w.errorObject(); e.Code != exits.CodeUsage {
			t.Fatalf("%v: %+v", args, e)
		}
	}
}

// --skeleton prints the question's form, client-side: no session is needed, nothing is sent,
// and the form is one QuestionForm accepts.
func TestAskHumanSkeletonPrintsTheForm(t *testing.T) {
	w := newWorld(t)
	w.env = map[string]string{}
	if code := w.do("ask-human", "--skeleton"); code != term.ExitOK {
		t.Fatalf("exit %d %q", code, w.stderr.String())
	}
	got := w.stdout.String()
	for _, section := range []string{"\nContext: ", "\nQuestion: ", "\nWhy a human: ", "\nOptions:\n"} {
		if !strings.Contains(got, section) {
			t.Errorf("the skeleton has no %q: %q", section, got)
		}
	}
	if _, _, err := proto.QuestionForm(1, "Q1 "+strings.TrimSuffix(got, "\n")); err != nil {
		t.Errorf("the skeleton is not A 10.2's form: %v", err)
	}
}

// A close whose question another seat asked is not retitled: the record says so, and so does
// one stderr line, with exit 0.
func TestACloseNotRetitledSaysWhyOnStderr(t *testing.T) {
	alpha, err := paths.ParseTeamID("alpha")
	if err != nil {
		t.Fatal(err)
	}
	q := recordLine(t, records.Question{Team: alpha, N: new(int64(4)), State: "closed", ID: new(eventID), Headline: "h", Retitled: new(false)})
	w := newWorld(t)
	w.stdin = strings.NewReader("")
	w.listen(recordLine(t, records.Sent{Team: alpha, ID: new(eventID), Room: "questions"}), q, doneOK)
	if code := w.do("close", "Q4"); code != term.ExitOK {
		t.Fatalf("exit %d %q", code, w.stderr.String())
	}
	if !strings.Contains(w.stdout.String(), q) || !strings.Contains(w.stderr.String(), "Q4 was not retitled") {
		t.Fatalf("stdout %q stderr %q", w.stdout.String(), w.stderr.String())
	}
}
