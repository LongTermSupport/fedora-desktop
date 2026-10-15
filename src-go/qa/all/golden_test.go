package all

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/gates"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// The golden replay: scenarios.json is a corpus of runs over a world of stub gates (defaults
// gives each gate's passing answer, each scenario overrides some), and golden/<scenario>.json is
// what the shipped bash qa-all.bash printed for each, recorded from a git revision by
// CLAUDE/Plan/00192-.../qa-all-golden/capture.py. This test runs the Go over the same world with
// a runner that behaves like those stubs and compares stdout, stderr, the exit status, the order
// the gates ran in and the machine report. A difference is allowed only as a numbered bug of
// SPEC-qa-all.md section 10, pinned in deviations.json with the reason and in deviation/<name>.json
// (the output the Go is expected to give). -update-deviations rewrites the pinned files for the
// entries listed.

var updateDeviations = flag.Bool("update-deviations", false, "rewrite testdata/deviation for the scenarios listed in deviations.json")

// stubReply is one gate's answer; a nil field is unset (a stub with no such file).
type stubReply struct {
	Out    *string `json:"out"`
	Err    *string `json:"err"`
	Code   *int    `json:"code"`
	Report *string `json:"report"`
	Counts *string `json:"counts"`
}

func (r stubReply) over(o stubReply) stubReply {
	for _, pair := range [][2]**string{{&r.Out, &o.Out}, {&r.Err, &o.Err}, {&r.Report, &o.Report}, {&r.Counts, &o.Counts}} {
		if *pair[1] != nil {
			*pair[0] = *pair[1]
		}
	}
	if o.Code != nil {
		r.Code = o.Code
	}
	return r
}

type goldenScenario struct {
	Name        string               `json:"name"`
	Args        []string             `json:"args"`
	Replies     map[string]stubReply `json:"replies"`
	Stale       bool                 `json:"stale"`
	EmptyCounts bool                 `json:"emptyCounts"`
	EmptyReport string               `json:"emptyReport"`
}

type goldenRecord struct {
	Args   []string `json:"args,omitempty"`
	Stdout string   `json:"stdout"`
	Stderr string   `json:"stderr"`
	Status int      `json:"status"`
	Log    []string `json:"log"`
	Report *string  `json:"report"`
}

func (r goldenRecord) String() string {
	report := "<none>"
	if r.Report != nil {
		report = *r.Report
	}
	return fmt.Sprintf("status %d\nstdout:\n%s\nstderr:\n%s\nlog: %q\nreport:\n%s", r.Status, r.Stdout, r.Stderr, r.Log, report)
}

func (r goldenRecord) sameAs(o goldenRecord) bool {
	return r.Stdout == o.Stdout && r.Stderr == o.Stderr && r.Status == o.Status && slices.Equal(r.Log, o.Log) &&
		(r.Report == nil) == (o.Report == nil) && (r.Report == nil || *r.Report == *o.Report)
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
	var corpus struct {
		Revision  string               `json:"revision"`
		Defaults  map[string]stubReply `json:"defaults"`
		Scenarios []goldenScenario     `json:"scenarios"`
	}
	readJSON(t, "testdata/scenarios.json", &corpus)
	deviations := map[string]deviation{}
	readJSON(t, "testdata/deviations.json", &deviations)
	if len(corpus.Scenarios) == 0 || corpus.Revision == "" || len(corpus.Defaults) == 0 {
		t.Fatal("empty corpus")
	}
	names := map[string]bool{}
	for _, sc := range corpus.Scenarios {
		names[sc.Name] = true
		t.Run(sc.Name, func(t *testing.T) {
			var want goldenRecord
			readJSON(t, filepath.Join("testdata/golden", sc.Name+".json"), &want)
			dev, deviates := deviations[sc.Name]
			compareToGolden(t, sc.Name, want, replay(t, corpus.Defaults, sc), dev, deviates)
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

var (
	tokenText = regexp.MustCompile(`qa-all-[0-9]+-[0-9]+`)
	tmpFile   = regexp.MustCompile(`\S*/qa-all-[0-9]+`)
)

// replay builds the scenario's world and runs the Go over it.
func replay(t *testing.T, defaults map[string]stubReply, sc goldenScenario) goldenRecord {
	t.Helper()
	body, gs := tableAsRecorded(t)
	w := newWorld(t, body)
	results := w.env["QA_RESULTS_JSON"]
	if sc.Stale {
		if err := os.WriteFile(results, []byte(`{"status":"pass"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runner := &goldenRunner{t: t, gates: gs, root: w.root, replies: map[string]stubReply{}, emptyCounts: sc.EmptyCounts, emptyReport: sc.EmptyReport}
	for _, g := range gs {
		if isAddedAfterPort(g.Name) {
			passing := "passed: 1\n"
			runner.replies[g.Name] = stubReply{Out: &passing}
			continue
		}
		runner.replies[g.Name] = defaults[g.Name].over(sc.Replies[g.Name])
	}
	var out, errOut bytes.Buffer
	env := Env{
		Getenv:   func(k string) string { return w.env[k] },
		Stdout:   &out,
		Stderr:   &errOut,
		Runner:   runner,
		Cwd:      w.root,
		NewToken: func() string { return "qa-all-1-2" },
	}
	// The bash ignored its arguments (bug B1), so the same ones are handed to the Go as they were.
	code := Main(context.Background(), append([]string{"--root", w.root}, sc.Args...), env)
	norm := func(s string) string {
		s = strings.ReplaceAll(strings.ReplaceAll(s, results, "<REPORT>"), w.root, "<ROOT>")
		return tmpFile.ReplaceAllString(tokenText.ReplaceAllString(s, "<TOKEN>"), "<TMP>")
	}
	record := goldenRecord{Stdout: withoutAddedGates(norm(out.String())), Stderr: norm(errOut.String()), Status: int(code), Log: make([]string, 0, len(runner.log))}
	for _, line := range runner.log {
		record.Log = append(record.Log, norm(line))
	}
	if raw, err := os.ReadFile(results); err == nil {
		text := norm(string(raw))
		record.Report = &text
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return record
}

// goldenRunner stands in for the stub gates of the capture: each logs its name and arguments and
// answers from the scenario, writing the report or the counts file its stub would.
type goldenRunner struct {
	t           *testing.T
	gates       []gates.Gate
	root        string
	replies     map[string]stubReply
	emptyCounts bool
	emptyReport string
	log         []string
}

// Run implements proc.Runner.
func (r *goldenRunner) Run(_ context.Context, c proc.Cmd) (proc.Result, error) {
	g := r.gateFor(c.Argv)
	reply := r.replies[g.Name]
	var line string
	if name, ok := g.Script(); ok {
		// The stub logs "$name $*${QA_JSON_OUT:+ json}": a space after the name even with no arguments.
		line = strings.TrimSuffix(name, ".bash") + " " + strings.Join(c.Argv[2:], " ")
		if envOf(c.Env, "QA_JSON_OUT") != "" {
			line += " json"
		}
	} else {
		line = strings.Join(c.Argv, " ")
	}
	if !isAddedAfterPort(g.Name) {
		r.log = append(r.log, line)
	}
	r.writeFiles(c, g, reply)
	res := proc.Result{Code: orZero(reply.Code)}
	out, errOut := orEmpty(reply.Out), orEmpty(reply.Err)
	if c.Merge {
		res.Stdout = out + errOut
		return res, nil
	}
	res.Stdout, res.Stderr = r.deliver(c.StdoutTo, out), r.deliver(c.StderrTo, errOut)
	return res, nil
}

func orZero(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func orEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func (r *goldenRunner) gateFor(argv []string) gates.Gate {
	for _, g := range r.gates {
		want := g.Command(r.root)
		if len(argv) >= len(want) && slices.Equal(argv[:len(want)], want) {
			return g
		}
	}
	r.t.Fatalf("no gate runs %v", argv)
	return gates.Gate{}
}

// writeFiles does what the stub does with QA_JSON_OUT and --counts-file.
func (r *goldenRunner) writeFiles(c proc.Cmd, g gates.Gate, reply stubReply) {
	if path := envOf(c.Env, "QA_JSON_OUT"); path != "" && reply.Report != nil && r.emptyReport != g.Name {
		r.write(path, *reply.Report)
	}
	if path := argAfter(c.Argv, "--counts-file"); path != "" && reply.Counts != nil && !r.emptyCounts {
		r.write(path, strings.ReplaceAll(*reply.Counts, "%TOKEN%", argAfter(c.Argv, "--counts-token")))
	}
}

func (r *goldenRunner) write(path, body string) {
	r.t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

// deliver streams to the writer when the command has one, else hands the text back.
func (r *goldenRunner) deliver(to io.Writer, text string) string {
	r.t.Helper()
	if to == nil {
		return text
	}
	if _, err := io.WriteString(to, text); err != nil {
		r.t.Fatal(err)
	}
	return ""
}
