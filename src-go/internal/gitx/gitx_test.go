package gitx_test

import (
	"context"
	"errors"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
)

const dir = "/srv/proj"

func gitFail(msg string) execx.Result {
	return execx.Result{Stderr: []byte(msg), ExitCode: 128}
}

func TestProjectHere(t *testing.T) {
	cases := []struct {
		name string
		res  execx.Result
		want bool
	}{
		{"inside", execx.Result{Stdout: []byte("true\n")}, true},
		{"bare repo", execx.Result{Stdout: []byte("false\n")}, false},
		{"not a repo", gitFail("fatal: not a git repository"), false},
	}
	for _, c := range cases {
		f := &execx.Fake{}
		f.On([]string{"git", "rev-parse", "--is-inside-work-tree"}, c.res)
		got, err := gitx.New(f).ProjectHere(context.Background(), dir)
		if err != nil || got != c.want {
			t.Fatalf("%s: got %v, %v", c.name, got, err)
		}
		if f.Calls()[0].Dir != dir {
			t.Fatalf("%s: dir %q", c.name, f.Calls()[0].Dir)
		}
	}
}

func TestProjectHereGitMissing(t *testing.T) {
	if _, err := gitx.New(errRunner{}).ProjectHere(context.Background(), dir); err == nil {
		t.Fatal("want error")
	}
}

func TestTopLevel(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "rev-parse", "--show-toplevel"}, execx.Result{Stdout: []byte("/srv/proj\n")})
	got, err := gitx.New(f).TopLevel(context.Background(), dir)
	if err != nil || got != "/srv/proj" {
		t.Fatalf("got %q, %v", got, err)
	}
	if f.Calls()[0].Dir != dir {
		t.Fatalf("dir %q", f.Calls()[0].Dir)
	}
}

func TestTopLevelFails(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "rev-parse", "--show-toplevel"}, gitFail("fatal: not a git repository"))
	if _, err := gitx.New(f).TopLevel(context.Background(), dir); err == nil {
		t.Fatal("want error")
	}
}

func TestRemoteURL(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "remote", "get-url", "origin"},
		execx.Result{Stdout: []byte("git@github.com:example-org/proj.git\n")})
	got, err := gitx.New(f).RemoteURL(context.Background(), dir)
	if err != nil || got != "git@github.com:example-org/proj.git" {
		t.Fatalf("got %q, %v", got, err)
	}
	if f.Calls()[0].Dir != dir {
		t.Fatalf("dir %q", f.Calls()[0].Dir)
	}
}

func TestRemoteURLNoOriginIsEmpty(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"git", "remote", "get-url", "origin"}, gitFail("error: No such remote 'origin'"))
	got, err := gitx.New(f).RemoteURL(context.Background(), dir)
	if err != nil || got != "" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestRemoteURLGitMissing(t *testing.T) {
	if _, err := gitx.New(errRunner{}).RemoteURL(context.Background(), dir); err == nil {
		t.Fatal("want error")
	}
}

type errRunner struct{}

func (errRunner) Run(context.Context, execx.Cmd) (execx.Result, error) {
	return execx.Result{}, errors.New("git: not installed")
}
