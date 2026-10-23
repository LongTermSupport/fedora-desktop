package forge_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/forge"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

const (
	wall0    = int64(1_800_000_000_000)
	firstTok = "gho_firstTOKEN"
	sha      = "1111111111111111111111111111111111111111"
	sha2     = "2222222222222222222222222222222222222222"
	headSHA  = "3333333333333333333333333333333333333333"
	tool     = "/repos/acme/tool"
	lib      = "/repos/other/lib"
)

var ghArgv = []string{"gh", "auth", "token", "--hostname", "github.com"}

// ghub is a fake GitHub API, scripted per request URI; an unscripted URI is 404.
type ghub struct {
	t      *testing.T
	mu     sync.Mutex
	routes map[string]http.HandlerFunc
	hits   []string
	auth   []string
	hit    chan string
	srv    *httptest.Server
}

func newGhub(t *testing.T) *ghub {
	t.Helper()
	g := &ghub{t: t, routes: map[string]http.HandlerFunc{}, hit: make(chan string, 64)}
	g.srv = httptest.NewServer(http.HandlerFunc(g.serve))
	t.Cleanup(g.srv.Close)
	return g
}

func (g *ghub) serve(w http.ResponseWriter, r *http.Request) {
	uri := r.URL.RequestURI()
	g.mu.Lock()
	g.hits = append(g.hits, uri)
	g.auth = append(g.auth, r.Header.Get("Authorization"))
	h := g.routes[uri]
	g.mu.Unlock()
	select {
	case g.hit <- uri:
	default:
	}
	if h == nil {
		http.NotFound(w, r)
		return
	}
	h(w, r)
}

func (g *ghub) route(uri string, h http.HandlerFunc) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.routes[uri] = h
}

func (g *ghub) json(uri string, status int, body string) {
	g.route(uri, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		g.write(w, body)
	})
}

func (g *ghub) write(w io.Writer, body string) {
	if _, err := io.WriteString(w, body); err != nil {
		g.t.Errorf("fake GitHub: write: %v", err)
	}
}

func (g *ghub) count(uri string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := 0
	for _, h := range g.hits {
		if h == uri {
			n++
		}
	}
	return n
}

func (g *ghub) total() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.hits)
}

func (g *ghub) auths() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.auth...)
}

// rigClock is the fake clock whose Sleep moves time by the wait, so a rate wait needs no
// second goroutine, and records each wait.
type rigClock struct {
	*clock.Fake

	mu     sync.Mutex
	sleeps []time.Duration
}

func (c *rigClock) Sleep(ctx context.Context, d time.Duration) error {
	c.mu.Lock()
	c.sleeps = append(c.sleeps, d)
	c.mu.Unlock()
	c.Advance(d)
	return ctx.Err()
}

func (c *rigClock) waits() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.sleeps...)
}

type rig struct {
	gh  *ghub
	run *execx.Fake
	clk *rigClock
	c   *forge.Checker
}

func newRig(t *testing.T) *rig {
	t.Helper()
	clk := &rigClock{Fake: clock.NewFake(time.Hour, wall0)}
	run := &execx.Fake{}
	run.On(ghArgv, execx.Result{Stdout: []byte(firstTok + "\n")})
	gh := newGhub(t)
	c := forge.New(forge.Config{Login: forge.NewLogin(run, clk), Clock: clk})
	forge.SetOrigin(c, gh.srv.URL)
	return &rig{gh: gh, run: run, clk: clk, c: c}
}

func (r *rig) ghRuns() int {
	n := 0
	for _, c := range r.run.Calls() {
		if c.Line() == strings.Join(ghArgv, " ") {
			n++
		}
	}
	return n
}

func (r *rig) check(t *testing.T, n proto.ForgeCheck, f proto.Forge) (forge.Result, error) {
	t.Helper()
	return r.c.Check(context.Background(), n, f)
}

func (r *rig) expect(t *testing.T, n proto.ForgeCheck, f proto.Forge, want string) {
	t.Helper()
	res, err := r.check(t, n, f)
	if err != nil {
		t.Fatalf("Check(%s): error %v", n.Ref.Text, err)
	}
	if res.Outcome != want {
		t.Fatalf("Check(%s) = %q, want %q", n.Ref.Text, res.Outcome, want)
	}
}

func listed(branches ...string) proto.Forge {
	return proto.Forge{Kind: proto.ForgeGitHub, Repos: []proto.ForgeRepo{{Repo: "acme/tool", Branches: branches}}}
}

func orgForge() proto.Forge { return proto.Forge{Kind: proto.ForgeGitHub, Org: "acme"} }

func externalForge() proto.Forge { return proto.Forge{Kind: proto.ForgeGitHub} }

func need(t *testing.T, ref string, f proto.Forge, branch string) proto.ForgeCheck {
	t.Helper()
	r, ok := proto.ParseRef(ref)
	if !ok {
		t.Fatalf("ParseRef(%q) failed", ref)
	}
	return proto.ForgeCheck{Ref: r, Class: f.Class(r), Branch: branch, Branches: f.ListedBranches(r)}
}

func repoJSON(full, owner string, fork, private bool, def string) string {
	return fmt.Sprintf(`{"full_name":%q,"owner":{"login":%q},"fork":%t,"private":%t,"default_branch":%q}`,
		full, owner, fork, private, def)
}

func commitRef(repo, s string) string { return "commit:" + repo + "@" + s }

func compare(repo, base, head string) string { return repo + "/compare/" + base + "..." + head }

func status(s string) string { return `{"status":"` + s + `"}` }

// acme is acme/tool as GitHub describes it: owned by acme, not a fork, public, main default.
func (r *rig) acme() { r.gh.json(tool, 200, repoJSON("acme/tool", "acme", false, false, "main")) }

func (r *rig) other() { r.gh.json(lib, 200, repoJSON("other/lib", "other", false, false, "main")) }

func TestSD5_1TokenReadOnceHeldTenMinutes(t *testing.T) {
	r := newRig(t)
	r.acme()
	r.gh.json(compare(tool, "main", sha), 200, status("behind"))
	r.gh.json(compare(tool, "main", sha2), 200, status("identical"))
	r.expect(t, need(t, commitRef("acme/tool", sha), orgForge(), ""), orgForge(), forge.Proven)
	if got := r.ghRuns(); got != 1 {
		t.Fatalf("gh ran %d times on the first check, want 1", got)
	}
	r.clk.Advance(10*time.Minute - time.Millisecond)
	r.expect(t, need(t, commitRef("acme/tool", sha2), orgForge(), ""), orgForge(), forge.Proven)
	if got := r.ghRuns(); got != 1 {
		t.Fatalf("gh ran %d times inside 10 minutes, want 1", got)
	}
	r.gh.json(compare(tool, "main", headSHA), 200, status("behind"))
	r.clk.Advance(time.Millisecond)
	r.expect(t, need(t, commitRef("acme/tool", headSHA), orgForge(), ""), orgForge(), forge.Proven)
	if got := r.ghRuns(); got != 2 {
		t.Fatalf("gh ran %d times past 10 minutes, want 2", got)
	}
}

func TestSD5_1ConcurrentChecksShareOneRead(t *testing.T) {
	r := newRig(t)
	r.acme()
	shas := []string{sha, sha2, headSHA}
	for _, s := range shas {
		r.gh.json(compare(tool, "main", s), 200, status("behind"))
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(shas))
	for _, s := range shas {
		wg.Go(func() {
			n := need(t, commitRef("acme/tool", s), orgForge(), "")
			res, err := r.c.Check(context.Background(), n, orgForge())
			if err == nil && res.Outcome != forge.Proven {
				err = fmt.Errorf("%s: %q", s, res.Outcome)
			}
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := r.ghRuns(); got != 1 {
		t.Fatalf("gh ran %d times for three concurrent checks, want 1", got)
	}
}

func TestSD5_2UnauthorisedDropsToken(t *testing.T) {
	r := newRig(t)
	var tries atomic.Int32
	r.gh.route(tool, func(w http.ResponseWriter, _ *http.Request) {
		if tries.Add(1) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		r.gh.write(w, repoJSON("acme/tool", "acme", false, false, "main"))
	})
	r.gh.json(compare(tool, "main", sha), 200, status("identical"))
	reads := 0
	r.run.OnFunc(ghArgv, func(execx.Cmd) (execx.Result, error) {
		reads++
		return execx.Result{Stdout: []byte(fmt.Sprintf("gho_tok%d\n", reads))}, nil
	})
	r.expect(t, need(t, commitRef("acme/tool", sha), orgForge(), ""), orgForge(), forge.Proven)
	if got := r.ghRuns(); got != 2 {
		t.Fatalf("gh ran %d times around a 401, want 2", got)
	}
	auths := r.gh.auths()
	if auths[0] != "Bearer gho_tok1" || auths[1] != "Bearer gho_tok2" {
		t.Fatalf("authorizations %q: the retry must carry the token read again", auths)
	}
}

func TestSD5_2SecondUnauthorisedIsUnresolved(t *testing.T) {
	r := newRig(t)
	r.gh.json(tool, http.StatusUnauthorized, `{}`)
	r.expect(t, need(t, commitRef("acme/tool", sha), orgForge(), ""), orgForge(), proto.ReasonUnresolved)
	if got := r.gh.count(tool); got != 2 {
		t.Fatalf("%d requests, want the first and one retry", got)
	}
	r.expect(t, need(t, commitRef("acme/tool", sha), orgForge(), ""), orgForge(), proto.ReasonUnresolved)
	if got := r.ghRuns(); got != 4 {
		t.Fatalf("gh ran %d times, want each 401 to drop the token (4)", got)
	}
}

func TestSD5_3PositiveResultsKeptAnHour(t *testing.T) {
	r := newRig(t)
	r.acme()
	r.gh.json(compare(tool, "main", sha), 200, status("behind"))
	n := need(t, commitRef("acme/tool", sha), orgForge(), "")
	r.expect(t, n, orgForge(), forge.Proven)
	before := r.gh.total()
	r.clk.Advance(3599 * time.Second)
	r.expect(t, n, orgForge(), forge.Proven)
	if got := r.gh.total(); got != before {
		t.Fatalf("%d requests at 3599 s, want none", got-before)
	}
	r.clk.Advance(time.Second)
	r.expect(t, n, orgForge(), forge.Proven)
	if got := r.gh.count(compare(tool, "main", sha)); got != 2 {
		t.Fatalf("compare asked %d times by 3600 s, want 2", got)
	}
}

func TestSD5_3NegativeResultNeverCached(t *testing.T) {
	r := newRig(t)
	r.acme()
	r.gh.json(compare(tool, "main", sha), 200, status("diverged"))
	n := need(t, commitRef("acme/tool", sha), orgForge(), "")
	r.expect(t, n, orgForge(), proto.ReasonProvenance)
	r.expect(t, n, orgForge(), proto.ReasonProvenance)
	if got := r.gh.count(compare(tool, "main", sha)); got != 2 {
		t.Fatalf("a diverged compare asked %d times, want 2 (never cached)", got)
	}
	r.gh.json(tool, 200, repoJSON("acme/tool", "acme", true, false, "main"))
	r.c = forge.New(forge.Config{Login: forge.NewLogin(r.run, r.clk), Clock: r.clk})
	forge.SetOrigin(r.c, r.gh.srv.URL)
	r.expect(t, n, orgForge(), proto.ReasonProvenance)
	r.expect(t, n, orgForge(), proto.ReasonProvenance)
	if got := r.gh.count(tool); got != 3 {
		t.Fatalf("a fork's metadata asked %d times in all, want 3 (never cached)", got)
	}
}

func TestSD5_3CachesEvictOldest(t *testing.T) {
	r := newRig(t)
	r.acme()
	for i := range forge.CacheEntries + 1 {
		s := fmt.Sprintf("%040x", i+1)
		r.gh.json(compare(tool, "main", s), 200, status("behind"))
		r.expect(t, need(t, commitRef("acme/tool", s), orgForge(), ""), orgForge(), forge.Proven)
	}
	first := fmt.Sprintf("%040x", 1)
	last := fmt.Sprintf("%040x", forge.CacheEntries+1)
	r.expect(t, need(t, commitRef("acme/tool", last), orgForge(), ""), orgForge(), forge.Proven)
	r.expect(t, need(t, commitRef("acme/tool", first), orgForge(), ""), orgForge(), forge.Proven)
	if got := r.gh.count(compare(tool, "main", last)); got != 1 {
		t.Fatalf("the newest entry asked %d times, want 1", got)
	}
	if got := r.gh.count(compare(tool, "main", first)); got != 2 {
		t.Fatalf("the oldest entry asked %d times, want 2 (evicted)", got)
	}
}

func TestSD5_4LoginFailures(t *testing.T) {
	cases := map[string]func(*execx.Fake){
		"absent":   func(f *execx.Fake) { f.OnError(ghArgv, &execx.NotInstalledError{Name: "gh"}) },
		"exit 1":   func(f *execx.Fake) { f.On(ghArgv, execx.Result{ExitCode: 1, Stderr: []byte("not logged in")}) },
		"empty":    func(f *execx.Fake) { f.On(ghArgv, execx.Result{Stdout: []byte("\n")}) },
		"long":     func(f *execx.Fake) { f.On(ghArgv, execx.Result{Stdout: []byte(strings.Repeat("a", 1025) + "\n")}) },
		"space":    func(f *execx.Fake) { f.On(ghArgv, execx.Result{Stdout: []byte("gho a\n")}) },
		"two nl":   func(f *execx.Fake) { f.On(ghArgv, execx.Result{Stdout: []byte("gho\n\n")}) },
		"non-utf8": func(f *execx.Fake) { f.On(ghArgv, execx.Result{Stdout: []byte("gho\xff")}) },
	}
	for name, script := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			script(r.run)
			r.acme()
			res, err := r.check(t, need(t, commitRef("acme/tool", sha), orgForge(), ""), orgForge())
			if !errors.Is(err, forge.ErrLogin) || res.Outcome != proto.ReasonUnresolved {
				t.Fatalf("Check = %q, %v; want unresolved, ErrLogin", res.Outcome, err)
			}
			if r.gh.total() != 0 {
				t.Fatal("a request went out with no login")
			}
		})
	}
}

func TestSD5_4LongestTokenAccepted(t *testing.T) {
	r := newRig(t)
	r.run.On(ghArgv, execx.Result{Stdout: []byte(strings.Repeat("!", 512) + strings.Repeat("~", 512))})
	l := forge.NewLogin(r.run, r.clk)
	if _, err := l.Token(context.Background()); err != nil {
		t.Fatalf("a 1024-byte token without a newline: %v", err)
	}
}

func TestSD5_4CachedFactsRunNoGh(t *testing.T) {
	r := newRig(t)
	r.acme()
	r.gh.json(compare(tool, "main", sha), 200, status("behind"))
	n := need(t, commitRef("acme/tool", sha), orgForge(), "")
	r.expect(t, n, orgForge(), forge.Proven)
	r.run.On(ghArgv, execx.Result{ExitCode: 1})
	r.clk.Advance(30 * time.Minute)
	r.expect(t, n, orgForge(), forge.Proven)
	if got := r.ghRuns(); got != 1 {
		t.Fatalf("gh ran %d times, want 1: a check whose facts are cached runs none", got)
	}
}

func TestSD5_4ContextEndedDuringLogin(t *testing.T) {
	r := newRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	r.run.OnFunc(ghArgv, func(execx.Cmd) (execx.Result, error) {
		cancel()
		return execx.Result{}, context.Canceled
	})
	r.acme()
	res, err := r.c.Check(ctx, need(t, commitRef("acme/tool", sha), orgForge(), ""), orgForge())
	if !errors.Is(err, context.Canceled) || errors.Is(err, forge.ErrLogin) || res.Outcome != proto.ReasonUnresolved {
		t.Fatalf("Check = %q, %v; want unresolved with the context's error", res.Outcome, err)
	}
}

func TestSD5_4DropForgetsToken(t *testing.T) {
	r := newRig(t)
	l := forge.NewLogin(r.run, r.clk)
	for range 2 {
		if _, err := l.Token(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	l.Drop()
	if _, err := l.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := r.ghRuns(); got != 2 {
		t.Fatalf("gh ran %d times, want 2 (once, then once after Drop)", got)
	}
}

type proofCase struct {
	name   string
	f      proto.Forge
	ref    string
	branch string
	routes func(r *rig)
	want   string
}

const (
	commit   = "commit:acme/tool@" + sha
	pathRef  = "path:acme/tool@" + sha + ":docs/a.md"
	contents = tool + "/contents/docs/a.md?ref=" + sha
	prRef    = "pr:acme/tool#7@" + sha
)

func pull(full, assoc string) string {
	return fmt.Sprintf(`{"head":{"sha":%q,"repo":{"full_name":%q}},"author_association":%q}`, headSHA, full, assoc)
}

func both(fns ...func(r *rig)) func(r *rig) {
	return func(r *rig) {
		for _, fn := range fns {
			fn(r)
		}
	}
}

func js(uri string, code int, body string) func(r *rig) {
	return func(r *rig) { r.gh.json(uri, code, body) }
}

func acme(r *rig) { r.acme() }

func proofCases() []proofCase {
	return slices.Concat(listedCases(), orgCases(), externalPathCases(), prIssueCases())
}

func listedCases() []proofCase {
	return []proofCase{
		{
			"listed branches, ping branch listed", listed("main", "rel"), commit, "rel",
			both(acme, js(tool+"/branches/rel", 200, `{}`), js(compare(tool, "rel", sha), 200, status("behind"))), forge.Proven,
		},
		{"listed branches, ping branch not listed", listed("main"), commit, "dev", acme, proto.ReasonProvenance},
		{"listed branches, ping branch missing", listed("main", "rel"), commit, "rel", acme, proto.ReasonProvenance},
		{
			"listed branches, any listed branch reaches", listed("main", "rel"), commit, "",
			both(acme, js(compare(tool, "main", sha), 200, status("diverged")), js(compare(tool, "rel", sha), 200, status("identical"))), forge.Proven,
		},
		{
			"listed branches, none reaches", listed("main", "rel"), commit, "",
			both(acme, js(compare(tool, "main", sha), 200, status("ahead")), js(compare(tool, "rel", sha), 200, status("diverged"))), proto.ReasonProvenance,
		},
		{
			"listed, name differs", listed(), commit, "",
			both(js(tool, 200, repoJSON("acme/renamed", "acme", false, false, "main")), js(compare(tool, "main", sha), 200, status("behind"))), proto.ReasonProvenance,
		},
		{
			"listed, a fork", listed(), commit, "",
			both(js(tool, 200, repoJSON("acme/tool", "acme", true, false, "main")), js(compare(tool, "main", sha), 200, status("behind"))), proto.ReasonProvenance,
		},
		{
			"listed, private allowed, default branch", listed(), commit, "",
			both(js(tool, 200, repoJSON("Acme/Tool", "acme", false, true, "trunk")), js(compare(tool, "trunk", sha), 200, status("behind"))), forge.Proven,
		},
		{
			"listed, ping branch exists", listed(), commit, "feat/x",
			both(acme, js(tool+"/branches/feat/x", 200, `{}`), js(compare(tool, "feat/x", sha), 200, status("identical"))), forge.Proven,
		},
		{"listed, ping branch missing", listed(), commit, "feat/x", acme, proto.ReasonProvenance},
		{"listed, ping branch with a dot-dot", listed(), commit, "a/../b", acme, proto.ReasonProvenance},
		{
			"listed branches, a roster branch with a dot-dot", listed("../x", "main"), commit, "",
			both(acme, js(compare(tool, "main", sha), 200, status("behind"))), forge.Proven,
		},
	}
}

func orgCases() []proofCase {
	return []proofCase{
		{"org, owner matches", orgForge(), commit, "", both(acme, js(compare(tool, "main", sha), 200, status("behind"))), forge.Proven},
		{
			"org, owner moved", orgForge(), commit, "",
			both(js(tool, 200, repoJSON("acme/tool", "someone", false, false, "main")), js(compare(tool, "main", sha), 200, status("behind"))), proto.ReasonProvenance,
		},
		{
			"org, a fork", orgForge(), commit, "",
			both(js(tool, 200, repoJSON("acme/tool", "acme", true, false, "main")), js(compare(tool, "main", sha), 200, status("behind"))), proto.ReasonProvenance,
		},
		{"org, served from a fork's parent", orgForge(), commit, "", both(acme, js(compare(tool, "main", sha), 200, status("diverged"))), proto.ReasonProvenance},
		{"org, commit not found", orgForge(), commit, "", acme, proto.ReasonUnresolved},
		{"org, no default branch", orgForge(), commit, "", js(tool, 200, repoJSON("acme/tool", "acme", false, false, "")), proto.ReasonUnresolved},
		{"repository not found", orgForge(), commit, "", func(*rig) {}, proto.ReasonUnresolved},
		{"repository answer not JSON", orgForge(), commit, "", js(tool, 200, `{"full_name":`), proto.ReasonUnresolved},
		{"compare answer not JSON", orgForge(), commit, "", both(acme, js(compare(tool, "main", sha), 200, `[`)), proto.ReasonUnresolved},
	}
}

func externalPathCases() []proofCase {
	return []proofCase{
		{
			"external, public", externalForge(), commitRef("other/lib", sha), "",
			func(r *rig) { r.other(); r.gh.json(compare(lib, "main", sha), 200, status("behind")) }, forge.Proven,
		},
		{
			"external, private", externalForge(), commitRef("other/lib", sha), "",
			func(r *rig) {
				r.gh.json(lib, 200, repoJSON("other/lib", "other", false, true, "main"))
				r.gh.json(compare(lib, "main", sha), 200, status("behind"))
			}, proto.ReasonProvenance,
		},
		{
			"external, a fork", externalForge(), commitRef("other/lib", sha), "",
			func(r *rig) {
				r.gh.json(lib, 200, repoJSON("other/lib", "other", true, false, "main"))
				r.gh.json(compare(lib, "main", sha), 200, status("behind"))
			}, proto.ReasonProvenance,
		},
		{
			"external, branch reaches", externalForge(), commitRef("other/lib", sha), "dev",
			func(r *rig) {
				r.other()
				r.gh.json(lib+"/branches/dev", 200, `{}`)
				r.gh.json(compare(lib, "dev", sha), 200, status("behind"))
			}, forge.Proven,
		},
		{
			"external, branch does not reach", externalForge(), commitRef("other/lib", sha), "dev",
			func(r *rig) {
				r.other()
				r.gh.json(lib+"/branches/dev", 200, `{}`)
				r.gh.json(compare(lib, "dev", sha), 200, status("ahead"))
			}, proto.ReasonProvenance,
		},
		{
			"path, a file", orgForge(), pathRef, "",
			both(acme, js(compare(tool, "main", sha), 200, status("behind")), js(contents, 200, `{"type":"file"}`)), forge.Proven,
		},
		{
			"path, a directory", orgForge(), pathRef, "",
			both(acme, js(compare(tool, "main", sha), 200, status("behind")), js(contents, 200, `[{"type":"file"}]`)), proto.ReasonUnresolved,
		},
		{
			"path, a symlink", orgForge(), pathRef, "",
			both(acme, js(compare(tool, "main", sha), 200, status("behind")), js(contents, 200, `{"type":"symlink"}`)), proto.ReasonUnresolved,
		},
		{"path, not found", orgForge(), pathRef, "", both(acme, js(compare(tool, "main", sha), 200, status("behind"))), proto.ReasonUnresolved},
		{
			"path, commit unproven", orgForge(), pathRef, "",
			both(acme, js(compare(tool, "main", sha), 200, status("diverged")), js(contents, 200, `{"type":"file"}`)), proto.ReasonProvenance,
		},
	}
}

func prIssueCases() []proofCase {
	return []proofCase{
		{
			"pr, proven", orgForge(), prRef, "",
			both(acme, js(tool+"/pulls/7", 200, pull("acme/tool", "MEMBER")), js(compare(tool, sha, headSHA), 200, status("ahead"))), forge.Proven,
		},
		{
			"pr, head at the sha", listed(), prRef, "",
			both(acme, js(tool+"/pulls/7", 200, pull("acme/tool", "OWNER")), js(compare(tool, sha, headSHA), 200, status("identical"))), forge.Proven,
		},
		{
			"pr, collaborator", orgForge(), prRef, "",
			both(acme, js(tool+"/pulls/7", 200, pull("acme/tool", "COLLABORATOR")), js(compare(tool, sha, headSHA), 200, status("ahead"))), forge.Proven,
		},
		{
			"pr, head from a fork", orgForge(), prRef, "",
			both(acme, js(tool+"/pulls/7", 200, pull("evil/tool", "MEMBER")), js(compare(tool, sha, headSHA), 200, status("ahead"))), proto.ReasonProvenance,
		},
		{
			"pr, head repository gone", orgForge(), prRef, "",
			both(acme, js(tool+"/pulls/7", 200, `{"head":{"sha":"`+headSHA+`","repo":null},"author_association":"MEMBER"}`)), proto.ReasonProvenance,
		},
		{
			"pr, outsider", orgForge(), prRef, "",
			both(acme, js(tool+"/pulls/7", 200, pull("acme/tool", "CONTRIBUTOR")), js(compare(tool, sha, headSHA), 200, status("ahead"))), proto.ReasonProvenance,
		},
		{
			"pr, external outsider", externalForge(), "pr:other/lib#7@" + sha, "",
			func(r *rig) {
				r.other()
				r.gh.json(lib+"/pulls/7", 200, pull("other/lib", "NONE"))
				r.gh.json(compare(lib, sha, headSHA), 200, status("ahead"))
			}, forge.Proven,
		},
		{
			"pr, head behind the sha", orgForge(), prRef, "",
			both(acme, js(tool+"/pulls/7", 200, pull("acme/tool", "MEMBER")), js(compare(tool, sha, headSHA), 200, status("behind"))), proto.ReasonProvenance,
		},
		{"pr, sha not found", orgForge(), prRef, "", both(acme, js(tool+"/pulls/7", 200, pull("acme/tool", "MEMBER"))), proto.ReasonUnresolved},
		{
			"pr, bad head sha", orgForge(), prRef, "",
			both(acme, js(tool+"/pulls/7", 200, `{"head":{"sha":"x","repo":{"full_name":"acme/tool"}},"author_association":"MEMBER"}`)), proto.ReasonUnresolved,
		},
		{"pr, not JSON", orgForge(), prRef, "", both(acme, js(tool+"/pulls/7", 200, `]`)), proto.ReasonUnresolved},
		{"pr, not found", orgForge(), prRef, "", acme, proto.ReasonUnresolved},
		{"issue, exists", orgForge(), "issue:acme/tool#9", "", both(acme, js(tool+"/issues/9", 200, `{"number":9,"pull_request":null}`)), forge.Proven},
		{
			"issue, a pull request", orgForge(), "issue:acme/tool#9", "",
			both(acme, js(tool+"/issues/9", 200, `{"number":9,"pull_request":{"url":"x"}}`)), proto.ReasonUnresolved,
		},
		{"issue, gone", orgForge(), "issue:acme/tool#9", "", both(acme, js(tool+"/issues/9", 410, `{}`)), proto.ReasonUnresolved},
		{"issue, not JSON", orgForge(), "issue:acme/tool#9", "", both(acme, js(tool+"/issues/9", 200, `nope`)), proto.ReasonUnresolved},
		{
			"issue, external", externalForge(), "issue:other/lib#9", "",
			func(r *rig) { r.other(); r.gh.json(lib+"/issues/9", 200, `{"number":9}`) }, forge.Proven,
		},
	}
}

func TestSD5_5ProofTables(t *testing.T) {
	for _, pc := range proofCases() {
		t.Run(pc.name, func(t *testing.T) {
			r := newRig(t)
			pc.routes(r)
			n := need(t, pc.ref, pc.f, pc.branch)
			r.expect(t, n, pc.f, pc.want)
			if pc.want == forge.Proven {
				wantTrust := proto.TrustTrusted
				if n.Class == proto.RefExternal {
					wantTrust = proto.TrustExternal
				}
				if n.Trust() != wantTrust {
					t.Fatalf("trust %q, want %q", n.Trust(), wantTrust)
				}
			}
		})
	}
}

func TestSD5_5ClassesThatNeverProve(t *testing.T) {
	r := newRig(t)
	r.acme()
	n := need(t, commitRef("acme/tool", sha), orgForge(), "")
	n.Class = proto.RefExcluded
	r.expect(t, n, orgForge(), proto.ReasonUnresolved)
	unset := forge.New(forge.Config{Login: forge.NewLogin(r.run, r.clk), Clock: r.clk})
	res, err := unset.Check(context.Background(), need(t, commitRef("acme/tool", sha), orgForge(), ""), proto.Forge{Kind: "gitlab", Org: "acme"})
	if err == nil || res.Outcome != proto.ReasonUnresolved {
		t.Fatalf("an unknown forge kind = %q, %v; want unresolved with an error", res.Outcome, err)
	}
	if r.gh.total() != 0 {
		t.Fatal("a check that cannot prove made a request")
	}
}

func TestSD5_6TokenOnlyInAuthorizationToOrigin(t *testing.T) {
	r := newRig(t)
	r.gh.route(tool, func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "/repositories/1", http.StatusMovedPermanently)
	})
	r.gh.json("/repositories/1", 200, repoJSON("acme/tool", "acme", false, false, "main"))
	r.gh.json(compare(tool, "main", sha), 200, status("behind"))
	r.expect(t, need(t, commitRef("acme/tool", sha), orgForge(), ""), orgForge(), forge.Proven)
	auths := r.gh.auths()
	if len(auths) != 3 {
		t.Fatalf("%d requests, want 3", len(auths))
	}
	for i, a := range auths {
		if a != "Bearer "+firstTok {
			t.Fatalf("request %d Authorization %q", i, a)
		}
	}
	r.gh.mu.Lock()
	defer r.gh.mu.Unlock()
	for _, h := range r.gh.hits {
		if strings.Contains(h, firstTok) {
			t.Fatalf("the token is in a request URI: %s", h)
		}
	}
}

func TestSD5_6RedirectToAnotherOriginRefused(t *testing.T) {
	r := newRig(t)
	other := newGhub(t)
	r.gh.route(tool, func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, other.srv.URL+tool, http.StatusFound)
	})
	res, err := r.check(t, need(t, commitRef("acme/tool", sha), orgForge(), ""), orgForge())
	if err != nil || res.Outcome != proto.ReasonUnresolved {
		t.Fatalf("Check = %q, %v; want unresolved", res.Outcome, err)
	}
	if other.total() != 0 {
		t.Fatal("the other origin received a request")
	}
}

func TestSD5_6RedirectLoopRefused(t *testing.T) {
	r := newRig(t)
	r.gh.route(tool, func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, tool, http.StatusFound)
	})
	r.expect(t, need(t, commitRef("acme/tool", sha), orgForge(), ""), orgForge(), proto.ReasonUnresolved)
	if got := r.gh.count(tool); got > 6 {
		t.Fatalf("followed %d redirects", got)
	}
}

func TestSD5_6ResponseCap(t *testing.T) {
	body := repoJSON("acme/tool", "acme", false, false, "main")
	for _, tc := range []struct {
		size int
		want string
	}{{forge.MaxBody, forge.Proven}, {forge.MaxBody + 1, proto.ReasonUnresolved}} {
		r := newRig(t)
		r.gh.json(tool, 200, body+strings.Repeat(" ", tc.size-len(body)))
		r.gh.json(compare(tool, "main", sha), 200, status("behind"))
		r.expect(t, need(t, commitRef("acme/tool", sha), orgForge(), ""), orgForge(), tc.want)
	}
}

func TestSD5_6RequestHeldPastLimitIsDown(t *testing.T) {
	r := newRig(t)
	r.gh.route(tool, func(_ http.ResponseWriter, req *http.Request) { <-req.Context().Done() })
	type outcome struct {
		res forge.Result
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := r.c.Check(context.Background(), need(t, commitRef("acme/tool", sha), orgForge(), ""), orgForge())
		done <- outcome{res, err}
	}()
	<-r.gh.hit
	r.clk.Advance(15*time.Second - time.Millisecond)
	select {
	case got := <-done:
		t.Fatalf("the request ended before 15 s: %v", got.err)
	default:
	}
	r.clk.Advance(time.Millisecond)
	got := <-done
	if !errors.Is(got.err, forge.ErrDown) || got.res.Outcome != proto.ReasonUnresolved {
		t.Fatalf("Check = %q, %v; want unresolved, ErrDown", got.res.Outcome, got.err)
	}
}

func TestSD5_6CallerContextEndedIsNotDown(t *testing.T) {
	r := newRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	r.gh.route(tool, func(_ http.ResponseWriter, req *http.Request) {
		cancel()
		<-req.Context().Done()
	})
	res, err := r.c.Check(ctx, need(t, commitRef("acme/tool", sha), orgForge(), ""), orgForge())
	if !errors.Is(err, context.Canceled) || errors.Is(err, forge.ErrDown) || res.Outcome != proto.ReasonUnresolved {
		t.Fatalf("Check = %q, %v; want unresolved with the context's error", res.Outcome, err)
	}
	r.acme()
	r.gh.json(compare(tool, "main", sha), 200, status("behind"))
	r.expect(t, need(t, commitRef("acme/tool", sha), orgForge(), ""), orgForge(), forge.Proven)
}

func TestSD5_7RateWaits(t *testing.T) {
	cases := []struct {
		name    string
		code    int
		headers map[string]string
		wait    time.Duration
	}{
		{"429 Retry-After", 429, map[string]string{"Retry-After": "7"}, 7 * time.Second},
		{"429 reset", 429, map[string]string{"X-Ratelimit-Reset": strconv.FormatInt(wall0/1000+2, 10)}, 2 * time.Second},
		{"429 default", 429, nil, 5 * time.Second},
		{"429 date Retry-After", 429, map[string]string{"Retry-After": "Wed, 21 Oct 2026 07:28:00 GMT"}, 5 * time.Second},
		{"403 remaining 0", 403, map[string]string{"X-Ratelimit-Remaining": "0", "X-Ratelimit-Reset": strconv.FormatInt(wall0/1000, 10)}, 0},
		{"403 Retry-After", 403, map[string]string{"Retry-After": "60"}, 60 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			var tries atomic.Int32
			r.gh.route(tool, func(w http.ResponseWriter, _ *http.Request) {
				if tries.Add(1) == 1 {
					for k, v := range tc.headers {
						w.Header().Set(k, v)
					}
					w.WriteHeader(tc.code)
					return
				}
				r.gh.write(w, repoJSON("acme/tool", "acme", false, false, "main"))
			})
			r.gh.json(compare(tool, "main", sha), 200, status("behind"))
			r.expect(t, need(t, commitRef("acme/tool", sha), orgForge(), ""), orgForge(), forge.Proven)
			if got := r.clk.waits(); len(got) != 1 || got[0] != tc.wait {
				t.Fatalf("waits %v, want [%v]", got, tc.wait)
			}
		})
	}
}

func TestSD5_7ThreeTriesThenForgeRate(t *testing.T) {
	r := newRig(t)
	r.gh.json(tool, http.StatusTooManyRequests, `{}`)
	r.expect(t, need(t, commitRef("acme/tool", sha), orgForge(), ""), orgForge(), proto.ReasonForgeRate)
	if got := r.gh.count(tool); got != 3 {
		t.Fatalf("%d tries, want 3", got)
	}
	if got := r.clk.waits(); len(got) != 2 {
		t.Fatalf("waits %v, want two", got)
	}
}

func TestSD5_7LongWaitNotSlept(t *testing.T) {
	r := newRig(t)
	r.gh.route(tool, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "61")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	r.expect(t, need(t, commitRef("acme/tool", sha), orgForge(), ""), orgForge(), proto.ReasonForgeRate)
	if got := r.clk.waits(); len(got) != 0 {
		t.Fatalf("slept %v for a wait over 60 s", got)
	}
}

func TestSD5_7SleepEndedByContext(t *testing.T) {
	r := newRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	r.gh.route(tool, func(w http.ResponseWriter, _ *http.Request) {
		cancel()
		w.WriteHeader(http.StatusTooManyRequests)
	})
	res, err := r.c.Check(ctx, need(t, commitRef("acme/tool", sha), orgForge(), ""), orgForge())
	if !errors.Is(err, context.Canceled) || res.Outcome != proto.ReasonUnresolved {
		t.Fatalf("Check = %q, %v; want unresolved with the context's error", res.Outcome, err)
	}
}

func TestSD5_7PlainForbiddenIsUnresolved(t *testing.T) {
	r := newRig(t)
	r.gh.json(tool, http.StatusForbidden, `{}`)
	r.expect(t, need(t, commitRef("acme/tool", sha), orgForge(), ""), orgForge(), proto.ReasonUnresolved)
	if got := r.gh.count(tool); got != 1 {
		t.Fatalf("%d tries for a plain 403, want 1", got)
	}
	r.gh.json(tool, http.StatusTeapot, `{}`)
	r.expect(t, need(t, commitRef("acme/tool", sha), orgForge(), ""), orgForge(), proto.ReasonUnresolved)
}

func TestSD5_8ForgeHealth(t *testing.T) {
	r := newRig(t)
	r.gh.json(tool, http.StatusBadGateway, `{}`)
	n := need(t, commitRef("acme/tool", sha), orgForge(), "")
	res, err := r.check(t, n, orgForge())
	if !errors.Is(err, forge.ErrDown) || res.Outcome != proto.ReasonUnresolved {
		t.Fatalf("a 502 = %q, %v; want unresolved, ErrDown", res.Outcome, err)
	}
	r.acme()
	r.gh.json(compare(tool, "main", sha), 200, status("behind"))
	before := r.gh.total()
	r.clk.Advance(59999 * time.Millisecond)
	res, err = r.check(t, need(t, commitRef("other/lib", sha), externalForge(), ""), externalForge())
	if !errors.Is(err, forge.ErrDown) || res.Outcome != proto.ReasonUnresolved || r.gh.total() != before {
		t.Fatalf("at 59.999 s: %q, %v, %d requests; want ErrDown and none", res.Outcome, err, r.gh.total()-before)
	}
	r.clk.Advance(time.Millisecond)
	r.expect(t, n, orgForge(), forge.Proven)
}

func TestSD5_8NoConnectionIsDown(t *testing.T) {
	r := newRig(t)
	r.gh.srv.Close()
	res, err := r.check(t, need(t, commitRef("acme/tool", sha), orgForge(), ""), orgForge())
	if !errors.Is(err, forge.ErrDown) || res.Outcome != proto.ReasonUnresolved {
		t.Fatalf("no connection = %q, %v; want unresolved, ErrDown", res.Outcome, err)
	}
}

func TestSD5_8DefaultClockAndOrigin(t *testing.T) {
	run := &execx.Fake{}
	c := forge.New(forge.Config{Login: forge.NewLogin(run, nil)})
	if got := forge.OriginOf(c, proto.ForgeGitHub); got != "https://api.github.com" {
		t.Fatalf("origin %q", got)
	}
}

func TestSD5_7RateWaitPastTheBoundIsForgeRate(t *testing.T) {
	r := newRig(t)
	r.gh.route(tool, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusForbidden)
	})
	ctx := clock.WithBound(context.Background(), r.clk.Boot()+30*time.Second)
	res, err := r.c.Check(ctx, need(t, commitRef("acme/tool", sha), orgForge(), ""), orgForge())
	if err != nil || res.Outcome != proto.ReasonForgeRate {
		t.Fatalf("a 60 s wait under a 30 s bound = %q, %v; want forge-rate and no error", res.Outcome, err)
	}
	if got := r.clk.waits(); len(got) != 0 {
		t.Fatalf("slept %v past the caller's bound", got)
	}
	r.gh.route(tool, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "20")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	r.clk.Advance(time.Minute)
	ctx = clock.WithBound(context.Background(), r.clk.Boot()+30*time.Second)
	if _, err := r.c.Check(ctx, need(t, commitRef("acme/tool", sha), orgForge(), ""), orgForge()); err != nil {
		t.Fatal(err)
	}
	if got := r.clk.waits(); len(got) != 1 || got[0] != 20*time.Second {
		t.Fatalf("waits %v; want the one 20 s wait inside the bound, then forge-rate", got)
	}
}

func TestSD5_7RateHealth(t *testing.T) {
	r := newRig(t)
	r.gh.route(tool, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "61")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	r.expect(t, need(t, commitRef("acme/tool", sha), orgForge(), ""), orgForge(), proto.ReasonForgeRate)
	r.other()
	r.gh.json(compare(lib, "main", sha), 200, status("behind"))
	before := r.gh.total()
	r.clk.Advance(61*time.Second - time.Millisecond)
	r.expect(t, need(t, commitRef("other/lib", sha), externalForge(), ""), externalForge(), proto.ReasonForgeRate)
	if got := r.gh.total() - before; got != 0 {
		t.Fatalf("%d requests inside the rate wait, want none", got)
	}
	r.clk.Advance(time.Millisecond)
	r.expect(t, need(t, commitRef("other/lib", sha), externalForge(), ""), externalForge(), forge.Proven)
}

// receive is a receive check's context whose sender budget answers allow, counting the asks.
func receive(allow bool, asked *int) context.Context {
	return team.WithReceive(context.Background(), func() bool { *asked++; return allow })
}

func TestSD5_9AReceiveCheckSpendsOncePerUncachedCheck(t *testing.T) {
	r := newRig(t)
	r.acme()
	r.gh.json(compare(tool, "main", sha), 200, status("behind"))
	n := need(t, commitRef("acme/tool", sha), orgForge(), "")
	asked := 0
	for range 2 {
		res, err := r.c.Check(receive(true, &asked), n, orgForge())
		if err != nil || res.Outcome != forge.Proven {
			t.Fatalf("Check = %q, %v", res.Outcome, err)
		}
	}
	if asked != 1 {
		t.Fatalf("the budget was asked %d times for one uncached and one cached check, want 1", asked)
	}
	before := r.gh.total()
	res, err := r.c.Check(receive(false, &asked), need(t, commitRef("acme/tool", sha2), orgForge(), ""), orgForge())
	if err != nil || res.Outcome != proto.ReasonUnresolved || r.gh.total() != before {
		t.Fatalf("past the budget: %q, %v, %d requests; want unresolved and none", res.Outcome, err, r.gh.total()-before)
	}
}

func TestSD5_9ReceiveChecksKeepTheReserve(t *testing.T) {
	r := newRig(t)
	resetS := wall0/1000 + 3600
	remaining := atomic.Int64{}
	remaining.Store(1000)
	rated := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Ratelimit-Limit", "5000")
			w.Header().Set("X-Ratelimit-Remaining", strconv.FormatInt(remaining.Load(), 10))
			w.Header().Set("X-Ratelimit-Reset", strconv.FormatInt(resetS, 10))
			r.gh.write(w, body)
		}
	}
	r.gh.route(tool, rated(repoJSON("acme/tool", "acme", false, false, "main")))
	for _, s := range []string{sha, sha2, headSHA} {
		r.gh.route(compare(tool, "main", s), rated(status("behind")))
	}
	asked := 0
	check := func(ctx context.Context, s string) (forge.Result, error) {
		return r.c.Check(ctx, need(t, commitRef("acme/tool", s), orgForge(), ""), orgForge())
	}
	if res, err := check(receive(true, &asked), sha); err != nil || res.Outcome != forge.Proven {
		t.Fatalf("at the reserve: %q, %v; want proven", res.Outcome, err)
	}
	remaining.Store(999)
	if res, err := check(receive(true, &asked), sha2); err != nil || res.Outcome != forge.Proven {
		t.Fatalf("the answer that leaves 999: %q, %v; want proven", res.Outcome, err)
	}
	before := r.gh.total()
	if res, err := check(receive(true, &asked), headSHA); err != nil || res.Outcome != proto.ReasonForgeRate || r.gh.total() != before {
		t.Fatalf("below the reserve: %q, %v, %d requests; want forge-rate and none", res.Outcome, err, r.gh.total()-before)
	}
	if res, err := check(context.Background(), headSHA); err != nil || res.Outcome != forge.Proven {
		t.Fatalf("a send below the reserve: %q, %v; want proven", res.Outcome, err)
	}
	sha3 := strings.Repeat("4", 40)
	r.gh.route(compare(tool, "main", sha3), rated(status("behind")))
	r.clk.StepWall(3600 * 1000)
	if res, err := check(receive(true, &asked), sha3); err != nil || res.Outcome != forge.Proven {
		t.Fatalf("past the window's reset: %q, %v; want proven", res.Outcome, err)
	}
}

// awaitHit waits for the fake GitHub to be asked uri.
func (r *rig) awaitHit(uri string) {
	for got := range r.gh.hit {
		if got == uri {
			return
		}
	}
}

func TestSD5_8ACompareCutIsUnresolvedNotDown(t *testing.T) {
	r := newRig(t)
	r.acme()
	r.gh.route(compare(tool, "main", sha), func(_ http.ResponseWriter, req *http.Request) { <-req.Context().Done() })
	type outcome struct {
		res forge.Result
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := r.c.Check(context.Background(), need(t, commitRef("acme/tool", sha), orgForge(), ""), orgForge())
		done <- outcome{res, err}
	}()
	r.awaitHit(compare(tool, "main", sha))
	r.clk.Advance(15 * time.Second)
	got := <-done
	if got.err != nil || got.res.Outcome != proto.ReasonUnresolved {
		t.Fatalf("a compare cut at 15 s = %q, %v; want unresolved and no error", got.res.Outcome, got.err)
	}
	r.gh.json(compare(tool, "main", sha2), 200, status("behind"))
	r.expect(t, need(t, commitRef("acme/tool", sha2), orgForge(), ""), orgForge(), forge.Proven)
}

func TestSD5_8AServerErrorOffTheRepositoryIsNotDown(t *testing.T) {
	for name, uri := range map[string]string{
		"compare":  compare(tool, "main", sha),
		"contents": contents,
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			r.acme()
			r.gh.json(compare(tool, "main", sha), 200, status("behind"))
			r.gh.json(uri, http.StatusBadGateway, `{}`)
			res, err := r.check(t, need(t, pathRef, orgForge(), ""), orgForge())
			if err != nil || res.Outcome != proto.ReasonUnresolved {
				t.Fatalf("a 502 on the %s = %q, %v; want unresolved and no error", name, res.Outcome, err)
			}
			r.gh.json(compare(tool, "main", sha2), 200, status("behind"))
			r.expect(t, need(t, commitRef("acme/tool", sha2), orgForge(), ""), orgForge(), forge.Proven)
		})
	}
}

func TestSD5_8ANoConnectionOnACompareIsDown(t *testing.T) {
	r := newRig(t)
	r.acme()
	r.gh.json(compare(tool, "main", sha), 200, status("behind"))
	r.expect(t, need(t, commitRef("acme/tool", sha), orgForge(), ""), orgForge(), forge.Proven)
	r.gh.srv.Close()
	res, err := r.check(t, need(t, commitRef("acme/tool", sha2), orgForge(), ""), orgForge())
	if !errors.Is(err, forge.ErrDown) || res.Outcome != proto.ReasonUnresolved {
		t.Fatalf("no connection for a compare = %q, %v; want unresolved, ErrDown", res.Outcome, err)
	}
}
