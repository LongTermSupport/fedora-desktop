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

// CL2: n3f's four verbs are still unbuilt, each reported as exit 1 internal naming slice n3f,
// through the binary's own run function and its verbs.
func TestTheN3fVerbsAreStillUnbuilt(t *testing.T) {
	for _, args := range [][]string{{"ask"}, {"thread", "Q1"}, {"questions"}, {"report"}} {
		var stdout, stderr bytes.Buffer
		code := run(args, strings.NewReader(""), &stdout, &stderr, strings.Repeat("ab", 32))
		want := "teamchat " + args[0] + " is not built in this binary; it lands with slice n3f"
		if code != term.ExitFailure || stdout.Len() != 0 || !strings.Contains(stderr.String(), want) {
			t.Errorf("%q: exit %d, stdout %q, stderr %q", args, code, stdout.String(), stderr.String())
		}
	}
}
