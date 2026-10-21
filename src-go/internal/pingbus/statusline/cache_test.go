package statusline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// cacheWorld is a session on one team, alpha, with its record, bundle and team record, and no
// inner status line: the segment alone, so a render shows what the cache gave.
type cacheWorld struct {
	t     *testing.T
	root  string
	env   map[string]string
	stdin []byte
}

const alphaMember = `{"protocol": 1, "team": "alpha", "user_id": "@myrepo.1+workstation.podman:alpha.example.org", ` +
	`"server_name": "alpha.example.org", "base_url": "http://127.0.0.1:1", "plain_http_hosts": ["127.0.0.1"], ` +
	`"token_file": "token", "admin": "@admin:alpha.example.org", "room": "!RRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRR"}`

func alphaRecord(role string) string {
	return `{"v":1,"team":"alpha","humans":["@alice:alpha.example.org"],"roles":{"@myrepo.1+workstation.podman:alpha.example.org":"` +
		role + `"},"repos":[{"repo":"example-org/myrepo"}],"forge_api":"https://api.github.com"}`
}

func newCacheWorld(t *testing.T) *cacheWorld {
	t.Helper()
	root := t.TempDir()
	cw := &cacheWorld{t: t, root: root, stdin: []byte(`{"session_id":"sess-1"}`)}
	cw.env = map[string]string{
		innerVar:        "1",
		"HOME":          root + "/home",
		"PINGBUS_HOME":  root + "/pingbus",
		sessionsDirVar:  root + "/sessions",
		"PINGBUS_TEAMS": "alpha",
		"COLUMNS":       "80",
	}
	cw.dir("sessions")
	cw.file("sessions/sess-1", "alpha", 0o600)
	cw.dir("pingbus")
	cw.dir("pingbus/alpha")
	cw.file("pingbus/alpha/member.json", alphaMember, 0o644)
	cw.file("pingbus/alpha/token", "syt_token", 0o600)
	cw.dir("pingbus/alpha/state")
	cw.file("pingbus/alpha/state/team.json", alphaRecord("coordinator"), 0o600)
	return cw
}

func (cw *cacheWorld) dir(rel string) {
	cw.t.Helper()
	if err := os.Mkdir(filepath.Join(cw.root, rel), 0o700); err != nil {
		cw.t.Fatal(err)
	}
}

func (cw *cacheWorld) file(rel, content string, mode os.FileMode) {
	cw.t.Helper()
	if err := os.WriteFile(filepath.Join(cw.root, rel), []byte(content), mode); err != nil {
		cw.t.Fatal(err)
	}
}

// newestChange is the latest ctime of anything in the world: the clock the tests start from, so
// no test reads the wall clock and every one is placed against the file system's own changes.
func (cw *cacheWorld) newestChange() time.Time {
	cw.t.Helper()
	var newest int64
	err := filepath.WalkDir(cw.root, func(path string, _ fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		var st syscall.Stat_t
		if err := syscall.Lstat(path, &st); err != nil {
			return err
		}
		newest = max(newest, st.Ctim.Nano())
		return nil
	})
	if err != nil {
		cw.t.Fatal(err)
	}
	return time.Unix(0, newest)
}

// settled is a clock past every change to the world so far by more than racyWindow, so nothing
// in it is racy.
func (cw *cacheWorld) settled(after time.Duration) func() time.Time {
	at := cw.newestChange().Add(racyWindow + time.Second + after)
	return func() time.Time { return at }
}

// rendered is one render's stdout and stderr.
type rendered struct{ out, err string }

func (cw *cacheWorld) render(now func() time.Time) rendered {
	cw.t.Helper()
	var stderr bytes.Buffer
	out, code := Run(context.Background(), Input{Env: cw.env, Cwd: cw.root, Stdin: cw.stdin, Now: now}, &stderr)
	if code != 0 {
		cw.t.Fatalf("exit %d", code)
	}
	return rendered{string(out), stderr.String()}
}

func (cw *cacheWorld) cacheFile() string { return filepath.Join(cw.root, "pingbus", cacheFile) }

// S32, S35: a settled render is cached at <home>/statusline-cache.json, mode 0600, and a repeat
// render gives the same bytes from it, the stored role line included.
func TestCacheStoresASettledRenderAndServesItAgain(t *testing.T) {
	cw := newCacheWorld(t)
	cw.file("pingbus/alpha/state/team.json", "{", 0o600)
	now := cw.settled(0)
	first := cw.render(now)
	if !strings.Contains(first.out, "alpha (role?)") || !strings.HasPrefix(first.err, "pingbus status-line: the role cannot be read (alpha: ") {
		t.Fatalf("first render: %+v", first)
	}
	info, err := os.Stat(cw.cacheFile())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("cache mode %o", info.Mode().Perm())
	}
	// Proof the repeat is served from the cache: the stored segment is changed by hand.
	poison(t, cw.cacheFile())
	again := cw.render(now)
	if !strings.Contains(again.out, "alpha (POISON role?)") || again.err != first.err {
		t.Errorf("repeat render: %+v", again)
	}
}

// S33: a change to any file the render read misses the cache.
func TestCacheMissesWhenAnInputChanges(t *testing.T) {
	cw := newCacheWorld(t)
	if first := cw.render(cw.settled(0)); !strings.Contains(first.out, "alpha (coordinator)") {
		t.Fatalf("first: %+v", first)
	}
	cw.file("pingbus/alpha/state/team.json", alphaRecord("worker"), 0o600)
	if second := cw.render(cw.settled(time.Second)); !strings.Contains(second.out, "alpha (worker)") || second.err != "" {
		t.Errorf("after the role changed: %+v", second)
	}
	if err := os.Chmod(filepath.Join(cw.root, "pingbus/alpha/token"), 0o640); err != nil {
		t.Fatal(err)
	}
	if third := cw.render(cw.settled(2 * time.Second)); !strings.Contains(third.out, "alpha (role?)") {
		t.Errorf("after the token's chmod: %+v", third)
	}
}

// S32: the cache is keyed by what is not on disk: another COLUMNS, session or working
// directory misses.
func TestCacheIsKeyedByTheEnvironment(t *testing.T) {
	cw := newCacheWorld(t)
	now := cw.settled(0)
	cw.render(now)
	poison(t, cw.cacheFile())
	if got := cw.render(now); !strings.Contains(got.out, "POISON") {
		t.Fatalf("the same key missed: %q", got.out)
	}
	cw.env["COLUMNS"] = "12"
	if got := cw.render(now); strings.Contains(got.out, "POISON") {
		t.Errorf("COLUMNS change served from the cache: %q", got.out)
	}
	cw.file("sessions/sess-2", "alpha", 0o600)
	poison(t, cw.cacheFile())
	cw.stdin = []byte(`{"session_id":"sess-2"}`)
	if got := cw.render(cw.settled(time.Second)); strings.Contains(got.out, "POISON") {
		t.Errorf("another session served from the cache: %q", got.out)
	}
}

// S34: an entry lasts 60 s, and no longer than a fresh down mark it shows lasts.
func TestCacheEntryExpiresAtTheTTLOrTheMarksAgeOut(t *testing.T) {
	cw := newCacheWorld(t)
	base := cw.settled(0)()
	at := func(d time.Duration) func() time.Time { return func() time.Time { return base.Add(d) } }
	cw.render(at(0))
	poison(t, cw.cacheFile())
	if got := cw.render(at(cacheTTL - time.Second)); !strings.Contains(got.out, "POISON") {
		t.Errorf("within the TTL: %q", got.out)
	}
	if got := cw.render(at(cacheTTL)); strings.Contains(got.out, "POISON") {
		t.Errorf("at the TTL: %q", got.out)
	}
	if got := cw.render(at(-time.Second)); strings.Contains(got.out, "POISON") {
		t.Errorf("a clock before the entry was written: %q", got.out)
	}

	cw.dir("pingbus/down")
	cw.file("pingbus/down/alpha", "", 0o600)
	// The mark is re-dated against a clock placed after it was made; the re-dating changes its
	// ctime again, a moment later, which settled's margin covers.
	markBase := cw.settled(0)()
	markAt := markBase.Add(-downFreshFor + 20*time.Second)
	if err := os.Chtimes(filepath.Join(cw.root, "pingbus/down/alpha"), markAt, markAt); err != nil {
		t.Fatal(err)
	}
	atMark := func(d time.Duration) func() time.Time { return func() time.Time { return markBase.Add(d) } }
	if got := cw.render(atMark(0)); !strings.Contains(got.out, "(down)") {
		t.Fatalf("fresh mark not shown: %q", got.out)
	}
	poison(t, cw.cacheFile())
	if got := cw.render(atMark(10 * time.Second)); !strings.Contains(got.out, "POISON") {
		t.Errorf("before the mark ages out: %q", got.out)
	}
	if got := cw.render(atMark(20 * time.Second)); strings.Contains(got.out, "POISON") || strings.Contains(got.out, "(down)") {
		t.Errorf("after the mark aged out: %q", got.out)
	}
}

// cachedThenPoisoned renders the world settled, proves the render was stored and that a repeat is
// served from the cache, and leaves every stored segment replaced by POISON.
func (cw *cacheWorld) cachedThenPoisoned(want string) {
	cw.t.Helper()
	now := cw.settled(0)
	if first := cw.render(now); !strings.Contains(first.out, want) {
		cw.t.Fatalf("first render %q, want %q", first.out, want)
	}
	raw, err := os.ReadFile(cw.cacheFile())
	if err != nil {
		cw.t.Fatalf("the settled render was not cached: %v", err)
	}
	var content cacheContent
	if err = json.Unmarshal(raw, &content); err != nil {
		cw.t.Fatal(err)
	}
	for i := range content.Entries {
		content.Entries[i].Segment = "POISON"
	}
	if raw, err = json.Marshal(content); err != nil {
		cw.t.Fatal(err)
	}
	cw.file("pingbus/"+cacheFile, string(raw), 0o600)
	if again := cw.render(now); !strings.Contains(again.out, "POISON") {
		cw.t.Fatalf("the repeat was not served from the cache: %q", again.out)
	}
}

// renderAfterChange renders settled past the latest change, and fails on a stale segment.
func (cw *cacheWorld) renderAfterChange(what, want string, absent ...string) {
	cw.t.Helper()
	got := cw.render(cw.settled(0))
	if strings.Contains(got.out, "POISON") || !strings.Contains(got.out, want) {
		cw.t.Errorf("after %s: %q, want %q at once", what, got.out, want)
	}
	for _, a := range absent {
		if strings.Contains(got.out, a) {
			cw.t.Errorf("after %s: %q still shows %q", what, got.out, a)
		}
	}
}

func (cw *cacheWorld) betaBundle() {
	cw.t.Helper()
	cw.dir("pingbus/beta")
	cw.file("pingbus/beta/member.json", strings.ReplaceAll(alphaMember, "alpha", "beta"), 0o644)
	cw.file("pingbus/beta/token", "syt_token", 0o600)
	cw.dir("pingbus/beta/state")
	cw.file("pingbus/beta/state/team.json", strings.ReplaceAll(alphaRecord("worker"), "alpha", "beta"), 0o600)
}

func membershipRecord(teams ...string) string {
	seats := make([]string, 0, len(teams))
	for i, team := range teams {
		seats = append(seats, `{"team": "`+team+`", "seat": "`+string(rune('1'+i))+`"}`)
	}
	return `{"v": 1, "session": "ccy-x-1", "kind": "ccy", "checkout": "/srv/x", "generation": 3, ` +
		`"written_ms": 1, "seats": [` + strings.Join(seats, ", ") + `], "farewell": []}`
}

func appliedRecord(teams ...string) string {
	seats := make([]string, 0, len(teams))
	for i, team := range teams {
		seats = append(seats, `{"handle": "myrepo.1+local.podman", "seat": "`+string(rune('1'+i))+
			`", "state": "held", "team": "`+team+`"}`)
	}
	return `{"generation": 3, "refused": [], "v": 1, "seats": [` + strings.Join(seats, ", ") + `]}`
}

// The owner's case (S33): in a membership session, a seat added and then one dropped show at
// once, though the segment before each change was being served from the cache.
func TestCacheShowsAMembershipChangeAtOnce(t *testing.T) {
	cw := newCacheWorld(t)
	cw.betaBundle()
	delete(cw.env, "PINGBUS_TEAMS")
	cw.env[membershipVar] = cw.root + "/membership"
	cw.dir("membership")
	cw.file("membership/membership.json", membershipRecord("alpha"), 0o644)
	cw.file("pingbus/applied.json", appliedRecord("alpha"), 0o600)
	cw.cachedThenPoisoned("alpha (coordinator)")

	cw.file("membership/membership.json", membershipRecord("alpha", "beta"), 0o644)
	cw.file("pingbus/applied.json", appliedRecord("alpha", "beta"), 0o600)
	cw.renderAfterChange("a seat added", "alpha (coordinator), beta (worker)")

	cw.cachedThenPoisoned("beta (worker)")
	cw.file("membership/membership.json", membershipRecord("beta"), 0o644)
	cw.file("pingbus/applied.json", appliedRecord("beta"), 0o600)
	cw.renderAfterChange("a seat dropped", "beta (worker)", "alpha")
}

// S33: a role changed in the team record shows at once over a cached segment.
func TestCacheShowsARoleChangeAtOnce(t *testing.T) {
	cw := newCacheWorld(t)
	cw.cachedThenPoisoned("alpha (coordinator)")
	cw.file("pingbus/alpha/state/team.json", alphaRecord("worker"), 0o600)
	cw.renderAfterChange("the role changed", "alpha (worker)")
}

// S33: a down mark made, then removed, shows and clears at once over a cached segment.
func TestCacheShowsADownMarkAtOnce(t *testing.T) {
	cw := newCacheWorld(t)
	cw.cachedThenPoisoned("alpha (coordinator)")
	cw.dir("pingbus/down")
	cw.file("pingbus/down/alpha", "", 0o600)
	cw.renderAfterChange("a mark made", "alpha (coordinator) (down)")

	cw.cachedThenPoisoned("(down)")
	if err := os.Remove(filepath.Join(cw.root, "pingbus/down/alpha")); err != nil {
		t.Fatal(err)
	}
	cw.renderAfterChange("the mark removed", "alpha (coordinator)", "(down)")
}

func poison(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes.ReplaceAll(raw, []byte("alpha ("), []byte("alpha (POISON ")), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The racy rule: a render that read a file changed a moment ago is not cached.
func TestCacheSkipsARacyRender(t *testing.T) {
	cw := newCacheWorld(t)
	cw.render(cw.newestChange)
	if _, err := os.Stat(cw.cacheFile()); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a racy render was cached: %v", err)
	}
}

// S36: a cache that cannot be read, is not this user's alone, or is not one this writes never
// changes the output; nor does a home the cache cannot be written in.
func TestCacheFailuresNeverChangeTheOutput(t *testing.T) {
	cw := newCacheWorld(t)
	now := cw.settled(0)
	want := cw.render(now)
	for name, content := range map[string]string{
		"not json":     "{",
		"other shape":  `{"v":2,"entries":[]}`,
		"empty object": "{}",
	} {
		cw.file("pingbus/"+cacheFile, content, 0o600)
		if got := cw.render(now); got != want {
			t.Errorf("%s: %+v", name, got)
		}
	}
	cw.render(now)
	poison(t, cw.cacheFile())
	if err := os.Chmod(cw.cacheFile(), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := cw.render(now); got != want {
		t.Errorf("a cache others can read was used: %+v", got)
	}
	if err := os.Remove(cw.cacheFile()); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(cw.cacheFile(), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := cw.render(now); got != want {
		t.Errorf("an unwritable cache: %+v", got)
	}
	cw.env["PINGBUS_HOME"] = "relative"
	if got := cw.render(now); !strings.Contains(got.out, "(role?)") {
		t.Errorf("an unresolvable home: %+v", got)
	}
}

// S32: the cache keeps an entry per key, at most cacheEntries, newest first.
func TestCacheKeepsSeveralSessions(t *testing.T) {
	cw := newCacheWorld(t)
	now := cw.settled(0)
	for i := range cacheEntries + 2 {
		cw.env["COLUMNS"] = "1" + strings.Repeat("0", i%3) + string(rune('0'+i%10)) + string(rune('0'+i/10))
		cw.render(now)
	}
	raw, err := os.ReadFile(cw.cacheFile())
	if err != nil {
		t.Fatal(err)
	}
	var file cacheContent
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Entries) != cacheEntries {
		t.Errorf("%d entries", len(file.Entries))
	}
}
