package bashlimits

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeGit answers by the first two arguments of the call; a reply starting "!" is a refusal.
type fakeGit map[string]string

func (f fakeGit) Run(_ context.Context, _ string, args ...string) (string, error) {
	reply, ok := f[strings.Join(args[:min(2, len(args))], " ")]
	switch {
	case !ok:
		return "", errors.New("unexpected call: " + strings.Join(args, " "))
	case strings.HasPrefix(reply, "!"):
		return "", &RefusedError{Args: args, Stderr: reply[1:]}
	}
	return reply, nil
}

func TestListFilesDropsDuplicates(t *testing.T) {
	r := gitRepo{fakeGit{"ls-files -z": "a\x00b\x00a\x00"}, "."}
	got, err := r.listFiles(context.Background())
	if err != nil || len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %v, %v", got, err)
	}
	if _, err := (gitRepo{fakeGit{}, "."}).listFiles(context.Background()); err == nil {
		t.Fatal("a failed ls-files was not an error")
	}
}

func TestTopLevel(t *testing.T) {
	got, err := gitRepo{fakeGit{"rev-parse --show-toplevel": "/srv/x\n"}, "."}.topLevel(context.Background())
	if err != nil || got != "/srv/x" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestFileAtRef(t *testing.T) {
	ctx := context.Background()
	have := fakeGit{"rev-parse --verify": "abc\n", "ls-tree --name-only": "x.txt\n", "show HEAD:x.txt": "body\n"}
	if text, found, err := (gitRepo{have, "."}).fileAtRef(ctx, "HEAD", "x.txt"); err != nil || !found || text != "body\n" {
		t.Fatalf("got %q, %v, %v", text, found, err)
	}
	absent := fakeGit{"rev-parse --verify": "abc\n", "ls-tree --name-only": "\n"}
	if _, found, err := (gitRepo{absent, "."}).fileAtRef(ctx, "HEAD", "x.txt"); err != nil || found {
		t.Fatalf("found=%v, %v", found, err)
	}
	if _, _, err := (gitRepo{fakeGit{"rev-parse --verify": "!"}, "."}).fileAtRef(ctx, "HAED", "x.txt"); err == nil {
		t.Fatal("an unresolvable ref was not an error")
	}
	if _, _, err := (gitRepo{fakeGit{"rev-parse --verify": "abc\n"}, "."}).fileAtRef(ctx, "HEAD", "x.txt"); err == nil {
		t.Fatal("a failed ls-tree was not an error")
	}
	noShow := fakeGit{"rev-parse --verify": "abc\n", "ls-tree --name-only": "x.txt\n"}
	if _, _, err := (gitRepo{noShow, "."}).fileAtRef(ctx, "HEAD", "x.txt"); err == nil {
		t.Fatal("a failed show was not an error")
	}
}

func TestMergeBaseWithDefault(t *testing.T) {
	ctx := context.Background()
	ok := fakeGit{"symbolic-ref --quiet": "refs/remotes/origin/F44\n", "merge-base HEAD": "abc123\n"}
	if got, err := (gitRepo{ok, "."}).mergeBaseWithDefault(ctx); err != nil || got != "abc123" {
		t.Fatalf("got %q, %v", got, err)
	}
	unset := fakeGit{"symbolic-ref --quiet": "!"}
	if _, err := (gitRepo{unset, "."}).mergeBaseWithDefault(ctx); err == nil || !strings.Contains(err.Error(), "default branch") {
		t.Fatalf("an unknown default branch: %v", err)
	}
	if _, err := (gitRepo{fakeGit{}, "."}).mergeBaseWithDefault(ctx); err == nil || strings.Contains(err.Error(), "default branch") {
		t.Fatalf("a git failure that is not a refusal: %v", err)
	}
	noBase := fakeGit{"symbolic-ref --quiet": "refs/remotes/origin/F44\n"}
	if _, err := (gitRepo{noBase, "."}).mergeBaseWithDefault(ctx); err == nil {
		t.Fatal("a failed merge-base was not an error")
	}
}

func TestOSGitSeparatesRefusalFromFailure(t *testing.T) {
	ctx := context.Background()
	_, err := OSGit{}.Run(ctx, t.TempDir(), "rev-parse", "--is-inside-work-tree")
	var refused *RefusedError
	if !errors.As(err, &refused) || !strings.Contains(refused.Error(), "rev-parse") {
		t.Fatalf("outside a repository: %v", err)
	}
	_, err = OSGit{}.Run(ctx, "/no/such/directory", "status")
	if refusal, isRefusal := errors.AsType[*RefusedError](err); err == nil || isRefusal {
		t.Fatalf("a missing directory is not a refusal: %v, %v", err, refusal)
	}
	out, err := OSGit{}.Run(ctx, t.TempDir(), "--version")
	if err != nil || !strings.HasPrefix(out, "git version") {
		t.Fatalf("got %q, %v", out, err)
	}
}
