# Plan 00139: fixes for the first qa-reviewer pass

This fixes all ten findings in [260924-qa-reviewer-opus-5.md](260924-qa-reviewer-opus-5.md).
Finding 5's journal half was filed earlier, in `ddc77c74`. PLAN.md's Delivery section is
filled in here.

## 1 (blocking): tests that read the machine's git config

**How it was measured.** A scratch global config was passed in with `GIT_CONFIG_GLOBAL`,
in two versions. One signs with a real throwaway key, the way a deployed host does. The
other signs with a key path that doesn't exist, so any commit that reads the global config
fails. A test passing under the second one therefore reads no global config at all.

The tests covered are every test that makes commits or tags: `test-self-update-cycle`,
`test-secret-scan`, `test-ccy-git-signing`, and every helper unit test through
`qa-helper-tests.bash`.

| Test                                 | Plain    | Signs             | Missing key                    |
| ------------------------------------ | -------- | ----------------- | ------------------------------ |
| self-update-cycle, before            | 163 pass | 149 pass, 14 fail | 149 pass, 14 fail              |
| helper tests, before                 | OK       | OK                | 1 error: `test_affected_plays` |
| secret-scan, ccy-git-signing, before | pass     | pass              | pass                           |
| every test, after                    | pass     | pass              | pass (163 / 29 / 23 / OK)      |

In the helper suite only one test read the global config:
`test_affected_plays.TestCli.test_old_and_new_shas_come_from_git`. The four `play_ledger`
tests, `test_judge_run` and `test_login_report` already isolate their git calls, or never
commit through the user's config. Under the missing-key config they pass.

**The fixes:**

- `scripts/test-self-update-cycle.bash`:
  - It exports `GIT_CONFIG_GLOBAL=/dev/null` and `GIT_CONFIG_NOSYSTEM=1` for the whole
    script, which reaches the wrapper's git calls too.
  - It unsets `GIT_CONFIG_COUNT` and `GIT_CONFIG_PARAMETERS`.
  - A new guard runs before any case: a commit made without `-S` must come out unsigned,
    or the script exits 1.
  - Proof: a copy with the export and unset lines removed, run under the signing config,
    exits 1 on that guard.
- `tests/helpers/self_update/test_affected_plays.py`: the test's git calls get their own
  isolated env. Proof: that module alone, run under the missing-key config, is OK.
- `scripts/qa-helper-tests.bash` is the shared mechanism. It exports the same isolation
  before the single unittest call, so every current and future helper test run through
  the gate is covered.
- `helpers/CLAUDE.md` now says each test passes an isolated env, because the runner
  doesn't cover a module run by hand.

## 2: deploy order

`deploy.bash` now runs `play-claude-yolo.yml` first, then `play-git-configure-and-tools.yml`.

- **The reason:** a 3.66 launcher with signing off stages nothing and refuses nothing.
  Signing switched on under an older launcher would start containers whose every commit
  fails. The deploy stops at the first failing leg, so this order can never leave that
  state behind.
- **The text:** the header comment and the help text say so.
- **The NEXT hint:** it now switches gh to the right account first (nit 9).

## 3: ccy docs

- `docs/ccy.md`:
  - The `--no-ssh` row and the example comment say the signing key is still staged.
  - The residual-risk list has a new bullet: the signing key is in every session, and it
    is release authority for a self-updating server that trusts it.
- `docs/ccy-changelog.md` 3.66.0 no longer claims the key "never leaves" the temp
  directory. On the host the copy sits only in that directory. Inside the container,
  anything the session runs can read it.

No launcher file changed, so there is no CCY version bump.

## 4: passphrase assert diagnosis

`ssh-keygen -y -P ""` was measured on OpenSSH 9.2:

| Key              | stderr                                                 |
| ---------------- | ------------------------------------------------------ |
| Has a passphrase | "incorrect passphrase supplied to decrypt private key" |
| Empty file       | "error in libcrypto"                                   |
| Garbage          | "error in libcrypto"                                   |
| Truncated        | "error in libcrypto"                                   |

The play now has two asserts, each with its own remedy:

- **"Is a private key ssh-keygen can read"** passes when rc is 0 or stderr says
  "incorrect passphrase". Otherwise it quotes ssh-keygen's words. The remedy is to remove
  the pair and re-run, or to point `git_signing_key` at a real key.
- **"Needs no passphrase"** keeps its existing message.

`--syntax-check` passes, run with a dummy vault password file because the worktree has
none. The OpenSSH 10 wording on Fedora 44 was not observed here. The string comes from
OpenSSH's fixed error table for `SSH_ERR_KEY_WRONG_PASSPHRASE`.

## Nits

- **6 (`test-ccy-git-signing.bash`, now 23 checks):**
  - It asserts that staging comes before the session's single
    `container_cmd run $DOCKER_FLAGS --rm` line.
  - A new case stages a REAL key with the mount point set to the stage directory, then
    deletes the host key, as it is absent inside the container. It then makes a plain
    commit and an annotated tag with only the copy as global config, and verifies both
    against the public half.
  - Mutation check: a library that leaves the copy naming the host key fails 4 checks,
    including both real-signing checks.
- **7 (acceptance check 8):** only `git config` exit 1 counts as "not set". Any other exit
  is reported as unreadable.
- **8 (acceptance check 11):**
  - It now states what it proves: the key is registered as a signing key on a logged-in
    account.
  - It also states what it cannot prove: that commits will show Verified, which needs the
    committer email verified on that account.
  - When no account could be read, it reports UNKNOWN rather than "not registered".
  - When some accounts could be read, it names the count read and the unreadable ones.
- **9:** `docs/configuration.md` and the deploy hint both run
  `gh auth switch --user <that account>` before `gh auth refresh` / `gh ssh-key add`.
- **10:** Plan 00137's acceptance owner step for the unsigned commit now says to use
  `git -c commit.gpgsign=false commit ...`.

## Gates

`ccy-git-signing` passes 23 of 23, `self-update-cycle` 163 of 163, and the helper tests are
OK. shellcheck is clean on every changed script. For `qa-all.bash`, see the commit message.
The review report this links to was saved in the main checkout by the coordinator.
