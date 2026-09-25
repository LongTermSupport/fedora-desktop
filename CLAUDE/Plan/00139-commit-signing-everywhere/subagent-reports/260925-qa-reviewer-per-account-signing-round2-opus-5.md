# QA review, round 2: per-account signing keys (confirming pass)

- **Reviewer:** `qa-reviewer` (Opus 5.5).
- **Diff:** `00aa7d6d..HEAD` on F44.
- **Saved by the coordinator.** The reviewer is read-only and cannot write files, so this
  is the summary it returned.

**Verdict: FIX-BEFORE-MERGE.** Nothing is blocking.

## Round 1

All eight round-1 findings are resolved, except the part of #3 the coordinator declined
(see Should fix 4).

**Blocking finding: fixed and verified.** The new tests fail five times against the 3.67.0
library and pass 31/31 at HEAD. The launch is refused when the signing key comes from any
of these:

- an include;
- `includeIf`;
- a worktree config;
- `GIT_CONFIG_COUNT`;
- a redirected `.git` file.

Hooks and `core.fsmonitor` are never reached on the host. `user:email` covers
`user/emails`. The Jinja renders and parses.

## Should fix

1. **The container can choose the next session's signing key.**
   - How: it adds a `github.com-<B>` remote, and the includeIf keyed on the remote URL
     (`play-github-cli-multi.yml:688-693`) then stages account B's key.
   - Nothing prompts and nothing is printed (`claude-yolo:2012`). The SSH identity, by
     contrast, is picked by the user.
   - Fix: tie the staged key to the session's chosen SSH identity. At least print which
     key was staged, and name the risk in `docs/ccy.md:367-372`.
2. **Acceptance check 11 does not check the #3 fix.**
   - `acceptance.bash:165-199` passes when the machine key is on any account.
   - Its comment at `:169-171` says reading emails needs a scope the check lacks. That is
     no longer true.
   - Fix: assert the key is on the account whose verified emails include `user_email`.
3. **`gh-update-git-config` reads the active account, not the alias's.**
   - `gh api user` at `:849` and `:851` asks about whichever account is active.
   - `gh auth switch` (`:823`) and `git config` (`:854`) are not checked, and it reports
     success regardless.
   - Fix: use that account's `GH_TOKEN`, add `|| return 1`, and add a `*)` arm.
4. **The decline is only half justified.**
   - The author-change reason holds.
   - The push-protection reason does not, because the noreply address answers it.
   - As shipped, a commit in another account's repository is signed but not Verified by
     default, and the docs do not say so.
   - Fix: document that at least. Better: write each account's noreply address into its
     include.
5. **The refusal test covers only the `local` scope.** Add cases for a worktree config,
   `GIT_CONFIG_COUNT` and an `[include]`.
6. **Not established:** whether `user/emails` lists the noreply address. If it does not, a
   noreply `user_email` makes the play refuse on every run.

## Nits

- **The refusal's remedy is wrong for an include.** Probed: the suggested command exits 5
  and the value remains. Print the file that `--show-origin` names.
- **The refusal is broader than needed when signing is off.**
- **`play-github-cli-multi.yml:633-634`** still says "leaves GitHub as it was".
- **`docs/ccy.md:370`** is 112 characters long.
- **The `docs/configuration.md:218` table** is misaligned.
- **The fatal message at `run.bash:2533`** names only the vars file.
- **plan-qa** advises that the 00139 journal is out of order.

## Gates

- `qa-all.bash`: rc 0, 1094 files (1111 in round 1). This diff deletes no file, and the
  reviewer did not find the cause.
- `plan-qa --sweep`: 0 blocking.
- `--syntax-check`: rc 0 on all three playbooks.
- `qa-helper-tests.bash`: 2180 tests, OK.
- `test-run-bash-gh-scopes.bash`: 23/23.
- Extension gates: not triggered.
