## QA Review: commit 36b56d78 (the gh-<alias> and clone-<alias> wrappers)

**Verdict**: FIX-BEFORE-MERGE (no blockers; one defect of the same kind left in place, plus test and plan gaps)

### Should fix
1. **The same defect is still live in two other wrappers in this block.** `gh-token-<alias>` (`playbooks/imports/play-github-cli-multi.yml:1212-1215`) and `gh-<alias>-token-phpstorm` (around `:1235`) both run `gh auth switch --user "{{ username }}" 2>/dev/null` and never switch back. That leaves gh on another account, which is the exact failure this commit fixes. The first one also hides a failed switch and then prints the wrong account's token. Both should go through `_gh_as_account`.
2. **The test never runs `clone-<alias>`.** `scripts/test-gh-alias-wrappers.bash:153` only greps that the line exists. A regression in its `|| exit_code=$?` handling or in the restore-after-clone path would pass. The test should extract and run it, as it already does for `gh-{{ alias }}`.
3. **The stub ignores the real `--jq` query.** The `auth status` stub (`:56-58`) does `cat active` whatever arguments it gets, so a broken query in `_gh_active_account` would pass. I checked the query by hand against gh 2.101.0 (results below). The test should either check the arguments it receives or replay a captured `--json hosts` document through real `jq`.
4. **An empty `previous` silently skips the restore** (`:1131`). With `GH_TOKEN` set, gh returns an active row with an empty login and exit code 0, which I observed. The wrapper then switches and never switches back. It should fail when `previous` is empty instead of carrying on.
5. **Plan drift.** The earlier review's finding 3 (`CLAUDE/Plan/00139-commit-signing-everywhere/subagent-reports/261002-qa-reviewer-config-repo-primary-opus.md:21`) asked for a tracked follow-up. Neither PLAN.md nor a JOURNAL entry records that this commit settles it. Journal line 651 still says the wrappers "already put the active account back", and nothing later corrects it.

### Checked and clean
- **Invalid token:** I probed with a scratch `GH_CONFIG_DIR` holding two invalid tokens. `--json hosts --jq …` exits 0 and prints the active login. Plain `gh auth status` exits 1. With nobody logged in, it exits 0, stdout is empty, and the message goes to stderr. So an invalid token does not make every wrapper fail.
- **Other callers:**
  - `run.bash:2978-2993` sources the file and uses `gh-lts`. It benefits from the change.
  - Nothing else uses `gh-get-default` or the restore-to-default behaviour. `run.bash:2855` reads `default-account` on its own.
  - Nothing under `files/` or the ccy launcher calls these wrappers.
- **Jinja and Ansible 2.19:** I rendered the real block with Ansible's jinja2. It renders, and `bash -n` passes on the output. The new functions use no jinja delimiters.
- **Stderr:** every message goes to stderr, and switch output goes to `/dev/null`.
- **Exit codes:** the command's exit code is passed through, and a failed switch either way returns 1.

### Mechanical gates
- **qa-all.bash:** exit 0, including `✓ gh-alias-wrappers: passed: 16`.
- **plan-qa --sweep:** 0 block and 11 advise. One advise is about the 00139 journal's time order, which this commit did not touch. The sweep still exited 1.
- **syntax-check:** `play-github-cli-multi.yml` passes with a dummy vault script.
- **No version bump needed:** nothing under `claude-yolo` changed. I did not read the vault or `localhost.yml`.

I created some probe files and did not delete them: `/workspace/untracked/scratch/ghprobe/`, `ghprobe-empty/`, `dummy-vault.sh`, `rendered-gh-aliases.bash` and `qa-all-review.log`.