package changed

import (
	"strings"
	"testing"
)

// planned is the "check <id>" and "suite <name>" lines of a --plan run, in order.
func planned(out string) []string {
	var got []string
	for _, line := range lines(out) {
		if strings.HasPrefix(line, "check ") || strings.HasPrefix(line, "suite ") {
			got = append(got, strings.SplitN(line, ":", 2)[0])
		}
	}
	return got
}

// CI splits a diff run across parallel jobs: --only for the jobs that take named checks,
// --except for the job that takes the rest, the suites included. Together they plan each check
// and suite exactly once.
func TestOnlyAndExceptOfOneListPlanEveryCheckAndSuiteExactlyOnce(t *testing.T) {
	w := newRunWorld(t)
	w.changeKept()
	if code := w.main("--plan"); code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.errOut.String())
	}
	whole := planned(w.out.String())
	if code := w.main("--plan", "--only", "shellcheck,gitleaks"); code != ExitOK {
		t.Fatalf("--only: exit %d, stderr %q", code, w.errOut.String())
	}
	only := planned(w.out.String())
	if want := []string{"check shellcheck", "check gitleaks"}; strings.Join(only, "|") != strings.Join(want, "|") {
		t.Fatalf("--only planned %v, want %v", only, want)
	}
	if !strings.Contains(w.out.String(), "note: --only shellcheck,gitleaks:") {
		t.Fatalf("--only does not say what it left out:\n%s", w.out.String())
	}
	if code := w.main("--plan", "--except", "shellcheck,gitleaks"); code != ExitOK {
		t.Fatalf("--except: exit %d, stderr %q", code, w.errOut.String())
	}
	rest := planned(w.out.String())
	both := append(append([]string{}, only...), rest...)
	if len(both) != len(whole) {
		t.Fatalf("the halves planned %v, the whole run %v", both, whole)
	}
	for _, item := range whole {
		n := 0
		for _, got := range both {
			if got == item {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("%s planned %d times across the halves", item, n)
		}
	}
}

// A subset runs only what it planned.
func TestOnlyRunsOnlyTheNamedCheck(t *testing.T) {
	w := newRunWorld(t)
	w.changeKept()
	if code := w.main("--only", "bash-syntax"); code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.errOut.String())
	}
	for _, line := range lines(w.out.String()) {
		if strings.HasPrefix(line, "✓ ") && !strings.HasPrefix(line, "✓ bash-syntax:") && !strings.HasPrefix(line, "✓ qa-changed passed:") {
			t.Fatalf("ran more than bash-syntax:\n%s", w.out.String())
		}
	}
	if !strings.Contains(w.out.String(), "✓ bash-syntax: 1 file(s)") {
		t.Fatalf("bash-syntax did not run:\n%s", w.out.String())
	}
}

// A name that is no check is refused before anything runs, as is a second subset flag or an
// empty name: a renamed check must not drop out of every CI job unnoticed.
func TestASubsetNamingNoCheckIsRefused(t *testing.T) {
	for _, args := range [][]string{
		{"--only", "go,nosuch"},
		{"--except", "nosuch"},
		{"--only", "go", "--except", "docs"},
		{"--only", "go,,docs"},
		{"--except"},
	} {
		if _, err := parseArgs(args); err == nil {
			t.Fatalf("%v: accepted", args)
		}
	}
}

func TestUsageNamesTheSubsetFlags(t *testing.T) {
	for _, flag := range []string{"--only", "--except"} {
		if !strings.Contains(usageText, flag) {
			t.Errorf("usage lacks %s", flag)
		}
	}
}
