package archpin

import (
	"strings"
	"testing"
)

const v2 = "# format: 2\n"

func TestTheAllowlistReadsOneRowPerFindingAndSkipsCommentsAndBlankLines(t *testing.T) {
	list, err := parseAllowlist("# header\n" + v2 + "\na/Dockerfile\tRUN wget x/yq_linux_amd64\tyq: arm64 build exists\nb.yml\turl: x86_64\tno aarch64 build\n")
	if err != nil {
		t.Fatal(err)
	}
	if list.format != 2 || len(list.entries) != 2 || list.entries[0].text != "RUN wget x/yq_linux_amd64" || list.entries[1].note != "no aarch64 build" || list.entries[1].line != 5 {
		t.Fatalf("list %+v", list)
	}
	if got := list.perFile(); got["a/Dockerfile"] != 1 || got["b.yml"] != 1 {
		t.Fatalf("per file %v", got)
	}
}

func TestAFormatOneListIsReadAsCountsPerFile(t *testing.T) {
	list, err := parseAllowlist("# old\na/Dockerfile\t3\tnote\n")
	if err != nil {
		t.Fatal(err)
	}
	if list.format != 1 || list.perFile()["a/Dockerfile"] != 3 || len(list.entries) != 0 {
		t.Fatalf("list %+v", list)
	}
}

func TestAMalformedAllowlistRowNamesItsLine(t *testing.T) {
	for name, text := range map[string]string{
		"two fields":         v2 + "a\tx\n",
		"four fields":        v2 + "a\tx\tb\tc\n",
		"empty note":         v2 + "a\tx\t \n",
		"empty finding":      v2 + "a\t\tnote\n",
		"uncollapsed":        v2 + "a\t  x  y\tnote\n",
		"empty path":         v2 + "\tx\tnote\n",
		"absolute path":      v2 + "/a\tx\tnote\n",
		"unclean path":       v2 + "a/../b\tx\tnote\n",
		"format 1 zero":      "# c\na\t0\tnote\n",
		"format 1 not count": "# c\na\tx\tnote\n",
		"format 1 twice":     "a\t1\tnote\na\t2\tnote\n",
		"unknown format":     "# c\n# format: 9\n",
	} {
		if _, err := parseAllowlist(text); err == nil || !strings.Contains(err.Error(), allowlistFile+":2") {
			t.Errorf("%s: err %v, want one naming %s:2", name, err, allowlistFile)
		}
	}
}

// judged runs the ratchet over findings (path to findings) against an allowlist text.
func judged(t *testing.T, list string, found map[string][]finding, scanned ...string) []problem {
	t.Helper()
	al, err := parseAllowlist(list)
	if err != nil {
		t.Fatal(err)
	}
	return ratchet(al, found, scanned)
}

func TestAFileWithExactlyItsListedFindingsPassesWhereverTheyMoved(t *testing.T) {
	list := v2 + "a\tget amd64\tnote\na\tget x86_64\tnote\n"
	if got := judged(t, list, map[string][]finding{"a": {{"get x86_64", 3}, {"get amd64", 40}}}, "a", "b"); len(got) != 0 {
		t.Fatalf("problems %+v", got)
	}
}

func TestANewFileWithAFindingFails(t *testing.T) {
	got := judged(t, v2, map[string][]finding{"b": {{"x", 4}}}, "b")
	if len(got) != 1 || got[0].path != "b" || !strings.Contains(got[0].why, "not on "+allowlistFile) || len(got[0].lines) != 1 {
		t.Fatalf("problems %+v", got)
	}
}

// The defect the count-only ratchet had: one pin fixed and a new one added keeps the count.
func TestAListedFileThatFixedOnePinAndAddedAnotherFails(t *testing.T) {
	got := judged(t, v2+"a\told amd64\tnote\n", map[string][]finding{"a": {{"new x86_64", 9}}}, "a")
	if len(got) != 2 || !strings.Contains(got[0].why, "1 finding(s) that no row of "+allowlistFile+" names") || got[0].lines[0] != 9 || !strings.Contains(got[1].why, "line 2 names a finding the file no longer has") {
		t.Fatalf("problems %+v", got)
	}
}

func TestARepeatedLineIsHeldToItsNumberOfRows(t *testing.T) {
	got := judged(t, v2+"a\tx86_64\tnote\n", map[string][]finding{"a": {{"x86_64", 1}, {"x86_64", 5}}}, "a")
	if len(got) != 1 || got[0].lines[0] != 5 {
		t.Fatalf("problems %+v", got)
	}
}

func TestAListedFileWithNoFindingOrGoneMustLeaveTheList(t *testing.T) {
	got := judged(t, v2+"a\tx\tnote\nb\tx\tnote\n", map[string][]finding{}, "a")
	if len(got) != 2 || !strings.Contains(got[0].why, "has no finding") || !strings.Contains(got[1].why, "is not a scanned file") {
		t.Fatalf("problems %+v", got)
	}
}

// compared runs the base comparison of two allowlist texts.
func compared(t *testing.T, base, cur string) []problem {
	t.Helper()
	b, err := parseAllowlist(base)
	if err != nil {
		t.Fatal(err)
	}
	c, err := parseAllowlist(cur)
	if err != nil {
		t.Fatal(err)
	}
	return againstBase(b, c, "base0")
}

func TestAgainstAFormatTwoBaseARowMayGoAndNeverCome(t *testing.T) {
	base := v2 + "a\tx\tnote\na\tx\tnote\nb\ty\tnote\n"
	if got := compared(t, base, v2+"a\tx\tnote\n"); len(got) != 0 {
		t.Fatalf("a shrunk list: %+v", got)
	}
	got := compared(t, base, v2+"a\tx\tnote\na\tx\tnote\na\tx\tnote\nc\tz\tnote\n")
	if len(got) != 2 || got[0].path != "a" || !strings.Contains(got[0].why, "line 4 is a row base0 does not have") || got[1].path != "c" {
		t.Fatalf("problems %+v", got)
	}
}

func TestAgainstAFormatOneBaseAFileMayLoseRowsAndNeverGain(t *testing.T) {
	base := "a\t2\tnote\n"
	if got := compared(t, base, v2+"a\tx\tnote\na\ty\tnote\n"); len(got) != 0 {
		t.Fatalf("the same count: %+v", got)
	}
	got := compared(t, base, v2+"a\tx\tnote\na\ty\tnote\na\tz\tnote\nb\tx\tnote\n")
	if len(got) != 2 || !strings.Contains(got[0].why, "3 row(s) on "+allowlistFile+", above the 2 at base0") || !strings.Contains(got[1].why, "none at base0") {
		t.Fatalf("problems %+v", got)
	}
}
