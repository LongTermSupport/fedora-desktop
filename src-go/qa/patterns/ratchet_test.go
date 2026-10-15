package patterns

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// partialWorld is one 20-line script whose first `gap` lines semgrep did not parse, and the
// parse allowlist text (nil is a file that is missing).
func partialWorld(t *testing.T, gap int, allowlist *string) *world {
	t.Helper()
	files := map[string]string{"scripts/a.bash": shebang + strings.Repeat("echo x\n", 19)}
	if allowlist != nil {
		files["scripts/"+partialAllowlistFile] = *allowlist
	}
	w := newWorld(t, files)
	if allowlist == nil {
		if err := os.Remove(filepath.Join(w.root, "scripts", partialAllowlistFile)); err != nil {
			t.Fatal(err)
		}
		w.run.files = w.listAll()
	}
	w.run.scan = func(string) string {
		if gap == 0 {
			return w.run.okReport()
		}
		return semgrepJSON([]string{"scripts/a.bash"}, nil,
			`{"type":["PartialParsing",[{"start":{"line":1},"end":{"line":`+strconv.Itoa(gap)+`}}]],"path":"scripts/a.bash"}`)
	}
	return w
}

func list1() *string {
	s := "# header\nscripts/a.bash\t12\t10# in arithmetic\n"
	return &s
}

const empty = "# no files\n"

func TestRatchetAFileMostlyOutsideTheParseTreeAndNotListedFails(t *testing.T) {
	e := empty
	w := partialWorld(t, 12, &e)
	got := w.main()
	wantExit(t, got, ExitFailed)
	for _, part := range []string{"scripts/a.bash", "12 of 20 lines outside the parse tree", "not on " + partialAllowlistFile} {
		if !strings.Contains(got.stdout, part) {
			t.Fatalf("stdout lacks %q:\n%s", part, got.stdout)
		}
	}
	if w.report()["status"] != "fail" {
		t.Fatalf("report: %v", w.report())
	}
}

func TestRatchetExactlyHalfIsNotMostly(t *testing.T) {
	e := empty
	wantExit(t, partialWorld(t, 10, &e).main(), ExitOK)
}

func TestRatchetAListedFileAtItsRecordPasses(t *testing.T) {
	wantExit(t, partialWorld(t, 12, list1()).main(), ExitOK)
}

func TestRatchetAListedFileThatGrewFails(t *testing.T) {
	got := partialWorld(t, 14, list1()).main()
	wantExit(t, got, ExitFailed)
	if !strings.Contains(got.stdout, "grew past its record (14 > 12)") {
		t.Fatalf("stdout:\n%s", got.stdout)
	}
}

func TestRatchetAListedFileUnderItsRecordMustBeTightened(t *testing.T) {
	got := partialWorld(t, 11, list1()).main()
	wantExit(t, got, ExitFailed)
	if !strings.Contains(got.stdout, "under its record (11 < 12)") {
		t.Fatalf("stdout:\n%s", got.stdout)
	}
}

func TestRatchetAListedFileThatIsNoLongerMostlyPartialMustBeRemoved(t *testing.T) {
	for _, gap := range []int{0, 5} {
		got := partialWorld(t, gap, list1()).main()
		wantExit(t, got, ExitFailed)
		if !strings.Contains(got.stdout, "remove its line from "+partialAllowlistFile) {
			t.Fatalf("gap %d stdout:\n%s", gap, got.stdout)
		}
	}
}

func TestRatchetAMissingOrMalformedAllowlistIsExit2(t *testing.T) {
	wantExit(t, partialWorld(t, 0, nil).main(), ExitCannotRun)
	for _, bad := range []string{"scripts/a.bash\n", "scripts/a.bash\tmany\tx\n", "scripts/a.bash\t12\t\n", "scripts/a.bash\t0\tx\n"} {
		wantExit(t, partialWorld(t, 0, &bad).main(), ExitCannotRun)
	}
}
