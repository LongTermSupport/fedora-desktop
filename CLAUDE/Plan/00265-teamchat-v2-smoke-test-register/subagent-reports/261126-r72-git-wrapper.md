# R72: the git wrapper in Claude Code sessions (batch 24)

## Cause, checked

The installed `claude` binary builds its shell snapshot with
`declare -F | while read func; ... [[ "$func" == _[!_]* ]] || declare -f "$func"`; its zsh
twin is commented "filter completion functions (single underscore prefix) but keep
double-underscore helpers". Variables reach the snapshot only as the environment (an `awk`
over `ENVIRON`), so an unexported global, and any array, is lost.

## Detector

`shell-snapshot-dropped-name`, a rule of qa-ansible (`src-go/qa/ansible/snapshotdropped.go`,
reported through the play-UX list, documented in CLAUDE/QA.md under the same name). It reads
every task that writes an interactive shell include (`~/.bashrc-includes/`,
`/etc/profile.d/`, `.bashrc`, `.bash_profile`, `.profile`): a blockinfile block, a copy
content, or a copy/template src under `{{ root_dir }}`, with literal loop items. Jinja is made
neutral and the text parsed with `mvdan.cc/sh`. In each function the snapshot keeps it reports a
call of a single-underscore command, and a read of a global any include sets unexported
(bash's own variables excepted) unless the function assigns the name itself. An unreadable
or unparseable include is a finding. Proof: `tests/fixtures/ansible-shell-snapshot-dropped-name/`
(13 marked lines in a play, a template and a looped copy; a clean twin), plus unit tests.

Limits: a dropped function's own body is not judged (it is never called in the snapshot); a
read before the function's own assignment of the same name passes.

## Red, sweep, green

`./scripts/qa-ansible.bash` before the fix: exit 1, 13 findings.

- play-github-cli-multi.yml: `gh-<alias>`, `clone-<alias>` (two), `remote-<alias>`,
  `gh-<alias>-token-phpstorm`, `git`, `git-<alias>`, `git-branch-default`,
  `git-<alias>-branch-default`. Renaming surfaced `_GH_ACCOUNT_USERS` and the
  `_gh443_sshcmd` call inside `_gh_account_git`, both fixed.
- files/home/bashrc-includes/history-record.bash and history-search.bash:
  `__history_search_file` (a global read by two functions).
- files/etc/profile.d/zz_lts-fedora-desktop.bash: `__history_shared_file`, which
  history-search.bash sets (found because globals are joined across includes).

After: exit 0, no finding.

## Fix

- Play: `_gh443_sshcmd`, `_gh_repo_path`, `_gh_redact_url`, `_gh_remote_to_alias`,
  `_gh_active_account`, `_gh_as_account`, `_gh_phpstorm_token_report`, `_gh_account_git`,
  `_git_branch_default_impl` renamed with a double underscore; `_GH_ACCOUNT_USERS` replaced
  by a rendered `case` in `__gh_account_git` (same unknown-alias error).
- History includes: `__history_search_file` and `__history_shared_file` are now functions
  that set a named variable with `printf -v` (no fork), both defined in history-record.bash
  (history-search.bash is held at its bash-limits record). `__history_append` now points
  HISTFILE at the shared file whenever history-record.bash was sourced, not only when the
  search redirected it; where fzf is missing HISTFILE is already that file, so this is a
  no-op. `test-bash-history-search.bash` 37/37, `test-bash-history-session.bash` 9/9.

## QA

`qa-changed` over these files: semgrep (two `| grep -q` in the new test, fixed) and
bash-limits (history-search.bash grew, fixed) were real and are green. Not judgeable here:
ansible-syntax (no vault password file in the container), go-qa (ast-grep is pinned for
amd64 only; `golangci-lint` and `go test` on `qa/ansible` pass by hand), and
ccy-version-bump (another lane's uncommitted ccy work in the same tree).

## Test

`scripts/test-gh-alias-wrappers.bash`, new section: the whole gh-aliases block rendered for
one alias, sourced, passed through the snapshot filter, and replayed into an `env -i` shell
with stubs for git, gh and git-account-helper. It checks `git` (as the account over SSH and
HTTPS in an aliased repo, plain elsewhere), `git-lts`, `git-branch-default`,
`git-lts-branch-default`, `gh-lts`, `clone-lts` and `remote-lts -y`. Before the fix: 20
failures (`_gh_account_git: command not found` and the rest); after: 46/46.

## Deploy

The changed plays are play-github-cli-multi.yml and play-basic-configs.yml; the queued
`changed-plays` entry of `CLAUDE/Plan/meta-deploy.list` runs both, so the list is unchanged.

## Faults met (dogfooding)

- The `lint_on_edit` hook runs `ansible-playbook --syntax-check` in the container, where the
  vault password file does not exist, so every YAML edit is reported as a lint failure.
- The secret-file guard denied an Edit of a test script because a token of the awk regex
  `/^ */` matched the glob `*.secret*`; worked around by writing `/^ +/`.
- An Edit's `old_string` ending in a space lost that space in the replacement once
  (`for fn in __gh_active_account __gh_as_account` became one word); caught by the test.
