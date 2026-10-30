package reclaim

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// E3, E4, E5, E6: the report's sections in order, df without duf, du figures formatted.
func TestReportSections(t *testing.T) {
	w := newWorld(t)
	w.install("journalctl")
	w.dir(".cache")
	w.answer([]string{"df", "-h", "-x"}, "Mounted on  Size\n/  100G\n", 0)
	w.answer([]string{"journalctl", "--disk-usage"}, "Archived and active journals take up 1.2G in the file system.\n", 0)
	w.answer([]string{"du", "-s", "-B1"}, "4096\tsomewhere\n", 0)
	wantCode(t, w.run("report"), term.ExitOK)
	got := w.stdout.String()
	want := "reclaim disk report — box\n\nFilesystems:\nMounted on  Size\n/  100G\n\n" +
		"Journal:\nArchived and active journals take up 1.2G in the file system.\n\n" +
		"Notable directories:\n4.0KB\t" + w.home + "/.cache\n"
	if !strings.HasPrefix(got, want) {
		t.Fatalf("report:\n%s\nwant prefix:\n%s", got, want)
	}
	if strings.Contains(got, "Podman storage") {
		t.Error("podman section without podman")
	}
	if !w.ran("df -h -x tmpfs -x devtmpfs -x efivarfs -x overlay --output=target,size,used,avail,pcent") {
		t.Error("df not run with its filters")
	}
	if w.ran("duf") {
		t.Error("duf run while not installed")
	}
}

// E4: duf when installed and it works; df -h when the filtered df fails; all failing is noted.
func TestReportFilesystems(t *testing.T) {
	w := newWorld(t)
	w.install("duf")
	w.answer([]string{"duf", "--only", "local"}, "DUF TABLE\n", 0)
	wantCode(t, w.run("report"), term.ExitOK)
	w.wantOut("Filesystems:\nDUF TABLE\n")
	if w.ran("df") {
		t.Error("df run although duf answered")
	}

	w = newWorld(t)
	w.answer([]string{"df", "-h", "-x"}, "", 1)
	w.answer([]string{"df", "-h"}, "PLAIN DF\n", 0)
	w.answer([]string{"df", "-h", "-x"}, "", 1)
	wantCode(t, w.run("report"), term.ExitOK)
	w.wantOut("Filesystems:\nPLAIN DF\n")

	w = newWorld(t)
	w.answerErr([]string{"df"}, "", "df: broken\n", 1)
	wantCode(t, w.run("report"), term.ExitFailure)
	w.wantOut("  (filesystem usage unavailable)\n", "Journal:")
	w.wantErr("reclaim: filesystem usage: filtered df: df exited 1: df: broken; df -h: df exited 1: df: broken\n")

	// Every step's reason is said, not only the last one's.
	w = newWorld(t)
	w.install("duf")
	w.answerErr([]string{"duf"}, "", "duf: no mounts\n", 2)
	w.answerErr([]string{"df", "-h", "-x"}, "", "df: bad filter\n", 1)
	w.answerErr([]string{"df", "-h"}, "", "df: broken\n", 1)
	w.answerErr([]string{"df", "-h", "-x"}, "", "df: bad filter\n", 1)
	wantCode(t, w.run("report"), term.ExitFailure)
	w.wantErr("reclaim: filesystem usage: duf: duf exited 2: duf: no mounts; filtered df: df exited 1: df: bad filter; df -h: df exited 1: df: broken\n")
}

// E5: journalctl absent, and failing.
func TestReportJournal(t *testing.T) {
	w := newWorld(t)
	wantCode(t, w.run("report"), term.ExitOK)
	w.wantOut("Journal:\njournal: journalctl not present\n")

	w = newWorld(t)
	w.install("journalctl")
	w.answerErr([]string{"journalctl"}, "", "No journal files were found.\n", 1)
	wantCode(t, w.run("report"), term.ExitFailure)
	w.wantOut("Journal:\njournal: usage unavailable\n", "Notable directories:")
	w.wantErr("No journal files were found.")
}

// E6, B7: a partial du is a lower bound and does not fail; no figure does.
func TestReportDirectories(t *testing.T) {
	w := newWorld(t)
	w.dir(".cache")
	w.dir(".local/share/Trash")
	w.answerErr([]string{"du", "-s", "-B1", w.home + "/.cache"}, "2048\tx\n", "du: cannot read directory\n", 1)
	w.answerErr([]string{"du", "-s", "-B1", w.home + "/.local/share/Trash"}, "", "du: gone\n", 1)
	wantCode(t, w.run("report"), term.ExitFailure)
	w.wantOut("2.0KB\t"+w.home+"/.cache\t(at least: some entries could not be read)\n",
		"?\t"+w.home+"/.local/share/Trash\n")
	w.wantErr("du: gone")
}

// E6: the system directories are sized after the home ones, each that exists.
func TestReportSystemDirectories(t *testing.T) {
	w := newWorld(t)
	logs := t.TempDir()
	w.varDirs = []string{logs, logs + "/missing"}
	w.answer([]string{"du", "-s", "-B1", logs}, "1048576\t"+logs+"\n", 0)
	wantCode(t, w.run("report"), term.ExitOK)
	w.wantOut("Notable directories:\n1.0MB\t" + logs + "\n")
	if strings.Contains(w.stdout.String(), "missing") {
		t.Error("a missing directory was listed")
	}
	if NewEnv().VarDirs[0] != "/var/log" || NewEnv().VarDirs[1] != "/var/cache" {
		t.Errorf("production VarDirs: %v", NewEnv().VarDirs)
	}
}

// E3, E7: podman's section, and podman failing.
func TestReportPodman(t *testing.T) {
	w := newWorld(t)
	w.install("podman")
	w.answer([]string{"podman", "system", "df"}, "TYPE TOTAL\nImages 3\n", 0)
	wantCode(t, w.run("report"), term.ExitOK)
	w.wantOut("\nPodman storage:\nTYPE TOTAL\nImages 3\n")

	w = newWorld(t)
	w.install("podman")
	w.answerErr([]string{"podman", "system", "df"}, "", "Error: stale temp dir\n", 125)
	wantCode(t, w.run("report"), term.ExitFailure)
	w.wantOut("Podman storage:\n  (podman storage unavailable)\n")
	w.wantErr("stale temp dir")
}

// E29: the report writes nothing but diagnostics to stderr.
func TestReportStderrClean(t *testing.T) {
	w := newWorld(t)
	wantCode(t, w.run("report"), term.ExitOK)
	if w.stderr.Len() != 0 {
		t.Errorf("stderr: %q", w.stderr.String())
	}
}

// E6: du's permission refusals under one root are one line, counted, the first named; any
// other complaint of du's is shown as it is.
func TestReportDuRefusalsSummarised(t *testing.T) {
	w := newWorld(t)
	logs := t.TempDir()
	w.varDirs = []string{logs}
	w.answerErr([]string{"du", "-s", "-B1", logs}, "8192\t"+logs+"\n",
		"du: cannot read directory '"+logs+"/audit': Permission denied\n"+
			"du: cannot read directory '"+logs+"/private': Permission denied\n"+
			"du: cannot access '"+logs+"/x/y': Permission denied\n"+
			"du: fts_read failed: odd\n", 1)
	wantCode(t, w.run("report"), term.ExitOK)
	w.wantOut("8.0KB\t" + logs + "\t(at least: some entries could not be read)\n")
	w.wantErr("reclaim: du "+logs+": 3 entries could not be read (permission denied), the first '"+
		logs+"/audit'; the size is a lower bound.\n", "du: fts_read failed: odd\n")
	w.wantNoErr("cannot read directory", "cannot access")

	w = newWorld(t)
	w.varDirs = []string{logs}
	w.answerErr([]string{"du", "-s", "-B1", logs}, "8192\t"+logs+"\n",
		"du: cannot read directory '"+logs+"/audit': Permission denied\n", 1)
	wantCode(t, w.run("report"), term.ExitOK)
	w.wantErr("reclaim: du " + logs + ": 1 entry could not be read (permission denied), the first '" +
		logs + "/audit'; the size is a lower bound.\n")
}

// E7: a podman system df refused permission (a volume's data owned by a container's uid) is
// read again under podman unshare, the refusal named; both failing names both.
func TestReportPodmanUnshare(t *testing.T) {
	refused := "Error: open /s/volumes/v/_data: permission denied\n"
	w := newWorld(t)
	w.install("podman")
	w.answerErr([]string{"podman", "system", "df"}, "", refused, 125)
	w.answer([]string{"podman", "unshare", "podman", "system", "df"}, "TYPE TOTAL\nImages 3\n", 0)
	wantCode(t, w.run("report"), term.ExitOK)
	w.wantOut("\nPodman storage:\nTYPE TOTAL\nImages 3\n")
	w.wantErr("reclaim: podman system df: podman exited 125: Error: open /s/volumes/v/_data: permission denied\n",
		"reclaim: reading it again inside podman's user namespace: podman unshare podman system df\n")

	w = newWorld(t)
	w.install("podman")
	w.answerErr([]string{"podman", "system", "df"}, "", refused, 125)
	w.answerErr([]string{"podman", "unshare", "podman", "system", "df"}, "", "Error: still no\n", 125)
	wantCode(t, w.run("report"), term.ExitFailure)
	w.wantOut("Podman storage:\n  (podman storage unavailable)\n")
	w.wantErr("permission denied", "reclaim: podman unshare podman system df: podman exited 125: Error: still no\n")

	w = newWorld(t)
	w.install("podman")
	w.answerErr([]string{"podman", "system", "df"}, "", "Error: stale temp dir\n", 125)
	wantCode(t, w.run("report"), term.ExitFailure)
	if w.ran("podman unshare") {
		t.Error("podman unshare run for a failure that was not a refused permission")
	}
}
