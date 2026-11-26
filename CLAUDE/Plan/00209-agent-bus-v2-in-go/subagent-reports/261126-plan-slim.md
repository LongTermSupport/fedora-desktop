# Plan 00209 PLAN.md slim, and Task 3.5 superseded

Commit 141c08acf on `v2-smoke`, pushed.

- PLAN.md went from 48,444 bytes to 19,326 bytes. That is under the 35,000-byte hard limit, and
  1.3 KB over the 18,000-byte advisory threshold, which plan QA reports as advise only.
- Nothing was deleted. The text that moved out is unchanged in these files:
  - `OWNER-DIRECTIONS.md`: directions 1-7 and the clarification. PLAN.md keeps a one-line
    summary of each.
  - `DECISIONS.md`: the decisions table, the requirements carried from other plans, and the
    M0 fold-ins.
  - `TASKS-in-full.md`: the long bodies of Tasks 2.2-2.5, 3.1-3.3, 3.5, 4.2-4.5, 4.7, 6.4,
    6.5 and 7.5. Each task line in PLAN.md links to its section.
- The dated 2026-11-17 cross-network observation under Task 4.4 is now a journal entry:
  `JOURNAL/2026-11-26/184521Z-finding-a5478e.md`, written with `mkplan.bash --journal`.
- Task 3.5 is ticked as superseded by Plan 00267 Task 7.1 (the coalesce window,
  `src-go/internal/teamchat/member/engine/coalesce.go`), with Task 7.5 as the human message
  debounce. The commit says "Addresses #129" and does not close the issue.
- `plan-qa --check-staged` found 0 blocks. Its three advisories concern other files (two
  `.inc.bash` files at the plan root and long README index rows) and were left alone.
- The conflict-marker gate could not check the commit because of `GIT_INDEX_FILE`. Searching the
  commit's added lines for conflict markers by hand found none.
