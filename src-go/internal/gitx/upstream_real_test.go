package gitx

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// upstreamPair is a real checkout cloned from a real bare repository, with one commit each
// side knows, so the upstream questions are answered by git itself.
type upstreamPair struct {
	g        *Git
	bare     string
	checkout string
}

func newUpstreamPair(t *testing.T) upstreamPair {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	root := t.TempDir()
	p := upstreamPair{g: New(execx.OS{}), bare: filepath.Join(root, "upstream.git"), checkout: filepath.Join(root, "checkout")}
	p.git(t, root, "init", "--quiet", "--bare", "--initial-branch", "main", p.bare)
	p.git(t, root, "clone", "--quiet", p.bare, p.checkout)
	p.commit(t, p.checkout, "first")
	p.git(t, p.checkout, "push", "--quiet", "-u", "origin", "main")
	return p
}

func (p upstreamPair) git(t *testing.T, dir string, args ...string) {
	t.Helper()
	if _, err := p.g.run(context.Background(), dir, args...); err != nil {
		t.Fatal(err)
	}
}

func (p upstreamPair) commit(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p.git(t, dir, "add", name)
	p.git(t, dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false",
		"commit", "--quiet", "--message", name)
}

// pushFromElsewhere puts a commit on the upstream that the checkout has not fetched.
func (p upstreamPair) pushFromElsewhere(t *testing.T) {
	t.Helper()
	other := filepath.Join(filepath.Dir(p.bare), "other")
	p.git(t, filepath.Dir(p.bare), "clone", "--quiet", p.bare, other)
	p.commit(t, other, "theirs")
	p.git(t, other, "push", "--quiet", "origin", "main")
}

func (p upstreamPair) counts(t *testing.T) (int, int) {
	t.Helper()
	ctx := context.Background()
	if err := p.g.FetchUpstream(ctx, p.checkout); err != nil {
		t.Fatal(err)
	}
	ahead, behind, err := p.g.AheadBehind(ctx, p.checkout)
	if err != nil {
		t.Fatal(err)
	}
	return ahead, behind
}

func TestRealCheckoutLevelWithItsUpstream(t *testing.T) {
	p := newUpstreamPair(t)
	ctx := context.Background()
	branch, on, err := p.g.Branch(ctx, p.checkout)
	if err != nil || !on || branch != "main" {
		t.Fatalf("branch %q %v %v", branch, on, err)
	}
	if has, err := p.g.HasUpstream(ctx, p.checkout, branch); err != nil || !has {
		t.Fatalf("has upstream %v %v", has, err)
	}
	if up, err := p.g.Upstream(ctx, p.checkout); err != nil || up != "origin/main" {
		t.Fatalf("upstream %q %v", up, err)
	}
	if ahead, behind := p.counts(t); ahead != 0 || behind != 0 {
		t.Fatalf("ahead %d behind %d", ahead, behind)
	}
	if paths, err := p.g.ChangedPaths(ctx, p.checkout); err != nil || len(paths) != 0 {
		t.Fatalf("changed %q %v", paths, err)
	}
}

func TestRealCheckoutBehindAheadAndDiverged(t *testing.T) {
	p := newUpstreamPair(t)
	p.pushFromElsewhere(t)
	if ahead, behind := p.counts(t); ahead != 0 || behind != 1 {
		t.Fatalf("behind: ahead %d behind %d", ahead, behind)
	}
	p.commit(t, p.checkout, "mine")
	if ahead, behind := p.counts(t); ahead != 1 || behind != 1 {
		t.Fatalf("diverged: ahead %d behind %d", ahead, behind)
	}
	q := newUpstreamPair(t)
	q.commit(t, q.checkout, "mine")
	if ahead, behind := q.counts(t); ahead != 1 || behind != 0 {
		t.Fatalf("ahead: ahead %d behind %d", ahead, behind)
	}
}

func TestRealCheckoutChangedPathsCountUntrackedButNotIgnored(t *testing.T) {
	p := newUpstreamPair(t)
	for name, text := range map[string]string{".gitignore": "scratch/\n", "first": "edited\n", "scratch/x": "x\n", "new.txt": "n\n"} {
		path := filepath.Join(p.checkout, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	paths, err := p.g.ChangedPaths(context.Background(), p.checkout)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(paths)
	if want := []string{".gitignore", "first", "new.txt"}; !slices.Equal(paths, want) {
		t.Fatalf("got %q, want %q", paths, want)
	}
}

func TestRealCheckoutDetachedAndWithNoUpstream(t *testing.T) {
	p := newUpstreamPair(t)
	ctx := context.Background()
	p.git(t, p.checkout, "switch", "--quiet", "-c", "local")
	if has, err := p.g.HasUpstream(ctx, p.checkout, "local"); err != nil || has {
		t.Fatalf("no upstream: %v %v", has, err)
	}
	p.git(t, p.checkout, "switch", "--quiet", "--detach", "main")
	if branch, on, err := p.g.Branch(ctx, p.checkout); err != nil || on || branch != "" {
		t.Fatalf("detached: %q %v %v", branch, on, err)
	}
}

func TestRealCheckoutFetchFromAnUpstreamThatIsGone(t *testing.T) {
	p := newUpstreamPair(t)
	if err := os.RemoveAll(p.bare); err != nil {
		t.Fatal(err)
	}
	if err := p.g.FetchUpstream(context.Background(), p.checkout); err == nil {
		t.Fatal("want the fetch to fail")
	}
}
