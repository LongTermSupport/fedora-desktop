# QA Review round 2: Plan 00139 fixes (d16b7653..8003868e)

Saved by the coordinator: the reviewer is read-only.

**Verdict: PASS WITH NITS.** One should-fix: a new guard is blind in one case. It cannot
produce a false pass.

## Findings 1–10

01. **FIXED.** Both tests were run five ways, with a throwaway ed25519 key in
    `untracked/scratch/`: plain, signing on via `GIT_CONFIG_COUNT`, missing key via `COUNT`,
    signing on via a global config file, missing key via that file.
    - `test-self-update-cycle.bash`: 163 passed, 0 failed, every way.
    - `qa-helper-tests.bash`: 2116 tests, OK, every way. Running every helper module
      without the runner under either config file is also OK.
    - The guard, with isolation removed and the key present: lines 72–80 were replayed. The
      commit came out signed and the guard exited 1.
02. **FIXED.** `deploy.bash:72-76` runs the launcher first.
03. **FIXED.** `docs/ccy.md:367-371,651,1117` and `docs/ccy-changelog.md:25-28`. No
    launcher change, and `CCY_VERSION` 3.66.0 matches the changelog.
04. **FIXED.** `play-git-configure-and-tools.yml:111-121`. On OpenSSH 9.2: "incorrect
    passphrase…" for a key with a passphrase, and "error in libcrypto" for empty, garbage and
    truncated files.
05. **FIXED.** Journal entry at line 49; Delivery at `PLAN.md:125-128` (see nit C).
06. **FIXED.** See nit A.
07. **FIXED.** `acceptance.bash:131-140`.
08. **FIXED.** `acceptance.bash:166-199`.
09. **FIXED.** `docs/configuration.md:217-221` and `deploy.bash:80`.
10. **FIXED.** `00137/acceptance.bash:97`.

## Should fix

1. **The guard reads "no commit" as "unsigned"** (`scripts/test-self-update-cycle.bash:72-73`).
   - The probe commit's exit code is never checked.
   - With isolation removed and the key missing, the commit fails with rc=128.
     `cat-file HEAD` then fails, `grep` finds nothing, and the guard passes. Confirmed by
     replaying those lines.
   - The later fixture commits still fail loudly, so the effect is a wrong diagnosis, not a
     false pass.
   - Fix: require the commit to succeed and `git rev-parse --verify HEAD` to work, and exit
     1 otherwise.

## Nits

- **A.** The real-key case in `scripts/test-ccy-git-signing.bash:163-191` never unsets
  `GIT_CONFIG_COUNT` or `GIT_CONFIG_PARAMETERS`. Under both `COUNT` variants it scores
  21/23, with both signing checks failing. Under the config-file variants it scores 23/23.
  Fix: add the `unset` near the top, as the other two scripts have.
- **B.** The rule in `helpers/CLAUDE.md:78-81` and the fix in `test_affected_plays.py:413`
  let an inherited `GIT_CONFIG_COUNT` through.
  - Running the modules without the runner under the `COUNT` missing-key variant gives 12
    errors: 1 in `test_affected_plays`, 11 in the older `test_update.py`.
  - A normal gate run is clean, because the runner unsets it.
- **C.** Nothing in the journal covers the fix round, and there is no handoff entry.
  Delivery doesn't list the merge commit `8003868e`.

## Checked and clean

- **Runner-wide isolation:** breaks no helper test.
- **Signing-key checks:** the "can read" check runs before the passphrase check.
- **Old wording:** none of the old `--no-ssh` or "never leaves" wording is left.
- **Public-repo hygiene:** across `1382ef08..8003868e`, only `<user>` / `{{ user_login }}`
  paths and `example.com` addresses. No other infrastructure repo, hostname or username is
  named.
- **Plan tracking:** the README row is present, and Task 4.4 is 🔄.

## Gates

- **`qa-all.bash`:** rc=1, from the same three container-tool gates as round 1:
  toolchain, bash-history-search (no `gawk` in this container) and helper-counts-reader.
  `ccy-git-signing` (23), `self-update-cycle` (163) and `secret-scan-tests` (29) pass.
- **`plan-qa --sweep`:** 0 blocking, nothing for 00139.
- **`--syntax-check`** on the play: rc=0.
- **`qa-helper-tests.bash`:** OK (needed, because `tests/helpers/` changed).
