# Plan 00137 — qa-reviewer round 2 (Task 5.2), opus-5, 2026-09-23

Condensed by the fixing fork. The reviewer is read-only and could not write this file.
Reviewed tree: F44 at 95ed491f, the merge of the round-1 fixes.

**Verdict: BLOCK**

## Blocking

1. **Unsigned code can still reach root through ignored files in the clone.**
   - The clone task (`play-self-update.yml`, the `ansible.builtin.git` task) leaves
     `recursive` at its default of `yes`. A fresh clone therefore pulls in the unsigned
     tip's submodules.
   - `update.py --anchor` then runs `checkout -B` and never checks the tree again. Git cannot
     remove a submodule directory that still holds files, so it stays behind.
   - The wrapper checks `git status --untracked-files=all`, which never lists ignored files,
     and `.gitignore` ignores `__pycache__/` and `*.pyc`.
   - Root then runs `python3 -m helpers.self_update.cycle` from inside the clone. Python loads
     an unchecked hash-based pyc from `helpers/self_update/__pycache__/` without comparing
     it to the source.
   - So anyone who can push can plant root's code. The play's `find` ownership check passes,
     because the files are root's.
   - The reviewer did not reproduce this (the review was read-only). The ignored-file blind
     spot itself is certain.
   - `docs/playbooks.md` and the wrapper comment say the tree matches the signed commit,
     which is untrue while this stands.
   - Fix:
     - set `recursive: false` on the clone;
     - make the wrapper and the anchor refuse any ignored file except the host_vars copy;
     - run root's Python, and the plays, with `-B` and a `PYTHONPYCACHEPREFIX` in a
       root-only directory;
     - add an end-to-end test of an unsigned tip with a submodule at an ignored path.

## Should fix

2. **The pinned search-path list is ansible-core 2.19's, but the play requires 2.20 or
   newer.**
   - The list (DESIGN-cycle.md, `cycle.py`) is complete for 2.19.13; the reviewer checked it
     against `base.yml`.
   - The test copies the same list, so a search path added in a later release passes both.
   - Fix: in `check_toolchain`, run `ansible-config dump` as the user and refuse any search
     path under `$HOME`.

## Nits

- The wrapper checks HEAD against `ALLOWED_SIGNERS` without checking who owns that file or
  can write it. The Python check runs only after the clone's code is imported.
- `acceptance.bash` check [0] treats any sudo refusal as a pass, including "a password is
  required".
- The password test only searches `$TMPDIR` for leftover copies ("tmp-copies"), which is
  narrower than the claim that the password never touches disk.
- The cycle's first-run refusal cannot be reached through the real wrapper, which exits
  first. DESIGN-cycle.md says so honestly.

## Checked and clean

- Other routes into root:
  - only root writes the clone;
  - `.pth` files are read only from root-owned site directories;
  - git hooks, fsmonitor, the `ext` transport and the gpg programs are pinned on the command
    line;
  - the interpreter is the system Python.
- run.bash 1.25.0: the password travels only through pipes. Ansible 2.19 reads `-` from
  stdin.
- The 19 pinned paths match 2.19's `ANSIBLE_HOME` defaults, `PYTHONNOUSERSITE` is set, and
  `~/.ansible/tmp` is stated honestly as the remaining risk.
- Sudoers: exactly four argument lists. The five extra-argument tests each exit 64 with
  nothing called.
- Refusals: a wrong remote, a gate refusal and a tampered commit each exit 20, are recorded,
  and are tested.
- Round-1 nits: both fixed.
- PLAN.md, the index row and the Overview are current.

## Mechanical gates

- `qa-all.bash`: exit 0. This included ansible-syntax on 82 playbooks, 2054 helper tests,
  self-update-cycle (119 passed) and run-bash-single-play (51 passed).
- `plan-qa --sweep`: 0 blocking, 7 advisories, none for 00137.
- ESLint and the extension-compat check: not triggered by this diff.
