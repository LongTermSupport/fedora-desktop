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
	cw.dir("sessions", 0o700)
	cw.file("sessions/sess-1", "alpha", 0o600)
	cw.dir("pingbus", 0o700)
	cw.dir("pingbus/alpha", 0o700)
	cw.file("pingbus/alpha/member.json", alphaMember, 0o644)
	cw.file("pingbus/alpha/token", "syt_token", 0o600)
	cw.dir("pingbus/alpha/state", 0o700)
	cw.file("pingbus/alpha/state/team.json", alphaRecord("coordinator"), 0o600)
	return cw
}

func (cw *cacheWorld) dir(rel string, mode os.FileMode) {
	cw.t.Helper()
	if err := os.Mkdir(filepath.Join(cw.root, rel), mode); err != nil {
		cw.t.Fatal(err)
	}
}

func (cw *cacheWorld) file(rel, content string, mode os.FileMode) {
	cw.t.Helper()
	if err := os.WriteFile(filepath.Join(cw.root, rel), []byte(content), mode); err != nil {
		cw.t.Fatal(err)
	}
}

// settled is a clock well past every change to the world, so nothing in it is racy.
func settled(after time.Duration) func() time.Time {
	return func() time.Time { return time.Now().Add(racyWindow + after) }
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
	now := settled(0)
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
	if first := cw.render(settled(0)); !strings.Contains(first.out, "alpha (coordinator)") {
		t.Fatalf("first: %+v", first)
	}
	cw.file("pingbus/alpha/state/team.json", alphaRecord("worker"), 0o600)
	if second := cw.render(settled(time.Second)); !strings.Contains(second.out, "alpha (worker)") || second.err != "" {
		t.Errorf("after the role changed: %+v", second)
	}
	if err := os.Chmod(filepath.Join(cw.root, "pingbus/alpha/token"), 0o640); err != nil {
		t.Fatal(err)
	}
	if third := cw.render(settled(2 * time.Second)); !strings.Contains(third.out, "alpha (role?)") {
		t.Errorf("after the token's chmod: %+v", third)
	}
}

// S32: the cache is keyed by what is not on disk: another COLUMNS misses.
func TestCacheIsKeyedByTheEnvironment(t *testing.T) {
	cw := newCacheWorld(t)
	cw.env["PINGBUS_TEAMS"] = "alpha"
	now := settled(0)
	wide := cw.render(now)
	cw.env["COLUMNS"] = "12"
	narrow := cw.render(now)
	if wide.out == narrow.out || strings.Count(narrow.out, "\n") < 2 {
		t.Errorf("COLUMNS change served from the cache: %q then %q", wide.out, narrow.out)
	}
}

// S34: an entry lasts 60 s, and no longer than a fresh down mark it shows lasts.
func TestCacheEntryExpiresAtTheTTLOrTheMarksAgeOut(t *testing.T) {
	cw := newCacheWorld(t)
	base := time.Now().Add(racyWindow)
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

	cw.dir("pingbus/down", 0o700)
	cw.file("pingbus/down/alpha", "", 0o600)
	markAt := base.Add(-downFreshFor + 20*time.Second)
	if err := os.Chtimes(filepath.Join(cw.root, "pingbus/down/alpha"), markAt, markAt); err != nil {
		t.Fatal(err)
	}
	if got := cw.render(at(0)); !strings.Contains(got.out, "(down)") {
		t.Fatalf("fresh mark not shown: %q", got.out)
	}
	poison(t, cw.cacheFile())
	if got := cw.render(at(10 * time.Second)); !strings.Contains(got.out, "POISON") {
		t.Errorf("before the mark ages out: %q", got.out)
	}
	if got := cw.render(at(20 * time.Second)); strings.Contains(got.out, "POISON") || strings.Contains(got.out, "(down)") {
		t.Errorf("after the mark aged out: %q", got.out)
	}
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
	cw.render(time.Now)
	if _, err := os.Stat(cw.cacheFile()); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a racy render was cached: %v", err)
	}
}

// S36: a cache that cannot be read, is not this user's alone, or is not one this writes never
// changes the output; nor does a home the cache cannot be written in.
func TestCacheFailuresNeverChangeTheOutput(t *testing.T) {
	cw := newCacheWorld(t)
	now := settled(0)
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
	now := settled(0)
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
