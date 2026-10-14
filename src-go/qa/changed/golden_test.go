package changed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// The golden replay: scenarios.json is a corpus of command lines over a small world
// (world.json), and golden/<scenario>.json is what the shipped bash qa-changed printed for each,
// recorded from a git revision by CLAUDE/Plan/00192-.../qa-changed-golden/capture.py under stub
// tools that log their argv. This test runs the Go over the same world with a runner that
// behaves like those stubs and compares stdout, stderr, the exit status and the argv log. A
// difference is allowed only as a numbered bug of SPEC-qa-changed.md section 10, pinned in
// deviations.json with the reason and in deviation/<scenario>.json (the output the Go is
// expected to give). -update-deviations rewrites the pinned files for the entries listed.

var updateDeviations = flag.Bool("update-deviations", false, "rewrite testdata/deviation for the scenarios listed in deviations.json")

type goldenScenario struct {
	Name            string            `json:"name"`
	Args            []string          `json:"args"`
	Setup           goldenSetup       `json:"setup"`
	Exits           map[string]int    `json:"exits"`
	Missing         []string          `json:"missing"`
	SemgrepFindings []json.RawMessage `json:"semgrepFindings"`
}

type goldenSetup struct {
	Write  map[string]string `json:"write"`
	Delete []string          `json:"delete"`
	Rename [][2]string       `json:"rename"`
	Exec   []string          `json:"exec"`
	Stage  bool              `json:"stage"`
	Commit bool              `json:"commit"`
}

type goldenRecord struct {
	Args   []string `json:"args,omitempty"`
	Stdout string   `json:"stdout"`
	Stderr string   `json:"stderr"`
	Status int      `json:"status"`
	Log    []string `json:"log"`
}

func (r goldenRecord) String() string {
	return fmt.Sprintf("status %d\nstdout:\n%s\nstderr:\n%s\nlog: %q", r.Status, r.Stdout, r.Stderr, r.Log)
}

func (r goldenRecord) sameAs(o goldenRecord) bool {
	return r.Stdout == o.Stdout && r.Stderr == o.Stderr && r.Status == o.Status && slices.Equal(r.Log, o.Log)
}

type deviation struct {
	Bugs   []string `json:"bugs"`
	Reason string   `json:"reason"`
}

func readJSON(t *testing.T, path string, into any) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, into); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func TestGoldenReplayMatchesTheBashExceptForNumberedDeviations(t *testing.T) {
	var world map[string]string
	readJSON(t, "testdata/world.json", &world)
	var corpus struct {
		Revision  string           `json:"revision"`
		Scenarios []goldenScenario `json:"scenarios"`
	}
	readJSON(t, "testdata/scenarios.json", &corpus)
	deviations := map[string]deviation{}
	readJSON(t, "testdata/deviations.json", &deviations)
	if len(corpus.Scenarios) == 0 || corpus.Revision == "" {
		t.Fatal("empty corpus")
	}
	names := map[string]bool{}
	for _, sc := range corpus.Scenarios {
		names[sc.Name] = true
		t.Run(sc.Name, func(t *testing.T) {
			var want goldenRecord
			readJSON(t, filepath.Join("testdata/golden", sc.Name+".json"), &want)
			dev, deviates := deviations[sc.Name]
			compareToGolden(t, sc.Name, want, replay(t, world, sc), dev, deviates)
		})
	}
	for name := range deviations {
		if !names[name] {
			t.Errorf("deviation %s names no scenario", name)
		}
	}
}

func compareToGolden(t *testing.T, name string, want, got goldenRecord, dev deviation, deviates bool) {
	t.Helper()
	switch same := want.sameAs(got); {
	case same && deviates:
		t.Fatalf("%s is pinned as a deviation but matches the bash: drop the entry", name)
	case same:
		return
	case !deviates:
		t.Fatalf("differs from the bash and is not a numbered deviation:\n--- bash\n%s\n--- go\n%s", want, got)
	case len(dev.Bugs) == 0 || dev.Reason == "":
		t.Fatalf("deviation %s needs bugs and a reason", name)
	}
	pinned := filepath.Join("testdata/deviation", name+".json")
	if *updateDeviations {
		if err := os.MkdirAll(filepath.Dir(pinned), 0o750); err != nil {
			t.Fatal(err)
		}
		body, err := json.MarshalIndent(got, "", " ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(pinned, append(body, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	var expected goldenRecord
	readJSON(t, pinned, &expected)
	if !expected.sameAs(got) {
		t.Fatalf("deviation %v (%s) drifted:\n--- pinned\n%s\n--- go\n%s", dev.Bugs, dev.Reason, expected, got)
	}
}

// replay builds the scenario's world, applies its setup and runs the Go over it.
func replay(t *testing.T, world map[string]string, sc goldenScenario) goldenRecord {
	t.Helper()
	root := t.TempDir()
	for rel, text := range world {
		writeFile(t, root, rel, text)
	}
	writeFile(t, root, "scripts/qa-routing.tsv", readReal(t, "qa-routing.tsv"))
	writeFile(t, root, "scripts/qa-trees.tsv", readReal(t, "qa-trees.tsv"))
	gitIn(t, root, "init", "-q")
	gitIn(t, root, "add", "-A")
	gitIn(t, root, "commit", "-q", "-m", "base")
	first := gitIn(t, root, "rev-parse", "HEAD")
	applySetup(t, root, sc.Setup)
	runner := &goldenRunner{t: t, exits: sc.Exits, findings: sc.SemgrepFindings, world: world}
	var out, errOut bytes.Buffer
	env := Env{
		Getenv: func(string) string { return "" },
		Stdout: &out,
		Stderr: &errOut,
		Runner: runner,
		LookPath: func(tool string) error {
			if slices.Contains(sc.Missing, tool) {
				return errors.New("missing")
			}
			return nil
		},
		Cwd: root,
	}
	args := make([]string, 0, 2+len(sc.Args))
	args = append(args, "--scripts", filepath.Join(root, "scripts"))
	for _, a := range sc.Args {
		args = append(args, strings.NewReplacer("{ROOT}", root, "{FIRST}", first).Replace(a))
	}
	code := Main(context.Background(), args, env)
	explicit := slices.Contains(sc.Args, "--files")
	norm := func(s string) string { return normaliseGolden(strings.ReplaceAll(s, first, "<FIRST>"), root, explicit) }
	log := make([]string, 0, len(runner.log))
	for _, line := range runner.log {
		log = append(log, normaliseGolden(line, root, explicit))
	}
	return goldenRecord{Stdout: norm(out.String()), Stderr: norm(errOut.String()), Status: int(code), Log: log}
}

var (
	jsonOutArg = regexp.MustCompile(`--json-output \S+`)
	tmpPath    = regexp.MustCompile(`/tmp/[A-Za-z0-9._-]+`)
)

var explicitPlanHeader = regexp.MustCompile(`plan: ([0-9]+) changed file\(s\) \(explicit\)`)

// normaliseGolden writes the temporary root as <ROOT>, and puts the bash's wording back for the
// one text change that touches every --files run (and only those: a git-collected run that
// printed the explicit header would differ from the bash and fail the replay): the bash said "vs HEAD" for a list that is
// diffed against nothing (SPEC-qa-changed.md bug B16, fixed on purpose), so the Go's
// "(explicit)" is mapped to it here rather than pinned in a deviation file for each scenario.
func normaliseGolden(text, root string, explicit bool) string {
	if explicit {
		text = explicitPlanHeader.ReplaceAllString(text, "plan: $1 changed file(s) vs HEAD")
	}
	text = strings.ReplaceAll(text, root, "<ROOT>")
	text = jsonOutArg.ReplaceAllString(text, "--json-output <JSON>")
	return tmpPath.ReplaceAllString(text, "<TMP>")
}

func applySetup(t *testing.T, root string, s goldenSetup) {
	t.Helper()
	for rel, text := range s.Write {
		writeFile(t, root, rel, text)
	}
	for _, rel := range s.Exec {
		if err := os.Chmod(filepath.Join(root, rel), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range s.Delete {
		if err := os.Remove(filepath.Join(root, rel)); err != nil {
			t.Fatal(err)
		}
	}
	for _, pair := range s.Rename {
		gitIn(t, root, "mv", pair[0], pair[1])
	}
	if s.Stage || s.Commit {
		gitIn(t, root, "add", "-A")
	}
	if s.Commit {
		gitIn(t, root, "commit", "-q", "-m", "second")
	}
}

// goldenRunner stands in for the stub tools of the capture: version control, the go tool and
// bash -n run for real; the other tools and the gate and suite scripts log their argv and exit
// as the scenario says.
type goldenRunner struct {
	t        *testing.T
	exits    map[string]int
	findings []json.RawMessage
	world    map[string]string
	log      []string
}

var suiteCount = regexp.MustCompile(`passed: ([0-9]+) failed`)

func (r *goldenRunner) Run(ctx context.Context, c proc.Cmd) (proc.Result, error) {
	name := filepath.Base(c.Argv[0])
	switch {
	case name == "git" || name == "go" || (name == "bash" && len(c.Argv) > 1 && c.Argv[1] == "-n"):
		return proc.OS{}.Run(ctx, c)
	case name == "bash":
		return r.script(c), nil
	case name == "semgrep":
		return r.semgrep(c), nil
	}
	line := name + " " + strings.Join(c.Argv[1:], " ")
	switch name {
	case "python3":
		line += " | GIT_CONFIG_GLOBAL=" + envOf(c.Env, "GIT_CONFIG_GLOBAL") + " GIT_CONFIG_NOSYSTEM=" + envOf(c.Env, "GIT_CONFIG_NOSYSTEM")
	case "ansible-playbook":
		line += " | ANSIBLE_CONFIG=" + envOf(c.Env, "ANSIBLE_CONFIG")
	case "eslint":
		line += " in " + filepath.Base(c.Dir)
	}
	r.log = append(r.log, line)
	return r.exit(c, name), nil
}

func envOf(env []string, key string) string {
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, key+"="); ok {
			return v
		}
	}
	return ""
}

// exit is the Result of a stub that prints a failure message when told to fail.
func (r *goldenRunner) exit(c proc.Cmd, name string) proc.Result {
	res := proc.Result{Code: r.exits[name]}
	if res.Code != 0 {
		if c.Merge {
			res.Stdout = name + ": stub failure\n"
		} else {
			res.Stderr = name + ": stub failure\n"
		}
	}
	return res
}

func (r *goldenRunner) script(c proc.Cmd) proc.Result {
	name := filepath.Base(c.Argv[1])
	if !strings.HasPrefix(name, "test-") {
		jsonFlag := ""
		if envOf(c.Env, "QA_JSON_OUT") != "" {
			jsonFlag = "1"
		}
		r.log = append(r.log, "gate:"+name+" json="+jsonFlag)
		return r.exit(c, name)
	}
	r.log = append(r.log, "suite:"+name)
	res := r.exit(c, name)
	if res.Code == 0 {
		count := "0"
		if m := suiteCount.FindStringSubmatch(r.world["scripts/"+name]); m != nil {
			count = m[1]
		}
		res.Stdout = "passed: " + count + " failed: 0\n"
	}
	return res
}

func (r *goldenRunner) semgrep(c proc.Cmd) proc.Result {
	var mirror []string
	err := filepath.WalkDir(c.Dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return walkErr
		}
		rel, relErr := filepath.Rel(c.Dir, path)
		mirror = append(mirror, "./"+rel)
		return relErr
	})
	if err != nil {
		r.t.Fatal(err)
	}
	slices.Sort(mirror)
	r.log = append(r.log, "semgrep "+strings.Join(c.Argv[1:], " ")+" | mirror: "+strings.Join(mirror, " ")+" ")
	report := `{"results":[]}`
	if len(r.findings) > 0 {
		body, marshalErr := json.Marshal(map[string]any{"results": r.findings})
		if marshalErr != nil {
			r.t.Fatal(marshalErr)
		}
		report = string(body)
	}
	if writeErr := os.WriteFile(c.Argv[slices.Index(c.Argv, "--json-output")+1], []byte(report), 0o600); writeErr != nil {
		r.t.Fatal(writeErr)
	}
	return r.exit(c, "semgrep")
}
