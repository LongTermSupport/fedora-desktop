# Plan 00079 — qa-reviewer, third confirming review, opus-5, 2026-09-24

Saved by the coordinator. The reviewer is read-only and could not write this file.
Reviewed tree: F44 at 27fc80e5 plus the coordinator's uncommitted PLAN.md ticks.

**Verdict: FIX-BEFORE-MERGE.** All four should-fix items and all three nits from the
second confirming review are fixed. The Task 3.7, Task 3.8 and acceptance-criterion ticks
are backed by the HOST captures.

## Should fix

1. **The 2026-09-24 host verdicts are not in the journal.**
   - At HEAD, Task 3.8 said "journal the verdict", and the uncommitted edit removed that
     instruction without doing it.
   - Fix: add a 26-09-24 journal entry giving both run directories, the verdict and
     coverage, and the version probe result.
2. **The uncommitted tree mixes 00079, 00109 and 00134 PLAN.md with `meta-deploy.bash`.**
   The Plan Commit Rule forbids bundling unrelated plan edits in one commit.

## Nits

- One PLAN.md line is 167 characters, where the rest of the file wraps at about 90.
- Check 0 printed `OK —` without incrementing `PASS`. Each capture shows 22 OK lines
  under a verdict of "21 passed". This predates the change.
- The tool and library compares print nothing when they pass, so the captures alone
  cannot show that the library compare ran.

## Checked and clean

- **Acceptance criterion and Task 3.8:** in both 2026-09-24 captures, the deploy-chained
  and the standalone acceptance each give `VERDICT: PASS — 21 check(s) passed, 2 skipped`, with 5 of 5 sessions labelled. The deploy recap shows `failed=0`.
- **The fixed gate ran:** 9b prints SKIP, which dates the gate to 880154a6 or later, the
  commit that added the library compare. 00079 first entered `meta-deploy.bash` in
  34eb1aca, and c2f95be7 is its ancestor, so the check 9 fix was in too.
- **Check 9:** `ok` is judged only when the dry run ran.
- **Task 3.7:** every triage log shows `deployed ccy version (rc=0)` with the version.
- **Status line and criteria:** they match the evidence. The stale references are gone,
  and 3.5(b) links both earlier reports.
- **Fail-fast, over-claims and privacy:** clean.

## Gates

- `qa-all.bash`: rc=0.
- `plan-qa --sweep`: 0 blocking. The only 00079 advisory is the journal-ordering one, from
  the deliberate 19:46 correction entry.

## How the coordinator resolved it

- **Should fix 1:** the journal entry was already committed in `7470b077`, and the
  review ran before that commit.
- **Should fix 2:** `7470b077` did bundle the three plans with the meta change. That
  commit is pushed and stands; later plan edits are committed per plan.
- **Nits:**
  - the PLAN.md line is wrapped;
  - check 0 now counts through `ok()`;
  - both compares print a pass line and count toward the verdict, so a clean run now
    reads 24 passed.
