package gitx_test

import (
	"context"
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
)

const (
	repoURL = "https://github.com/example-org/proj"
	sha1    = "1111111111111111111111111111111111111111"
	sha2    = "2222222222222222222222222222222222222222"
)

func TestDefaultBranchReadsTheSymref(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "ls-remote", "--symref", repoURL, "HEAD"},
		execx.Result{Stdout: []byte("ref: refs/heads/trunk\tHEAD\n" + sha1 + "\tHEAD\n")})
	got, err := gitx.New(f).DefaultBranch(context.Background(), repoURL)
	if err != nil || got != "trunk" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestDefaultBranchWithoutASymrefIsAnError(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "ls-remote"}, execx.Result{Stdout: []byte(sha1 + "\tHEAD\n")})
	if _, err := gitx.New(f).DefaultBranch(context.Background(), repoURL); err == nil {
		t.Fatal("a reply with no default branch was accepted")
	}
}

func TestDefaultBranchFailureIsAnError(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "ls-remote"}, gitFail("fatal: repository not found"))
	if _, err := gitx.New(f).DefaultBranch(context.Background(), repoURL); err == nil {
		t.Fatal("a failing ls-remote was not an error")
	}
}

func TestCloneBranch(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "clone"}, execx.Result{})
	if err := gitx.New(f).CloneBranch(context.Background(), repoURL, "main", "/srv/c/o__p"); err != nil {
		t.Fatal(err)
	}
	want := []string{"clone", "--branch", "main", "--single-branch", repoURL, "/srv/c/o__p"}
	if got := f.Calls()[0].Args; !slices.Equal(got, want) {
		t.Fatalf("args %v", got)
	}
}

func TestCloneBranchFailure(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "clone"}, gitFail("fatal"))
	if err := gitx.New(f).CloneBranch(context.Background(), repoURL, "main", "/x"); err == nil {
		t.Fatal("a failing clone was not an error")
	}
}

func TestFetchBranchAndRemoteHead(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "fetch", "origin", "+refs/heads/main:refs/remotes/origin/main"}, execx.Result{})
	f.On([]string{"git", "rev-parse", "--verify", "refs/remotes/origin/main"}, execx.Result{Stdout: []byte(sha2 + "\n")})
	g := gitx.New(f)
	if err := g.FetchBranch(context.Background(), dir, "main"); err != nil {
		t.Fatal(err)
	}
	head, err := g.RemoteHead(context.Background(), dir, "main")
	if err != nil || head != sha2 {
		t.Fatalf("got %q, %v", head, err)
	}
	if f.Calls()[0].Dir != dir || f.Calls()[1].Dir != dir {
		t.Fatalf("ran in %q and %q", f.Calls()[0].Dir, f.Calls()[1].Dir)
	}
}

func TestFetchBranchFailure(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "fetch"}, gitFail("fatal: unable to access"))
	if err := gitx.New(f).FetchBranch(context.Background(), dir, "main"); err == nil {
		t.Fatal("a failing fetch was not an error")
	}
}

func TestHasCommit(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "cat-file", "-e", sha1 + "^{commit}"}, execx.Result{})
	f.On([]string{"git", "cat-file", "-e", sha2 + "^{commit}"}, gitFail("fatal: Not a valid object name"))
	g := gitx.New(f)
	if ok, err := g.HasCommit(context.Background(), dir, sha1); err != nil || !ok {
		t.Fatalf("present: %v, %v", ok, err)
	}
	if ok, err := g.HasCommit(context.Background(), dir, sha2); err != nil || ok {
		t.Fatalf("absent: %v, %v", ok, err)
	}
	if _, err := gitx.New(errRunner{}).HasCommit(context.Background(), dir, sha1); err == nil {
		t.Fatal("git being unrunnable was read as an absent commit")
	}
}

func TestCommitsBetweenAndChangedPaths(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "log", "--format=%h %s", sha1 + ".." + sha2},
		execx.Result{Stdout: []byte("bbb2222 second\naaa1111 first\n")})
	f.On([]string{"git", "diff", "--name-only", sha1, sha2}, execx.Result{Stdout: []byte("a/b.go\nREADME.md\n")})
	g := gitx.New(f)
	commits, err := g.CommitsBetween(context.Background(), dir, sha1, sha2)
	if err != nil || !slices.Equal(commits, []string{"bbb2222 second", "aaa1111 first"}) {
		t.Fatalf("got %v, %v", commits, err)
	}
	paths, err := g.ChangedPaths(context.Background(), dir, sha1, sha2)
	if err != nil || !slices.Equal(paths, []string{"a/b.go", "README.md"}) {
		t.Fatalf("got %v, %v", paths, err)
	}
}

func TestCommitsBetweenFailure(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "log"}, gitFail("fatal: bad revision"))
	if _, err := gitx.New(f).CommitsBetween(context.Background(), dir, sha1, sha2); err == nil {
		t.Fatal("a failing log was not an error")
	}
	f.On([]string{"git", "diff"}, gitFail("fatal"))
	if _, err := gitx.New(f).ChangedPaths(context.Background(), dir, sha1, sha2); err == nil {
		t.Fatal("a failing diff was not an error")
	}
}
