package loadgen

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/traffic"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

type ran struct {
	code           term.ExitCode
	stdout, stderr string
}

func cli(ctx context.Context, c *stepClock, args ...string) ran {
	var out, errOut bytes.Buffer
	code := Main(ctx, args, Env{Stdout: &out, Stderr: &errOut, Clock: c})
	return ran{code, out.String(), errOut.String()}
}

// setupDir is a team set up through the command on a fresh lab server, in its directory.
func setupDir(t *testing.T, c *stepClock) string {
	t.Helper()
	srv := lab(t, labName, c)
	keyFile := filepath.Join(t.TempDir(), "registration-key")
	if err := os.WriteFile(keyFile, []byte(srv.RegistrationKey()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "team")
	r := cli(t.Context(), c, "setup", "--homeserver", srv.URL(), "--server-name", labName, "--registration-key-file", keyFile, "--agents", "3", "--dir", dir)
	if r.code != term.ExitOK || r.stdout != filepath.Join(dir, TeamFile)+"\n" {
		t.Fatalf("setup: %+v", r)
	}
	if !strings.Contains(r.stderr, "@tester:"+labName) || strings.Contains(r.stderr, mustTeam(t, dir).HumanPassword) {
		t.Fatalf("setup's report names the human and never the password: %q", r.stderr)
	}
	return dir
}

func mustTeam(t *testing.T, dir string) Team {
	t.Helper()
	team, err := ReadTeam(dir)
	if err != nil {
		t.Fatal(err)
	}
	return team
}

func tallyOf(t *testing.T, stdout string) Tally {
	t.Helper()
	var tally Tally
	if err := json.Unmarshal([]byte(stdout), &tally); err != nil {
		t.Fatalf("stdout %q: %v", stdout, err)
	}
	return tally
}

func TestCommandSetsUpThenRunsPatterns(t *testing.T) {
	c := newStepClock()
	dir := setupDir(t, c)
	r := cli(t.Context(), c, "run", "--dir", dir, "--reports", "6", "--pings", "2", "--for", "1h")
	if r.code != term.ExitOK {
		t.Fatalf("run: %+v", r)
	}
	tally := tallyOf(t, r.stdout)
	if tally.Units[PatternReports] != 6 || tally.Units[PatternPings] != 2 || tally.Made[KindAnswer] != 2 {
		t.Fatalf("tally %+v", tally)
	}
}

func TestCommandReplaysTheProbesDocument(t *testing.T) {
	c := newStepClock()
	dir := setupDir(t, c)
	counts := traffic.Counts{
		Window: traffic.Window{SinceMS: hourAt(0), UntilMS: hourAt(1)},
		Rooms:  []traffic.RoomRead{{Room: proto.PurposeMain, Events: 3, Pages: 1}},
		Rows: []traffic.Row{
			msg(proto.PurposeMain, 0, proto.KeyPing, "", 2),
			row(proto.PurposeMain, 0, "m.room.member", "", "", 1),
		},
	}
	doc, err := traffic.JSON("busy", counts)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "traffic.json")
	if err := os.WriteFile(file, doc, 0o600); err != nil {
		t.Fatal(err)
	}
	if r := cli(t.Context(), c, "run", "--dir", dir, "--replay", file); r.code != term.ExitFailure || !strings.Contains(r.stderr, "--skip-unmade") {
		t.Fatalf("a replay with an unmade row: %+v", r)
	}
	r := cli(t.Context(), c, "run", "--dir", dir, "--replay", file, "--skip-unmade", "--speed", "60")
	if r.code != term.ExitOK || !strings.Contains(r.stderr, "left out: main m.room.member") {
		t.Fatalf("replay: %+v", r)
	}
	if tally := tallyOf(t, r.stdout); tally.Made[KindPing] != 2 {
		t.Fatalf("tally %+v", tally)
	}
}

func TestCommandInterruptedStillGivesTheTally(t *testing.T) {
	c := newStepClock()
	dir := setupDir(t, c)
	ctx, cancel := context.WithCancel(t.Context())
	c.onSleep = cancel
	r := cli(ctx, c, "run", "--dir", dir, "--reports", "6", "--for", "1h")
	if r.code != term.ExitCancelled || tallyOf(t, r.stdout).Units[PatternReports] != 1 {
		t.Fatalf("interrupted run: %+v", r)
	}
}

func TestCommandUsage(t *testing.T) {
	c := newStepClock()
	cases := []struct {
		args []string
		code term.ExitCode
		says string
	}{
		{nil, term.ExitUsage, "usage"},
		{[]string{"fly"}, term.ExitUsage, `no verb "fly"`},
		{[]string{"-h"}, term.ExitOK, "usage"},
		{[]string{"setup", "--bogus"}, term.ExitUsage, "bogus"},
		{[]string{"setup", "--homeserver", "http://127.0.0.1:1", "--server-name", labName, "--agents", "2", "--dir", "x"}, term.ExitUsage, "--registration-key-file"},
		{[]string{"setup", "--homeserver", "http://127.0.0.1:1", "--server-name", labName, "--registration-key-file", "k", "--agents", "2"}, term.ExitUsage, "--dir"},
		{[]string{"setup", "extra"}, term.ExitUsage, "extra"},
		{[]string{"run"}, term.ExitUsage, "--dir"},
		{[]string{"run", "--dir", "d", "--pings", "1", "--replay", "f"}, term.ExitUsage, "not both"},
		{[]string{"run", "--dir", "d", "--speed", "2", "--pings", "1"}, term.ExitUsage, "--speed"},
		{[]string{"run", "--dir", "d", "--pings", "x"}, term.ExitUsage, "pings"},
	}
	for _, tc := range cases {
		r := cli(t.Context(), c, tc.args...)
		if r.code != tc.code || !strings.Contains(r.stdout+r.stderr, tc.says) {
			t.Errorf("%v: %+v, want exit %d saying %q", tc.args, r, tc.code, tc.says)
		}
	}
}

func TestCommandFailures(t *testing.T) {
	c := newStepClock()
	missing := filepath.Join(t.TempDir(), "missing")
	cases := [][]string{
		{"setup", "--homeserver", "http://192.0.2.4:8008", "--server-name", labName, "--registration-key-file", missing, "--agents", "2", "--dir", missing},
		{"setup", "--homeserver", "http://127.0.0.1:1", "--server-name", labName, "--registration-key-file", missing, "--agents", "2", "--dir", missing},
		{"run", "--dir", missing, "--pings", "1"},
	}
	for _, args := range cases {
		if r := cli(t.Context(), c, args...); r.code != term.ExitFailure || r.stdout != "" || r.stderr == "" {
			t.Errorf("%v: %+v", args, r)
		}
	}
	dir := setupDir(t, c)
	if r := cli(t.Context(), c, "run", "--dir", dir, "--replay", missing); r.code != term.ExitFailure {
		t.Errorf("a missing replay: %+v", r)
	}
	if r := cli(t.Context(), c, "run", "--dir", dir, "--pings", "1", "--big-bytes", "5000"); r.code != term.ExitFailure || !strings.Contains(r.stderr, "--big-bytes") {
		t.Errorf("a big body past the limit: %+v", r)
	}
}
