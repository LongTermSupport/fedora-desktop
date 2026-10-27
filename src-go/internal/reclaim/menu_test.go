package reclaim

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// E9: no terminal, no menu.
func TestMenuNeedsATerminal(t *testing.T) {
	const msg = "reclaim: needs an interactive terminal; run 'reclaim report' for a non-interactive summary."
	w := newWorld(t)
	w.stdinTTY = false
	wantCode(t, w.run(), term.ExitFailure)
	w.wantErr(msg)

	w = newWorld(t)
	w.noTTY = true
	wantCode(t, w.run(), term.ExitFailure)
	w.wantErr(msg)
	if len(w.fake.Calls()) != 0 {
		t.Errorf("ran %v", w.fake.Calls())
	}
}

// E10, E11, E12: the header, the menu, and q.
func TestMenuShowsHeaderAndQuits(t *testing.T) {
	w := newWorld(t)
	w.install("journalctl", "podman", "ncdu")
	w.answer([]string{"df"}, "/  100G\n", 0)
	w.answer([]string{"journalctl", "--disk-usage"}, "Journals take up 8M.\n", 0)
	w.typed("q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("reclaim\n", "box · v2.0.4", "  /  100G\n", "  Journals take up 8M.\n",
		"Analyse\n", "   1) Biggest directories        ncdu\n", "   2) Biggest files\n",
		"Reclaim  (destructive — each confirms first)\n",
		"   3) dnf autoremove + clean all (sudo)\n",
		"   4) Remove old kernels         (sudo, keep newest 2)\n",
		"   5) Vacuum systemd journal     (sudo)\n",
		"   6) Prune containers           podman\n",
		"   7) Flatpak unused runtimes    (flatpak absent)\n",
		"   8) Clear ~/.cache\n", "   9) Empty trash\n", "  10) Safe sweep (run 3,5,6,7,9)\n",
		"Other\n", "   r) Refresh report     q) Quit\n", "Choose [1-10, r, q]: ", "Bye.")
	if w.stdout.Len() != 0 {
		t.Errorf("the menu wrote to stdout: %q", w.stdout.String())
	}
}

// E11: the notes beside 1, 6 and 7 follow what is installed.
func TestMenuNotes(t *testing.T) {
	w := newWorld(t)
	w.install("docker", "flatpak")
	w.typed("quit")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("   1) Biggest directories        du summary\n",
		"   6) Prune containers           docker\n",
		"   7) Flatpak unused runtimes    flatpak\n")

	w = newWorld(t)
	w.install("docker", "podman")
	w.typed("exit")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("   6) Prune containers           podman + docker\n")

	w = newWorld(t)
	w.typed("Q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("   6) Prune containers           -\n")
}

// E12: end of input ends the run cleanly; an empty line asks again without counting.
func TestMenuEOFAndEmpty(t *testing.T) {
	w := newWorld(t)
	w.tty = ""
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("No more input — exiting.")

	w = newWorld(t)
	w.typed("", "", "", "x", "", "q")
	wantCode(t, w.run(), term.ExitOK)
	if n := strings.Count(w.stderr.String(), "Choose [1-10, r, q]: "); n != 6 {
		t.Errorf("asked %d times", n)
	}
	w.wantErr("Bye.")
}

// E12: three invalid choices in a row end the run; a valid one resets the count.
func TestMenuInvalidChoices(t *testing.T) {
	w := newWorld(t)
	w.typed("x", "11", "0")
	wantCode(t, w.run(), term.ExitFailure)
	w.wantErr("  Not a valid choice: 'x'. Pick 1-10, r, or q.", "  Not a valid choice: '11'.",
		"Too many invalid choices — exiting.")

	w = newWorld(t)
	w.typed("x", "y", "r", "x", "y", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantNoErr("Too many invalid choices")
}

// E10: r draws the header again.
func TestMenuRefresh(t *testing.T) {
	w := newWorld(t)
	w.answer([]string{"df"}, "/  100G\n", 0)
	w.typed("r", "q")
	wantCode(t, w.run(), term.ExitOK)
	if n := strings.Count(w.stderr.String(), "  /  100G\n"); n != 2 {
		t.Errorf("header drawn %d times", n)
	}
}

// E14: each action starts with its heading.
func TestActionHeadings(t *testing.T) {
	names := map[string]string{
		"1": "Biggest directories", "2": "Biggest files", "3": "dnf autoremove + clean all",
		"4": "Remove old kernels", "5": "Vacuum systemd journal", "6": "Prune containers",
		"7": "Flatpak unused runtimes", "8": "Clear ~/.cache", "9": "Empty trash", "10": "Safe sweep",
	}
	for choice, name := range names {
		w := newWorld(t)
		w.typed(choice)
		// Nothing is installed, so some actions fail; only the heading is asked about here.
		w.run()
		w.wantErr("reclaim › " + name + "\n")
	}
}

// E13: an action that failed makes the run exit 1 when it ends (B9).
func TestFailedActionFailsTheRun(t *testing.T) {
	w := newWorld(t)
	w.install("dnf")
	w.answer([]string{"dnf", "-y", "autoremove"}, "", 1)
	w.typed("3", "y", "q")
	wantCode(t, w.run(), term.ExitFailure)
	w.wantErr("  dnf reported an error — see output above.", "Bye.",
		"reclaim: 1 action(s) failed in this run; see the messages above.")
	if w.ran("sudo -n dnf clean all") {
		t.Error("clean ran after autoremove failed")
	}
}

// E29: colour only on a terminal without NO_COLOR, the same text either way.
func TestColour(t *testing.T) {
	w := newWorld(t)
	w.colour = true
	w.typed("x", "q")
	wantCode(t, w.run(), term.ExitOK)
	if !strings.Contains(w.stderr.String(), "\x1b[") {
		t.Error("no colour on a terminal")
	}
	plain := strings.Contains(w.stderr.String(), "Not a valid choice: 'x'")
	if !plain {
		t.Error("text changed with colour")
	}

	w = newWorld(t)
	w.colour = true
	w.env["NO_COLOR"] = "1"
	w.typed("q")
	wantCode(t, w.run(), term.ExitOK)
	if strings.Contains(w.stderr.String(), "\x1b[") {
		t.Errorf("colour with NO_COLOR: %q", w.stderr.String())
	}
}
