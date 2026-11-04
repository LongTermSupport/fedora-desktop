package release_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/release"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/releasever"
)

const (
	sha1 = "1111111111111111111111111111111111111111"
	sha2 = "2222222222222222222222222222222222222222"
	sha3 = "3333333333333333333333333333333333333333"
)

// fakeGit answers the four questions Decide asks, and records the range it was asked to log.
type fakeGit struct {
	tags       []string
	tagsErr    error
	log        []gitx.LogEntry
	logErr     error
	logRev     string
	subjects   []gitx.Subject
	subjectErr error
	branches   map[string]bool
	branchErr  error
	asked      []string
}

func (f *fakeGit) Tags(_ context.Context, _, pattern string) ([]string, error) {
	f.asked = append(f.asked, "tags "+pattern)
	return f.tags, f.tagsErr
}

func (f *fakeGit) FirstParentLog(_ context.Context, _, rev string) ([]gitx.LogEntry, error) {
	f.asked = append(f.asked, "log "+rev)
	f.logRev = rev
	return f.log, f.logErr
}

func (f *fakeGit) SubjectsMatching(_ context.Context, _, rev, _ string) ([]gitx.Subject, error) {
	f.asked = append(f.asked, "subjects "+rev)
	return f.subjects, f.subjectErr
}

func (f *fakeGit) RemoteBranchExists(_ context.Context, _, _, branch string) (bool, error) {
	f.asked = append(f.asked, "branch "+branch)
	return f.branches[branch], f.branchErr
}

// fakePulls answers with the pull request of a merge commit by sha.
type fakePulls struct {
	messages map[string]string
	err      error
	asked    []string
}

func (f *fakePulls) Message(_ context.Context, sha string) (string, bool, error) {
	f.asked = append(f.asked, sha)
	if f.err != nil {
		return "", false, f.err
	}
	m, ok := f.messages[sha]
	return m, ok, nil
}

func decide(t *testing.T, branch string, g *fakeGit, p *fakePulls) (release.Plan, error) {
	t.Helper()
	d := release.Decider{Git: g, Pulls: p, Dir: "/repo", Branch: branch, Remote: "origin"}
	return d.Decide(context.Background())
}

func mustDecide(t *testing.T, g *fakeGit, p *fakePulls) release.Plan {
	t.Helper()
	plan, err := decide(t, "F44", g, p)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	return plan
}

// R1: only a branch named F<major> releases.
func TestABranchThatIsNotAReleaseBranchIsRefused(t *testing.T) {
	for _, branch := range []string{"main", "release-44.1.0", "F", "F4x", ""} {
		if _, err := decide(t, branch, &fakeGit{}, &fakePulls{}); !errors.Is(err, releasever.ErrBranch) {
			t.Fatalf("%q: got %v, want ErrBranch", branch, err)
		}
	}
}

// R2 and R3: the first release of a major is made by hand, so a major with no release tag is
// left alone, and nothing else is asked of git or GitHub.
func TestNoReleaseTagMeansNothingToDo(t *testing.T) {
	g := &fakeGit{tags: []string{"44.rc1", "43.9.0", "44.01.0", "v44.1.0", "44.1"}}
	plan := mustDecide(t, g, &fakePulls{})
	if plan.Action != release.ActionNone || !strings.Contains(plan.Reason, "release.bash prepare first") {
		t.Fatalf("got %+v, want none naming how the first release is made", plan)
	}
	if len(g.asked) != 1 || g.asked[0] != "tags 44.*" {
		t.Fatalf("asked %q, want only the tag listing", g.asked)
	}
}

// R2: the newest release is the highest by number, not by text (44.10.0 is after 44.9.0).
func TestTheNewestTagIsByNumber(t *testing.T) {
	g := &fakeGit{
		tags: []string{"44.9.0", "44.10.0", "44.2.5", "44.rc1"},
		log:  []gitx.LogEntry{{SHA: sha1, Parents: 1, Message: "fix: a bug"}},
	}
	plan := mustDecide(t, g, &fakePulls{})
	if plan.Action != release.ActionPrepare || plan.Version != "44.10.1" {
		t.Fatalf("got %+v, want prepare 44.10.1", plan)
	}
	if g.logRev != "44.10.0..HEAD" {
		t.Fatalf("logged %q, want the range since the newest release", g.logRev)
	}
}

// R4: a signed release commit merged without its tag is what a tagging run is for.
func TestAMergedReleaseCommitWithoutItsTagIsTagged(t *testing.T) {
	g := &fakeGit{
		tags:     []string{"44.0.0", "44.1.0"},
		subjects: []gitx.Subject{{SHA: sha2, Subject: "Release 44.2.0"}},
	}
	plan := mustDecide(t, g, &fakePulls{})
	want := release.Plan{Action: release.ActionTag, Version: "44.2.0", Commit: sha2}
	if plan.Action != want.Action || plan.Version != want.Version || plan.Commit != want.Commit {
		t.Fatalf("got %+v, want %+v", plan, want)
	}
}

func TestOnlyAnExactReleaseSubjectAfterTheLastReleaseCounts(t *testing.T) {
	cases := map[string]gitx.Subject{
		"its tag exists":        {SHA: sha2, Subject: "Release 44.1.0"},
		"an older release":      {SHA: sha2, Subject: "Release 44.0.0"},
		"another major":         {SHA: sha2, Subject: "Release 45.2.0"},
		"words after":           {SHA: sha2, Subject: "Release 44.2.0 again"},
		"not a version":         {SHA: sha2, Subject: "Release 44.two.0"},
		"a leading zero":        {SHA: sha2, Subject: "Release 44.02.0"},
		"a different word":      {SHA: sha2, Subject: "release 44.2.0"},
		"a merge of the commit": {SHA: sha2, Subject: "Merge pull request 9 from x/release-44.2.0"},
	}
	for name, subject := range cases {
		g := &fakeGit{
			tags:     []string{"44.0.0", "44.1.0"},
			subjects: []gitx.Subject{subject},
			log:      []gitx.LogEntry{{SHA: sha1, Parents: 1, Message: "chore: nothing"}},
		}
		plan := mustDecide(t, g, &fakePulls{})
		if plan.Action == release.ActionTag {
			t.Fatalf("%s: tagged %+v, want it ignored", name, plan)
		}
	}
}

func TestTheHighestPendingReleaseCommitIsTheOneTagged(t *testing.T) {
	g := &fakeGit{
		tags: []string{"44.0.0"},
		subjects: []gitx.Subject{
			{SHA: sha1, Subject: "Release 44.1.0"},
			{SHA: sha3, Subject: "Release 44.3.0"},
			{SHA: sha2, Subject: "Release 44.2.0"},
		},
	}
	plan := mustDecide(t, g, &fakePulls{})
	if plan.Action != release.ActionTag || plan.Version != "44.3.0" || plan.Commit != sha3 {
		t.Fatalf("got %+v, want tag 44.3.0 on %s", plan, sha3)
	}
}

// R5: a merge commit stands for its pull request's title and body; a commit that is not a merge
// stands for itself.
func TestAMergeCommitIsReadAsItsPullRequestAndADirectCommitAsItself(t *testing.T) {
	g := &fakeGit{
		tags: []string{"44.0.0"},
		log: []gitx.LogEntry{
			{SHA: sha1, Parents: 2, Message: "Merge PR 7 (feat/x): a long custom merge subject"},
			{SHA: sha2, Parents: 1, Message: "fix: a direct commit"},
		},
	}
	p := &fakePulls{messages: map[string]string{sha1: "feat: a new thing\n\nbody"}}
	plan := mustDecide(t, g, p)
	if plan.Version != "44.1.0" {
		t.Fatalf("got %+v, want the minor of the pull request title", plan)
	}
	want := []string{"feat: a new thing", "fix: a direct commit"}
	if strings.Join(plan.Releasable, "|") != strings.Join(want, "|") {
		t.Fatalf("releasable %q, want %q", plan.Releasable, want)
	}
	if len(p.asked) != 1 || p.asked[0] != sha1 {
		t.Fatalf("GitHub asked about %q, want only the merge commit", p.asked)
	}
}

// A merge with no pull request behind it (a branch merged by hand) is read as its own message.
func TestAMergeWithNoPullRequestIsReadAsItself(t *testing.T) {
	g := &fakeGit{
		tags: []string{"44.0.0"},
		log:  []gitx.LogEntry{{SHA: sha1, Parents: 2, Message: "Merge branch 'feat/x'"}},
	}
	plan := mustDecide(t, g, &fakePulls{})
	if plan.Action != release.ActionNone || !strings.Contains(plan.Reason, "no releasable") {
		t.Fatalf("got %+v, want none: the merge message is no conventional header", plan)
	}
}

// A body's breaking footer reaches the version rule, which refuses it on a released major.
func TestABreakingChangeInAPullRequestIsRefused(t *testing.T) {
	g := &fakeGit{
		tags: []string{"44.0.0"},
		log:  []gitx.LogEntry{{SHA: sha1, Parents: 2, Message: "Merge PR 7"}},
	}
	p := &fakePulls{messages: map[string]string{sha1: "feat: new\n\nBREAKING CHANGE: removes a thing"}}
	if _, err := decide(t, "F44", g, p); !errors.Is(err, releasever.ErrBreaking) {
		t.Fatalf("got %v, want ErrBreaking", err)
	}
}

func TestNothingReleasableMeansNoRelease(t *testing.T) {
	g := &fakeGit{
		tags: []string{"44.0.0"},
		log:  []gitx.LogEntry{{SHA: sha1, Parents: 1, Message: "docs: words"}, {SHA: sha2, Parents: 1, Message: "Plan 00246: a task"}},
	}
	plan := mustDecide(t, g, &fakePulls{})
	if plan.Action != release.ActionNone || !strings.Contains(plan.Reason, "no releasable") {
		t.Fatalf("got %+v", plan)
	}
}

// R7: the pull request of a release already open is not opened twice.
func TestAnOpenReleaseBranchIsNotOpenedTwice(t *testing.T) {
	g := &fakeGit{
		tags:     []string{"44.0.0"},
		log:      []gitx.LogEntry{{SHA: sha1, Parents: 1, Message: "feat: a thing"}},
		branches: map[string]bool{"release-44.1.0": true},
	}
	plan := mustDecide(t, g, &fakePulls{})
	if plan.Action != release.ActionNone || !strings.Contains(plan.Reason, "release-44.1.0") {
		t.Fatalf("got %+v, want none naming the branch", plan)
	}
}

func TestPrepareNamesTheVersionAndWhatDecidedIt(t *testing.T) {
	g := &fakeGit{
		tags: []string{"44.0.0"},
		log:  []gitx.LogEntry{{SHA: sha1, Parents: 1, Message: "fix: one"}, {SHA: sha2, Parents: 1, Message: "feat: two"}},
	}
	plan := mustDecide(t, g, &fakePulls{})
	if plan.Action != release.ActionPrepare || plan.Version != "44.1.0" {
		t.Fatalf("got %+v", plan)
	}
	if strings.Join(plan.Releasable, "|") != "feat: two|fix: one" {
		t.Fatalf("releasable %q, want the feat first", plan.Releasable)
	}
}

// Nothing is decided on an answer that failed: each failing question is an error.
func TestAFailedQuestionIsAnErrorNotAnAnswer(t *testing.T) {
	boom := errors.New("boom")
	base := func() *fakeGit {
		return &fakeGit{tags: []string{"44.0.0"}, log: []gitx.LogEntry{{SHA: sha1, Parents: 2, Message: "m"}}}
	}
	cases := map[string]func(*fakeGit, *fakePulls){
		"tags":     func(g *fakeGit, _ *fakePulls) { g.tagsErr = boom },
		"subjects": func(g *fakeGit, _ *fakePulls) { g.subjectErr = boom },
		"log":      func(g *fakeGit, _ *fakePulls) { g.logErr = boom },
		"pulls":    func(_ *fakeGit, p *fakePulls) { p.err = boom },
		"branch": func(g *fakeGit, _ *fakePulls) {
			g.log = []gitx.LogEntry{{SHA: sha1, Parents: 1, Message: "feat: a"}}
			g.branchErr = boom
		},
	}
	for name, mutate := range cases {
		g, p := base(), &fakePulls{}
		mutate(g, p)
		if _, err := decide(t, "F44", g, p); !errors.Is(err, boom) {
			t.Fatalf("%s: got %v, want the failure carried", name, err)
		}
	}
}
