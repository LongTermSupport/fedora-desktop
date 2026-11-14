package sessions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ccylaunch"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// The production reader finds a launch's token file as the launcher does (claude-yolo, "Token
// name provided"): a value with a slash is the file itself; a name is the first of
// <dir>/<name>.*.token in byte order, a regular file. A name matches literally, never as a
// pattern.
func TestLaunchFileIsTheLaunchersChoice(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude-tokens", "ccy", "tokens")
	if err := os.MkdirAll(filepath.Join(dir, "gone.2030-01-01.token"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"work.2031-05-01.token", "work.2030-06-01.token", "workday.2029-01-01.token", "w*.2029-01-01.token"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	l := launchTokens{front: ccylaunch.Front{Environ: []string{"HOME=" + home}}}
	for token, want := range map[string]string{
		"work":    filepath.Join(dir, "work.2030-06-01.token"),
		"workday": filepath.Join(dir, "workday.2029-01-01.token"),
		"w*":      filepath.Join(dir, "w*.2029-01-01.token"),
		"wor":     "",
		"gone":    "",
		"nobody":  "",
		filepath.Join(dir, "work.2031-05-01.token"):   filepath.Join(dir, "work.2031-05-01.token"),
		filepath.Join(dir, "absent.2031-05-01.token"): "",
	} {
		got, found := l.LaunchFile(token)
		if got != want || found != (want != "") {
			t.Errorf("%s: %q %v, want %q", token, got, found, want)
		}
	}
	if got, found := (launchTokens{front: ccylaunch.Front{Environ: []string{"HOME=" + t.TempDir()}}}).LaunchFile("work"); found {
		t.Errorf("no token directory: %q", got)
	}
}

// The production reader asks the launch's own reader (ccylaunch.Front.TokenUsability), wired
// from this process's environment; a pool it cannot read is usage unknown, never a fault.
func TestUsabilityIsTheLaunchsReader(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	app := &App{Runner: &execx.Fake{}}
	app.wireTokens()
	u := app.Tokens.Usability(t.Context(), filepath.Join(os.Getenv("HOME"), "work.2099-01-01.token"))
	if u.State != ccylaunch.UsageUnknown || !strings.Contains(u.Reason, "no token directory") {
		t.Fatalf("got %+v", u)
	}
}

// The recorded token is the last --token among the launcher's own arguments, its value whatever
// it is; a --token with no value, or one after --, is no token.
func TestRecordedToken(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--token", "work", "--no-ssh"}, "work"},
		{[]string{"--token", "a", "--token", "b"}, "b"},
		{[]string{"--token", "-c"}, "-c"},
		{[]string{"--no-ssh", "--token"}, ""},
		{[]string{"--", "--token", "work"}, ""},
		{nil, ""},
	} {
		got, ok := recordedToken(c.args)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("%q: %q %v, want %q", c.args, got, ok, c.want)
		}
	}
}
