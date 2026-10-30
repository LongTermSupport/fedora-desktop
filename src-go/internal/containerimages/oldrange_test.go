package containerimages

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// userWorld is the user's half of old-range: a store with c/storage's records, root's scan of
// it in a file, and a fake podman.
type userWorld struct {
	t        *testing.T
	fake     *execx.Fake
	dir      string
	home     string
	store    string
	scanPath string
	out, err bytes.Buffer
	euid     int
}

// ps is the podman view: db is a stopped container on I2 mounting the volume v.
const (
	userPS      = `[{"ID":"C1","Names":["db"],"ImageID":"sha256:I2","Created":"2026-10-01T00:00:00Z","State":"exited","Command":["postgres"]}]`
	userInspect = `[{"ID":"C1","Mounts":[{"Type":"volume","Name":"v"},{"Type":"bind","Name":""}]}]`
	userVolumes = `[{"Name":"v","Mountpoint":"/x/volumes/v/_data"},{"Name":"e","Mountpoint":"/x/volumes/e/_data"}]`
)

// newUserWorld is a store of an unused image I1, an image I2 a stopped container uses, a volume
// v that container mounts and an empty volume e, each holding old-range files.
func newUserWorld(t *testing.T) *userWorld {
	t.Helper()
	dir := t.TempDir()
	home := filepath.Join(dir, "home", "user")
	w := &userWorld{t: t, fake: &execx.Fake{}, dir: dir, home: home, store: filepath.Join(home, storeRel), euid: 1000}
	writeFile(t, filepath.Join(w.store, "overlay-layers", "layers.json"), `[{"id":"L1"},{"id":"L2"},{"id":"CL","parent":"L2"}]`)
	writeFile(t, filepath.Join(w.store, "overlay-images", "images.json"),
		`[{"id":"I1","names":["docker.io/library/x:1"],"layer":"L1"},{"id":"I2","layer":"L2"}]`)
	writeFile(t, filepath.Join(w.store, "overlay-containers", "containers.json"), `[{"id":"C1","names":["db"],"image":"I2","layer":"CL"}]`)
	dirs := []StoreDir{
		{Key: "overlay/L1", Paths: 2, UIDs: []int64{100099}, GIDs: []int64{100103}, Samples: []string{"overlay/L1/diff/var/cache/apt/archives/partial"}, Size: 4096, Data: true},
		{Key: "overlay/L2", Paths: 1, UIDs: []int64{100000}, GIDs: []int64{100000}, Samples: []string{"overlay/L2/diff/etc/shadow"}, Size: 2048, Data: true},
		{Key: "volumes/e", Paths: 1, UIDs: []int64{100998}, GIDs: []int64{100998}, Samples: []string{"volumes/e/_data"}},
		{Key: "volumes/v", Paths: 3, UIDs: []int64{100998}, GIDs: []int64{100998}, Samples: []string{"volumes/v/_data/base"}, Size: 1 << 20, Data: true},
	}
	w.writeScan(Scan{Store: w.store, User: "user", Owners: owners(), Dirs: dirs})
	w.fake.On([]string{"podman", "ps", "--all", "--external", "--format", "json"}, execx.Result{Stdout: []byte(userPS)})
	w.fake.On([]string{"podman", "container", "inspect", "C1"}, execx.Result{Stdout: []byte(userInspect)})
	w.fake.On([]string{"podman", "volume", "ls", "--format", "json"}, execx.Result{Stdout: []byte(userVolumes)})
	return w
}

func (w *userWorld) writeScan(s Scan) {
	w.t.Helper()
	text, err := json.Marshal(s)
	if err != nil {
		w.t.Fatal(err)
	}
	w.scanPath = filepath.Join(w.dir, "scan.json")
	writeFile(w.t, w.scanPath, string(text))
}

func (w *userWorld) file(name, text string) string {
	w.t.Helper()
	path := filepath.Join(w.dir, name)
	writeFile(w.t, path, text)
	return path
}

func (w *userWorld) run(args ...string) term.ExitCode {
	w.t.Helper()
	w.out.Reset()
	w.err.Reset()
	app := App{Env: Env{
		Runner: w.fake, Now: func() time.Time { return now }, Home: w.home, UID: w.euid, EUID: w.euid,
		Getenv: func(string) string { return "" }, Stdout: &w.out, Stderr: &w.err,
	}}
	return app.Run(w.t.Context(), append([]string{"old-range"}, args...))
}

func (w *userWorld) podmanCalls() []string {
	calls := w.fake.Calls()
	got := make([]string, 0, len(calls))
	for _, c := range calls {
		got = append(got, c.Line())
	}
	return got
}

// The command line: each subcommand takes its own flags, a required one missing, an unknown
// one or a flag without its value is a usage error, answered with the usage text.
func TestOldRangeUsage(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"zap"},
		{"scan"},
		{"scan", "--user"},
		{"scan", "--user", "joe", "--yes"},
		{"triage"},
		{"triage", "--scan", "f", "--old-start", "x"},
		{"triage", "--scan", "f", "--old-count", "-1x"},
		{"plan", "--scan", "f"},
		{"plan", "--decisions", "f"},
		{"reown"},
		{"remove", "--plan", "f", "--yes", "--yes"},
		{"remove", "--plan", "f", "extra"},
	} {
		w := newUserWorld(t)
		if code := w.run(args...); code != term.ExitUsage {
			t.Errorf("%v: exit %d", args, code)
		}
		if !strings.Contains(w.err.String(), "container-images old-range") || len(w.fake.Calls()) != 0 {
			t.Errorf("%v: stderr %q, calls %v", args, w.err.String(), w.podmanCalls())
		}
	}
}

// O14: triage joins root's scan with c/storage's records and podman's view, prints its report
// (markdown, for a person) and writes the proposed decisions file.
func TestTriageReportsEachItemAndWritesTheProposal(t *testing.T) {
	w := newUserWorld(t)
	proposed := filepath.Join(w.dir, "proposed.tsv")
	if code := w.run("triage", "--scan", w.scanPath, "--proposed", proposed); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	report := w.out.String()
	for _, want := range []string{
		"# Old-range triage", w.store, "old range assumed: 100000-165535", "current subuid range: 524288-589823",
		"4 items: 2 REMOVE, 2 RE-OWN, 0 ASK, 0 HELD",
		"## image I1 (docker.io/library/x:1)", "REMOVE: no container uses it",
		"## image I2", "RE-OWN: used by stopped container db",
		"## volume v", "container db (exited)", "## volume e", "holds no data",
		"overlay/L1/diff/var/cache/apt/archives/partial", "uids 100000-100998",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("no %q in the report:\n%s", want, report)
		}
	}
	text, err := os.ReadFile(filepath.Clean(proposed))
	if err != nil {
		t.Fatal(err)
	}
	d, err := parseDecisions(string(text))
	if err != nil {
		t.Fatalf("the proposal does not read: %v\n%s", err, text)
	}
	want := map[itemKey]string{{kindImage, "I1"}: actRemove, {kindImage, "I2"}: actReown, {kindVolume, "v"}: actReown, {kindVolume, "e"}: actRemove}
	if d.old != oldRange || len(d.actions) != len(want) {
		t.Fatalf("proposal = %+v", d)
	}
	for k, a := range want {
		if d.actions[k] != a {
			t.Errorf("%v proposed %q, want %q", k, d.actions[k], a)
		}
	}
	if calls := w.podmanCalls(); !slices.Contains(calls, "podman container inspect C1") {
		t.Errorf("calls = %v", calls)
	}
}

// Without --proposed the report alone is printed; --old-start and --old-count change the old
// range the proposal names and judges by.
func TestTriageTakesTheOldRange(t *testing.T) {
	w := newUserWorld(t)
	proposed := filepath.Join(w.dir, "proposed.tsv")
	if code := w.run("triage", "--scan", w.scanPath, "--old-start", "100000", "--old-count", "50", "--proposed", proposed); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	text, err := os.ReadFile(filepath.Clean(proposed))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), "old-range\t100000\t50\n") || !strings.Contains(string(text), "ask\timage\tI1") {
		t.Fatalf("proposal:\n%s", text)
	}
	if code := w.run("triage", "--scan", w.scanPath); code != term.ExitOK || !strings.Contains(w.out.String(), "## image I1") {
		t.Fatalf("no --proposed: exit %d: %s %s", code, w.out.String(), w.err.String())
	}
}

// O14: the user's half runs as the user the scan is of, never as root, and stops before any
// output when a scan, a record or a podman list cannot be read.
func TestTriageAndPlanRefuseWhatTheyCannotJudge(t *testing.T) {
	cases := map[string]func(w *userWorld){
		"as root":      func(w *userWorld) { w.euid = 0 },
		"another user": func(w *userWorld) { w.euid = 1001 },
		"no scan":      func(w *userWorld) { w.scanPath = filepath.Join(w.dir, "nope.json") },
		"not a scan":   func(w *userWorld) { w.scanPath = w.file("bad.json", "{") },
		"bad record":   func(w *userWorld) { writeFile(t, filepath.Join(w.store, "overlay-images", "images.json"), "{") },
		"podman ps":    func(w *userWorld) { w.fake.OnError([]string{"podman", "ps"}, errors.New("podman broke")) },
		"inspect": func(w *userWorld) {
			w.fake.OnError([]string{"podman", "container", "inspect"}, errors.New("no such container"))
		},
		"volumes":       func(w *userWorld) { w.fake.OnError([]string{"podman", "volume", "ls"}, errors.New("podman broke")) },
		"no store path": func(w *userWorld) { w.writeScan(Scan{Store: "", User: "user", Owners: owners()}) },
	}
	for name, spoil := range cases {
		for _, sub := range []string{"triage", "plan"} {
			w := newUserWorld(t)
			spoil(w)
			proposed := filepath.Join(w.dir, "proposed.tsv")
			args := []string{"triage", "--scan", w.scanPath, "--proposed", proposed}
			if sub == "plan" {
				args = []string{"plan", "--scan", w.scanPath, "--decisions", w.file("d.tsv", "old-range 100000 65536\n")}
			}
			if code := w.run(args...); code != term.ExitFailure {
				t.Errorf("%s %s: exit %d", sub, name, code)
			}
			if w.out.Len() != 0 || !strings.Contains(w.err.String(), "container-images: ") {
				t.Errorf("%s %s: stdout %q, stderr %q", sub, name, w.out.String(), w.err.String())
			}
			if _, err := os.Stat(proposed); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("%s %s: a proposal was written", sub, name)
			}
		}
	}
}

// A proposal that cannot be written fails the triage.
func TestTriageFailsWhenTheProposalCannotBeWritten(t *testing.T) {
	w := newUserWorld(t)
	if code := w.run("triage", "--scan", w.scanPath, "--proposed", filepath.Join(w.dir, "no", "such", "dir", "p.tsv")); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
}

const safeDecisions = "old-range 100000 65536\nremove image I1\nreown image I2\nreown volume v\nremove volume e\n"

// O11: plan checks the decisions against the store now and prints the plan root and the user
// carry out.
func TestPlanPrintsThePlanOfTheDecisions(t *testing.T) {
	w := newUserWorld(t)
	if code := w.run("plan", "--scan", w.scanPath, "--decisions", w.file("d.tsv", safeDecisions)); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	var p Plan
	if err := json.Unmarshal(w.out.Bytes(), &p); err != nil {
		t.Fatalf("not a plan: %v\n%s", err, w.out.String())
	}
	if got := stepKeys(p.Reown); !slices.Equal(got, []string{"overlay/L1", "overlay/L2", "volumes/e", "volumes/v"}) {
		t.Errorf("reown = %v", got)
	}
	if got := stepKeys(p.Remove); !slices.Equal(got, []string{"I1", "e"}) {
		t.Errorf("remove = %v", got)
	}
	if p.Store != w.store || p.Old != oldRange || p.Owners != owners() {
		t.Errorf("plan header = %+v", p)
	}
}

// rerunWorld is newUserWorld after its decisions were carried out: I1 and e removed, I2 and v
// re-owned, so root's fresh scan finds no old-range file.
func rerunWorld(t *testing.T) *userWorld {
	t.Helper()
	w := newUserWorld(t)
	writeFile(t, filepath.Join(w.store, "overlay-layers", "layers.json"), `[{"id":"L2"},{"id":"CL","parent":"L2"}]`)
	writeFile(t, filepath.Join(w.store, "overlay-images", "images.json"), `[{"id":"I2","layer":"L2"}]`)
	w.writeScan(Scan{Store: w.store, User: "user", Owners: owners(), Dirs: []StoreDir{}})
	w.fake.On([]string{"podman", "volume", "ls", "--format", "json"}, execx.Result{Stdout: []byte(`[{"Name":"v"}]`)})
	w.fake.On([]string{"podman", "image", "exists", "I2"}, execx.Result{})
	w.fake.On([]string{"podman", "volume", "exists", "v"}, execx.Result{})
	w.fake.On([]string{"podman", "image", "exists", "I1"}, execx.Result{ExitCode: 1})
	w.fake.On([]string{"podman", "volume", "exists", "e"}, execx.Result{ExitCode: 1})
	return w
}

// O11: a rerun of the same decisions over a store they were carried out on plans nothing: what
// was decided remove and podman no longer has is already gone, what was decided reown and is
// still there with no old-range file is already re-owned; each is named, and remove passes.
func TestPlanRerunOfCarriedOutDecisionsPasses(t *testing.T) {
	w := rerunWorld(t)
	if code := w.run("plan", "--scan", w.scanPath, "--decisions", w.file("d.tsv", safeDecisions)); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	var p Plan
	if err := json.Unmarshal(w.out.Bytes(), &p); err != nil {
		t.Fatalf("not a plan: %v\n%s", err, w.out.String())
	}
	if len(p.Reown) != 0 || len(p.Remove) != 0 || len(p.RootRemove) != 0 {
		t.Fatalf("a rerun planned work: %+v", p)
	}
	want := []string{"already gone: image I1", "already re-owned: image I2", "already gone: volume e", "already re-owned: volume v"}
	if !slices.Equal(p.Done, want) {
		t.Fatalf("done %v, want %v", p.Done, want)
	}
	plan := w.file("plan.json", w.out.String())
	if code := w.run("remove", "--plan", plan, "--yes"); code != term.ExitOK {
		t.Fatalf("remove: exit %d: %s", code, w.err.String())
	}
	for _, line := range want {
		if !strings.Contains(w.out.String(), line) {
			t.Errorf("remove does not say %q:\n%s", line, w.out.String())
		}
	}
}

// O11: a decided item that is not an item now is refused unless it is done: a reown podman
// no longer has, a remove podman still has, a leave, a reown or remove podman cannot be asked.
func TestPlanRefusesADecidedItemThatIsNotDone(t *testing.T) {
	for name, spoil := range map[string]func(w *userWorld){
		"a reown gone":         func(w *userWorld) { w.fake.On([]string{"podman", "volume", "exists", "v"}, execx.Result{ExitCode: 1}) },
		"a remove still there": func(w *userWorld) { w.fake.On([]string{"podman", "image", "exists", "I1"}, execx.Result{}) },
		"podman unasked": func(w *userWorld) {
			w.fake.On([]string{"podman", "image", "exists", "I1"}, execx.Result{ExitCode: 125})
		},
	} {
		w := rerunWorld(t)
		spoil(w)
		if code := w.run("plan", "--scan", w.scanPath, "--decisions", w.file("d.tsv", safeDecisions)); code != term.ExitFailure || w.out.Len() != 0 {
			t.Errorf("%s: exit %d, out %q", name, code, w.out.String())
		}
	}
	w := rerunWorld(t)
	text := strings.Replace(safeDecisions, "reown image I2", "leave image I2", 1)
	if code := w.run("plan", "--scan", w.scanPath, "--decisions", w.file("d.tsv", text)); code != term.ExitFailure {
		t.Errorf("a leave not an item now: exit %d", code)
	}
}

func TestPlanRefusesUnsafeDecisions(t *testing.T) {
	for name, text := range map[string]string{
		"remove a used image": strings.Replace(safeDecisions, "reown image I2", "remove image I2", 1),
		"an item missing":     strings.Replace(safeDecisions, "remove volume e\n", "", 1),
		"unreadable":          "remove image I1\n",
	} {
		w := newUserWorld(t)
		if code := w.run("plan", "--scan", w.scanPath, "--decisions", w.file("d.tsv", text)); code != term.ExitFailure {
			t.Errorf("%s: exit %d", name, code)
		}
		if w.out.Len() != 0 {
			t.Errorf("%s: a plan was printed: %s", name, w.out.String())
		}
	}
	w := newUserWorld(t)
	if code := w.run("plan", "--scan", w.scanPath, "--decisions", filepath.Join(w.dir, "nope")); code != term.ExitFailure {
		t.Errorf("a missing decisions file: exit %d", code)
	}
}
