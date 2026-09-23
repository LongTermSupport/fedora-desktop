# Plan 00137 — qa-reviewer round 4 (Task 5.2, confirming), opus-5, 2026-09-23

Saved by the coordinator. The reviewer is read-only and could not write this file.
Reviewed tree: F44 at 8122fc0a, the merge of the round-3 fixes (872a762a).

**Verdict: PASS WITH NITS.** All three round-3 fixes hold. The reviewer found no route
for unsigned or home-planted code to reach root.

## Nits

1. **A comment says string-valued settings are never places code is read from.** It is
   in `helpers/self_update/cycle.py` (`home_search_paths`) and in `DESIGN-cycle.md`.
   - Ansible reads its module cache from `DEFAULT_LOCAL_TMP/ansiballz_cache`.
   - It is still safe: that setting gets a fresh temporary directory for each process,
     so nothing can be planted there before a run. The remaining risk is tampering while
     a play runs, which PLAN.md D5 already records.
   - Fix: reword both places to say that.
2. **No test covers the wrapper's new error branch.** It fires when git cannot list the
   clone's untracked files. The reviewer checked the pattern by hand and it catches a
   failure. The gap is only that the test suite would not catch a regression.

## Checked and clean

- **Fix 1, the remedy wording:** both dirty-clone refusals give the working remedy.
  Turning `self_update_enabled` off deletes the clone, and the clone task never updates
  an existing clone (`update: false`). Two e2e tests check the wording, and both docs
  match.
- **Fix 2, the exact-path exception:** the wrapper compares exact paths from
  `ls-files --others -z`, as `update.py` does.
  - A directory at the host_vars path is refused.
  - A nested repository there shows up as `path/`, which does not match either.
- **Fix 3, the bare `~`:** tested against real ansible-core 2.19.13.
  - Ansible expands `~` in every path-typed setting before dumping it. The reviewer set
    `ANSIBLE_LIBRARY` to `~`, `~root/x`, `~zzz/y` and `~/../z`, and the dump gave the
    home, a path under root's home, a path relative to the working directory, and
    `/z`. Every result under the home is flagged.
  - So `~`, `~user` and `~/../x` cannot reach the check unexpanded in any path setting.
  - The only bare `~` left in the dump is the suffix in `INVENTORY_IGNORE_EXTS` and
    `MODULE_IGNORE_EXTS`.
  - Literal `~/`, `~//x` and `~/../root/y` are all flagged.
  - The unpinned dump gives the 18 findings the journal claims.
- **What is left, and does not reach root:** a literal `~user`, or `label@~/x` in the
  vault-identity list, is not flagged in settings that are not path-typed. Config comes
  only from the signed clone's `ansible.cfg` and the pinned `env -i` environment. A vault
  script would run as the user, and the vault password file is pinned to `/dev/fd`.
- **The open item on string-valued path settings:** honestly recorded in DESIGN and PLAN
  D5.
  - `DEFAULT_LOCAL_TMP` is fresh for each run.
  - The galaxy and persistent-connection settings are unused under `transport=local`.
  - Plugin settings such as `remote_tmp` are not checked, because `ansible-config dump`
    defaults to `-t base`. That is the same `~/.ansible/tmp` risk D5 records.
- **Acceptance \[0\]:** the extra-argument check passes only when all four exact argument
  lists were allowed.
- **Plan state:** Task 5.2 was correctly still open, the journal entry matches the diff,
  and there are no untracked plan files.
- **Public-repo safety:** nothing in the diff identifies an install.

## Mechanical gates

- `qa-all.bash`: exit 0, including the self-update-cycle suite and the helper tests.
- `plan-qa --sweep`: 0 blocking, and none of the advisories are about 00137.
- `--syntax-check`: not needed; no playbook changed.
