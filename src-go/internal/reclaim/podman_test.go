package reclaim

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	unreadablePath = "/s/volumes/vol-a/_data"
	refusedVolume  = "Error: open " + unreadablePath + ": permission denied\n"
)

// unreadableVolume is a world whose podman system df is refused permission on one volume both
// as the user and under podman unshare, as a volume whose data a uid outside the account's
// subordinate range owns is, and whose listings answer.
func unreadableVolume(t *testing.T) *world {
	t.Helper()
	w := newWorld(t)
	w.install("podman")
	w.answerErr([]string{"podman", "system", "df"}, "", refusedVolume, 125)
	w.answerErr([]string{"podman", "unshare", "podman", "system", "df"}, "", refusedVolume, 125)
	w.answer([]string{"podman", "images"}, "REPOSITORY TAG SIZE\nimg-a latest 1GB\n", 0)
	w.answer([]string{"podman", "ps", "--all", "--size"}, "CONTAINER ID NAMES SIZE\nc1 ctr-a 5MB\n", 0)
	w.answer([]string{"podman", "volume", "ls"}, "DRIVER VOLUME NAME\nlocal vol-a\n", 0)
	w.owners = map[string]int{unreadablePath: 200100}
	w.subuid = "other:100000:65536\n" + testUser + ":300000:65536\n"
	return w
}

// E7a: a podman system df refused permission as the user and in its user namespace is reported
// in part from the listings that do not open a volume, labelled as partial, the unreadable
// volume named with why and how to look at it without changing it; the report still exits 1.
func TestReportPodmanPartial(t *testing.T) {
	w := unreadableVolume(t)
	wantCode(t, w.run("report"), term.ExitFailure)
	w.wantOut("Podman storage:\n  (partial: podman system df could not read every volume, so there are no totals)\n",
		"  Unreadable volume vol-a ("+unreadablePath+"): owned by user ID 200100, outside this account's current"+
			" subordinate ID range (300000-365535) — probably left from an older setup; podman cannot read or size it.\n",
		"  To look at it without changing anything:\n    podman volume inspect vol-a\n    sudo ls -lan "+unreadablePath+
			"\n    grep -E '^("+testUser+"|1000):' /etc/subuid\n",
		"  Images (podman images):\nREPOSITORY TAG SIZE\nimg-a latest 1GB\n",
		"  Containers (podman ps --all --size):\nCONTAINER ID NAMES SIZE\nc1 ctr-a 5MB\n",
		"  Volumes (podman volume ls):\nDRIVER VOLUME NAME\nlocal vol-a\n")
	w.wantErr("reclaim: podman system df: podman exited 125: "+refusedVolume,
		"reclaim: podman unshare podman system df: podman exited 125: "+refusedVolume)
	if got := w.changes(); len(got) != 0 {
		t.Errorf("changes %v", got)
	}
}

// E7a: each reason a volume could not be read is said as found, never guessed.
func TestReportPodmanPartialReasons(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(w *world)
		want string
	}{
		{
			"inside the range", func(w *world) { w.owners[unreadablePath] = 300100 },
			"owned by user ID 300100, inside this account's subordinate ID range, yet podman could not read it in its user namespace either.",
		},
		{
			"this account", func(w *world) { w.owners[unreadablePath] = testUID },
			"owned by this account (user ID 1000), yet podman could not read it.",
		},
		{
			"no range", func(w *world) { w.subuid = "other:100000:65536\n" },
			"owned by user ID 200100, and this account has no subordinate ID range in /etc/subuid; podman cannot read or size it.",
		},
		{
			"range by uid", func(w *world) { w.subuid = "1000:200000:65536\n" },
			"owned by user ID 200100, inside this account's subordinate ID range",
		},
		{
			"owner unreadable", func(w *world) { w.owners = nil },
			"its owner could not be read (lstat " + unreadablePath + ": permission denied).",
		},
		{
			"subuid unreadable", func(w *world) { w.subuidErr = errors.New("no such file") },
			"owned by user ID 200100; whether that is in this account's subordinate ID range is unknown," +
				" since /etc/subuid could not be read (no such file).",
		},
		{
			"subuid malformed", func(w *world) { w.subuid = testUser + ":x:65536\n" },
			"owned by user ID 200100; whether that is in this account's subordinate ID range is unknown," +
				` since /etc/subuid could not be read (line 1: "` + testUser + `:x:65536" is not name:start:count).`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := unreadableVolume(t)
			tc.set(w)
			wantCode(t, w.run("report"), term.ExitFailure)
			w.wantOut("  Unreadable volume vol-a (" + unreadablePath + "): " + tc.want)
		})
	}
}

// E7a: a refusal that names no volume is still reported in part, and says it names none; a
// listing that fails is named and marked, the others still shown.
func TestReportPodmanPartialGaps(t *testing.T) {
	w := unreadableVolume(t)
	w.answerErr([]string{"podman", "system", "df"}, "", "Error: permission denied\n", 125)
	w.answerErr([]string{"podman", "unshare", "podman", "system", "df"}, "", "Error: permission denied\n", 125)
	w.answerErr([]string{"podman", "ps", "--all", "--size"}, "", "Error: ps broke\n", 125)
	wantCode(t, w.run("report"), term.ExitFailure)
	w.wantOut("  podman's refusal names no volume, so which one it could not read is not known.\n",
		"  Images (podman images):\nREPOSITORY",
		"  Containers (podman ps --all --size):\n  (unavailable)\n", "  Volumes (podman volume ls):\nDRIVER")
	w.wantErr("reclaim: podman ps --all --size: podman exited 125: Error: ps broke\n")
}

// E22a: option 6 with a volume podman cannot read shows the storage in part, says why
// podman system prune is not offered, and offers only the image prune; yes runs it, no runs
// nothing.
func TestContainersPodmanPartial(t *testing.T) {
	w := unreadableVolume(t)
	w.answer([]string{"podman", "image", "prune", "-af"}, "", 0)
	w.typed("6", "y", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  Cannot query podman fully: podman system df could not read every volume. Reason:\n    "+refusedVolume,
		"  Unreadable volume vol-a ("+unreadablePath+"): owned by user ID 200100, outside",
		"    podman volume inspect vol-a\n",
		"  Podman storage, in part (no totals):\n",
		"  Images (podman images):\n    REPOSITORY TAG SIZE\n    img-a latest 1GB\n",
		"  Containers (podman ps --all --size):\n    CONTAINER ID NAMES SIZE\n",
		"  Volumes (podman volume ls):\n    DRIVER VOLUME NAME\n",
		"  podman system prune is not offered: podman could not read all of its storage, and the prune"+
			" removes stopped containers, which may be what still holds that volume.\n",
		"  Prune unused podman images (podman image prune -af)? [y/N] ", "  Podman images pruned.")
	if got := w.changes(); !slices.Equal(got, []string{"podman image prune -af"}) {
		t.Errorf("changes %v", got)
	}

	w = unreadableVolume(t)
	w.typed("6", "n", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  Skipped podman.")
	if got := w.changes(); len(got) != 0 {
		t.Errorf("changes %v", got)
	}

	w = unreadableVolume(t)
	w.answer([]string{"podman", "image", "prune", "-af"}, "", 1)
	w.typed("6", "y", "q")
	wantCode(t, w.run(), term.ExitFailure)
	w.wantErr("  podman image prune reported an error.")
}

// E22a: when the image list cannot be read either, nothing is offered.
func TestContainersPodmanPartialNoImages(t *testing.T) {
	w := unreadableVolume(t)
	w.answerErr([]string{"podman", "images"}, "", "Error: no images\n", 125)
	w.typed("6", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  Images (podman images):\n    (unavailable: podman exited 125: Error: no images)\n",
		"  No podman prune is offered: podman could not list its images either.")
	if w.ran("podman image prune") || w.ran("podman system prune") {
		t.Error("pruned an engine that could not be read")
	}
}

// E7a: an account that cannot be named is said, and the subuid lookup it would need is not
// suggested.
func TestReportPodmanPartialNoAccount(t *testing.T) {
	w := unreadableVolume(t)
	w.accountErr = errors.New("no passwd entry")
	wantCode(t, w.run("report"), term.ExitFailure)
	w.wantOut("  Unreadable volume vol-a (" + unreadablePath + "): this account could not be named (no passwd entry).\n" +
		"  To look at it without changing anything:\n    podman volume inspect vol-a\n    sudo ls -lan " + unreadablePath + "\n  Images")
}

// The production owner and account are this process's.
func TestOwnerAndAccount(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if uid, err := ownerUID(p); err != nil || uid != os.Getuid() {
		t.Errorf("ownerUID: %d, %v", uid, err)
	}
	if _, err := ownerUID(p + "-missing"); err == nil {
		t.Error("ownerUID of a missing path succeeded")
	}
	if _, uid, err := account(); err != nil || uid != os.Getuid() {
		t.Errorf("account: %d, %v", uid, err)
	}
}
