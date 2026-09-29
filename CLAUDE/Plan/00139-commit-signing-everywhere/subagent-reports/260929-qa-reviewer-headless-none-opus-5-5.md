## QA Review: `fix-headless-none-commit-signing` (fb3b0203, one commit on F44 b1628ab2)

**Verdict**: FIX-BEFORE-MERGE

Nothing blocks. The fix is correct, it sits in the right place, and it is tested on the production path. Three small documentation and plan drifts need fixing before merge.

### Blocking

None.

### Should fix

1. **The play's catalogue entry still says it signs on every machine.** `docs/playbooks.md:249`
   The bullet for `play-git-configure-and-tools.yml` reads "Signs every commit and tag with this machine's login key, `~/.ssh/id`…" with no qualifier. After this change that is false for a `github_accounts: {}` box. The same unqualified wording opens the SSoT section at `docs/configuration.md:208` ("On by default. Every commit and tag made on this machine is signed"), while the exception only appears 60 lines later at `:267`. The play's own header comment has the same pattern: line 42 is unconditional and the qualifier sits at lines 52–53.
   Fix: qualify the playbooks.md bullet (e.g. "…where the machine declares a GitHub identity or `git_signing_key`"). Change configuration.md:208 to say signing is on by default *except* on a no-identity box, pointing at the `:267` paragraph.

2. **Five hand-written gate counts in `CLAUDE/QA.md` are each one more wrong.** `CLAUDE/QA.md:19-22, 49, 270, 278`

   - The claimed figures are "forty-four gates", "thirty-seven" separate gates, "45 stage names", "29" composed stage lines, and "22 gates" using `qa_gate_case_count()`.
   - They were already stale at F44. The table under "Thirty-seven further gates" had 47 rows and now has 48 (counted with awk). `=$(qa_gate_case_count ` call sites went from 38 to 39 (`git show F44:` vs HEAD). The CI log shows about 57 distinct stage names.
   - The diff adds its row directly under the "Thirty-seven" sentence and leaves every figure alone.
   - Fix: delete the numerals rather than bump them. QA.md's own "This inventory is derived, not maintained" paragraph (`:30-35`) explains why. `check_qa_gate_inventory` already enforces the row-to-gate bijection, so the numbers carry no information that isn't already checked.

3. **PLAN.md ticks a PR that does not exist.** `CLAUDE/Plan/00139-commit-signing-everywhere/PLAN.md:210`
   The ✅ sub-item says "Branch `fix-headless-none-commit-signing`, opened as a PR against F44". `gh pr list --head fix-headless-none-commit-signing --state all` returns `[]`. PlanWorkflow.md:383/394 says ✅ means finished and verified.
   Fix: open the PR (the body is already drafted), or reword to "pushed" until it exists.

### Nits

- **A none box that later adds an account fails at the key assert, with advice that doesn't fit that path.** `run.bash`'s none `localhost.yml` points to `scripts/gh-account-setup.bash --add=alias:username` (run.bash:608-609). That script only creates `~/.ssh/github_<alias>` (gh-account-setup.bash:382-388), never `~/.ssh/id`. On the next play run `git_signing_declared` becomes true and the play fails at the assert, whose message says "run.bash generates ~/.ssh/id".
  - This fails loudly, so it respects fail-fast.
  - It predates this change: `play-github-cli-multi.yml:653-664` asserts the same key.
  - Rerunning run.bash does generate the key (run.bash:2375-2377).
  - Fix: one sentence in the new configuration.md paragraph saying what adding an identity later requires.
- **The walker's definition of "signing task" is three substrings.** These are `git_signing_key_path`, `git_signing_public_key` and `gpgsign` (test:197). A future ungated task using a literal `~/.ssh/id`, or setting `user.signingkey`/`gpg.format` directly, would pass "no signing task runs outside the gate". The exact-7 count (test:269) guards against tasks moving out of the block, but not against new ones added outside it. Acceptable. A one-line comment stating the classifier's scope would stop the check reading as exhaustive.
- **Part 3 never runs the block with the gate made true by `github_accounts` alone.** Every desktop case also sets `git_signing_key` (test:322-336). That truth path is only exercised through Part 1's verdict play. That play uses the same vars file and the same `when:`, so the gap is small.

### Checked and clean

- **Gate semantics.** `vars/git-signing.yml:14-17` is consistent with its callers:
  - run.bash's none path writes `github_accounts: {}` (run.bash:604-611) and skips keygen (run.bash:2363-2367).
  - Interactive run.bash always writes `github_accounts` (run.bash:3055-3057).
  - No tracked group_vars or `.dist` file gives `github_accounts` a default.
  - Treating an undefined `github_accounts` as "sign" differs deliberately from the retire block (play:157) and from `github_accounts_configured` (cli-multi:165). Those two ask "is anything registered on GitHub"; the new gate is fail-closed, and the vars comment says so. There is no contradiction: an undefined box both signs and runs the local retire.
- **Other dependencies on the none path in `playbook-main.yml`:**
  - `play-basic-configs.yml:350-363` copies the key only if the stat finds it.
  - `play-github-cli-multi.yml` does `end_play` at :168-178 when there are no accounts, and its signing block is gated on `github_accounts_configured`.
  - No other `~/.ssh/id`, `gpgsign` or `signingkey` reference exists in `playbooks/` or deployed `files/` templates.
  - ccy's `configure_git_signing` returns early when gpgsign is off (ssh-handling.bash:1259-1269).
  - The optional self-update play is not imported by main.
- **The test exercises the production path.** It reads the real vars file, parses the real play with ansible's own loader, lifts the gated block unchanged, and runs it under ansible-core 2.19.12. My run: `passed: 21 failed: 0`. The author's red log shows 0/8 against the old play. The perturbation log (gate forced true) fails exactly the two none cases, with the downstream error message. The pass line is printed via `qa_pass_line`.
- **Fail-fast.** No new `failed_when`/`ignore_errors`. The moved probe keeps its `FAIL-FAST-OK` probe-then-fail. The test drops `set -e` with a written reason (test:27-28), checks every result explicitly, and exits 2 on a missing tool or collection.
- **Placement.** This is an edit to the play that owns the concern, not a new play. The gate lives in the vars file both plays already load. Task naming is action-oriented.
- **Version bumps.** No CCY launcher/lib, image or run.bash files changed, so none are owed. The play keeps its shebang and mode 100755. The new script is 0755.
- **Plan and journal.** Updated in the same commit, with no README row needed. I verified the journal's claims:
  - 6001a540 removed five `when: git_signing_key is defined`.
  - vmtest defaults to none (vmtest:920, 1238).
  - cli-multi ends the play (:168-178).
  - The "Not handled" pre-existing-gpgsign case is disclosed honestly.
- **Heredoc Python and backticks in comments.** Both have precedent in 3 other `scripts/*.bash`. The file is not in the semgrep partial-parse list in qa-after.log, so the backticks don't break parsing.
- **Public-repo safety.** The diff, commit message and pending `untracked/scratch/pr-body.md` contain no IPs, hostnames, real usernames, private repo names or home paths. Placeholders are `<user>`, `example-user` and "a downstream deployment", all within ExampleValues.md's `<…>` convention.
- **Base.** `git rev-parse F44` equals `git ls-remote origin refs/heads/F44`, both b1628ab2. The base has not moved.

### Mechanical gates

- **qa-all.bash:** not rerun, per the caller. The authoritative result is CI run 36570113502 on head fb3b0203: conclusion `success`, `✓ toolchain: TOOLCHAIN-OK`, `✓ git-signing-declared: passed: 21`, `✓ ansible-syntax: 82 playbooks OK`. The local `qa-after.log` fails only on the known environmental stages (toolchain versions, shellcheck SC2218, bash-history-search, helper-counts-reader). The baseline's 15 ansible-syntax failures are gone because community.general was available.
- **plan-qa --sweep:** the nested repo ships no daemon binary, so I ran the parent's with `--project-root` pointing at fedora-desktop. Result: 0 block, 13 advise, none from this change (00139's advisory is on the 26-09-25 day file). `--lint` on 00139 PLAN.md: 0 findings.
- **syntax-check:** `scripts/qa-ansible-syntax.bash` with the scratch collections path gives `✓ 82 playbooks OK (79 under playbooks/imports/, 3 elsewhere)`, rc=0.
- **Conditional gates:** not triggered. There are no `helpers/`, `tests/helpers/` or `extensions/` changes, so qa-helper-tests, extension-compat and eslint are not required.
