package reclaim

import (
	"errors"
	"io/fs"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// E26: trash-cli's listing failing is noted and still asks; no is no; trash-empty failing fails.
func TestTrashCLIFailures(t *testing.T) {
	w := newWorld(t)
	w.install("trash-empty", "trash-list")
	w.answer([]string{"trash-list"}, "", 1)
	w.typed("9", "n", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  (trash empty or unavailable)", "  Skipped.")
	if w.ran("trash-empty") {
		t.Error("emptied after no")
	}

	w = newWorld(t)
	w.install("trash-empty", "trash-list")
	w.answer([]string{"trash-empty", "-f"}, "", 1)
	w.typed("9", "y", "q")
	wantCode(t, w.run(), term.ExitFailure)
	w.wantErr("  trash-empty reported an error.")
}

// E17: baobab that cannot be started fails the action, named.
func TestBaobabFailures(t *testing.T) {
	w := newWorld(t)
	w.install("ncdu", "baobab")
	w.answer([]string{"ncdu"}, "", 0)
	w.answerErr([]string{"setsid", "-f", "baobab"}, "", "setsid: failed\n", 1)
	w.env["DISPLAY"] = ":0"
	w.typed("1", "", "y", "q")
	wantCode(t, w.run(), term.ExitFailure)
	w.wantErr("  baobab could not start: setsid exited 1: setsid: failed")

	w = newWorld(t)
	w.install("ncdu", "baobab")
	w.answer([]string{"ncdu"}, "", 0)
	w.devNull = w.home + "/missing/null"
	w.env["DISPLAY"] = ":0"
	w.typed("1", "", "y", "q")
	wantCode(t, w.run(), term.ExitFailure)
	w.wantErr("  baobab not started: ")
}

// E20: a kernel removal that fails fails the action; no removes nothing.
func TestOldKernelsRemovalFails(t *testing.T) {
	w := newWorld(t)
	w.install("dnf")
	w.answer([]string{"dnf", "repoquery", "--installonly"}, "kernel-6.1\n", 0)
	w.answer([]string{"dnf", "-y", "remove"}, "", 1)
	w.typed("4", "y", "q")
	wantCode(t, w.run(), term.ExitFailure)
	w.wantErr("  dnf remove reported an error.")

	w = newWorld(t)
	w.install("dnf")
	w.answer([]string{"dnf", "repoquery", "--installonly"}, "kernel-6.1\n", 0)
	w.typed("4", "n", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  Skipped.")
	if w.ran("sudo") {
		t.Error("sudo asked after no")
	}
}

// E21: a journal line that could not be read is said with its reason, and the vacuum is still
// offered.
func TestJournalUsageUnread(t *testing.T) {
	w := newWorld(t)
	w.install("journalctl")
	w.answerErr([]string{"journalctl", "--disk-usage"}, "", "No journal files.\n", 1)
	w.typed("5", "", "n", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("    journal: usage unavailable (see the reason above)", "No journal files.")
}

// E18: a count that is never valid gives up without failing the run.
func TestBiggestFilesGivesUp(t *testing.T) {
	w := newWorld(t)
	w.typed("2", "", "0", "-1", "x", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  Giving up on this action after 3 tries.")
}

// E3: a host name that cannot be read is said, and the report goes on.
func TestHostnameUnread(t *testing.T) {
	w := newWorld(t)
	env := w.envFor()
	env.Hostname = func() (string, error) { return "", errors.New("no uts") }
	wantCode(t, App{Env: env}.Run(w.runCtx(), []string{"report"}), term.ExitOK)
	w.wantOut("reclaim disk report — this host\n")
	w.wantErr("reclaim: could not read the host name: no uts")
}

// E18: an unreadable entry is counted and the first named, by its path error's own path.
func TestFoundNote(t *testing.T) {
	var f found
	f.note("/x/a", &fs.PathError{Op: "open", Path: "/x/a/inner", Err: fs.ErrPermission})
	f.note("/x/b", errors.New("gone"))
	if f.unreadable != 2 || f.first != "/x/a/inner: permission denied" {
		t.Errorf("%+v", f)
	}
	var g found
	g.note("/x/b", errors.New("gone"))
	if g.first != "/x/b: gone" {
		t.Errorf("%+v", g)
	}
}

// capital starts a sentence with an upper-case letter, leaving any other start as it is.
func TestCapital(t *testing.T) {
	for in, want := range map[string]string{"the trash": "The trash", "~/.cache": "~/.cache", "": ""} {
		if got := capital(in); got != want {
			t.Errorf("capital(%q) = %q", in, got)
		}
	}
}
