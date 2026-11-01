package archpin

import (
	"strings"
	"testing"
)

func TestTheAllowlistReadsRowsAndSkipsCommentsAndBlankLines(t *testing.T) {
	list, err := parseAllowlist("# header\n\na/Dockerfile\t3\tyq, gum: arm64 builds exist\nb.yml\t1\tno aarch64 build\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list["a/Dockerfile"].count != 3 || list["b.yml"].note != "no aarch64 build" || list["b.yml"].line != 4 {
		t.Fatalf("list %+v", list)
	}
}

func TestAMalformedAllowlistRowNamesItsLine(t *testing.T) {
	for name, text := range map[string]string{
		"two fields":    "a\t1\n",
		"four fields":   "a\t1\tb\tc\n",
		"zero":          "a\t0\tnote\n",
		"not a number":  "a\tx\tnote\n",
		"empty note":    "a\t1\t \n",
		"empty path":    "\t1\tnote\n",
		"absolute path": "/a\t1\tnote\n",
		"unclean path":  "a/../b\t1\tnote\n",
	} {
		if _, err := parseAllowlist("# c\n" + text); err == nil || !strings.Contains(err.Error(), allowlistFile+":2") {
			t.Errorf("%s: err %v, want one naming %s:2", name, err, allowlistFile)
		}
	}
}

func TestADuplicateRowNamesBothLines(t *testing.T) {
	_, err := parseAllowlist("a\t1\tnote\na\t2\tnote\n")
	if err == nil || !strings.Contains(err.Error(), allowlistFile+":2: a is already listed on line 1") {
		t.Fatalf("err %v", err)
	}
}

// judged runs the ratchet over findings (path to lines) against an allowlist text.
func judged(t *testing.T, list string, found map[string][]int, scanned ...string) []problem {
	t.Helper()
	al, err := parseAllowlist(list)
	if err != nil {
		t.Fatal(err)
	}
	return ratchet(al, found, scanned)
}

func TestAFileAtItsRecordPasses(t *testing.T) {
	if got := judged(t, "a\t2\tnote\n", map[string][]int{"a": {3, 9}}, "a", "b"); len(got) != 0 {
		t.Fatalf("problems %+v", got)
	}
}

func TestANewFileWithAFindingFails(t *testing.T) {
	got := judged(t, "", map[string][]int{"b": {4}}, "b")
	if len(got) != 1 || got[0].path != "b" || !strings.Contains(got[0].why, "not on "+allowlistFile) || len(got[0].lines) != 1 {
		t.Fatalf("problems %+v", got)
	}
}

func TestAListedFileThatGrewFails(t *testing.T) {
	got := judged(t, "a\t1\tnote\n", map[string][]int{"a": {3, 9}}, "a")
	if len(got) != 1 || !strings.Contains(got[0].why, "2 finding(s), above its record of 1") || len(got[0].lines) != 2 {
		t.Fatalf("problems %+v", got)
	}
}

func TestAListedFileThatShrankMustLowerItsRecord(t *testing.T) {
	got := judged(t, "a\t3\tnote\n", map[string][]int{"a": {3}}, "a")
	if len(got) != 1 || !strings.Contains(got[0].why, "lower its record from 3 to 1") {
		t.Fatalf("problems %+v", got)
	}
}

func TestAListedFileWithNoFindingOrGoneMustLeaveTheList(t *testing.T) {
	got := judged(t, "a\t1\tnote\nb\t1\tnote\n", map[string][]int{}, "a")
	if len(got) != 2 || !strings.Contains(got[0].why, "has no finding") || !strings.Contains(got[1].why, "is not a scanned file") {
		t.Fatalf("problems %+v", got)
	}
}
