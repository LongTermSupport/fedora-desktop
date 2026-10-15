package gates

import (
	"path/filepath"
	"strings"
	"testing"
)

// G1 (SPEC-qa-helper-tests): HelperRun is the integer-valued sibling of HelperCounts and refuses
// the same files.
func TestG1HelperRunReturnsTheNumbers(t *testing.T) {
	got, err := HelperRun(countsFile(t, goodCounts("1483", "7", "66", "65")), token)
	want := HelperNumbers{Tests: 1483, Skipped: 7, Modules: 66, Tracked: 65}
	if err != nil || got != want {
		t.Fatalf("got %+v, %v; want %+v", got, err, want)
	}
}

func TestG1HelperRunRefusesWhatHelperCountsRefuses(t *testing.T) {
	cases := map[string]string{
		"empty":             "",
		"a foreign token":   "token=other\ntests=1\nskipped=0\nmodules=1\ntracked=1\n",
		"no token":          "tests=1\nskipped=0\nmodules=1\ntracked=1\n",
		"a missing key":     "token=" + token + "\ntests=1\nskipped=0\nmodules=1\n",
		"a repeated key":    goodCounts("1", "0", "1", "1") + "tests=2\n",
		"an unknown key":    goodCounts("1", "0", "1", "1") + "extra=2\n",
		"not a number":      goodCounts("many", "0", "1", "1"),
		"a negative number": goodCounts("-1", "0", "1", "1"),
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := countsFile(t, content)
			if got, err := HelperRun(path, token); err == nil {
				t.Fatalf("accepted %+v", got)
			}
			if _, err := HelperCounts(path, token); err == nil {
				t.Fatal("HelperCounts accepted what HelperRun refused")
			}
		})
	}
	if _, err := HelperRun(filepath.Join(t.TempDir(), "absent"), token); err == nil || !strings.Contains(err.Error(), "no counts file at") {
		t.Fatalf("a missing file: %v", err)
	}
}

// A number no int holds is refused by HelperRun; HelperCounts only formats the digits it was given.
func TestG1HelperRunRefusesANumberTooLargeForAnInt(t *testing.T) {
	got, err := HelperRun(countsFile(t, goodCounts("99999999999999999999999999", "0", "1", "1")), token)
	if err == nil || !strings.Contains(err.Error(), "tests=") {
		t.Fatalf("got %+v, %v", got, err)
	}
}

// One reader, one refusal set: HelperCounts is built on HelperRun, so a number no int holds is
// refused by both, and a leading zero is printed as the number it is.
func TestG1HelperCountsRefusesWhatHelperRunRefuses(t *testing.T) {
	if got, err := HelperCounts(countsFile(t, goodCounts("99999999999999999999999999", "0", "1", "1")), token); err == nil {
		t.Fatalf("accepted %q", got)
	}
	got, err := HelperCounts(countsFile(t, goodCounts("007", "0", "1", "1")), token)
	if err != nil || got != "Ran 7 tests in 1 module (1 tracked), 0 skipped" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestG1HelperRunWithNoTokenWantsNoToken(t *testing.T) {
	if _, err := HelperRun(countsFile(t, "tests=1\nskipped=0\nmodules=1\ntracked=1\n"), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := HelperRun(countsFile(t, goodCounts("1", "0", "1", "1")), ""); err == nil {
		t.Fatal("a token nobody asked for was accepted")
	}
}
