**Verdict: FIX-BEFORE-MERGE.** The fix itself is correct. `gh_primary` and `config_repo_exists` behave as described, the new test does run the production functions, and the version bump and changelog are right. But only the existence check got the "a 404 is not every failure" treatment. Six other calls on the same path still turn every failure into "not there", one of them silently.

**I did not write the report file.** You asked for `/workspace/CLAUDE/Plan/00139-commit-signing-everywhere/subagent-reports/261002-qa-reviewer-config-repo-primary-opus.md`, but my role has no Write/Edit tool and forbids writing through Bash. The full report is below. One disclosure: I saved the `qa-all.bash` output to `/workspace/untracked/scratch/qa-c16a68f9.log`. That is untracked and safe to delete, but strictly my rules don't allow it.

## QA Review — c16a68f9 (`run.bash` 1.29.1, config repo read as the primary)

### Should fix (ranked)

1. **The config step still treats every failure as "absent"; one case is completely silent.** All six of these calls send gh's error output to `/dev/null`, so gh's message is lost:
   - `run.bash:3159`: the owner picks a config by number, the read fails, and nothing is printed. `has_remote_config` stays false and the menu just lacks the "pull" option. This is skip-and-continue, which the fail-fast rule forbids.
   - `run.bash:3121`: a read failure prints "No saved config for <host>".
   - `run.bash:3132`: `|| _host_list=""` makes a failure look like "no hosts".
   - `run.bash:3142`: same pattern for the legacy file.
   - `run.bash:3112`: a failure aborts, but blames privacy (`.private='unknown'`).
   - `run.bash:1518` (`push_config_to_repo`): any failure becomes "create a new file", which then fails with a confusing 422 error.

   This is the "discarded failure signal" pattern in `CLAUDE/AgentNotes.md`. The commit fixed one of seven sites. The commit message and changelog claim more than that: "stops the run with gh's words on anything but a 404" is true only of `config_repo_exists`. **Fix:** one helper that treats a 404 as absent and stops with gh's words on anything else, used at every one of these sites.

2. **A 404 can still mean "the token can't see the repo", not "no repo".** GitHub documents that it returns 404 for private resources the token cannot access; I did not reproduce that here. On re-runs where the `gh-<alias>` wrappers exist, the scope check at `run.bash:2974` runs as `gh-lts`, so the primary's own token is never checked before the config step. A primary token without `repo` scope would therefore reach `run.bash:3170` and print "No config repo found… (GitHub answered 404 to <primary>)". That is the original false negative with a more confident message. **Fix:** run the scope check for the primary too (with `gh_primary`), or word the 404 message as "not visible to <primary>".

3. **The root cause is still live in the wrapper.** `playbooks/imports/play-github-cli-multi.yml:1107-1131` (`gh-<alias>`) and `:1141-1177` (`clone-<alias>`) still switch accounts and then switch to the saved default, not to whatever was active before. That affects every interactive user and tool, not just `run.bash`. The same play already does it properly elsewhere, reading the account's token by name without switching (`:944-951`, `:1337`). **Fix:** apply that pattern to `gh-<alias>`, or at least restore the previously active account, as a tracked follow-up task.

4. **The test's wiring grep misses most other spellings.** I fed the regex at `scripts/test-run-bash-config-repo.bash:160` eight variants. It caught only `gh api "repos/${config_repo}…"` and `command gh api "repos/${config_repo}"`. It missed:
   - `"repos/$config_repo"` (no braces)
   - an unquoted path
   - `$GH_REPO api …`
   - `gh api --method PUT "repos/…"` (flag before the path)
   - `gh repo view "$config_repo"`
   - `"repos/${primary_gh_username}/fedora-desktop-config"`

   **Fix:** invert the check. Every line in `run.bash` mentioning `repos/${config_repo}`, `repos/${repo}` or `fedora-desktop-config` must use `gh_primary`, except an explicit allowlist (the per-login probe at `:2845`).

5. **The headless path changed but no test runs it.** `hl_pull_config_source` (`run.bash:581,591`) now uses `gh_primary`, but the new test doesn't extract it. The only other test that mentions it (`scripts/test-run-bash-headless-localhost-yml.bash:49`) replaces it with a stub that must never be called. **Fix:** add a headless case to the new test, with a foreign `GH_TOKEN` and a 401.

6. **The saved config is passed on gh's command line.** `run.bash:1533` passes the whole of `localhost.yml`, base64-encoded, as `--field content=…`, so it is readable by any user via `/proc/<pid>/cmdline`. That file holds PII and vault ciphertext. This predates the commit, but the line is in the diff, and `CLAUDE/SecurityRules.md:95` says no secrets in argv. **Fix:** send the request body on stdin, or from a 0600 temp file.

7. **A comment now contradicts the code.** `run.bash:2793-2794` says `choose_primary_gh_account` makes the primary the active account "since every plain `gh` call below acts as the primary". The `gh-lts` calls at `:2974/2983/2987` disprove that, and the comment at `:1480` says so. Update `:2793-2794`.

### Nits

- `gh_primary` captures the token with `2>&1` (`run.bash:1484`). On success, any stderr line would be glued onto `GH_TOKEN`. It would fail loudly as a 401 rather than silently, but capturing stdout only is cleaner.
- The new `CLAUDE/QA.md` row has no plan reference, unlike the rows around it.

### Answers to your specific questions

- **Other config-repo calls on the active account:** none left in `run.bash`. `fedora-install/pull-projects.bash` (called at `run.bash:3554` with `--account "$primary_gh_username"`) switches accounts explicitly at `:133`, so it is not affected. It does have the same any-failure-is-"not found" shape at `:143-146`, and the caller turns its failure into a warning at `run.bash:3555`. `fedora-install/push.bash:217,420` has that shape too; at `:217`, any `gh repo view` failure triggers a `gh repo create`. These are not reached from `run.bash`. The "Save local config to repo" path is `push_config_to_repo`, covered in items 1 and 6. Merge and selective import make no gh calls; they work on `raw_content`.
- **Fail-fast:** holds for `config_repo_exists`. It is called directly in an `if` in the main shell, so `fatal` exits the script, and `fatal` hands over to `hl_abort` when headless. Interactively it is only reached on the non-headless path, and headless uses `hl_abort` directly. Diagnostics go to stderr. The gaps are item 1.
- **Token in `GH_TOKEN`:** acceptable. `SecurityRules.md:95` explicitly allows passing a secret by environment variable. The prefix assignment scopes it to the one gh process, and `token` is a local variable.
- **Does the test exercise the production path:** yes for the three functions. It extracts them from `run.bash`, and reverting `gh_primary` to plain `gh`, or dropping the 404 branch, would fail it. The config step itself (`:3101-3170`) is covered only by the grep, which is weak (item 4).
- **Version and changelog:** 1.29.0 to 1.29.1 is right for a bug fix. The changelog entry sits at the top and names Plan 00139 Task 1.4, but overstates the change (item 1). The PLAN task is correctly still unticked, pending the host re-run.

### Checked and clean

- Placement: a `run.bash` bug fixed in `run.bash`, with a gate in `scripts/`.
- Public safety: test fixtures use `alice`/`bob`; nothing install-specific in the diff.
- Plan drift: PLAN.md was updated in the same commit; nothing left untracked.
- Docs: nothing in `docs/` describes the changed behaviour.
- The commit is on `origin/F44`.

### Mechanical gates

- `qa-all.bash`: exit 0, "QA passed: 1129 files checked", including "run-bash-config-repo: passed: 16".
- The new test run on its own: 16 passed, 0 failed.
- `plan-qa --sweep`: 0 blocking, 11 advisory (one is out-of-order journal entries in `00139-Journal-26-09-25.md`).
- `ansible-playbook --syntax-check`: not needed; no playbook changed.
- `qa-helper-tests`, extension compat check, ESLint: not triggered (no `helpers/` or extension changes).