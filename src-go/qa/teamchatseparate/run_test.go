package teamchatseparate

import (
	"errors"
	"io/fs"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

const (
	pathsGo  = "src-go/internal/teamchat/paths/paths.go"
	v2Play   = "playbooks/imports/play-teamchat.yml"
	v2Tasks  = "tasks/teamchat-units.yml"
	v1Names  = "src-go/internal/teamchat/v1names/v1names.go"
	fenceGo  = "src-go/internal/teamchat/migrate/fence.go"
	fenceLit = "Before=agent-bus-hs@"
)

// world is a fake tree: files by path, the list git would publish, and the captured streams.
type world struct {
	files     map[string]string
	published []string
	gitErr    error
	out, err  strings.Builder
}

// shippedList is the real v1 name list, so the fixtures are judged by what the gate ships.
func shippedList(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(namesFile[len(packageDir):])
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// cleanWorld is a v2 tree with no v1 name, and the two data files.
func cleanWorld(t *testing.T) *world {
	t.Helper()
	w := &world{files: map[string]string{
		namesFile:   shippedList(t),
		allowedFile: "# no rows\n",
		pathsGo:     "package paths\n\nconst Product = \"teamchat\"\n",
		v2Play:      "- name: teamchat\n  hosts: localhost\n",
	}}
	w.publishAll()
	return w
}

func (w *world) publishAll() {
	w.published = w.published[:0]
	for p := range w.files {
		w.published = append(w.published, p)
	}
	slices.Sort(w.published)
}

func (w *world) set(path, text string) {
	w.files[path] = text
	w.publishAll()
}

func (w *world) run(args ...string) ExitCode { return Main(args, w.env()) }

func (w *world) env() Env {
	mem := fstest.MapFS{}
	for p, text := range w.files {
		mem[p] = &fstest.MapFile{Data: []byte(text)}
	}
	return Env{
		Stdout:    &w.out,
		Stderr:    &w.err,
		Cwd:       "/repo",
		Resolve:   func(p string) (string, error) { return p, nil },
		Open:      func(string) fs.FS { return mem },
		Published: func(string) ([]string, error) { return w.published, w.gitErr },
	}
}

func TestK1HelpUsageAndRoot(t *testing.T) {
	w := cleanWorld(t)
	if code := w.run("--help"); code != ExitOK || !strings.Contains(w.out.String(), "usage: qa-teamchat-separate") {
		t.Fatalf("--help: %d %q", code, w.out.String())
	}
	for _, args := range [][]string{{"--nope"}, {"extra"}} {
		bad := cleanWorld(t)
		code := bad.run(args...)
		if code != ExitCannotRun || !strings.Contains(bad.err.String(), "ERROR: unknown argument") ||
			!strings.Contains(bad.err.String(), "usage:") || bad.out.String() != "" {
			t.Fatalf("%v: %d out=%q err=%q", args, code, bad.out.String(), bad.err.String())
		}
	}
	w = cleanWorld(t)
	if code := w.run("--root"); code != ExitCannotRun || !strings.Contains(w.err.String(), "ERROR: ") {
		t.Fatalf("--root with no value: %d %q", code, w.err.String())
	}
}

func TestK1RootFromScriptsParentAndRootFlag(t *testing.T) {
	var roots []string
	for _, args := range [][]string{nil, {"--root", "/some/root"}, {"--scripts", "/some/root/scripts"}} {
		w := cleanWorld(t)
		env := w.env()
		env.Published = func(root string) ([]string, error) {
			roots = append(roots, root)
			return w.published, nil
		}
		if code := Main(args, env); code != ExitOK {
			t.Fatalf("%v: %d %s", args, code, w.err.String())
		}
	}
	if want := []string{"/repo", "/some/root", "/some/root"}; !slices.Equal(roots, want) {
		t.Fatalf("roots %q want %q", roots, want)
	}
}

func TestK2ACleanTreePasses(t *testing.T) {
	w := cleanWorld(t)
	if code := w.run(); code != ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	want := "✓ teamchat-keeps-off-v1: 2 v2 file(s) hold no v1 name (14 listed, 0 allowed reference(s))\n"
	if w.out.String() != want || w.err.String() != "" {
		t.Fatalf("out=%q err=%q", w.out.String(), w.err.String())
	}
}

// The red proof of D 4.1: one v1 path in a v2 play fails the gate, naming the file and line.
func TestK3AV1PathInAV2PlayFails(t *testing.T) {
	w := cleanWorld(t)
	w.set(v2Play, "- name: teamchat\n  hosts: localhost\n  vars:\n    state: /var/lib/agent-bus/ops\n")
	if code := w.run(); code != ExitFailed {
		t.Fatalf("exit %d: %s", code, w.out.String())
	}
	if !strings.Contains(w.err.String(), v2Play+":4: v1 name agent-bus: state: /var/lib/agent-bus/ops") {
		t.Fatalf("stderr:\n%s", w.err.String())
	}
	if !strings.Contains(w.err.String(), "✗ teamchat-keeps-off-v1: 1 v1 reference(s) in 1 v2 file(s)") || w.out.String() != "" {
		t.Fatalf("out=%q err=%q", w.out.String(), w.err.String())
	}
}

// Every listed name fails in every part of the scope.
func TestK3EveryListedNameFailsEverywhereInScope(t *testing.T) {
	names, err := ParseNames(shippedList(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		for _, p := range []string{
			pathsGo, "src-go/cmd/teamchat/main.go", "files/opt/claude-yolo/optional/teamchat/settings.json",
			v2Play, v2Tasks,
		} {
			w := cleanWorld(t)
			w.set(p, "ok\nx "+n.Text+"y\n")
			if code := w.run(); code != ExitFailed || !strings.Contains(w.err.String(), p+":2: v1 name "+n.Text) {
				t.Errorf("%q in %s: exit %d\n%s", n.Text, p, code, w.err.String())
			}
		}
	}
}

// One finding per name per line, however often the name appears on it; every name is reported.
func TestK3FindingsAreOnePerNamePerLine(t *testing.T) {
	w := cleanWorld(t)
	w.set(pathsGo, "a pingbus pingbus agent-bus\n")
	if code := w.run(); code != ExitFailed {
		t.Fatalf("exit %d", code)
	}
	if got := strings.Count(w.err.String(), pathsGo+":1:"); got != 2 {
		t.Fatalf("%d findings, want 2:\n%s", got, w.err.String())
	}
}

// The two named homes may hold v1 names, and files outside the scope are never judged.
func TestK4HomesAndFilesOutsideTheScopeAreNotJudged(t *testing.T) {
	w := cleanWorld(t)
	w.set(v1Names, "const State = \"/var/lib/agent-bus\"\n")
	w.set("src-go/internal/teamchat/migrate/v1/adapter.go", "// reads /var/lib/agent-bus-applied\n")
	w.set("src-go/internal/teams/board.go", "agent-bus\n")
	w.set("docs/teamchat.md", "v1 was pingbus\n")
	w.files["src-go/internal/teamchat/paths/scratch.go"] = "pingbus\n" // on disk, not published
	if code := w.run(); code != ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if !strings.Contains(w.out.String(), "2 v2 file(s)") {
		t.Fatalf("a file outside the scope was counted: %q", w.out.String())
	}
}

// The green proof of D 4.1: the unit's v1 paths come from v1names, and the v2 play task that
// renders it names no v1 path, so the tree passes.
func TestK4AUnitRenderedFromV1NamesPasses(t *testing.T) {
	w := cleanWorld(t)
	w.set(v1Names, "var Inaccessible = []string{\"-/var/lib/agent-bus\", \"-/etc/agent-bus\"}\n")
	w.set(v2Tasks, "- name: Render the server unit\n  ansible.builtin.command: teamchat-admin unit render server\n")
	if code := w.run(); code != ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
}

// A row of the allowlist covers its literal on its file, and nothing else.
func TestK5TheAllowlistCoversItsLiteralOnly(t *testing.T) {
	row := fenceGo + "\t" + fenceLit + "\tthe fence unit orders itself before v1's server\n"
	w := cleanWorld(t)
	w.set(fenceGo, "const before = \"Before=agent-bus-hs@%s.service\"\n")
	w.set(allowedFile, row)
	if code := w.run(); code != ExitOK || !strings.Contains(w.out.String(), "1 allowed reference(s)") {
		t.Fatalf("an allowed reference failed: %d %q %q", code, w.out.String(), w.err.String())
	}
	w = cleanWorld(t)
	w.set(fenceGo, "const before = \"Before=agent-bus-hs@%s.service\" // agent-bus\n")
	w.set(allowedFile, row)
	if code := w.run(); code != ExitFailed || !strings.Contains(w.err.String(), fenceGo+":1: v1 name agent-bus") {
		t.Fatalf("a hit outside the literal passed: %d %q", code, w.err.String())
	}
	w = cleanWorld(t)
	w.set(pathsGo, "const before = \"Before=agent-bus-hs@%s.service\"\n")
	w.set(fenceGo, "const before = \"Before=agent-bus-hs@%s.service\"\n")
	w.set(allowedFile, row)
	if code := w.run(); code != ExitFailed || !strings.Contains(w.err.String(), pathsGo+":1:") {
		t.Fatalf("the row covered another file: %d %q", code, w.err.String())
	}
}

// A row that covers no finding is stale, and fails the gate.
func TestK5AStaleRowFails(t *testing.T) {
	w := cleanWorld(t)
	w.set(allowedFile, "# header\n"+fenceGo+"\t"+fenceLit+"\tgone\n")
	if code := w.run(); code != ExitFailed {
		t.Fatalf("exit %d: %s", code, w.out.String())
	}
	if !strings.Contains(w.err.String(), allowedFile+":2: stale: "+fenceLit+" covers no v1 name in "+fenceGo) {
		t.Fatalf("stderr:\n%s", w.err.String())
	}
}

// D 4.1 is one row per reference: a row whose literal covers v1 names on two lines fails, so a
// second copy of an allowed reference cannot pass under the first one's row.
func TestK5ARowCoversOneLineOnly(t *testing.T) {
	w := cleanWorld(t)
	w.set(fenceGo, "a \"Before=agent-bus-hs@%s.service\"\nb \"Before=agent-bus-hs@%s.service\"\n")
	w.set(allowedFile, "# header\n"+fenceGo+"\t"+fenceLit+"\tthe fence unit orders itself before v1's server\n")
	if code := w.run(); code != ExitFailed {
		t.Fatalf("exit %d: %s", code, w.out.String())
	}
	want := allowedFile + ":2: " + fenceLit + " covers v1 names on lines 1 and 2 of " + fenceGo + ": one row per reference"
	if !strings.Contains(w.err.String(), want) || !strings.Contains(w.err.String(), "1 allowlist row(s) stale or covering more than one line") {
		t.Fatalf("stderr:\n%s", w.err.String())
	}
}

// The v2 matrix client's replay test reads recorded real-Tuwunel captures, which hold v1 names:
// the fixtures live under tests/, outside the scope, and the three lines of the test that name
// what the capture recorded are allowed by one exact row each. These rows go into allowed.tsv
// with the test file itself (a row for a file not in the tree is stale).
const (
	replayTest = "src-go/internal/teamchat/matrix/fakehs/replay_test.go"
	replayRows = replayTest + "\ttests/helpers/pingbus/fixtures/tuwunel\tthe recorded real-Tuwunel captures the replay reads, which v1's tests own\n" +
		replayTest + "\tprobe.agent-bus.internal\tthe server name the capture was recorded under; the replay must match it byte for byte\n" +
		replayTest + "\t\"agent_bus.status\"\tthe v1 event type the capture recorded, which the replay picks out of the recorded sync\n"
	replayText = "package fakehs\n\n" +
		"const fixtureDir = \"../../../../../tests/helpers/pingbus/fixtures/tuwunel\"\n\n" +
		"const probeServerName = \"probe.agent-bus.internal\"\n\n" +
		"func status(ev map[string]any) bool {\n" +
		"\treturn ev[\"type\"] == \"agent_bus.status\" && ev[\"state_key\"] == \"probe\"\n}\n"
)

func TestK5TheMatrixReplayRowsAllowExactlyItsThreeReferences(t *testing.T) {
	w := cleanWorld(t)
	w.set(replayTest, replayText)
	w.set(allowedFile, replayRows)
	if code := w.run(); code != ExitOK || !strings.Contains(w.out.String(), "3 allowed reference(s)") {
		t.Fatalf("green: exit %d out=%q err=%q", code, w.out.String(), w.err.String())
	}
	// Red: any other v1 name in the same file still fails, on its own line or beside an allowed one.
	for _, extra := range []string{
		"// see agent-bus-hs@probe\n",
		"const again = \"probe.agent-bus.internal\" // a second copy\n",
		"var x = \"agent_bus.status\" + pingbus\n",
	} {
		red := cleanWorld(t)
		red.set(replayTest, replayText+extra)
		red.set(allowedFile, replayRows)
		if code := red.run(); code != ExitFailed || !strings.Contains(red.err.String(), "✗ teamchat-keeps-off-v1") {
			t.Errorf("red %q: exit %d err=%q", extra, code, red.err.String())
		}
	}
	// Without the rows, each of the three lines is a finding.
	w = cleanWorld(t)
	w.set(replayTest, replayText)
	if code := w.run(); code != ExitFailed || !strings.Contains(w.err.String(), "3 v1 reference(s) in 1 v2 file(s)") {
		t.Fatalf("no rows: exit %d err=%q", code, w.err.String())
	}
}

// A v2 play or task file names only v2's own files and the listed shared ones: a task file named
// without teamchat would otherwise leave the scope unseen (fail fast, not a silent hole).
func TestK7APlayNamingAFileOutsideTheScopeFails(t *testing.T) {
	w := cleanWorld(t)
	w.set(v2Play, "- name: teamchat\n  tasks:\n    - ansible.builtin.include_tasks: \"{{ playbook_dir }}/../../tasks/render-units.yml\"\n")
	if code := w.run(); code != ExitFailed {
		t.Fatalf("exit %d: %s", code, w.out.String())
	}
	if !strings.Contains(w.err.String(), v2Play+":3: names tasks/render-units.yml, which v2 does not own") {
		t.Fatalf("stderr:\n%s", w.err.String())
	}
	// A path joined to root_dir, a vars file, and a path that climbs out of v2's directory are
	// each judged where they lead.
	for line, want := range map[string]string{
		"  src: \"{{ root_dir ~ '/files/home/.local/bin/x' }}\"\n":                     "names files/home/.local/bin/x, which v2 does not own",
		"  vars_files: \"{{ root_dir }}/vars/version-pins.yml\"\n":                     "names vars/version-pins.yml, which v2 does not own",
		"  include_tasks: \"{{ root_dir }}/tasks/teamchat/../render-units.yml\"\n":     "names tasks/render-units.yml, which v2 does not own",
		"  include_tasks: \"{{ root_dir }}/tasks/../playbooks/x.yml\"\n":               "names playbooks/x.yml, which v2 does not own",
		"  include_tasks: \"{{ root_dir }}/tasks/../../etc/x.yml\"\n":                  "names ../etc/x.yml, which v2 does not own",
		"  include_tasks: \"{{ root_dir }}/tasks/teamchat/../build-go-command.yml\"\n": "",
	} {
		red := cleanWorld(t)
		red.set(v2Tasks, line)
		code := red.run()
		if want == "" {
			if code != ExitOK {
				t.Errorf("%q: exit %d: %s", line, code, red.err.String())
			}
			continue
		}
		if code != ExitFailed || !strings.Contains(red.err.String(), v2Tasks+":1: "+want) {
			t.Errorf("%q: exit %d, stderr:\n%s", line, code, red.err.String())
		}
	}
	for name, text := range map[string]string{
		"shared":  "- ansible.builtin.import_tasks: \"{{ playbook_dir }}/../../tasks/build-go-command.yml\"\n",
		"v2's":    "- ansible.builtin.include_tasks: tasks/teamchat-units.yml\n  src: \"{{ root_dir }}/files/etc/teamchat/owner-group\"\n",
		"vars":    "  vars_files: \"{{ root_dir }}/vars/teamchat.yml\"\n",
		"no path": "- name: Render\n  ansible.builtin.command: teamchat-admin unit render server\n",
	} {
		green := cleanWorld(t)
		green.set(v2Tasks, text)
		if code := green.run(); code != ExitOK {
			t.Errorf("%s: exit %d: %s", name, code, green.err.String())
		}
	}
	// Only YAML is read for references: Go code naming tasks/ is not an include.
	w = cleanWorld(t)
	w.set(pathsGo, "// not an include: tasks/render-units.yml\n")
	if code := w.run(); code != ExitOK {
		t.Fatalf("a Go file: exit %d: %s", code, w.err.String())
	}
}

func TestK6TheRunThatCannotJudgeIsExit2(t *testing.T) {
	for name, change := range map[string]func(w *world){
		"git fails":         func(w *world) { w.gitErr = errors.New("not a git repository") },
		"no v2 file":        func(w *world) { delete(w.files, pathsGo); delete(w.files, v2Play); w.publishAll() },
		"no list":           func(w *world) { delete(w.files, namesFile) },
		"an empty list":     func(w *world) { w.files[namesFile] = "# nothing\n" },
		"a bad list":        func(w *world) { w.files[namesFile] = "a !b !c\n" },
		"no allowlist":      func(w *world) { delete(w.files, allowedFile) },
		"a bad allowlist":   func(w *world) { w.files[allowedFile] = "only-one-field\n" },
		"an unreadable one": func(w *world) { w.published = append(w.published, "src-go/internal/teamchat/gone.go") },
	} {
		w := cleanWorld(t)
		change(w)
		if code := w.run(); code != ExitCannotRun || !strings.Contains(w.err.String(), "ERROR: ") || w.out.String() != "" {
			t.Errorf("%s: exit %d out=%q err=%q", name, code, w.out.String(), w.err.String())
		}
	}
}

// failWriter fails every write.
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestK6AnUnwritableStreamIsExit2(t *testing.T) {
	w := cleanWorld(t)
	env := w.env()
	env.Stdout = failWriter{}
	if code := Main(nil, env); code != ExitCannotRun || !strings.Contains(w.err.String(), "ERROR: cannot write to stdout") {
		t.Fatalf("stdout: %d %q", code, w.err.String())
	}
	w = cleanWorld(t)
	w.set(pathsGo, "pingbus\n")
	env = w.env()
	env.Stderr = failWriter{}
	if code := Main(nil, env); code != ExitCannotRun {
		t.Fatalf("stderr: %d", code)
	}
}
