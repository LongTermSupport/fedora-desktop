package watchlist_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/watchlist"
)

const (
	alpha     = "example-org/alpha"
	beta      = "example-org/beta"
	gamma     = "example-org/gamma"
	mitText   = "MIT licence text"
	oddHeader = "# a header Format would not write\n"
)

type world struct {
	root string
	fake *execx.Fake
	out  bytes.Buffer
	err  bytes.Buffer
}

// newWorld is a checkout holding the given list rows under a header Format would not write, so a
// test can tell whether the list was rewritten.
func newWorld(t *testing.T, rows ...string) *world {
	t.Helper()
	w := &world{root: t.TempDir(), fake: &execx.Fake{}}
	if err := os.MkdirAll(filepath.Join(w.root, "CLAUDE"), 0o750); err != nil {
		t.Fatal(err)
	}
	writeFile(t, w.listPath(), oddHeader+strings.Join(rows, ""))
	// A clone writes a repository with an MIT LICENSE at its destination.
	w.fake.OnFunc([]string{"git", "clone"}, func(c execx.Cmd) (execx.Result, error) {
		dest := c.Args[len(c.Args)-1]
		if err := os.MkdirAll(filepath.Join(dest, ".git"), 0o750); err != nil {
			return execx.Result{}, err
		}
		return execx.Result{Stdout: []byte("Cloning into...\n")}, os.WriteFile(filepath.Join(dest, "LICENSE"), []byte(mitText), 0o600)
	})
	return w
}

func (w *world) listPath() string { return watchlist.ListPath(w.root) }

func (w *world) list(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(w.listPath())
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (w *world) entries(t *testing.T) []watchlist.Entry {
	t.Helper()
	got, err := watchlist.Parse([]byte(w.list(t)), "list")
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func entry(t *testing.T, entries []watchlist.Entry, repo string) watchlist.Entry {
	t.Helper()
	for _, e := range entries {
		if e.Repo == repo {
			return e
		}
	}
	t.Fatalf("%s is not on the list", repo)
	return watchlist.Entry{}
}

func shortSHA(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])[:16]
}

// cloneAt makes a clone of repo exist under the given licence and state, with an MIT LICENSE.
func (w *world) cloneAt(t *testing.T, repo, licence, state string) string {
	t.Helper()
	dir := watchlist.Entry{Repo: repo, Licence: licence, State: state}.CloneDir(w.root)
	writeFile(t, filepath.Join(dir, "LICENSE"), mitText)
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o750); err != nil {
		t.Fatal(err)
	}
	return dir
}

// fetchesOK scripts every fetch, fast-forward and clean status to succeed, and every HEAD to be sha.
func (w *world) fetchesOK(sha string) {
	w.fake.On([]string{"git", "status"}, ok(""))
	w.fake.On([]string{"git", "rev-parse", "--is-shallow-repository"}, ok("false\n"))
	w.fake.On([]string{"git", "fetch"}, ok(""))
	w.fake.On([]string{"git", "merge"}, ok(""))
	w.fake.On([]string{"git", "rev-parse", "--verify", "HEAD"}, ok(sha+"\n"))
}

func (w *world) stars(json string) {
	w.fake.On([]string{"gh", "api", "user/starred", "--paginate"}, ok(json))
}

func (w *world) run(args ...string) term.ExitCode {
	w.out.Reset()
	w.err.Reset()
	return watchlist.NewApp(w.root, w.fake, &w.out, &w.err).Run(context.Background(), args)
}

func (w *world) ran(prefix ...string) bool {
	for _, c := range w.fake.Calls() {
		argv := append([]string{c.Name}, c.Args...)
		if slices.Equal(argv[:min(len(prefix), len(argv))], prefix) {
			return true
		}
	}
	return false
}

func ok(out string) execx.Result { return execx.Result{Stdout: []byte(out)} }

func refuse(msg string) execx.Result { return execx.Result{Stderr: []byte(msg), ExitCode: 128} }

func mitRow(repo, state, group string) string {
	return row(repo, "star", "MIT", "lift", state, group, "LICENSE", shortSHA(mitText))
}

func TestHelpIsStdoutAndExitZero(t *testing.T) {
	w := newWorld(t)
	if code := w.run("--help"); code != term.ExitOK || !strings.Contains(w.out.String(), "run new") {
		t.Fatalf("exit %d, out %q", code, w.out.String())
	}
}

// G3: mark is gone; every usage mistake is exit 64 on stderr.
func TestUsageMistakesAreExit64(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"frobnicate"},
		{"--nope", "sync"},
		{"--root"},
		{"mark", alpha, shaA, "--run", "r"},
		{"add"},
		{"add", "https://github.com/example-org/beta", "extra"},
		{"set"},
		{"set", alpha},
		{"set", alpha, "--state"},
		{"set", alpha, "--colour", "red"},
		{"set", alpha, "--state", "new", "--state", "study"},
		{"set", alpha, "stray"},
		{"run"},
		{"run", "frob"},
		{"run", "new"},
		{"run", "new", "baseline-20261120-1200"},
		{"run", "new", "baseline-20261120-1200", "--mode"},
		{"run", "new", "baseline-20261120-1200", "--mode", "baseline", "--x", "y"},
		{"run", "rows"},
		{"run", "rows", "a", "b", "c"},
		{"run", "mark", "id"},
		{"run", "status", "a", "b"},
	} {
		w := newWorld(t)
		if code := w.run(args...); code != term.ExitUsage {
			t.Errorf("%v: exit %d", args, code)
		}
		if w.out.Len() != 0 || w.err.Len() == 0 {
			t.Errorf("%v: usage must be on stderr only (out %q)", args, w.out.String())
		}
	}
}

func TestRootFlagNamesTheCheckout(t *testing.T) {
	w := newWorld(t, mitRow(alpha, "new", "-"))
	w.cloneAt(t, alpha, "MIT", "new")
	w.fetchesOK(shaA)
	app := watchlist.NewApp(t.TempDir(), w.fake, &w.out, &w.err)
	if code := app.Run(context.Background(), []string{"--root", w.root, "sync", alpha}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
}

func TestAMissingListIsAnError(t *testing.T) {
	w := newWorld(t)
	if err := os.Remove(w.listPath()); err != nil {
		t.Fatal(err)
	}
	if code := w.run("sync"); code != term.ExitFailure || !strings.HasPrefix(w.err.String(), "ERROR:") || len(w.fake.Calls()) != 0 {
		t.Fatalf("exit %d, err %q", code, w.err.String())
	}
}

// S1: a gh failure, or a reply that is not the stars, stops the command before any git or write.
func TestSyncStopsWhenTheStarsCannotBeRead(t *testing.T) {
	for name, res := range map[string]execx.Result{
		"gh refuses":   refuse("HTTP 401: Bad credentials"),
		"not json":     ok("<html>"),
		"no full name": ok(`[{"license":null}]`),
		"bad name":     ok(`[{"full_name":"no-slash","license":null}]`),
		"bad licence":  ok(`[{"full_name":"example-org/x","license":{"spdx_id":"a b"}}]`),
	} {
		w := newWorld(t, mitRow(alpha, "study", "fleets"))
		w.fake.On([]string{"gh"}, res)
		if code := w.run("sync"); code != term.ExitFailure || !strings.HasPrefix(w.err.String(), "ERROR:") {
			t.Errorf("%s: exit %d, err %q", name, code, w.err.String())
		}
		if w.ran("git") || !strings.HasPrefix(w.list(t), oddHeader) {
			t.Errorf("%s: went on after the stars failed", name)
		}
	}
}

// S1, S2, S4, S6, S7, S8: a new star is added as new, licence from GitHub (or NONE), cloned in
// full, its licence file read; the list is written once.
func TestSyncAddsAndClonesNewStars(t *testing.T) {
	w := newWorld(t, mitRow(alpha, "excluded", "-"))
	w.cloneAt(t, alpha, "MIT", "excluded")
	w.fetchesOK(shaA)
	w.stars(`[{"full_name":"example-org/alpha","license":{"spdx_id":"MIT"}}]` + "\n" +
		`[{"full_name":"example-org/gamma","license":null},{"full_name":"example-org/beta","license":{"spdx_id":"Apache-2.0"}}]`)
	if code := w.run("sync"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	want := "new example-org/beta\nnew example-org/gamma\ncloned example-org/beta\ncloned example-org/gamma\n"
	if w.out.String() != want {
		t.Fatalf("out %q\nwant %q", w.out.String(), want)
	}
	got := w.entries(t)
	b := entry(t, got, beta)
	wantB := watchlist.Entry{Repo: beta, Source: "star", Licence: "Apache-2.0", Reuse: "lift", State: "new", Group: "-", LicenceFile: "LICENSE", LicenceSHA: shortSHA(mitText)}
	if b != wantB {
		t.Fatalf("beta %+v", b)
	}
	if g := entry(t, got, gamma); g.Licence != "NONE" || g.Reuse != "clean-room" {
		t.Fatalf("gamma %+v", g)
	}
	wantClone := []string{"git", "clone", "--quiet", "https://github.com/example-org/beta", filepath.Join(w.root, "untracked", "starred-repos", "Apache-2.0", "example-org", "beta")}
	if !w.ran(wantClone...) {
		t.Fatalf("calls %v", w.fake.Calls())
	}
	if !strings.HasPrefix(w.list(t), "# The watch list") {
		t.Fatal("the list was not written in Format's form")
	}
	// S5: the excluded clone is fetched where it lives.
	for _, c := range w.fake.Calls() {
		if c.Args[0] == "fetch" && !strings.Contains(c.Dir, "starred-repos-excluded") {
			t.Fatalf("fetched %q", c.Dir)
		}
	}
}

// S3: a star row absent from the stars becomes unstarred and keeps its clone; an added row is not a
// star; an unstarred row starred again is new.
func TestSyncMarksUnstarredAndRestarred(t *testing.T) {
	w := newWorld(t,
		mitRow(alpha, "study", "fleets"),
		row(beta, "added", "MIT", "lift", "study", "fleets", "LICENSE", shortSHA(mitText)),
		mitRow(gamma, "unstarred", "-"),
	)
	alphaDir := w.cloneAt(t, alpha, "MIT", "study")
	w.cloneAt(t, beta, "MIT", "study")
	w.cloneAt(t, gamma, "MIT", "unstarred")
	w.fetchesOK(shaA)
	w.stars(`[{"full_name":"example-org/gamma","license":{"spdx_id":"MIT"}}]`)
	if code := w.run("sync"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	for _, line := range []string{"unstarred example-org/alpha\n", "restarred example-org/gamma\n", "unstudied example-org/beta\n"} {
		if !strings.Contains(w.out.String(), line) {
			t.Fatalf("out %q lacks %q", w.out.String(), line)
		}
	}
	got := w.entries(t)
	if a := entry(t, got, alpha); a.State != "unstarred" || a.Group != "fleets" {
		t.Fatalf("alpha %+v", a)
	}
	if b := entry(t, got, beta); b.State != "study" {
		t.Fatalf("an added row was unstarred: %+v", b)
	}
	if g := entry(t, got, gamma); g.State != "new" {
		t.Fatalf("gamma %+v", g)
	}
	if _, err := os.Stat(alphaDir); err != nil {
		t.Fatalf("the unstarred clone was not kept: %v", err)
	}
}

// S3, with the paths table: an excluded star that is unstarred moves to starred-repos.
func TestSyncMovesAnUnstarredExcludedClone(t *testing.T) {
	w := newWorld(t, mitRow(alpha, "excluded", "-"))
	old := w.cloneAt(t, alpha, "MIT", "excluded")
	w.fetchesOK(shaA)
	w.stars(`[]`)
	if code := w.run("sync"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old clone still there: %v", err)
	}
	if _, err := os.Stat(filepath.Join(w.root, "untracked", "starred-repos", "MIT", alpha, ".git")); err != nil {
		t.Fatal(err)
	}
}

// S8: nothing changed, nothing written.
func TestSyncWritesNothingWhenNothingChanged(t *testing.T) {
	w := newWorld(t, mitRow(alpha, "excluded", "-"))
	w.cloneAt(t, alpha, "MIT", "excluded")
	w.fetchesOK(shaA)
	w.stars(`[{"full_name":"example-org/alpha","license":{"spdx_id":"MIT"}}]`)
	if code := w.run("sync"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if !strings.HasPrefix(w.list(t), oddHeader) || w.out.Len() != 0 {
		t.Fatalf("list rewritten or events printed: %q", w.out.String())
	}
	// Progress is on stderr.
	if !strings.Contains(w.err.String(), alpha) {
		t.Fatalf("no progress on stderr: %q", w.err.String())
	}
}

// S5: fetched, then fast-forwarded to origin/HEAD, in the clone.
func TestSyncFetchesAndFastForwardsEachClone(t *testing.T) {
	w := newWorld(t, mitRow(alpha, "new", "-"))
	dir := w.cloneAt(t, alpha, "MIT", "new")
	w.fetchesOK(shaA)
	if code := w.run("sync", alpha); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	var seen []string
	for _, c := range w.fake.Calls() {
		if c.Dir != dir {
			t.Fatalf("%v ran in %q", c.Args, c.Dir)
		}
		seen = append(seen, c.Args[0])
	}
	if !slices.Equal(seen, []string{"status", "rev-parse", "fetch", "merge"}) {
		t.Fatalf("calls %v", seen)
	}
	if w.ran("gh") {
		t.Fatal("a named sync read the stars")
	}
}

// S9: a shallow clone is completed before it is fetched.
func TestSyncUnshallowsAShallowClone(t *testing.T) {
	w := newWorld(t, mitRow(alpha, "new", "-"))
	w.cloneAt(t, alpha, "MIT", "new")
	w.fetchesOK(shaA)
	w.fake.On([]string{"git", "rev-parse", "--is-shallow-repository"}, ok("true\n"))
	if code := w.run("sync", alpha); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if w.out.String() != "unshallowed example-org/alpha\n" || !w.ran("git", "fetch", "--quiet", "--unshallow", "origin") {
		t.Fatalf("out %q, calls %v", w.out.String(), w.fake.Calls())
	}
	w = newWorld(t, mitRow(alpha, "new", "-"))
	w.cloneAt(t, alpha, "MIT", "new")
	w.fetchesOK(shaA)
	w.fake.On([]string{"git", "rev-parse", "--is-shallow-repository"}, ok("true\n"))
	w.fake.On([]string{"git", "fetch", "--quiet", "--unshallow"}, refuse("fatal: unable to access"))
	if code := w.run("sync", alpha); code != term.ExitFailure || !strings.Contains(w.err.String(), "unshallow") || w.ran("git", "merge") {
		t.Fatalf("exit %d, err %q", code, w.err.String())
	}
}

// S5, G1: a clone that cannot fast-forward, or holds local changes, stops the command, naming the
// repository and the remedy; later repositories are not touched and the list is not written.
func TestSyncStopsOnACloneThatCannotFastForward(t *testing.T) {
	stages := map[string]func(w *world){
		"merge refused": func(w *world) {
			w.fetchesOK(shaA)
			w.fake.On([]string{"git", "merge"}, refuse("fatal: Not possible to fast-forward, aborting."))
		},
		"local changes": func(w *world) {
			w.fetchesOK(shaA)
			w.fake.On([]string{"git", "status"}, ok(" M README.md\n"))
		},
	}
	for name, script := range stages {
		w := newWorld(t, mitRow(alpha, "new", "-"), mitRow(beta, "new", "-"))
		w.cloneAt(t, alpha, "MIT", "new")
		w.cloneAt(t, beta, "MIT", "new")
		w.stars(`[{"full_name":"example-org/alpha","license":null},{"full_name":"example-org/gamma","license":null}]`)
		script(w)
		if code := w.run("sync"); code != term.ExitFailure {
			t.Errorf("%s: exit %d", name, code)
		}
		if !strings.Contains(w.err.String(), alpha) || !strings.Contains(w.err.String(), "re-clone") {
			t.Errorf("%s: err %q", name, w.err.String())
		}
		for _, c := range w.fake.Calls() {
			if strings.Contains(c.Dir, "beta") || slices.Contains(c.Args, "clone") {
				t.Errorf("%s: went on to %v", name, c.Args)
			}
		}
		if !strings.HasPrefix(w.list(t), oddHeader) {
			t.Errorf("%s: the list was written", name)
		}
	}
}

// G1: each failing step names the repository and the step.
func TestSyncFailuresNameTheRepositoryAndStep(t *testing.T) {
	for step, script := range map[string]func(w *world){
		"fetch": func(w *world) {
			w.fetchesOK(shaA)
			w.fake.On([]string{"git", "fetch"}, refuse("fatal: unable to access"))
		},
		"status": func(w *world) {
			w.fetchesOK(shaA)
			w.fake.On([]string{"git", "status"}, refuse("fatal: not a git repository"))
		},
		"head": func(w *world) {
			w.fetchesOK(shaA)
			w.fake.On([]string{"git", "rev-parse", "--verify"}, refuse("fatal: bad HEAD"))
		},
		"shallow": func(w *world) {
			w.fetchesOK(shaA)
			w.fake.On([]string{"git", "rev-parse", "--is-shallow-repository"}, refuse("fatal"))
		},
	} {
		w := newWorld(t, mitRow(alpha, "study", "fleets"))
		w.cloneAt(t, alpha, "MIT", "study")
		script(w)
		if code := w.run("sync", alpha); code != term.ExitFailure ||
			!strings.Contains(w.err.String(), alpha) || !strings.Contains(w.err.String(), step) {
			t.Errorf("%s: exit %d, err %q", step, code, w.err.String())
		}
	}
	w := newWorld(t, mitRow(alpha, "new", "-"))
	w.fake.On([]string{"git", "clone"}, refuse("fatal: could not read"))
	if code := w.run("sync", alpha); code != term.ExitFailure || !strings.Contains(w.err.String(), "clone") {
		t.Fatalf("clone: exit %d, err %q", code, w.err.String())
	}
}

// S4: a directory left by a killed clone is named, not fetched.
func TestSyncNamesAnInterruptedCloneDirectory(t *testing.T) {
	w := newWorld(t, mitRow(alpha, "new", "-"))
	dir := watchlist.Entry{Repo: alpha, Licence: "MIT", State: "new"}.CloneDir(w.root)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	w.fetchesOK(shaA)
	if code := w.run("sync", alpha); code != term.ExitFailure || !strings.Contains(w.err.String(), "not a git clone") || w.ran("git", "fetch") {
		t.Fatalf("exit %d, err %q", code, w.err.String())
	}
}

func TestSyncNamingAnUnknownRepoIsAnError(t *testing.T) {
	w := newWorld(t, mitRow(alpha, "new", "-"))
	if code := w.run("sync", "example-org/nope"); code != term.ExitFailure || !strings.Contains(w.err.String(), "example-org/nope") {
		t.Fatalf("exit %d, err %q", code, w.err.String())
	}
}

// S6: a licence file that changed is reported; the row's licence stays until set changes it.
func TestSyncReportsALicenceChange(t *testing.T) {
	w := newWorld(t, row(alpha, "star", "MIT", "lift", "new", "-", "LICENSE", lsha))
	dir := w.cloneAt(t, alpha, "MIT", "new")
	writeFile(t, filepath.Join(dir, "COPYING"), "gpl")
	w.fetchesOK(shaA)
	if code := w.run("sync", alpha); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if w.out.String() != "licence-changed example-org/alpha COPYING\n" {
		t.Fatalf("out %q", w.out.String())
	}
	if a := entry(t, w.entries(t), alpha); a.Licence != "MIT" || a.LicenceFile != "COPYING" || a.LicenceSHA != shortSHA("gpl") {
		t.Fatalf("alpha %+v", a)
	}
}

// S7, R4: a study row reports moved from its newest done study, or unstudied; a row still at its
// studied commit reports nothing.
func TestSyncReportsMovedAndUnstudied(t *testing.T) {
	w := newWorld(t, mitRow(alpha, "study", "fleets"), mitRow(beta, "study", "fleets"), mitRow(gamma, "study", "fleets"))
	for _, r := range []string{alpha, beta, gamma} {
		w.cloneAt(t, r, "MIT", "study")
	}
	w.writeRun(t, "baseline-20261101-0900", mrow(alpha, "fleets", "baseline", "-", shaA, "done"), mrow(beta, "fleets", "baseline", "-", shaC, "done"))
	w.writeRun(t, "diff-20261110-0900", mrow(alpha, "fleets", "diff", shaA, shaC, "pending"), mrow(beta, "fleets", "diff", shaC, shaB, "done"))
	w.fetchesOK(shaB)
	w.stars(`[{"full_name":"example-org/alpha","license":null},{"full_name":"example-org/beta","license":null},{"full_name":"example-org/gamma","license":null}]`)
	if code := w.run("sync"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	want := "moved example-org/alpha " + shaA + ".." + shaB + "\nunstudied example-org/gamma\n"
	if w.out.String() != want {
		t.Fatalf("out %q\nwant %q", w.out.String(), want)
	}
}

// A1
func TestAddRefusesBadURLsAndDuplicatesBeforeAnyCall(t *testing.T) {
	for _, u := range []string{
		"http://github.com/example-org/beta", "git@github.com:example-org/beta.git", "example-org/beta",
		"https://github.com/example-org/beta.git", "https://github.com/example-org/beta/",
		"https://gitlab.com/example-org/beta", "https://github.com/example-org/alpha", "https://github.com/Example-Org/Alpha",
	} {
		w := newWorld(t, mitRow(alpha, "new", "-"))
		if code := w.run("add", u); code != term.ExitFailure || !strings.HasPrefix(w.err.String(), "ERROR:") {
			t.Errorf("%s: exit %d, err %q", u, code, w.err.String())
		}
		if len(w.fake.Calls()) != 0 || !strings.HasPrefix(w.list(t), oddHeader) {
			t.Errorf("%s: went on after the refusal", u)
		}
	}
}

// A2
func TestAddReadsTheLicenceAndClones(t *testing.T) {
	w := newWorld(t, mitRow(alpha, "new", "-"))
	w.fake.On([]string{"gh", "api", "repos/example-org/beta"}, ok(`{"full_name":"example-org/beta","license":{"spdx_id":"MPL-2.0"}}`))
	if code := w.run("add", "https://github.com/example-org/beta"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if w.out.String() != "cloned example-org/beta\nadded example-org/beta\n" {
		t.Fatalf("out %q", w.out.String())
	}
	b := entry(t, w.entries(t), beta)
	want := watchlist.Entry{Repo: beta, Source: "added", Licence: "MPL-2.0", Reuse: "whole-file", State: "new", Group: "-", LicenceFile: "LICENSE", LicenceSHA: shortSHA(mitText)}
	if b != want {
		t.Fatalf("beta %+v", b)
	}
	if !w.ran("git", "clone", "--quiet", "https://github.com/example-org/beta", filepath.Join(w.root, "untracked", "starred-repos", "MPL-2.0", "example-org", "beta")) {
		t.Fatalf("calls %v", w.fake.Calls())
	}
}

// A2, G1: a failed lookup or clone adds nothing; a renamed repository is refused.
func TestAddFailsLoudly(t *testing.T) {
	for name, script := range map[string]func(w *world){
		"gh refuses": func(w *world) { w.fake.On([]string{"gh"}, refuse("HTTP 404: Not Found")) },
		"renamed":    func(w *world) { w.fake.On([]string{"gh"}, ok(`{"full_name":"other/beta","license":null}`)) },
		"clone fails": func(w *world) {
			w.fake.On([]string{"gh"}, ok(`{"full_name":"example-org/beta","license":null}`))
			w.fake.On([]string{"git", "clone"}, refuse("fatal"))
		},
	} {
		w := newWorld(t)
		script(w)
		if code := w.run("add", "https://github.com/example-org/beta"); code != term.ExitFailure || !strings.Contains(w.err.String(), beta) {
			t.Errorf("%s: exit %d, err %q", name, code, w.err.String())
		}
		if !strings.HasPrefix(w.list(t), oddHeader) {
			t.Errorf("%s: the list changed", name)
		}
	}
}

// T1, T2, T3: a new licence re-derives reuse, moves the clone and re-reads its licence file.
func TestSetLicenceMovesTheClone(t *testing.T) {
	w := newWorld(t, row(alpha, "star", "NOASSERTION", "clean-room", "new", "-", "-", "-"))
	old := w.cloneAt(t, alpha, "NOASSERTION", "new")
	if code := w.run("set", alpha, "--licence", "BSD-2-Clause"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	a := entry(t, w.entries(t), alpha)
	if a.Licence != "BSD-2-Clause" || a.Reuse != "lift" || a.LicenceFile != "LICENSE" || a.LicenceSHA != shortSHA(mitText) {
		t.Fatalf("alpha %+v", a)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old clone still there: %v", err)
	}
	if _, err := os.Stat(a.CloneDir(w.root)); err != nil {
		t.Fatal(err)
	}
	if len(w.fake.Calls()) != 0 {
		t.Fatal("set ran a program")
	}
	if !strings.Contains(w.out.String(), alpha) {
		t.Fatalf("out %q", w.out.String())
	}
}

// T1, T2: excluded drops the group and moves the clone to the excluded tree; study takes a group.
func TestSetState(t *testing.T) {
	w := newWorld(t, mitRow(alpha, "new", "-"))
	w.cloneAt(t, alpha, "MIT", "new")
	if code := w.run("set", alpha, "--state", "study", "--group", "fleets"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if a := entry(t, w.entries(t), alpha); a.State != "study" || a.Group != "fleets" {
		t.Fatalf("alpha %+v", a)
	}
	if code := w.run("set", alpha, "--state", "excluded"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	a := entry(t, w.entries(t), alpha)
	if a.State != "excluded" || a.Group != "-" || !strings.Contains(a.CloneDir(w.root), "starred-repos-excluded") {
		t.Fatalf("alpha %+v", a)
	}
	if _, err := os.Stat(filepath.Join(a.CloneDir(w.root), ".git")); err != nil {
		t.Fatal(err)
	}
	// The group set earlier is gone, so study needs one again; one already set is enough.
	if code := w.run("set", alpha, "--state", "study"); code != term.ExitFailure || !strings.Contains(w.err.String(), "group") {
		t.Fatalf("exit %d, err %q", code, w.err.String())
	}
	if code := w.run("set", alpha, "--state", "unstarred", "--group", "tooling"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if code := w.run("set", alpha, "--state", "study"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
}

// T1, T2: refusals leave the list and the clone alone.
func TestSetRefusals(t *testing.T) {
	for name, args := range map[string][]string{
		"unknown repo":        {"set", "example-org/nope", "--state", "new"},
		"bad state":           {"set", alpha, "--state", "done"},
		"bad licence":         {"set", alpha, "--licence", "a/b"},
		"bad group":           {"set", alpha, "--group", "Fleets"},
		"excluded with group": {"set", alpha, "--state", "excluded", "--group", "fleets"},
		"study without group": {"set", alpha, "--state", "study"},
		"target exists":       {"set", alpha, "--licence", "Apache-2.0"},
		"no clone":            {"set", beta, "--licence", "Apache-2.0"},
	} {
		w := newWorld(t, mitRow(alpha, "new", "-"), mitRow(beta, "new", "-"))
		dir := w.cloneAt(t, alpha, "MIT", "new")
		w.cloneAt(t, alpha, "Apache-2.0", "new")
		if code := w.run(args...); code != term.ExitFailure || !strings.HasPrefix(w.err.String(), "ERROR:") {
			t.Errorf("%s: exit %d, err %q", name, code, w.err.String())
		}
		if !strings.HasPrefix(w.list(t), oddHeader) {
			t.Errorf("%s: the list changed", name)
		}
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("%s: the clone moved", name)
		}
	}
}
