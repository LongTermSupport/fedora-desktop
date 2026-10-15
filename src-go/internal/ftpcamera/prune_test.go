package ftpcamera

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// pruneTree is a library with two orphans (one in a dated tree with a JPEG/ sibling) and two
// RAWs whose JPG is still there.
func pruneTree(k *kit) {
	k.put("photos/2026/10/14/RAW/A.ARW", make([]byte, 1000), testStart)
	k.put("photos/2026/10/14/JPG/a.jpg", jpegBytes(), testStart) // case-insensitive pair
	k.put("photos/2026/10/14/RAW/B.ARW", make([]byte, 1500), testStart)
	k.put("photos/2026/10/15/RAW/C.arw", make([]byte, 600), testStart)
	k.put("photos/2026/10/15/JPEG/C.JPEG", jpegBytes(), testStart)
	k.put("photos/2026/10/15/RAW/D.ARW", make([]byte, 100), testStart)
	k.put("photos/2026/10/15/RAW/notes.txt", []byte("x"), testStart)
}

// E37-E40.
func TestPruneDeletesOrphansOnYes(t *testing.T) {
	k := newKit(t)
	k.realRoot()
	pruneTree(k)
	k.stdin = "y\n"
	k.stdinTTY = true
	k.wantCode(k.run("--prune"), term.ExitOK)
	k.wantOut("Scanning for orphan RAW files...\n",
		"Found 2 orphan RAW file(s) (1.6KiB total):\n",
		"  photos/2026/10/14/RAW/B.ARW\n", "  photos/2026/10/15/RAW/D.ARW\n",
		"Prune complete: 2 of 2 files removed (1.6KiB freed)\n")
	k.wantErr("Delete these files? [y/N] ")
	for rel, want := range map[string]bool{
		"photos/2026/10/14/RAW/A.ARW": true, "photos/2026/10/14/RAW/B.ARW": false,
		"photos/2026/10/15/RAW/C.arw": true, "photos/2026/10/15/RAW/D.ARW": false,
	} {
		if exists(t, filepath.Join(k.upload, rel)) != want {
			t.Errorf("%s exists=%v, want %v", rel, !want, want)
		}
	}
	k.wantRan("sudo -n find " + k.upload + " -mindepth 1 -type d -empty -delete")
}

// E39: anything but y is no; end of input is no, with a blank line; no prompt off a terminal.
func TestPruneDeclines(t *testing.T) {
	for _, in := range []string{"n\n", "yes\n", "", "Y"} {
		k := newKit(t)
		k.realRoot()
		pruneTree(k)
		k.stdin = in
		k.wantCode(k.run("--prune"), term.ExitOK)
		if in == "Y" {
			// A last line with no newline still ends in end of input, as read does: no.
			k.wantOut("Aborted — no files removed.\n")
			continue
		}
		k.wantOut("Aborted — no files removed.\n")
		if strings.Contains(k.err.String(), "Delete these files?") {
			t.Errorf("%q: prompted off a terminal", in)
		}
		if in == "" && k.err.String() != "\n" {
			t.Errorf("end of input: stderr %q", k.err.String())
		}
		k.wantNotRan("sudo -n rm")
	}
}

func TestPruneNothingToDo(t *testing.T) {
	k := newKit(t)
	k.wantCode(k.run("--prune"), term.ExitOK)
	k.wantOut("No photos/ subtree — nothing to prune.\n")
	k.put("photos/2026/10/14/RAW/A.ARW", []byte("x"), testStart)
	k.put("photos/2026/10/14/JPG/A.JPG", jpegBytes(), testStart)
	k.wantCode(k.run("--prune"), term.ExitOK)
	k.wantOut("No orphan RAW files found.\n")
	if err := os.RemoveAll(k.upload); err != nil {
		t.Fatal(err)
	}
	k.wantCode(k.run("--prune"), term.ExitFailure)
	k.wantErr("ERROR: Upload directory not found: " + k.upload + "\n")
}

// E40: a failed removal is reported and the rest go on.
func TestPruneRemovalFailureGoesOn(t *testing.T) {
	k := newKit(t)
	k.realRoot()
	pruneTree(k)
	k.fake.On([]string{"sudo", "-n", "rm", "-f", filepath.Join(k.upload, "photos/2026/10/14/RAW/B.ARW")}, execx.Result{ExitCode: 1})
	k.stdin = "Y\n"
	k.wantCode(k.run("--prune"), term.ExitOK)
	k.wantErr("  failed  photos/2026/10/14/RAW/B.ARW\n")
	k.wantOut("Prune complete: 1 of 2 files removed")
}
