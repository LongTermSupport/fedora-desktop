package release

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// ghProgram is the GitHub CLI; it takes its token from GH_TOKEN in the environment.
const ghProgram = "gh"

var (
	// repoName is owner/name as GitHub allows them: nothing that could change a URL path.
	repoName = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	// commitSHA is a full lowercase sha.
	commitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// validRepo is owner/name where neither part is a path step (. or ..): the name is put in a URL path.
func validRepo(repo string) bool {
	if !repoName.MatchString(repo) {
		return false
	}
	for part := range strings.SplitSeq(repo, "/") {
		if strings.Trim(part, ".") == "" {
			return false
		}
	}
	return true
}

// GitHubPulls answers Pulls through `gh api`.
type GitHubPulls struct {
	Runner execx.Runner
	Repo   string // owner/name
	Branch string // the base branch a merge must have been into
}

// pullFields reshapes GitHub's pull requests (snake_case, nested) to the fields read here, so
// the wire format stays GitHub's and the struct below is ours.
const pullFields = `[.[] | {title: .title, body: .body, mergeCommitSha: .merge_commit_sha, baseRef: .base.ref}]`

// pull is the part of a pull request read, as pullFields shapes it.
type pull struct {
	Title          string  `json:"title"`
	Body           *string `json:"body"`
	MergeCommitSHA *string `json:"mergeCommitSha"`
	BaseRef        string  `json:"baseRef"`
}

// Message is the title, then the body, of the pull request whose merge commit is sha on the
// base branch. A pull request that only contains the commit, or went to another branch, is
// not the one. GitHub failing, or answering in a shape that is not a list of pull requests, is
// an error: a merge is never read as "no pull request" on an answer that was not given.
func (g GitHubPulls) Message(ctx context.Context, sha string) (string, bool, error) {
	answer, err := g.pullsOf(ctx, sha, pullFields)
	if err != nil {
		return "", false, err
	}
	var pulls []pull
	if err := json.Unmarshal(answer, &pulls); err != nil {
		return "", false, fmt.Errorf("gh api pulls of %s: not a list of pull requests: %w", sha, err)
	}
	for _, p := range pulls {
		if p.MergeCommitSHA == nil || *p.MergeCommitSHA != sha || p.BaseRef != g.Branch {
			continue
		}
		message := strings.TrimSpace(p.Title)
		if p.Body != nil && strings.TrimSpace(*p.Body) != "" {
			message += "\n\n" + strings.TrimSpace(*p.Body)
		}
		return message, true, nil
	}
	return "", false, nil
}

// headFields reshapes the same list to what MergedFrom reads: the branch and repository a pull
// request came from, the commit that was its head, the branch it went into, and whether it was
// merged.
const headFields = `[.[] | {headRef: .head.ref, headRepo: .head.repo.full_name, headSha: .head.sha, baseRef: .base.ref, merged: (.merged_at != null)}]`

// headPull is the part of a pull request MergedFrom reads, as headFields shapes it. HeadRepo is
// null when the repository it came from is gone.
type headPull struct {
	HeadRef  string  `json:"headRef"`
	HeadRepo *string `json:"headRepo"`
	HeadSHA  string  `json:"headSha"`
	BaseRef  string  `json:"baseRef"`
	Merged   bool    `json:"merged"`
}

// MergedFrom is whether sha is the head of a pull request that was merged into the base branch
// from the branch head of this same repository. A commit that carries a release subject any other
// way (a contributor's, a rebased one) is not the release pull request's head, and a pull request
// from a fork, or one that was closed unmerged, is not it either. An answer that was not given is
// an error, never "not merged".
func (g GitHubPulls) MergedFrom(ctx context.Context, sha, head string) (bool, error) {
	answer, err := g.pullsOf(ctx, sha, headFields)
	if err != nil {
		return false, err
	}
	var pulls []headPull
	if err := json.Unmarshal(answer, &pulls); err != nil {
		return false, fmt.Errorf("gh api pulls of %s: not a list of pull requests: %w", sha, err)
	}
	for _, p := range pulls {
		if p.Merged && p.HeadRef == head && p.HeadSHA == sha && p.BaseRef == g.Branch &&
			p.HeadRepo != nil && strings.EqualFold(*p.HeadRepo, g.Repo) {
			return true, nil
		}
	}
	return false, nil
}

// pullsOf asks GitHub for the pull requests associated with a commit, reshaped by jq, and
// returns its answer. The repository and the commit are put in a URL path, so a malformed one is
// refused before gh runs.
func (g GitHubPulls) pullsOf(ctx context.Context, sha, jq string) ([]byte, error) {
	if !validRepo(g.Repo) {
		return nil, fmt.Errorf("repository %q is not owner/name", g.Repo)
	}
	if !commitSHA.MatchString(sha) {
		return nil, fmt.Errorf("%q is not a full commit sha", sha)
	}
	res, err := g.Runner.Run(ctx, execx.Cmd{
		Name: ghProgram,
		Args: []string{"api", "repos/" + g.Repo + "/commits/" + sha + "/pulls", "--jq", jq},
	})
	if err != nil {
		return nil, fmt.Errorf("gh api pulls of %s: %w", sha, err)
	}
	return res.Stdout, nil
}
