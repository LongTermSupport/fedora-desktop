package gitx_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
)

func TestTagsAreTheNamesGitListsForThePattern(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "tag", "--list", "44.*"}, execx.Result{Stdout: []byte("44.0.0\n44.1.0\n\n44.rc1\n")})
	got, err := gitx.New(f).Tags(context.Background(), dir, "44.*")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "44.0.0,44.1.0,44.rc1" {
		t.Fatalf("got %q", got)
	}
	if f.Calls()[0].Dir != dir {
		t.Fatalf("ran in %q, want %q", f.Calls()[0].Dir, dir)
	}
}

func TestTagsNoneIsEmptyNotAnError(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "tag", "--list"}, execx.Result{})
	got, err := gitx.New(f).Tags(context.Background(), dir, "44.*")
	if err != nil || len(got) != 0 {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestTagsGitFailureIsAnError(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "tag"}, gitFail("fatal: not a git repository"))
	if _, err := gitx.New(f).Tags(context.Background(), dir, "44.*"); err == nil {
		t.Fatal("want an error: a failed listing must not read as no release")
	}
}

// A record is the sha, the parents, and the whole message, separated by US (0x1f); records are
// NUL-ended (git log -z). The message keeps its body, which can hold newlines.
func logRecord(sha, parents, message string) string {
	return sha + "\x1f" + parents + "\x1f" + message + "\x00"
}

func TestFirstParentLogReadsParentsAndWholeMessages(t *testing.T) {
	f := &execx.Fake{}
	out := logRecord("aaa", "p1 p2", "Merge PR 7\n\nfeat: a thing\n") +
		logRecord("bbb", "p3", "fix: direct commit\n") +
		logRecord("ccc", "", "root\n")
	f.On([]string{"git", "log", "--first-parent", "-z", "--format=%H%x1f%P%x1f%B", "44.0.0..HEAD"}, execx.Result{Stdout: []byte(out)})
	got, err := gitx.New(f).FirstParentLog(context.Background(), dir, "44.0.0..HEAD")
	if err != nil {
		t.Fatal(err)
	}
	want := []gitx.LogEntry{
		{SHA: "aaa", Parents: 2, Message: "Merge PR 7\n\nfeat: a thing"},
		{SHA: "bbb", Parents: 1, Message: "fix: direct commit"},
		{SHA: "ccc", Parents: 0, Message: "root"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entry %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestFirstParentLogMalformedRecordIsAnError(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "log"}, execx.Result{Stdout: []byte("just-a-sha\x00")})
	if _, err := gitx.New(f).FirstParentLog(context.Background(), dir, "HEAD"); err == nil {
		t.Fatal("want an error for a record that is not sha, parents and message")
	}
}

func TestFirstParentLogGitFailureIsAnError(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "log"}, gitFail("fatal: bad revision"))
	if _, err := gitx.New(f).FirstParentLog(context.Background(), dir, "nope..HEAD"); err == nil {
		t.Fatal("want an error: an unreadable range must not read as no change")
	}
}

func TestSubjectsMatchingReadsShaAndSubject(t *testing.T) {
	f := &execx.Fake{}
	out := "aaa\x1fRelease 44.1.0\x00bbb\x1fRelease 44.0.0\x00"
	f.On([]string{"git", "log", "--extended-regexp", "--grep=^Release [0-9]+[.][0-9]+[.][0-9]+$", "-z", "--format=%H%x1f%s", "HEAD"}, execx.Result{Stdout: []byte(out)})
	got, err := gitx.New(f).SubjectsMatching(context.Background(), dir, "HEAD", "^Release [0-9]+[.][0-9]+[.][0-9]+$")
	if err != nil {
		t.Fatal(err)
	}
	want := []gitx.Subject{{SHA: "aaa", Subject: "Release 44.1.0"}, {SHA: "bbb", Subject: "Release 44.0.0"}}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %+v", got)
	}
}

func TestSubjectsMatchingMalformedRecordIsAnError(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "log"}, execx.Result{Stdout: []byte("no-separator\x00")})
	if _, err := gitx.New(f).SubjectsMatching(context.Background(), dir, "HEAD", "x"); err == nil {
		t.Fatal("want an error")
	}
}

func TestRemoteBranchExists(t *testing.T) {
	sha := strings.Repeat("a", 40)
	cases := []struct {
		name string
		out  string
		want bool
	}{
		{"present", sha + "\trefs/heads/release-44.1.0\n", true},
		{"absent", "", false},
	}
	for _, c := range cases {
		f := &execx.Fake{}
		f.On([]string{"git", "ls-remote", "--heads", "origin", "release-44.1.0"}, execx.Result{Stdout: []byte(c.out)})
		got, err := gitx.New(f).RemoteBranchExists(context.Background(), dir, "origin", "release-44.1.0")
		if err != nil || got != c.want {
			t.Fatalf("%s: got %v, %v", c.name, got, err)
		}
	}
}

func TestRemoteBranchExistsGitFailureIsAnError(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "ls-remote"}, gitFail("fatal: could not read from remote"))
	got, err := gitx.New(f).RemoteBranchExists(context.Background(), dir, "origin", "release-44.1.0")
	if err == nil || got {
		t.Fatalf("got %v, %v: an unreachable remote must not read as no branch", got, err)
	}
	if exitErr, ok := errors.AsType[*execx.ExitError](err); !ok || exitErr.Code != 128 {
		t.Fatalf("error %v does not carry git's exit status 128", err)
	}
}
