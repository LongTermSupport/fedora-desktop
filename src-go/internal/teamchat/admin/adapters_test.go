package admin

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/health"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

// stubChild answers every request with a and err, and keeps the requests.
type stubChild struct {
	a    Answer
	err  error
	reqs []Request
}

func (c *stubChild) Ask(_ context.Context, req Request) (Answer, error) {
	c.reqs = append(c.reqs, req)
	return c.a, c.err
}

func failedWith(class ErrorClass) Answer {
	return Answer{Error: &AnswerError{Class: class, Message: "the child's words"}}
}

func testRun(t *testing.T) paths.RunID {
	t.Helper()
	r, err := paths.ParseRunID("0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// arg is the request arg key, which must be there.
func arg(t *testing.T, req Request, key string) proto.Value {
	t.Helper()
	v, given := req.Args.Get(key)
	if !given {
		t.Fatalf("no %s in %s", key, req.Args.Compact())
	}
	return v
}

// TestChildViewerIsTheChildsView is CO4 end to end: the applied team.json root reads, the child's
// view through the real codecs.
func TestChildViewerIsTheChildsView(t *testing.T) {
	w := withMember(t)
	v, err := ChildViewer{Child: inProcessChild{w: w}, Root: w.root}.View(t.Context(), alpha(t))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(v.Members, func(m status.Member) bool { return m.Handle == sampleHandle }) {
		t.Fatalf("view %+v", v)
	}
	stub := &stubChild{a: Answer{OK: true, View: &health.View{}}}
	if _, err := (ChildViewer{Child: stub, Root: w.root}).View(t.Context(), alpha(t)); err != nil {
		t.Fatal(err)
	}
	req := stub.reqs[0]
	if req.Op != OpTeamView || req.Team != alpha(t) || len(req.Args.Members()) != 1 || arg(t, req, keyTeamFile).Kind() != proto.KindObject {
		t.Fatalf("request %+v", req)
	}
}

// TestChildViewerClassesItsFailures is CO4's classes.
func TestChildViewerClassesItsFailures(t *testing.T) {
	w := newWorld(t)
	w.applied(t)
	cases := map[string]struct {
		child *stubChild
		root  string
		want  error
	}{
		"no team.json":   {&stubChild{a: Answer{OK: true, View: &health.View{}}}, t.TempDir(), health.ErrViewFiles},
		"child failed":   {&stubChild{err: ErrChild}, w.root, health.ErrViewChild},
		"refused answer": {&stubChild{a: Answer{OK: true}}, w.root, health.ErrViewChild},
		"config":         {&stubChild{a: failedWith(ClassConfig)}, w.root, health.ErrViewFiles},
		"unreachable":    {&stubChild{a: failedWith(ClassUnreachable)}, w.root, matrix.ErrUnreachable},
		"failed":         {&stubChild{a: failedWith(ClassFailed)}, w.root, health.ErrViewChild},
		"refused":        {&stubChild{a: failedWith(ClassRefused)}, w.root, health.ErrViewChild},
	}
	for name, c := range cases {
		_, err := ChildViewer{Child: c.child, Root: c.root}.View(t.Context(), alpha(t))
		if !errors.Is(err, c.want) {
			t.Errorf("%s: got %v", name, err)
		}
	}
	if len(cases["no team.json"].child.reqs) != 0 {
		t.Fatal("the child was asked without a team.json")
	}
}

// TestChildAnnouncerPostsThroughTheChild is CO4's announcer end to end.
func TestChildAnnouncerPostsThroughTheChild(t *testing.T) {
	w := withMember(t)
	joinedWithCopy(t, w)
	n, err := ChildAnnouncer{Child: inProcessChild{w: w}, Root: w.root}.Disconnected(t.Context(), alpha(t), sampleHandle, testRun(t), t0ms)
	if err != nil || !n.Posted {
		t.Fatalf("notice %+v, %v", n, err)
	}
	stub := &stubChild{a: Answer{OK: true, Notice: &health.Notice{Why: "the subject's token was refused"}}}
	n, err = ChildAnnouncer{Child: stub, Root: w.root}.Disconnected(t.Context(), alpha(t), sampleHandle, testRun(t), t0ms)
	if err != nil || n.Posted || n.Why != "the subject's token was refused" {
		t.Fatalf("notice %+v, %v", n, err)
	}
	req := stub.reqs[0]
	if req.Op != OpMemberDisconnected || arg(t, req, keyHandle).Str() != sampleHandle || arg(t, req, keyRun).Str() != testRun(t).String() || arg(t, req, keySinceMS).Int() != t0ms {
		t.Fatalf("request %+v", req)
	}
}

// TestChildAnnouncerFailuresAreRetried: a child that failed, an answer that failed or does not
// fit, and no team.json are the error the tick retries.
func TestChildAnnouncerFailuresAreRetried(t *testing.T) {
	w := newWorld(t)
	w.applied(t)
	for name, c := range map[string]struct {
		child *stubChild
		root  string
	}{
		"child failed": {&stubChild{err: ErrChild}, w.root},
		"unreachable":  {&stubChild{a: failedWith(ClassUnreachable)}, w.root},
		"config":       {&stubChild{a: failedWith(ClassConfig)}, w.root},
		"no notice":    {&stubChild{a: Answer{OK: true}}, w.root},
		"no team.json": {&stubChild{a: Answer{OK: true, Notice: &health.Notice{Posted: true}}}, t.TempDir()},
	} {
		if _, err := (ChildAnnouncer{Child: c.child, Root: c.root}).Disconnected(t.Context(), alpha(t), sampleHandle, testRun(t), 1); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
