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
	scripted bool
	// sticky is a podman whose removals report success and leave the thing there.
	sticky bool
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
	return w
}

func (w *world) script() {
	if w.scripted {
		return
	}
	w.scripted = true
	gone := map[string]bool{}
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
			m.Names = slices.DeleteFunc(slices.Clone(m.Names), func(n string) bool { return gone[n] })
			if len(m.Names) > 0 {
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
	removal := func(c execx.Cmd) (execx.Result, error) {
		if !w.sticky {
			gone[c.Args[len(c.Args)-1]] = true
		}
		return execx.Result{}, nil
	}
	w.fake.OnFunc([]string{"podman", "image", "rm"}, removal)
	w.fake.OnFunc([]string{"podman", "rm", "--force"}, removal)
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

func TestAStaleProjectImageIsRemovedAndBaseAndLatestNeverAre(t *testing.T) {
	w := newWorld(t)
	w.images = []img{
		{ID: id("1"), Names: []string{"localhost/claude-yolo:latest"}, Size: 5, Created: ago(days(90))},
		{ID: id("2"), Names: []string{"localhost/claude-yolo:base"}, Size: 5, Created: ago(days(90))},
		{ID: id("3"), Names: []string{"localhost/claude-yolo:project-b"}, Size: 3 << 30, Created: ago(days(31))},
		{ID: id("4"), Names: []string{"localhost/claude-yolo:project-c"}, Size: 5, Created: ago(days(29))},
		{ID: id("5"), Names: []string{"localhost/claude-yolo:project-d"}, Size: 5, Created: ago(days(60))},
		{ID: id("6"), Names: []string{"localhost/claude-yolo:project-e", "localhost/other:1"}, Size: 7, Created: ago(days(60))},
		{ID: id("7"), Names: []string{"quay.io/example/tool:1"}, Size: 5, Created: ago(days(400))},
	}
	w.ctrs = []ctr{{ID: "s1", Names: []string{"project-d"}, ImageID: id("5"), Created: agoText(days(50)), State: "exited"}}
	if code := w.run("prune", "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	got := w.removed()
	if len(got) != 2 || got[0] != "podman image rm localhost/claude-yolo:project-b" ||
		got[1] != "podman image rm localhost/claude-yolo:project-e" {
		t.Fatalf("removed %v, want the two stale unused project tags", got)
	}
	log := w.logText()
	if !strings.Contains(log, "tag\tlocalhost/claude-yolo:project-b\t"+id("3")+"\t3221225472\t") ||
		!strings.Contains(log, "tag\tlocalhost/claude-yolo:project-e\t"+id("6")+"\t0\t") {
		t.Fatalf("log: %s", log)
	}
	out := w.out.String()
	for _, want := range []string{
		"keep tag localhost/claude-yolo:project-c: created 29 days ago, under 30",
		"keep tag localhost/claude-yolo:project-d: used by container project-d",
		"untagged localhost/claude-yolo:project-e: it keeps its other names",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not say %q:\n%s", want, out)
		}
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
	if err := os.WriteFile(filepath.Join(w.home, ".local"), []byte("a file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := w.run("prune", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d, want failure", code)
	}
	if len(w.removed()) != 0 || !strings.Contains(w.err.String(), "container-images.log") {
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
