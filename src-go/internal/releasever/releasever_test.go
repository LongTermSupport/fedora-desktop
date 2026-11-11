package releasever

import (
	"errors"
	"strings"
	"testing"
)

func TestBranchMajorReadsTheFedoraMajor(t *testing.T) {
	cases := []struct {
		branch string
		want   int
		ok     bool
	}{
		{"F44", 44, true},
		{"F9", 9, true},
		{"refs/heads/F44", 0, false},
		{"f44", 0, false},
		{"F", 0, false},
		{"F044", 0, false},
		{"F44x", 0, false},
		{"main", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		got, err := BranchMajor(c.branch)
		if c.ok != (err == nil) || got != c.want {
			t.Errorf("BranchMajor(%q) = %d, %v; want %d, ok=%v", c.branch, got, err, c.want, c.ok)
		}
		if !c.ok && !errors.Is(err, ErrBranch) {
			t.Errorf("BranchMajor(%q) error %v is not ErrBranch", c.branch, err)
		}
	}
}

func TestParseVersionTakesOnlyMajorMinorPatch(t *testing.T) {
	good := map[string]Version{
		"44.0.0":  {44, 0, 0},
		"44.12.3": {44, 12, 3},
	}
	for in, want := range good {
		got, err := ParseVersion(in)
		if err != nil || got != want || got.String() != in {
			t.Errorf("ParseVersion(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "44", "44.1", "v44.1.2", "44.1.2-rc1", "44.01.2", "44.1.2.3", "44.-1.2", "a.b.c", " 44.1.2"} {
		if _, err := ParseVersion(in); !errors.Is(err, ErrVersion) {
			t.Errorf("ParseVersion(%q) error = %v, want ErrVersion", in, err)
		}
	}
}

func TestSplitCutsGitLogZOutput(t *testing.T) {
	raw := "feat: a\n\x00fix: b\n\nbody line\n\x00\x00docs: c\n"
	got := Split(raw)
	want := []string{"feat: a", "fix: b\n\nbody line", "docs: c"}
	if len(got) != len(want) {
		t.Fatalf("Split = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Split[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if got := Split(""); len(got) != 0 {
		t.Errorf("Split(\"\") = %q, want none", got)
	}
}

func TestNextChoosesTheBumpFromTheCommitTypes(t *testing.T) {
	cases := []struct {
		name string
		last string
		msgs []string
		want string
		bump Bump
	}{
		{"a fix is a patch", "44.1.2", []string{"fix: close the file"}, "44.1.3", BumpPatch},
		{"a feature is a minor and resets the patch", "44.1.2", []string{"feat: add the verb"}, "44.2.0", BumpMinor},
		{"the highest bump wins", "44.1.2", []string{"fix: a", "feat: b", "fix: c"}, "44.2.0", BumpMinor},
		{"a scope does not matter", "44.1.2", []string{"fix(ccy): a"}, "44.1.3", BumpPatch},
		{"the type is not case sensitive", "44.1.2", []string{"Feat: a"}, "44.2.0", BumpMinor},
		{"the body is not read for the type", "44.1.2", []string{"docs: a\n\nfeat: not a header"}, "", BumpNone},
		{"docs and chores release nothing", "44.1.2", []string{"docs: a", "chore: b", "ci: c", "test: d", "refactor: e", "style: f", "build: g", "perf: h"}, "", BumpNone},
		{"a merge, a revert and a release commit release nothing", "44.1.2", []string{"Merge branch 'x' into F44", "Revert \"feat: a\"", "Release 44.1.2"}, "", BumpNone},
		{"a subject with no type releases nothing", "44.1.2", []string{"Plan 00246: land the work", "Add a play"}, "", BumpNone},
		{"a header needs the space after the colon", "44.1.2", []string{"feat:no space", "fix : spaced"}, "", BumpNone},
		{"no commits releases nothing", "44.1.2", nil, "", BumpNone},
		{"a fixup is not a fix", "44.1.2", []string{"fixup! fix: a"}, "", BumpNone},
	}
	for _, c := range cases {
		got, err := Next("F44", c.last, c.msgs)
		if c.want == "" {
			if !errors.Is(err, ErrNoRelease) {
				t.Errorf("%s: error = %v, want ErrNoRelease", c.name, err)
			}
			continue
		}
		if err != nil || got.Version.String() != c.want || got.Bump != c.bump {
			t.Errorf("%s: Next = %+v, %v; want %s (%v)", c.name, got, err, c.want, c.bump)
		}
	}
}

func TestNextNamesTheCommitsThatDecidedTheBump(t *testing.T) {
	got, err := Next("F44", "44.1.2", []string{"docs: x", "fix: a", "feat: b\n\nlong body", "chore: y"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"feat: b", "fix: a"}
	if len(got.Releasable) != len(want) || got.Releasable[0] != want[0] || got.Releasable[1] != want[1] {
		t.Errorf("Releasable = %q, want %q (the subjects, feat first, in log order)", got.Releasable, want)
	}
}

func TestNextRefusesABreakingMarkerOnAMajorBranch(t *testing.T) {
	breaking := map[string]string{
		"a bang":                 "feat!: drop the flag",
		"a bang with a scope":    "fix(ccy)!: change the exit code",
		"a bang on a chore":      "chore!: remove a thing",
		"a footer":               "feat: new shape\n\nBREAKING CHANGE: the file moved",
		"a hyphenated footer":    "fix: a\n\nsome text\nBREAKING-CHANGE: gone",
		"a footer on a docs fix": "docs: a\n\nBREAKING CHANGE: gone",
	}
	for name, msg := range breaking {
		_, err := Next("F44", "44.1.2", []string{"fix: fine", msg})
		if !errors.Is(err, ErrBreaking) {
			t.Errorf("%s: error = %v, want ErrBreaking", name, err)
			continue
		}
		if !strings.Contains(err.Error(), strings.SplitN(msg, "\n", 2)[0]) {
			t.Errorf("%s: error %q does not name the commit", name, err)
		}
	}
}

func TestNextIgnoresTextThatIsNotABreakingMarker(t *testing.T) {
	msgs := []string{
		"fix: a\n\nthis is not a BREAKING CHANGE: it is mid-line",
		"fix: b\n\nbreaking change: lower case is prose",
		"BREAKING CHANGE: in the subject line is no footer",
	}
	got, err := Next("F44", "44.1.2", msgs)
	if err != nil || got.Version.String() != "44.1.3" {
		t.Errorf("Next = %+v, %v; want 44.1.3", got, err)
	}
}

func TestNextStartsAMajorAtZeroAndAllowsABreakingMarkerThere(t *testing.T) {
	cases := []struct {
		name string
		msgs []string
	}{
		{"a feature", []string{"feat: first"}},
		{"a fix", []string{"fix: first"}},
		{"a breaking feature", []string{"feat!: first", "fix: a"}},
		{"a breaking footer", []string{"fix: a\n\nBREAKING CHANGE: new base"}},
	}
	for _, c := range cases {
		got, err := Next("F45", "", c.msgs)
		if err != nil || got.Version.String() != "45.0.0" {
			t.Errorf("%s: Next = %+v, %v; want 45.0.0", c.name, got, err)
		}
	}
	if _, err := Next("F45", "", []string{"docs: a", "Merge x"}); !errors.Is(err, ErrNoRelease) {
		t.Errorf("a first release with nothing releasable: error = %v, want ErrNoRelease", err)
	}
}

func TestNextRefusesALastTagOfAnotherMajor(t *testing.T) {
	if _, err := Next("F45", "44.9.1", []string{"feat: a"}); !errors.Is(err, ErrVersion) {
		t.Errorf("error = %v, want ErrVersion", err)
	}
	if _, err := Next("F44", "not-a-version", []string{"feat: a"}); !errors.Is(err, ErrVersion) {
		t.Errorf("error = %v, want ErrVersion", err)
	}
	if _, err := Next("main", "44.1.2", []string{"feat: a"}); !errors.Is(err, ErrBranch) {
		t.Errorf("error = %v, want ErrBranch", err)
	}
}

// A history cut from the shape this repository's log really has: most subjects carry no type, a
// few do. Only the typed ones decide, so a plain subject never causes a release.
func TestNextOverARealisticHistory(t *testing.T) {
	log := strings.Join([]string{
		"Merge pull request #131 from LongTermSupport/feat/x\n\nPlan 00244: x",
		"Plan 00246: Task 8.7 is green and ready for review",
		"Auto: hooks daemon regenerated CLAUDE.md handler guidance",
		"fix(ccy-teams): the launch start is the suite's own clock\n\nThe check bounded a recorded time by reads of the real clock.",
		"docs: say where the release key lives",
		"feat(release): compute the next version from the commits",
		"Add play-matrix-clients: Nheko from Flathub, apart from the agent bus",
	}, "\x00") + "\x00"
	got, err := Next("F44", "44.3.7", Split(log))
	if err != nil || got.Version.String() != "44.4.0" || got.Bump != BumpMinor {
		t.Fatalf("Next = %+v, %v; want 44.4.0 minor", got, err)
	}
	if len(got.Releasable) != 2 {
		t.Errorf("Releasable = %q, want the feat and the fix subjects", got.Releasable)
	}
}
