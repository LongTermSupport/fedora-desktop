package containerimages

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// now is the fixed time every test runs at.
var now = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

func id(c string) string { return strings.Repeat(c, 64) }

// img is one entry of `podman images --format json`; podman's keys match these names in any
// case (internal/engine's tests hold the real shape).
type img struct {
	ID         string
	Names      []string
	Size       int64
	Labels     map[string]string
	Containers int
	Created    int64
}

// ctr is one entry of `podman ps --all --external --format json`.
type ctr struct {
	ID      string
	Names   []string
	ImageID string
	Created string
	State   string
	Command []string
}

// inspected is the part of one entry of `podman image inspect --format json` that is read.
type inspected struct {
	ID     string
	RootFS rootFS
}

type rootFS struct{ Layers []string }

func ago(d time.Duration) int64      { return now.Add(-d).Unix() }
func agoText(d time.Duration) string { return now.Add(-d).Format(time.RFC3339Nano) }
func days(n int) time.Duration       { return time.Duration(n) * 24 * time.Hour }
func listJSON[T any](t *testing.T, v []T) []byte {
	t.Helper()
	if v == nil {
		return []byte("[]\n")
	}
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

type world struct {
	t        *testing.T
	fake     *execx.Fake
	home     string
	runtime  string
	out, err bytes.Buffer
	// dangling is each listing of the dangling images in turn; the last repeats.
	dangling [][]img
	images   []img
	ctrs     []ctr
	volumes  string
	// layers is each image's RootFS layers by id, for `podman image inspect`; an image not
	// named here stands on one layer of its own.
	layers   map[string][]string
	scripted bool
	// sticky is a podman whose removals report success and leave the thing there.
	sticky bool
	// gone is each image id, name and container id podman no longer has.
	gone map[string]bool
}

func newWorld(t *testing.T) *world {
	t.Helper()
	dir := t.TempDir()
	w := &world{t: t, fake: &execx.Fake{}, home: filepath.Join(dir, "home"), runtime: filepath.Join(dir, "run")}
	for _, d := range []string{w.home, w.runtime} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	markStoreClean(t, w.home)
	return w
}

// storeCleanPath is the marker play-podman.yml writes when root's probe finds no store file
// owned outside the user's subuid range (P9).
func storeCleanPath(home string) string {
	return filepath.Join(home, ".local", "state", "fedora-desktop", "container-images.store-clean")
}

// markStoreClean writes the marker as the play does; every world and shelf starts with it.
func markStoreClean(t *testing.T, home string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(storeCleanPath(home)), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storeCleanPath(home), []byte("2026-10-28T11:00:00Z\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// unconfirmStore removes the marker, as the play does when its probe finds such a file.
func unconfirmStore(t *testing.T, home string) {
	t.Helper()
	if err := os.Remove(storeCleanPath(home)); err != nil {
		t.Fatal(err)
	}
}

const unconfirmedSays = "the podman store has not been confirmed free of old-range files (Plan 00235 Q6)"

func (w *world) script() {
	if w.scripted {
		return
	}
	w.scripted = true
	gone := map[string]bool{}
	w.gone = gone
	listings := 0
	w.fake.OnFunc([]string{"podman", "images", "--filter", "dangling=true", "--format", "json"}, func(execx.Cmd) (execx.Result, error) {
		i := min(listings, len(w.dangling)-1)
		listings++
		var left []img
		if i >= 0 {
			left = slices.DeleteFunc(slices.Clone(w.dangling[i]), func(m img) bool { return gone[m.ID] })
		}
		return execx.Result{Stdout: listJSON(w.t, left)}, nil
	})
	w.fake.OnFunc([]string{"podman", "images", "--format", "json"}, func(execx.Cmd) (execx.Result, error) {
		var left []img
		for _, m := range w.images {
			named := len(m.Names) > 0
			m.Names = slices.DeleteFunc(slices.Clone(m.Names), func(n string) bool { return gone[n] })
			if (len(m.Names) > 0 || !named) && !gone[m.ID] {
				left = append(left, m)
			}
		}
		return execx.Result{Stdout: listJSON(w.t, left)}, nil
	})
	w.fake.OnFunc([]string{"podman", "ps", "--all", "--external", "--format", "json"}, func(execx.Cmd) (execx.Result, error) {
		left := slices.DeleteFunc(slices.Clone(w.ctrs), func(c ctr) bool { return gone[c.ID] })
		return execx.Result{Stdout: listJSON(w.t, left)}, nil
	})
	w.fake.On([]string{"podman", "volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}, execx.Result{Stdout: []byte(w.volumes)})
	w.fake.OnFunc([]string{"podman", "image", "inspect", "--format", "json"}, func(c execx.Cmd) (execx.Result, error) {
		out := make([]inspected, 0, len(c.Args)-4)
		for _, ref := range c.Args[4:] {
			layers, ok := w.layers[ref]
			if !ok {
				layers = []string{"sha256:own-" + ref}
			}
			out = append(out, inspected{ID: ref, RootFS: rootFS{Layers: layers}})
		}
		return execx.Result{Stdout: listJSON(w.t, out)}, nil
	})
	removal := func(c execx.Cmd) (execx.Result, error) {
		if !w.sticky {
			gone[c.Args[len(c.Args)-1]] = true
		}
		return execx.Result{}, nil
	}
	w.fake.OnFunc([]string{"podman", "image", "rm"}, removal)
	w.fake.OnFunc([]string{"podman", "rm", "--force"}, removal)
	exists := func(c execx.Cmd) (execx.Result, error) {
		if gone[c.Args[len(c.Args)-1]] {
			return execx.Result{ExitCode: 1}, nil
		}
		return execx.Result{}, nil
	}
	w.fake.OnFunc([]string{"podman", "image", "exists"}, exists)
	w.fake.OnFunc([]string{"podman", "container", "exists"}, exists)
}

func (w *world) run(args ...string) term.ExitCode {
	w.t.Helper()
	w.script()
	app := App{Env: Env{
		Runner: w.fake,
		Getenv: func(k string) string {
			if k == "XDG_RUNTIME_DIR" {
				return w.runtime
			}
			return ""
		},
		LookPath: func(string) (string, error) { return "/usr/bin/podman", nil },
		Now:      func() time.Time { return now },
		Home:     w.home,
		UID:      4242,
		Stdout:   &w.out,
		Stderr:   &w.err,
	}}
	return app.Run(context.Background(), args)
}

func (w *world) removed() []string {
	var got []string
	for _, c := range w.fake.Calls() {
		line := c.Line()
		if strings.HasPrefix(line, "podman image rm ") || strings.HasPrefix(line, "podman rm --force ") {
			got = append(got, line)
		}
	}
	return got
}

func (w *world) logText() string {
	w.t.Helper()
	text, err := os.ReadFile(filepath.Join(w.home, ".local", "state", "fedora-desktop", "container-images.log"))
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		w.t.Fatal(err)
	}
	return string(text)
}

// The backlog: an old unused dangling image, a young one, one a stopped container still uses,
// and one podman itself counts a container for.
func backlog(w *world) {
	w.dangling = [][]img{{
		{ID: id("a"), Size: 2 << 30, Labels: map[string]string{"claude-yolo-version": "1.2.3"}, Created: ago(days(40))},
		{ID: id("b"), Size: 1 << 20, Created: ago(2 * time.Hour)},
		{ID: id("c"), Size: 1 << 20, Created: ago(days(9))},
		{ID: id("d"), Size: 1 << 20, Containers: 1, Created: ago(days(9))},
	}, nil}
	w.ctrs = []ctr{{ID: "s1", Names: []string{"project-a"}, ImageID: id("c"), Created: agoText(days(3)), State: "exited", Command: []string{"claude"}}}
}

func TestADryRunListsWhatWouldGoAndRemovesNothing(t *testing.T) {
	w := newWorld(t)
	backlog(w)
	if code := w.run("prune"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if got := w.removed(); len(got) != 0 {
		t.Fatalf("a dry run removed %v", got)
	}
	out := w.out.String()
	for _, want := range []string{
		"a dry run",
		"would remove image aaaaaaaaaaaa (2.0 GiB): dangling (no tag), used by no container, created 2026-09-18",
		"keep image bbbbbbbbbbbb: created 2h0m0s ago, under a day: a build may still be using it",
		"keep image cccccccccccc: used by container project-a",
		"keep image dddddddddddd: podman counts 1 container using it",
		"prune --yes",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the dry run does not say %q:\n%s", want, out)
		}
	}
	if w.logText() != "" {
		t.Fatalf("a dry run wrote the log: %s", w.logText())
	}
}

func TestYesRemovesTheUnusedOldDanglingImagesAndLogsEachAfterItWent(t *testing.T) {
	w := newWorld(t)
	backlog(w)
	if code := w.run("prune", "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if got := w.removed(); len(got) != 1 || got[0] != "podman image rm "+id("a") {
		t.Fatalf("removed %v, want only the old unused dangling image", got)
	}
	log := w.logText()
	fields := strings.Split(strings.TrimSuffix(log, "\n"), "\t")
	if strings.Count(log, "\n") != 1 || len(fields) != 7 {
		t.Fatalf("log is not one line of seven fields: %q", log)
	}
	want := []string{
		"2026-10-28T12:00:00Z", "image", "aaaaaaaaaaaa", id("a"), "2147483648", "claude-yolo-version=1.2.3",
		"dangling (no tag), used by no container, created 2026-09-18",
	}
	for i := range want {
		if fields[i] != want[i] {
			t.Errorf("log field %d = %q, want %q", i, fields[i], want[i])
		}
	}
	if !strings.Contains(w.out.String(), "removed image aaaaaaaaaaaa (2.0 GiB)") ||
		!strings.Contains(w.out.String(), "freed about 2.0 GiB") {
		t.Errorf("the run does not report the removal:\n%s", w.out.String())
	}
}

// Removing a dangling image can leave its parent dangling: the run looks again until a pass
// removes nothing, so the deploy's one run clears the whole backlog.
func TestYesLooksAgainUntilAPassRemovesNothing(t *testing.T) {
	w := newWorld(t)
	w.dangling = [][]img{
		{{ID: id("a"), Size: 10, Created: ago(days(5))}},
		{{ID: id("e"), Size: 20, Created: ago(days(6))}},
		nil,
	}
	if code := w.run("prune", "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if got := w.removed(); len(got) != 2 || got[1] != "podman image rm "+id("e") {
		t.Fatalf("removed %v, want the image and then its parent", got)
	}
}

// A dangling image that keeps coming back (a removal podman reports done but that did not
// happen) cannot hold the run in a loop: the passes are bounded and the run fails, saying so.
func TestThePassesAreBounded(t *testing.T) {
	w := newWorld(t)
	w.dangling = [][]img{{{ID: id("a"), Size: 10, Created: ago(days(5))}}}
	w.sticky = true
	if code := w.run("prune", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d, want failure", code)
	}
	if got := len(w.removed()); got != maxPasses {
		t.Fatalf("%d removals, want %d", got, maxPasses)
	}
	if !strings.Contains(w.err.String(), "still finds images to remove") {
		t.Fatalf("stderr: %s", w.err.String())
	}
}

// P2: four weeks unused and an image goes, a project image of ccy's or a pulled one; ccy's
// :base and :latest never, and an image built here that nothing rebuilds is left for a person.
func TestAnImageUnusedForFourWeeksIsRemovedAndBaseAndLatestNeverAre(t *testing.T) {
	w := newWorld(t)
	w.images = []img{
		{ID: id("1"), Names: []string{"localhost/claude-yolo:latest"}, Size: 5, Created: ago(days(90))},
		{ID: id("2"), Names: []string{"localhost/claude-yolo:base"}, Size: 5, Created: ago(days(90))},
		{ID: id("3"), Names: []string{"localhost/claude-yolo:project-b"}, Size: 3 << 30, Created: ago(days(29))},
		{ID: id("4"), Names: []string{"localhost/claude-yolo:project-c"}, Size: 5, Created: ago(days(27))},
		{ID: id("5"), Names: []string{"localhost/claude-yolo:project-d"}, Size: 5, Created: ago(days(60))},
		{ID: id("6"), Names: []string{"localhost/claude-yolo:project-e", "localhost/other:1"}, Size: 7, Created: ago(days(60))},
		{ID: id("7"), Names: []string{"quay.io/example/tool:1", "quay.io/example/tool:latest"}, Size: 9, Created: ago(days(400))},
		{ID: id("8"), Names: []string{"localhost/devtools:latest"}, Size: 5, Created: ago(days(400))},
		{ID: id("9"), Names: []string{"docker.io/library/postgres:16"}, Size: 5, Created: ago(days(20))},
	}
	w.ctrs = []ctr{{ID: "s1", Names: []string{"project-d"}, ImageID: id("5"), Created: agoText(days(50)), State: "exited"}}
	if code := w.run("prune", "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	got := w.removed()
	want := []string{
		"podman image rm localhost/claude-yolo:project-b", "podman image rm localhost/claude-yolo:project-e",
		"podman image rm quay.io/example/tool:1", "podman image rm quay.io/example/tool:latest",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("removed %v, want %v", got, want)
	}
	log := w.logText()
	for _, line := range []string{
		"tag\tlocalhost/claude-yolo:project-b\t" + id("3") + "\t3221225472\t",
		"tag\tlocalhost/claude-yolo:project-e\t" + id("6") + "\t0\t",
		"tag\tquay.io/example/tool:1\t" + id("7") + "\t0\t",
		"tag\tquay.io/example/tool:latest\t" + id("7") + "\t9\t",
	} {
		if !strings.Contains(log, line) {
			t.Errorf("log does not hold %q:\n%s", line, log)
		}
	}
	out := w.out.String()
	for _, want := range []string{
		"keep tag localhost/claude-yolo:project-c: created 27 days ago, under 28",
		"keep tag localhost/claude-yolo:project-d: used by container project-d",
		"untagged localhost/claude-yolo:project-e: it keeps its other names",
		"keep tag localhost/other:1: built on this host, and nothing here builds it again: left for a person",
		"keep tag localhost/devtools:latest: built on this host, and nothing here builds it again: left for a person",
		"keep tag docker.io/library/postgres:16: created 20 days ago, under 28",
		"removed tag quay.io/example/tool:latest (9 B): an image used by no container and with none built on it, created 400 days ago; a pull fetches it again",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not say %q:\n%s", want, out)
		}
	}
}

// podman refuses to remove an image another is built on, so an old unused base image whose
// layers start another image's (a pulled php:8 under a project's own build) is kept, and so is
// one whose layers podman did not report.
func TestAnImageAnotherIsBuiltOnIsKept(t *testing.T) {
	w := newWorld(t)
	w.images = []img{
		{ID: id("7"), Names: []string{"docker.io/library/php:8"}, Size: 5, Created: ago(days(90))},
		{ID: id("8"), Names: []string{"quay.io/example/gone:1"}, Size: 5, Created: ago(days(90))},
	}
	w.dangling = [][]img{{{ID: id("c"), Size: 5, Created: ago(2 * time.Hour)}}}
	w.layers = map[string][]string{id("7"): {"sha256:l1", "sha256:l2"}, id("c"): {"sha256:l1", "sha256:l2", "sha256:l3"}, id("8"): nil}
	if code := w.run("prune", "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if got := w.removed(); len(got) != 0 {
		t.Fatalf("removed %v", got)
	}
	for _, want := range []string{
		"keep tag docker.io/library/php:8: image cccccccccccc is built on it",
		"keep tag quay.io/example/gone:1: podman did not report its layers",
	} {
		if !strings.Contains(w.out.String(), want) {
			t.Errorf("output does not say %q:\n%s", want, w.out.String())
		}
	}
}

// The layers are asked only when there is a tagged image; an inspect that fails stops the run.
func TestAnInspectThatFailsStopsTheRunBeforeAnyRemoval(t *testing.T) {
	w := newWorld(t)
	backlog(w)
	w.images = []img{{ID: id("7"), Names: []string{"quay.io/example/tool:1"}, Size: 5, Created: ago(days(90))}}
	w.script()
	w.fake.On([]string{"podman", "image", "inspect"}, execx.Result{ExitCode: 125, Stderr: []byte("Error: boom\n")})
	if code := w.run("prune", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d, want failure", code)
	}
	if len(w.removed()) != 0 || !strings.Contains(w.err.String(), "boom") {
		t.Fatalf("removed %v; stderr %s", w.removed(), w.err.String())
	}
}

func TestAnOldBuildahWorkingContainerIsRemovedWhileNoBuildRuns(t *testing.T) {
	w := newWorld(t)
	w.ctrs = []ctr{
		{ID: "b1", Names: []string{"working-a"}, ImageID: id("f"), Created: agoText(days(2)), State: "storage", Command: []string{"buildah"}},
		{ID: "b2", Names: []string{"working-b"}, ImageID: id("f"), Created: agoText(time.Hour), State: "storage", Command: []string{"buildah"}},
	}
	if code := w.run("prune", "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if got := w.removed(); len(got) != 1 || got[0] != "podman rm --force b1" {
		t.Fatalf("removed %v, want the old working container only", got)
	}
	if !strings.Contains(w.logText(), "container\tworking-a\tb1\t") {
		t.Fatalf("log: %s", w.logText())
	}
}

func TestNoBuildahContainerIsTouchedWhileACcyImageBuildHoldsItsLock(t *testing.T) {
	w := newWorld(t)
	w.ctrs = []ctr{{ID: "b1", Names: []string{"working-a"}, Created: agoText(days(2)), State: "storage", Command: []string{"buildah"}}}
	dir := filepath.Join(w.runtime, "ccy", "image-builds")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "claude-yolo_idle.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	held, err := os.OpenFile(filepath.Clean(filepath.Join(dir, "claude-yolo_latest.lock")), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := held.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := syscall.Flock(int(held.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	if code := w.run("prune", "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if got := w.removed(); len(got) != 0 {
		t.Fatalf("removed %v while a build ran", got)
	}
	if !strings.Contains(w.out.String(), "keep container working-a: a ccy image build is running (claude-yolo_latest.lock is held)") {
		t.Fatalf("output: %s", w.out.String())
	}
}

func TestALockThatIsNotAFileStopsTheRun(t *testing.T) {
	w := newWorld(t)
	dir := filepath.Join(w.runtime, "ccy", "image-builds")
	if err := os.MkdirAll(filepath.Join(dir, "odd.lock"), 0o700); err != nil {
		t.Fatal(err)
	}
	if code := w.run("prune", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d, want failure", code)
	}
	if len(w.removed()) != 0 || !strings.Contains(w.err.String(), "odd.lock") {
		t.Fatalf("removed %v; stderr %s", w.removed(), w.err.String())
	}
}

func TestAFailedRemovalIsNamedNotLoggedAndTheRunFails(t *testing.T) {
	w := newWorld(t)
	w.dangling = [][]img{{
		{ID: id("a"), Size: 10, Created: ago(days(5))},
		{ID: id("e"), Size: 20, Created: ago(days(5))},
	}, nil}
	w.script()
	w.fake.On([]string{"podman", "image", "rm", id("a")}, execx.Result{ExitCode: 2, Stderr: []byte("Error: image is in use by a container\n")})
	if code := w.run("prune", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d, want failure", code)
	}
	if !strings.Contains(w.err.String(), "could not remove image aaaaaaaaaaaa") ||
		!strings.Contains(w.err.String(), "image is in use by a container") {
		t.Fatalf("stderr: %s", w.err.String())
	}
	log := w.logText()
	if strings.Contains(log, id("a")) || !strings.Contains(log, id("e")) {
		t.Fatalf("log: %s", log)
	}
	if !w.fake.Called("podman image exists " + id("a")) {
		t.Fatal("podman was not asked whether it still has the image a failed removal left")
	}
}

// P6b: podman lists an image once per tag, so an image named composer:latest and composer:2
// is planned twice. The first :latest untags it, :2 takes the image, and the second round's
// removals fail on what podman no longer has: done, said and logged so, and the run passes.
func TestATagAnEarlierRemovalTookIsDone(t *testing.T) {
	const latest, two = "docker.io/library/composer:latest", "docker.io/library/composer:2"
	w := newWorld(t)
	composer := img{ID: id("c"), Names: []string{latest, two}, Size: 9, Created: ago(days(400))}
	w.images = []img{composer, composer}
	w.script()
	w.fake.OnFunc([]string{"podman", "image", "rm"}, func(c execx.Cmd) (execx.Result, error) {
		ref := c.Args[len(c.Args)-1]
		switch {
		case w.gone[ref]:
			return execx.Result{ExitCode: 1, Stderr: []byte("Error: " + ref + ": image not known\n")}, nil
		case ref == two:
			w.gone[latest], w.gone[two], w.gone[id("c")] = true, true, true
		default:
			w.gone[ref] = true
		}
		return execx.Result{}, nil
	})
	if code := w.run("prune", "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	for _, say := range []string{"removed with an earlier item: tag " + latest, "removed with an earlier item: tag " + two} {
		if !strings.Contains(w.out.String(), say) {
			t.Errorf("output does not say %q:\n%s", say, w.out.String())
		}
	}
	if n := strings.Count(w.logText(), "\ttag\t"); n != 4 {
		t.Errorf("log holds %d tag lines, want 4:\n%s", n, w.logText())
	}
}

func TestDanglingVolumesAreListedAndNeverRemoved(t *testing.T) {
	w := newWorld(t)
	w.volumes = "vol-a\nvol-b\n"
	if code := w.run("prune", "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if !strings.Contains(w.out.String(), "dangling volumes: 2, never removed by this command: vol-a vol-b") {
		t.Fatalf("output: %s", w.out.String())
	}
	for _, c := range w.fake.Calls() {
		if strings.Contains(c.Line(), "volume rm") || strings.Contains(c.Line(), "volume prune") {
			t.Fatalf("a volume was removed: %s", c.Line())
		}
	}
}

// podman system df fails (exit 125) on a store holding a volume owned outside the user's
// subuid range, so the run never asks it.
func TestTheRunNeverAsksForSystemDf(t *testing.T) {
	w := newWorld(t)
	backlog(w)
	if code := w.run("prune", "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	for _, c := range w.fake.Calls() {
		if strings.Contains(c.Line(), "system") {
			t.Fatalf("ran %s", c.Line())
		}
	}
}

func TestAnEngineThatCannotListStopsTheRunBeforeAnyRemoval(t *testing.T) {
	for _, prefix := range [][]string{
		{"podman", "ps"},
		{"podman", "images", "--format"},
		{"podman", "images", "--filter"},
		{"podman", "volume"},
	} {
		w := newWorld(t)
		backlog(w)
		w.script()
		w.fake.On(prefix, execx.Result{ExitCode: 125, Stderr: []byte("Error: boom\n")})
		if code := w.run("prune", "--yes"); code != term.ExitFailure {
			t.Errorf("%v failing: exit %d", prefix, code)
		}
		if len(w.removed()) != 0 {
			t.Errorf("%v failing: removed %v", prefix, w.removed())
		}
	}
}

func TestALogThatCannotBeWrittenStopsTheRunBeforeAnyRemoval(t *testing.T) {
	w := newWorld(t)
	backlog(w)
	if err := os.MkdirAll(filepath.Join(w.home, ".local", "state", "fedora-desktop", "container-images.log"), 0o700); err != nil {
		t.Fatal(err)
	}
	if code := w.run("prune", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d, want failure", code)
	}
	if len(w.removed()) != 0 || !strings.Contains(w.err.String(), "container-images.log") {
		t.Fatalf("removed %v; stderr %s", w.removed(), w.err.String())
	}
}

// A state directory that cannot be made stops the run before anything is removed.
func TestAStateDirectoryThatCannotBeMadeStopsTheRun(t *testing.T) {
	w := newWorld(t)
	backlog(w)
	if err := os.RemoveAll(filepath.Join(w.home, ".local")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.home, ".local"), []byte("a file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := w.run("prune", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d, want failure", code)
	}
	if len(w.removed()) != 0 || !strings.Contains(w.err.String(), "for the run lock") {
		t.Fatalf("removed %v; stderr %s", w.removed(), w.err.String())
	}
}

func TestUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"frob"}, {"prune", "--force"}, {"prune", "--yes", "--yes"}} {
		w := newWorld(t)
		if code := w.run(args...); code != term.ExitUsage {
			t.Errorf("%v: exit %d, want usage", args, code)
		}
		if !strings.Contains(w.err.String(), "usage: container-images") {
			t.Errorf("%v: stderr %s", args, w.err.String())
		}
		if len(w.fake.Calls()) != 0 {
			t.Errorf("%v: ran %v", args, w.fake.Calls())
		}
	}
	w := newWorld(t)
	if code := w.run("--help"); code != term.ExitOK || !strings.Contains(w.out.String(), "prune [--yes]") {
		t.Fatalf("--help: exit %d, %s", code, w.out.String())
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, os.ErrClosed }

// A report that cannot be written is a failed run, never a silent success.
func TestAnUnwritableReportFailsTheRun(t *testing.T) {
	w := newWorld(t)
	backlog(w)
	w.script()
	app := App{Env: Env{
		Runner: w.fake, Getenv: func(string) string { return w.runtime }, Now: func() time.Time { return now },
		Home: w.home, Stdout: brokenWriter{}, Stderr: brokenWriter{},
	}}
	for _, args := range [][]string{{"prune"}, {"--help"}, {"nonsense"}} {
		if code := app.Run(context.Background(), args); code != term.ExitFailure {
			t.Errorf("%v: exit %d, want failure", args, code)
		}
	}
	w.fake.On([]string{"podman", "volume"}, execx.Result{ExitCode: 125})
	if code := app.Run(context.Background(), []string{"prune"}); code != term.ExitFailure {
		t.Errorf("a failing engine with no stderr: exit %d", code)
	}
}

func TestHumanBytesAndProjectTags(t *testing.T) {
	for n, want := range map[int64]string{0: "0 B", 1023: "1023 B", 1536: "1.5 KiB", 3 << 40: "3.0 TiB"} {
		if got := humanBytes(n); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", n, got, want)
		}
	}
	for name, want := range map[string]bool{
		"claude-yolo:proj": true, "localhost/claude-yolo:proj": true, "claude-yolo:base": false,
		"claude-yolo": false, "claude-yolo:": false, "other/claude-yolo:proj": false,
	} {
		if got := projectTag(name); got != want {
			t.Errorf("projectTag(%q) = %v, want %v", name, got, want)
		}
	}
}

// holdLock takes an exclusive flock on path, as the ccy launcher's flock does, until the test ends.
func holdLock(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	held, err := os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := held.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := syscall.Flock(int(held.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
}

// launched is a ccy launch of a claude-yolo:<project> image age ago: the launcher touches
// ~/.cache/claude-yolo-update-checks/claude-yolo_<project> on every launch day.
func (w *world) launched(project string, age time.Duration) {
	w.t.Helper()
	dir := filepath.Join(w.home, ".cache", "claude-yolo-update-checks")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		w.t.Fatal(err)
	}
	path := filepath.Join(dir, "claude-yolo_"+project)
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		w.t.Fatal(err)
	}
	when := now.Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		w.t.Fatal(err)
	}
}

// F1: podman lists two kinds of "storage" container: buildah's working containers (command
// "buildah") and a container whose record podman's database lost (command "storage"), which
// may be a person's toolbox. Only buildah's is removed; the other is named and kept, and so is
// the image it was made from.
func TestAStorageContainerThatIsNotBuildahsIsNeverRemoved(t *testing.T) {
	w := newWorld(t)
	w.ctrs = []ctr{
		{ID: "t1", Names: []string{"toolbox-a"}, ImageID: id("f"), Created: agoText(days(40)), State: "storage", Command: []string{"storage"}},
		{ID: "t2", ImageID: id("9"), Created: agoText(days(40)), State: "storage"},
	}
	w.dangling = [][]img{{{ID: id("f"), Size: 10, Created: ago(days(50))}}}
	if code := w.run("prune", "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if got := w.removed(); len(got) != 0 {
		t.Fatalf("removed %v, want nothing", got)
	}
	out := w.out.String()
	for _, want := range []string{
		"keep container toolbox-a: not buildah's working container (its command is storage): podman's record of it is gone; left for a person",
		"keep container t2: not buildah's working container (its command is unknown)",
		"keep image ffffffffffff: used by container toolbox-a",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not say %q:\n%s", want, out)
		}
	}
}

// The dangling image an old buildah working container was made from goes in the pass after
// the container: while the container is there, it is in use.
func TestTheImageOfABuildahContainerIsKeptWhileTheContainerIsThere(t *testing.T) {
	w := newWorld(t)
	w.ctrs = []ctr{{ID: "b1", Names: []string{"working-a"}, ImageID: id("f"), Created: agoText(days(2)), State: "storage", Command: []string{"buildah"}}}
	w.dangling = [][]img{{{ID: id("f"), Size: 10, Created: ago(days(5))}}}
	if code := w.run("prune"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if !strings.Contains(w.out.String(), "keep image ffffffffffff: used by container working-a") ||
		!strings.Contains(w.out.String(), "would remove container working-a") {
		t.Fatalf("output: %s", w.out.String())
	}
}

// F2: Q1, now the owner's four weeks, is "since it was created or last used": a project image
// goes only when both its creation and its last launch (ccy's per-image update-check stamp) are
// over 28 days old; with no stamp, its creation alone.
func TestAProjectImageLaunchedRecentlyIsKept(t *testing.T) {
	w := newWorld(t)
	w.images = []img{
		{ID: id("3"), Names: []string{"localhost/claude-yolo:project-b"}, Size: 5, Created: ago(days(31))},
		{ID: id("4"), Names: []string{"claude-yolo:project-c"}, Size: 5, Created: ago(days(60))},
		{ID: id("5"), Names: []string{"localhost/claude-yolo:project-d"}, Size: 5, Created: ago(days(60))},
	}
	w.launched("project-b", days(1))
	w.launched("project-c", days(45))
	if code := w.run("prune", "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	got := w.removed()
	if len(got) != 2 || got[0] != "podman image rm claude-yolo:project-c" || got[1] != "podman image rm localhost/claude-yolo:project-d" {
		t.Fatalf("removed %v, want project-c (both old) and project-d (no stamp, old)", got)
	}
	out := w.out.String()
	for _, want := range []string{
		"keep tag localhost/claude-yolo:project-b: last launched 1 days ago, under 28",
		"removed tag claude-yolo:project-c (5 B): a claude-yolo project image used by no container, created 60 days ago, last launched 45 days ago",
		"removed tag localhost/claude-yolo:project-d (5 B): a claude-yolo project image used by no container, created 60 days ago, no launch recorded",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not say %q:\n%s", want, out)
		}
	}
}

func TestALaunchStampThatCannotBeReadStopsTheRun(t *testing.T) {
	w := newWorld(t)
	w.images = []img{{ID: id("3"), Names: []string{"claude-yolo:project-b"}, Size: 5, Created: ago(days(60))}}
	if err := os.WriteFile(filepath.Join(w.home, ".cache"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if code := w.run("prune", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d, want failure", code)
	}
	if len(w.removed()) != 0 || !strings.Contains(w.err.String(), "claude-yolo_project-b") {
		t.Fatalf("removed %v; stderr %s", w.removed(), w.err.String())
	}
}

// F3: a `podman build --layers` cache hit can reuse an old dangling image, so while a ccy
// image build holds its lock no dangling image or project tag is removed either.
func TestNothingIsRemovedWhileACcyImageBuildHoldsItsLock(t *testing.T) {
	w := newWorld(t)
	w.dangling = [][]img{{{ID: id("a"), Size: 10, Created: ago(days(40))}}}
	w.images = []img{{ID: id("3"), Names: []string{"claude-yolo:project-b"}, Size: 5, Created: ago(days(60))}}
	holdLock(t, filepath.Join(w.runtime, "ccy", "image-builds", "claude-yolo_latest.lock"))
	if code := w.run("prune", "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if got := w.removed(); len(got) != 0 {
		t.Fatalf("removed %v while a build ran", got)
	}
	if !strings.Contains(w.err.String(), "a ccy image build is running (claude-yolo_latest.lock is held): this run removes no image and no buildah container") {
		t.Fatalf("stderr: %s", w.err.String())
	}
	for _, want := range []string{
		"keep image aaaaaaaaaaaa: a ccy image build is running",
		"keep tag claude-yolo:project-b: a ccy image build is running",
	} {
		if !strings.Contains(w.out.String(), want) {
			t.Errorf("output does not say %q:\n%s", want, w.out.String())
		}
	}
}

// A second `prune --yes` while one runs (the timer and a person) would plan the same removals
// and fail on the ones the first made: it refuses, removing nothing.
func TestASecondPruneWhileOneRunsRefuses(t *testing.T) {
	w := newWorld(t)
	backlog(w)
	holdLock(t, filepath.Join(w.home, ".local", "state", "fedora-desktop", "container-images.lock"))
	if code := w.run("prune", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d, want failure", code)
	}
	if len(w.removed()) != 0 || !strings.Contains(w.err.String(), "another container-images prune --yes is running") {
		t.Fatalf("removed %v; stderr %s", w.removed(), w.err.String())
	}
	if code := w.run("prune"); code != term.ExitOK {
		t.Fatalf("a dry run beside a running prune: exit %d: %s", code, w.err.String())
	}
}

// The play runs the first prune until one has succeeded: only a successful `prune --yes`
// writes its stamp.
func TestOnlyASuccessfulRunWritesTheSuccessStamp(t *testing.T) {
	stamp := func(w *world) bool {
		_, err := os.Stat(filepath.Join(w.home, ".local", "state", "fedora-desktop", "container-images.succeeded"))
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		return err == nil
	}
	w := newWorld(t)
	backlog(w)
	if code := w.run("prune"); code != term.ExitOK || stamp(w) {
		t.Fatalf("a dry run: exit %d, stamp %v", code, stamp(w))
	}
	if code := w.run("prune", "--yes"); code != term.ExitOK || !stamp(w) {
		t.Fatalf("a successful run: exit %d, stamp %v: %s", code, stamp(w), w.err.String())
	}
	w = newWorld(t)
	backlog(w)
	w.script()
	w.fake.On([]string{"podman", "image", "rm", id("a")}, execx.Result{ExitCode: 2, Stderr: []byte("Error: refused\n")})
	if code := w.run("prune", "--yes"); code != term.ExitFailure || stamp(w) {
		t.Fatalf("a failed run: exit %d, stamp %v", code, stamp(w))
	}
}

func TestAnUnreadableLockDirectoryStopsTheRun(t *testing.T) {
	w := newWorld(t)
	if err := os.MkdirAll(filepath.Join(w.runtime, "ccy"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.runtime, "ccy", "image-builds"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if code := w.run("prune", "--yes"); code != term.ExitFailure || !strings.Contains(w.err.String(), "image build locks") {
		t.Fatalf("exit %d; stderr %s", code, w.err.String())
	}
}

// The two errors podman gave when c/storage could not unlink files a removed layer held,
// owned by host uids of an old subuid range: the removal that met them, and every store load
// after it (c/storage retries the stale temporary directory on each load).
const (
	unlinkDenied = "Error: unlinkat /srv/store/overlay/tempdirs/temp-dir-17/1-0f3c/diff/etc/skel/.bash_logout: permission denied\n"
	staleDenied  = "Error: loading primary layer store data: error removing stale temp dir: unlinkat /srv/store/overlay/tempdirs/temp-dir-17/1-0f3c/diff/opt/yarn-v1.22.22/bin/yarn: permission denied\n"
	wantCause    = "files owned outside your subuid range; run play-podman.yml, which repairs the store as root"
)

// P6a: a removal c/storage refuses with a permission denied in its temporary directories stops
// the run at once: every later removal would fail the same way.
func TestAStoreHoldingFilesOutsideTheSubuidRangeStopsTheRunAtOnce(t *testing.T) {
	for _, denied := range []string{unlinkDenied, staleDenied} {
		w := newWorld(t)
		w.dangling = [][]img{{
			{ID: id("e"), Size: 10, Created: ago(days(5))},
			{ID: id("a"), Size: 20, Created: ago(days(5))},
			{ID: id("f"), Size: 30, Created: ago(days(5))},
		}, nil}
		w.script()
		w.fake.On([]string{"podman", "image", "rm", id("a")}, execx.Result{ExitCode: 125, Stderr: []byte(denied)})
		w.fake.On([]string{"podman", "image", "rm", id("f")}, execx.Result{ExitCode: 125, Stderr: []byte(staleDenied)})
		if code := w.run("prune", "--yes"); code != term.ExitFailure {
			t.Fatalf("exit %d, want failure", code)
		}
		want := []string{"podman image rm " + id("e"), "podman image rm " + id("a")}
		if got := w.removed(); !slices.Equal(got, want) {
			t.Fatalf("removals %v, want %v", got, want)
		}
		stderr := w.err.String()
		if strings.Count(stderr, wantCause) != 1 || !strings.Contains(stderr, "image aaaaaaaaaaaa") ||
			strings.Contains(stderr, "ffffffffffff") {
			t.Fatalf("stderr: %s", stderr)
		}
		if log := w.logText(); !strings.Contains(log, id("e")) || strings.Contains(log, id("a")) {
			t.Fatalf("log: %s", log)
		}
	}
}

// A store that cannot even be listed for that cause says so too, before any removal.
func TestAStoreThatCannotLoadForThatCauseNamesTheRepair(t *testing.T) {
	w := newWorld(t)
	backlog(w)
	w.script()
	w.fake.On([]string{"podman", "ps"}, execx.Result{ExitCode: 125, Stderr: []byte(staleDenied)})
	if code := w.run("prune", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d, want failure", code)
	}
	if len(w.removed()) != 0 || strings.Count(w.err.String(), wantCause) != 1 {
		t.Fatalf("removed %v; stderr %s", w.removed(), w.err.String())
	}
}

// While the store is not confirmed free of old-range files, a removal could leave a temporary
// directory only root can delete (P6a), so prune --yes removes nothing and fails, loudly (P9).
func TestYesRemovesNothingWhileTheStoreIsNotConfirmedClean(t *testing.T) {
	w := newWorld(t)
	backlog(w)
	unconfirmStore(t, w.home)
	if code := w.run("prune", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d, want failure: %s", code, w.err.String())
	}
	if got := w.removed(); len(got) != 0 {
		t.Fatalf("removed %v from an unconfirmed store", got)
	}
	want := "container-images: " + unconfirmedSays + "; nothing removed; run play-podman.yml\n"
	if w.err.String() != want {
		t.Fatalf("stderr %q, want %q", w.err.String(), want)
	}
	if w.logText() != "" {
		t.Fatalf("log %q", w.logText())
	}
	if _, err := os.Stat(filepath.Join(w.home, ".local", "state", "fedora-desktop", "container-images.succeeded")); !os.IsNotExist(err) {
		t.Fatalf("the success stamp was written: %v", err)
	}
}

// A dry run lists as ever and notes that prune --yes would remove nothing (P9).
func TestADryRunNotesAStoreNotConfirmedClean(t *testing.T) {
	w := newWorld(t)
	backlog(w)
	unconfirmStore(t, w.home)
	if code := w.run("prune"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if !strings.Contains(w.out.String(), "would remove image aaaaaaaaaaaa") || len(w.removed()) != 0 {
		t.Fatalf("removed %v; stdout %s", w.removed(), w.out.String())
	}
	if strings.Count(w.err.String(), unconfirmedSays) != 1 || !strings.Contains(w.err.String(), "prune --yes would remove nothing") {
		t.Fatalf("stderr %s", w.err.String())
	}
}

func TestAMarkerThatCannotBeReadStopsTheDryRun(t *testing.T) {
	w := newWorld(t)
	backlog(w)
	if err := os.RemoveAll(filepath.Join(w.home, ".local")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.home, ".local"), []byte("a file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := w.run("prune"); code != term.ExitFailure {
		t.Fatalf("exit %d, want failure", code)
	}
	if len(w.fake.Calls()) != 0 || !strings.Contains(w.err.String(), "store-clean marker") {
		t.Fatalf("calls %v; stderr %s", w.fake.Calls(), w.err.String())
	}
}
