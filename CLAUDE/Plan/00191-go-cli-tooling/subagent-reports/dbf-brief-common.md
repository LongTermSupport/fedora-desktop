# DBF sweep brief (Plan 00191 Task 4.9), shared by four workers

Your worktree is your session's current directory: the harness made it for you, from the
branch plan/00191-usage-bars at fba561cbe. Work only there. Run `git rev-parse --show-toplevel`
first and use that path; call it WT below.

The rule `project_command_failure_dropped` (WT/src-go/qa/rules/commandfailure.go: read its
Doc first) flags a command run through the seam (internal/execx, qa/internal/proc) whose
error is only tested (err != nil, errors.Is) and never carried. The origin: ccy's usage read
said "could not reach the API" for any curl failure. The fix at each site puts the error
itself, or what errors.As finds in it (execx.ExitError: exit code, stderr), into the message
or record the caller sees, or returns it (fmt.Errorf("...: %w", err)). Read each site and
decide what the user should see; keep messages terse and in the package's existing voice.
Never suppress, never add //nolint, never narrow the rule, and don't edit src-go/qa/rules.
If a site truly cannot carry its cause (for example a probe whose failure IS the answer, where
a spec or comment says so), or you are unsure, leave it unfixed and list it with the reason.
Don't guess. Where a SPEC-*.md in a plan folder quotes a message you change, update the quote
in the same commit.

The site list is <repo>/untracked/scratch/dbf-findings-sites.txt (paths relative to
src-go/; qa/... sites are in the nested module src-go/qa).

Checks, targeted only. NEVER run qa-all.bash, qa-changed.bash or qa-go-qa.bash: full QA is
the coordinator's.
1. Build the rules driver once: cd WT/src-go/qa && go build -o <repo>/untracked/scratch/dbf-driver-<N> ./driver
2. go vet -vettool=<repo>/untracked/scratch/dbf-driver-<N> on each of your packages (from
   WT/src-go for internal/..., from WT/src-go/qa for qa/...). It must report no
   project_command_failure_dropped, and no new findings of any other kind.
3. nice -n 19 go test on the same packages. Update tests whose expected messages change.
   Where a message now carries a cause, add or adjust a test asserting the cause appears.
   Follow WT/src-go/CLAUDE.md (no discarded errors, through the fakes).
4. gofmt -l on your changed files prints nothing.

Commit in WT with: git -C WT commit -F <message file under <repo>/untracked/scratch/>.
The body says what changed and anything left; its last line is
"Co-Authored-By: Claude Opus 5.5 <user@example.com>". Don't push, merge, or touch any
other worktree. No sed, no pipes to head or tail. Write the report to
WT/CLAUDE/Plan/00191-go-cli-tooling/subagent-reports/261030-dbf-cfd-<N>.md and commit it with
the work. Reply with WT, the commit id and a summary of 5 lines or fewer: sites fixed, sites
left with reasons, test results.
