package paths

import (
	"errors"
	"strings"
	"testing"
)

// envOf is a getenv over a map, as a process sees its environment.
func envOf(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

// TestInboxSocketFollowsClaudeCodesOrder holds Claude Code's own order of places for the inbox
// socket: XDG_RUNTIME_DIR, else CLAUDE_CODE_TMPDIR, else os.tmpdir() (TMPDIR, TMP, TEMP, else
// /tmp), resolved; /tmp/cc-socks-<uid> for a path longer than 103 bytes (issue 266).
func TestInboxSocketFollowsClaudeCodesOrder(t *testing.T) {
	long := "/" + strings.Repeat("d", 100)
	for name, c := range map[string]struct {
		vars map[string]string
		want string
	}{
		"a desktop session":                    {map[string]string{"XDG_RUNTIME_DIR": "/run/user/1000", "TMPDIR": "/var/tmp"}, "/run/user/1000/cc-socks/4242.sock"},
		"a ccy session, no XDG, TMPDIR unset":  {map[string]string{}, "/tmp/cc-socks/4242.sock"},
		"a ccy session, no XDG, TMPDIR set":    {map[string]string{"TMPDIR": "/scratch/tmp/"}, "/scratch/tmp/cc-socks/4242.sock"},
		"an empty XDG is unset":                {map[string]string{"XDG_RUNTIME_DIR": "", "TMPDIR": "/scratch"}, "/scratch/cc-socks/4242.sock"},
		"CLAUDE_CODE_TMPDIR before TMPDIR":     {map[string]string{"CLAUDE_CODE_TMPDIR": "/cc/tmp", "TMPDIR": "/scratch"}, "/cc/tmp/cc-socks/4242.sock"},
		"XDG before CLAUDE_CODE_TMPDIR":        {map[string]string{"XDG_RUNTIME_DIR": "/run/user/7", "CLAUDE_CODE_TMPDIR": "/cc/tmp"}, "/run/user/7/cc-socks/4242.sock"},
		"TMP when TMPDIR is unset":             {map[string]string{"TMP": "/t1", "TEMP": "/t2"}, "/t1/cc-socks/4242.sock"},
		"TEMP when TMPDIR and TMP are unset":   {map[string]string{"TEMP": "/t2"}, "/t2/cc-socks/4242.sock"},
		"resolved, as path.resolve does":       {map[string]string{"XDG_RUNTIME_DIR": "/run/user//1000/x/.."}, "/run/user/1000/cc-socks/4242.sock"},
		"the root":                             {map[string]string{"XDG_RUNTIME_DIR": "/"}, "/cc-socks/4242.sock"},
		"too long falls back to cc-socks-uid":  {map[string]string{"XDG_RUNTIME_DIR": long}, "/tmp/cc-socks-1000/4242.sock"},
		"103 bytes exactly is kept":            {map[string]string{"XDG_RUNTIME_DIR": "/" + strings.Repeat("d", 103-len("//cc-socks/4242.sock"))}, "/" + strings.Repeat("d", 103-len("//cc-socks/4242.sock")) + "/cc-socks/4242.sock"},
		"a desktop session with an empty TEMP": {map[string]string{"XDG_RUNTIME_DIR": "/run/user/1000", "TEMP": ""}, "/run/user/1000/cc-socks/4242.sock"},
	} {
		got, err := InboxSocket(InboxEnvOf(envOf(c.vars), 1000), 4242)
		if err != nil || got != c.want {
			t.Errorf("%s: InboxSocket = %q, %v; want %q", name, got, err, c.want)
		}
		if err == nil && !IsInboxSocket(got, 4242) {
			t.Errorf("%s: IsInboxSocket(%q, 4242) = false for the resolver's own answer", name, got)
		}
	}
}

func TestInboxSocketRefusesWhatClaudeCodeCannotBeMatchedOn(t *testing.T) {
	for name, c := range map[string]struct {
		env InboxEnv
		pid int
	}{
		"a relative directory, resolved against a cwd unknown here": {InboxEnv{RuntimeDir: "run/user/1000"}, 1},
		"a pid of zero":                    {InboxEnv{RuntimeDir: "/run/user/1000"}, 0},
		"a negative pid":                   {InboxEnv{RuntimeDir: "/run/user/1000"}, -3},
		"a negative uid, for the fallback": {InboxEnv{RuntimeDir: "/" + strings.Repeat("d", 120), UID: -1}, 1},
	} {
		if got, err := InboxSocket(c.env, c.pid); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: InboxSocket = %q, %v; want ErrInvalid", name, got, err)
		}
	}
}

func TestIsInboxSocket(t *testing.T) {
	for _, good := range []string{"/run/user/1000/cc-socks/4242.sock", "/tmp/cc-socks/4242.sock", "/tmp/cc-socks-1000/4242.sock", "/cc-socks/4242.sock"} {
		if !IsInboxSocket(good, 4242) {
			t.Errorf("IsInboxSocket(%q, 4242) = false, want true", good)
		}
	}
	for _, bad := range []string{
		"", "cc-socks/4242.sock", "/r/cc-socks/4243.sock", "/r/socks/4242.sock", "/r/x/../cc-socks/4242.sock",
		"/r/cc-socks//4242.sock", "/r/cc-socks-/4242.sock", "/r/cc-socks-x/4242.sock", "/r/cc-socks/4242.sockx",
		"/r/cc-socksy/4242.sock", "/r/cc-socks/04242.sock",
	} {
		if IsInboxSocket(bad, 4242) {
			t.Errorf("IsInboxSocket(%q, 4242) = true, want false", bad)
		}
	}
}
