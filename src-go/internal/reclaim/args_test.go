package reclaim

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// E1: the help and the version are the payload, on stdout.
func TestHelpAndVersion(t *testing.T) {
	for _, arg := range []string{"-h", "--help"} {
		w := newWorld(t)
		wantCode(t, w.run(arg), term.ExitOK)
		w.wantOut("Usage: reclaim [SUBCOMMAND]", "report")
		if w.stderr.Len() != 0 {
			t.Errorf("%s wrote to stderr: %q", arg, w.stderr.String())
		}
	}
	w := newWorld(t)
	wantCode(t, w.run("--version"), term.ExitOK)
	if got := w.stdout.String(); got != "reclaim "+Version+"\n" {
		t.Errorf("--version printed %q", got)
	}
	if Version != "2.0.5" {
		t.Errorf("Version is %q", Version)
	}
}

// E2: anything else is a usage error, before anything runs.
func TestUnknownArgument(t *testing.T) {
	for _, args := range [][]string{{"--frob"}, {"report", "extra"}, {"menu"}, {"--help", "x"}} {
		w := newWorld(t)
		wantCode(t, w.run(args...), term.ExitUsage)
		bad := args[0]
		if len(args) > 1 {
			bad = args[1]
		}
		w.wantErr("reclaim: unknown argument: " + bad + " — see 'reclaim --help'.")
		if w.stdout.Len() != 0 || len(w.fake.Calls()) != 0 {
			t.Errorf("%v: printed %q, ran %d commands", args, w.stdout.String(), len(w.fake.Calls()))
		}
	}
}

// E29: an unwritable stdout is a failure.
func TestUnwritableStdout(t *testing.T) {
	w := newWorld(t)
	env := w.envFor()
	env.Stdout = failWriter{}
	if code := (App{Env: env}).Run(t.Context(), []string{"--version"}); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.stderr.String(), "reclaim: could not write the output") {
		t.Errorf("stderr: %q", w.stderr.String())
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errClosed }
