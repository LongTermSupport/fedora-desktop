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
	if !validRepo(g.Repo) {
		return "", false, fmt.Errorf("repository %q is not owner/name", g.Repo)
	}
	if !commitSHA.MatchString(sha) {
		return "", false, fmt.Errorf("%q is not a full commit sha", sha)
	}
	res, err := g.Runner.Run(ctx, execx.Cmd{
		Name: ghProgram,
		Args: []string{"api", "repos/" + g.Repo + "/commits/" + sha + "/pulls", "--jq", pullFields},
	})
	if err != nil {
		return "", false, fmt.Errorf("gh api pulls of %s: %w", sha, err)
	}
	var pulls []pull
	if err := json.Unmarshal(res.Stdout, &pulls); err != nil {
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
