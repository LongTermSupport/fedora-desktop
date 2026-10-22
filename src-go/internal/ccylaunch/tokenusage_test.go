package ccylaunch

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// The pinned clock's reset times: an hour and three days on.
const (
	reset5h = "1894712400"
	reset7d = "1895000000"
)

// usageHeaders are the response headers of an API that reports both buckets.
const usageHeaders = "HTTP/2 200\r\ncontent-type: application/json\r\n" +
	"anthropic-ratelimit-unified-5h-utilization: 0.25\r\n" +
	"anthropic-ratelimit-unified-7d-utilization: 0.6\r\n" +
	"anthropic-ratelimit-unified-5h-reset: " + reset5h + "\r\n" +
	"anthropic-ratelimit-unified-7d-reset: " + reset7d + "\r\n" +
	"anthropic-ratelimit-unified-representative-claim: five_hour\r\n\r\n"

// usageRun is a --token-usage run over a pool of files, its own TMPDIR, and curl answering
// code with headers.
func usageRun(t *testing.T, files []string, code, headers string, args ...string) *run {
	t.Helper()
	dir := install(t)
	r := tokenRun(t, dir, files, append([]string{"--token-usage"}, args...)...)
	r.env("TMPDIR=" + t.TempDir())
	r.front.Clock = usageClock{now: pinnedNow}
	r.curl(code, headers, 0)
	return r
}

// usageClock is the pinned now, with a file this run wrote stamped now, as the harness's
// pinned clock stamps it; a time a test gave a file (in the pinned clock's years) as it is.
type usageClock struct{ now time.Time }

func (c usageClock) Now() time.Time { return c.now }

func (c usageClock) ModTime(info os.FileInfo) time.Time {
	if info.ModTime().Year() >= c.now.Year()-1 {
		return info.ModTime()
	}
	return c.now
}

// setenv replaces a variable the run was given.
func (r *run) setenv(kv string) {
	name := kv[:strings.IndexByte(kv, '=')+1]
	r.front.Environ = slices.DeleteFunc(r.front.Environ, func(e string) bool { return strings.HasPrefix(e, name) })
	r.env(kv)
}

// curl makes the fake curl write headers to its --dump-header file and print code.
func (r *run) curl(code, headers string, exit int) {
	r.runner.OnFunc([]string{"curl"}, func(c execx.Cmd) (execx.Result, error) {
		at := slices.Index(c.Args, "--dump-header")
		if at < 0 || at+1 >= len(c.Args) {
			return execx.Result{ExitCode: 2}, nil
		}
		if err := os.WriteFile(c.Args[at+1], []byte(headers), 0o600); err != nil {
			return execx.Result{}, err
		}
		return execx.Result{Stdout: []byte(code), ExitCode: exit}, nil
	})
}

func (r *run) cache() string { return filepath.Join(r.home, ".claude-tokens", "ccy", "usage-cache") }

// result writes the cache entry of the name work, stamped at.
func (r *run) result(t *testing.T, content string, at time.Time) {
	t.Helper()
	if err := os.MkdirAll(r.cache(), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(r.cache(), "work.result")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

func (r *run) answered(t *testing.T, want term.ExitCode) {
	t.Helper()
	if code := r.start(t); code != want {
		t.Fatalf("exit %d, want %d; stdout %q stderr %q", code, want, r.stdout, r.stderr)
	}
	if len(r.execer.Execs()) != 0 {
		t.Fatalf("the front handed over (stderr %q)", r.stderr)
	}
}

func (r *run) curls() []execx.Cmd {
	var calls []execx.Cmd
	for _, c := range r.runner.Calls() {
		if c.Name == "curl" {
			calls = append(calls, c)
		}
	}
	return calls
}

const tableHead = "NAME  EXPIRES     5-HOUR  RESETS                                7-DAY  RESETS                            STATUS\n"

// F11: the table, one API call with the token on stdin and never in argv or the environment,
// and the cache entry it leaves.
func TestTokenUsageTable(t *testing.T) {
	r := usageRun(t, []string{"work.2030-06-01.token"}, "200", usageHeaders)
	r.answered(t, term.ExitOK)
	want := tableHead +
		"work  2030-06-01     25%  in 60 minutes (2030-01-15 13:00 UTC)    60%  in 3 days (2030-01-18 20:53 UTC)  ok; binding: 5-hour limit\n"
	if r.stdout.String() != want {
		t.Fatalf("stdout:\n%s\nwant:\n%s", r.stdout, want)
	}
	if want := "Reading usage for 1 account(s): 1 small API call each, unless read in the last 900 seconds...\n"; r.stderr.String() != want {
		t.Fatalf("stderr %q, want %q", r.stderr, want)
	}
	calls := r.curls()
	if len(calls) != 1 {
		t.Fatalf("%d curl calls, want 1", len(calls))
	}
	r.checkCurl(t, calls[0])
	r.checkEntry(t)
}

// checkCurl holds the one curl call to the bash's argv, the token fed on stdin, and the
// temporaries removed.
func (r *run) checkCurl(t *testing.T, c execx.Cmd) {
	t.Helper()
	tmp := r.front.getenv("TMPDIR")
	args := slices.Clone(c.Args)
	for i, a := range args {
		if digits, ok := strings.CutPrefix(a, tmp+"/tmp."); ok && digits != "" && strings.Trim(digits, "0123456789") == "" {
			args[i] = "TMP"
		}
	}
	wantArgs := []string{
		"--config", "-", "--silent", "--request", "POST", "--header", "content-type: application/json",
		"--header", "anthropic-version: 2023-06-01", "--header", "anthropic-beta: oauth-2025-04-20",
		"--data", `{"model":"claude-haiku-4-5-20251001","max_tokens":1,"messages":[{"role":"user","content":"."}]}`,
		"--dump-header", "TMP", "--connect-timeout", "5", "--max-time", "20", "--output", "TMP",
		"--write-out", "%{http_code}", "https://api.anthropic.com/v1/messages",
	}
	if !slices.Equal(args, wantArgs) {
		t.Fatalf("curl argv %q\nwant %q", c.Args, wantArgs)
	}
	if c.Feed == nil || !c.Feed.Secret.Is("secret-work.2030-06-01.token") ||
		c.Feed.Before != `header = "Authorization: Bearer ` || c.Feed.After != "\"\n" {
		t.Fatalf("the token was not fed on stdin as curl's config: %+v", c.Feed)
	}
	if c.Secrets != nil || c.Env != nil || c.Stdin != nil {
		t.Fatalf("the token went another way: %+v", c)
	}
	entries, err := os.ReadDir(tmp)
	if err != nil || len(entries) != 0 {
		t.Fatalf("the temporaries were left in TMPDIR: %v %v", entries, err)
	}
}

// checkEntry holds the cache directory and the entry the table's fetch wrote.
func (r *run) checkEntry(t *testing.T) {
	t.Helper()
	info, err := os.Stat(r.cache())
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("the cache directory: %v %v", info, err)
	}
	path := filepath.Join(r.cache(), "work.result")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "work.2030-06-01.token\n200\n0.25\t" + reset5h + "\t0.6\t" + reset7d + "\tfive_hour\n"; string(raw) != want {
		t.Fatalf("the cache entry %q, want %q", raw, want)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the cache entry's mode: %v %v", info, err)
	}
}

// F11: JSON, two names each fetched once, a later file of a name shadowed and not fetched, an
// expired one not fetched.
func TestTokenUsageJSON(t *testing.T) {
	r := usageRun(t, []string{"work.2030-06-01.token", "work.2030-09-01.token", "old.2030-01-14.token"}, "200", usageHeaders, "--json")
	r.answered(t, term.ExitOK)
	ok := `"five_hour_pct": 25, "seven_day_pct": 60, "resets": {"five_hour": "2030-01-15T13:00:00Z", "seven_day": "2030-01-18T20:53:20Z"}, "binding_limit": "five_hour", "http_status": 200, "fetched_at": "2030-01-15T12:00:00Z"`
	want := "{\n  \"schema\": 1,\n  \"generated_at\": \"2030-01-15T12:00:00Z\",\n  \"scale\": \"fraction\",\n  \"error\": null,\n  \"tokens\": [" +
		"\n    {\"name\": \"old\", \"file\": \"old.2030-01-14.token\", \"expires\": \"2030-01-14\", \"status\": \"unavailable\", \"reason\": \"token expired on 2030-01-14; renew: ccy --update-token=old\", \"five_hour_pct\": null, \"seven_day_pct\": null, \"resets\": {\"five_hour\": null, \"seven_day\": null}, \"binding_limit\": null, \"http_status\": null, \"fetched_at\": null, \"warnings\": [], \"shadowed\": []}," +
		"\n    {\"name\": \"work\", \"file\": \"work.2030-06-01.token\", \"expires\": \"2030-06-01\", \"status\": \"ok\", \"reason\": null, " + ok +
		", \"warnings\": [\"shadows a later file of this name, which ccy never uses: remove the one you do not want\"], \"shadowed\": [\"work.2030-09-01.token\"]}" +
		"\n  ]\n}\n"
	if r.stdout.String() != want {
		t.Fatalf("stdout:\n%s\nwant:\n%s", r.stdout, want)
	}
	if r.stderr.Len() != 0 || len(r.curls()) != 1 {
		t.Fatalf("stderr %q, %d curl calls", r.stderr, len(r.curls()))
	}
}

// F11: the shadow rows of the table, and a shadowed renewal behind an expired file.
func TestTokenUsageTableShadows(t *testing.T) {
	r := usageRun(t, []string{"old.2030-01-15.token", "old.2030-08-01.token"}, "200", usageHeaders)
	r.answered(t, term.ExitFailure)
	blank := "       -  -           -  -       "
	want := "NAME  EXPIRES     5-HOUR  RESETS  7-DAY  RESETS  STATUS\n" +
		"old   2030-01-15" + blank + "unavailable: token expires today, so ccy will not launch with it; renew: ccy --update-token=old; SHADOWED RENEWAL: ccy launches with this unusable file, not the later one; remove old.2030-01-15.token\n" +
		"old   2030-08-01" + blank + "shadowed: ccy --token old launches with old.2030-01-15.token\n"
	if r.stdout.String() != want {
		t.Fatalf("stdout:\n%s\nwant:\n%s", r.stdout, want)
	}
	if want := "ccy --token-usage: usage could not be read for any token\n"; r.stderr.String() != want {
		t.Fatalf("stderr %q", r.stderr)
	}
	if len(r.curls()) != 0 {
		t.Fatal("an unusable token was fetched")
	}
}

// F11: what the API answered, as the row's status and warnings.
func TestTokenUsageStatuses(t *testing.T) {
	cases := []struct {
		name, code, headers, scale string
		exit                       int
		row                        string
	}{
		{"no response", "000", "", "", 7, `"status": "unavailable", "reason": "could not reach the API", "five_hour_pct": null, "seven_day_pct": null, "resets": {"five_hour": null, "seven_day": null}, "binding_limit": null, "http_status": null`},
		{"refused", "401", "HTTP/2 401\r\n", "", 0, `"reason": "this token was not authorised to read it"`},
		{"rate limited without figures", "429", "", "", 0, `"reason": "rate limited", "five_hour_pct": null, "seven_day_pct": null, "resets": {"five_hour": null, "seven_day": null}, "binding_limit": null, "http_status": 429`},
		{"no limits", "200", "", "", 0, `"reason": "the API reported no limits"`},
		{"another status", "503", "", "", 0, `"reason": "HTTP 503"`},
		{
			"one bucket", "429", "Anthropic-Ratelimit-unified-7d-Utilization: 1.01\r\nAnthropic-Ratelimit-Unified-5h-Utilization: 0.5\r\n", "", 0,
			`"status": "ok", "reason": null, "five_hour_pct": null, "seven_day_pct": 101, "resets": {"five_hour": null, "seven_day": null}, "binding_limit": null, "http_status": 429, "fetched_at": "2030-01-15T12:00:00Z", "warnings": ["5-hour: the API did not report it"]`,
		},
		{
			"not numbers", "200", "anthropic-ratelimit-unified-5h-utilization: high\nanthropic-ratelimit-unified-7d-utilization: 1e3\n", "", 0,
			`"status": "unavailable", "reason": "the API sent no usable figures", "five_hour_pct": null, "seven_day_pct": null, "resets": {"five_hour": null, "seven_day": null}, "binding_limit": null, "http_status": 200, "fetched_at": "2030-01-15T12:00:00Z", "warnings": ["5-hour: the API sent a value that is not a number (high)", "7-day: the API sent a value that is not a number (1e3)"]`,
		},
		{
			"a scale mismatch", "200", "anthropic-ratelimit-unified-5h-utilization: 34\n", "", 0,
			`"warnings": ["5-hour: SCALE MISMATCH, the API sent 34, impossible under CCY_USAGE_SCALE=fraction", "7-day: the API did not report it"]`,
		},
		{
			"percent", "200", "anthropic-ratelimit-unified-5h-utilization: 34.567\nanthropic-ratelimit-unified-7d-utilization: 250\n", "percent", 0,
			`"five_hour_pct": 34.57, "seven_day_pct": null, "resets": {"five_hour": null, "seven_day": null}, "binding_limit": null, "http_status": 200, "fetched_at": "2030-01-15T12:00:00Z", "warnings": ["7-day: SCALE MISMATCH, the API sent 250, impossible under CCY_USAGE_SCALE=percent"]`,
		},
		{
			"the last line unterminated and a claim with a tab", "200", "anthropic-ratelimit-unified-representative-claim: a\tb\nanthropic-ratelimit-unified-5h-utilization: 0.0001\nanthropic-ratelimit-unified-7d-utilization: 0.5", "", 0,
			`"five_hour_pct": 0.01, "seven_day_pct": null, "resets": {"five_hour": null, "seven_day": null}, "binding_limit": null, "http_status": 200, "fetched_at": "2030-01-15T12:00:00Z", "warnings": ["7-day: the API did not report it"]`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := usageRun(t, []string{"work.2030-06-01.token"}, c.code, c.headers, "--json")
			r.curl(c.code, c.headers, c.exit)
			if c.scale != "" {
				r.env("CCY_USAGE_SCALE=" + c.scale)
			}
			r.start(t)
			if len(r.execer.Execs()) != 0 {
				t.Fatalf("handed over: %q", r.stderr)
			}
			if !strings.Contains(r.stdout.String(), c.row) {
				t.Fatalf("stdout lacks %s:\n%s", c.row, r.stdout)
			}
		})
	}
}

// F11: the table's figures: a value below one percent, a reset already passed, hours.
func TestTokenUsageTableFigures(t *testing.T) {
	headers := "anthropic-ratelimit-unified-5h-utilization: 0.004\nanthropic-ratelimit-unified-5h-reset: 1894708000\n" +
		"anthropic-ratelimit-unified-7d-utilization: 0\nanthropic-ratelimit-unified-7d-reset: 1894716000\n" +
		"anthropic-ratelimit-unified-representative-claim: seven_day_opus\n"
	r := usageRun(t, []string{"work.2030-06-01.token"}, "200", headers)
	r.answered(t, term.ExitOK)
	want := "NAME  EXPIRES     5-HOUR  RESETS" + strings.Repeat(" ", 30) + "  7-DAY  RESETS" + strings.Repeat(" ", 27) + "  STATUS\n" +
		"work  2030-06-01     <1%  resetting now (2030-01-15 11:46 UTC)     0%  in 2 hours (2030-01-15 14:00 UTC)  ok; binding: weekly limit (Opus)\n"
	if r.stdout.String() != want {
		t.Fatalf("stdout:\n%s\nwant:\n%s", r.stdout, want)
	}
}

// F11: a fresh cache entry of this very file is not fetched again; one past the TTL that a
// fetch did not replace says so.
func TestTokenUsageCache(t *testing.T) {
	entry := "work.2030-06-01.token\n200\n0.25\t\t0.6\t\t\n"
	r := usageRun(t, []string{"work.2030-06-01.token"}, "200", usageHeaders, "--json")
	r.result(t, entry, pinnedNow.Add(-899*time.Second))
	r.answered(t, term.ExitOK)
	if len(r.curls()) != 0 || !strings.Contains(r.stdout.String(), `"five_hour_pct": 25, "seven_day_pct": 60`) {
		t.Fatalf("a fresh entry was fetched again, or not read: %d calls\n%s", len(r.curls()), r.stdout)
	}
	r = usageRun(t, []string{"work.2030-06-01.token"}, "200", usageHeaders, "--json")
	r.result(t, entry, pinnedNow.Add(-900*time.Second))
	r.start(t)
	if len(r.curls()) != 1 {
		t.Fatalf("a stale entry was not fetched: %d calls", len(r.curls()))
	}
	r = usageRun(t, []string{"work.2030-06-01.token"}, "200", usageHeaders, "--json")
	r.result(t, "work.2030-01-01.token\n200\n0.25\n", pinnedNow)
	r.runner.OnFunc([]string{"curl"}, func(execx.Cmd) (execx.Result, error) { return execx.Result{ExitCode: 7}, nil })
	r.start(t)
	if len(r.curls()) != 1 {
		t.Fatalf("an entry of another file was taken as fresh: %d calls", len(r.curls()))
	}
}

// F11: a stale entry no fetch replaced is reported as stale.
func TestTokenUsageStaleEntry(t *testing.T) {
	r := usageRun(t, []string{"work.2030-06-01.token"}, "200", usageHeaders, "--json")
	r.token(t, "work.2030-06-01.token")
	if err := os.WriteFile(filepath.Join(r.pool(), "work.2030-06-01.token"), []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.result(t, "work.2030-06-01.token\n200\n0.25\t\t0.6\t\t\n", pinnedNow.Add(-time.Hour))
	r.answered(t, term.ExitFailure)
	if len(r.curls()) != 0 || !strings.Contains(r.stdout.String(), `"reason": "this run's fetch did not complete, and the cached figures are out of date"`) {
		t.Fatalf("%d calls\n%s", len(r.curls()), r.stdout)
	}
}

// F11: the errors before any token is read, and an option other than --json.
func TestTokenUsageErrors(t *testing.T) {
	r := usageRun(t, []string{"work.2030-06-01.token"}, "200", usageHeaders, "--json")
	r.env("CCY_TOKEN_USAGE=0")
	r.answered(t, term.ExitFailure)
	if want := "{\n  \"schema\": 1,\n  \"generated_at\": \"2030-01-15T12:00:00Z\",\n  \"scale\": \"fraction\",\n  \"error\": \"usage reading is turned off (CCY_TOKEN_USAGE=0)\",\n  \"tokens\": []\n}\n"; r.stdout.String() != want {
		t.Fatalf("stdout %q", r.stdout)
	}
	if _, err := os.Stat(r.cache()); !os.IsNotExist(err) {
		t.Fatalf("usage turned off made the cache: %v", err)
	}

	r = usageRun(t, nil, "200", usageHeaders)
	r.answered(t, term.ExitFailure)
	if want := "ccy --token-usage: no token directory at " + r.pool() + " (create a token with: ccy --create-token)\n"; r.stdout.Len() != 0 || r.stderr.String() != want {
		t.Fatalf("stdout %q stderr %q", r.stdout, r.stderr)
	}

	r = usageRun(t, []string{}, "200", usageHeaders)
	r.answered(t, term.ExitFailure)
	if want := "ccy --token-usage: no tokens in " + r.pool() + " (create one with: ccy --create-token)\n"; r.stderr.String() != want {
		t.Fatalf("stderr %q", r.stderr)
	}

	r = usageRun(t, []string{"work.2030-06-01.token"}, "200", usageHeaders)
	look := r.front.LookPath
	r.front.LookPath = func(name string) (string, error) {
		if name == "curl" {
			return "", os.ErrNotExist
		}
		return look(name)
	}
	r.answered(t, term.ExitFailure)
	if want := "ccy --token-usage: needs curl (run play-claude-yolo.yml)\n"; r.stderr.String() != want || r.stdout.Len() != 0 {
		t.Fatalf("stdout %q stderr %q", r.stdout, r.stderr)
	}

	r = usageRun(t, []string{"work.2030-06-01.token"}, "200", usageHeaders, "--json", "--fast", "--slow")
	r.answered(t, term.ExitUsage)
	if want := "ccy --token-usage: unknown option --fast (usage: ccy --token-usage [--json])\n"; r.stderr.String() != want || r.stdout.Len() != 0 {
		t.Fatalf("stdout %q stderr %q", r.stdout, r.stderr)
	}
}

// F11's F4: wherever the bash could read the pool, the settings or the API's answer otherwise,
// the bash answers.
func TestTokenUsageHandsOver(t *testing.T) {
	cases := map[string]func(t *testing.T, r *run){
		"a TTL that is not a number": func(_ *testing.T, r *run) { r.env("CCY_USAGE_TTL=15m") },
		"a junk token a curl argument holds": func(t *testing.T, r *run) {
			t.Helper()
			r.tokenHolding(t, "user")
		},
		"a junk token the temporary names start with": func(t *testing.T, r *run) {
			t.Helper()
			r.tokenHolding(t, "tmp.")
		},
		"an undated token file":       func(t *testing.T, r *run) { t.Helper(); r.token(t, "plain.token") },
		"a name outside ASCII":        func(t *testing.T, r *run) { t.Helper(); r.token(t, "wörk.2030-06-01.token") },
		"a setting outside ASCII":     func(_ *testing.T, r *run) { r.env("CCY_USAGE_MODEL=modèle") },
		"a home outside ASCII":        func(_ *testing.T, r *run) { r.front.Environ[0] += "/\u00e9" },
		"a setting holding the token": func(_ *testing.T, r *run) { r.env("CCY_USAGE_ENDPOINT=https://x/secret-work.2030-06-01.token") },
		"a token holding a NUL": func(t *testing.T, r *run) {
			t.Helper()
			if err := os.WriteFile(filepath.Join(r.pool(), "work.2030-06-01.token"), []byte("a\x00b"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"an unreadable token": func(t *testing.T, r *run) {
			t.Helper()
			if os.Geteuid() == 0 {
				t.Skip("root reads a file of mode 0")
			}
			if err := os.Chmod(filepath.Join(r.pool(), "work.2030-06-01.token"), 0); err != nil {
				t.Fatal(err)
			}
		},
		"a cache that is not a directory": func(t *testing.T, r *run) {
			t.Helper()
			if err := os.WriteFile(r.cache(), nil, 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"an entry that is not a file": func(t *testing.T, r *run) {
			t.Helper()
			if err := os.MkdirAll(filepath.Join(r.cache(), "work.result"), 0o700); err != nil {
				t.Fatal(err)
			}
		},
		"an entry outside ASCII": func(t *testing.T, r *run) {
			t.Helper()
			r.result(t, "work.2030-06-01.token\n200\n0.25\t\t0.6\t\tfünf\n", pinnedNow)
		},
		"another version": func(_ *testing.T, r *run) { r.front.Version = "0.0.1" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			r := usageRun(t, []string{"work.2030-06-01.token"}, "200", usageHeaders, "--json")
			change(t, r)
			r.start(t)
			if len(r.execer.Execs()) != 1 {
				t.Fatalf("answered: stdout %q stderr %q", r.stdout, r.stderr)
			}
			if r.stdout.Len() != 0 {
				t.Fatalf("printed %q before handing over", r.stdout)
			}
		})
	}
}

// tokenHolding makes the work token file hold token.
func (r *run) tokenHolding(t *testing.T, token string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(r.pool(), "work.2030-06-01.token"), []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// D1, D4, D8, D9: what only the bash's own reading made special is answered by the front.
func TestTokenUsageAnswersWhatTheBashOnlyReadOtherwise(t *testing.T) {
	cases := map[string]struct {
		change func(t *testing.T, r *run)
		want   string
	}{
		"a TTL with a leading nought": {func(_ *testing.T, r *run) { r.env("CCY_USAGE_TTL=0900") }, `"five_hour_pct": 25`},
		"a TTL of nought":             {func(_ *testing.T, r *run) { r.env("CCY_USAGE_TTL=0") }, `"five_hour_pct": 25`},
		"a reset with a leading nought": {func(_ *testing.T, r *run) {
			r.curl("200", "anthropic-ratelimit-unified-5h-utilization: 0.25\nanthropic-ratelimit-unified-5h-reset: 0"+reset5h+"\n", 0)
		}, `"five_hour": "2030-01-15T13:00:00Z"`},
		"an answer outside ASCII": {func(_ *testing.T, r *run) {
			r.curl("200", "anthropic-ratelimit-unified-5h-utilization: 0.25\nanthropic-ratelimit-unified-representative-claim: fünf\n", 0)
		}, `"binding_limit": null`},
		"a typical desktop": {func(t *testing.T, r *run) {
			t.Helper()
			r.setenv("LANG=en_GB.UTF-8")
			r.setenv("TZ=Europe/London")
			r.env("LC_ALL=en_GB.UTF-8")
			r.token(t, "Work-2.2030-07-01.token")
		}, `"name": "Work-2"`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := usageRun(t, []string{"work.2030-06-01.token"}, "200", usageHeaders, "--json")
			c.change(t, r)
			r.answered(t, term.ExitOK)
			if !strings.Contains(r.stdout.String(), c.want) {
				t.Fatalf("stdout lacks %q:\n%s", c.want, r.stdout)
			}
		})
	}
}

// D6: a TMPDIR that cannot hold the temporary files fails the run, naming the error.
func TestTokenUsageTMPDIRMissingFails(t *testing.T) {
	r := usageRun(t, []string{"work.2030-06-01.token"}, "200", usageHeaders)
	r.setenv("TMPDIR=/nonexistent/tmp")
	r.answered(t, term.ExitFailure)
	if !strings.Contains(r.stderr.String(), usagePrefix+"open /nonexistent/tmp/tmp.") || r.stdout.Len() != 0 {
		t.Fatalf("stdout %q stderr %q", r.stdout, r.stderr)
	}
}

// tempToken is a junk token that neither a fixed curl argument nor the temporary names' prefix
// holds: only a temporary name the test hands out can hold it.
const tempToken = "zq"

// temps makes the front's temporary files under TMPDIR with the names name gives, in order,
// and returns how many it made.
func (r *run) temps(t *testing.T, name func(i int) string) func() int {
	t.Helper()
	var mu sync.Mutex
	made := 0
	tmp := r.front.getenv("TMPDIR")
	r.front.CreateTemp = func(dir, pattern string) (*os.File, error) {
		mu.Lock()
		defer mu.Unlock()
		if dir != tmp || pattern != tempPrefix {
			t.Errorf("a temporary file in %q named %q*, want %q and %q", dir, pattern, tmp, tempPrefix)
		}
		path := filepath.Join(dir, name(made))
		made++
		return os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	}
	return func() int {
		mu.Lock()
		defer mu.Unlock()
		return made
	}
}

// Finding 2 of the review: a temporary name that happens to hold the token is made again, so a
// short token is still fetched rather than failing the run.
func TestTokenUsageRetriesATemporaryNameHoldingTheToken(t *testing.T) {
	r := usageRun(t, []string{"work.2030-06-01.token"}, "200", usageHeaders, "--json")
	r.tokenHolding(t, tempToken)
	made := r.temps(t, func(i int) string {
		if i == 0 {
			return "tmp." + tempToken
		}
		return fmt.Sprintf("tmp.clean%d", i)
	})
	r.answered(t, term.ExitOK)
	if made() != 4 {
		t.Fatalf("%d temporary files made, want 4: one refused try, then one that ran", made())
	}
	if len(r.curls()) != 1 || !strings.Contains(r.stdout.String(), `"five_hour_pct": 25`) {
		t.Fatalf("%d calls\n%s", len(r.curls()), r.stdout)
	}
	if args := strings.Join(r.curls()[0].Args, " "); strings.Contains(args, tempToken) {
		t.Fatalf("curl's argv holds the token: %q", args)
	}
	r.noTemporaries(t)
}

// After maxTempTries names that each hold the token, the run fails, naming the file only.
func TestTokenUsageFailsWhenEveryTemporaryNameHoldsTheToken(t *testing.T) {
	r := usageRun(t, []string{"work.2030-06-01.token"}, "200", usageHeaders, "--json")
	r.tokenHolding(t, tempToken)
	made := r.temps(t, func(i int) string { return fmt.Sprintf("tmp.%s%d", tempToken, i) })
	r.answered(t, term.ExitFailure)
	if made() != 2*maxTempTries {
		t.Fatalf("%d temporary files made, want %d", made(), 2*maxTempTries)
	}
	if len(r.curls()) != 0 || r.stdout.Len() != 0 {
		t.Fatalf("%d calls, stdout %q", len(r.curls()), r.stdout)
	}
	if want := usagePrefix + "every temporary name held the token of work.2030-06-01.token\n"; r.stderr.String() != want {
		t.Fatalf("stderr %q, want %q", r.stderr, want)
	}
	r.noTemporaries(t)
}

func (r *run) noTemporaries(t *testing.T) {
	t.Helper()
	entries, err := os.ReadDir(r.front.getenv("TMPDIR"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("the temporaries were left in TMPDIR: %v %v", entries, err)
	}
}

// Finding 4 of the review: the names' prefix is held as os.CreateTemp builds it, uncleaned, so
// a junk token an unclean TMPDIR's names hold hands over rather than failing every try.
func TestTokenUsageHandsOverATokenAnUncleanTMPDIRHolds(t *testing.T) {
	r := usageRun(t, []string{"work.2030-06-01.token"}, "200", usageHeaders, "--json")
	r.setenv("TMPDIR=" + r.front.getenv("TMPDIR") + "/./")
	r.tokenHolding(t, "/./")
	r.start(t)
	if len(r.execer.Execs()) != 1 || r.stdout.Len() != 0 || r.stderr.Len() != 0 {
		t.Fatalf("answered: stdout %q stderr %q", r.stdout, r.stderr)
	}
}

func TestTempPrefixIsAsCreateTempBuildsIt(t *testing.T) {
	for dir, want := range map[string]string{"/tmp": "/tmp/tmp.", "/tmp/": "/tmp/tmp.", "/a//b": "/a//b/tmp.", "./x": "./x/tmp."} {
		if got := tempStart(dir); got != want {
			t.Errorf("tempStart(%q) = %q, want %q", dir, got, want)
		}
	}
}

func TestUsageHumanReset(t *testing.T) {
	for delta, want := range map[int64]string{
		-5: "resetting now", 0: "resetting now", 29: "in 0 minutes", 30: "in 1 minute", 3600: "in 60 minutes",
		5399: "in 90 minutes", 5400: "in 2 hours", 172799: "in 48 hours", 172800: "in 2 days", 259199: "in 3 days",
		864000: "in 10 days",
	} {
		if got := usageHumanReset(1000000+delta, 1000000); got != want {
			t.Errorf("a reset %ds on reads %q, want %q", delta, got, want)
		}
	}
}

func TestUsagePctLabel(t *testing.T) {
	for v, want := range map[string]string{"0": "  0%", "0.4": " <1%", "0.5": " <1%", "0.51": "  1%", "2.5": "  2%", "25": " 25%", "101": "101%", "100.5": "100%"} {
		if got := usagePctLabel(v); got != want {
			t.Errorf("usagePctLabel(%q) = %q, want %q", v, got, want)
		}
	}
}

func TestUsageJSONString(t *testing.T) {
	if got, want := usageJSONString("a\\b\"c\nd\re\tf\x01g\x7f"), `"a\\b\"c\nd\re\tf\u0001g\u007f"`; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}
