package gitx_test

import (
	"context"
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
)

func argvs(f *execx.Fake) [][]string {
	calls := f.Calls()
	got := make([][]string, 0, len(calls))
	for _, c := range calls {
		got = append(got, c.Args)
	}
	return got
}

func TestInitRepoNamesTheBranchAndRunsInTheDirectory(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "init"}, execx.Result{})
	if err := gitx.New(f).InitRepo(context.Background(), dir, "main"); err != nil {
		t.Fatal(err)
	}
	c := f.Calls()[0]
	if !slices.Equal(c.Args, []string{"init", "--quiet", "--initial-branch", "main"}) || c.Dir != dir {
		t.Fatalf("got %v in %q", c.Args, c.Dir)
	}
}

func TestInitRepoReportsGitsWords(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "init"}, gitFail("fatal: cannot mkdir"))
	if err := gitx.New(f).InitRepo(context.Background(), dir, "main"); err == nil {
		t.Fatal("want an error")
	}
}

func TestSetLocalConfigWritesTheRepositorysOwnConfig(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "config"}, execx.Result{})
	if err := gitx.New(f).SetLocalConfig(context.Background(), dir, "user.name", "A B"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"config", "--local", "user.name", "A B"}; !slices.Equal(f.Calls()[0].Args, want) {
		t.Fatalf("got %v", f.Calls()[0].Args)
	}
}

func TestDirty(t *testing.T) {
	for _, c := range []struct {
		out  string
		want bool
	}{{"", false}, {"?? notes.txt\n", true}} {
		f := &execx.Fake{}
		f.On([]string{"git", "status"}, execx.Result{Stdout: []byte(c.out)})
		got, err := gitx.New(f).Dirty(context.Background(), dir)
		if err != nil || got != c.want {
			t.Fatalf("%q: got %v, %v", c.out, got, err)
		}
		if want := []string{"status", "--porcelain", "--untracked-files=all"}; !slices.Equal(f.Calls()[0].Args, want) {
			t.Fatalf("got %v", f.Calls()[0].Args)
		}
	}
}

func TestDirtyReportsGitsWords(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "status"}, gitFail("fatal: not a git repository"))
	if _, err := gitx.New(f).Dirty(context.Background(), dir); err == nil {
		t.Fatal("want an error")
	}
}

func TestCommitAllStagesEverythingThenCommitsUnsigned(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git"}, execx.Result{})
	if err := gitx.New(f).CommitAll(context.Background(), dir, "snapshot"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"add", "--all"},
		{"-c", "commit.gpgsign=false", "commit", "--quiet", "--message", "snapshot"},
	}
	if got := argvs(f); !slices.EqualFunc(got, want, slices.Equal) {
		t.Fatalf("got %v", got)
	}
}

func TestCommitAllStopsWhenTheAddFails(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "add"}, gitFail("fatal: unable to index"))
	if err := gitx.New(f).CommitAll(context.Background(), dir, "snapshot"); err == nil {
		t.Fatal("want an error")
	}
	if len(f.Calls()) != 1 {
		t.Fatalf("ran on after the failed add: %v", argvs(f))
	}
}
