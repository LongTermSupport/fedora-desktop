package open

import (
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// E1: every option, long and short, and -d implying --ask.
func TestParseArgsReadsEveryOption(t *testing.T) {
	o, err := parseArgs([]string{"-a", "--terminal", "-l", "--dry-run", "x", "y"})
	if err != nil {
		t.Fatal(err)
	}
	if !o.ask || !o.terminal || !o.list || !o.dryRun || o.setDefault || o.help {
		t.Fatalf("options %+v", o)
	}
	if !slices.Equal(o.targets, []string{"x", "y"}) {
		t.Fatalf("targets %q", o.targets)
	}
}

// E1: the other spelling of each option.
func TestParseArgsReadsTheOtherSpellings(t *testing.T) {
	o, err := parseArgs([]string{"--set-default", "--ask", "-t", "-n", "--list", "-h"})
	if err != nil || !o.setDefault || !o.ask || !o.terminal || !o.dryRun || !o.list || !o.help {
		t.Fatalf("long and short forms: %+v, %v", o, err)
	}
	o, err = parseArgs([]string{"-d", "f"})
	if err != nil || !o.setDefault || !o.ask {
		t.Fatalf("-d implies --ask: %+v, %v", o, err)
	}
}

// E1: -- ends the options, so a target may start with a dash.
func TestParseArgsDoubleDashEndsTheOptions(t *testing.T) {
	o, err := parseArgs([]string{"-a", "--", "-weird", "--help"})
	if err != nil || o.help || !slices.Equal(o.targets, []string{"-weird", "--help"}) {
		t.Fatalf("got %+v, %v", o, err)
	}
}

// E1, B14: --help anywhere wins, even after an unknown option.
func TestHelpAnywherePrintsTheUsageOnStdout(t *testing.T) {
	for _, args := range [][]string{{"-h"}, {"--bogus", "--help"}, {"x", "-h"}} {
		w := newWorld(t)
		if code := w.run(args...); code != term.ExitOK {
			t.Fatalf("%q: exit %d", args, code)
		}
		if !strings.HasPrefix(w.stdout.String(), "Usage: open [options] TARGET...") {
			t.Fatalf("%q: stdout %q", args, w.stdout.String())
		}
		if w.stderr.Len() != 0 || len(w.ran()) != 0 {
			t.Fatalf("%q: stderr %q, ran %q", args, w.stderr.String(), w.ran())
		}
	}
}

// E2, B14: an unknown option and no target are usage errors, exit 64.
func TestUsageErrorsExit64(t *testing.T) {
	w := newWorld(t)
	if code := w.run("--bogus", "x"); code != term.ExitUsage {
		t.Fatalf("unknown option: exit %d", code)
	}
	if got := w.stderr.String(); got != "open: unknown option '--bogus' — see 'open --help'.\n" {
		t.Fatalf("stderr %q", got)
	}
	w = newWorld(t)
	if code := w.run("-a"); code != term.ExitUsage {
		t.Fatalf("no target: exit %d", code)
	}
	if got := w.stderr.String(); got != "open: nothing to open — see 'open --help'.\n" {
		t.Fatalf("stderr %q", got)
	}
	w = newWorld(t)
	if code := w.run("-x"); code != term.ExitUsage || !strings.Contains(w.stderr.String(), "'-x'") {
		t.Fatalf("a short unknown option: exit %d, %q", code, w.stderr.String())
	}
}

// The usage text lists every option and says where --list and --dry-run print.
func TestUsageListsEveryOption(t *testing.T) {
	for _, want := range []string{"--ask", "--terminal", "--set-default", "--list", "--dry-run", "--help", "stdout"} {
		if !strings.Contains(usageText, want) {
			t.Fatalf("usage lacks %s", want)
		}
	}
}
