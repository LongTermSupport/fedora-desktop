# Plan 00079 — qa-reviewer, second confirming review, opus-5, 2026-09-23

Saved by the coordinator. The reviewer is read-only and could not write this file.
Reviewed tree: F44 at e4d9f2ca, the merge of 880154a6.

**Verdict: FIX-BEFORE-MERGE.** Every finding from the first confirming review is fixed in
the code. The plan text around those fixes has drifted.

## Should fix

1. **An acceptance criterion stays ticked, but the fixed gate has never run on the host.**
   - PLAN.md says "the next run covers both files", but no task schedules that run. Task
     3.7 is triage only.
   - The 26-09-23 PASS came from the old gate: the old check 9b, and no library compare.
   - Fix: add a task to re-run acceptance on the host, or un-tick the criterion.
2. **Check 9 still counts a pass for a dry run that never ran.**
   - `acceptance.bash` calls `ok "nothing was frozen by the --ccy dry run"`
     unconditionally. That includes after the skip branch and after the `bad` branch.
   - This breaks the rule that the new check-9b comment itself quotes.
   - Fix: move the `ok` inside the branch that runs the tool.
3. **The PLAN.md status line and Task 3.5(b) are stale.**
   - The status says two success criteria remain, both in Task 3.5. In fact three
     criteria are open, plus Tasks 3.6 and 3.7.
   - Two places still call Task 3.3c the last verdict.
   - The first confirming review is recorded only in the journal.
   - Fix: correct the status and those two places, and link the review from 3.5(b).
4. **The new version probe works only from the root of a main checkout.**
   - `triage.bash` runs `/var/local/claude-yolo/claude-yolo --version`. The launcher only
     reaches `--version` after `check_git_repo`, which tests `[ -d .git ]` in the current
     directory. A migration `read -rp` also runs before it.
   - `probe()` never changes directory, so from any other directory the probe exits 1.
     That includes the root of a worktree.
   - Fix: read `CCY_VERSION` from the deployed file instead, as the 19:12 journal entry
     proposed.

## Nits

- A success criterion now says "resolves", which its note says is shown. It is un-ticked
  for freeze/thaw, which the new wording no longer mentions. Reword it to match Task 3.6.
- The journal says its times are UTC, but it stamps two entries 20:30 and 20:35. They are
  inside a commit made at 19:32 UTC. PLAN.md repeats "20:30".
- A journal line says "19:xx"; the entry it refers to is 19:12.

## Checked and clean

- **Library compare:** correct. The path matches how podfreeze finds its library, and it is
  the only file in `lib/freeze/`.
- **Check 9b:** now `skip()`. A recount gives 21 passed and 2 skipped.
- **`--network` note:** accurate, and the unit suite covers it.
- **Stale comments and journal:** the stale comments are corrected. The commit claims check
  out, and the journal is still append-only.
- **Fail-fast and public-repo safety:** no new fail-fast problems and no private
  identifiers.

## Gates

- `qa-all.bash`: rc=0.
- `plan-qa --sweep`: 0 blocking, and no advisories about 00079.
- `--syntax-check`: not needed; no playbook changed.
