# Plan 00079 — qa-reviewer, fourth confirming review, opus-5, 2026-09-24

Saved by the coordinator. The reviewer is read-only and could not write this file.
Reviewed tree: F44 at 43cf3595. The commits after it do not touch 00079.

**Verdict: FIX-BEFORE-MERGE.** One claimed fix was never made. Everything else checks out,
including the new `ok()` calls.

## Should fix

1. **The long-line wrap is recorded as done, but the line was never wrapped.** The Task 3.8
   line was still 167 characters. Two records nonetheless say it was fixed:

   - the 07:45 journal entry;
   - the resolution section of the third review's report.

   Fix: wrap the line, and correct the record with a new journal entry.

## Nits

- **"A clean run reads 24 passed" holds only for a fully labelled fleet.** The reviewer
  counted the `ok()` calls on the all-pass path:

  | Checks           | `ok()` calls              |
  | ---------------- | ------------------------- |
  | the two compares | 2                         |
  | check 0          | 1                         |
  | checks 1 to 8c   | 11 (check 4 counts twice) |
  | check 9          | 3                         |
  | checks 10 to 12  | 3                         |
  | check 13         | 2                         |
  | checks 14 and 15 | 2                         |

  That is 24 passed and 2 skipped (9b and 13b). With an unlabelled session running, 9b
  adds 1 and 13b adds 2, so it reads 27 passed and 0 skipped.

- **`acceptance.bash --help` omits the two deployed-copy compares**, which now count
  toward the verdict.

## Checked and clean

- **The compares:** each `ok()` is reached only after its `cmp -s` passes. A mismatch or a
  `cmp` error exits 1. The `-x` and `-f` checks run first.
- **Check 0:** `ok()` runs only when the suite exits 0 and prints a `passed:` line.
- **No other unearned pass:** no `ok()` counts an assertion that did not run. 9b, 13 and
  13b use `skip()` on their branches that run no tool, and check 9's third `ok` is gated on
  the dry run having run.
- **The third review's findings:** should-fix 1 is resolved by `7470b077`. Should-fix 2
  stands as recorded.
- **Status and criteria:** they match the evidence. 3.5(a) and 3.6 are correctly left as
  human HOST checks.
- **Plan Commit Rule, fail-fast and privacy:** clean.

## Gates

- `qa-all.bash`: rc=0.
- `plan-qa --sweep`: 0 blocking. The only 00079 advisory is the known journal-ordering one.
- `bash -n` and `shellcheck` on `acceptance.bash`: clean.

## How the coordinator resolved it

- **The line:** reworded, so that no break falls inside an inline code span. The markdown
  formatter had been rejoining the earlier break for that reason.
- **The verdict count:** stated for both fleets.
- **`--help`:** now names the compares.
- **The record:** corrected by the 08:05 journal entry.
