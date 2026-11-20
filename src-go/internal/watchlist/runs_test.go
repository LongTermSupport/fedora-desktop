package watchlist_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/watchlist"
)

func mrow(repo, group, mode, from, to, status string) string {
	return row(repo, group, mode, from, to, status)
}

func (w *world) runDir(id string) string { return filepath.Join(watchlist.RunsDir(w.root), id) }

func (w *world) writeRun(t *testing.T, id string, rows ...string) {
	t.Helper()
	writeFile(t, filepath.Join(w.runDir(id), "manifest.tsv"), "# h\n"+strings.Join(rows, ""))
}

func (w *world) manifest(t *testing.T, id string) []watchlist.RunRow {
	t.Helper()
	path := filepath.Join(w.runDir(id), "manifest.tsv")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := watchlist.ParseManifest(b, path)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// heads scripts each clone's HEAD by repository.
func (w *world) heads(t *testing.T, byRepo map[string]string) {
	t.Helper()
	w.fake.On([]string{"git", "rev-parse", "--is-shallow-repository"}, ok("false\n"))
	w.fake.OnFunc([]string{"git", "rev-parse", "--verify", "HEAD"}, func(c execx.Cmd) (execx.Result, error) {
		for repo, sha := range byRepo {
			if strings.HasSuffix(c.Dir, filepath.FromSlash(repo)) {
				return ok(sha + "\n"), nil
			}
		}
		t.Errorf("HEAD asked in %q", c.Dir)
		return refuse("no"), nil
	})
}

// studyWorld holds alpha and beta in fleets and gamma in tooling, each cloned.
func studyWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t, mitRow(alpha, "study", "fleets"), mitRow(beta, "study", "fleets"), mitRow(gamma, "study", "tooling"),
		mitRow("example-org/delta", "excluded", "-"))
	for _, r := range []string{alpha, beta, gamma} {
		w.cloneAt(t, r, "MIT", "study")
	}
	return w
}

// The manifest is read strictly: every malformed row names its line.
func TestParseManifestRejectsMalformedRows(t *testing.T) {
	for name, data := range map[string]string{
		"five fields":  row(alpha, "fleets", "baseline", "-", shaA),
		"bad repo":     mrow("alpha", "fleets", "baseline", "-", shaA, "pending"),
		"bad group":    mrow(alpha, "-", "baseline", "-", shaA, "pending"),
		"bad mode":     mrow(alpha, "fleets", "full", "-", shaA, "pending"),
		"bad from":     mrow(alpha, "fleets", "diff", "abc", shaA, "pending"),
		"diff no from": mrow(alpha, "fleets", "diff", "-", shaA, "pending"),
		"bad to":       mrow(alpha, "fleets", "baseline", "-", "-", "pending"),
		"bad status":   mrow(alpha, "fleets", "baseline", "-", shaA, "skipped"),
		"duplicate":    mrow(alpha, "fleets", "baseline", "-", shaA, "pending") + mrow(alpha, "tooling", "baseline", "-", shaA, "pending"),
	} {
		_, err := watchlist.ParseManifest([]byte("# h\n"+data), "m.tsv")
		if err == nil || !strings.Contains(err.Error(), "m.tsv:") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestFormatManifestRoundTrips(t *testing.T) {
	rows := []watchlist.RunRow{
		{Repo: beta, Group: "fleets", Mode: "diff", FromSHA: shaA, ToSHA: shaB, Status: "pending"},
		{Repo: alpha, Group: "fleets", Mode: "baseline", FromSHA: "-", ToSHA: shaA, Status: "done"},
	}
	out := watchlist.FormatManifest(rows)
	if !strings.HasPrefix(string(out), "#") {
		t.Fatalf("no header: %q", out)
	}
	back, err := watchlist.ParseManifest(out, "m")
	if err != nil || len(back) != 2 || back[0] != rows[1] || back[1] != rows[0] {
		t.Fatalf("round trip %v, %v", back, err)
	}
}

// R1: the id is <mode>-<yyyymmdd>-<hhmm> of the given mode, and new.
func TestRunNewRefusesBadIDs(t *testing.T) {
	for name, args := range map[string][]string{
		"no stamp":      {"run", "new", "baseline", "--mode", "baseline"},
		"mode mismatch": {"run", "new", "diff-20261120-1200", "--mode", "baseline"},
		"bad mode":      {"run", "new", "full-20261120-1200", "--mode", "full"},
		"bad date":      {"run", "new", "baseline-20261340-1200", "--mode", "baseline"},
		"bad time":      {"run", "new", "baseline-20261120-2561", "--mode", "baseline"},
		"suffix":        {"run", "new", "baseline-20261120-1200-x", "--mode", "baseline"},
		"exists":        {"run", "new", "baseline-20261101-0900", "--mode", "baseline"},
		"same stamp":    {"run", "new", "diff-20261101-0900", "--mode", "diff"},
	} {
		w := studyWorld(t)
		w.writeRun(t, "baseline-20261101-0900", mrow(alpha, "fleets", "baseline", "-", shaA, "done"))
		w.heads(t, map[string]string{alpha: shaA, beta: shaA, gamma: shaA})
		if code := w.run(args...); code != term.ExitFailure || !strings.HasPrefix(w.err.String(), "ERROR:") {
			t.Errorf("%s: exit %d, err %q", name, code, w.err.String())
		}
	}
}

// R2
func TestRunNewRefusesWhileARowIsNew(t *testing.T) {
	w := studyWorld(t)
	writeFile(t, w.listPath(), w.list(t)+mitRow("example-org/epsilon", "new", "-"))
	if code := w.run("run", "new", "baseline-20261120-1200", "--mode", "baseline"); code != term.ExitFailure ||
		!strings.Contains(w.err.String(), "example-org/epsilon") {
		t.Fatalf("exit %d, err %q", code, w.err.String())
	}
	if _, err := os.Stat(w.runDir("baseline-20261120-1200")); !os.IsNotExist(err) {
		t.Fatal("the run was created")
	}
}

// R3, R4, R5, R6: baseline mode takes every study row, from_sha from the newest done row of an
// earlier run, and prints the groups and their counts.
func TestRunNewBaseline(t *testing.T) {
	w := studyWorld(t)
	w.writeRun(t, "baseline-20261101-0900", mrow(alpha, "fleets", "baseline", "-", shaA, "done"), mrow(beta, "fleets", "baseline", "-", shaA, "pending"))
	w.writeRun(t, "diff-20261201-0900", mrow(alpha, "fleets", "diff", shaA, shaC, "done"))
	w.heads(t, map[string]string{alpha: shaB, beta: shaB, gamma: shaC})
	if code := w.run("run", "new", "baseline-20261120-1200", "--mode", "baseline"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if w.out.String() != "fleets 2\ntooling 1\n" {
		t.Fatalf("out %q", w.out.String())
	}
	got := w.manifest(t, "baseline-20261120-1200")
	want := []watchlist.RunRow{
		{Repo: alpha, Group: "fleets", Mode: "baseline", FromSHA: shaA, ToSHA: shaB, Status: "pending"},
		{Repo: beta, Group: "fleets", Mode: "baseline", FromSHA: "-", ToSHA: shaB, Status: "pending"},
		{Repo: gamma, Group: "tooling", Mode: "baseline", FromSHA: "-", ToSHA: shaC, Status: "pending"},
	}
	if len(got) != len(want) {
		t.Fatalf("manifest %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d: %+v, want %+v", i, got[i], want[i])
		}
	}
}

// R5: diff mode: no from_sha is baseline, an unmoved row is left out, the rest are diff.
func TestRunNewDiff(t *testing.T) {
	w := studyWorld(t)
	w.writeRun(t, "baseline-20261101-0900", mrow(alpha, "fleets", "baseline", "-", shaA, "done"), mrow(beta, "fleets", "baseline", "-", shaB, "done"))
	w.heads(t, map[string]string{alpha: shaC, beta: shaB, gamma: shaC})
	if code := w.run("run", "new", "diff-20261120-1200", "--mode", "diff"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	got := w.manifest(t, "diff-20261120-1200")
	if len(got) != 2 || got[0].Repo != alpha || got[0].Mode != "diff" || got[0].FromSHA != shaA ||
		got[1].Repo != gamma || got[1].Mode != "baseline" {
		t.Fatalf("manifest %+v", got)
	}
}

// R3: --group and --repo filter; a filter that matches no study row is an error.
func TestRunNewFilters(t *testing.T) {
	w := studyWorld(t)
	w.heads(t, map[string]string{alpha: shaA, beta: shaA, gamma: shaA})
	if code := w.run("run", "new", "baseline-20261120-1200", "--mode", "baseline", "--group", "tooling", "--repo", alpha); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	got := w.manifest(t, "baseline-20261120-1200")
	if len(got) != 2 || got[0].Repo != alpha || got[1].Repo != gamma {
		t.Fatalf("manifest %+v", got)
	}
	for name, args := range map[string][]string{
		"unknown group": {"--group", "nope"},
		"excluded repo": {"--repo", "example-org/delta"},
	} {
		w := studyWorld(t)
		w.heads(t, map[string]string{alpha: shaA, beta: shaA, gamma: shaA})
		full := append([]string{"run", "new", "baseline-20261120-1200", "--mode", "baseline"}, args...)
		if code := w.run(full...); code != term.ExitFailure {
			t.Errorf("%s: exit %d", name, code)
		}
	}
}

// R5: a diff run with nothing moved writes no run.
func TestRunNewWithNothingToStudy(t *testing.T) {
	w := studyWorld(t)
	w.writeRun(t, "baseline-20261101-0900", mrow(alpha, "fleets", "baseline", "-", shaA, "done"),
		mrow(beta, "fleets", "baseline", "-", shaA, "done"), mrow(gamma, "tooling", "baseline", "-", shaA, "done"))
	w.heads(t, map[string]string{alpha: shaA, beta: shaA, gamma: shaA})
	if code := w.run("run", "new", "diff-20261120-1200", "--mode", "diff"); code != term.ExitOK || w.out.Len() != 0 ||
		!strings.Contains(w.err.String(), "nothing") {
		t.Fatalf("exit %d, out %q, err %q", code, w.out.String(), w.err.String())
	}
	if _, err := os.Stat(w.runDir("diff-20261120-1200")); !os.IsNotExist(err) {
		t.Fatal("an empty run was created")
	}
}

// R3, G1: a study row with no clone, or a HEAD git cannot give, stops the run unwritten.
func TestRunNewNeedsEachClone(t *testing.T) {
	w := newWorld(t, mitRow(alpha, "study", "fleets"))
	if code := w.run("run", "new", "baseline-20261120-1200", "--mode", "baseline"); code != term.ExitFailure || !strings.Contains(w.err.String(), alpha) {
		t.Fatalf("exit %d, err %q", code, w.err.String())
	}
	w.cloneAt(t, alpha, "MIT", "study")
	w.fake.On([]string{"git", "rev-parse"}, refuse("fatal"))
	if code := w.run("run", "new", "baseline-20261120-1200", "--mode", "baseline"); code != term.ExitFailure || !strings.Contains(w.err.String(), alpha) {
		t.Fatalf("exit %d, err %q", code, w.err.String())
	}
	if _, err := os.Stat(w.runDir("baseline-20261120-1200")); !os.IsNotExist(err) {
		t.Fatal("the run was created")
	}
}

// R7: a shallow clone in the selection stops the run, naming every such repository.
func TestRunNewRefusesShallowClones(t *testing.T) {
	w := studyWorld(t)
	w.heads(t, map[string]string{alpha: shaA, beta: shaA, gamma: shaA})
	w.fake.OnFunc([]string{"git", "rev-parse", "--is-shallow-repository"}, func(c execx.Cmd) (execx.Result, error) {
		if strings.HasSuffix(c.Dir, "gamma") {
			return ok("false\n"), nil
		}
		return ok("true\n"), nil
	})
	if code := w.run("run", "new", "baseline-20261120-1200", "--mode", "baseline"); code != term.ExitFailure ||
		!strings.Contains(w.err.String(), alpha) || !strings.Contains(w.err.String(), beta) ||
		strings.Contains(w.err.String(), gamma) || !strings.Contains(w.err.String(), "watchlist sync") {
		t.Fatalf("exit %d, err %q", code, w.err.String())
	}
	if _, err := os.Stat(w.runDir("baseline-20261120-1200")); !os.IsNotExist(err) {
		t.Fatal("the run was created")
	}
}

// The runs this repository ships load strictly (the tree may hold none yet).
func TestTheShippedRunsLoad(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(watchlist.RunsDir(root))
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("an empty runs directory")
	}
	var out, errOut strings.Builder
	if code := watchlist.NewApp(root, &execx.Fake{}, &out, &errOut).Run(t.Context(), []string{"run", "status"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
}

// A runs directory holding anything but runs is an error, not skipped.
func TestRunsDirectoryIsReadStrictly(t *testing.T) {
	w := studyWorld(t)
	writeFile(t, filepath.Join(watchlist.RunsDir(w.root), "notes.txt"), "x")
	if code := w.run("run", "status"); code != term.ExitFailure || !strings.Contains(w.err.String(), "notes.txt") {
		t.Fatalf("exit %d, err %q", code, w.err.String())
	}
	w = studyWorld(t)
	w.writeRun(t, "baseline-20261101-0900", "broken\n")
	if code := w.run("run", "status"); code != term.ExitFailure || !strings.Contains(w.err.String(), "manifest.tsv:2") {
		t.Fatalf("exit %d, err %q", code, w.err.String())
	}
}

// W1
func TestRunRowsJoinsTheList(t *testing.T) {
	w := studyWorld(t)
	w.writeRun(t, "diff-20261120-1200", mrow(alpha, "fleets", "diff", shaA, shaB, "pending"), mrow(gamma, "tooling", "baseline", "-", shaC, "done"))
	if code := w.run("run", "rows", "diff-20261120-1200", "fleets"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	dir := watchlist.Entry{Repo: alpha, Licence: "MIT", State: "study"}.CloneDir(w.root)
	if want := row(alpha, "MIT", "lift", "diff", shaA, shaB, "pending", dir); w.out.String() != want {
		t.Fatalf("out %q\nwant %q", w.out.String(), want)
	}
	if code := w.run("run", "rows", "diff-20261120-1200"); code != term.ExitOK || strings.Count(w.out.String(), "\n") != 2 {
		t.Fatalf("exit %d, out %q", code, w.out.String())
	}
	for name, args := range map[string][]string{
		"unknown group": {"run", "rows", "diff-20261120-1200", "nope"},
		"unknown run":   {"run", "rows", "diff-20261121-1200"},
		"bad id":        {"run", "rows", "../x"},
	} {
		if code := w.run(args...); code != term.ExitFailure {
			t.Errorf("%s: exit %d", name, code)
		}
	}
	writeFile(t, w.listPath(), oddHeader+mitRow(beta, "study", "fleets"))
	if code := w.run("run", "rows", "diff-20261120-1200"); code != term.ExitFailure || !strings.Contains(w.err.String(), alpha) {
		t.Fatalf("a row off the list: exit %d, err %q", code, w.err.String())
	}
}

func (w *world) report(t *testing.T, id, group, body string) {
	t.Helper()
	writeFile(t, filepath.Join(w.runDir(id), group+".md"), body)
}

// M1, M2: every pending row of the group passes, so they become done; other groups are untouched.
func TestRunMarkMarksAGroupDone(t *testing.T) {
	w := studyWorld(t)
	id := "baseline-20261120-1200"
	w.writeRun(t, id, mrow(alpha, "fleets", "baseline", "-", shaA, "pending"), mrow(beta, "fleets", "baseline", "-", shaB, "done"),
		mrow(gamma, "tooling", "baseline", "-", shaC, "pending"))
	w.report(t, id, "fleets", "# Fleets\n\n## example-org/alpha\n\nStudied: "+shaA+" (main)\n\nText.\n")
	if code := w.run("run", "mark", id, "fleets"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if w.out.String() != "done example-org/alpha\n" {
		t.Fatalf("out %q", w.out.String())
	}
	got := w.manifest(t, id)
	if got[0].Status != "done" || got[1].Status != "done" || got[2].Status != "pending" {
		t.Fatalf("manifest %+v", got)
	}
}

// M1, M2: one failing row changes nothing, and each failure is named with its reason.
func TestRunMarkRefusesAnIncompleteReport(t *testing.T) {
	w := studyWorld(t)
	id := "baseline-20261120-1200"
	w.writeRun(t, id, mrow(alpha, "fleets", "baseline", "-", shaA, "pending"), mrow(beta, "fleets", "baseline", "-", shaB, "pending"))
	before, err := os.ReadFile(filepath.Join(w.runDir(id), "manifest.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"beta missing":       "## example-org/alpha\nStudied: " + shaA + "\n",
		"short sha":          "## example-org/alpha\nStudied: " + shaA + "\n## example-org/beta\nStudied: bbbbbbb\n",
		"studied elsewhere":  "## example-org/alpha\nStudied: " + shaA + "\n## example-org/beta\n## Other\nStudied: " + shaB + "\n",
		"not at line start":  "## example-org/alpha\nStudied: " + shaA + "\n## example-org/beta\nWe Studied: " + shaB + "\n",
		"heading too deep":   "## example-org/alpha\nStudied: " + shaA + "\n### example-org/beta\nStudied: " + shaB + "\n",
		"heading other repo": "## example-org/alpha\nStudied: " + shaA + "\n## example-org/beta-two\nStudied: " + shaB + "\n",
	} {
		w.report(t, id, "fleets", body)
		if code := w.run("run", "mark", id, "fleets"); code != term.ExitFailure || !strings.Contains(w.err.String(), beta) || strings.Contains(w.err.String(), alpha+":") {
			t.Errorf("%s: exit %d, err %q", name, code, w.err.String())
		}
		after, readErr := os.ReadFile(filepath.Join(w.runDir(id), "manifest.tsv"))
		if readErr != nil || string(after) != string(before) {
			t.Errorf("%s: the manifest changed", name)
		}
	}
	if err := os.Remove(filepath.Join(w.runDir(id), "fleets.md")); err != nil {
		t.Fatal(err)
	}
	if code := w.run("run", "mark", id, "fleets"); code != term.ExitFailure || !strings.Contains(w.err.String(), "fleets.md") {
		t.Fatalf("no report: exit %d, err %q", code, w.err.String())
	}
	if code := w.run("run", "mark", id, "nope"); code != term.ExitFailure {
		t.Fatalf("unknown group: exit %d", code)
	}
}

// U1: the newest run by its timestamp, or the one named; exit 0 with work pending.
func TestRunStatus(t *testing.T) {
	w := studyWorld(t)
	if code := w.run("run", "status"); code != term.ExitFailure || !strings.Contains(w.err.String(), "no runs") {
		t.Fatalf("no runs: exit %d, err %q", code, w.err.String())
	}
	w.writeRun(t, "diff-20261201-0900", mrow(alpha, "fleets", "baseline", "-", shaA, "pending"), mrow(beta, "fleets", "baseline", "-", shaB, "done"),
		mrow(gamma, "tooling", "baseline", "-", shaC, "done"))
	w.writeRun(t, "baseline-20261120-1200", mrow(alpha, "fleets", "baseline", "-", shaA, "done"))
	if code := w.run("run", "status"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	want := "run diff-20261201-0900\nfleets pending 1 done 1\ntooling pending 0 done 1\npending example-org/alpha fleets\n"
	if w.out.String() != want {
		t.Fatalf("out %q\nwant %q", w.out.String(), want)
	}
	if code := w.run("run", "status", "baseline-20261120-1200"); code != term.ExitOK || w.out.String() != "run baseline-20261120-1200\nfleets pending 0 done 1\n" {
		t.Fatalf("exit %d, out %q", code, w.out.String())
	}
}
