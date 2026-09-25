# QA Review, round 2: Plan 00139 D5 (c40a8ab7)

Saved by the coordinator from the reviewer's reply; the reviewer had no file-writing tool.

**Verdict: PASS WITH NITS.** All nine should-fix items from round 1 are closed, and nothing
new is broken.

## Should-fix items closed

1. **Old ccy sessions:**
   - `deploy.bash` refuses before any leg if a `/tmp/claude-yolo-*/git-signing-key` file exists.
   - The play finds and asserts the same just before the retire step
     (`play-github-cli-multi.yml:759-783`). Ansible's `find` counts `/tmp/X/file` as depth 2,
     so `depth: 2` finds them.
   - Acceptance check 7 looks for them too.
2. **`_retired_blob`:**
   - It reads the `.pub` when there is one, otherwise derives the public half from the
     private key.
   - It refuses if it cannot, and skips a key only when both files are gone.
   - It runs before any delete; two new tests cover it.
3. **Real-agent tests:** the `key::` literal case uses the real `ssh-add`, commits and
   verifies. The no-`.pub` case also signs and verifies. 60 of 60 pass.
4. **Check 6** covers every signing key, prints n of m, and tells an empty or locked agent
   (rc 1) apart from no agent (rc 2).
5. **Check 14** counts only this machine's titles. `uname -n` cut at the first dot gives the
   same name as Ansible's hostname fact.
6. **Comments and server step:** the leg-1 comment matches the code, and `deploy.bash` prints
   which key the server must trust.
7. **Server step in the plan:** Task 3.1's host step names that key.
8. **Plan drift:** Plan 00137 and this plan's D3 and D5 are corrected.
9. **Machines with no accounts:** the git-configure block runs exactly when
   `github_accounts_configured` is false. It refuses if `git_signing_key` still names the
   retired key, and runs after `~/.gitconfig` points at the new key.

CCY is 3.70.1, with a changelog entry.

## Nits

- **Warnings on every run:** the play's `find` runs as the user and walks `/tmp`. On a
  standard Fedora host that should print "Skipped … access issue" warnings for root-only
  directories each time. Matching `claude-yolo-*` directories first would avoid that.
- **Stale directories:** the refusal cannot tell a running session from a directory a
  crashed one left behind. Its "can be removed" advice is a manual step.
- **Key mismatch with the server:** a ccy session whose main key is a key file signs with
  that key rather than the one the host picks. If ccy is started in this checkout with a key
  other than the account's alias key, the self-update server would refuse those commits.

**Declined nits:** both reasons accepted.

## Gates

- `qa-all.bash` passed through ansible-syntax (82 playbooks) and stopped at js: the worktree
  has no `extensions/node_modules`.
- The gates after js pass when run by hand: helper tests 2194 OK, ssh-handling 44, signing
  unit tests 50.
- `--syntax-check` passes on both plays. `plan-qa`: 0 block.
- No identifying data in the added lines.
