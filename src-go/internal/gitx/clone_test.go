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

func TestRemoteBranchTipReadsOneSha(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "ls-remote", repoURL, "refs/heads/F44"}, execx.Result{Stdout: []byte(sha1 + "\trefs/heads/F44\n")})
	got, err := gitx.New(f).RemoteBranchTip(context.Background(), repoURL, "F44")
	if err != nil || got != sha1 {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestRemoteBranchTipRefusesAnythingButOneSha(t *testing.T) {
	for name, reply := range map[string]execx.Result{
		"no branch":  {},
		"two lines":  {Stdout: []byte(sha1 + "\trefs/heads/F44\n" + sha2 + "\trefs/heads/F44\n")},
		"not a sha":  {Stdout: []byte("main\trefs/heads/F44\n")},
		"a short id": {Stdout: []byte("1111111\trefs/heads/F44\n")},
		"a failure":  gitFail("fatal: could not read from remote repository"),
	} {
		f := &execx.Fake{}
		f.On([]string{"git", "ls-remote"}, reply)
		if got, err := gitx.New(f).RemoteBranchTip(context.Background(), repoURL, "F44"); err == nil {
			t.Fatalf("%s: accepted %q", name, got)
		}
	}
}

func TestCloneIsAFullClone(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "clone"}, execx.Result{})
	if err := gitx.New(f).Clone(context.Background(), repoURL, "/srv/c/o/p"); err != nil {
		t.Fatal(err)
	}
	want := []string{"clone", "--quiet", repoURL, "/srv/c/o/p"}
	if got := f.Calls()[0].Args; !slices.Equal(got, want) {
		t.Fatalf("args %v", got)
	}
	f.On([]string{"git", "clone"}, gitFail("fatal"))
	if err := gitx.New(f).Clone(context.Background(), repoURL, "/x"); err == nil {
		t.Fatal("a failing clone was not an error")
	}
}

func TestFetchAllPrunesAndTakesTags(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "fetch"}, execx.Result{})
	if err := gitx.New(f).FetchAll(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	c := f.Calls()[0]
	if c.Dir != dir || !slices.Equal(c.Args, []string{"fetch", "--quiet", "--tags", "--prune", "origin"}) {
		t.Fatalf("fetch %v in %q", c.Args, c.Dir)
	}
	f.On([]string{"git", "fetch"}, gitFail("fatal: unable to access"))
	if err := gitx.New(f).FetchAll(context.Background(), dir); err == nil {
		t.Fatal("a failing fetch was not an error")
	}
}

func TestFastForwardToOriginHead(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "merge"}, execx.Result{})
	if err := gitx.New(f).FastForwardToOriginHead(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	c := f.Calls()[0]
	if c.Dir != dir || !slices.Equal(c.Args, []string{"merge", "--quiet", "--ff-only", "refs/remotes/origin/HEAD"}) {
		t.Fatalf("merge %v in %q", c.Args, c.Dir)
	}
	f.On([]string{"git", "merge"}, gitFail("fatal: Not possible to fast-forward, aborting."))
	if err := gitx.New(f).FastForwardToOriginHead(context.Background(), dir); err == nil {
		t.Fatal("a refused fast-forward was not an error")
	}
}

func TestIsShallow(t *testing.T) {
	for reply, want := range map[string]bool{"true\n": true, "false\n": false} {
		f := &execx.Fake{}
		f.On([]string{"git", "rev-parse", "--is-shallow-repository"}, execx.Result{Stdout: []byte(reply)})
		got, err := gitx.New(f).IsShallow(context.Background(), dir)
		if err != nil || got != want || f.Calls()[0].Dir != dir {
			t.Fatalf("%q: got %v, %v", reply, got, err)
		}
	}
	for name, reply := range map[string]execx.Result{
		"a failure":   gitFail("fatal: not a git repository"),
		"not a reply": {Stdout: []byte("--is-shallow-repository\n")},
	} {
		f := &execx.Fake{}
		f.On([]string{"git", "rev-parse"}, reply)
		if _, err := gitx.New(f).IsShallow(context.Background(), dir); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
}

func TestUnshallow(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "fetch"}, execx.Result{})
	if err := gitx.New(f).Unshallow(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	c := f.Calls()[0]
	if c.Dir != dir || !slices.Equal(c.Args, []string{"fetch", "--quiet", "--unshallow", "origin"}) {
		t.Fatalf("fetch %v in %q", c.Args, c.Dir)
	}
	f.On([]string{"git", "fetch"}, gitFail("fatal: --unshallow on a complete repository does not make sense"))
	if err := gitx.New(f).Unshallow(context.Background(), dir); err == nil {
		t.Fatal("a failing unshallow was not an error")
	}
}

func TestHeadIsTheFullSha(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "rev-parse", "--verify", "HEAD"}, execx.Result{Stdout: []byte(sha2 + "\n")})
	got, err := gitx.New(f).Head(context.Background(), dir)
	if err != nil || got != sha2 || f.Calls()[0].Dir != dir {
		t.Fatalf("got %q, %v", got, err)
	}
	for name, reply := range map[string]execx.Result{
		"a failure": gitFail("fatal: ambiguous argument 'HEAD'"),
		"not a sha": {Stdout: []byte("HEAD\n")},
	} {
		g := &execx.Fake{}
		g.On([]string{"git", "rev-parse"}, reply)
		if got, err := gitx.New(g).Head(context.Background(), dir); err == nil {
			t.Fatalf("%s: accepted %q", name, got)
		}
	}
}
