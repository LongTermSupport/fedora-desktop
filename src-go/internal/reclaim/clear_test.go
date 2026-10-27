package reclaim

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// B6: a du that could read only part of what it sized shows its own stderr, naming the entry.
func TestPartialDuShowsItsReason(t *testing.T) {
	w := newWorld(t)
	w.answerErr([]string{"du", "-x", "-B1", "--max-depth=1", w.home}, "5\t/d/small\n", "du: cannot read directory '/d/locked': Permission denied\n", 1)
	w.typed("1", "", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("du: cannot read directory '/d/locked': Permission denied\n", "  (some entries could not be read)")

	w = newWorld(t)
	w.dir(".cache")
	w.answerErr([]string{"du", "-s", "-B1"}, "2048\tx\n", "du: cannot read directory 'x/locked': Permission denied\n", 1)
	wantCode(t, w.run("report"), term.ExitOK)
	w.wantErr("du: cannot read directory 'x/locked': Permission denied\n")

	w = newWorld(t)
	trash := w.dir(".local/share/Trash")
	w.file(".local/share/Trash/files/a", "x")
	w.answerErr([]string{"du", "-s", "-B1", trash}, "8192\t"+trash+"\n", "du: cannot read directory 'files/x': Permission denied\n", 1)
	w.typed("9", "n", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  Trash size: 8.0KB (at least)", "du: cannot read directory 'files/x': Permission denied\n")
}

// E24, E26, B3: a listing that fails ends the action before asking, as a failed preview does;
// nothing is removed.
func TestFailedListingStopsTheClear(t *testing.T) {
	w := newWorld(t)
	w.file(".cache/a", "x")
	w.answerErr([]string{"du"}, "", "du: broken\n", 1)
	w.typed("8", "y", "q")
	wantCode(t, w.run(), term.ExitFailure)
	w.wantErr("  Not clearing ~/.cache: its contents could not be listed.")
	w.wantNoErr("Delete the CONTENTS of ~/.cache now?")
	if len(entries(t, filepath.Join(w.home, ".cache"))) != 1 {
		t.Error("cleared after a failed listing")
	}

	w = newWorld(t)
	w.file(".local/share/Trash/files/a", "x")
	w.answerErr([]string{"du"}, "", "du: broken\n", 1)
	w.typed("9", "y", "q")
	wantCode(t, w.run(), term.ExitFailure)
	w.wantErr("  Not emptying the trash: its size could not be read.")
	w.wantNoErr("Empty ~/.local/share/Trash now?")
	if len(entries(t, filepath.Join(w.home, ".local/share/Trash/files"))) != 1 {
		t.Error("emptied after a failed listing")
	}
}

// B13: ~/.cache, the trash or its files and info directories as a symlink are refused, naming
// the link and its target; nothing behind the link is touched.
func TestClearRefusesASymlink(t *testing.T) {
	w := newWorld(t)
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "precious"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(w.home, ".cache")); err != nil {
		t.Fatal(err)
	}
	w.typed("8", "y", "q")
	wantCode(t, w.run(), term.ExitFailure)
	w.wantErr("  ~/.cache is a symbolic link (to " + target + "); reclaim does not clear through a link.")
	w.wantNoErr("Delete the CONTENTS")
	if len(entries(t, target)) != 1 {
		t.Error("cleared through the link")
	}

	for _, link := range []string{".local/share/Trash", ".local/share/Trash/files", ".local/share/Trash/info"} {
		w = newWorld(t)
		w.dir(".local/share")
		w.dir(".local/share/Trash")
		target := t.TempDir()
		if err := os.WriteFile(filepath.Join(target, "precious"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if link == ".local/share/Trash" {
			if err := os.Remove(filepath.Join(w.home, link)); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Symlink(target, filepath.Join(w.home, link)); err != nil {
			t.Fatal(err)
		}
		w.typed("9", "y", "q")
		wantCode(t, w.run(), term.ExitFailure)
		w.wantErr("is a symbolic link (to " + target + "); reclaim does not clear through a link.")
		if len(entries(t, target)) != 1 {
			t.Errorf("%s: emptied through the link", link)
		}
	}
}
