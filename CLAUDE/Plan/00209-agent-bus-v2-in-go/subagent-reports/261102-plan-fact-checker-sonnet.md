# Plan fact check: Plan 00209, Task 8.6 marked done

Document: `CLAUDE/Plan/00209-agent-bus-v2-in-go/PLAN.md`
Diff checked: `.claude/hooks-daemon/untracked/plan-fact-check/00209-agent-bus-v2-in-go.diff`
(one hunk: Task 8.6 changes from `[ ] ⬜` to `[x] ✅`; its text is unchanged).
Tree judged: HEAD `438b59d63` ("Plan 00209 Task 8.6: put back the v2 lines the merge lost"),
on top of merge `155bfed56`. The staged changes in the session's git status were committed
as 438b59d63 while this check ran; the working tree is clean.

The claim the diff adds is that Task 8.6 is done: the v2 lines the merge dropped from five
files are back, "merged by meaning". Method: take every line `F44...origin/feat/bus-v2-n3e`
adds to each file and test it against the file at HEAD, exactly first, then with whitespace
collapsed (table columns were re-aligned). The pre-fix tree, `438b59d63~1`, was read to
confirm the loss was real.

## REFUTED (most consequential first)

### R1. Claim 1c: GoProjectRules.md has the lost v2 lines back (partly refuted)

Everything n3e added is at HEAD except one block: the teamchat-scoped text of
`project_realtime_bound`. After whitespace normalisation 67 non-blank lines are absent: the
table row ("a teamchat bound on real time ..."), the What / Why / flags / narrowings / fix
paragraphs, the `fix/internal/teamchat/realtime` fixture description, the "Red, real tree ...
five findings" list with `file:line` entries, "Red at the merge into n3e" (`leakcheck`), the
Green paragraph, the "Not caught" bullet (matrix `TestRequestTimeLimit`) and "The next wider
rule, not built" with its pointer to `subagent-reports/261023-n3-probebound.md`.

The section at HEAD is F44's rule of the same name
(`CLAUDE/GoProjectRules.md` lines 654-751). It covers every package of both modules, uses
the `processBounds` table, and its fixtures are `fix/internal/realtime` and
`fix/internal/realclock`. It is a superset by scope, and it cites the same originating
instance (`TestProbesThatHangEndAtTheirOwnBound`, Plan 00209). So dropping the v2 text is
defensible as supersession, not necessarily a loss. But nothing in the plan or the commit
message records that decision. The commit message lists what was restored and does not
mention `project_realtime_bound`.

Consequences for the plan:

- The Task 8.6 `[x]` holds for the other four files and for the rest of GoProjectRules.md,
  but not for "every lost v2 line is back". Either say in the task that the v2
  `project_realtime_bound` text was deliberately left to F44's wider rule, or carry the
  teamchat-specific findings (the `leakcheck` and `TestRequestTimeLimit` misses) into the
  F44 section.
- `src-go/qa/rules/testdata/src/fix/internal/teamchat/realtime/` (4 files) and
  `.../teamchat/realclock/real.go` are tracked at HEAD. No Go file under `src-go/qa`
  references them: `realtimebound_test.go:19` lists only `fix/internal/realtime` and
  `fix/internal/realclock`. They are fixtures for the n3e version of the rule that F44's
  replaced. Not a doc claim, but they are the other half of the same leftover.

### R2. Claim 3: "`git diff F44...feat/bus-v2-n3e` on those files shows them" does not run as written

`git diff --stat F44...feat/bus-v2-n3e` exits 128: `fatal: bad revision`. There is no local
branch `feat/bus-v2-n3e`; only `origin/feat/bus-v2-n3e` exists
(`git rev-parse --verify feat/bus-v2-n3e` fails). With the `origin/` prefix the command works
and shows the five files. The task text is unchanged by the diff, so this is a pre-existing
wording defect. Fix: write `origin/feat/bus-v2-n3e`.

## Claims table

| #   | Claim                                                                             | Verdict                        | Evidence                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| --- | --------------------------------------------------------------------------------- | ------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | Task 8.6 is done: v2 lines lost in the merge are put back in all five named files | VERIFIED with one gap (see R1) | 1a to 1e below                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| 1a  | CLAUDE/QA.md: v2 lines restored                                                   | VERIFIED                       | The `qa-go-qa.bash` row now carries the `busfailpoints` `[[stage]]` text and the `project-rules-tagged` sentence, and F44's version of the row is intact (the F44 row is the HEAD row minus the v2 sentences). The `qa-teamchat-separate.bash` row is present. `438b59d63~1` had 0 occurrences of `busfailpoints` in QA.md. The link `src-go/CLAUDE.md#build-tags` resolves: `## Build tags` exists.                                                                                                                             |
| 1b  | src-go/CLAUDE.md: v2 lines restored                                               | VERIFIED                       | `## Build tags` section present (`busfailpoints`, `teamchatrelease`, `tuwunel`, `ccyteamstest`/`difftest`). The `qa/teamchatseparate` paragraph (`teamchat-keeps-off-v1`) is present. The "Project rules" bullet (lines 180-188) is merged by meaning: it holds F44's "no real-time bound a test can shorten ... no Duration built from a number nothing bounds" and v2's "teamchat's bounds on its injected clock, teamchat's launch value branched on only in its agent seam". Every other n3e-added line is present verbatim. |
| 1c  | CLAUDE/GoProjectRules.md: v2 lines restored                                       | PARTLY REFUTED (R1)            | All 22 table-row names and every `###` heading of both n3e and F44 are at HEAD, including `lifecycle_turn`, `keeper_log_barrier` and `agent_branch` (0 occurrences at `438b59d63~1`, 1 now, 1 in n3e). Every n3e-added line is present after whitespace normalisation except the 67 `project_realtime_bound` lines in R1.                                                                                                                                                                                                        |
| 1d  | Plan 00194 PLAN.md: v2 lines restored                                             | VERIFIED                       | Tasks 2.0e (line 135, "Specified in Plan 00209 Task 3.1 (SPEC-n3 RS1-RS3)") and 2.0g (line 165, "Folded into Plan 00209's N3 ... SPEC-n3 UH1") carry the n3e wording. All 5 n3e-added lines are present exactly.                                                                                                                                                                                                                                                                                                                 |
| 1e  | Plan 00209 PLAN.md: v2 lines restored                                             | VERIFIED                       | All 250 lines n3e added are present after whitespace normalisation (0 non-blank misses). Rows INT3 (line 108) and n2f-D12 (line 112) are present.                                                                                                                                                                                                                                                                                                                                                                                |
| 2   | In the merge, the five files "took F44's side of each conflicting hunk"           | VERIFIED                       | `438b59d63~1` lacks the lifecycle_turn, keeper_log_barrier and agent_branch sections, lacks `busfailpoints` in QA.md, and has no `MUST_EXCEED_PLAN_SIZE_BECAUSE` in the 00209 plan. `155bfed56` (merge of origin/F44) touches QA.md, GoProjectRules.md and the 00209 and 00194 plans (merge stat).                                                                                                                                                                                                                               |
| 3   | `git diff F44...feat/bus-v2-n3e` on those files shows the lost lines              | REFUTED as written (R2)        | Exit 128, bad revision. `origin/feat/bus-v2-n3e` works. Its tip `406d97c0a` is the merge-base with HEAD, so n3e is wholly merged into the branch.                                                                                                                                                                                                                                                                                                                                                                                |
| 4   | The five files named are the whole set the merge dropped v2 lines from            | UNVERIFIABLE-HERE              | The merge stat shows other files changed by F44, but which of them had conflicting hunks resolved to F44's side is recorded only in the merge's own resolution. A check would be `git diff -c 155bfed56` or re-running the merge in a scratch clone. A line-presence sweep of other n3e-touched docs was not run.                                                                                                                                                                                                                |
| 5   | Plan 00209 PLAN.md has no other place that still counts Task 8.6 as open          | VERIFIED                       | `grep -n "8\.6"` on the plan matches only line 575, the task itself.                                                                                                                                                                                                                                                                                                                                                                                                                                                             |

## Method notes

- Exact-line matching reports blank added lines as missing (a grep quirk); they were
  discarded. Table rows re-aligned by F44's wider columns were matched after collapsing runs
  of spaces and hyphens.
- Judged against the current tree, not history. Commit 438b59d63's message was read only
  to learn what the author says was restored, then each item was checked in the files.
- Only claims the diff adds were checked. Task 8.5 (12 gitleaks findings) and Tasks 8.7 and
  8.8 appear as hunk context and were not checked.
- A cleared `untracked/scratch/fc-*` set was used for captures; no tracked file was changed
  apart from this report.
