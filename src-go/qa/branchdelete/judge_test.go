package branchdelete

import (
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/branchhygiene"
)

const (
	tipA = "1111111111111111111111111111111111111111"
	tipB = "2222222222222222222222222222222222222222"
)

func policy() branchhygiene.Policy {
	return branchhygiene.Policy{
		Cap: 10, WarnDays: 3, FailDays: 7,
		Exempt: []string{"F44", "F42", "F43", "F44-arm", "keep-me"},
		Parked: []branchhygiene.Parked{{Branch: "feat/waiting", Reason: "owner to choose", Until: "2030-01-31"}},
	}
}

func remote() Remote {
	return Remote{
		Default: "F44",
		Heads: map[string]string{
			"F44": tipA, "F42": tipA, "F43": tipA, "F44-arm": tipA, "F45": tipA, "keep-me": tipA,
			"feat/waiting": tipA, "feat/done": tipA, "fix/old": tipB, "main": tipA,
			"feat/unarchived": tipA, "feat/moved": tipB,
		},
		Archives: map[string]string{
			"feat/done": tipA, "fix/old": tipB, "feat/moved": tipA,
			"F42": tipA, "F43": tipA, "keep-me": tipA, "feat/waiting": tipA, "F45": tipA,
		},
	}
}

func reasonsFor(t *testing.T, refusals []Refusal, name string) string {
	t.Helper()
	i := slices.IndexFunc(refusals, func(r Refusal) bool { return r.Branch == name })
	if i < 0 {
		t.Fatalf("%s was not refused: %+v", name, refusals)
	}
	return strings.Join(refusals[i].Reasons, "; ")
}

func TestArchivedWorkBranchesPass(t *testing.T) {
	if got := Judge(policy(), remote(), []string{"feat/done", "fix/old"}); len(got) != 0 {
		t.Fatalf("refused %+v", got)
	}
}

// The incident: F42 and F43 were in a bulk delete list built from a triage's verdicts.
func TestTheExemptReleaseLinesAreRefusedEvenWhenArchived(t *testing.T) {
	got := Judge(policy(), remote(), []string{"feat/done", "F42", "F43"})
	if len(got) != 2 {
		t.Fatalf("want F42 and F43 refused, got %+v", got)
	}
	for _, name := range []string{"F42", "F43"} {
		r := reasonsFor(t, got, name)
		if !strings.Contains(r, "exempt") || !strings.Contains(r, "release line") {
			t.Errorf("%s: %s", name, r)
		}
	}
}

// A release line the policy has not listed yet is still a release line.
func TestAnyFDigitBranchIsRefusedWhetherOrNotThePolicyListsIt(t *testing.T) {
	got := Judge(policy(), remote(), []string{"F45"})
	r := reasonsFor(t, got, "F45")
	if !strings.Contains(r, "release line") || strings.Contains(r, "exempt") {
		t.Fatalf("F45: %s", r)
	}
	if got := Judge(policy(), remote(), []string{"Fix-thing"}); strings.Contains(reasonsFor(t, got, "Fix-thing"), "release line") {
		t.Fatal("F followed by a letter is not a release line")
	}
}

// A branch that is never deleted is not told to archive its tip, which would read as a way through.
func TestAProtectedBranchIsNotToldToArchiveItsTip(t *testing.T) {
	r := remote()
	delete(r.Archives, "F42")
	if got := reasonsFor(t, Judge(policy(), r, []string{"F42"}), "F42"); strings.Contains(got, "archive") {
		t.Fatalf("F42: %s", got)
	}
}

func TestTheDefaultBranchIsRefused(t *testing.T) {
	r := remote()
	r.Default = "main"
	r.Archives["main"] = tipA
	got := Judge(policy(), r, []string{"main"})
	if !strings.Contains(reasonsFor(t, got, "main"), "default branch") {
		t.Fatalf("%+v", got)
	}
}

func TestExemptAndParkedBranchesAreRefused(t *testing.T) {
	got := Judge(policy(), remote(), []string{"keep-me", "feat/waiting"})
	if !strings.Contains(reasonsFor(t, got, "keep-me"), "exempt in "+branchhygiene.PolicyFile) {
		t.Error("keep-me not refused as exempt")
	}
	if r := reasonsFor(t, got, "feat/waiting"); !strings.Contains(r, "parked") || !strings.Contains(r, "owner to choose") {
		t.Errorf("feat/waiting: %s", r)
	}
}

func TestAnUnarchivedTipIsRefusedWithTheArchiveCommand(t *testing.T) {
	got := Judge(policy(), remote(), []string{"feat/unarchived"})
	r := reasonsFor(t, got, "feat/unarchived")
	if !strings.Contains(r, "no refs/archive/feat/unarchived") || !strings.Contains(r, "git push origin "+tipA+":refs/archive/feat/unarchived") {
		t.Fatalf("%s", r)
	}
}

// An archive of an older tip would lose the commits made since.
func TestAnArchiveAtAnotherCommitThanTheTipIsRefused(t *testing.T) {
	r := reasonsFor(t, Judge(policy(), remote(), []string{"feat/moved"}), "feat/moved")
	if !strings.Contains(r, "refs/archive/feat/moved is at "+tipA) || !strings.Contains(r, "tip is "+tipB) {
		t.Fatalf("%s", r)
	}
}

func TestABranchOriginDoesNotHaveIsRefused(t *testing.T) {
	r := reasonsFor(t, Judge(policy(), remote(), []string{"feat/gone"}), "feat/gone")
	if !strings.Contains(r, "no branch feat/gone on origin") {
		t.Fatalf("%s", r)
	}
}

func TestAPrefixedOrUnsafeNameIsRefused(t *testing.T) {
	got := Judge(policy(), remote(), []string{"origin/feat/done", "refs/heads/feat/done", "feat/x;rm", "-x"})
	if !strings.Contains(reasonsFor(t, got, "origin/feat/done"), "bare branch name") {
		t.Error("origin/ prefix not named")
	}
	if !strings.Contains(reasonsFor(t, got, "refs/heads/feat/done"), "bare branch name") {
		t.Error("refs/ prefix not named")
	}
	for _, name := range []string{"feat/x;rm", "-x"} {
		if !strings.Contains(reasonsFor(t, got, name), "not a plain branch name") {
			t.Errorf("%s not refused as unsafe", name)
		}
	}
}

func TestANameListedTwiceIsRefused(t *testing.T) {
	got := Judge(policy(), remote(), []string{"feat/done", "fix/old", "feat/done"})
	if len(got) != 1 || !strings.Contains(reasonsFor(t, got, "feat/done"), "listed twice") {
		t.Fatalf("%+v", got)
	}
}

// Every offender is named in input order, each with every reason that applies.
func TestEveryOffenderIsNamedInInputOrder(t *testing.T) {
	got := Judge(policy(), remote(), []string{"F43", "feat/done", "feat/unarchived", "F42"})
	names := make([]string, 0, len(got))
	for _, r := range got {
		names = append(names, r.Branch)
	}
	if strings.Join(names, " ") != "F43 feat/unarchived F42" {
		t.Fatalf("%v", names)
	}
}

func TestCandidatesAreOneNamePerLineWithBlankLinesSkipped(t *testing.T) {
	got := Candidates("  feat/done \n\n\tfix/old\r\nlast")
	if strings.Join(got, ",") != "feat/done,fix/old,last" {
		t.Fatalf("%q", got)
	}
}
