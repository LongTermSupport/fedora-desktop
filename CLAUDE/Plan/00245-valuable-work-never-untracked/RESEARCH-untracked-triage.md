# Triage of `untracked/` (initial pass, 2026-11-03)

Read-only. Scope: everything under `untracked/` except `repos/`, `plan-runs/`, `facts/`.
Nothing was moved, copied, deleted or committed. Install-specific names are replaced by
placeholders. Files whose name carries the secret patterns were not read; none were seen
outside `repos/`.

Method and limits. Counts for the large trees are by rule (name pattern, directory, size),
not file by file. "Published" was tested by (a) blob or file-name match against every ref
(`git log --all`), and (b) the three longest lines of each text file searched with
`git grep -F` on F44. Test (b) under-reports (journal bodies are reformatted when filed), so
the VALUABLE count is an upper bound for the small journal-like files and a fair count for
reviews, specs and reports. Sub-agent report bodies in `agent-reports/` were sampled by
header, not read in full.

## Summary by class

| Class              | Files (approx) |         Bytes (approx) | What                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| ------------------ | -------------: | ---------------------: | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| SCRATCH            |        272,000 |                10.5 GB | QA/run logs (about 80 `scratch-qaall*` logs at top level, 1,100 `.log` and 1,200 `.txt` in `scratch/`), build and verify trees with binaries (`m12v`, `m12verify`, `goqaci-tools`, `gobin`, `realbin` ...), 346 journal/issue/comment/message drafts in `scratch/`, 310 compaction summaries in `agent-reports/auto/`, third-party clones from 2025, `tools/` binaries, `lint/`, `vmtest-bridge/` (VM lab runtime queue), `supervise/decision.log`, daemon upgrade advisories (`truth-changes/`, `config-changes/`, 28 files), most of `worktrees/` |
| PUBLISHED          |          2,950 |                 0.6 GB | clones whose branch is level with its remote (`projects/*`, the go-qa-ci work clone, the private config repo clone: dirty state not counted); 22 `issue-reports/` bodies (upstream issues exist for the titles I matched); 36 of 58 `wip-rescue/` files are byte-identical to a committed file on some branch (not on F44); 12 `agent-reports/` files; about 35 `scratch/*.md` (merged plan or doc working copies); `github-443-solution.md` (docs/github-ssh-over-443.md), `cc-handover.md` (Plan 00166)                                           |
| VALUABLE-UNTRACKED |            237 |                 3.0 MB | listed in full below                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| UNKNOWN            |      about 280 | 3 MB plus 12 worktrees | see "Unknown"                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |

## VALUABLE-UNTRACKED, in full

"IS" = install-specific (must be anonymised before it can be tracked). Dates are file mtimes.

### Rescued work (`untracked/wip-rescue/`, 2026-10-21 to 2026-11-03, created by a coordinator before removing worktrees)

| Path                                                                                                                 | What                                                                 | Belongs                                              | IS  |
| -------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------- | ---------------------------------------------------- | --- |
| `wip-rescue/bus-v2-m1-proto-envelope/` (render.go, render_test.go, envelope.json 354 KB, unicode.json) plus `.patch` | uncommitted Go and fixtures from a dirty M1 worktree                 | Plan 00209 branch or a WIP commit pushed to a branch | no  |
| `wip-rescue/bus-v2-m1-proto-limits/` (lines_test.go, lines.json) plus empty `.patch`                                 | same                                                                 | Plan 00209                                           | no  |
| `wip-rescue/bus-v2-m1-proto-ping/` (ping, receive, text, plus tests) plus `.patch`                                   | same                                                                 | Plan 00209                                           | no  |
| `wip-rescue/bus-v2-m1-proto-rooms.patch` (52 KB: qindex, questions, rooms)                                           | patch only, no copy elsewhere                                        | Plan 00209                                           | no  |
| `wip-rescue/bus-v2-n3e-seam.patch` (0 bytes)                                                                         | empty: the rescue itself produced nothing, check the source worktree | Plan 00209                                           | no  |

`src-go/internal/bus/proto/` and `tests/fixtures/agent-bus-protocol/` do not exist on F44 at all.
The 36 other `wip-rescue` files (Plan 00123, 00194, 00197, 00203, 00201, 00177 plan texts,
reviews, probes) are committed on some ref other than F44: PUBLISHED on a branch only. Seven
of them differ from the committed copy (README.md x2, meta-deploy.list, 00197 DESIGN/PLAN/
JOURNAL in one copy, 00194 PLAN/JOURNAL, 00203 JOURNAL, n3e seam report): the rescue copy may be newer
or older, unverified. Branch survival is covered by `branch-triage-261103.md`.

### Owner-facing sheets and decisions (top level, created by coordinators)

| Path                                                                      | What                                                              | Date       | Belongs                                                                                        | IS                                           |
| ------------------------------------------------------------------------- | ----------------------------------------------------------------- | ---------- | ---------------------------------------------------------------------------------------------- | -------------------------------------------- |
| `owner-decisions-2026-10-30.md` (14 KB)                                   | owner's answers A1..; says some are "recorded in Plan 00209"      | 2026-11-02 | the plan that each answer governs (00209, 00235, others), as a dated decision record           | yes (a database, project and checkout named) |
| `owner-unblock-2026-10-28.md` (6 KB)                                      | questions A, decisions C                                          | 2026-10-28 | same                                                                                           | partly                                       |
| `branch-triage-261103.md` (28 KB, 154 branches classified)                | the branch fate table                                             | 2026-11-03 | Plan 00245 or the branch-cleanup plan                                                          | no (branch names only)                       |
| `plan-00235/old-range-decisions.tsv` (11 KB)                              | owner's per-item remove/reown/leave answers, an input to a deploy | 2026-10-30 | the file itself says "keep under untracked/"; needs a tracked home or the deploy input is lost | yes (image ids, container data)              |
| `Plan/performance-audit/PLAN.md` (9.7 KB)                                 | private plan, "never commit"                                      | 2026-10-21 | a tracked plan with names anonymised, captures stay in `triage/perf-261021/`                   | yes                                          |
| `triage/oom-terminals/TRIAGE-FINDINGS.md`                                 | findings of a host triage                                         | 2026-09/10 | its plan's RESEARCH file                                                                       | yes                                          |
| `reports/2026-09-02-skill-opportunities.md` (16 KB)                       | skill-opportunity report derived from transcripts                 | 2026-09-02 | docs or a plan, after redaction                                                                | yes (transcript-derived)                     |
| `<client website analysis dir>/` (19 files, 63 KB: probe scripts, output) | analysis of a third party's site                                  | 2026-05-07 | stay private; do not track                                                                     | yes (client)                                 |

### `scratch/*.md`, substantive, no tracked copy found (about 52 files, 1.1 MB). Created by coordinators and by dispatches told to write there.

| Path (under `untracked/scratch/`)                                                                                                                     | What                                                 | Date           | Belongs                                                                                                  | IS                                |
| ----------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------- | -------------- | -------------------------------------------------------------------------------------------------------- | --------------------------------- |
| `SPEC-n3.merged.md` (268 KB), `SPEC-n3.seam.md` (281 KB), `specn3-tip.md` (100 KB)                                                                    | merge-resolution copies of the N3 spec               | 2026-10-23     | Plan 00209 SPEC (check the tracked SPEC is the resolved one; these are likely superseded working copies) | no                                |
| `_spec_SPEC-pingbus-hooks.md` (71 KB)                                                                                                                 | spec text                                            | 2026-10-21     | Plan 00209 / 00194                                                                                       | no                                |
| `00161-units.md` (32 KB)                                                                                                                              | unit table of Plan 00161                             | 2026-10-06     | Plan 00161                                                                                               | no                                |
| `blocked-review-g1..g5.md` (55 KB), `qa-review-3821.md`                                                                                               | review of blocked plans                              | 2026-10-06     | the plan README triage notes or a RESEARCH file                                                          | no                                |
| `261015-*-review-fable.md` x9 (201-fixes, bus-bugs, inject-build, inject-spec, md-viewer-play, sec-client, sec-infra, ux-fixes, wsi-watermark; 95 KB) | adversarial reviews                                  | 2026-10-15     | each plan's `subagent-reports/`                                                                          | no                                |
| `fable-review-abi.md`, `fable-review-qac.md` (40 KB)                                                                                                  | adversarial reviews of Go ports                      | 2026-10-14     | Plan 00192 `subagent-reports/`                                                                           | no                                |
| `261022-n3c-review-A.md`, `261021-n1-qa-reviewer.md`, `261022-n2-qa-reviewer-raw.md`, `n2g-spec-check-findings.md`, `n3e-seam-6-1.md` (34 KB)         | slice reviews                                        | 2026-10-21..23 | Plan 00209 `subagent-reports/`                                                                           | no                                |
| `audit-a1..a3.md`, `00177-factcheck.md`, `00213-fact-check.md`                                                                                        | audits and fact checks                               | 2026-10-21..26 | Plans 00177, 00213, the unblock plan                                                                     | no                                |
| `00183-scoping` report (31 KB)                                                                                                                        | scoping of a sibling repo's side of cross-site seats | 2026-11-02     | Plan 00183                                                                                               | yes (sibling-repo and host names) |
| `status-survey.md` (20 KB), `deploy-batch.md` (14 KB), `hooks-daemon-319-investigation.md`, `watchleak-search.md`, `conf-committed.md`                | surveys and investigations                           | 2026-10-06..23 | Plan README, their plans                                                                                 | mixed                             |
| `fable-decisions-2026-10-31.md`, `fable-winddown*.md`, `dbf-brief-common.md`, `261028-issue-3-12-notes.md`, `261028-feature-request-3-12.md`          | decision review, briefs, drafts                      | 2026-10-19..31 | Plans 00192, 00203, the DBF doc                                                                          | no                                |
| `00161-u23-timeouts-journal*.md`, `261022-n2*-journal.md`, `vmtest-journal.md`                                                                        | journal bodies never appended (hit count 0)          | 2026-10-09..22 | their plans' JOURNAL via `mkplan.bash --journal`                                                         | no                                |

### `agent-reports/` (daemon `subagent_report_persistence`, mode 0600)

| Group                                                                                                                                                                                                                                                                                                                           |      Files / bytes | Dates             | Belongs                                                                                                              | IS                                                 |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -----------------: | ----------------- | -------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------- |
| top-level, no tracked copy (QA-reviewer rounds, plan surveys and reviews for Plans 00063..00213, wallpaper/GNOME research, plan-triage sweeps, `261023-unblock-audit-A/B`, `261023-cross-machine-teams`, `261023-ccy-shutdown-hang`, `261022-asr-leaderboard`, hooks-daemon outage and upgrade analyses, `dbf-conformance-...`) | 65 / about 0.85 MB | 2026-09-03..10-23 | each plan's `subagent-reports/` (the plan-workflow convention); QA rounds on closed plans have expired value         | mostly no; the cross-machine and shutdown ones yes |
| `auto/` general-purpose, qa-reviewer, DBF, Explore, other replies over 2.5 KB                                                                                                                                                                                                                                                   | 72 / about 0.38 MB | 2026-10-28..11-03 | same; the qa-reviewer ones say "my rules forbid creating files", so the copy here is the only record of the findings | mixed                                              |

## Unknown

| Item                                                                                                                           | Why                                                                                                          |
| ------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------ |
| 12 dirty `worktrees/` entries (33 changed lines in total, of 30 present)                                                       | uncommitted work may live there; not read                                                                    |
| `scratch/{stt,tuwunel,203rev,00143,p4doc,dbf,<sibling-repo dir>}` (about 130 text files)                                       | research notes; Plans 00148/00179/00161/00209 hold similar tracked research, but equality not shown          |
| `hooks-daemon-umask.md`, `hooks-daemon-hooks-chmod.md`, `upstream-bug-report-plugin-handler-suffix.md`, `ccy-sub-agent-cap.md` | written as upstream reports; no matching upstream issue found by search; mostly stale (daemon 3.51 and 2.13) |
| `speech-recorder/` (a CLAUDE.md and deploy.bash)                                                                               | looks like a deploy aid for a recordings folder; owner intent unknown                                        |
| `mutation_check.py`, `go-qa-ci-work/*.py`, `scratch-m5/`                                                                       | one-off tooling, reuse unknown                                                                               |

## Creation paths, ranked by valuable material produced

1. Daemon `subagent_report_persistence` (agent-reports): 137 valuable files, 1.2 MB, 2026-09 onward. Reviewers and researchers are told not to write files, so this copy is the only one until a coordinator files it. Nothing prompts the coordinator to.
2. Convention "scratch is where bodies and reviews go" (`scratch/`, written by coordinators and by dispatches given an output path there): about 52 files, 1.1 MB, dominated by specs and adversarial reviews that belong in `subagent-reports/`.
3. Coordinator rescue before worktree removal (`wip-rescue/`): 22 files with no commit anywhere (about 0.5 MB of uncommitted Go and fixtures) plus 36 files on non-F44 branches only.
4. Coordinator owner sheets at top level (`owner-*.md`, `branch-triage-*.md`, `plan-00235/`): 5 files, 60 KB, each the sole record of an owner decision or classification.
5. Private host triage by agents (`Plan/performance-audit`, `triage/`, `reports/`, client analysis): 22 files; install-specific, so tracking needs anonymisation first.
6. `hooks-daemon issue-report` (`issue-reports/`): no loss, bodies are filed upstream; scratch by design.

Defence implications. Paths 1 and 2 are automatic and need a hook or a filing step, not
agent discipline. Path 3 needs a rule that a worktree with uncommitted or unpushed work cannot
be removed until its content is on a pushed branch. Paths 4 and 5 need a tracked home
(plan folder or `docs/`) and an anonymisation step.
