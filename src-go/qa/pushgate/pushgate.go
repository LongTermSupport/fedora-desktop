// Package pushgate is the pre-push gate on CI's QA verdict (rule push-without-qa-verdict): a
// protected branch moves only to a commit that GitHub CI's QA workflow (.github/workflows/qa.yml)
// passed on a push. Work reaches a protected branch through a branch: the commit is pushed to
// ci/<topic>, CI judges it there with ./scripts/qa-changed.bash in diff mode against the line's
// tip (src-go/qa/ciscope), and that same commit is then pushed to the protected branch, whose own
// push runs the full ./scripts/qa-all.bash. Either run of the commit counts.
//
// The protected branches are the release and architecture lines that scripts/branch-policy.toml
// exempts (read by branchhygiene.ReadPolicy, as branch hygiene and branch-delete-list read it).
// A push to any other ref, a deletion and a push that leaves a branch's tip where it was are not
// judged. GitHub is asked through gh for the workflow runs of the commit filtered by event=push,
// so a pull request's run, which judges a merge of the commit rather than the commit, never
// counts. Any run of the commit that completed with success passes it; otherwise the newest run
// is named with its state (queued, in progress, failed, cancelled) and its URL.
package pushgate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/branchhygiene"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/emit"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/prepush"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// ExitCode is the gate's status: git refuses the push on any but ExitOK.
type ExitCode int

// 0 the push may go, 1 refused, 2 the gate could not judge (bad usage, an unreadable policy, a
// remote that names no GitHub repository, gh failing or unauthenticated).
const (
	ExitOK        ExitCode = 0
	ExitRefused   ExitCode = 1
	ExitCannotRun ExitCode = 2
)

// RuleID is the identifier every refusal carries; CLAUDE/QA.md documents it.
const RuleID = "push-without-qa-verdict"

// ExemptEnv names the variable that lets an owner's tool push without a verdict, with the reason
// as its value; the reason is printed. Agents never set it.
const ExemptEnv = "QA_PUSH_GATE_EXEMPT"

// QAWorkflow is the workflow whose push run is CI's QA verdict on a commit.
const QAWorkflow = ".github/workflows/qa.yml"

// ghTimeout bounds one question to GitHub.
const ghTimeout = 60 * time.Second

const short = 12

const usageText = `usage: qa-push-gate REMOTE [URL]   (git's pre-push arguments; the ref lines on stdin)

The pre-push gate on CI's QA verdict (rule ` + RuleID + `). A protected branch (one exempt in
` + branchhygiene.PolicyFile + `) moves only to a commit that GitHub CI's QA workflow (` + QAWorkflow + `)
completed with success on a push. Push the commit to a branch first, wait for CI, then push that
same commit to the protected branch:

  git push origin HEAD:refs/heads/ci/<topic>
  gh run watch                 (or: gh run list --commit <sha>)
  git push origin <sha>:F44
  git push origin --delete ci/<topic>

A push to any other ref, a deletion, or a push that leaves a branch where it was is not judged.

` + ExemptEnv + `=<reason>  an owner's tool pushing without a verdict; the reason is printed.

Exit codes: 0 the push may go, 1 refused, 2 the gate could not judge.
`

// Env is what the gate touches: the hook's stdin, the streams, gh, the checkout's policy file
// and the environment.
type Env struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	Runner         proc.Runner
	Cwd            string
	Getenv         func(string) string
	ReadFile       func(path string) ([]byte, error)
}

// refusal is a push the gate refuses; anything else that goes wrong is a run that cannot judge.
type refusal struct{ msg string }

func (r *refusal) Error() string { return r.msg }

func refuse(format string, args ...any) error {
	return &refusal{msg: fmt.Sprintf(format, args...)}
}

type gate struct {
	env    Env
	remote string
	url    string
	pol    *branchhygiene.Policy
}

// Main judges one pre-push run: args are git's (REMOTE, then URL), the ref lines on stdin.
func Main(ctx context.Context, args []string, env Env) ExitCode {
	out, errOut := emit.New(env.Stdout), emit.New(env.Stderr)
	code := judge(ctx, args, env, out, errOut)
	return emit.Finish(out, errOut, code, ExitCannotRun)
}

func judge(ctx context.Context, args []string, env Env, out, errOut *emit.Emitter) ExitCode {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		out.Block(usageText)
		return ExitOK
	}
	if len(args) == 0 || args[0] == "" || len(args) > 2 {
		errOut.Line("ERROR: qa-push-gate: expected REMOTE [URL]")
		errOut.Block(usageText)
		return ExitCannotRun
	}
	if reason := env.Getenv(ExemptEnv); reason != "" {
		errOut.Line("⚠ qa-push-gate: exempt (" + ExemptEnv + "): " + reason)
		return ExitOK
	}
	refs, err := prepush.Parse(env.Stdin)
	if err != nil {
		errOut.Line("ERROR: qa-push-gate: " + err.Error())
		return ExitCannotRun
	}
	g := &gate{env: env, remote: args[0], url: args[len(args)-1]}
	judged := 0
	for _, ref := range refs {
		branch, passed, err := g.judgeRef(ctx, ref)
		if refused, ok := errors.AsType[*refusal](err); ok {
			g.refused(errOut, ref, branch, refused.msg)
			return ExitRefused
		}
		if err != nil {
			errOut.Line("ERROR: qa-push-gate: " + ref.RemoteRef + ": " + err.Error())
			return ExitCannotRun
		}
		if passed != "" {
			out.Line("✓ qa-push-gate: " + ref.RemoteRef + " at " + ref.LocalID[:short] + ": CI's QA run passed: " + passed)
			judged++
		}
	}
	if judged == 0 {
		out.Line("✓ qa-push-gate: no protected branch moves")
	}
	return ExitOK
}

func (g *gate) refused(errOut *emit.Emitter, ref prepush.Ref, branch, msg string) {
	errOut.Line("✗ qa-push-gate [" + RuleID + "]: " + ref.RemoteRef + " at " + ref.LocalID[:short] + ": " + msg)
	errOut.Line("  " + branch + " is protected: it moves only to a commit CI passed on a push. Push the commit")
	errOut.Line("  to a branch, wait for CI, then push that same commit to " + branch + ":")
	errOut.Line("    git push " + g.remote + " " + ref.LocalID + ":refs/heads/ci/<topic>")
	errOut.Line("    gh run watch    (or: gh run list --commit " + ref.LocalID + ")")
	errOut.Line("    git push " + g.remote + " " + ref.LocalID + ":" + branch)
	errOut.Line("  See CLAUDE/QA.md#" + RuleID)
}

// judgeRef answers the protected branch a ref moves and the URL of CI's passing run of its new
// tip; a ref that moves no protected branch answers "", "".
func (g *gate) judgeRef(ctx context.Context, ref prepush.Ref) (string, string, error) {
	if ref.Deletes() || ref.LocalID == ref.RemoteID {
		return "", "", nil
	}
	pol, err := g.policy()
	if err != nil {
		return "", "", err
	}
	branch := protected(pol, ref.RemoteRef)
	if branch == "" {
		return "", "", nil
	}
	repo, err := repoOf(g.url)
	if err != nil {
		return branch, "", err
	}
	passed, err := g.ciVerdict(ctx, repo, ref.LocalID)
	return branch, passed, err
}

// protected is the branch name when ref is a branch the policy exempts, else "".
func protected(pol branchhygiene.Policy, ref string) string {
	name, ok := strings.CutPrefix(ref, "refs/heads/")
	if !ok || !slices.Contains(pol.Exempt, name) {
		return ""
	}
	return name
}

func (g *gate) policy() (branchhygiene.Policy, error) {
	if g.pol == nil {
		pol, err := branchhygiene.ReadPolicy(g.env.ReadFile, g.env.Cwd)
		if err != nil {
			return branchhygiene.Policy{}, err
		}
		g.pol = &pol
	}
	return *g.pol, nil
}

var repoPart = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// repoOf is the OWNER/REPO a GitHub remote URL names: scp-like ([user@]host:owner/repo) or an
// ssh, https or git URL, with or without .git.
func repoOf(remote string) (string, error) {
	path, ok := remotePath(remote)
	parts := strings.Split(strings.TrimSuffix(path, ".git"), "/")
	if !ok || len(parts) != 2 || !validPart(parts[0]) || !validPart(parts[1]) {
		return "", fmt.Errorf("remote URL %q names no GitHub OWNER/REPO", remote)
	}
	return parts[0] + "/" + parts[1], nil
}

// remotePath is the repository path of a network remote URL, without its leading slash.
func remotePath(remote string) (string, bool) {
	if !strings.Contains(remote, "://") {
		host, rest, ok := strings.Cut(remote, ":")
		return rest, ok && host != "" && !strings.Contains(host, "/")
	}
	u, err := url.Parse(remote)
	if err != nil || u.Host == "" || !slices.Contains([]string{"ssh", "https", "http", "git"}, u.Scheme) {
		return "", false
	}
	return strings.TrimPrefix(u.Path, "/"), true
}

func validPart(s string) bool {
	return repoPart.MatchString(s) && s != "." && s != ".."
}

// runsFilter is gh's --jq projection of GitHub's workflow-runs listing into the fields the gate
// reads, renamed from GitHub's snake_case to this module's JSON case.
const runsFilter = `{totalCount: .total_count, runs: [.workflow_runs[] | {path, event, headSha: .head_sha, status, conclusion, createdAt: .created_at, htmlUrl: .html_url}]}`

type workflowRun struct {
	Path       string `json:"path"`
	Event      string `json:"event"`
	HeadSHA    string `json:"headSha"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	CreatedAt  string `json:"createdAt"`
	HTMLURL    string `json:"htmlUrl"`
}

type runList struct {
	TotalCount *int          `json:"totalCount"`
	Runs       []workflowRun `json:"runs"`
}

// ciVerdict is the URL of a passing push run of the QA workflow on sha, a refusal naming why
// there is none, or an error when GitHub could not be asked.
func (g *gate) ciVerdict(ctx context.Context, repo, sha string) (string, error) {
	runs, err := g.qaRuns(ctx, repo, sha)
	if err != nil {
		return "", err
	}
	if len(runs) == 0 {
		return "", refuse("no QA run on GitHub for %s: CI has not run %s on a push of this commit", sha[:short], QAWorkflow)
	}
	if i := slices.IndexFunc(runs, func(r workflowRun) bool { return r.Status == "completed" && r.Conclusion == "success" }); i >= 0 {
		return runs[i].HTMLURL, nil
	}
	newest := runs[0]
	if newest.Status != "completed" {
		return "", refuse("CI's QA run of %s is %s, not finished: %s", sha[:short], newest.Status, newest.HTMLURL)
	}
	return "", refuse("CI's QA run of %s concluded %s: %s", sha[:short], newest.Conclusion, newest.HTMLURL)
}

// qaRuns is the push runs of the QA workflow on sha, newest first.
func (g *gate) qaRuns(ctx context.Context, repo, sha string) ([]workflowRun, error) {
	ctx, cancel := context.WithTimeout(ctx, ghTimeout)
	defer cancel()
	endpoint := "repos/" + repo + "/actions/runs?head_sha=" + sha + "&event=push&per_page=100"
	res, err := g.env.Runner.Run(ctx, proc.Cmd{Argv: []string{"gh", "api", endpoint, "--jq", runsFilter}})
	switch {
	case err != nil:
		return nil, fmt.Errorf("gh api %s: %w", endpoint, err)
	case res.TimedOut:
		return nil, fmt.Errorf("gh api %s: no answer within %s", endpoint, ghTimeout)
	case res.Code != 0 || res.Leftover:
		return nil, fmt.Errorf("gh api %s: exit %d: %s", endpoint, res.Code, strings.TrimSpace(res.Stderr))
	}
	var list runList
	if err := json.Unmarshal([]byte(res.Stdout), &list); err != nil {
		return nil, fmt.Errorf("gh api %s: not a list of workflow runs: %w", endpoint, err)
	}
	if list.TotalCount == nil || *list.TotalCount != len(list.Runs) {
		return nil, fmt.Errorf("gh api %s: the listing is not whole (%d runs listed of the total_count given)", endpoint, len(list.Runs))
	}
	type dated struct {
		run workflowRun
		at  time.Time
	}
	var found []dated
	for _, r := range list.Runs {
		if r.Event != "push" || r.HeadSHA != sha || (r.Path != QAWorkflow && !strings.HasPrefix(r.Path, QAWorkflow+"@")) {
			continue
		}
		at, err := time.Parse(time.RFC3339, r.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("gh api %s: a run's created_at: %w", endpoint, err)
		}
		found = append(found, dated{run: r, at: at})
	}
	slices.SortStableFunc(found, func(a, b dated) int { return b.at.Compare(a.at) })
	runs := make([]workflowRun, 0, len(found))
	for _, d := range found {
		runs = append(runs, d.run)
	}
	return runs, nil
}
