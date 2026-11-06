// Package branchdelete is branch-delete-list: a candidate list of origin's branch names turned
// into the commands that delete them, or refused. A candidate is refused, never dropped, when it
// is exempt or parked in scripts/branch-policy.toml (read by branchhygiene.ReadPolicy, as
// qa-branch-hygiene reads it), when it is a release line (F followed by a digit: F44, F42,
// F44-arm), when it is origin's default branch, when origin has no branch of that name, or when
// origin has no refs/archive/<name> at exactly the branch's tip. One refusal refuses the list:
// the owner gets a delete list only when every name passes. It deletes nothing itself.
//
// The rule (Judge) is pure over the policy, origin's refs and the names; run.go asks git.
package branchdelete

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/branchhygiene"
)

// Remote is what origin holds: its default branch, and the commit of each branch and archive ref.
type Remote struct {
	Default  string
	Heads    map[string]string
	Archives map[string]string
}

// Refusal is one candidate refused, with every reason that applies.
type Refusal struct {
	Branch  string
	Reasons []string
}

const archivePrefix = "refs/archive/"

var (
	// releaseLine is the glob F[0-9]*: the per-version default-branch lines.
	releaseLine = regexp.MustCompile(`^F[0-9]`)
	// plainName is what may be printed into a shell command unquoted.
	plainName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
)

// Candidates is the names in text, one per line, surrounding space trimmed, blank lines skipped.
func Candidates(text string) []string {
	var names []string
	for line := range strings.SplitSeq(text, "\n") {
		if name := strings.TrimSpace(line); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// Judge is the refusals for the candidates, in input order; none means every one may go.
func Judge(pol branchhygiene.Policy, remote Remote, candidates []string) []Refusal {
	var out []Refusal
	seen := map[string]int{}
	for _, name := range candidates {
		seen[name]++
	}
	reported := map[string]bool{}
	for _, name := range candidates {
		if reported[name] {
			continue
		}
		reported[name] = true
		var reasons []string
		if seen[name] > 1 {
			reasons = append(reasons, fmt.Sprintf("listed twice (%d times)", seen[name]))
		}
		reasons = append(reasons, nameReasons(pol, remote, name)...)
		if len(reasons) > 0 {
			out = append(out, Refusal{Branch: name, Reasons: reasons})
		}
	}
	return out
}

func nameReasons(pol branchhygiene.Policy, remote Remote, name string) []string {
	var reasons []string
	switch {
	case strings.HasPrefix(name, "origin/") || strings.HasPrefix(name, "refs/"):
		reasons = append(reasons, "give the bare branch name, as origin lists it under refs/heads/")
	case !plainName.MatchString(name):
		reasons = append(reasons, "not a plain branch name (letters, digits, . _ / -, not starting with - or .)")
	}
	if slices.Contains(pol.Exempt, name) {
		reasons = append(reasons, "exempt in "+branchhygiene.PolicyFile)
	}
	if i := slices.IndexFunc(pol.Parked, func(p branchhygiene.Parked) bool { return p.Branch == name }); i >= 0 {
		pk := pol.Parked[i]
		reasons = append(reasons, fmt.Sprintf("parked in %s until %s (%s)", branchhygiene.PolicyFile, pk.Until, pk.Reason))
	}
	if releaseLine.MatchString(name) {
		reasons = append(reasons, "a release line (F[0-9]*); release lines are never deleted")
	}
	if name == remote.Default {
		reasons = append(reasons, "the default branch of origin")
	}
	if len(reasons) > 0 {
		// A branch that is never deleted is not told how to archive its way through.
		return reasons
	}
	return archiveReasons(remote, name)
}

// archiveReasons is why the branch's tip is not safe in refs/archive/<name>.
func archiveReasons(remote Remote, name string) []string {
	tip, ok := remote.Heads[name]
	if !ok {
		return []string{"no branch " + name + " on origin"}
	}
	archived, ok := remote.Archives[name]
	switch {
	case !ok:
		return []string{fmt.Sprintf("no %s%s on origin; archive the tip first: git push origin %s:%s%s", archivePrefix, name, tip, archivePrefix, name)}
	case archived != tip:
		return []string{fmt.Sprintf("%s%s is at %s but the branch tip is %s; archive the current tip first", archivePrefix, name, archived, tip)}
	}
	return nil
}
