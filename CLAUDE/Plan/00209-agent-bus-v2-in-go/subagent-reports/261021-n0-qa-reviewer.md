# Plan 00209 N0: qa-reviewer verdict over feat/bus-v2-n0 at 63c00f98

The qa-reviewer agent has no Write tool, so the build session recorded its final reply here, word for word in substance.

**Verdict: FIX-BEFORE-MERGE.**

1. PLAN.md line 74 lists the decisions as OD-A1-A9, BD1-15 and DC1-17. DESIGN-v2 section 7 also has OD-A10-11, BD16-18 and DC18-21. Six flagged decisions (OD-A10, BD9, BD16, DC18, DC19, DC21) are missing from the flagged list that Task 0.5 points at.
2. PLAN.md line 116, Task 0.4, says "host fix pass still running". The host fix pass and round 2 are both merged. Verify-ab and verify-cd are FIX-FIRST and the round-2 text is unreviewed, so N0 is not at MERGE.
3. README.md line 45 still describes the in-place design: shared with v1, D1-D20, M0-M6.
4. The journal stops at 19:07. It has no entries for the reviews, the fix pass, the verification or round 2.
5. PLAN.md line 48 says "every change needs password", which DC19 contradicts.

Clean: no install-specific names; the plan-folder structure; the src-go/CLAUDE.md wording; no version bump needed for a docs-only change. qa-changed passes, and plan-qa shows 0 findings for 00209.

## Disposition (build session)

All five are fixed in the N0 close-out commit, after the round-2 re-check. Items 1, 2 and 5 are in PLAN.md, item 3 is in README.md, and item 4 is the journal entries made through mkplan.bash --journal.
