// Package releasever computes the next release version from the commits since the last release
// (Plan 00153 Task 5.2; the rule is the owner's, issue 82). It is pure: the caller reads the
// branch, the last release and the commit messages, and acts on the answer.
//
// The rules, each pinned by a test:
//
//   - V1: a release is on branch F<major> and is named <major>.<minor>.<patch>; the major is the
//     branch's, never the commits'.
//   - V2: the header of a commit is a conventional commit, type(scope)!: description. Only a
//     header decides: "feat" is a minor release and "fix" a patch release, whatever else the
//     body says; every other type, and a subject that is no header (a merge, a revert, a
//     "Release x.y.z" commit, a plain sentence), releases nothing.
//   - V3: the highest bump wins; a minor resets the patch.
//   - V4: a range with no releasable commit makes no release (ErrNoRelease).
//   - V5: a breaking marker, a "!" before the colon or a "BREAKING CHANGE:" or
//     "BREAKING-CHANGE:" line starting a line of the body, is refused on a Fedora-major branch
//     that already has a release (ErrBreaking), on any type; a release never changes the major.
//   - V6: the major's first release is <major>.0.0, and there a breaking marker is allowed,
//     because the major itself is what changes.
package releasever

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	// ErrBranch is a branch that is not F<major>.
	ErrBranch = errors.New("not a Fedora-major release branch (F<major>)")
	// ErrVersion is text that is not <major>.<minor>.<patch>, or a release of another major.
	ErrVersion = errors.New("not a release version of this branch")
	// ErrNoRelease is a range with no feat or fix commit.
	ErrNoRelease = errors.New("no releasable commit since the last release")
	// ErrBreaking is a breaking marker on a branch whose major already has a release.
	ErrBreaking = errors.New("breaking change on a Fedora-major branch that already has a release")
)

// maxDigits bounds a number, so the arithmetic below cannot overflow an int.
const maxDigits = 9

// header is type(scope)!: description. The description starts with a non-space character.
var header = regexp.MustCompile(`^([A-Za-z]+)(?:\([^()]*\))?(!)?: \S`)

// breakingFooter is a body line that declares a breaking change.
var breakingFooter = regexp.MustCompile(`(?m)^BREAKING[ -]CHANGE: `)

// Bump is how far a release moves the version.
type Bump int

// The bumps, lowest first.
const (
	BumpNone Bump = iota
	BumpPatch
	BumpMinor
)

// String names the bump.
func (b Bump) String() string {
	switch b {
	case BumpNone:
		return "none"
	case BumpPatch:
		return "patch"
	case BumpMinor:
		return "minor"
	default:
		return "unknown"
	}
}

// Version is a release version.
type Version struct{ Major, Minor, Patch int }

// String is <major>.<minor>.<patch>.
func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Release is the next release: its version, the bump that made it, and the subjects of the
// commits that decided it (the feat commits first, then the fix commits, each in log order).
type Release struct {
	Version    Version
	Bump       Bump
	Releasable []string
}

// BranchMajor is the Fedora major of an F<major> branch name (V1).
func BranchMajor(branch string) (int, error) {
	digits, ok := strings.CutPrefix(branch, "F")
	if !ok {
		return 0, fmt.Errorf("%w: %q", ErrBranch, branch)
	}
	n, err := count(digits)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%w: %q", ErrBranch, branch)
	}
	return n, nil
}

// ParseVersion reads <major>.<minor>.<patch>: three counts with no sign, no leading zero and
// nothing else around them.
func ParseVersion(s string) (Version, error) {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("%w: %q", ErrVersion, s)
	}
	var n [3]int
	for i, p := range parts {
		c, err := count(p)
		if err != nil {
			return Version{}, fmt.Errorf("%w: %q", ErrVersion, s)
		}
		n[i] = c
	}
	return Version{n[0], n[1], n[2]}, nil
}

// Split cuts the output of `git log -z --format=%B` into messages: one per NUL, trimmed, the
// empty ones dropped.
func Split(raw string) []string {
	var out []string
	for m := range strings.SplitSeq(raw, "\x00") {
		if m = strings.TrimSpace(m); m != "" {
			out = append(out, m)
		}
	}
	return out
}

// Next is the release after last on branch (V1 to V6). last is the newest release of the
// branch's major, or empty when the major has none. messages are the commits since it.
func Next(branch, last string, messages []string) (Release, error) {
	major, err := BranchMajor(branch)
	if err != nil {
		return Release{}, err
	}
	prev, hasLast, err := lastRelease(major, last)
	if err != nil {
		return Release{}, err
	}
	commits := make([]commit, len(messages))
	for i, m := range messages {
		commits[i] = classify(m)
		if hasLast && commits[i].breaking {
			return Release{}, fmt.Errorf("%w: %q", ErrBreaking, commits[i].subject)
		}
	}
	rel := decide(commits)
	if rel.Bump == BumpNone {
		return Release{}, ErrNoRelease
	}
	rel.Version = bumped(major, prev, hasLast, rel.Bump)
	return rel, nil
}

// commit is one message read for its header and its footers.
type commit struct {
	subject  string
	bump     Bump
	breaking bool
}

func classify(message string) commit {
	lines := strings.SplitN(message, "\n", 2)
	c := commit{subject: strings.TrimSpace(lines[0])}
	if m := header.FindStringSubmatch(c.subject); m != nil {
		switch strings.ToLower(m[1]) {
		case "feat":
			c.bump = BumpMinor
		case "fix":
			c.bump = BumpPatch
		}
		c.breaking = m[2] == "!"
	}
	if len(lines) == 2 && breakingFooter.MatchString(lines[1]) {
		c.breaking = true
	}
	return c
}

// decide is the highest bump and the subjects behind it, feat commits first.
func decide(commits []commit) Release {
	var rel Release
	for _, want := range []Bump{BumpMinor, BumpPatch} {
		for _, c := range commits {
			if c.bump == want {
				rel.Releasable = append(rel.Releasable, c.subject)
				rel.Bump = max(rel.Bump, want)
			}
		}
	}
	return rel
}

func lastRelease(major int, last string) (Version, bool, error) {
	if last == "" {
		return Version{}, false, nil
	}
	v, err := ParseVersion(last)
	if err != nil {
		return Version{}, false, err
	}
	if v.Major != major {
		return Version{}, false, fmt.Errorf("%w: %s is not a release of F%d", ErrVersion, last, major)
	}
	return v, true, nil
}

func bumped(major int, prev Version, hasLast bool, by Bump) Version {
	switch {
	case !hasLast:
		return Version{Major: major}
	case by == BumpMinor:
		return Version{major, prev.Minor + 1, 0}
	default:
		return Version{major, prev.Minor, prev.Patch + 1}
	}
}

// count reads a non-negative integer written in digits with no leading zero.
func count(s string) (int, error) {
	if s == "" || len(s) > maxDigits || (len(s) > 1 && s[0] == '0') {
		return 0, ErrVersion
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, ErrVersion
		}
	}
	return strconv.Atoi(s)
}
