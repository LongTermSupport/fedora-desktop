package branchhygiene

import (
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

func ago(days float64) time.Time {
	return now.Add(-time.Duration(days * 24 * float64(time.Hour)))
}

func policy() Policy {
	return Policy{Cap: 3, WarnDays: 3, FailDays: 7, Exempt: []string{"F44"}}
}

func groups(fs []Finding, g Group) []Finding {
	var out []Finding
	for _, f := range fs {
		if f.Group == g {
			out = append(out, f)
		}
	}
	return out
}

func TestFreshBranchesUnderTheCapAreClean(t *testing.T) {
	fs := Evaluate(policy(), []Branch{{Name: "a", Commit: ago(1)}, {Name: "b", Commit: ago(2.9)}}, now)
	if len(fs) != 0 {
		t.Fatalf("findings: %+v", fs)
	}
}

func TestMergedBranchFailsAndIsNotCounted(t *testing.T) {
	bs := []Branch{{Name: "m", Commit: ago(30), Merged: true}, {Name: "a", Commit: ago(1)}}
	fs := Evaluate(policy(), bs, now)
	m := groups(fs, MergedNotDeleted)
	if len(m) != 1 || m[0].Branch != "m" || !m[0].Fail {
		t.Fatalf("merged: %+v", fs)
	}
	if len(groups(fs, DormantFail)) != 0 {
		t.Fatalf("a merged branch was also judged dormant: %+v", fs)
	}
}

func TestExemptBranchesAreNeverJudged(t *testing.T) {
	bs := []Branch{{Name: "F44", Commit: ago(90)}, {Name: "F44", Merged: true, Commit: ago(90)}}
	if fs := Evaluate(policy(), bs, now); len(fs) != 0 {
		t.Fatalf("findings: %+v", fs)
	}
}

func TestOverCapListsEveryOpenBranchWithAge(t *testing.T) {
	bs := []Branch{{Name: "a", Commit: ago(1)}, {Name: "b", Commit: ago(2)}, {Name: "c", Commit: ago(0.5)}, {Name: "d", Commit: ago(1.5)}}
	fs := groups(Evaluate(policy(), bs, now), OverCap)
	if len(fs) != 5 || !fs[0].Fail || !strings.Contains(fs[0].Text, "4 open") || !strings.Contains(fs[0].Text, "cap 3") {
		t.Fatalf("over cap: %+v", fs)
	}
	if !strings.Contains(fs[1].Text, "b") || !strings.Contains(fs[1].Text, "2 days") {
		t.Fatalf("oldest first with age: %+v", fs)
	}
}

func TestAtTheCapIsNotOver(t *testing.T) {
	bs := []Branch{{Name: "a", Commit: ago(1)}, {Name: "b", Commit: ago(1)}, {Name: "c", Commit: ago(1)}}
	if fs := Evaluate(policy(), bs, now); len(fs) != 0 {
		t.Fatalf("findings: %+v", fs)
	}
}

func TestDormancyThresholds(t *testing.T) {
	p := policy()
	p.Cap = 10
	bs := []Branch{{Name: "three", Commit: ago(3)}, {Name: "warn", Commit: ago(3.1)}, {Name: "seven", Commit: ago(7)}, {Name: "fail", Commit: ago(7.1)}}
	fs := Evaluate(p, bs, now)
	w, f := groups(fs, DormantWarn), groups(fs, DormantFail)
	if len(w) != 2 || w[0].Fail || w[0].Branch != "seven" || w[1].Branch != "warn" {
		t.Fatalf("warn: %+v", w)
	}
	if len(f) != 1 || f[0].Branch != "fail" || !f[0].Fail {
		t.Fatalf("fail: %+v", f)
	}
}

func TestParkedBranchIsShownNotJudgedForDormancy(t *testing.T) {
	p := policy()
	p.Parked = []Parked{{Branch: "old", Reason: "owner decision", Until: "2026-11-03"}}
	fs := Evaluate(p, []Branch{{Name: "old", Commit: ago(40)}}, now)
	if len(fs) != 1 || fs[0].Group != Parking || fs[0].Fail || !strings.Contains(fs[0].Text, "owner decision") {
		t.Fatalf("parked: %+v", fs)
	}
}

func TestParkedPastItsDateFailsAndIsJudgedAgain(t *testing.T) {
	p := policy()
	p.Parked = []Parked{{Branch: "old", Reason: "r", Until: "2026-11-02"}}
	fs := Evaluate(p, []Branch{{Name: "old", Commit: ago(40)}}, now)
	pk := groups(fs, Parking)
	if len(pk) != 1 || !pk[0].Fail || !strings.Contains(pk[0].Text, "EXPIRED") {
		t.Fatalf("expired: %+v", fs)
	}
	if len(groups(fs, DormantFail)) != 1 {
		t.Fatalf("an expired park still shields the branch: %+v", fs)
	}
}

func TestParkedEntryForNoOpenBranchFails(t *testing.T) {
	p := policy()
	p.Parked = []Parked{{Branch: "gone", Reason: "r", Until: "2027-01-01"}}
	fs := groups(Evaluate(p, nil, now), Parking)
	if len(fs) != 1 || !fs[0].Fail || !strings.Contains(fs[0].Text, "no open branch") {
		t.Fatalf("stale park: %+v", fs)
	}
}

func TestParkedBranchStillCountsAgainstTheCap(t *testing.T) {
	p := policy()
	p.Cap = 1
	p.Parked = []Parked{{Branch: "a", Reason: "r", Until: "2027-01-01"}}
	fs := Evaluate(p, []Branch{{Name: "a", Commit: ago(40)}, {Name: "b", Commit: ago(1)}}, now)
	if len(groups(fs, OverCap)) != 3 {
		t.Fatalf("cap: %+v", fs)
	}
}

func TestValidate(t *testing.T) {
	ok := Parked{Branch: "a", Reason: "r", Until: "2027-01-01"}
	bad := []Policy{
		{Cap: 0, WarnDays: 1, FailDays: 2},
		{Cap: 1, WarnDays: 0, FailDays: 2},
		{Cap: 1, WarnDays: 2, FailDays: 2},
		{Cap: 1, WarnDays: 1, FailDays: 2, Parked: []Parked{{Reason: "r", Until: "2027-01-01"}}},
		{Cap: 1, WarnDays: 1, FailDays: 2, Parked: []Parked{{Branch: "a", Until: "2027-01-01"}}},
		{Cap: 1, WarnDays: 1, FailDays: 2, Parked: []Parked{{Branch: "a", Reason: "r", Until: "soon"}}},
		{Cap: 1, WarnDays: 1, FailDays: 2, Parked: []Parked{ok, ok}},
	}
	for i, p := range bad {
		if err := p.Validate(); err == nil {
			t.Errorf("policy %d accepted: %+v", i, p)
		}
	}
	if err := policy().Validate(); err != nil {
		t.Fatalf("good policy: %v", err)
	}
}
