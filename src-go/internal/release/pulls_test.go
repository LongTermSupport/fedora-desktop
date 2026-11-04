package release_test

import (
	"context"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/release"
)

const pullsEndpoint = "repos/acme/widgets/commits/" + sha1 + "/pulls"

func ghAnswers(body string) *execx.Fake {
	f := &execx.Fake{}
	f.On([]string{"gh", "api", pullsEndpoint}, execx.Result{Stdout: []byte(body)})
	return f
}

func gh(f *execx.Fake) release.GitHubPulls {
	return release.GitHubPulls{Runner: f, Repo: "acme/widgets", Branch: "F44"}
}

func TestThePullRequestOfAMergeCommitIsItsTitleThenItsBody(t *testing.T) {
	f := ghAnswers(`[{"title":"feat: a thing","body":"Why it matters.","mergeCommitSha":"` + sha1 + `","baseRef":"F44"}]`)
	msg, found, err := gh(f).Message(context.Background(), sha1)
	if err != nil || !found {
		t.Fatalf("got %q, %v, %v", msg, found, err)
	}
	if msg != "feat: a thing\n\nWhy it matters." {
		t.Fatalf("got %q", msg)
	}
	argv := f.Calls()[0].Args
	if len(argv) != 4 || argv[0] != "api" || argv[1] != pullsEndpoint || argv[2] != "--jq" || !strings.Contains(argv[3], "merge_commit_sha") {
		t.Fatalf("ran gh %q, want the endpoint and a --jq that reshapes the answer", argv)
	}
}

func TestAPullRequestWithNoBodyIsJustItsTitle(t *testing.T) {
	f := ghAnswers(`[{"title":"fix: a bug","body":null,"mergeCommitSha":"` + sha1 + `","baseRef":"F44"}]`)
	msg, found, err := gh(f).Message(context.Background(), sha1)
	if err != nil || !found || msg != "fix: a bug" {
		t.Fatalf("got %q, %v, %v", msg, found, err)
	}
}

// A pull request that merely contains the commit, or merged into another branch, is not the
// one this merge commit made.
func TestOnlyThePullRequestThatMadeTheMergeOnThisBranchCounts(t *testing.T) {
	other := strings.Repeat("9", 40)
	cases := map[string]string{
		"no pull request":             `[]`,
		"another merge commit":        `[{"title":"feat: x","body":"","mergeCommitSha":"` + other + `","baseRef":"F44"}]`,
		"another base branch":         `[{"title":"feat: x","body":"","mergeCommitSha":"` + sha1 + `","baseRef":"F43"}]`,
		"a pull request not yet done": `[{"title":"feat: x","body":"","mergeCommitSha":null,"baseRef":"F44"}]`,
	}
	for name, body := range cases {
		msg, found, err := gh(ghAnswers(body)).Message(context.Background(), sha1)
		if err != nil || found || msg != "" {
			t.Fatalf("%s: got %q, %v, %v, want none found", name, msg, found, err)
		}
	}
}

func TestGitHubFailingIsAnErrorNotNoPullRequest(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"gh", "api"}, execx.Result{ExitCode: 1, Stderr: []byte("HTTP 502")})
	if _, found, err := gh(f).Message(context.Background(), sha1); err == nil || found {
		t.Fatalf("got found=%v, %v: a failed lookup must not read as a merge with no pull request", found, err)
	}
}

func TestAnAnswerThatIsNotJSONIsAnError(t *testing.T) {
	if _, _, err := gh(ghAnswers(`<html>rate limited</html>`)).Message(context.Background(), sha1); err == nil {
		t.Fatal("want an error")
	}
}

const releasePull = `{"headRef":"release-44.1.0","headRepo":"acme/widgets","headSha":"` + sha1 + `","baseRef":"F44","merged":true}`

func TestTheHeadOfAMergedReleasePullRequestIsFound(t *testing.T) {
	f := ghAnswers(`[` + releasePull + `]`)
	ok, err := gh(f).MergedFrom(context.Background(), sha1, "release-44.1.0")
	if err != nil || !ok {
		t.Fatalf("got %v, %v, want the merged release pull request found", ok, err)
	}
	argv := f.Calls()[0].Args
	if len(argv) != 4 || argv[1] != pullsEndpoint || argv[2] != "--jq" ||
		!strings.Contains(argv[3], "head.ref") || !strings.Contains(argv[3], "merged_at") {
		t.Fatalf("ran gh %q, want the endpoint and a --jq that reads the head branch and when it was merged", argv)
	}
}

// Only a pull request that was merged, from this very branch of this repository, into this base,
// with this commit as its head, is the release pull request. Anything else only carries the
// subject, and must not stand for a release.
func TestOnlyAMergedPullRequestFromTheReleaseBranchOfThisRepositoryCounts(t *testing.T) {
	other := strings.Repeat("9", 40)
	with := func(old, repl string) string { return `[` + strings.Replace(releasePull, old, repl, 1) + `]` }
	cases := map[string]string{
		"no pull request":     `[]`,
		"another head branch": with(`"release-44.1.0"`, `"feat/x"`),
		"another version":     with(`"release-44.1.0"`, `"release-44.2.0"`),
		"a fork":              with(`"acme/widgets"`, `"mallory/widgets"`),
		"a deleted fork":      with(`"headRepo":"acme/widgets"`, `"headRepo":null`),
		"not merged":          with(`"merged":true`, `"merged":false`),
		"another base":        with(`"baseRef":"F44"`, `"baseRef":"F43"`),
		"another head commit": with(`"headSha":"`+sha1+`"`, `"headSha":"`+other+`"`),
	}
	for name, body := range cases {
		ok, err := gh(ghAnswers(body)).MergedFrom(context.Background(), sha1, "release-44.1.0")
		if err != nil || ok {
			t.Fatalf("%s: got %v, %v, want not found", name, ok, err)
		}
	}
}

func TestMergedFromReadsAFailedLookupAndAnUnreadableAnswerAsErrors(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"gh", "api"}, execx.Result{ExitCode: 1, Stderr: []byte("HTTP 502")})
	if ok, err := gh(f).MergedFrom(context.Background(), sha1, "release-44.1.0"); err == nil || ok {
		t.Fatalf("got %v, %v: a failed lookup must not read as not merged", ok, err)
	}
	if _, err := gh(ghAnswers(`<html>rate limited</html>`)).MergedFrom(context.Background(), sha1, "release-44.1.0"); err == nil {
		t.Fatal("want an error for an answer that is not JSON")
	}
}

func TestMergedFromRefusesAMalformedRepositoryOrCommitBeforeGHRuns(t *testing.T) {
	for _, c := range []struct{ repo, sha string }{{"../widgets", sha1}, {"acme/widgets", "abc"}, {"acme/widgets", sha1 + "/../x"}} {
		f := ghAnswers(`[]`)
		p := release.GitHubPulls{Runner: f, Repo: c.repo, Branch: "F44"}
		if _, err := p.MergedFrom(context.Background(), c.sha, "release-44.1.0"); err == nil {
			t.Fatalf("%q %q: want an error", c.repo, c.sha)
		}
		if len(f.Calls()) != 0 {
			t.Fatalf("%q %q: gh ran with %q", c.repo, c.sha, f.Calls()[0].Line())
		}
	}
}

// The repository and the commit are put in a URL path: anything that is not what they should
// be is refused before gh runs.
func TestAMalformedRepositoryOrCommitIsRefusedBeforeGHRuns(t *testing.T) {
	bad := []struct{ repo, sha string }{
		{"acme", sha1},
		{"acme/widgets/extra", sha1},
		{"acme/wid gets", sha1},
		{"../widgets", sha1},
		{"acme/widgets", "abc"},
		{"acme/widgets", strings.ToUpper(sha1) + "a"},
		{"acme/widgets", sha1 + "/../x"},
	}
	for _, c := range bad {
		f := ghAnswers(`[]`)
		p := release.GitHubPulls{Runner: f, Repo: c.repo, Branch: "F44"}
		if _, _, err := p.Message(context.Background(), c.sha); err == nil {
			t.Fatalf("%q %q: want an error", c.repo, c.sha)
		}
		if len(f.Calls()) != 0 {
			t.Fatalf("%q %q: gh ran with %q", c.repo, c.sha, f.Calls()[0].Line())
		}
	}
}
