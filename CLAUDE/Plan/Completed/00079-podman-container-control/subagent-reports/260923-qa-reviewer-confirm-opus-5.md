# Plan 00079 — confirming qa-reviewer, opus-5, 2026-09-23

Saved by the coordinator's fork: the reviewer is read-only and could not write this file.

**Verdict: FIX-BEFORE-MERGE**

## Should fix

1. **Acceptance compares only half of the tool with the repo.** `acceptance.bash:144` byte-compares `podfreeze` alone. `play-podfreeze.yml` says "half of podfreeze lives in the library" (`~/.local/lib/freeze/freeze-common.bash`), and nothing compares the deployed library with the repo. So the ticked criterion "refuses to vouch for a binary that differs from the repo", and the journal's "byte-checked deployed copy", cover only the entry script. This is the `CCY_HASH` shape: one file named where the program has grown into several. Fix: `cmp` the library too.
2. **Check 9b counts a pass for an assertion it never ran.** `acceptance.bash:491` calls `ok()` without invoking the tool when there are no unlabelled sessions, which breaks the rule at 633–636 that check 13b was fixed to follow. Today's run took that branch (all six sessions labelled), so "22 passed" is really 21. Fix: `skip()`, a journal correction, and the count restated.
3. **Two ticked criteria claim more than was shown.**
   - "`freeze --ccy` / `thaw --ccy` *operates*": `thaw --ccy` never runs, and `--ccy` only ever runs with `--dry-run` (checks 9 and 9b).
   - "`--network` pauses *exactly*": the throwaway network has one member, so nothing shows other containers being left alone. The unit suite covers that selection, but `do_action` is untested by its own header (`scripts/test-podfreeze.bash:31`).
   - Fix: reword to "resolves" and "selects", and cite what each check shows.

## Nits

- Stale comments: `acceptance.bash:678` says `deploy.bash` removes `podman-freeze`, but the play does. `:460`'s "4 of 6 live sessions predate 3.40.0" is out of date: triage shows 6 of 6 labelled.
- The 26-09-23 journal gives `3a3e8b93` as podfreeze's last change. That commit is the library's; podfreeze's is `cfe8afbe`. Both postdate the 3.3d fix `bb75d40d`, so the conclusion holds. It also puts the failing probe at `triage.bash:87`; it is at `:147`.
- The known `ccy --version` probe failure (rc=127) is journalled but has no task.

## Checked and clean

- `play-podfreeze.yml` owns the tool deploy, the library include and the rename cleanup; there is no manual `rm` in `deploy.bash`.
- None of the tool, library, suite, play or plan scripts has changed since `7a24c626`. `3900c255`'s `passed:` guard is correct.
- `docs/playbooks.md` matches the current behaviour.
- Tasks 3.5 and 3.4 are rightly open, and the README row is present.

## Gates

- `qa-all.bash`: rc=0. podfreeze 187 passed, freezelib 240 passed.
- `plan-qa --sweep`: no block, none about 00079.
- `--syntax-check play-podfreeze.yml`: OK.
