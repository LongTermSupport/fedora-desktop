package reclaim

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// E15: only y or yes, in any case, is yes.
func TestConfirmAnswers(t *testing.T) {
	answers := map[string]bool{
		"y": true, "Y": true, "yes": true, "YES": true, "Yes": true,
		"": false, "n": false, "no": false, "yy": false, "sure": false,
	}
	for answer, yes := range answers {
		w := newWorld(t)
		w.install("flatpak")
		w.answer([]string{"flatpak", "uninstall", "--unused", "-y"}, "", 0)
		w.typed("7", answer, "q")
		wantCode(t, w.run(), term.ExitOK)
		if got := w.ran("flatpak uninstall --unused -y"); got != yes {
			t.Errorf("answer %q: removed %v", answer, got)
		}
		w.wantErr("  Remove unused flatpak runtimes? [y/N] ")
		if !yes {
			w.wantErr("  Skipped.")
		}
	}
	// End of input at a confirmation is no.
	w := newWorld(t)
	w.install("flatpak")
	w.tty = "7\n"
	wantCode(t, w.run(), term.ExitOK)
	if w.ran("flatpak uninstall --unused -y") {
		t.Error("removed on end of input")
	}
}

// E16: a value question takes its default on an empty line, asks again on a bad value three
// times in all, and ends the action on end of input; neither fails the run.
func TestValueQuestions(t *testing.T) {
	w := newWorld(t)
	w.install("journalctl")
	w.answer([]string{"journalctl", "--vacuum-size=500M"}, "", 0)
	w.typed("5", "", "y", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("Vacuum journal down to what size? [500M] ", "  Vacuum system journal to 500M? [y/N] ")
	if !w.ran("sudo -n journalctl --vacuum-size=500M") {
		t.Error("default size not used")
	}

	w = newWorld(t)
	w.install("journalctl")
	w.answer([]string{"journalctl", "--vacuum-size=1G"}, "", 0)
	w.typed("5", "big", "1G", "y", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  Not a valid size (e.g. 500M, 1G): big")
	if !w.ran("sudo -n journalctl --vacuum-size=1G") {
		t.Error("second answer not used")
	}

	w = newWorld(t)
	w.install("journalctl")
	w.typed("5", "a", "b", "c", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  Giving up on this action after 3 tries.", "Bye.")
	if w.ran("sudo -n journalctl --vacuum") {
		t.Error("vacuumed after giving up")
	}

	w = newWorld(t)
	w.install("journalctl")
	w.tty = "5\n"
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  Cancelled.")
}

// E17: ncdu in the foreground on the chosen directory, its exit noted.
func TestBiggestDirectoriesNcdu(t *testing.T) {
	w := newWorld(t)
	w.install("ncdu")
	sub := w.dir("data")
	w.answer([]string{"ncdu"}, "", 2)
	w.typed("1", "/nonexistent-dir", sub, "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("Analyse which directory? ["+w.home+"] ", "  Not a directory: /nonexistent-dir", "  ncdu exited 2.")
	fg := w.fake.Foregrounds()
	if len(fg) != 1 || fg[0].Line() != "ncdu -x "+sub {
		t.Errorf("foreground: %v", fg)
	}
}

// E17: without ncdu, du's figures sorted, top 20, a partial du noted.
func TestBiggestDirectoriesDu(t *testing.T) {
	w := newWorld(t)
	var out strings.Builder
	for i := range 25 {
		out.WriteString(strings.Repeat("1", 1) + strings.Repeat("0", i%7+1) + "\t/d/" + string(rune('a'+i)) + "\n")
	}
	w.answerErr([]string{"du", "-x", "-B1", "--max-depth=1", w.home}, "5\t/d/small\n2048\t/d/big\n", "du: cannot read\n", 1)
	w.typed("1", "", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  ncdu is not installed — showing a du summary instead.",
		"     2.0KB  /d/big\n        5B  /d/small\n", "  (some entries could not be read)")

	w = newWorld(t)
	w.answer([]string{"du"}, out.String(), 0)
	w.typed("1", "", "q")
	wantCode(t, w.run(), term.ExitOK)
	if n := strings.Count(w.stderr.String(), "  /d/"); n != 20 {
		t.Errorf("listed %d entries", n)
	}

	w = newWorld(t)
	w.answer([]string{"du"}, "", 1)
	w.typed("1", "", "q")
	wantCode(t, w.run(), term.ExitFailure)
	w.wantErr("  du could not size " + w.home + ".")
}

// E17: baobab only on a desktop, only after a yes, detached with its output to /dev/null.
func TestBaobab(t *testing.T) {
	w := newWorld(t)
	w.install("ncdu", "baobab")
	w.answer([]string{"ncdu"}, "", 0)
	w.typed("1", "", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantNoErr("baobab")

	w = newWorld(t)
	w.install("ncdu", "baobab")
	w.answer([]string{"ncdu"}, "", 0)
	w.answer([]string{"setsid", "-f", "baobab"}, "", 0)
	w.env["WAYLAND_DISPLAY"] = "wayland-0"
	w.typed("1", "", "y", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  Also open baobab (graphical analyser) on "+w.home+"? [y/N] ", "  baobab launched in the background.")
	var launched bool
	for _, c := range w.fake.Calls() {
		if c.Line() == "setsid -f baobab "+w.home {
			launched = true
			f, ok := c.Stdout.(*os.File)
			if !ok || f.Name() != w.devNull || c.Stderr != c.Stdout {
				t.Errorf("baobab's output is not /dev/null: %v %v", c.Stdout, c.Stderr)
			}
		}
	}
	if !launched {
		t.Error("baobab not launched")
	}

	w = newWorld(t)
	w.install("ncdu", "baobab")
	w.answer([]string{"ncdu"}, "", 0)
	w.env["DISPLAY"] = ":0"
	w.typed("1", "", "n", "q")
	wantCode(t, w.run(), term.ExitOK)
	if w.ran("setsid") {
		t.Error("baobab launched after no")
	}
}

// E18: the largest regular files, one filesystem, no symlinks followed, ties by path.
func TestBiggestFiles(t *testing.T) {
	w := newWorld(t)
	w.file("a/big", strings.Repeat("x", 3000))
	w.file("a/mid1", strings.Repeat("x", 100))
	w.file("b/mid0", strings.Repeat("x", 100))
	w.file("small", "x")
	if err := os.Symlink(w.home+"/a/big", w.home+"/link"); err != nil {
		t.Fatal(err)
	}
	w.typed("2", "", "0", "3", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("Find big files under which directory? ["+w.home+"] ", "How many to list? [20] ",
		"  Not a positive number: 0",
		"     3.0KB  "+w.home+"/a/big\n      100B  "+w.home+"/a/mid1\n      100B  "+w.home+"/b/mid0\n")
	w.wantNoErr("/small", "/link")
}

// E18: unreadable entries are counted and the first named.
func TestBiggestFilesUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}
	w := newWorld(t)
	w.file("ok", "x")
	locked := w.dir("locked")
	w.file("locked/inner", "x")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(locked, openDir); err != nil {
			t.Error(err)
		}
	})
	w.typed("2", "", "", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  1 entries could not be read (first: "+locked, "1B  "+w.home+"/ok")
}

// E19, E25, B12: the orphan preview as the user, then sudo's reason, autoremove and clean on a
// yes.
func TestPackages(t *testing.T) {
	w := newWorld(t)
	w.install("dnf")
	w.answer([]string{"dnf", "repoquery", "--unneeded"}, "pkg-a\npkg-b\n", 0)
	w.answer([]string{"dnf", "-y", "autoremove"}, "", 0)
	w.answer([]string{"dnf", "clean", "all"}, "", 0)
	w.typed("3", "y", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  Orphaned packages (2):\n    pkg-a\n    pkg-b\n",
		"  Run 'dnf autoremove' + 'dnf clean all' now? [y/N] ", "  Package cleanup complete.")
	want := []string{"root dnf -y autoremove", "root dnf clean all"}
	if got := w.changes(); !slices.Equal(got, want) {
		t.Errorf("changes %v", got)
	}
	calls := w.fake.Calls()
	lines := make([]string, 0, len(calls))
	for _, c := range calls {
		lines = append(lines, c.Line())
	}
	preview, ready := slices.Index(lines, "dnf repoquery --unneeded -q"), slices.Index(lines, "sudo -n -v")
	if preview < 0 || ready < preview {
		t.Errorf("the preview did not run as the user before sudo was readied: %v", lines)
	}

	w = newWorld(t)
	w.install("dnf")
	w.typed("3", "n", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  (no orphaned packages detected in preview)", "  Skipped.")
	if got := w.changes(); len(got) != 0 {
		t.Errorf("changes after no: %v", got)
	}
	if w.ran("sudo") {
		t.Error("sudo asked before a yes")
	}
}

// E19, B3: a failing preview fails the action before asking.
func TestPackagesPreviewFails(t *testing.T) {
	w := newWorld(t)
	w.install("dnf")
	w.answerErr([]string{"dnf", "repoquery"}, "", "Error: repo broken\n", 1)
	w.typed("3", "q")
	wantCode(t, w.run(), term.ExitFailure)
	w.wantErr("  dnf could not list the orphaned packages.", "Error: repo broken")
	w.wantNoErr("now? [y/N]")
}

// E25: no sudo, and sudo refused.
func TestRootUnavailable(t *testing.T) {
	w := newWorld(t)
	w.install("dnf")
	w.uninstall("sudo")
	w.typed("3", "y", "q")
	wantCode(t, w.run(), term.ExitFailure)
	w.wantErr("  This action needs root through sudo, and sudo is not installed.")

	w = newWorld(t)
	w.install("dnf")
	w.sudoRefuses = true
	w.answer([]string{"dnf", "repoquery", "--installonly"}, "kernel-6.1\n", 0)
	w.typed("4", "y", "q")
	wantCode(t, w.run(), term.ExitFailure)
	if w.sudoAsked != 1 {
		t.Errorf("sudo asked %d times", w.sudoAsked)
	}
	if w.ran("sudo -n dnf") {
		t.Error("dnf ran without sudo")
	}
}

// E20: old kernels.
func TestOldKernels(t *testing.T) {
	w := newWorld(t)
	w.install("dnf")
	w.typed("4", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  No superfluous kernels — nothing to remove (keeping newest 2).")
	if !w.ran("dnf repoquery --installonly --latest-limit=-2 -q") || w.ran("sudo") {
		t.Error("preview not run as the user")
	}

	w = newWorld(t)
	w.install("dnf")
	w.answer([]string{"dnf", "repoquery", "--installonly"}, "kernel-6.1\nkernel-core-6.1\n", 0)
	w.answer([]string{"dnf", "-y", "remove"}, "", 0)
	w.typed("4", "yes", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("    kernel-6.1\n    kernel-core-6.1\n", "  Remove 2 old kernel package(s)? [y/N] ", "  Old kernels removed.")
	if got := w.changes(); !slices.Equal(got, []string{"root dnf -y remove kernel-6.1 kernel-core-6.1"}) {
		t.Errorf("changes %v", got)
	}

	w = newWorld(t)
	w.install("dnf")
	w.answer([]string{"dnf", "repoquery", "--installonly"}, "", 1)
	w.typed("4", "q")
	wantCode(t, w.run(), term.ExitFailure)
	w.wantErr("  dnf could not list the installed kernels.")
}

// E21: the journal; root only after the yes.
func TestJournal(t *testing.T) {
	w := newWorld(t)
	w.typed("5", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  journalctl is not available.")

	w = newWorld(t)
	w.install("journalctl")
	w.answer([]string{"journalctl", "--disk-usage"}, "Journals take up 2G.\n", 0)
	w.typed("5", "", "n", "q")
	wantCode(t, w.run(), term.ExitOK)
	if w.ran("sudo") {
		t.Error("sudo asked before a yes")
	}
	w.wantErr("    Journals take up 2G.\n")

	w = newWorld(t)
	w.install("journalctl")
	w.answer([]string{"journalctl", "--vacuum-size=500M"}, "", 1)
	w.typed("5", "", "y", "q")
	wantCode(t, w.run(), term.ExitFailure)
	w.wantErr("  journalctl --vacuum-size reported an error.")
}

// E22: podman shown and pruned on a yes; docker pruned as the user (B4).
func TestContainers(t *testing.T) {
	w := newWorld(t)
	w.typed("6", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  Neither podman nor docker is installed.")

	w = newWorld(t)
	w.install("podman", "docker")
	w.answer([]string{"podman", "system", "df"}, "TYPE TOTAL\n", 0)
	w.answer([]string{"docker", "system", "df"}, "DTYPE\n", 0)
	w.answer([]string{"podman", "system", "prune", "-f"}, "", 0)
	w.answer([]string{"podman", "images", "--format", "json"}, imagesJSON, 0)
	w.answer([]string{"podman", "image", "prune", "-af"}, "", 0)
	w.answer([]string{"docker", "system", "prune", "-f"}, "", 0)
	w.answer([]string{"docker", "image", "prune", "-af"}, "", 0)
	w.typed("6", "y", "y", "y", "y", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  Podman storage usage:\n    TYPE TOTAL\n",
		"  Prune stopped containers, dangling images and unused networks; no volume and no tagged image"+
			" (podman system prune -f)? [y/N] ",
		"  Podman pruned.",
		"  podman image prune -af removes every image no container uses, not only the dangling ones: here 2 tagged"+
			" image(s);",
		"  Among them, 1 claude-yolo project image(s), each rebuilt on that project's next launch:\n"+
			"         3.0GB  localhost/claude-yolo:proj-a\n",
		"  Also remove all 4 unused podman image(s), tagged ones included (podman image prune -af)? [y/N] ",
		"  Unused podman images removed.",
		"  Docker storage usage:\n    DTYPE\n",
		"  Prune stopped containers, dangling images, unused networks and dangling build cache; no volume and no"+
			" tagged image (docker system prune -f)? [y/N] ", "  Docker pruned.",
		"  docker image prune -af removes every image no container uses, not only the dangling ones;"+
			" each is pulled or rebuilt when it is next needed.\n"+
			"  Also remove all unused docker images (docker image prune -af)? [y/N] ",
		"  Unused docker images removed.")
	want := []string{"podman system prune -f", "podman image prune -af", "docker system prune -f", "docker image prune -af"}
	if got := w.changes(); !slices.Equal(got, want) {
		t.Errorf("changes %v", got)
	}
}

// E22: no to each question runs nothing; removing every unused image is asked even after no to
// the prune, and a podman that cannot list its images is not asked it.
func TestContainersDeclined(t *testing.T) {
	w := newWorld(t)
	w.install("podman", "docker")
	w.answer([]string{"podman", "system", "df"}, "TYPE TOTAL\n", 0)
	w.answer([]string{"docker", "system", "df"}, "DTYPE\n", 0)
	w.answer([]string{"podman", "images", "--format", "json"}, imagesJSON, 0)
	w.typed("6", "n", "n", "n", "n", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  Skipped podman system prune -f.", "  Also remove all 4 unused podman image(s)",
		"  Skipped docker system prune -f.", "  Also remove all unused docker images")
	if got := w.changes(); len(got) != 0 {
		t.Errorf("changes %v", got)
	}

	w = newWorld(t)
	w.install("podman")
	w.answer([]string{"podman", "system", "df"}, "TYPE TOTAL\n", 0)
	w.answerErr([]string{"podman", "images", "--format", "json"}, "", "Error: no images\n", 125)
	w.typed("6", "n", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  Removing every unused image is not offered: podman could not list its images" +
		" (podman exited 125: Error: no images).")
	if w.ran("podman image prune") {
		t.Error("offered removing every unused image without the list")
	}

	w = newWorld(t)
	w.install("docker")
	w.answer([]string{"docker", "system", "df"}, "DTYPE\n", 0)
	w.answer([]string{"docker", "image", "prune", "-af"}, "", 1)
	w.typed("6", "n", "y", "q")
	wantCode(t, w.run(), term.ExitFailure)
	w.wantErr("  docker image prune -a reported an error.")
}

// E22: podman wedged is shown with the repair hint and is not a failure; docker unusable skips.
func TestContainersUnreachable(t *testing.T) {
	w := newWorld(t)
	w.install("podman", "docker")
	w.answerErr([]string{"podman", "system", "df"}, "", "Error: stale temp dir in storage\n", 125)
	w.answer([]string{"docker", "system", "df"}, "", 1)
	w.typed("6", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  Cannot query podman — skipping podman prune. Reason:", "    Error: stale temp dir in storage",
		"ansible-playbook CLAUDE/Plan/00062-disk-reclaim-tui/podman-store-repair.yml",
		"  docker is installed but not usable (daemon down, or not in the docker group) — skipping.")
	if w.ran("podman system prune") || w.ran("docker system prune") {
		t.Error("pruned an engine that could not be read")
	}

	if w.ran("podman unshare") {
		t.Error("podman unshare run for a failure that was not a refused permission")
	}

	w = newWorld(t)
	w.install("podman")
	w.answer([]string{"podman", "system", "df"}, "x\n", 0)
	w.answer([]string{"podman", "system", "prune"}, "", 1)
	w.typed("6", "y", "q")
	wantCode(t, w.run(), term.ExitFailure)
	w.wantErr("  podman system prune reported an error.")
}

// E22: a podman system df refused permission is read again under podman unshare, the refusal
// shown, and pruning is offered on what that read; both refused is not offered.
func TestContainersPodmanUnshare(t *testing.T) {
	refused := "Error: open /s/volumes/v/_data: permission denied\n"
	w := newWorld(t)
	w.install("podman")
	w.answerErr([]string{"podman", "system", "df"}, "", refused, 125)
	w.answer([]string{"podman", "unshare", "podman", "system", "df"}, "TYPE TOTAL\n", 0)
	w.answer([]string{"podman", "system", "prune", "-f"}, "", 0)
	w.answer([]string{"podman", "images", "--format", "json"}, "[]", 0)
	w.typed("6", "y", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  podman system df was refused permission; reading it again inside podman's user namespace"+
		" (podman unshare podman system df). Reason:\n    Error: open /s/volumes/v/_data: permission denied\n",
		"  Podman storage usage:\n    TYPE TOTAL\n", "  Podman pruned.")
	if got := w.changes(); !slices.Equal(got, []string{"podman system prune -f"}) {
		t.Errorf("changes %v", got)
	}

	w = newWorld(t)
	w.install("podman")
	w.answerErr([]string{"podman", "system", "df"}, "", refused, 125)
	w.answerErr([]string{"podman", "unshare", "podman", "system", "df"}, "", "Error: still no\n", 125)
	w.typed("6", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  Cannot query podman — skipping podman prune. Reason:", "    Error: still no")
	if w.ran("podman system prune") {
		t.Error("pruned an engine that could not be read")
	}
}

// E23: flatpak preview and removal.
func TestFlatpak(t *testing.T) {
	w := newWorld(t)
	w.typed("7", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  flatpak is not installed.")

	w = newWorld(t)
	w.install("flatpak")
	w.answer([]string{"flatpak", "uninstall", "--unused", "--assumeno"}, "org.Runtime\n", 1)
	w.answer([]string{"flatpak", "uninstall", "--unused", "-y"}, "", 0)
	w.typed("7", "y", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("org.Runtime", "  (nothing unused, or preview unavailable)", "  Unused runtimes removed.")
}

// E24: ~/.cache emptied, the directory kept.
func TestCache(t *testing.T) {
	w := newWorld(t)
	w.typed("8", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  No ~/.cache directory.")

	w = newWorld(t)
	cache := w.dir(".cache")
	w.file(".cache/a/f", "x")
	w.file(".cache/b", "x")
	w.answer([]string{"du", "-B1", "--max-depth=1", cache}, "4096\t"+cache+"/a\n8192\t"+cache+"\n", 0)
	w.typed("8", "n", "8", "y", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  Largest items in ~/.cache:", "     8.0KB  "+cache+"\n", "  Note: applications recreate their caches on next use.",
		"  Delete the CONTENTS of ~/.cache now? [y/N] ", "  ~/.cache cleared.")
	if got := entries(t, cache); len(got) != 0 {
		t.Errorf("left %v", got)
	}
}

// E24: an entry that cannot be removed is named and fails the action; the others still go.
func TestCacheRemovalFails(t *testing.T) {
	w := newWorld(t)
	w.file(".cache/stuck/f", "x")
	w.file(".cache/other", "x")
	stuck := filepath.Join(w.home, ".cache", "stuck")
	w.removeAll = func(p string) error {
		if p == stuck {
			return &os.PathError{Op: "unlinkat", Path: p, Err: os.ErrPermission}
		}
		return os.RemoveAll(p)
	}
	w.answer([]string{"du"}, "4096\t"+stuck+"\n", 0)
	w.typed("8", "y", "q")
	wantCode(t, w.run(), term.ExitFailure)
	w.wantErr("reclaim: could not remove "+stuck, "  Some items in ~/.cache could not be removed.")
	if got := entries(t, filepath.Join(w.home, ".cache")); !slices.Equal(got, []string{"stuck"}) {
		t.Errorf("left %v", got)
	}
}

// E26: trash-cli when installed, else the trash directories themselves.
func TestTrash(t *testing.T) {
	w := newWorld(t)
	w.install("trash-empty", "trash-list")
	w.answer([]string{"trash-list"}, "2026-01-01 /home/u/old\n", 0)
	w.answer([]string{"trash-empty", "-f"}, "", 0)
	w.typed("9", "y", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  Trash contents:", "2026-01-01 /home/u/old", "  Empty the trash now? [y/N] ", "  Trash emptied.")
	if got := w.changes(); !slices.Equal(got, []string{"trash-empty -f"}) {
		t.Errorf("changes %v", got)
	}

	w = newWorld(t)
	w.typed("9", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  No trash directory.")

	w = newWorld(t)
	trash := w.dir(".local/share/Trash")
	w.file(".local/share/Trash/files/a", "x")
	w.file(".local/share/Trash/info/a.trashinfo", "x")
	w.answer([]string{"du", "-s", "-B1", trash}, "8192\t"+trash+"\n", 0)
	w.typed("9", "y", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  Trash size: 8.0KB", "  Empty ~/.local/share/Trash now? [y/N] ", "  Trash emptied.")
	if len(entries(t, trash+"/files")) != 0 || len(entries(t, trash+"/info")) != 0 {
		t.Error("trash not emptied")
	}
}

// E27: the sweep runs its steps in order, each asking, only after its own yes.
func TestSweep(t *testing.T) {
	w := newWorld(t)
	w.install("dnf", "journalctl", "podman", "flatpak", "trash-empty", "trash-list")
	w.answer([]string{"podman", "system", "df"}, "x\n", 0)
	for _, argv := range [][]string{
		{"dnf", "-y", "autoremove"},
		{"dnf", "clean", "all"},
		{"journalctl", "--vacuum-size=500M"},
		{"podman", "system", "prune", "-f"},
		{"flatpak", "uninstall", "--unused", "-y"},
		{"trash-empty", "-f"},
	} {
		w.answer(argv, "", 0)
	}
	w.typed("10", "y", "y", "", "y", "y", "y", "y", "q")
	wantCode(t, w.run(), term.ExitOK)
	want := []string{
		"root dnf -y autoremove", "root dnf clean all", "root journalctl --vacuum-size=500M",
		"podman system prune -f", "flatpak uninstall --unused --assumeno", "flatpak uninstall --unused -y", "trash-empty -f",
	}
	if got := w.changes(); !slices.Equal(got, want) {
		t.Errorf("changes %v", got)
	}
	w.wantErr("    • container prune: stopped containers, dangling images and unused networks, no tagged image"+
		" (if podman present)\n", sweepKeepsTagged, "  Safe sweep finished.")
	if w.ran("podman images") {
		t.Error("the sweep read the image list it never offers")
	}

	w = newWorld(t)
	w.install("dnf")
	w.typed("10", "n", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  Sweep cancelled.")
	if w.ran("sudo") {
		t.Error("ran something after no")
	}
}
