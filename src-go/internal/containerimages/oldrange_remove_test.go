package containerimages

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// removePlan removes a buildah leftover, an image built on another and that other, and an
// empty volume.
func removePlan(store string) Plan {
	return Plan{
		Store: store, Owners: owners(), Old: oldRange,
		Reown: []PlanStep{}, RootRemove: []PlanStep{}, Held: []PlanStep{}, Left: []string{"image LIVE"},
		Remove: []PlanStep{
			{Kind: kindCtr, Key: "B1", Name: "builder", Why: "a buildah working container", External: true},
			{Kind: kindImage, Key: "CHILD", Name: "CHILD", Why: "no container uses it"},
			{Kind: kindImage, Key: "BASE", Name: "docker.io/library/x:1", Why: "no container uses it"},
			{Kind: kindVolume, Key: "e", Name: "e", Why: "no container mounts it, and it holds no data"},
		},
	}
}

// removeWorld is the user's world at the remove step: a buildah leftover and a stopped
// container db on another image, mounting another volume.
func removeWorld(t *testing.T) (*userWorld, string) {
	t.Helper()
	w := newUserWorld(t)
	w.fake.On([]string{"podman", "ps", "--all", "--external", "--format", "json"}, execx.Result{Stdout: []byte(`[
		{"ID":"B1","Names":["builder"],"ImageID":"BASE","Created":"2026-10-01T00:00:00Z","State":"storage","Command":["buildah"]},
		{"ID":"C1","Names":["db"],"ImageID":"I2","Created":"2026-10-01T00:00:00Z","State":"exited","Command":["postgres"]}]`)})
	w.fake.On([]string{"podman", "container", "inspect"}, execx.Result{Stdout: []byte("[]")})
	w.fake.On([]string{"podman", "container", "inspect", "C1"}, execx.Result{Stdout: []byte(userInspect)})
	w.fake.On([]string{"podman", "rm"}, execx.Result{})
	w.fake.On([]string{"podman", "image", "rm"}, execx.Result{})
	w.fake.On([]string{"podman", "volume", "rm"}, execx.Result{})
	for _, kind := range []string{"image", "volume", "container"} {
		w.fake.On([]string{"podman", kind, "exists"}, execx.Result{})
	}
	return w, w.writePlan(removePlan(w.store))
}

// gone is podman's answer that it has no such thing: `exists` ends 1.
var gone = execx.Result{ExitCode: 1}

// O15: an item podman no longer has when the run looks is already gone: named, not removed,
// not logged, and no failure; the rest still go.
func TestRemoveNamesWhatIsAlreadyGone(t *testing.T) {
	w, plan := removeWorld(t)
	w.fake.On([]string{"podman", "image", "exists", "BASE"}, gone)
	if code := w.run("remove", "--plan", plan, "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if got, want := w.removals(), []string{"podman rm --force B1", "podman image rm CHILD", "podman volume rm e"}; !slices.Equal(got, want) {
		t.Fatalf("removals %v, want %v", got, want)
	}
	if !strings.Contains(w.out.String(), "already gone: image docker.io/library/x:1") || strings.Contains(w.removalLog(), "\tBASE\t") {
		t.Fatalf("output %q, log %q", w.out.String(), w.removalLog())
	}
}

// O15, the host run of Task 4.7d: podman removed an untagged parent with the image built on
// it, so the parent's own removal fails. podman, asked again, no longer has it: the removal is
// done, named so and logged, and the run passes.
func TestRemoveTakesAnItemAnEarlierRemovalTook(t *testing.T) {
	w, plan := removeWorld(t)
	removed := false
	w.fake.OnFunc([]string{"podman", "image", "rm", "CHILD"}, func(execx.Cmd) (execx.Result, error) {
		removed = true
		return execx.Result{}, nil
	})
	w.fake.On([]string{"podman", "image", "rm", "BASE"}, execx.Result{ExitCode: 1, Stderr: []byte("Error: BASE: image not known\n")})
	w.fake.OnFunc([]string{"podman", "image", "exists", "BASE"}, func(execx.Cmd) (execx.Result, error) {
		if removed {
			return gone, nil
		}
		return execx.Result{}, nil
	})
	if code := w.run("remove", "--plan", plan, "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if !strings.Contains(w.out.String(), "removed with an earlier item: image docker.io/library/x:1") {
		t.Errorf("output:\n%s", w.out.String())
	}
	if !strings.Contains(w.removalLog(), "\timage\tdocker.io/library/x:1\tBASE\t") {
		t.Errorf("no BASE in the log:\n%s", w.removalLog())
	}
}

// O15: a failed removal of what podman still has stands: named, not logged, the run fails.
func TestRemoveFailureOfWhatIsStillThereStands(t *testing.T) {
	w, plan := removeWorld(t)
	w.fake.On([]string{"podman", "image", "rm", "BASE"}, execx.Result{ExitCode: 2, Stderr: []byte("Error: image is in use\n")})
	if code := w.run("remove", "--plan", plan, "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.err.String(), "could not remove image docker.io/library/x:1") || strings.Contains(w.removalLog(), "\tBASE\t") {
		t.Fatalf("stderr %q, log %q", w.err.String(), w.removalLog())
	}
}

// O15: a podman that cannot say whether it has an item stops the run before any removal, and
// after a failed removal it leaves the failure standing.
func TestRemoveStopsWhenPodmanCannotSayWhatItHas(t *testing.T) {
	w, plan := removeWorld(t)
	w.fake.On([]string{"podman", "image", "exists", "BASE"}, execx.Result{ExitCode: 125})
	if code := w.run("remove", "--plan", plan, "--yes"); code != term.ExitFailure || len(w.removals()) != 0 {
		t.Errorf("before: exit %d, removals %v", code, w.removals())
	}
	w, plan = removeWorld(t)
	asked := 0
	w.fake.OnFunc([]string{"podman", "image", "exists", "BASE"}, func(execx.Cmd) (execx.Result, error) {
		asked++
		if asked > 1 {
			return execx.Result{ExitCode: 125}, nil
		}
		return execx.Result{}, nil
	})
	w.fake.On([]string{"podman", "image", "rm", "BASE"}, execx.Result{ExitCode: 1})
	if code := w.run("remove", "--plan", plan, "--yes"); code != term.ExitFailure || !strings.Contains(w.err.String(), "could not remove image docker.io/library/x:1") {
		t.Errorf("after: exit %d, stderr %q", code, w.err.String())
	}
}

// O15: the rerun after every removal went: each item is named already gone, nothing is
// removed or logged, and the run passes, the dry run too.
func TestRemoveRerunWhereEverythingIsGonePasses(t *testing.T) {
	for _, args := range [][]string{{"--yes"}, {}} {
		w, plan := removeWorld(t)
		w.fake.On([]string{"podman", "ps", "--all", "--external", "--format", "json"}, execx.Result{Stdout: []byte(`[]`)})
		for _, kind := range []string{"image", "volume", "container"} {
			w.fake.On([]string{"podman", kind, "exists"}, gone)
		}
		if code := w.run(append([]string{"remove", "--plan", plan}, args...)...); code != term.ExitOK {
			t.Fatalf("%v: exit %d: %s", args, code, w.err.String())
		}
		if got := w.removals(); len(got) != 0 {
			t.Errorf("%v: removed %v", args, got)
		}
		for _, want := range []string{"already gone: container builder", "already gone: image CHILD", "already gone: image docker.io/library/x:1", "already gone: volume e"} {
			if !strings.Contains(w.out.String(), want) {
				t.Errorf("%v: no %q in:\n%s", args, want, w.out.String())
			}
		}
		if log := w.removalLog(); log != "" {
			t.Errorf("%v: logged:\n%s", args, log)
		}
	}
}

func (w *userWorld) writePlan(p Plan) string {
	w.t.Helper()
	text, err := json.Marshal(p)
	if err != nil {
		w.t.Fatal(err)
	}
	return w.file("plan.json", string(text))
}

func (w *userWorld) removals() []string {
	var got []string
	for _, line := range w.podmanCalls() {
		if strings.HasPrefix(line, "podman rm") || strings.HasPrefix(line, "podman image rm") || strings.HasPrefix(line, "podman volume rm") {
			got = append(got, line)
		}
	}
	return got
}

func (w *userWorld) removalLog() string {
	w.t.Helper()
	text, err := readIfThere(filepath.Join(w.home, ".local", "state", "fedora-desktop", logName))
	if err != nil {
		w.t.Fatal(err)
	}
	return text
}

// O15: without --yes nothing is removed; the dry run names each removal and what is left.
func TestRemoveDryRunRemovesNothing(t *testing.T) {
	w, plan := removeWorld(t)
	if code := w.run("remove", "--plan", plan); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if got := w.removals(); len(got) != 0 {
		t.Fatalf("a dry run removed %v", got)
	}
	for _, want := range []string{"a dry run", "would remove container builder", "would remove image docker.io/library/x:1", "would remove volume e", "left: image LIVE"} {
		if !strings.Contains(w.out.String(), want) {
			t.Errorf("no %q in:\n%s", want, w.out.String())
		}
	}
}

// O15: --yes removes in the plan's order with podman's own removals (a buildah container is
// podman's only with --force), each logged once it went.
func TestRemoveYesRemovesInOrderAndLogsEach(t *testing.T) {
	w, plan := removeWorld(t)
	if code := w.run("remove", "--plan", plan, "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	want := []string{"podman rm --force B1", "podman image rm CHILD", "podman image rm BASE", "podman volume rm e"}
	if got := w.removals(); !slices.Equal(got, want) {
		t.Fatalf("removals %v, want %v", got, want)
	}
	log := w.removalLog()
	for _, want := range []string{"\tcontainer\tbuilder\tB1\t", "\timage\tCHILD\tCHILD\t", "\tvolume\te\te\t", "Plan 00235 old-range"} {
		if !strings.Contains(log, want) {
			t.Errorf("no %q in the log:\n%s", want, log)
		}
	}
	if !strings.Contains(w.out.String(), "removed image docker.io/library/x:1") {
		t.Errorf("output:\n%s", w.out.String())
	}
}

// O15: podman is asked again before any removal: what a container now uses, mounts or runs
// in is refused, and nothing is removed.
func TestRemoveRefusesWhatIsNowInUse(t *testing.T) {
	for name, ps := range map[string]string{
		"an image now used": `[{"ID":"N","Names":["new"],"ImageID":"sha256:CHILD","State":"exited","Command":["sh"]},
			{"ID":"B1","Names":["builder"],"ImageID":"BASE","State":"storage","Command":["buildah"]}]`,
		"a volume now mounted":    `[{"ID":"C1","Names":["db"],"ImageID":"I2","State":"exited"},{"ID":"B1","Names":["builder"],"ImageID":"BASE","State":"storage","Command":["buildah"]}]`,
		"a container now running": `[{"ID":"B1","Names":["builder"],"ImageID":"BASE","State":"running","Command":["buildah"]}]`,
		"a container gone":        `[]`,
	} {
		w, plan := removeWorld(t)
		w.fake.On([]string{"podman", "ps", "--all", "--external", "--format", "json"}, execx.Result{Stdout: []byte(ps)})
		if name == "a volume now mounted" {
			w.fake.On([]string{"podman", "container", "inspect", "C1"}, execx.Result{Stdout: []byte(`[{"ID":"C1","Mounts":[{"Type":"volume","Name":"e"}]}]`)})
		}
		if code := w.run("remove", "--plan", plan, "--yes"); code != term.ExitFailure {
			t.Errorf("%s: exit %d", name, code)
		}
		if got := w.removals(); len(got) != 0 {
			t.Errorf("%s: removed %v", name, got)
		}
		if !strings.Contains(w.err.String(), "triage again") {
			t.Errorf("%s: stderr %q", name, w.err.String())
		}
	}
}

// O15: a removal podman refuses is named, not logged, the others still go, and the run fails.
func TestRemoveNamesARefusalAndFails(t *testing.T) {
	w, plan := removeWorld(t)
	w.fake.On([]string{"podman", "image", "rm", "CHILD"}, execx.Result{ExitCode: 2, Stderr: []byte("Error: image is in use\n")})
	if code := w.run("remove", "--plan", plan, "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if got := w.removals(); len(got) != 4 {
		t.Fatalf("removals %v", got)
	}
	if !strings.Contains(w.err.String(), "could not remove image CHILD") || strings.Contains(w.removalLog(), "\tCHILD\t") {
		t.Fatalf("stderr %q, log %q", w.err.String(), w.removalLog())
	}
}

// P6a holds here too: a permission denied in c/storage's temporary directories stops the run.
func TestRemoveStopsWhereTheStoreRefuses(t *testing.T) {
	w, plan := removeWorld(t)
	w.fake.On([]string{"podman", "image", "rm", "CHILD"}, execx.Result{ExitCode: 125, Stderr: []byte(unlinkDenied)})
	if code := w.run("remove", "--plan", plan, "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if got := w.removals(); !slices.Equal(got, []string{"podman rm --force B1", "podman image rm CHILD"}) {
		t.Fatalf("removals %v", got)
	}
	if !strings.Contains(w.err.String(), storeCause) {
		t.Fatalf("stderr %q", w.err.String())
	}
}

// O14, O15: the remove step runs as the plan's user; another prune's run lock, a plan it
// cannot read, or a podman that cannot list stops it before a removal.
func TestRemoveRefusesWhatItCannotTrust(t *testing.T) {
	cases := map[string]func(w *userWorld, plan *string){
		"as root":      func(w *userWorld, _ *string) { w.euid = 0 },
		"another user": func(w *userWorld, _ *string) { w.euid = 1001 },
		"no plan":      func(w *userWorld, plan *string) { *plan = filepath.Join(w.dir, "nope") },
		"not a plan":   func(w *userWorld, plan *string) { *plan = w.file("bad.json", "[") },
		"podman ps":    func(w *userWorld, _ *string) { w.fake.OnError([]string{"podman", "ps"}, errors.New("podman broke")) },
		"inspect": func(w *userWorld, _ *string) {
			w.fake.OnError([]string{"podman", "container", "inspect"}, errors.New("podman broke"))
		},
		"run lock held": func(w *userWorld, _ *string) {
			holdLock(t, filepath.Join(w.home, ".local", "state", "fedora-desktop", lockName))
		},
		"no state dir": func(w *userWorld, _ *string) { writeFile(t, filepath.Join(w.home, ".local", "state"), "a file") },
	}
	for name, spoil := range cases {
		w, plan := removeWorld(t)
		spoil(w, &plan)
		if code := w.run("remove", "--plan", plan, "--yes"); code != term.ExitFailure {
			t.Errorf("%s: exit %d", name, code)
		}
		if got := w.removals(); len(got) != 0 {
			t.Errorf("%s: removed %v", name, got)
		}
	}
}

// A report that cannot be written fails the run.
func TestRemoveFailsWhenItsReportCannotBeWritten(t *testing.T) {
	for _, args := range [][]string{{"remove", "--plan", "P"}, {"remove", "--plan", "P", "--yes"}} {
		w, plan := removeWorld(t)
		args[2] = plan
		app := App{Env: Env{Runner: w.fake, Now: func() time.Time { return now }, Home: w.home, UID: 1000, EUID: 1000, Stdout: brokenWriter{}, Stderr: &w.err}}
		if code := app.Run(t.Context(), append([]string{"old-range"}, args...)); code != term.ExitFailure {
			t.Errorf("%v: exit %d", args, code)
		}
	}
}

func readIfThere(path string) (string, error) {
	text, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	return string(text), err
}
