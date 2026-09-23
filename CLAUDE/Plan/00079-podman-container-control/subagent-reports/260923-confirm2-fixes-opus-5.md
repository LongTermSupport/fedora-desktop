# Plan 00079 — fixes for the second confirming review, opus-5, 2026-09-23

Review: [260923-qa-reviewer-confirm2-opus-5.md](260923-qa-reviewer-confirm2-opus-5.md).
Base: F44 at 8122fc0a.

## Should-fix items

1. **Acceptance criterion.**
   - Un-ticked: its PASS came from the gate before both confirming reviews.
   - New Task 3.8 is the HOST re-run of `deploy.bash`, which chains into acceptance.
2. **Check 9.**
   - A flag, `ccy_dry_ran`, is set in the two branches that invoke `freeze --ccy --dry-run`,
     both the one that succeeds and the one that exits non-zero.
   - The "nothing was frozen" assertion is judged only when that flag is set.
   - The skip branch and the listing-failure branch no longer count a pass.
3. **PLAN.md.**
   - The status line lists the four open criteria (the one un-ticked in item 1 makes the
     fourth) and Tasks 3.5 to 3.8.
   - Both "last verdict (Task 3.3c)" references are replaced.
   - Task 3.5(b) links both confirming reports and this one.
4. **Triage version probe.**
   - `deployed_ccy_version()` reads `CCY_VERSION` from `/var/local/claude-yolo/claude-yolo`
     with awk, and no longer runs the launcher.
   - A missing file or a missing version line returns 1 with a message. `probe()` records
     that as `rc=1`, a probe failure, and the script carries on.
   - The awk expression was checked against the repo's launcher. Task 3.7's wording is
     updated.

## Nits

- **The `--ccy` criterion** now reads "pauses, and `thaw --ccy` resumes", matching Task
  3.6. Its note says only selection is shown so far.
- **Timestamps.**
  - PLAN.md's "20:30" reference is replaced by the entry's title and a note that its time
    is wrong.
  - A new 19:46 journal entry records that the 20:30 and 20:35 entries were written before
    19:32 UTC, and that "19:xx" means 19:12. It is appended out of order, as
    `CLAUDE/PlanJournalling.md` prescribes for corrections.

## Gates

- `shellcheck`: clean on both scripts.
- `./scripts/qa-all.bash`: rc=0.
  - As in earlier forks, the worktree needed two temporary stand-ins: a dummy vault
    password file, passed through `ANSIBLE_VAULT_PASSWORD_FILE`, and a link to the main
    checkout's `extensions/node_modules`.
  - Both were removed before the commit.
  - Without them, `ansible-syntax` and the extension gate fail on the missing files.

## Not done

- The HOST runs, Tasks 3.7 and 3.8, cannot be done from the container.
