package reclaim

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
	w.answer([]string{"podman", "images", "--format", "json"}, imagesJSON, 0)
	w.answer([]string{"podman", "ps", "--all", "--size", "--format", "json"}, containersJSON, 0)
	w.answer([]string{"podman", "volume", "ls", "--format", "json"}, volumesJSON, 0)
	w.owners = map[string]int{unreadablePath: 200100}
	w.subuid = "other:100000:65536\n" + testUser + ":300000:65536\n"
	return w
}

// imagesJSON is two claude-yolo project images (one used by a container), another tagged image
// no container uses, and two dangling ones (one marked so, one only nameless).
const imagesJSON = `[
 {"Id":"aaaaaaaaaaaaaaaaaaaa","Names":["localhost/claude-yolo:proj-a"],"Size":3221225472,"Containers":0},
 {"Id":"bbbbbbbbbbbbbbbbbbbb","Names":["localhost/claude-yolo:proj-b"],"Size":2147483648,"Containers":1},
 {"Id":"cccccccccccccccccccc","Names":["quay.io/example/tool:1"],"Size":536870912,"Containers":0},
 {"Id":"dddddddddddddddddddd","Size":104857600,"Containers":0,"Dangling":true},
 {"Id":"sha256:eeeeeeeeeeeeeeeeeeee","Names":[],"Size":104857600,"Containers":0}
]`

const containersJSON = `[
 {"Names":["ctr-b"],"State":"exited","Size":{"rootFsSize":10,"rwSize":1024}},
 {"Names":["ctr-a"],"State":"running","Size":{"rootFsSize":2147483648,"rwSize":5242880}}
]`

const volumesJSON = `[{"Name":"vol-l"},{"Name":"vol-b"},{"Name":"vol-a"},{"Name":"vol-c"},{"Name":"vol-d"},
 {"Name":"vol-e"},{"Name":"vol-f"},{"Name":"vol-g"},{"Name":"vol-h"},{"Name":"vol-i"},{"Name":"vol-j"},{"Name":"vol-k"}]`

// storageSummary is E7a's summary of imagesJSON, containersJSON and volumesJSON.
var storageSummary = []string{
	"  Images (podman images --format json): 5, total size not known: podman system df could not run, and images share layers\n" +
		"    Dangling (<none>): 2\n    Largest 5:\n" +
		"         3.0GB  localhost/claude-yolo:proj-a\n         2.0GB  localhost/claude-yolo:proj-b\n" +
		"         512MB  quay.io/example/tool:1\n         100MB  <none> dddddddddddd\n         100MB  <none> eeeeeeeeeeee\n",
	"  Containers (podman ps --all --size --format json): 2\n    Largest 2 by writable layer:\n" +
		"         5.0MB  ctr-a (running)\n         1.0KB  ctr-b (exited)\n",
	"  Volumes (podman volume ls --format json): 12, not sized, since sizing a volume opens its data\n" +
		"    First 10 by name:\n      vol-a\n      vol-b\n",
	"      vol-j\n    and 2 more\n",
}

// E7a: a podman system df refused permission as the user and in its user namespace is reported
// in part from a summary of the listings that do not open a volume, labelled as partial, the
// unreadable volume named with why and how to look at it without changing it; the report still
// exits 1.
func TestReportPodmanPartial(t *testing.T) {
	w := unreadableVolume(t)
	wantCode(t, w.run("report"), term.ExitFailure)
	w.wantOut("Podman storage:\n  (partial: podman system df could not read every volume, so there are no totals)\n",
		"  Unreadable volume vol-a ("+unreadablePath+"): owned by user ID 200100, outside this account's current"+
			" subordinate ID range (300000-365535) — probably left from an older setup; podman cannot read or size it.\n",
		"  To look at it without changing anything:\n    podman volume inspect vol-a\n    sudo ls -lan "+unreadablePath+
			"\n    grep -E '^("+testUser+"|1000):' /etc/subuid\n")
	w.wantOut(storageSummary...)
	if strings.Contains(w.stdout.String(), "vol-k") {
		t.Errorf("listed more than 10 volumes:\n%s", w.stdout.String())
	}
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
	w.answer([]string{"podman", "volume", "ls"}, "not json\n", 0)
	wantCode(t, w.run("report"), term.ExitFailure)
	w.wantOut("  podman's refusal names no volume, so which one it could not read is not known.\n",
		storageSummary[0],
		"  Containers (podman ps --all --size --format json): unavailable\n",
		"  Volumes (podman volume ls --format json): unavailable\n")
	w.wantErr("reclaim: podman ps --all --size --format json: podman exited 125: Error: ps broke\n",
		"reclaim: podman volume ls --format json: its output is not a JSON list: ")
}

// E7a: no dangling image, no container and no volume is said as such.
func TestReportPodmanPartialEmpty(t *testing.T) {
	w := unreadableVolume(t)
	w.answer([]string{"podman", "images"}, `[{"Id":"x","Names":["a:1"],"Size":1024,"Containers":0}]`, 0)
	w.answer([]string{"podman", "ps"}, "[]", 0)
	w.answer([]string{"podman", "volume", "ls"}, "[]\n", 0)
	wantCode(t, w.run("report"), term.ExitFailure)
	w.wantOut("  Images (podman images --format json): 1, total size not known: podman system df could not run, and images share layers\n"+
		"    Dangling (<none>): 0\n    Largest 1:\n         1.0KB  a:1\n",
		"  Containers (podman ps --all --size --format json): 0\n  Volumes (podman volume ls --format json): 0\n")
}

// E22a: option 6 with a volume podman cannot read shows the storage summary in part, says why
// podman system prune is not offered, and offers the dangling-image prune with what it frees;
// removing every unused image is a second question, naming the tagged images and listing the
// claude-yolo project images it would remove, default no.
func TestContainersPodmanPartial(t *testing.T) {
	w := unreadableVolume(t)
	w.answer([]string{"podman", "image", "prune"}, "", 0)
	w.typed("6", "y", "y", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  Cannot query podman fully: podman system df could not read every volume. Reason:\n    "+refusedVolume,
		"  Unreadable volume vol-a ("+unreadablePath+"): owned by user ID 200100, outside",
		"    podman volume inspect vol-a\n",
		"  Podman storage, in part (no totals):\n")
	w.wantErr(storageSummary...)
	w.wantErr("  podman system prune is not offered: podman could not read all of its storage, and the prune"+
		" removes stopped containers, which may be what still holds that volume.\n",
		"  Prune the 2 dangling (<none>) podman image(s) no container uses (podman image prune -f)? [y/N] ",
		"  Dangling podman images pruned.\n",
		"  podman image prune -af removes every image no container uses, not only the dangling ones: here 2 tagged"+
			" image(s); each is pulled or rebuilt when it is next needed.\n"+
			"  Among them, 1 claude-yolo project image(s), each rebuilt on that project's next launch:\n"+
			"         3.0GB  localhost/claude-yolo:proj-a\n"+
			"  and 1 other tagged image(s).\n"+
			"  Also remove all 2 unused podman image(s), tagged ones included (podman image prune -af)? [y/N] ",
		"  Unused podman images removed.")
	if got := w.changes(); !slices.Equal(got, []string{"podman image prune -f", "podman image prune -af"}) {
		t.Errorf("changes %v", got)
	}

	w = unreadableVolume(t)
	w.typed("6", "n", "n", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  Skipped podman image prune -f.", "  Also remove all 4 unused podman image(s)", "  Kept the unused images.")
	if got := w.changes(); len(got) != 0 {
		t.Errorf("changes %v", got)
	}

	w = unreadableVolume(t)
	w.answer([]string{"podman", "image", "prune"}, "", 1)
	w.typed("6", "y", "q")
	wantCode(t, w.run(), term.ExitFailure)
	w.wantErr("  podman image prune reported an error.")
	if w.ran("podman image prune -af") {
		t.Error("offered the second prune after the first failed")
	}

	w = unreadableVolume(t)
	w.answer([]string{"podman", "image", "prune", "-af"}, "", 1)
	w.typed("6", "n", "y", "q")
	wantCode(t, w.run(), term.ExitFailure)
	w.wantErr("  podman image prune -a reported an error.")
}

// E22a: with no dangling image there is no first question, and with no unused tagged image no
// second.
func TestContainersPodmanPartialNothingToPrune(t *testing.T) {
	w := unreadableVolume(t)
	w.answer([]string{"podman", "images"}, `[{"Id":"x","Names":["a:1"],"Size":1024,"Containers":2},`+
		`{"Id":"y","Size":1024,"Containers":1}]`, 0)
	w.typed("6", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  1 dangling podman image(s) are used by a container and are kept by the prune.\n",
		"  No dangling podman image to prune.", "  No other podman image is unused: nothing more to remove.")
	if got := w.changes(); len(got) != 0 {
		t.Errorf("changes %v", got)
	}
}

// E7a, E22a: a dangling image a container uses is counted apart in the report, and the prune
// question says it is kept, so the two counts agree.
func TestPodmanDanglingUsedByContainer(t *testing.T) {
	w := unreadableVolume(t)
	w.answer([]string{"podman", "images"}, `[{"Id":"x","Size":1024,"Containers":0},`+
		`{"Id":"y","Size":1024,"Containers":1},{"Id":"z","Size":1024,"Containers":3}]`, 0)
	w.answer([]string{"podman", "image", "prune"}, "", 0)
	w.typed("6", "n", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("    Dangling (<none>): 3, 2 of them used by a container and kept by the prune\n",
		"  2 dangling podman image(s) are used by a container and are kept by the prune.\n",
		"  Prune the 1 dangling (<none>) podman image(s) no container uses (podman image prune -f)? [y/N] ")
}

// E22a: when the image list cannot be read either, nothing is offered.
func TestContainersPodmanPartialNoImages(t *testing.T) {
	w := unreadableVolume(t)
	w.answerErr([]string{"podman", "images"}, "", "Error: no images\n", 125)
	w.typed("6", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  Images (podman images --format json): unavailable (podman exited 125: Error: no images)\n",
		"  No podman prune is offered: podman could not list its images either.")
	if w.ran("podman image prune") || w.ran("podman system prune") {
		t.Error("pruned an engine that could not be read")
	}
}

// E27: the safe sweep offers only the dangling-image prune, and says removing every unused
// image is not part of it.
func TestSweepPodmanPartial(t *testing.T) {
	w := unreadableVolume(t)
	w.install("dnf")
	w.answer([]string{"podman", "image", "prune"}, "", 0)
	w.typed("10", "y", "n", "y", "q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("  Dangling podman images pruned.\n" + sweepKeepsTagged)
	w.wantNoErr("prune -af")
	if got := w.changes(); !slices.Equal(got, []string{"podman image prune -f"}) {
		t.Errorf("changes %v", got)
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
