package ccylaunch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// limitHeaders are the response headers of an API reporting u5 and u7 (fractions) with the
// pinned clock's reset times: the 5-hour window clears in an hour, the 7-day in 3 days 9 hours.
func limitHeaders(u5, u7 string) string {
	return "HTTP/2 200\r\n" +
		"anthropic-ratelimit-unified-5h-utilization: " + u5 + "\r\n" +
		"anthropic-ratelimit-unified-7d-utilization: " + u7 + "\r\n" +
		"anthropic-ratelimit-unified-5h-reset: " + reset5h + "\r\n" +
		"anthropic-ratelimit-unified-7d-reset: " + reset7d + "\r\n\r\n"
}

// usableRun is a ccy-token-usable run over a pool holding work and play, with its own TMPDIR,
// and curl answering code with headers and exiting exit.
func usableRun(t *testing.T, code, headers string, exit int) *run {
	t.Helper()
	r := tokenRun(t, install(t), []string{"work.2030-06-01.token", "play.2030-12-31.token"})
	r.env("TMPDIR=" + t.TempDir())
	r.front.Clock = usageClock{now: pinnedNow}
	r.curl(code, headers, exit)
	return r
}

func (r *run) work() string { return filepath.Join(r.pool(), "work.2030-06-01.token") }

func (r *run) usable(t *testing.T, want term.ExitCode, stdout, stderr string, curls int) {
	t.Helper()
	if code := r.front.TokenUsable(context.Background()); code != want {
		t.Fatalf("exit %d, want %d; stdout %q stderr %q", code, want, r.stdout, r.stderr)
	}
	if r.stdout.String() != stdout {
		t.Fatalf("stdout %q\nwant   %q", r.stdout, stdout)
	}
	if r.stderr.String() != stderr {
		t.Fatalf("stderr %q\nwant   %q", r.stderr, stderr)
	}
	if got := len(r.curls()); got != curls {
		t.Fatalf("%d curl calls, want %d", got, curls)
	}
}

// U1: usage left in both windows is exit 0 and says nothing; the one token asked of the API
// once, as --token-usage asks it (the token on curl's stdin).
func TestUsableWithUsageLeft(t *testing.T) {
	r := usableRun(t, "200", limitHeaders("0.25", "0.6"), 0)
	r.front.Args = []string{"--", r.work()}
	r.usable(t, term.ExitOK, "", "", 1)
	r.checkCurl(t, r.curls()[0])
}

// U2: the 5-hour window used up is exit 3, named with its clock time and time to go.
func TestUsableFiveHourUsedUp(t *testing.T) {
	r := usableRun(t, "429", limitHeaders("1.0", "0.6"), 0)
	r.front.Args = []string{r.work()}
	r.usable(t, ExitUsedUp, "",
		"✗ No usage left on token work: 5-hour limit used up; clears today 13:00, in 1h. ccy will not start a session on it.\n", 1)
}

// U2: the 7-day window used up, past the limit.
func TestUsableSevenDayUsedUp(t *testing.T) {
	r := usableRun(t, "200", limitHeaders("0.25", "1.02"), 0)
	r.front.Args = []string{"--", r.work()}
	r.usable(t, ExitUsedUp, "",
		"✗ No usage left on token work: 7-day limit used up; clears Fri 20:53, in 3d 8h. ccy will not start a session on it.\n", 1)
}

// U2: both windows used up name both.
func TestUsableBothUsedUp(t *testing.T) {
	r := usableRun(t, "429", limitHeaders("1.01", "1"), 0)
	r.front.Args = []string{"--", r.work()}
	r.usable(t, ExitUsedUp, "",
		"✗ No usage left on token work: 5-hour limit used up; clears today 13:00, in 1h, and 7-day limit used up; clears Fri 20:53, in 3d 8h. ccy will not start a session on it.\n", 1)
}

// U3: usage that cannot be read does not block: exit 0, saying why.
func TestUsableUnreadableFailsOpen(t *testing.T) {
	r := usableRun(t, "000", "", 6)
	r.front.Args = []string{"--", r.work()}
	r.usable(t, term.ExitOK, "",
		"⚠ Could not check the usage left on token work (could not read the API: curl exited 6), so ccy starts on it anyway.\n", 1)
}

// U3: usage reading turned off asks nothing and does not block.
func TestUsableTurnedOff(t *testing.T) {
	r := usableRun(t, "200", limitHeaders("1", "1"), 0)
	r.env("CCY_TOKEN_USAGE=0")
	r.front.Args = []string{"--", r.work()}
	r.usable(t, term.ExitOK, "",
		"⚠ Could not check the usage left on token work (usage reading is turned off (CCY_TOKEN_USAGE=0)), so ccy starts on it anyway.\n", 0)
}

// U3: a refusal that carries no figures says the session may be refused too.
func TestUsableRateLimitedWithoutFigures(t *testing.T) {
	r := usableRun(t, "429", "HTTP/2 429\r\n\r\n", 0)
	r.front.Args = []string{"--", r.work()}
	r.usable(t, term.ExitOK, "",
		"⚠ Could not check the usage left on token work (the API refused the check (429) and sent no figures, so the session may be refused too), so ccy starts on it anyway.\n", 1)
}

// U8: a fault of this machine (a setting, the pool, the cache, a missing curl) is exit 1,
// named: the launch stops rather than starting unchecked.
func TestUsableLocalFaultFails(t *testing.T) {
	r := usableRun(t, "200", limitHeaders("0", "0"), 0)
	r.env("CCY_USAGE_TTL=soon")
	r.front.Args = []string{"--", r.work()}
	r.usable(t, term.ExitFailure, "", "ccy-token-usable: CCY_USAGE_TTL is not a whole number of seconds\n", 0)

	r = usableRun(t, "200", limitHeaders("0", "0"), 0)
	r.front.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	r.front.Args = []string{"--", r.work()}
	r.usable(t, term.ExitFailure, "", "ccy-token-usable: needs curl (run play-claude-yolo.yml)\n", 0)
}

// U3: an odd file elsewhere in the pool (one the reader refuses) does not block this token.
func TestUsableOddSiblingFailsOpen(t *testing.T) {
	r := usableRun(t, "200", limitHeaders("1", "1"), 0)
	if err := os.Mkdir(filepath.Join(r.pool(), "odd.2030-06-01.token"), 0o700); err != nil {
		t.Fatal(err)
	}
	r.front.Args = []string{"--", r.work()}
	r.usable(t, term.ExitOK, "",
		"⚠ Could not check the usage left on token work (a token file is not a regular file: odd.2030-06-01.token), so ccy starts on it anyway.\n", 0)
}

// U3: a relative path to the launch file is the launch file.
func TestUsableRelativePath(t *testing.T) {
	r := usableRun(t, "200", limitHeaders("1", "0.2"), 0)
	t.Chdir(r.pool())
	r.front.Args = []string{"--", "./work.2030-06-01.token"}
	r.usable(t, ExitUsedUp, "",
		"✗ No usage left on token work: 5-hour limit used up; clears today 13:00, in 1h. ccy will not start a session on it.\n", 1)
}

// U3: a file that is not the launch file of a name in the pool is not read.
func TestUsableOutsideThePool(t *testing.T) {
	r := usableRun(t, "200", limitHeaders("1", "1"), 0)
	r.front.Args = []string{"--", "/elsewhere/work.2030-06-01.token"}
	r.usable(t, term.ExitOK, "",
		"⚠ Could not check the usage left on token work (usage is read only for a token name's launch file in "+r.pool()+"), so ccy starts on it anyway.\n", 0)
}

// U4: a fresh cache entry from this very file is the answer, with no call.
func TestUsableReadsAFreshCache(t *testing.T) {
	r := usableRun(t, "200", limitHeaders("0", "0"), 0)
	r.result(t, "work.2030-06-01.token\n200\n0.2\t"+reset5h+"\t1.0\t"+reset7d+"\tseven_day\n", pinnedNow.Add(-time.Minute))
	r.front.Args = []string{"--", r.work()}
	r.usable(t, ExitUsedUp, "",
		"✗ No usage left on token work: 7-day limit used up; clears Fri 20:53, in 3d 8h. ccy will not start a session on it.\n", 0)
}

// U5: --mark reads the cache only, however old, and prints the mark of a used-up token.
func TestUsableMarkFromTheCache(t *testing.T) {
	r := usableRun(t, "200", limitHeaders("0", "0"), 0)
	r.result(t, "work.2030-06-01.token\n429\n1.0\t"+reset5h+"\t0.4\t"+reset7d+"\tfive_hour\n", pinnedNow.Add(-2*time.Hour+time.Minute))
	r.front.Args = []string{"--mark", "--", r.work()}
	r.usable(t, term.ExitOK, "   ✗ no usage left: 5-hour limit used up; clears today 13:00, in 1h\n", "", 0)
}

// U5: a window whose reset has passed is clear again; no entry is no mark.
func TestUsableMarkClearedOrUnknown(t *testing.T) {
	r := usableRun(t, "200", limitHeaders("0", "0"), 0)
	r.result(t, "work.2030-06-01.token\n429\n1.0\t1894700000\t0.4\t"+reset7d+"\n", pinnedNow.Add(-6*time.Hour))
	r.front.Args = []string{"--mark", r.work()}
	r.usable(t, term.ExitOK, "", "", 0)

	r = usableRun(t, "200", limitHeaders("0", "0"), 0)
	r.front.Args = []string{"--mark", "--", r.work()}
	r.usable(t, term.ExitOK, "", "", 0)
}

// U2: a used-up window the API gave no reset time for says so.
func TestUsableNoResetTime(t *testing.T) {
	r := usableRun(t, "429", "HTTP/2 429\r\nanthropic-ratelimit-unified-7d-utilization: 1.0\r\n\r\n", 0)
	r.front.Args = []string{"--", r.work()}
	r.usable(t, ExitUsedUp, "",
		"✗ No usage left on token work: 7-day limit used up; the API gave no time it clears. ccy will not start a session on it.\n", 1)
}

// U9: on a terminal the refusal is a banner, bold bright white on red and filled to the
// line's end, so it is not missed before the chooser; NO_COLOR and a pipe keep the plain line.
func TestUsableUsedUpBannerOnlyOnATerminal(t *testing.T) {
	text := "✗ No usage left on token work: 7-day limit used up; clears Fri 20:53, in 3d 8h. ccy will not start a session on it."
	banner := "\x1b[1;97;41m\x1b[K\x1b[0m\n" +
		"\x1b[1;97;41m " + text + " \x1b[K\x1b[0m\n" +
		"\x1b[1;97;41m\x1b[K\x1b[0m\n"
	for name, c := range map[string]struct {
		terminal bool
		env      string
		want     string
	}{"a terminal": {true, "", banner}, "NO_COLOR": {true, "NO_COLOR=1", text + "\n"}, "a pipe": {false, "", text + "\n"}} {
		t.Run(name, func(t *testing.T) {
			r := usableRun(t, "200", limitHeaders("0.25", "1.02"), 0)
			r.front.IsTerminal = func(any) bool { return c.terminal }
			if c.env != "" {
				r.env(c.env)
			}
			r.front.Args = []string{"--", r.work()}
			r.usable(t, ExitUsedUp, "", c.want, 1)
		})
	}
}

// U9: the chooser's mark is captured into the chooser's row, so it is never a banner.
func TestUsableMarkIsNeverABanner(t *testing.T) {
	r := usableRun(t, "200", limitHeaders("0", "0"), 0)
	r.front.IsTerminal = func(any) bool { return true }
	r.result(t, "work.2030-06-01.token\n429\n1.0\t"+reset5h+"\t0.4\t"+reset7d+"\tfive_hour\n", pinnedNow.Add(-2*time.Hour+time.Minute))
	r.front.Args = []string{"--mark", "--", r.work()}
	r.usable(t, term.ExitOK, "   ✗ no usage left: 5-hour limit used up; clears today 13:00, in 1h\n", "", 0)
}

// U6: anything but [--mark] [--] FILE is exit 64.
func TestUsableUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"--"}, {"--mark"}, {"--bogus", "f"}, {"a", "b"}} {
		r := usableRun(t, "200", "", 0)
		r.front.Args = args
		r.usable(t, term.ExitUsage, "", "usage: ccy-token-usable [--mark] [--] TOKEN_FILE\n", 0)
	}
}

// U7: the clock times and durations a reset is told in.
func TestClearsText(t *testing.T) {
	now := pinnedNow
	for _, c := range []struct {
		at   time.Time
		want string
	}{
		{now.Add(30 * time.Second), "clears today 12:00, in 1m"},
		{now.Add(45 * time.Minute), "clears today 12:45, in 45m"},
		{now.Add(13*time.Hour + 40*time.Minute), "clears tomorrow 01:40, in 13h 40m"},
		{now.Add(5*24*time.Hour + 2*time.Hour), "clears Sun 14:00, in 5d 2h"},
		{now.Add(7 * 24 * time.Hour), "clears Tue 22 Jan 12:00, in 7d"},
	} {
		if got := clearsText(c.at, now); got != c.want {
			t.Fatalf("clearsText(%v) = %q, want %q", c.at, got, c.want)
		}
	}
}

// The function the launch summary can reuse: the state, the name and the used-up windows.
func TestTokenUsabilityAnswer(t *testing.T) {
	r := usableRun(t, "200", limitHeaders("0.3", "1.0"), 0)
	u := r.front.TokenUsability(context.Background(), r.work(), true)
	if u.State != UsageUsedUp || u.Name != "work" || len(u.UsedUp) != 1 || u.UsedUp[0].Label != "7-day" ||
		u.UsedUp[0].Clears.Unix() != 1895000000 {
		t.Fatalf("usability %+v", u)
	}
	r = usableRun(t, "200", limitHeaders("0.3", "0.4"), 0)
	if u := r.front.TokenUsability(context.Background(), r.work(), true); u.State != UsageLeft || u.Reason != "" {
		t.Fatalf("usability %+v", u)
	}
}
