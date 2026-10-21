// Package difftest is the ccy launcher's differential harness (Plan 00191 Task 4.4,
// ASSESS-ccy-launcher.md sections 7 and 8, item 0.2). It is test only.
//
// It is a recorded oracle on purpose, an exception to "do not record the bash's output" in
// src-go/CLAUDE.md: the launcher's main path has no black-box test, and a port at the same
// install path (decision D1) is gated on doing what the bash does. The goldens are recorded from
// the bash launcher, and the same matrix is then run against a Go launcher.
//
// testdata/scenarios.json is the source of truth: reusable worlds (files, environment, the
// answers the fakes give, a git remote) and scenarios built from them (arguments, the directory
// a launch starts in, stdin). Each scenario runs the launcher once in a sandbox of its own: a
// scratch HOME, a git repository with one commit, a directory in no repository, TMPDIR and
// XDG_RUNTIME_DIR, and a PATH that is only the sandbox's bin directory. In it the observed
// programs (podman, docker, tmux, gh, curl, ssh-add, ...) are fakes that record their argv and
// the names, never the values, of the environment the launcher added, and answer from the
// scenario's rules; the quiet fakes (hostname, id) answer without being recorded; date is the
// real date with "now" pinned to the matrix's clock; a short list of pure tools runs for real.
// Nothing reaches a real engine, the network or a real token.
//
// testdata/golden/<scenario>.txt is the exit status, stdout, stderr, the calls in order, and
// every file the run left in the sandbox with its mode and content, with the sandbox's path, the
// launcher's pid, its version and its hash written as {sandbox}, {pid}, {ccy_version} and
// {ccy_hash}. A launcher passes when every scenario's text is byte for byte its golden.
//
// LAUNCHER=<path> runs the matrix against that program; unset, it runs the checkout's bash
// launcher, installed into a scratch directory as the play lays it out. `-update` re-records
// the goldens from the launcher under test; review the diff before committing it.
// TestOracleCanFail proves the matrix fails a launcher that differs by one flag.
package difftest

import (
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "re-record testdata/golden/ from the launcher under test")

func TestMain(m *testing.M) {
	if tool, ok := fakeTool(); ok {
		code := runFake(tool)
		// A coverage build's runtime may write a warning to stderr at exit, which the launcher
		// may be reading: the fake's answer is complete, so nothing more may reach it.
		if err := silenceStderr(); err != nil {
			os.Exit(fakeBroken)
		}
		os.Exit(code)
	}
	os.Exit(m.Run())
}

func goldenPath(name string) string {
	return filepath.Join("testdata", "golden", name+".txt")
}

// TestLauncherParity runs every scenario and compares it with its golden.
func TestLauncherParity(t *testing.T) {
	m := loadMatrix(t)
	l := probe(t, m, launcherUnderTest(t))
	for _, sc := range m.Scenarios {
		t.Run(sc.Name, func(t *testing.T) {
			t.Parallel()
			got := runScenario(t, m, l, sc)
			checkRun(t, m, sc, got)
			text := got.render()
			if *update {
				writeFile(t, goldenPath(sc.Name), text, libMode)
				return
			}
			want := readFile(t, goldenPath(sc.Name))
			if text != want {
				t.Errorf("scenario %s (%s) differs from its golden:\n%s", sc.Name, sc.About, lineDiff(want, text))
			}
		})
	}
}

// checkRun holds what the golden alone would not say loudly: the sandbox had every program the
// launcher ran, and no secret reached an argv.
func checkRun(t *testing.T, m matrix, sc scenario, got outcome) {
	t.Helper()
	for line := range strings.SplitSeq(got.stderr, "\n") {
		if strings.Contains(line, "command not found") || strings.Contains(line, "No such file or directory") && strings.Contains(line, "/bin/") {
			t.Errorf("scenario %s ran a program the sandbox does not have (add it to the fakes or the real tools): %s", sc.Name, line)
		}
		if strings.Contains(line, "difftest fake") {
			t.Errorf("scenario %s: a fake failed: %s", sc.Name, line)
		}
	}
	for _, c := range got.calls {
		argv := strings.Join(c.Args, " ")
		for _, private := range m.privateValues() {
			if strings.Contains(argv, private) {
				t.Errorf("scenario %s: a secret reached the argv of %s", sc.Name, c.Tool)
			}
		}
	}
}

// TestScenariosWellFormed holds the matrix and the goldens to each other: unique names, an area
// for every scenario, known worlds, and no golden without a scenario.
func TestScenariosWellFormed(t *testing.T) {
	m := loadMatrix(t)
	names := make([]string, 0, len(m.Scenarios))
	for _, sc := range m.Scenarios {
		if slices.Contains(names, sc.Name) {
			t.Errorf("scenario %s appears twice", sc.Name)
		}
		names = append(names, sc.Name)
		if sc.Area == "" || sc.About == "" {
			t.Errorf("scenario %s needs an area and an about", sc.Name)
		}
		for _, w := range sc.Worlds {
			if _, ok := m.Worlds[w]; !ok {
				t.Errorf("scenario %s names no world %q", sc.Name, w)
			}
		}
		if _, err := os.Stat(goldenPath(sc.Name)); err != nil && !*update {
			t.Errorf("scenario %s has no golden: %v", sc.Name, err)
		}
	}
	goldens, err := filepath.Glob(goldenPath("*"))
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range goldens {
		if name := strings.TrimSuffix(filepath.Base(g), ".txt"); !slices.Contains(names, name) {
			t.Errorf("golden %s has no scenario", g)
		}
	}
}

// mutation is one deliberate change to the bash launcher and a scenario that must then fail.
type mutation struct {
	about, from, to, scenario string
}

// mutations prove the oracle can fail: each changes one flag of the launcher, in the text a
// launch path builds, and the named scenario must no longer match its golden.
var mutations = []mutation{
	{
		about:    "the container command loses --dangerously-skip-permissions",
		from:     `    claude --dangerously-skip-permissions "${CLAUDE_CMD_ARGS[@]}"`,
		to:       `    claude "${CLAUDE_CMD_ARGS[@]}"`,
		scenario: "launch-plain",
	},
	{
		about:    "--version answers to -V instead of -v",
		from:     `[ "$1" = "-v" ]`,
		to:       `[ "$1" = "-V" ]`,
		scenario: "version-short",
	},
}

func TestOracleCanFail(t *testing.T) {
	if os.Getenv("LAUNCHER") != "" {
		t.Skip("the mutations edit the bash launcher; LAUNCHER names another program")
	}
	m := loadMatrix(t)
	for _, mu := range mutations {
		t.Run(mu.scenario, func(t *testing.T) {
			t.Parallel()
			path := installBash(t, func(text string) string {
				if strings.Count(text, mu.from) != 1 {
					t.Fatalf("mutation %q: %q is not in the launcher exactly once", mu.about, mu.from)
				}
				return strings.Replace(text, mu.from, mu.to, 1)
			})
			l := probe(t, m, path)
			idx := slices.IndexFunc(m.Scenarios, func(sc scenario) bool { return sc.Name == mu.scenario })
			if idx < 0 {
				t.Fatalf("mutation %q names no scenario %s", mu.about, mu.scenario)
			}
			got := runScenario(t, m, l, m.Scenarios[idx]).render()
			if got == readFile(t, goldenPath(mu.scenario)) {
				t.Errorf("mutation %q: scenario %s still matches its golden; the oracle cannot see it", mu.about, mu.scenario)
			}
		})
	}
}

// commonTable is, at [i][j], the length of the longest common subsequence of w[i:] and g[j:].
func commonTable(w, g []string) [][]int {
	common := make([][]int, len(w)+1)
	for i := range common {
		common[i] = make([]int, len(g)+1)
	}
	for i, wl := range slices.Backward(w) {
		row, below := common[i], common[i+1]
		for j, gl := range slices.Backward(g) {
			if wl == gl {
				row[j] = below[j+1] + 1
			} else {
				row[j] = max(below[j], row[j+1])
			}
		}
	}
	return common
}

// maxDiffLines bounds the report of one difference.
const maxDiffLines = 60

// lineDiff is the lines only the golden has (-) and only the run has (+), by a longest common
// subsequence of lines, at most maxDiffLines of them.
func lineDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	common := commonTable(w, g)
	var b strings.Builder
	shown := 0
	emit := func(mark, line string) {
		if shown < maxDiffLines {
			b.WriteString(mark + " " + line + "\n")
		}
		shown++
	}
	i, j := 0, 0
	for i < len(w) || j < len(g) {
		switch {
		case i < len(w) && j < len(g) && w[i] == g[j]:
			i, j = i+1, j+1
		case j < len(g) && (i == len(w) || common[i][j+1] >= common[i+1][j]):
			emit("+", g[j])
			j++
		default:
			emit("-", w[i])
			i++
		}
	}
	if shown > maxDiffLines {
		b.WriteString("... and " + strconv.Itoa(shown-maxDiffLines) + " more lines\n")
	}
	return b.String()
}
