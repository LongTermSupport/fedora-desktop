# Plan 00137 — qa-reviewer round 3 (Task 5.2), opus-5, 2026-09-23

Saved by the coordinator. The reviewer is read-only and could not write this file.
Reviewed tree: F44 at c7d0384a, the merge of the round-2 fixes.

**Verdict: FIX-BEFORE-MERGE**

The round-2 BLOCK is closed. The reviewer found no remaining route for unsigned code to
reach root.

## Should fix

1. **The refusal message for stray files names a remedy that cannot work.**
   - Where: `files/usr/local/sbin/fedora-desktop-self-update`, the "differs from HEAD"
     and stray-file refusals.
   - Both say "re-run the self-update play". The clone task has `update: false`, and the
     anchor (`helpers/self_update/update.py`) refuses the same files with exit 11. The e2e
     test expects that 11.
   - The only fix through the playbooks is to run the play with self-update disabled,
     which removes the clone, and then enabled again.
   - Fix: give these two refusals their own message saying that, and document it in
     `docs/playbooks.md`.
2. **The wrapper's host_vars exception is wider than the anchor's.**
   - The `--exclude=/…/localhost.yml` pattern also matches a directory of that name, and
     hides everything under it. The reviewer showed that an `--exclude` on a directory
     hides its whole subtree from `git ls-files --others`.
   - The anchor compares exact paths, so it refuses such a directory. The wrapper and
     the anchor therefore disagree.
   - This is not exploitable: only root writes the clone, and a symlink or `.pth` at that
     path is never imported. But `DESIGN-cycle.md` and the docs say only that one file is
     allowed.
   - Fix: use `ls-files --others -z` with exact-path comparison in the wrapper. Add a test
     with a directory at that path.
3. **The search-path check still depends on setting names containing `PATH`.**
   - Where: `helpers/self_update/cycle.py`.
   - The `DEFAULT_HOST_LIST` exception shows that the naming is not a reliable rule.
   - Judging every list-valued setting costs nothing, because relative values resolve
     under the clone, not the home directory.
   - Fix: drop the name filter.

## Nits

- `acceptance.bash` check \[0\]: the extra-argument probe passes on any refusal, even when
  the four grant checks before it failed. Make it COULD NOT ESTABLISH unless all four
  passed.
- `DESIGN-cycle.md`: the pycache prefix does not stop a `.pyc` that has no source file
  from being imported. Only the stray-file check stops that; say so.

## Checked and clean

- **Clone and anchor:**
  - the clone uses `recursive: false`;
  - the anchor checks for dirt and strays both before moving HEAD and after;
  - `--allow-untracked` works only with `--anchor`, and sudo cannot reach it.
- **Python flags:** root runs with `-E -s -B -X pycache_prefix`. The e2e test shows that
  nothing untracked is left in the clone after a cycle.
- **Search-path dump:**
  - it runs as the user with a clean environment (`env -i`), from inside the clone, so
    neither `ANSIBLE_CONFIG` nor `~/.ansible.cfg` can reach it;
  - its environment differs from the plays' only in settings that are not paths;
  - `run.bash` sets no `ANSIBLE_*` variable and runs from the same clone;
  - the check runs after the update, so it judges the new commit's settings.
- **Pinned list:** it covers every search path in ansible-core 2.19.13's `base.yml`.
- **Allowed-signers file:** the checks on the file and its directory run before the
  signature check.
- **Acceptance [0], `sudo -n true`:** the new pass conditions are sound.
- **PLAN.md and docs:** both current.

## Mechanical gates

- `qa-all.bash`: exit 0. That run included:
  - ansible-syntax on every playbook;
  - the self-update-cycle suite;
  - the helper tests.
- `plan-qa --sweep`: 0 blocking. No advisories for 00137.
- A standalone `--syntax-check` could not run in the container because no vault password
  file is available there. The ansible-syntax gate inside `qa-all` covers this play.
