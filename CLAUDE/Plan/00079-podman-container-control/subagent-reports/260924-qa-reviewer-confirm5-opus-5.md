# Plan 00079 — qa-reviewer, fifth confirming review, opus-5, 2026-09-24

Saved by the coordinator. The reviewer is read-only and could not write this file.
Reviewed tree: F44 at fb7ed1c8.

**Verdict: FIX-BEFORE-MERGE.** All three fixes from the fourth review hold when checked
with commands. One new should-fix concerns the journal.

## Should fix

1. **Journal times are later than the commits that contain them.**
   - The 26-09-24 `08:05` entry was committed in fb7ed1c8 at 07:48:32 UTC. The reviewer's
     own `date -u` read 07:49, so the entry was timed 16 minutes into the future.
   - The `07:45` entry was committed in 43cf3595 at 07:36:32.
   - On 26-09-23, the `20:30` and `20:35` entries were already in 880154a6, committed at
     19:32:15. They are what triggers plan-qa's `journal-entry-ordering` advisory.
   - Fix: one correction entry through `mkplan.bash --journal`, which reads the clock
     itself, giving the real times. Use `--journal` for every entry from now on.

## Checked and clean

- **Wrap:** no PLAN.md line is over 100 characters except one bare markdown link, which
  cannot be wrapped.
- **Counts:** from the `ok()` calls, 24 passed and 2 skipped when every session is
  labelled; 27 passed and 0 skipped when an unlabelled one is running. Both match PLAN.md.
- **`--help`:** exits 0 and names both deployed-copy compares, in an order that matches
  the script.
- **Regressions:** fb7ed1c8 is the only commit since 43cf3595 that touches 00079, and it
  changes only the `--help` text.
- **Status, criteria, Plan Commit Rule, privacy and fail-fast:** clean.

## Gates

- `qa-all.bash`: rc=0.
- `plan-qa --sweep`: 0 blocking. The only 00079 advisory is the journal-ordering one,
  caused by finding 1.
- `bash -n` and `shellcheck` on `acceptance.bash`: clean.

## How the coordinator resolved it

- **The correction:** a finding entry through `mkplan.bash --journal` (stamped 07:55 by the
  tool) gives the real commit time of each of the four entries.
- **Categories:** it also records that the hand-written entries used categories the
  grammar lacks (`verdict`, `correction`), which the scaffolder refuses.
