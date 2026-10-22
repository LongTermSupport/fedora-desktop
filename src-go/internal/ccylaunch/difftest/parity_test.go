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
// XDG_RUNTIME_DIR, its own build context, GPU directory and zone database (the launcher's
// CCY_DOCKERFILE_DIR, CCY_GPU_DEVICE_DIR and CCY_ZONEINFO_DIR), TZ=UTC, and a PATH that is only
// the sandbox's bin directory. In it the observed programs (podman, docker, tmux, gh, curl,
// ssh-add, ...) are fakes that record their argv and the names, never the values, of the
// environment variables that program reads (readsEnv in fake_test.go says which and why: the
// names an engine call passes by name, the whole environment for tmux, a token for gh and
// claude; never bash's leftovers), and answer from the scenario's rules; the quiet fakes
// (hostname, id) answer without being recorded; date is the real date with "now" pinned to the
// matrix's clock; a short list of pure tools runs for real. Every fake also names the variables
// whose value holds a secret, and a run fails when a program that does not read that name (or
// tmux and systemd-run, which hand it to a whole server) was given one; TestSecretCheckCanFail
// proves the check sees a leak. Nothing reaches a real engine, the network or a real token. The
// machine still answers two things: /etc/localtime, which the launcher always reads (readlink,
// when it is a link) but uses only when timedatectl names no zone (every world's timedatectl names one), and the
// real tools' own installation.
//
// testdata/golden/<scenario>.txt is the exit status, stdout, stderr, the calls in order, and
// every file the run left in the sandbox with its mode and content, with the sandbox's path, the
// launcher's pid, its version and its hash written as {sandbox}, {pid}, {ccy_version} and
// {ccy_hash}. A launcher passes when every scenario's text is byte for byte its golden.
//
// A Go launcher tells the time by the matrix's clock only when built for the harness: the
// in-process front reads it (pinclock), and a built claude-yolo needs `go build -tags difftest`
// (cmd/claude-yolo/clock_difftest.go), a build that refuses to run outside a sandbox.
//
// LAUNCHER=<path> runs the matrix against that program; unset, it runs the checkout's bash
// launcher, installed into a scratch directory as the play lays it out. `-update` re-records
// the goldens from the bash launcher, and refuses with LAUNCHER set: the goldens are the bash's
// behaviour, never another program's. Review the diff before committing it.
// TestOracleCanFail proves the matrix fails a launcher that differs by one flag, on the line
// that flag is on; it does not run under -update, which is rewriting the goldens it reads.
package difftest

import (
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ccylaunch"
)

var update = flag.Bool("update", false, "re-record testdata/golden/ from the launcher under test")

func TestMain(m *testing.M) {
	if isFront() {
		code := runFront()
		if err := silenceStderr(); err != nil {
			os.Exit(fakeBroken)
		}
		os.Exit(code)
	}
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
	if *update && os.Getenv("LAUNCHER") != "" {
		t.Fatal("-update records the goldens from the bash launcher; unset LAUNCHER")
	}
	m := loadMatrix(t)
	matchGoldens(t, m, probe(t, m, launcherUnderTest(t)), m.Scenarios)
}

// matchGoldens runs each scenario against l and compares it with its golden, or under -update
// records it.
func matchGoldens(t *testing.T, m matrix, l launcher, scenarios []scenario) {
	t.Helper()
	for _, sc := range scenarios {
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

// skipFront skips a test of the Go front where it cannot run: with LAUNCHER naming another
// program, or under -update, which records the bash's goldens.
func skipFront(t *testing.T) {
	t.Helper()
	if os.Getenv("LAUNCHER") != "" {
		t.Skip("LAUNCHER names the program under test")
	}
	if *update {
		t.Skip("-update records the goldens from the bash launcher")
	}
}

// TestFrontParity is the Go front's cut-over gate (Plan 00191 Task 4.5): installed as the play
// installs it, with the checkout's bash launcher beside it, it passes every scenario, the flags
// it answers itself and every run it hands to the bash.
func TestFrontParity(t *testing.T) {
	skipFront(t)
	m := loadMatrix(t)
	matchGoldens(t, m, probe(t, m, installFront(t, nil)), m.Scenarios)
}

// frontAnswers are the scenarios the front answers itself.
var frontAnswers = []string{
	"version", "version-short", "help-no-image", "help-image", "help-short-image-broken",
	"list-tokens-bare", "list-tokens-one", "list-tokens-states",
}

// frontAnswerLines are the bash launcher's lines that answer frontAnswers, each in its text once:
// TestFrontAnswersItself replaces each, so a scenario the bash answered no longer matches.
var frontAnswerLines = []string{
	`printf 'ccy version %s\nHash: %s\n' "$CCY_VERSION" "$CCY_HASH"`,
	"Claude Code YOLO Mode (ccy) - Container-based execution with --dangerously-skip-permissions",
	"if [ \"$#\" -eq 1 ] && [ \"$1\" = \"--list-tokens\" ]; then\n    list_tokens \"$TOKEN_DIR\" \"ccy\"",
}

// TestFrontAnswersItself proves the front's answers are its own: with the bash launcher's
// --version and --help answers broken, those scenarios still match, so the bash never ran them.
func TestFrontAnswersItself(t *testing.T) {
	skipFront(t)
	m := loadMatrix(t)
	path := installFront(t, func(text string) string {
		for _, from := range frontAnswerLines {
			if strings.Count(text, from) != 1 {
				t.Fatalf("%q is not in the launcher exactly once", from)
			}
			text = strings.Replace(text, from, `echo "the bash answered"`, 1)
		}
		return text
	})
	scenarios := make([]scenario, 0, len(frontAnswers))
	for _, name := range frontAnswers {
		scenarios = append(scenarios, scenarioNamed(t, m, name))
	}
	matchGoldens(t, m, probe(t, m, path), scenarios)
}

// TestFrontHandsOver proves a launch reaches the bash launcher beside the front: the launch
// mutation in that copy fails launch-plain on the line it changed.
func TestFrontHandsOver(t *testing.T) {
	skipFront(t)
	m := loadMatrix(t)
	mu := mutations[0]
	path := installFront(t, func(text string) string { return strings.Replace(text, mu.from, mu.to, 1) })
	got := runScenario(t, m, probe(t, m, path), scenarioNamed(t, m, mu.scenario)).render()
	if want := readFile(t, goldenPath(mu.scenario)); !lostLine(want, got, mu.shows) {
		t.Fatalf("with the bash launcher beside the front changed, %s still shows %q:\n%s", mu.scenario, mu.shows, lineDiff(want, got))
	}
}

// checkRun holds what the golden alone would not say loudly: the sandbox had every program the
// launcher ran, no secret reached an argv, and a secret reached the environment only of a
// program that reads it by that name (secretAllowed).
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
	for _, leak := range secretLeaks(got) {
		t.Errorf("scenario %s: %s", sc.Name, leak)
	}
	for _, leak := range frontLeaks(got) {
		t.Errorf("scenario %s: %s", sc.Name, leak)
	}
}

// frontLeaks are the calls whose environment still held the front's hand-over variable.
func frontLeaks(got outcome) []string {
	var leaks []string
	for _, c := range got.calls {
		if c.Front {
			leaks = append(leaks, c.Tool+" inherited "+ccylaunch.FrontEnv+": "+strings.Join(c.Args, " "))
		}
	}
	return leaks
}

// secretLeaks are the calls that were given a secret in a variable they do not read.
func secretLeaks(got outcome) []string {
	var leaks []string
	for _, c := range got.calls {
		for _, name := range c.Carries {
			if !secretAllowed(c, name) {
				leaks = append(leaks, c.Tool+" was given the secret in "+name+", which it does not read: "+strings.Join(c.Args, " "))
			}
		}
	}
	return leaks
}

// scenarioNamed is the matrix's scenario of that name.
func scenarioNamed(t *testing.T, m matrix, name string) scenario {
	t.Helper()
	idx := slices.IndexFunc(m.Scenarios, func(sc scenario) bool { return sc.Name == name })
	if idx < 0 {
		t.Fatalf("the matrix has no scenario %s", name)
	}
	return m.Scenarios[idx]
}

// unexportLine is the launcher's line that keeps a caller's exported token out of every program
// it starts; TestSecretCheckCanFail takes it away.
const unexportLine = "export -n GH_TOKEN CLAUDE_CODE_OAUTH_TOKEN CLAUDE_OAUTH_TOKEN\n"

// TestLaunchHandsTokensOn holds the other side of the narrowing: the session's container run is
// still given both tokens' values, by name, whether gh's login, the caller's GH_TOKEN or the
// file the outer launch hands to the one it re-runs inside tmux supplied the GitHub one, and
// that file is gone afterwards. A golden names the variables only, so it would pass an empty one.
func TestLaunchHandsTokensOn(t *testing.T) {
	m := loadMatrix(t)
	l := probe(t, m, launcherUnderTest(t))
	for _, name := range []string{"launch-plain", "launch-gh-token-env", "launch-gh-token-handoff", "launch-headless", "launch-quick-replay"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := runScenario(t, m, l, scenarioNamed(t, m, name))
			idx := slices.IndexFunc(got.calls, func(c call) bool {
				return c.Tool == "podman" && len(c.Args) > 0 && c.Args[0] == "run" && slices.Contains(c.Args, "--dangerously-skip-permissions")
			})
			if idx < 0 {
				t.Fatalf("scenario %s started no session container", name)
			}
			for _, file := range got.files {
				if strings.Contains(file, "ccy-gh-token.") {
					t.Errorf("scenario %s left the GitHub token hand-off behind: %s", name, file)
				}
			}
			run := got.calls[idx]
			for _, secret := range []string{"CLAUDE_CODE_OAUTH_TOKEN", "GH_TOKEN"} {
				if !slices.Contains(run.Carries, secret) || !slices.Contains(passedByName(run.Args), secret) {
					t.Errorf("scenario %s: the session container was not given %s by name (carries %q)", name, secret, run.Carries)
				}
			}
		})
	}
}

// TestSecretCheckCanFail proves checkRun's secret check sees a token handed to every child: with
// the caller's GH_TOKEN left exported, getenforce (the first program the launch starts) has it.
func TestSecretCheckCanFail(t *testing.T) {
	if os.Getenv("LAUNCHER") != "" {
		t.Skip("the mutation edits the bash launcher; LAUNCHER names another program")
	}
	if *update {
		t.Skip("-update is rewriting the goldens")
	}
	t.Parallel()
	m := loadMatrix(t)
	path := installBash(t, func(text string) string {
		if strings.Count(text, unexportLine) != 1 {
			t.Fatalf("%q is not in the launcher exactly once", unexportLine)
		}
		return strings.Replace(text, unexportLine, "", 1)
	})
	got := runScenario(t, m, probe(t, m, path), scenarioNamed(t, m, "launch-gh-token-env"))
	leaks := secretLeaks(got)
	if !slices.ContainsFunc(leaks, func(l string) bool { return strings.HasPrefix(l, "getenforce was given the secret in GH_TOKEN") }) {
		t.Errorf("with the caller's GH_TOKEN left exported, getenforce was not reported; leaks: %q", leaks)
	}
}

// handoffSource is the launcher's line that takes what a starting program hands it, the front's
// variable included (lib/token-handoff.bash unsets it).
const handoffSource = "source \"$SCRIPT_DIR/lib/token-handoff.bash\" || exit 1\n"

// TestFrontLeakCheckCanFail proves checkRun sees the front's variable reach a child: with the
// bash beside the front exporting it again after the library unset it, getenforce has it.
func TestFrontLeakCheckCanFail(t *testing.T) {
	skipFront(t)
	t.Parallel()
	m := loadMatrix(t)
	path := installFront(t, func(text string) string {
		if strings.Count(text, handoffSource) != 1 {
			t.Fatalf("%q is not in the launcher exactly once", handoffSource)
		}
		return strings.Replace(text, handoffSource, handoffSource+"export "+ccylaunch.FrontEnv+"=\"$CCY_LAUNCHER_PATH\"\n", 1)
	})
	leaks := frontLeaks(runScenario(t, m, probe(t, m, path), scenarioNamed(t, m, "launch-plain")))
	if !slices.ContainsFunc(leaks, func(l string) bool { return strings.HasPrefix(l, "getenforce inherited "+ccylaunch.FrontEnv) }) {
		t.Errorf("with %s exported again, getenforce was not reported; leaks: %q", ccylaunch.FrontEnv, leaks)
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

// mutation is one deliberate change to the bash launcher and a scenario that must then fail,
// on a golden line that holds shows: so a launcher that broke some other way does not pass.
type mutation struct {
	about, from, to, scenario, shows string
}

// mutations prove the oracle can fail: each changes one flag of the launcher, in the text a
// launch path builds, and the named scenario must no longer match its golden.
var mutations = []mutation{
	{
		about:    "the container command loses --dangerously-skip-permissions",
		from:     `    claude --dangerously-skip-permissions "${CLAUDE_CMD_ARGS[@]}"`,
		to:       `    claude "${CLAUDE_CMD_ARGS[@]}"`,
		scenario: "launch-plain",
		shows:    "--dangerously-skip-permissions",
	},
	{
		about:    "--version answers to -V instead of -v",
		from:     `[ "$1" = "-v" ]`,
		to:       `[ "$1" = "-V" ]`,
		scenario: "version-short",
		shows:    "ccy version {ccy_version}",
	},
}

func TestOracleCanFail(t *testing.T) {
	if os.Getenv("LAUNCHER") != "" {
		t.Skip("the mutations edit the bash launcher; LAUNCHER names another program")
	}
	if *update {
		t.Skip("-update is rewriting the goldens this test reads")
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
			got := runScenario(t, m, l, scenarioNamed(t, m, mu.scenario)).render()
			want := readFile(t, goldenPath(mu.scenario))
			if got == want {
				t.Fatalf("mutation %q: scenario %s still matches its golden; the oracle cannot see it", mu.about, mu.scenario)
			}
			if !lostLine(want, got, mu.shows) {
				t.Errorf("mutation %q: scenario %s differs, but no golden line holding %q is gone:\n%s",
					mu.about, mu.scenario, mu.shows, lineDiff(want, got))
			}
		})
	}
}

// lostLine reports whether a line of want that holds text is not in got: the difference is
// on the line the mutation changed.
func lostLine(want, got, text string) bool {
	gotLines := strings.Split(got, "\n")
	for line := range strings.SplitSeq(want, "\n") {
		if strings.Contains(line, text) && !slices.Contains(gotLines, line) {
			return true
		}
	}
	return false
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
