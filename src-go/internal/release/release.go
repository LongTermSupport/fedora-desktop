// Package release decides what a push to a release branch should do, for the release workflow
// (Plan 00153 Task 5.3, issue 82, owner decisions D1 to D4). It reads the branch, the tags and
// the merged pull requests, and answers with a Plan; the workflow's steps act on the answer.
//
// The rules, each pinned by a test:
//
//   - R1: only a branch named F<major> releases (releasever.BranchMajor).
//   - R2: the last release is the highest tag <major>.<minor>.<patch> of the branch's major, by
//     number; a tag of any other shape (a release candidate, another major) is not a release.
//   - R3: a major with no release tag is left alone: its first release, <major>.0.0, is made by
//     `scripts/release.bash prepare first`, because the history to judge is the whole branch.
//   - R4: a commit whose subject is exactly `Release <version>`, of this major, after the last
//     release, with no tag yet, and that is the head of a pull request merged into this branch
//     from the branch release-<version> of this repository, is a release commit waiting for its
//     tag: the answer is to tag it (the highest such version if there are several). The subject
//     alone is not enough: a commit that merely carries one (a contributor's, a rebased release)
//     can never be tagged by the workflow, so counting it would answer "tag" on every later push.
//   - R5: otherwise the changes since the last release are the first-parent commits after its
//     tag. A merge commit stands for the pull request merged by it (title, then body), which is
//     where the owner's convention is checked (D4); any other commit, and a merge with no pull
//     request behind it, stands for its own message.
//   - R6: the version rule (releasever.Next) judges those messages: no releasable change is no
//     release, and a breaking change on a released major is refused, as an error.
//   - R7: a release whose branch release-<version> is already on the remote is not prepared a
//     second time.
//
// Every question that fails is an error: a release is never decided on an answer that was not
// given.
package release

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/releasever"
)

// The actions a push can call for.
const (
	// ActionNone is nothing to release; Reason says why.
	ActionNone = "none"
	// ActionPrepare is a release to prepare: its version, and a pull request with the signed
	// release commit.
	ActionPrepare = "prepare"
	// ActionTag is a release commit to tag: its version and commit.
	ActionTag = "tag"
)

// Git is the questions Decide asks git; *gitx.Git answers them.
type Git interface {
	Tags(ctx context.Context, dir, pattern string) ([]string, error)
	FirstParentLog(ctx context.Context, dir, rev string) ([]gitx.LogEntry, error)
	SubjectsMatching(ctx context.Context, dir, rev, pattern string) ([]gitx.Subject, error)
	RemoteBranchExists(ctx context.Context, dir, remote, branch string) (bool, error)
}

// Pulls is the one question asked of GitHub: the pull request a merge commit merged.
type Pulls interface {
	// Message is the title of the pull request merged by the commit sha, then its body, and
	// whether there is one.
	Message(ctx context.Context, sha string) (message string, found bool, err error)
	// MergedFrom is whether sha is the head of a pull request merged into the branch from the
	// branch head of this repository.
	MergedFrom(ctx context.Context, sha, head string) (bool, error)
}

// Plan is what a push calls for.
type Plan struct {
	Action     string
	Version    string   // the release, for ActionPrepare and ActionTag
	Commit     string   // the release commit, for ActionTag
	Reason     string   // why there is nothing to do, for ActionNone
	Releasable []string // what decided the version, for ActionPrepare
}

// Decider decides for one branch of one checkout.
type Decider struct {
	Git    Git
	Pulls  Pulls
	Dir    string // the checkout
	Branch string // F<major>
	Remote string // where release-<version> branches are looked for
}

// releaseSubject is the subject of a release commit.
var releaseSubject = regexp.MustCompile(`^Release (\S+)$`)

// releaseGrep selects the candidates of R4 in git; each subject is judged again here.
const releaseGrep = `^Release [0-9]+[.][0-9]+[.][0-9]+$`

// Decide answers what the push to d.Branch calls for (R1 to R7).
func (d Decider) Decide(ctx context.Context) (Plan, error) {
	major, err := releasever.BranchMajor(d.Branch)
	if err != nil {
		return Plan{}, err
	}
	tags, err := d.Git.Tags(ctx, d.Dir, fmt.Sprintf("%d.*", major))
	if err != nil {
		return Plan{}, err
	}
	releases := releaseTags(major, tags)
	if len(releases) == 0 {
		return none(fmt.Sprintf("F%d has no release yet: its first release is made with scripts/release.bash prepare first", major)), nil
	}
	last := releases[len(releases)-1]
	plan, found, err := d.pendingTag(ctx, major, last)
	if err != nil || found {
		return plan, err
	}
	messages, err := d.changes(ctx, last)
	if err != nil {
		return Plan{}, err
	}
	next, err := releasever.Next(d.Branch, last.String(), messages)
	if errors.Is(err, releasever.ErrNoRelease) {
		return none("no releasable change (feat or fix) since " + last.String()), nil
	}
	if err != nil {
		return Plan{}, err
	}
	branch := "release-" + next.Version.String()
	open, err := d.Git.RemoteBranchExists(ctx, d.Dir, d.Remote, branch)
	if err != nil {
		return Plan{}, err
	}
	if open {
		return none(fmt.Sprintf("the release %s is already prepared: branch %s is on %s (if its pull request was closed without merging, delete the branch and push again)", next.Version, branch, d.Remote)), nil
	}
	return Plan{Action: ActionPrepare, Version: next.Version.String(), Releasable: next.Releasable}, nil
}

func none(reason string) Plan { return Plan{Action: ActionNone, Reason: reason} }

// releaseTags is the release versions among tags (R2), lowest first.
func releaseTags(major int, tags []string) []releasever.Version {
	var out []releasever.Version
	for _, tag := range tags {
		if v, err := releasever.ParseVersion(tag); err == nil && v.Major == major {
			out = append(out, v)
		}
	}
	slices.SortFunc(out, compare)
	return out
}

func compare(a, b releasever.Version) int {
	if a.Minor != b.Minor {
		return a.Minor - b.Minor
	}
	return a.Patch - b.Patch
}

// releaseCandidate is a commit with a release subject, before GitHub says whether it is one.
type releaseCandidate struct {
	version releasever.Version
	sha     string
}

// pendingTag is R4: the highest release commit after last that a merged release pull request
// brought in. A version above the newest tag has no tag, so "after last" is also "whose tag is
// absent". The candidates are asked highest first, and a lookup that fails is an error.
func (d Decider) pendingTag(ctx context.Context, major int, last releasever.Version) (Plan, bool, error) {
	subjects, err := d.Git.SubjectsMatching(ctx, d.Dir, "HEAD", releaseGrep)
	if err != nil {
		return Plan{}, false, err
	}
	var candidates []releaseCandidate
	for _, s := range subjects {
		m := releaseSubject.FindStringSubmatch(s.Subject)
		if m == nil {
			continue
		}
		v, parseErr := releasever.ParseVersion(m[1])
		if parseErr != nil || v.Major != major || compare(v, last) <= 0 {
			continue
		}
		candidates = append(candidates, releaseCandidate{version: v, sha: s.SHA})
	}
	slices.SortFunc(candidates, func(a, b releaseCandidate) int { return compare(b.version, a.version) })
	for _, c := range candidates {
		merged, err := d.Pulls.MergedFrom(ctx, c.sha, "release-"+c.version.String())
		if err != nil {
			return Plan{}, false, err
		}
		if merged {
			return Plan{Action: ActionTag, Version: c.version.String(), Commit: c.sha}, true, nil
		}
	}
	return Plan{}, false, nil
}

// changes is R5: the message that stands for each first-parent commit after last, newest first.
func (d Decider) changes(ctx context.Context, last releasever.Version) ([]string, error) {
	entries, err := d.Git.FirstParentLog(ctx, d.Dir, last.String()+"..HEAD")
	if err != nil {
		return nil, err
	}
	messages := make([]string, 0, len(entries))
	for _, e := range entries {
		message := e.Message
		if e.Parents > 1 {
			pr, found, err := d.Pulls.Message(ctx, e.SHA)
			if err != nil {
				return nil, err
			}
			if found {
				message = strings.TrimSpace(pr)
			}
		}
		messages = append(messages, message)
	}
	return messages, nil
}
