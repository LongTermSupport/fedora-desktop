package gitx_test

import (
	"context"
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
)

func TestChangedPathsReadsEveryRecordAndSkipsARenamesSource(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "status"}, execx.Result{Stdout: []byte(" M run.bash\x00R  new.md\x00old.md\x00?? notes/\x00")})
	got, err := gitx.New(f).ChangedPaths(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"run.bash", "new.md", "notes/"}; !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if want := []string{"status", "--porcelain=v1", "-z", "--untracked-files=normal"}; !slices.Equal(f.Calls()[0].Args, want) {
		t.Fatalf("got %v", f.Calls()[0].Args)
	}
}

func TestChangedPathsCleanIsNone(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "status"}, execx.Result{})
	got, err := gitx.New(f).ChangedPaths(context.Background(), dir)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestChangedPathsRefusesWhatItCannotRead(t *testing.T) {
	for name, out := range map[string]string{
		"a record too short":         "M\x00",
		"a rename with no source":    "R  new.md\x00",
		"a record with no separator": " Mrun.bash\x00",
	} {
		f := &execx.Fake{}
		f.On([]string{"git", "status"}, execx.Result{Stdout: []byte(out)})
		if _, err := gitx.New(f).ChangedPaths(context.Background(), dir); err == nil {
			t.Fatalf("%s: want an error", name)
		}
	}
	f := &execx.Fake{}
	f.On([]string{"git", "status"}, gitFail("fatal: not a git repository"))
	if _, err := gitx.New(f).ChangedPaths(context.Background(), dir); err == nil {
		t.Fatal("git failing: want an error")
	}
}

func TestBranchOnABranchAndDetached(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "symbolic-ref"}, execx.Result{Stdout: []byte("F44\n")})
	branch, on, err := gitx.New(f).Branch(context.Background(), dir)
	if err != nil || !on || branch != "F44" {
		t.Fatalf("got %q %v %v", branch, on, err)
	}
	if want := []string{"symbolic-ref", "--quiet", "--short", "HEAD"}; !slices.Equal(f.Calls()[0].Args, want) {
		t.Fatalf("got %v", f.Calls()[0].Args)
	}
	f = &execx.Fake{}
	f.On([]string{"git", "symbolic-ref"}, execx.Result{ExitCode: 1})
	if branch, on, err := gitx.New(f).Branch(context.Background(), dir); err != nil || on || branch != "" {
		t.Fatalf("detached: got %q %v %v", branch, on, err)
	}
	f = &execx.Fake{}
	f.On([]string{"git", "symbolic-ref"}, gitFail("fatal: not a git repository"))
	if _, _, err := gitx.New(f).Branch(context.Background(), dir); err == nil {
		t.Fatal("git failing: want an error")
	}
}

func TestHasUpstreamReadsTheBranchsMergeKey(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "config"}, execx.Result{Stdout: []byte("refs/heads/F44\n")})
	has, err := gitx.New(f).HasUpstream(context.Background(), dir, "F44")
	if err != nil || !has {
		t.Fatalf("got %v %v", has, err)
	}
	if want := []string{"config", "--get", "branch.F44.merge"}; !slices.Equal(f.Calls()[0].Args, want) {
		t.Fatalf("got %v", f.Calls()[0].Args)
	}
	f = &execx.Fake{}
	f.On([]string{"git", "config"}, execx.Result{ExitCode: 1})
	if has, err := gitx.New(f).HasUpstream(context.Background(), dir, "F44"); err != nil || has {
		t.Fatalf("not set: got %v %v", has, err)
	}
	f = &execx.Fake{}
	f.On([]string{"git", "config"}, execx.Result{ExitCode: 3, Stderr: []byte("error: invalid config file")})
	if _, err := gitx.New(f).HasUpstream(context.Background(), dir, "F44"); err == nil {
		t.Fatal("a broken config: want an error")
	}
}

func TestFetchUpstreamNeverAsksOnTheTerminal(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "fetch"}, execx.Result{})
	if err := gitx.New(f).FetchUpstream(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	c := f.Calls()[0]
	if !slices.Equal(c.Args, []string{"fetch", "--quiet"}) || !slices.Contains(c.Env, "GIT_TERMINAL_PROMPT=0") || c.Dir != dir {
		t.Fatalf("got %v env %v in %q", c.Args, c.Env, c.Dir)
	}
	f = &execx.Fake{}
	f.On([]string{"git", "fetch"}, gitFail("fatal: unable to access: Could not resolve host"))
	if err := gitx.New(f).FetchUpstream(context.Background(), dir); err == nil {
		t.Fatal("a failed fetch: want an error")
	}
}

func TestUpstreamIsItsShortName(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "rev-parse"}, execx.Result{Stdout: []byte("origin/F44\n")})
	got, err := gitx.New(f).Upstream(context.Background(), dir)
	if err != nil || got != "origin/F44" {
		t.Fatalf("got %q %v", got, err)
	}
	if want := []string{"rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"}; !slices.Equal(f.Calls()[0].Args, want) {
		t.Fatalf("got %v", f.Calls()[0].Args)
	}
	f = &execx.Fake{}
	f.On([]string{"git", "rev-parse"}, gitFail("fatal: upstream branch 'refs/heads/x' not stored as a remote-tracking branch"))
	if _, err := gitx.New(f).Upstream(context.Background(), dir); err == nil {
		t.Fatal("an upstream that is gone: want an error")
	}
}

func TestAheadBehindReadsBothCounts(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "rev-list"}, execx.Result{Stdout: []byte("2\t5\n")})
	ahead, behind, err := gitx.New(f).AheadBehind(context.Background(), dir)
	if err != nil || ahead != 2 || behind != 5 {
		t.Fatalf("got %d %d %v", ahead, behind, err)
	}
	if want := []string{"rev-list", "--left-right", "--count", "HEAD...@{upstream}"}; !slices.Equal(f.Calls()[0].Args, want) {
		t.Fatalf("got %v", f.Calls()[0].Args)
	}
	for _, out := range []string{"", "2\n", "two\t5\n", "2\t-1\n", "2\t5\t6\n"} {
		f = &execx.Fake{}
		f.On([]string{"git", "rev-list"}, execx.Result{Stdout: []byte(out)})
		if _, _, err := gitx.New(f).AheadBehind(context.Background(), dir); err == nil {
			t.Fatalf("%q: want an error", out)
		}
	}
	f = &execx.Fake{}
	f.On([]string{"git", "rev-list"}, gitFail("fatal: ambiguous argument"))
	if _, _, err := gitx.New(f).AheadBehind(context.Background(), dir); err == nil {
		t.Fatal("git failing: want an error")
	}
}
