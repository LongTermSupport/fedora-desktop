// Package golden is the parity oracle for `pingbus hook <event>` (Plan 00209 stage 2, Task
// 3.1). It is test only.
//
// testdata/scenarios.json is the hand-edited source of truth: named fragments (files and
// environment several worlds share) and the scenarios, each a world (the fragments it uses, its
// own files, its environment) and its steps. A step is one hook run in that world: its event,
// its fixed clock (now_ms), its stdin, and what changes before it runs (files placed or removed,
// variables set or unset, locks the harness holds with flock for the step, the fake homeserver's
// failure). The steps of a scenario share the world, so the stop guard meets its own record and
// a prompt follows a session start. testdata/golden/<scenario>/<n>.{stdout,stderr,exit,
// files.json,requests.json} is what the Python zipapp did at step n, recorded by `-update`.
//
// Every world has the fake `pingbus` (testdata/fake-pingbus) first on PATH, in {world}/bin: the
// watcher the hooks start is that script, which appends its arguments and whether the session's
// socket variable reached it to {world}/spawn.log and exits. A step that expects a start waits for
// it (bounded); at the end of a scenario the harness waits a moment more and fails a start no step
// expected. The goodbye's homeserver is an httptest server on 127.0.0.1 inside the harness
// (homeserver_test.go): member.json's base_url is {hs}. It serves the room state the goodbye
// verifies and the send, and can fail (500, 403, slow beyond the goodbye's 4 s, a wrong team
// record).
//
// The clock is data, never the wall clock: the Python zipapp runs as `python3 -c <prelude>
// <zipapp> hook <event>`, the prelude making time.time return the step's now_ms (passed in
// PINGBUS_ORACLE_NOW_MS, which it removes before the hook reads the environment; half a
// millisecond is added so int(time.time()*1000) is now_ms exactly); the Go run gets the same
// instant as Input.Now.
//
// The default run drives the Go hooks.Run in-process. `-impl=python` builds the Python zipapp
// from this checkout and judges it; `-impl=<command>` runs any binary, split on spaces, with the
// event as its last argument and PINGBUS_ORACLE_NOW_MS in its environment. `-update` re-records
// golden/ from the Python, running every scenario twice and failing where the two runs differ;
// review the diff before committing it. `-update-deviations` rewrites testdata/deviation/ from
// the Go run, for the steps testdata/deviations.json already pins.
//
// What a step's result is, and how it is compared:
//
//   - stdout and the exit status: byte for byte.
//   - files: every directory, regular file and symlink under the world after the step (not bin/),
//     with its path, mode, and content or link target, the world's root written {world} and the
//     homeserver's URL {hs}: byte for byte. No temporary file may survive a step.
//   - requests: each request the fake homeserver received during the step, in order: method,
//     decoded path with the send's transaction ID written {txn}, decoded query, the
//     Authorization header, and the body as canonical JSON: byte for byte.
//   - stderr: by shape. The same number of lines; a line that starts `pingbus: hook <event>: `
//     keeps that prefix and the first three words after it, any other line its first three
//     words; the shapes must be equal in order. Empty stays empty. So the Go may word the rest
//     of a diagnostic differently, never add, drop or reorder one.
//
// A Go departure the plan decided (DESIGN.md) is pinned in testdata/deviations.json: a file the
// Go no longer writes is dropped from every recorded step before the Go is judged
// (dropped_files), and a step whose whole result departs is judged against
// testdata/deviation/<scenario>/<n>.* instead (pinned). Neither applies to -impl=python.
package golden

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

var (
	implFlag         = flag.String("impl", "", `"python" runs the Python zipapp; another value is a command, split on spaces; empty runs the Go hooks.Run in-process`)
	update           = flag.Bool("update", false, "re-record golden/ from the Python zipapp built from this checkout")
	updateDeviations = flag.Bool("update-deviations", false, "rewrite deviation/ from the Go run, for the steps deviations.json pins")
)

// goPending is set while internal/pingbus/hooks is the stand-in (pending_test.go).
var goPending bool

const (
	goldenDir    = "golden"
	deviationDir = "deviation"
	dirMode      = 0o750
	fileMode     = 0o600
)

type fileEntry struct {
	Path       string  `json:"path"`
	Kind       string  `json:"kind"`
	Content    *string `json:"content"`
	ContentB64 *string `json:"content_b64"`
	Mode       string  `json:"mode"`
	Target     string  `json:"target"`
}

type stdinRepeat struct {
	Text  string `json:"text"`
	Count int    `json:"count"`
}

// stdinHolder is a stdin given as one of three keys.
type stdinHolder struct {
	Stdin       *string      `json:"stdin"`
	StdinB64    *string      `json:"stdin_b64"`
	StdinRepeat *stdinRepeat `json:"stdin_repeat"`
}

// heldLock is a lock the harness holds with flock for one step, the file holding content.
type heldLock struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type step struct {
	stdinHolder

	Event  string            `json:"event"`
	NowMs  int64             `json:"now_ms"`
	Files  []fileEntry       `json:"files"`
	Remove []string          `json:"remove"`
	Env    map[string]string `json:"env"`
	Unset  []string          `json:"unset"`
	Hold   []heldLock        `json:"hold"`
	// Spawn is whether this step starts the watcher (the fake pingbus records it).
	Spawn bool `json:"spawn"`
	// Homeserver is the fake homeserver's failure from this step on ("" serves).
	Homeserver *string `json:"homeserver"`
	// Exit is the status the Python must give; nil is 0.
	Exit *int `json:"exit"`
}

type fragment struct {
	Files []fileEntry       `json:"files"`
	Env   map[string]string `json:"env"`
}

type scenario struct {
	Name       string            `json:"name"`
	About      string            `json:"about"`
	Use        []string          `json:"use"`
	Files      []fileEntry       `json:"files"`
	Env        map[string]string `json:"env"`
	Homeserver string            `json:"homeserver"`
	Steps      []step            `json:"steps"`
}

type scenarioFile struct {
	V         int                 `json:"v"`
	Rooms     map[string]roomConf `json:"rooms"`
	Fragments map[string]fragment `json:"fragments"`
	Scenarios []scenario          `json:"scenarios"`
}

// stepResult is what one step did, already normalised ({world}, {hs}, {txn}).
type stepResult struct {
	stdout, stderr []byte
	code           int
	files          []byte
	requests       []byte
	problems       []string
}

func decodeStrict(raw []byte, into any, what string) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	return nil
}

func loadScenarios(t *testing.T) scenarioFile {
	t.Helper()
	raw, err := fs.ReadFile(os.DirFS("testdata"), "scenarios.json")
	if err != nil {
		t.Fatal(err)
	}
	var file scenarioFile
	if err := decodeStrict(raw, &file, "scenarios.json"); err != nil {
		t.Fatal(err)
	}
	if file.V != 1 {
		t.Fatalf("scenarios.json: version %d, want 1", file.V)
	}
	return file
}

// The scenarios themselves are well formed: unique names, an about, known fragments, an event
// and a clock on every step.
func TestScenariosAreWellFormed(t *testing.T) {
	file := loadScenarios(t)
	seen := map[string]bool{}
	for _, sc := range file.Scenarios {
		if seen[sc.Name] {
			t.Errorf("%s: the name is used twice", sc.Name)
		}
		seen[sc.Name] = true
		if sc.About == "" {
			t.Errorf("%s: no about", sc.Name)
		}
		for _, name := range sc.Use {
			if _, ok := file.Fragments[name]; !ok {
				t.Errorf("%s: unknown fragment %q", sc.Name, name)
			}
		}
		if len(sc.Steps) == 0 {
			t.Errorf("%s: no steps", sc.Name)
		}
		for i, st := range sc.Steps {
			if st.Event == "" || st.NowMs <= 0 {
				t.Errorf("%s step %d: an event and a positive now_ms are required", sc.Name, i+1)
			}
		}
	}
}

func TestHooksGolden(t *testing.T) {
	file := loadScenarios(t)
	devs := loadDeviations(t, file)
	switch {
	case *update:
		recordGolden(t, file)
		return
	case *updateDeviations:
		recordDeviations(t, file, devs)
		return
	}
	if *implFlag == "" && goPending {
		t.Skip("internal/pingbus/hooks is the stand-in: run -impl=python until the port lands")
	}
	render, isPython := rendererFor(t)
	for _, sc := range file.Scenarios {
		t.Run(sc.Name, func(t *testing.T) {
			t.Parallel()
			results := runScenario(t, render, file, sc)
			for i, got := range results {
				checkStep(t, sc.Name, i+1, got, devs, isPython)
			}
		})
	}
}

// checkStep compares one step with its record, or with its pinned departure.
func checkStep(t *testing.T, name string, n int, got stepResult, devs deviations, isPython bool) {
	t.Helper()
	for _, p := range got.problems {
		t.Errorf("step %d: %s", n, p)
	}
	dir := filepath.Join(goldenDir, name)
	pinned := !isPython && devs.pinned(name, n)
	if pinned {
		dir = filepath.Join(deviationDir, name)
	}
	want := readRecord(t, dir, n)
	if !isPython && !pinned {
		files, err := devs.dropFiles(want.files)
		if err != nil {
			t.Fatalf("step %d: %v", n, err)
		}
		want.files = files
	}
	if !bytes.Equal(got.stdout, want.stdout) {
		t.Errorf("step %d: stdout %q, recorded %q", n, got.stdout, want.stdout)
	}
	if !stderrMatches(want.stderr, got.stderr) {
		t.Errorf("step %d: stderr %q, recorded %q", n, got.stderr, want.stderr)
	}
	if got.code != want.code {
		t.Errorf("step %d: exit %d, recorded %d", n, got.code, want.code)
	}
	if !bytes.Equal(got.files, want.files) {
		t.Errorf("step %d: files differ\n got: %s\nwant: %s", n, got.files, want.files)
	}
	if !bytes.Equal(got.requests, want.requests) {
		t.Errorf("step %d: homeserver requests differ\n got: %s\nwant: %s", n, got.requests, want.requests)
	}
}

var recordExts = []string{"stdout", "stderr", "exit", "files.json", "requests.json"}

// readRecord is the recorded result of step n under testdata/<dir>.
func readRecord(t *testing.T, dir string, n int) stepResult {
	t.Helper()
	read := func(ext string) []byte {
		data, err := fs.ReadFile(os.DirFS(filepath.Join("testdata", dir)), fmt.Sprintf("%d.%s", n, ext))
		if err != nil {
			t.Fatalf("no record: %v (run with -update)", err)
		}
		return data
	}
	code, err := strconv.Atoi(strings.TrimSpace(string(read("exit"))))
	if err != nil {
		t.Fatalf("%s step %d: exit record: %v", dir, n, err)
	}
	return stepResult{
		stdout: read("stdout"), stderr: read("stderr"), code: code,
		files: read("files.json"), requests: read("requests.json"),
	}
}

func writeRecord(dir string, n int, r stepResult) error {
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return err
	}
	parts := [][]byte{r.stdout, r.stderr, []byte(strconv.Itoa(r.code) + "\n"), r.files, r.requests}
	for i, ext := range recordExts {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d.%s", n, ext)), parts[i], fileMode); err != nil {
			return err
		}
	}
	return nil
}

// recordGolden re-records golden/ from the Python zipapp. Each scenario runs twice, and a
// difference between the runs, or a Python that misses the scenario's own expectations
// (exit status, watcher starts), fails the recording.
func recordGolden(t *testing.T, file scenarioFile) {
	t.Helper()
	render := execRender(pythonCommand(t), true)
	if err := os.RemoveAll(filepath.Join("testdata", goldenDir)); err != nil {
		t.Fatal(err)
	}
	for _, sc := range file.Scenarios {
		t.Run(sc.Name, func(t *testing.T) {
			t.Parallel()
			recordScenario(t, render, file, sc)
		})
	}
}

// recordScenario runs one scenario twice and writes its record.
func recordScenario(t *testing.T, render renderer, file scenarioFile, sc scenario) {
	t.Helper()
	first := runScenario(t, render, file, sc)
	second := runScenario(t, render, file, sc)
	for i, r := range first {
		n := i + 1
		if len(r.problems) > 0 {
			t.Fatalf("step %d: the Python failed the scenario's own check: %v", n, r.problems)
		}
		if want := expectedExit(sc.Steps[i]); r.code != want {
			t.Fatalf("step %d: the Python exited %d, want %d (stderr %q)", n, r.code, want, r.stderr)
		}
		if diff := resultDiff(r, second[i]); diff != "" {
			t.Fatalf("step %d: two recordings differ in %s", n, diff)
		}
		if err := writeRecord(filepath.Join("testdata", goldenDir, sc.Name), n, r); err != nil {
			t.Fatal(err)
		}
	}
}

// recordDeviations rewrites the pinned steps' files from the Go run in-process.
func recordDeviations(t *testing.T, file scenarioFile, devs deviations) {
	t.Helper()
	if goPending {
		t.Fatal("internal/pingbus/hooks is the stand-in: there is no Go run to record")
	}
	for _, sc := range file.Scenarios {
		if !devs.anyPinned(sc.Name) {
			continue
		}
		results := runScenario(t, goRender, file, sc)
		for i, r := range results {
			if !devs.pinned(sc.Name, i+1) {
				continue
			}
			if err := writeRecord(filepath.Join("testdata", deviationDir, sc.Name), i+1, r); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func resultDiff(a, b stepResult) string {
	switch {
	case !bytes.Equal(a.stdout, b.stdout):
		return "stdout"
	case !bytes.Equal(a.stderr, b.stderr):
		return "stderr"
	case a.code != b.code:
		return "exit"
	case !bytes.Equal(a.files, b.files):
		return fmt.Sprintf("files:\n%s\n%s", a.files, b.files)
	case !bytes.Equal(a.requests, b.requests):
		return fmt.Sprintf("requests:\n%s\n%s", a.requests, b.requests)
	}
	return ""
}

func expectedExit(st step) int {
	if st.Exit == nil {
		return 0
	}
	return *st.Exit
}

// stderrWords is how many words of a diagnostic its shape keeps.
const stderrWords = 3

// stderrShape is each line's shape: a `pingbus: hook <event>: ` prefix kept whole, then the
// first stderrWords words of the rest (of the whole line when there is no such prefix).
func stderrShape(b []byte) []string {
	if len(b) == 0 {
		return nil
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	shapes := make([]string, 0, len(lines))
	for _, line := range lines {
		prefix := ""
		if rest, ok := strings.CutPrefix(line, "pingbus: hook "); ok {
			if event, _, found := strings.Cut(rest, ": "); found && !strings.Contains(event, " ") {
				prefix = "pingbus: hook " + event + ": "
			}
		}
		words := strings.Fields(strings.TrimPrefix(line, prefix))
		shapes = append(shapes, prefix+strings.Join(words[:min(len(words), stderrWords)], " "))
	}
	return shapes
}

// stderrMatches is the stderr rule of the package doc.
func stderrMatches(want, got []byte) bool {
	if bytes.Equal(want, got) {
		return true
	}
	if len(want) == 0 || len(got) == 0 {
		return false
	}
	return slices.Equal(stderrShape(want), stderrShape(got))
}

func TestStderrIsComparedByShape(t *testing.T) {
	recorded := []byte("pingbus: hook stop: replacing the guard file: {world}/x is malformed\n")
	cases := []struct {
		got  string
		want bool
	}{
		{"pingbus: hook stop: replacing the guard file because it is malformed\n", true},
		{"pingbus: hook stop: the guard file was replaced\n", false},
		{"pingbus: hook prompt: replacing the guard file: x\n", false},
		{"", false},
		{"pingbus: hook stop: replacing the guard file\nsecond line\n", false},
	}
	for _, c := range cases {
		if got := stderrMatches(recorded, []byte(c.got)); got != c.want {
			t.Errorf("%q against the record: %v, want %v", c.got, got, c.want)
		}
	}
	if !stderrMatches(nil, nil) || stderrMatches(nil, []byte("x\n")) {
		t.Error("empty must stay empty")
	}
	usage := []byte("usage: pingbus hook [-h] EVENT\npingbus hook: error: argument EVENT: invalid choice\n")
	if !stderrMatches(usage, []byte("usage: pingbus hook EVENT\npingbus hook: error: argument EVENT is not a hook\n")) {
		t.Error("a line without the hook prefix is compared by its first words")
	}
}

// deviations is testdata/deviations.json.
type deviations struct {
	V            int           `json:"v"`
	DroppedFiles []droppedFile `json:"dropped_files"`
	Pinned       []pinnedStep  `json:"pinned"`
}

type droppedFile struct {
	Path     string `json:"path"`
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

type pinnedStep struct {
	Scenario string `json:"scenario"`
	Step     int    `json:"step"`
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

func (d deviations) pinned(name string, n int) bool {
	return slices.ContainsFunc(d.Pinned, func(p pinnedStep) bool { return p.Scenario == name && p.Step == n })
}

func (d deviations) anyPinned(name string) bool {
	return slices.ContainsFunc(d.Pinned, func(p pinnedStep) bool { return p.Scenario == name })
}

// dropFiles is a recorded files.json without the files the Go no longer writes.
func (d deviations) dropFiles(recorded []byte) ([]byte, error) {
	var entries []snapEntry
	if err := decodeStrict(recorded, &entries, "files.json"); err != nil {
		return nil, err
	}
	kept := slices.DeleteFunc(entries, func(e snapEntry) bool {
		return slices.ContainsFunc(d.DroppedFiles, func(f droppedFile) bool { return f.Path == e.Path })
	})
	return encodeJSON(kept)
}

func loadDeviations(t *testing.T, file scenarioFile) deviations {
	t.Helper()
	raw, err := fs.ReadFile(os.DirFS("testdata"), "deviations.json")
	if err != nil {
		t.Fatal(err)
	}
	var devs deviations
	if err := decodeStrict(raw, &devs, "deviations.json"); err != nil {
		t.Fatal(err)
	}
	if devs.V != 1 {
		t.Fatalf("deviations.json: version %d, want 1", devs.V)
	}
	for _, p := range devs.Pinned {
		if p.Decision == "" || p.Reason == "" {
			t.Fatalf("deviations.json: %s step %d needs a decision and a reason", p.Scenario, p.Step)
		}
		i := slices.IndexFunc(file.Scenarios, func(sc scenario) bool { return sc.Name == p.Scenario })
		if i < 0 || p.Step < 1 || p.Step > len(file.Scenarios[i].Steps) {
			t.Fatalf("deviations.json: %s step %d is not a scenario's step", p.Scenario, p.Step)
		}
	}
	for _, f := range devs.DroppedFiles {
		if f.Path == "" || f.Decision == "" || f.Reason == "" {
			t.Fatalf("deviations.json: a dropped file needs a path, a decision and a reason")
		}
	}
	return devs
}

// Every pinned step has its departure on file, so the Go is never judged against nothing.
func TestEveryPinnedStepHasItsRecord(t *testing.T) {
	devs := loadDeviations(t, loadScenarios(t))
	for _, p := range devs.Pinned {
		for _, ext := range recordExts {
			path := filepath.Join(deviationDir, p.Scenario, fmt.Sprintf("%d.%s", p.Step, ext))
			if _, err := fs.Stat(os.DirFS("testdata"), path); err != nil {
				t.Errorf("%s: %v", path, err)
			}
		}
	}
}

func TestDroppedFilesLeaveTheRest(t *testing.T) {
	devs := deviations{DroppedFiles: []droppedFile{{Path: "pingbus/goodbye.json"}}}
	in, err := encodeJSON([]snapEntry{
		{Path: "pingbus", Kind: "dir", Mode: "0700"},
		{Path: "pingbus/goodbye.json", Kind: "file", Mode: "0600"},
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := devs.dropFiles(in)
	if err != nil {
		t.Fatal(err)
	}
	want, err := encodeJSON([]snapEntry{{Path: "pingbus", Kind: "dir", Mode: "0700"}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, want) {
		t.Errorf("got %s, want %s", out, want)
	}
	if _, err := devs.dropFiles([]byte("{")); err == nil {
		t.Error("a malformed record must be refused")
	}
}

func rendererFor(t *testing.T) (renderer, bool) {
	t.Helper()
	switch *implFlag {
	case "":
		return goRender, false
	case "python":
		return execRender(pythonCommand(t), true), true
	default:
		return execRender(strings.Fields(*implFlag), false), false
	}
}

func encodeJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", " ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
