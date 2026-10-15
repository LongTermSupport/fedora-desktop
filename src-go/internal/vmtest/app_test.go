package vmtest

import (
	"context"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// E1: help on stdout, exit 0; an unknown command is the usage on stderr and exit 64.
func TestE1HelpAndUnknownCommand(t *testing.T) {
	for _, args := range [][]string{nil, {"help"}, {"-h"}, {"--help"}} {
		k := newKit(t)
		k.wantCode(k.run(args...), term.ExitOK)
		if !strings.HasPrefix(k.out.String(), "vmtest — host CLI") || k.err.Len() != 0 {
			t.Fatalf("%v: stdout %q stderr %q", args, k.out.String(), k.err.String())
		}
	}
	k := newKit(t)
	k.wantCode(k.run("frobnicate"), term.ExitUsage)
	if k.out.Len() != 0 || !strings.HasPrefix(k.err.String(), "vmtest — host CLI") ||
		!strings.HasSuffix(k.err.String(), "ERROR: unknown command: frobnicate\n") {
		t.Fatalf("stdout %q stderr %q", k.out.String(), k.err.String())
	}
}

// E2: argument errors are usage errors, exit 64, and touch nothing.
func TestE2UsageErrors(t *testing.T) {
	cases := [][]string{
		{"fetch"},
		{"fetch", "a", "b"},
		{"build-base"},
		{"run"},
		{"verify"},
		{"refresh-base"},
		{"freshness-status", "--shallow"},
		{"freshness-status", "--deep", "x"},
		{"sweep", "x"},
		{"nightly", "x"},
		{"ensure-base"},
		{"member-up", "m1"},
		{"member-up", "m1", "k", "x"},
		{"member-ssh", "m1"},
		{"member-down"},
		{"run", "s", "--commit"},
		{"run", "s", "--bogus", "x"},
		{"run", "s", "--commit", testCommit, "--commit", testCommit},
		{"run", "s", "--github-account"},
	}
	for _, args := range cases {
		k := newKit(t)
		k.wantCode(k.run(args...), term.ExitUsage)
		if !strings.Contains(k.err.String(), "ERROR: ") || len(k.fake.Calls()) != 0 || k.out.Len() != 0 {
			t.Fatalf("%v: calls %d stdout %q stderr %q", args, len(k.fake.Calls()), k.out.String(), k.err.String())
		}
	}
}

// E4: a signal ends the command with 128+n, never 0, whatever the step returned.
func TestE4SignalExitStatus(t *testing.T) {
	for sig, want := range map[syscall.Signal]term.ExitCode{syscall.SIGINT: 130, syscall.SIGTERM: 143, syscall.SIGHUP: 129} {
		k := newKit(t)
		ctx, cancel := context.WithCancelCause(context.Background())
		cancel(InterruptError{Signal: sig})
		k.wantCode(k.runCtx(ctx, "member-down", "m1"), want)
		k.wantErr("interrupted")
	}
}

// E6: VMTEST_HELPERS_LIB is the helpers' PYTHONPATH, and the module runs with -P (B2).
func TestE6HelperSeam(t *testing.T) {
	k := newKit(t)
	k.run("ensure-base", "server-fast")
	c, ok := k.findCall("helpers.vmtest.validate_manifest")
	if !ok {
		t.Fatal("the validator was not run")
	}
	if c.Name != "python3" || c.Args[0] != "-P" || !strings.Contains(strings.Join(c.Env, " "), "PYTHONPATH=/opt/helpers") {
		t.Fatalf("got %s env %v", c.Line(), c.Env)
	}
	if got := stdinText(t, c); got != "{}\n" {
		t.Fatalf("the manifest was not on stdin: %q", got)
	}
	k.env["VMTEST_HELPERS_LIB"] = ""
	k.fake = &execx.Fake{}
	k.fake.OnFunc(helperArgv("validate_manifest"), k.validator)
	k.run("ensure-base", "server-fast")
	if c, ok := k.findCall("validate_manifest"); !ok || !strings.Contains(strings.Join(c.Env, " "), "PYTHONPATH=/usr/local/lib/ccy-helpers") {
		t.Fatalf("default helpers lib not used: %v", c.Env)
	}
}
