# Fact check: DESIGN-defence.md (Plan 00245)

Scope: the diff adds all of DESIGN-defence.md. Note the diff names DESIGN-defence.md, not PLAN.md.

## REFUTED / imprecise
1. Section 1: "`untracked/scratch/` holds over 3,500 files, about 1,100 over 4 KiB, 440 markdown". Measured: 3,354 files directly in scratch (not over 3,500), 1,093 over 4 KiB, 440 md at top level; recursively 47,130 files, 19,236 over 4 KiB, 6,157 md. Figures are top-level only and 3,354 is under 3,500. State depth and use current numbers.

## Table
| # | Claim | Verdict | Evidence |
|---|---|---|---|
| 1 | project_containment denies /tmp, steers to untracked/scratch | VERIFIED | project_containment.py header; R-WRITE-OUTSIDE-PROJECT-ROOT fix text |
| 2 | markdown_organization allows untracked/ "ad-hoc temporary docs" | VERIFIED | markdown_organization.py:58 |
| 3 | dispatch_declaration non-plan fallback is untracked/agent-reports/; AgentNotes repeats it | VERIFIED | AgentNotes.md:232; dir exists |
| 4 | AgentNotes says keep WIP under untracked/ (patch or scratch copy) | VERIFIED | AgentNotes.md:165 |
| 5 | docs-optimise writes proposal.patch in scratch | VERIFIED | skills/docs-optimise/references/procedure.md:52-53 |
| 6 | scratch counts (3,500 / 1,100 / 440) | REFUTED (see above) | find counts |
| 7 | 96 registered worktrees | VERIFIED | git worktree list: 96 |
| 8 | subagent_report_persistence prunes at 500 files / 30 days | VERIFIED | subagent_report_persistence.py:80-81 |
| 9 | untracked/agent-reports/auto, issue-reports, repos, clones, plan-runs, worktrees, wip-rescue exist | VERIFIED | ls |
| 10 | git stash denied; git branch -D guard; untracked memory ban | VERIFIED | destructive_git.py, handler table R-GIT-STASH-PUSH, R-GIT-BRANCH-FORCE-DELETE, R-MARKDOWN-UNTRACKED-MEMORY |
| 11 | project handlers dir .claude/hooks/handlers/pre_tool_use/ exists | VERIFIED | ls |
| 12 | qa-all.bash, qa-changed.bash, CLAUDE/QA.md, src-go/qa exist | VERIFIED | ls |
| 13 | CI clean checkout has no untracked/ | VERIFIED (inferred) | untracked/ is gitignored (untracked/.gitignore `*`); no workflow mentions untracked |
| 14 | subagent_report_path_verifier exists | VERIFIED | handlers/subagent_stop/ |
| 15 | daemon has a written-path accessor | VERIFIED | get_written_file_paths in core/utils.py |
| 16 | dispatch_declaration.options.strict not yet set here ("now") | UNVERIFIABLE-HERE | no dispatch_declaration key in .claude/hooks-daemon.yaml; rule table shows the strict-mode deny already listed |
| 17 | RESEARCH-untracked-triage.md "had not been written" | VERIFIED | not in plan folder |
| 18 | "11 finished commits on branches no remote held", 10 days of work, /tmp incident etc. | UNVERIFIABLE-HERE | incident history, not in tree |
