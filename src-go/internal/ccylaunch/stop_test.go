package ccylaunch

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sessclose"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// stopCloser stands in for sessclose: it records what it was asked to close and answers.
type stopCloser struct {
	asked   []sessclose.Target
	answers map[string]sessclose.Result
}

func (c *stopCloser) Close(_ context.Context, targets []sessclose.Target) []sessclose.Result {
	c.asked = append(c.asked, targets...)
	out := make([]sessclose.Result, len(targets))
	for i, t := range targets {
		out[i] = c.answers[t.Name]
		out[i].Name = t.Name
	}
	return out
}

func (c *stopCloser) Limits() sessclose.Waits { return sessclose.DefaultWaits }

// closeRun is a front given args, whose tmux server runs the named sessions, closing through c.
func closeRun(t *testing.T, c *stopCloser, live []string, args ...string) *run {
	t.Helper()
	r := newRun(install(t), args...)
	r.front.Closer = c
	r.runner.On([]string{"tmux", "-L", "ccy", "list-sessions"}, execx.Result{Stdout: []byte(sessionLines(live))})
	return r
}

func sessionLines(names []string) string {
	var b strings.Builder
	for _, n := range names {
		b.WriteString(n + " 0 /srv/proj\n")
	}
	return b.String()
}

// F23: `ccy --stop SESSION...` closes each session through sessclose, compacting first unless
// --no-compact, says what each close did on stderr and is exit 0 only when every one ended.
func TestStopClosesThroughSessclose(t *testing.T) {
	for _, tc := range []struct {
		args      []string
		noCompact bool
	}{
		{[]string{stopFlag, "ccy-a", "cc-b"}, false},
		{[]string{stopFlag, noCompactFlag, "ccy-a", "cc-b"}, true},
		{[]string{stopFlag, "ccy-a", "cc-b", noCompactFlag}, true},
	} {
		c := &stopCloser{answers: map[string]sessclose.Result{
			"ccy-a": {Outcome: sessclose.Exited, Note: "compacted first"},
			"cc-b":  {Outcome: sessclose.Stopped, Note: "it did not end within 60 s of /exit, so its tmux session was killed"},
		}}
		r := closeRun(t, c, []string{"ccy-a", "cc-b"}, tc.args...)
		if code := r.start(t); code != term.ExitOK {
			t.Fatalf("%q: exit %d\n%s", tc.args, code, r.stderr)
		}
		want := []sessclose.Target{{Name: "ccy-a", NoCompact: tc.noCompact}, {Name: "cc-b", NoCompact: tc.noCompact}}
		if !slices.Equal(c.asked, want) {
			t.Fatalf("%q: asked %+v", tc.args, c.asked)
		}
		mustContain(t, "stderr", r.stderr.String(), "ccy-a: exited cleanly; compacted first",
			"cc-b: stopped: it did not end within 60 s of /exit")
		if len(r.execer.Execs()) != 0 || r.stdout.Len() != 0 {
			t.Fatalf("%q: handed over or printed on stdout %q", tc.args, r.stdout)
		}
	}
}

// F23: a close that failed (its compaction did not complete) is exit 1, with sessclose's words.
func TestStopThatFailsIsExitOne(t *testing.T) {
	c := &stopCloser{answers: map[string]sessclose.Result{
		"ccy-a": {Outcome: sessclose.Failed, Note: "its /compact did not complete within 600 s"},
	}}
	r := closeRun(t, c, []string{"ccy-a"}, stopFlag, "ccy-a")
	if code := r.start(t); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	mustContain(t, "stderr", r.stderr.String(), "ccy-a: failed: its /compact did not complete within 600 s")
}

// F23: no session named, an unknown option, or a session not on ccy's tmux server is refused
// before anything is closed.
func TestStopRefusesBeforeClosingAnything(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code term.ExitCode
		want string
	}{
		{[]string{stopFlag}, term.ExitUsage, "ccy: --stop needs the name of a session"},
		{[]string{stopFlag, "--force", "ccy-a"}, term.ExitUsage, "ccy: --stop does not take '--force'"},
		{[]string{stopFlag, "ccy-a", "ccy-gone"}, term.ExitFailure, "ccy: there is no session ccy-gone on ccy's tmux server; ccy-sessions lists them. Nothing was closed."},
	} {
		c := &stopCloser{}
		r := closeRun(t, c, []string{"ccy-a"}, tc.args...)
		if code := r.start(t); code != tc.code {
			t.Fatalf("%q: exit %d", tc.args, code)
		}
		mustContain(t, "stderr", r.stderr.String(), tc.want)
		if len(c.asked) != 0 {
			t.Fatalf("%q: closed %+v", tc.args, c.asked)
		}
	}
}

// F23: --stop is the front's own flag: only CCY_LEGACY hands it over.
func TestStopUnderLegacyHandsOver(t *testing.T) {
	dir := install(t)
	r := newRun(dir, stopFlag, "ccy-a").env(LegacyEnv + "=1")
	r.start(t)
	r.handedOver(t, dir)
}

// F23: the production front closes with sessclose over its own runner.
func TestStopBuildsASesscloseCloser(t *testing.T) {
	r := newRun(install(t), stopFlag, "ccy-a")
	if _, ok := r.front.closer().(*sessclose.Closer); !ok {
		t.Fatalf("closer %T", r.front.closer())
	}
}
