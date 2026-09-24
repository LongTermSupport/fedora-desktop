# Plan 00130 — qa-reviewer round 2 (opus-5, 2026-09-23)

Saved by the coordinator: the reviewer is read-only and could not write this file.

**Verdict: FIX-BEFORE-MERGE**, nothing blocking. No leak, and no fail-fast breach.

## The eight round-1 findings

1. Fixed. `test-podfreeze.bash` covers every case group of the deleted script, including both `identity_axis_discriminates` mutants.
2. Moot: the file is deleted.
3. Fixed. `_planlib.inc.bash`, 032 and 00109 no longer claim a change gate.
4. Fixed.
5. Fixed (00079 PLAN.md).
6. Fixed. All five owed scripts carry `plan_require_host`.
7. Moot: the file is deleted.
8. **Not fixed.** PLAN.md replaced the old counts with new ones ("45 now", "1,002"), which date just the same.

## New findings, should fix

- PLAN.md and README.md said "seven still-active plans". Task 1.1 and the triage log list six: 00062, 00066, 00075, 00079, 00080 and 00098.
- `JOURNAL/00130-Journal-26-09-23.md` has a "plan scaffolded" entry dated today. The plan was created on 26-09-16 at 18:05. The file is committed, so the fix is a correction entry.
- The journal cites a "case by case" re-review that left no record in `subagent-reports/` and is not mentioned in PLAN.md.

## Nits

- `00079/acceptance.bash`: on a pass, the suite's `passed: N` count is thrown away.
- PLAN.md: the check-0 caveat is about a deleted script; move it to the journal.
- `00079/PLAN.md:121` contradicts `:122-124`.
- The `-y` help text in `032/deploy.bash` and `00109/deploy.bash` tells the history of a removal instead of describing the flag.

## Gates

- `qa-all.bash` is green.
- `plan-qa --sweep` returns 7 advisories, the same as last time, none in this plan's files.
- The diff touches only `CLAUDE/Plan/`.
