package wsitoken

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// A placeholder of a real token's shape: the prefix and a length in 90-120.
var (
	tokenA = "sk-ant-oat01-" + strings.Repeat("a", 95)
	tokenB = "sk-ant-oat01-" + strings.Repeat("b", 95)
	tokenC = "sk-ant-oat01-" + strings.Repeat("c", 95)
)

const ccyPath = "/opt/stub/claude-yolo"

type kit struct {
	t         *testing.T
	run       *execx.Fake
	home, bin string
	out, errb bytes.Buffer
}

func newKit(t *testing.T) *kit {
	t.Helper()
	root := t.TempDir()
	k := &kit{t: t, run: &execx.Fake{}, home: filepath.Join(root, "home"), bin: filepath.Join(root, "bin")}
	if err := os.MkdirAll(k.tokenDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	return k
}

func (k *kit) tokenDir() string { return filepath.Join(k.home, ".claude-tokens/ccy/tokens") }

func (k *kit) warningPath() string {
	return filepath.Join(k.home, ".local/share/speech-to-text/claude-tokens-exhausted")
}

func (k *kit) token(file, value string) string {
	k.t.Helper()
	path := filepath.Join(k.tokenDir(), file)
	if err := os.WriteFile(path, []byte(value+"\n"), 0o600); err != nil {
		k.t.Fatal(err)
	}
	return path
}

func (k *kit) setting(key, value string) {
	k.run.On([]string{filepath.Join(k.bin, "wsi-setting"), key}, execx.Result{Stdout: []byte(value + "\n")})
}

func (k *kit) auto(on bool) {
	if on {
		k.setting("claude-token-auto", "true")
		return
	}
	k.setting("claude-token-auto", "false")
}

func (k *kit) usage(json string, code int) {
	k.run.On([]string{ccyPath, "--token-usage", "--json"}, execx.Result{Stdout: []byte(json), ExitCode: code})
}

func (k *kit) exec(args ...string) term.ExitCode {
	k.t.Helper()
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	app := App{Env: Env{
		Runner: k.run, Stdout: &k.out, Stderr: &k.errb, Home: k.home, BinDir: k.bin, CCY: ccyPath,
		Now: func() time.Time { return now },
	}}
	return app.Run(context.Background(), args)
}

func (k *kit) want(code, wantCode term.ExitCode, stdout string) {
	k.t.Helper()
	if code != wantCode || k.out.String() != stdout {
		k.t.Fatalf("exit %d stdout %q, want exit %d stdout %q; stderr %q", code, k.out.String(), wantCode, stdout, k.errb.String())
	}
}

func (k *kit) wantStderr(part string) {
	k.t.Helper()
	if !strings.Contains(k.errb.String(), part) {
		k.t.Fatalf("stderr %q lacks %q", k.errb.String(), part)
	}
}

func (k *kit) noTokenValueSeen() {
	k.t.Helper()
	for _, v := range []string{tokenA, tokenB, tokenC, "sk-ant-oat01-"} {
		if strings.Contains(k.out.String()+k.errb.String(), v) {
			k.t.Fatalf("a token value was printed: %q %q", k.out.String(), k.errb.String())
		}
	}
}

func (k *kit) warning() (string, bool) {
	k.t.Helper()
	data, err := os.ReadFile(k.warningPath())
	if os.IsNotExist(err) {
		return "", false
	}
	if err != nil {
		k.t.Fatal(err)
	}
	return string(data), true
}

func (k *kit) plantWarning() {
	k.t.Helper()
	if err := os.MkdirAll(filepath.Dir(k.warningPath()), 0o700); err != nil {
		k.t.Fatal(err)
	}
	if err := os.WriteFile(k.warningPath(), []byte("old\n"), 0o600); err != nil {
		k.t.Fatal(err)
	}
}

func usageJSON(tokens ...string) string {
	return `{"schema": 1, "generated_at": "2026-10-16T12:00:00Z", "scale": "fraction", "error": null, "tokens": [` +
		strings.Join(tokens, ",") + `]}`
}

func okToken(name, file, p5, p7, r5, r7 string) string {
	return `{"name": "` + name + `", "file": "` + file + `", "expires": "2099-01-01", "status": "ok", "reason": null, ` +
		`"five_hour_pct": ` + p5 + `, "seven_day_pct": ` + p7 + `, "resets": {"five_hour": ` + r5 + `, "seven_day": ` + r7 + `}, ` +
		`"binding_limit": null, "http_status": 200, "fetched_at": "2026-10-16T12:00:00Z", "warnings": [], "shadowed": []}`
}

func unavailableToken(name, reason string) string {
	return `{"name": "` + name + `", "file": "` + name + `.2099-01-01.token", "expires": "2099-01-01", "status": "unavailable", ` +
		`"reason": "` + reason + `", "five_hour_pct": null, "seven_day_pct": null, "resets": {"five_hour": null, "seven_day": null}, ` +
		`"binding_limit": null, "http_status": null, "fetched_at": null, "warnings": [], "shadowed": []}`
}

// K1

func TestK1AnUnreadableAutoSettingFailsBeforeAnyTokenIsRead(t *testing.T) {
	k := newKit(t)
	k.run.On([]string{filepath.Join(k.bin, "wsi-setting"), "claude-token-auto"}, execx.Result{ExitCode: 1})
	k.want(k.exec(), term.ExitFailure, "")
	k.wantStderr("wsi-claude-token: cannot read the claude-token-auto setting (re-run play-speech-to-text.yml)")
}

func TestK1AnAutoSettingThatIsNotABooleanFails(t *testing.T) {
	k := newKit(t)
	k.setting("claude-token-auto", "maybe")
	k.want(k.exec(), term.ExitFailure, "")
	k.wantStderr("cannot read the claude-token-auto setting")
}

// K2

func TestK2AnEmptyNamedTokenIsTheDesktopLogin(t *testing.T) {
	k := newKit(t)
	k.auto(false)
	k.setting("claude-token", "")
	k.want(k.exec(), term.ExitOK, "")
	k.wantStderr("using the desktop login (claude-token is empty)")
}

func TestK2AnUnreadableNamedTokenSettingFails(t *testing.T) {
	k := newKit(t)
	k.auto(false)
	k.run.On([]string{filepath.Join(k.bin, "wsi-setting"), "claude-token"}, execx.Result{ExitCode: 1})
	k.want(k.exec(), term.ExitFailure, "")
	k.wantStderr("cannot read the claude-token setting (re-run play-speech-to-text.yml)")
}

// K3

func TestK3TheNewestUnexpiredFileOfTheNamedTokenIsChosen(t *testing.T) {
	k := newKit(t)
	k.auto(false)
	k.setting("claude-token", "work")
	want := k.token("work.2099-01-01.token", tokenA)
	k.token("work.2026-10-16.token", tokenB)
	k.token("work.2000-01-01.token", tokenC)
	k.token("other.2099-06-01.token", tokenC)
	k.want(k.exec(), term.ExitOK, want+"\n")
	k.wantStderr("using token 'work' (work.2099-01-01.token)")
	k.noTokenValueSeen()
}

func TestK3AFileExpiringTodayIsStillChosen(t *testing.T) {
	k := newKit(t)
	k.auto(false)
	k.setting("claude-token", "work")
	want := k.token("work.2026-10-16.token", tokenA)
	k.want(k.exec(), term.ExitOK, want+"\n")
}

func TestK3ANameWithOnlyExpiredFilesFails(t *testing.T) {
	k := newKit(t)
	k.auto(false)
	k.setting("claude-token", "work")
	k.token("work.2000-01-01.token", tokenA)
	k.token("workshop.2099-01-01.token", tokenB)
	k.want(k.exec(), term.ExitFailure, "")
	k.wantStderr("no unexpired token named 'work' in " + k.tokenDir() +
		" (Settings → Claude token; make one with ccy --create-token)")
}

func TestK3ANameThatIsNotATokenNameIsRefused(t *testing.T) {
	k := newKit(t)
	k.auto(false)
	k.setting("claude-token", "../work")
	k.want(k.exec(), term.ExitFailure, "")
	k.wantStderr("the claude-token setting '../work' is not a token name")
}

// K4

func TestK4AFileThatDoesNotHoldATokenIsRefusedWithoutPrintingIt(t *testing.T) {
	k := newKit(t)
	k.auto(false)
	k.setting("claude-token", "work")
	path := k.token("work.2099-01-01.token", "sk-ant-oat01-short")
	k.want(k.exec(), term.ExitFailure, "")
	k.wantStderr("the token file " + path + " is not a Claude Code OAuth token")
	k.noTokenValueSeen()
}

// K5

func TestK5UsageThatCannotBeReadFailsAndLeavesTheWarningAlone(t *testing.T) {
	cases := map[string]func(k *kit){
		"exit 64":   func(k *kit) { k.usage("", 64) },
		"not JSON":  func(k *kit) { k.usage("Reading usage...", 0) },
		"schema 2":  func(k *kit) { k.usage(`{"schema": 2, "tokens": []}`, 0) },
		"no schema": func(k *kit) { k.usage(`{"tokens": []}`, 0) },
		"not started": func(k *kit) {
			k.run.OnFunc([]string{ccyPath}, func(execx.Cmd) (execx.Result, error) { return execx.Result{}, os.ErrNotExist })
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			k := newKit(t)
			k.auto(true)
			k.plantWarning()
			setup(k)
			k.want(k.exec(), term.ExitFailure, "")
			k.wantStderr("cannot read the tokens' usage (" + ccyPath + " --token-usage --json: ")
			k.wantStderr("re-run play-claude-yolo.yml")
			if got, ok := k.warning(); !ok || got != "old\n" {
				t.Fatalf("warning file %q (present %v): a broken reader is not a verdict on the tokens", got, ok)
			}
		})
	}
}

func TestK5CcysOwnFirstLineOfStderrIsInTheReason(t *testing.T) {
	k := newKit(t)
	k.auto(true)
	k.run.On([]string{ccyPath, "--token-usage", "--json"},
		execx.Result{Stderr: []byte("ccy: the token library is missing\nsecond line\n"), ExitCode: 2})
	k.want(k.exec(), term.ExitFailure, "")
	k.wantStderr("(" + ccyPath + " --token-usage --json: exit 2: ccy: the token library is missing; re-run play-claude-yolo.yml)")
	if strings.Contains(k.errb.String(), "second line") {
		t.Fatalf("stderr %q carries more than ccy's first line", k.errb.String())
	}
}

// K6

func TestK6TheTokenWithTheMostRoomIsChosenAndTheWarningCleared(t *testing.T) {
	k := newKit(t)
	k.auto(true)
	k.plantWarning()
	k.token("busy.2099-01-01.token", tokenA)
	want := k.token("idle.2099-02-01.token", tokenB)
	k.token("half.2099-03-01.token", tokenC)
	k.usage(usageJSON(
		okToken("busy", "busy.2099-01-01.token", "10", "90", "null", "null"),
		okToken("idle", "idle.2099-02-01.token", "34", "8", "null", "null"),
		okToken("half", "half.2099-03-01.token", "50", "20", "null", "null"),
	), 0)
	k.want(k.exec(), term.ExitOK, want+"\n")
	k.wantStderr("using token 'idle', the most allowance left (5-hour 34%, 7-day 8%)")
	if _, ok := k.warning(); ok {
		t.Fatal("the warning file stayed after a token with room was found")
	}
	k.noTokenValueSeen()
}

func TestK6ATieGoesToTheLowerWeeklyFigureThenTheName(t *testing.T) {
	k := newKit(t)
	k.auto(true)
	k.token("b.2099-01-01.token", tokenA)
	k.token("c.2099-01-01.token", tokenB)
	want := k.token("a.2099-01-01.token", tokenC)
	k.usage(usageJSON(
		okToken("b", "b.2099-01-01.token", "40", "40", "null", "null"),
		okToken("c", "c.2099-01-01.token", "40", "10", "null", "null"),
		okToken("a", "a.2099-01-01.token", "40", "10", "null", "null"),
	), 0)
	k.want(k.exec(), term.ExitOK, want+"\n")
}

func TestK6ATokenWithOneBucketReportedIsRankedOnThatBucket(t *testing.T) {
	k := newKit(t)
	k.auto(true)
	want := k.token("one.2099-01-01.token", tokenA)
	k.token("two.2099-01-01.token", tokenB)
	k.usage(usageJSON(
		okToken("one", "one.2099-01-01.token", "null", "5", "null", "null"),
		okToken("two", "two.2099-01-01.token", "30", "30", "null", "null"),
	), 0)
	k.want(k.exec(), term.ExitOK, want+"\n")
	k.wantStderr("(5-hour not reported, 7-day 5%)")
}

func TestK6AFileNameThatLeavesTheStoreIsRefused(t *testing.T) {
	k := newKit(t)
	k.auto(true)
	k.usage(usageJSON(okToken("x", "../x.2099-01-01.token", "1", "1", "null", "null")), 0)
	k.want(k.exec(), term.ExitFailure, "")
	k.wantStderr("ccy --token-usage named the file '../x.2099-01-01.token' for token 'x', which is not a token file name")
}

func TestK6TheChosenFileIsCheckedToHoldAToken(t *testing.T) {
	k := newKit(t)
	k.auto(true)
	path := k.token("x.2099-01-01.token", "not a token")
	k.usage(usageJSON(okToken("x", "x.2099-01-01.token", "1", "1", "null", "null")), 0)
	k.want(k.exec(), term.ExitFailure, "")
	k.wantStderr("the token file " + path + " is not a Claude Code OAuth token")
}

// K7

func TestK7NoTokenWithRoomFailsNamingEachAndRaisesTheWarning(t *testing.T) {
	k := newKit(t)
	k.auto(true)
	k.token("week.2099-01-01.token", tokenA)
	k.token("hour.2099-01-01.token", tokenB)
	k.usage(usageJSON(
		okToken("week", "week.2099-01-01.token", "12", "100", `"2026-10-16T14:00:00Z"`, `"2026-10-18T09:00:00Z"`),
		okToken("hour", "hour.2099-01-01.token", "101.5", "40", `"2026-10-16T14:00:00Z"`, `"2026-10-20T09:00:00Z"`),
		unavailableToken("gone", "token expired on 2026-10-01; renew: ccy --update-token=gone"),
	), 0)
	k.want(k.exec(), term.ExitFailure, "")
	lines := []string{
		"week: 5-hour 12%, 7-day 100% (resets Sun 18 Oct 09:00)",
		"hour: 5-hour 101.5% (resets Fri 16 Oct 14:00), 7-day 40%",
		"gone: unavailable (token expired on 2026-10-01; renew: ccy --update-token=gone)",
	}
	k.wantStderr("wsi-claude-token: no Claude token has allowance left: " + strings.Join(lines, "; "))
	got, ok := k.warning()
	if !ok || got != "No Claude token has allowance left\n"+strings.Join(lines, "\n")+"\n" {
		t.Fatalf("warning file %q (present %v)", got, ok)
	}
	info, err := os.Stat(k.warningPath())
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("warning file mode %v (%v), want 0600", info, err)
	}
}

func TestK7EveryTokenUnreadableIsAlsoNoRoom(t *testing.T) {
	k := newKit(t)
	k.auto(true)
	json := `{"schema": 1, "error": "usage could not be read for any token", "tokens": [` +
		unavailableToken("a", "could not reach the API") + `]}`
	k.usage(json, 1)
	k.want(k.exec(), term.ExitFailure, "")
	k.wantStderr("no Claude token has allowance left: a: unavailable (could not reach the API)")
	if _, ok := k.warning(); !ok {
		t.Fatal("no warning file")
	}
}

func TestK7NoTokensAtAllSaysWhyCcyGave(t *testing.T) {
	k := newKit(t)
	k.auto(true)
	k.usage(`{"schema": 1, "error": "no tokens in /x (create one with: ccy --create-token)", "tokens": []}`, 1)
	k.want(k.exec(), term.ExitFailure, "")
	k.wantStderr("no Claude token has allowance left: no tokens in /x (create one with: ccy --create-token)")
}

// K8

func TestK8AnyArgumentIsAUsageError(t *testing.T) {
	k := newKit(t)
	k.want(k.exec("--debug"), term.ExitUsage, "")
	k.wantStderr("wsi-claude-token: unknown argument: --debug")
	k.wantStderr("usage: wsi-claude-token")
	if len(k.run.Calls()) != 0 {
		t.Fatalf("ran %v on a usage error", k.run.Calls())
	}
}

func TestK8HelpIsTheUsageOnStdout(t *testing.T) {
	k := newKit(t)
	if code := k.exec("--help"); code != term.ExitOK || !strings.HasPrefix(k.out.String(), "usage: wsi-claude-token") {
		t.Fatalf("exit %d stdout %q", code, k.out.String())
	}
}
