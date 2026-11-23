package main

import (
	"bytes"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestRunWiresTheCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	digest := strings.Repeat("ab", 32)
	if code := run([]string{"--version"}, strings.NewReader(""), &stdout, &stderr, digest); code != term.ExitOK ||
		stdout.String() != digest+"\n" || stderr.Len() != 0 {
		t.Fatalf("--version: %d %q %q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	if code := run(nil, strings.NewReader(""), &stdout, &stderr, digest); code != term.ExitUsage || stdout.Len() != 0 {
		t.Fatalf("no verb: %d %q", code, stdout.String())
	}
}

// The verbs built outside cli are passed in, every one (n3c-D5).
func TestRunPassesTheVerbsBuiltOutsideCLI(t *testing.T) {
	if got, want := slices.Sorted(maps.Keys(verbs)), slices.Sorted(slices.Values(cli.Outside())); !slices.Equal(got, want) {
		t.Fatalf("verbs = %q, want %q", got, want)
	}
}

// noSession is the environment of a process Claude Code runs a hook or the status line in when
// no teamchat session is its parent: no TEAMCHAT_RUNTIME, and no inner status line to run.
func noSession(t *testing.T) {
	t.Helper()
	t.Setenv(paths.EnvRuntime, "")
	t.Setenv(paths.EnvStatusLineInner, "1")
}

// HK1, n3e-12: each of the four hook events, run through the binary's own run function, is built
// and with no TEAMCHAT_RUNTIME answers {} and exits 0, nothing on stderr.
func TestEveryHookEventIsBuiltInTheBinary(t *testing.T) {
	noSession(t)
	for _, event := range cli.HookEvents {
		var stdout, stderr bytes.Buffer
		code := run([]string{"hook", event}, strings.NewReader(`{"session_id":"s1"}`), &stdout, &stderr, "")
		if code != term.ExitOK || stdout.String() != "{}\n" || stderr.Len() != 0 {
			t.Errorf("hook %s: exit %d, stdout %q, stderr %q", event, code, stdout.String(), stderr.String())
		}
	}
}

// SL1, n3e-12: the status line is built in the binary, and with no session and no inner line it
// writes zero bytes (never a lone newline) and exits 0.
func TestTheStatusLineIsBuiltInTheBinary(t *testing.T) {
	noSession(t)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"status-line"}, strings.NewReader(`{"session_id":"s1"}`), &stdout, &stderr, ""); code != term.ExitOK ||
		stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("status-line: exit %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
}

// n3f-b: `report` is built in the binary: with no TEAMCHAT_RUNTIME it reaches the client and is
// 69 no-session, never "is not built in this binary" or "is not wired".
func TestReportIsBuiltInTheBinary(t *testing.T) {
	noSession(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"report"}, strings.NewReader("Headline\nBody\n"), &stdout, &stderr, strings.Repeat("ab", 32))
	if code != term.ExitCode(69) || stdout.Len() != 0 || !strings.Contains(stderr.String(), `"code":"no-session"`) {
		t.Fatalf("report: exit %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
}

// R0 step 5: the questions verbs are built and wired: with no session each is 69 no-session from
// the client, as every link verb is, through the binary's own run function and its verbs.
func TestTheQuestionsVerbsReachTheClient(t *testing.T) {
	noSession(t)
	for _, args := range [][]string{{"ask-human"}, {"thread", "Q1"}, {"close", "Q1"}, {"retitle", "Q1"}, {"bump", "--waiting"}, {"questions"}} {
		var stdout, stderr bytes.Buffer
		code := run(args, strings.NewReader("text"), &stdout, &stderr, strings.Repeat("ab", 32))
		if code != term.ExitCode(69) || stdout.Len() != 0 || !strings.Contains(stderr.String(), `"code":"no-session"`) {
			t.Errorf("%q: exit %d, stdout %q, stderr %q", args, code, stdout.String(), stderr.String())
		}
	}
}
