// Package branchhygiene is the rule behind the branch-hygiene job: a repository's open work
// branches judged against a policy (a cap on their number, no merged branch left undeleted, no
// branch dormant past a limit unless it is parked on a human decision with a review date).
// The rule is pure: it takes the branch facts, the policy and the time, and returns findings.
// Reading the policy and asking git for the facts is run.go.
package branchhygiene

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

// Group is the heading a finding is printed under; the order here is the print order.
type Group string

// The finding groups.
const (
	MergedNotDeleted Group = "MERGED-NOT-DELETED"
	OverCap          Group = "OVER-CAP"
	DormantFail      Group = "DORMANT-FAIL"
	DormantWarn      Group = "DORMANT-WARN"
	Parking          Group = "PARKED"
)

// Groups is every group in print order.
var Groups = []Group{MergedNotDeleted, OverCap, DormantFail, DormantWarn, Parking}

const dateLayout = "2006-01-02"

const day = 24 * time.Hour

// Parked is a branch held on a human decision until a review date.
type Parked struct {
	Branch string `toml:"branch"`
	Reason string `toml:"reason"`
	Until  string `toml:"until"`
}

// Policy is the owner's branch policy.
type Policy struct {
	Cap      int      `toml:"cap"`
	WarnDays int      `toml:"warn_days"`
	FailDays int      `toml:"fail_days"`
	Exempt   []string `toml:"exempt"`
	Parked   []Parked `toml:"parked"`
}

// Branch is what git says about one remote branch.
type Branch struct {
	Name   string
	Commit time.Time
	// Merged is that the branch tip is an ancestor of the default branch.
	Merged bool
}

// Finding is one line of the report.
type Finding struct {
	Group  Group
	Branch string
	Text   string
	Fail   bool
}

// Validate refuses a policy the rule cannot judge by.
func (p Policy) Validate() error {
	if p.Cap < 1 {
		return errors.New("cap must be at least 1")
	}
	if p.WarnDays < 1 || p.FailDays <= p.WarnDays {
		return errors.New("need 1 <= warn_days < fail_days")
	}
	seen := map[string]bool{}
	for i, pk := range p.Parked {
		if pk.Branch == "" || pk.Reason == "" {
			return fmt.Errorf("parked entry %d needs a branch and a reason", i+1)
		}
		if _, err := time.Parse(dateLayout, pk.Until); err != nil {
			return fmt.Errorf("parked %s: until %q is not a date (YYYY-MM-DD)", pk.Branch, pk.Until)
		}
		if seen[pk.Branch] {
			return fmt.Errorf("parked %s is listed twice", pk.Branch)
		}
		seen[pk.Branch] = true
	}
	return nil
}

// ageDays is the whole days since t.
func ageDays(now, t time.Time) int { return int(now.Sub(t) / day) }

// Evaluate judges the branches. Findings come grouped in Groups order, oldest branch first.
func Evaluate(p Policy, branches []Branch, now time.Time) []Finding {
	var merged, open []Branch
	for _, b := range branches {
		switch {
		case slices.Contains(p.Exempt, b.Name):
		case b.Merged:
			merged = append(merged, b)
		default:
			open = append(open, b)
		}
	}
	byAge := func(a, b Branch) int { return a.Commit.Compare(b.Commit) }
	slices.SortStableFunc(merged, byAge)
	slices.SortStableFunc(open, byAge)

	var out []Finding
	for _, b := range merged {
		out = append(out, Finding{MergedNotDeleted, b.Name, b.Name + " is merged into the default branch; delete it", true})
	}
	if len(open) > p.Cap {
		out = append(out, Finding{OverCap, "", fmt.Sprintf("%d open work branches, cap %d", len(open), p.Cap), true})
		for _, b := range open {
			out = append(out, Finding{OverCap, b.Name, fmt.Sprintf("  %s: last commit %d days ago", b.Name, ageDays(now, b.Commit)), true})
		}
	}
	parkedOut, shielded := judgeParked(p, open, now)
	for _, g := range []Group{DormantFail, DormantWarn} {
		for _, b := range open {
			if shielded[b.Name] || dormancy(p, b, now) != g {
				continue
			}
			text := fmt.Sprintf("%s: no commit for %d days", b.Name, ageDays(now, b.Commit))
			out = append(out, Finding{g, b.Name, text, g == DormantFail})
		}
	}
	return append(out, parkedOut...)
}

// dormancy is the group a branch's idleness puts it in, or "" when it is not dormant.
func dormancy(p Policy, b Branch, now time.Time) Group {
	idleDays := now.Sub(b.Commit).Hours() / 24
	switch {
	case idleDays > float64(p.FailDays):
		return DormantFail
	case idleDays > float64(p.WarnDays):
		return DormantWarn
	}
	return ""
}

// judgeParked is the PARKED findings and the set of branches a live park shields from dormancy.
func judgeParked(p Policy, open []Branch, now time.Time) ([]Finding, map[string]bool) {
	today := now.UTC().Truncate(day)
	shielded := map[string]bool{}
	var out []Finding
	for _, pk := range p.Parked {
		until, err := time.Parse(dateLayout, pk.Until)
		switch {
		case err != nil:
			out = append(out, Finding{Parking, pk.Branch, fmt.Sprintf("%s: until %q is not a date", pk.Branch, pk.Until), true})
		case !slices.ContainsFunc(open, func(b Branch) bool { return b.Name == pk.Branch }):
			out = append(out, Finding{Parking, pk.Branch, pk.Branch + " is parked but there is no open branch of that name; remove the entry", true})
		case until.Before(today):
			out = append(out, Finding{Parking, pk.Branch, fmt.Sprintf("%s: park EXPIRED on %s (%s); decide, or renew the entry", pk.Branch, pk.Until, pk.Reason), true})
		default:
			shielded[pk.Branch] = true
			out = append(out, Finding{Parking, pk.Branch, fmt.Sprintf("%s: parked until %s (%s)", pk.Branch, pk.Until, pk.Reason), false})
		}
	}
	return out, shielded
}
