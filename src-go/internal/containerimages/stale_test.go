package containerimages

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const installed = "3.103.0"

func labelled(version string) map[string]string {
	return map[string]string{labelCCYVersion: version, "claude-yolo-version": "2.79.15"}
}

// staleWorld is the store after ccy moved on to 3.103.0: images it built itself, older ones,
// one built before ccy labelled its images, a newer one and someone else's.
func staleWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t)
	w.images = []img{
		{ID: id("1"), Names: []string{"localhost/claude-yolo:latest"}, Size: 5, Labels: labelled(installed), Created: ago(time.Hour)},
		{ID: id("2"), Names: []string{"localhost/claude-yolo:base"}, Size: 5, Labels: labelled("3.90.0"), Created: ago(days(9))},
		{ID: id("3"), Names: []string{"localhost/claude-yolo:project-a"}, Size: 3 << 30, Labels: labelled("3.102.2"), Created: ago(days(1))},
		{ID: id("4"), Names: []string{"localhost/claude-yolo:project-b"}, Size: 5, Labels: labelled(installed), Created: ago(days(1))},
		{ID: id("5"), Names: []string{"localhost/claude-yolo:project-c"}, Size: 7, Labels: map[string]string{"claude-yolo-version": "2.70.0"}, Created: ago(days(3))},
		{ID: id("6"), Names: []string{"localhost/claude-yolo:project-d"}, Size: 5, Labels: labelled("3.110.0"), Created: ago(days(3))},
		{ID: id("7"), Names: []string{"quay.io/example/tool:1"}, Size: 5, Created: ago(days(300))},
		{ID: id("8"), Names: []string{"localhost/claude-yolo:project-e"}, Size: 9, Labels: labelled("3.9.9"), Created: ago(days(3))},
	}
	return w
}

// S1-S3: a project image an older ccy built, or one from before ccy labelled its images, goes
// as soon as no container uses it, whatever its age; ccy's own :base and :latest, a current or
// newer one, and an image that is not ccy's stay.
func TestAProjectImageAnOlderCcyBuiltIsRemovedOnceNoContainerUsesIt(t *testing.T) {
	w := staleWorld(t)
	if code := w.run("stale", "--yes", installed); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	want := []string{
		"podman image rm localhost/claude-yolo:project-a", "podman image rm localhost/claude-yolo:project-c",
		"podman image rm localhost/claude-yolo:project-e",
	}
	if got := w.removed(); !slices.Equal(got, want) {
		t.Fatalf("removed %v, want %v", got, want)
	}
	for _, say := range []string{
		"container-images: removed tag localhost/claude-yolo:project-a (3.0 GiB): built by ccy 3.102.2, older than the installed 3.103.0, and no container uses it",
		"container-images: removed tag localhost/claude-yolo:project-c (7 B): built before ccy labelled its images (ccy-version), and no container uses it",
	} {
		if !strings.Contains(w.err.String(), say) {
			t.Errorf("stderr does not say %q:\n%s", say, w.err.String())
		}
	}
	if w.out.Len() != 0 {
		t.Errorf("stdout is the payload, and stale has none: %q", w.out.String())
	}
	log := w.logText()
	if strings.Count(log, "\n") != 3 || !strings.Contains(log, "\ttag\tlocalhost/claude-yolo:project-a\t"+id("3")+"\t3221225472\t") {
		t.Fatalf("log: %s", log)
	}
}

// S4: a stale image a container uses stays, and so does the container: a stopped one is named
// on stderr and in the log as the old session keeping it, a running one on stderr only.
func TestAStaleImageAContainerUsesIsKeptAndAStoppedOneIsNamedInTheLog(t *testing.T) {
	w := newWorld(t)
	w.images = []img{
		{ID: id("a"), Size: 4 << 30, Labels: labelled("3.80.0"), Created: ago(days(40))},
		{ID: id("f"), Names: []string{"localhost/claude-yolo:project-f"}, Size: 5, Labels: labelled("3.80.0"), Created: ago(days(40))},
		{ID: id("9"), Size: 5, Labels: labelled("3.80.0"), Created: ago(days(40))},
	}
	w.ctrs = []ctr{
		{ID: "s1", Names: []string{"old-session"}, ImageID: id("a"), Created: agoText(days(40)), State: "exited"},
		{ID: "s2", Names: []string{"live"}, ImageID: id("f"), Created: agoText(days(2)), State: "running"},
	}
	if code := w.run("stale", "--yes", installed); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if got := w.removed(); len(got) != 0 {
		t.Fatalf("removed %v", got)
	}
	for _, say := range []string{
		"container-images: keep image aaaaaaaaaaaa (4.0 GiB): built by ccy 3.80.0, older than the installed 3.103.0, but the stopped container old-session (exited, created 2026-09-18) uses it; ccy never removes a container: remove the container to let the image go",
		"container-images: keep tag localhost/claude-yolo:project-f (5 B): built by ccy 3.80.0, older than the installed 3.103.0, but the running container live uses it",
	} {
		if !strings.Contains(w.err.String(), say) {
			t.Errorf("stderr does not say %q:\n%s", say, w.err.String())
		}
	}
	if strings.Contains(w.err.String(), "999999999999") {
		t.Errorf("a stale dangling image no container uses is the daily prune's, not named here:\n%s", w.err.String())
	}
	log := w.logText()
	if strings.Count(log, "\n") != 1 || !strings.Contains(log, "\theld\told-session\t"+id("a")+"\t4294967296\t") ||
		!strings.Contains(log, "the stopped container old-session (exited, created 2026-09-18) uses it") {
		t.Fatalf("log: %s", log)
	}
}

// Without --yes, the same judgement, nothing removed and nothing logged.
func TestAStaleDryRunRemovesAndLogsNothing(t *testing.T) {
	w := staleWorld(t)
	w.ctrs = []ctr{{ID: "s1", Names: []string{"old-session"}, ImageID: id("8"), Created: agoText(days(3)), State: "exited"}}
	if code := w.run("stale", installed); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if got := w.removed(); len(got) != 0 || w.logText() != "" {
		t.Fatalf("a dry run removed %v, logged %q", got, w.logText())
	}
	for _, say := range []string{
		"container-images: would remove tag localhost/claude-yolo:project-a (3.0 GiB): built by ccy 3.102.2",
		"container-images: keep tag localhost/claude-yolo:project-e (9 B): built by ccy 3.9.9, older than the installed 3.103.0, but the stopped container old-session",
	} {
		if !strings.Contains(w.err.String(), say) {
			t.Errorf("stderr does not say %q:\n%s", say, w.err.String())
		}
	}
}

// S5: the store-clean marker and the other build locks gate every removal, as for replaced.
func TestStaleRemovesNothingWithoutTheMarkerOrWhileAnotherBuildRuns(t *testing.T) {
	w := staleWorld(t)
	unconfirmStore(t, w.home)
	if code := w.run("stale", "--yes", installed); code != term.ExitOK {
		t.Fatalf("no marker: exit %d: %s", code, w.err.String())
	}
	if len(w.removed()) != 0 || strings.Count(w.err.String(), unconfirmedSays) != 1 {
		t.Fatalf("no marker: removed %v; stderr %s", w.removed(), w.err.String())
	}

	w = staleWorld(t)
	locks := filepath.Join(w.runtime, "ccy", "image-builds")
	holdLock(t, filepath.Join(locks, "claude-yolo_project-x.lock"))
	if code := w.run("stale", "--yes", installed); code != term.ExitOK {
		t.Fatalf("another build: exit %d: %s", code, w.err.String())
	}
	if len(w.removed()) != 0 || !strings.Contains(w.err.String(),
		"keep tag localhost/claude-yolo:project-a (3.0 GiB): another ccy image build is running (claude-yolo_project-x.lock is held)") {
		t.Fatalf("another build: removed %v; stderr %s", w.removed(), w.err.String())
	}

	w = staleWorld(t)
	own := filepath.Join(w.runtime, "ccy", "image-builds", "claude-yolo_latest.lock")
	holdLock(t, own)
	if code := w.run("stale", "--lock", own, "--yes", installed); code != term.ExitOK {
		t.Fatalf("own lock: exit %d: %s", code, w.err.String())
	}
	if len(w.removed()) != 3 {
		t.Fatalf("own lock: removed %v; stderr %s", w.removed(), w.err.String())
	}
}

// A stale image another is built on stays: podman would refuse it.
func TestAStaleImageAnotherIsBuiltOnIsKept(t *testing.T) {
	w := newWorld(t)
	w.images = []img{
		{ID: id("3"), Names: []string{"localhost/claude-yolo:project-a"}, Size: 5, Labels: labelled("3.1.0"), Created: ago(days(1))},
		{ID: id("4"), Names: []string{"localhost/claude-yolo:project-a2"}, Size: 5, Labels: labelled(installed), Created: ago(days(1))},
	}
	w.layers = map[string][]string{id("3"): {"sha256:l1"}, id("4"): {"sha256:l1", "sha256:l2"}}
	if code := w.run("stale", "--yes", installed); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if len(w.removed()) != 0 || !strings.Contains(w.err.String(), "keep tag localhost/claude-yolo:project-a (5 B): built by ccy 3.1.0, older than the installed 3.103.0, but image 444444444444 is built on it") {
		t.Fatalf("removed %v; stderr %s", w.removed(), w.err.String())
	}
}

// S6: a list the engine cannot give stops the run before a removal; a refused removal is named
// and fails the run, which the ccy build reports and does not fail for.
func TestStaleFailsOnAListItCannotGetOrARefusedRemoval(t *testing.T) {
	for _, prefix := range [][]string{{"podman", "ps"}, {"podman", "images"}, {"podman", "image", "inspect"}} {
		w := staleWorld(t)
		w.script()
		w.fake.On(prefix, execx.Result{ExitCode: 125, Stderr: []byte("Error: boom\n")})
		if code := w.run("stale", "--yes", installed); code != term.ExitFailure {
			t.Errorf("%v failing: exit %d", prefix, code)
		}
		if len(w.removed()) != 0 || !strings.Contains(w.err.String(), "boom") {
			t.Errorf("%v failing: removed %v; stderr %s", prefix, w.removed(), w.err.String())
		}
	}
	w := staleWorld(t)
	w.script()
	w.fake.On([]string{"podman", "image", "rm", "localhost/claude-yolo:project-a"}, execx.Result{ExitCode: 2, Stderr: []byte("Error: in use\n")})
	if code := w.run("stale", "--yes", installed); code != term.ExitFailure {
		t.Fatalf("a refused removal: exit %d", code)
	}
	if !strings.Contains(w.err.String(), "could not remove tag localhost/claude-yolo:project-a") || strings.Contains(w.logText(), "project-a") {
		t.Fatalf("stderr %s; log %s", w.err.String(), w.logText())
	}
	if !strings.Contains(w.logText(), "project-c") {
		t.Fatalf("the removals after a refused one did not run: %s", w.logText())
	}
}

// The rule reads podman's store only; under docker it says so and removes nothing.
func TestStaleUnderDockerSaysSoAndRemovesNothing(t *testing.T) {
	w := newWorld(t)
	if code := w.run("--engine", "docker", "stale", "--yes", installed); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if len(w.fake.Calls()) != 0 || !strings.Contains(w.err.String(), "reads podman's store only; nothing removed") {
		t.Fatalf("ran %v; stderr %s", w.fake.Calls(), w.err.String())
	}
}

func TestStaleUsage(t *testing.T) {
	for _, args := range [][]string{
		{"stale"},
		{"stale", "--yes"},
		{"stale", "v3"},
		{"stale", "3.1", "4.2"},
		{"stale", "--lock"},
		{"stale", "--lock", "", installed},
		{"stale", "--yes", "--yes", installed},
		{"stale", "3..1"},
	} {
		w := newWorld(t)
		if code := w.run(args...); code != term.ExitUsage {
			t.Errorf("%v: exit %d, want usage", args, code)
		}
		if len(w.fake.Calls()) != 0 {
			t.Errorf("%v: ran %v", args, w.fake.Calls())
		}
	}
}

func TestOlderVersion(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"3.9.9", "3.103.0", true},
		{"3.103.0", "3.103.0", false},
		{"3.103", "3.103.0", false},
		{"3.103.0", "3.103.1", true},
		{"4.0.0", "3.200.0", false},
		{"3.102", "3.102.1", true},
	} {
		if got := olderVersion(c.a, c.b); got != c.want {
			t.Errorf("olderVersion(%s, %s) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
