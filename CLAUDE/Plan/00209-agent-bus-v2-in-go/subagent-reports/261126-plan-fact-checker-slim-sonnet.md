# Fact check: Plan 00209 PLAN.md slim (commit 141c08acf), against HEAD on v2-smoke

Method: old PLAN.md = `git show 141c08acf~1:...`, new = `git show HEAD:...`; every old paragraph split into sentences and searched (whitespace-normalised) in the new PLAN.md, DECISIONS.md, OWNER-DIRECTIONS.md, TASKS-in-full.md and the journal entry.

| #   | Claim                                                              | Verdict  | Evidence                                                                                                                                                                                                                                                                                                                                   |
| --- | ------------------------------------------------------------------ | -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 1   | Every task line and status is still in PLAN.md                     | VERIFIED | 62 checkbox lines before and after; the only differences are in 7 lines (2.4, 2.5, 3.1, 3.2, 3.3, 4.2, 4.4, 6.5 titles shortened, and 3.5 ticked). No status other than 3.5 changed.                                                                                                                                                       |
| 2   | Moved text is "unchanged" (TASKS-in-full.md, DECISIONS.md headers) | VERIFIED | Only 6 sentences from the old PLAN.md are not found verbatim anywhere; all are connective rewordings in PLAN.md itself (see below). The full old task-line text of 2.4, 2.5, 3.2, 3.3, 4.2, 4.4, 6.5 sits at the head of its `## Task N.N` section in TASKS-in-full.md.                                                                    |
| 3   | Owner directions moved verbatim to OWNER-DIRECTIONS.md             | VERIFIED | File exists, no sentence missing.                                                                                                                                                                                                                                                                                                          |
| 4   | Decisions table and carried requirements in DECISIONS.md           | VERIFIED | 20 table rows in old PLAN.md, 20 in DECISIONS.md; no sentence missing.                                                                                                                                                                                                                                                                     |
| 5   | Task 4.4 cross-network observation is now a journal entry          | VERIFIED | JOURNAL/2026-11-26/184521Z-finding-a5478e.md carries it. TASKS-in-full.md 4.4 points to it as "the journal, 2026-11-26" (vague but resolves).                                                                                                                                                                                              |
| 6   | Every link in PLAN.md resolves                                     | VERIFIED | All relative file links exist (DESIGN-v2, DONE-n0-n1, BUILT-n2/n3, LAUNCH-ccy, SPEC-\*, ../00246.../PLAN.md, the three subagent-reports); `DESIGN-v2.md#7-the-owner-decisions` matches `## 7. The owner decisions` (line 279). `TASKS-in-full.md#task-22,23,24,25,31,32,33,35,42,43,44,45,47,64,65,75` each match a `## Task N.N` heading. |
| 7   | Task 3.5 superseded by Plan 00267 Task 7.1 (coalesce window)       | VERIFIED | 00267 PLAN.md line 142: `[x] Task 7.1 ... This supersedes Plan 00209 Task 3.5`; `src-go/internal/teamchat/member/engine/coalesce.go` is tracked at HEAD and implements the window (coalesceQuiet 2 s, coalesceCeiling 10 s).                                                                                                               |
| 8   | Plan 00267 Task 7.5 is the human message debounce                  | VERIFIED | 00267 PLAN.md line 159; coalesce.go humanSettle 3 s / humanCeiling 10 s, comment cites Tasks 7.1 and 7.5.                                                                                                                                                                                                                                  |
| 9   | Task 3.5 original text kept at TASKS-in-full.md#task-35            | VERIFIED | Present at `## Task 3.5` (line 205).                                                                                                                                                                                                                                                                                                       |

## REFUTED claims

None.

## Lost content

None. The 6 reworded sentences (all in PLAN.md, not task content):

- "decisions are defaulted and may be overturned (Decisions, below)" -> link form.
- 0.5's "...and Decisions, above" -> DECISIONS.md link.
- 2.1's "the YAML module with n2b, `godbus` with n2f):" -> parenthesis reshaped.
- "n2f-D12 (Decisions, above)" -> plain.
- 3.1's "(Requirements carried)" -> dropped; the text is now in DECISIONS.md.
- 4.4's dated 2026-11-17 observation -> journal (intended).

## Observations (not refutations)

- The ticked Task 3.5 line says "superseded", which is true of the mechanism, but the built behaviour differs from the original spec still shown in TASKS-in-full.md#task-35: a 30 s default quiet window became 2 s quiet / 10 s ceiling, and a human's direct message does not bypass the hold but gets its own 3 s settle (halt ping does bypass). The Task 3.5 line does not say this; one clause would prevent a reader taking the 30 s figure as built.
- Task 4.5 refers to `DESIGN-dual-bus.md`, which does not exist in the tree. The same reference was in the old PLAN.md (a "design first" deliverable), so not a regression.
- Several tasks (0.x, 1.x, 2.1, 3.4, 4.1, 4.6, 5.1) have no TASKS-in-full link; their full text remains in PLAN.md, so nothing is lost.

Summary: 9 claims, 9 verified, 0 refuted, 0 unverifiable.
