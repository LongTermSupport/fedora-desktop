# QA Review: Plan 00138, confirming review of fix commit 01f4a90a

Saved by the coordinating session: the reviewer is read-only and returned this report
inline. Resolutions are recorded in the plan's JOURNAL.

**Verdict: FIX-BEFORE-MERGE**

- **FIX-BEFORE-MERGE:** the new logical-repo-root code puts unrelated directories into the
  repository tier. The common case is a stow-style dotfiles link, where the tier grows to
  all of `$HOME`.

## Blocker 1 (typed line in argv): resolved

- **The bind string is fixed text.** `\${…}` inside double quotes stays literal.
- **`READLINE_LINE` reaches fzf only as an environment prefix** on that one command, and no
  other argv route exists.
- **The query really filters.** A real fzf 0.74.4 under a Python pty, with the production
  flags, took its query only from `__history_search_query`. After Enter it printed
  `export TOKEN=` as the query and picked `export TOKEN=abc`.
- **The query command leaks nothing.** fzf's `$SHELL -c` child has fixed argv, and
  `printf` is a builtin.

## Fix before merge

1. **The symlink fix can put unrelated paths in the repository tier.**
   `bash-history-rank` strips git's `--show-prefix` off the logical path without checking
   that the result resolves to the repository.
   - A link INTO a repository, such as `~/.config/nvim -> dotfiles/.config/nvim`, ends in
     the same prefix, so `repo_logical` becomes the home directory. A newer command run in
     `home/unrelated` then ranked above the real repo command. This is exactly how GNU stow
     lays out dotfiles.
   - The repository top level, including an empty prefix, works.
   - Fix: accept `repo_logical` only when `(cd "$repo_logical" && pwd -P) == "$repo"`, and
     add the stow case as a negative test.

## Should fix

2. **The QA gate no longer proves the typed text becomes the query.** The stub checks that
   the text is in the environment and not in argv, but never that the
   `start:transform-query` bind is passed. If `--bind="${load_query}"` were deleted, the gate
   would stay green. Assert the exact bind argument.

## Nits

- **Check 11 also passes if the shell never ran.** Gate it on check 10's
  `in_history == "with timestamp"`.
- **Scratch files:** the reviewer's probes left untracked scratch files in the worktree; the
  coordinator should delete them.

## Checked and clean

- **deploy.bash lab leg:** correct.
  - The marker path matches the play's `vm_test_lab_dir`.
  - The play is opt-in.
  - `vmtest` hashes `guest-cleanup.bash` into the recipe digest, so bases rebuild on their
    own and no recipe-version bump is needed.
- **guest-cleanup:** covers all three files for `/root` and `/home/*`.
- **The `.bash_profile` assert:** the anchored form rejects a commented-out line.
- **e2e `env -u SSH_CONNECTION`:** matches the agent-prompt guard.
- **Docs and index:** current.
- **Public repo:** no install-specific names in the diff.

## Mechanical gates

- **qa-all.bash:** `bash-history-search` passes 21, and shellcheck is clean. The other
  failures are the known ones (toolchain/ruff, the self-update-check test, self-update-cycle,
  helper-counts-reader, and ansible-syntax without a vault file in a worktree).
- **plan-qa --sweep:** one block finding, `index-retention-window`. It already exists on F44
  and is not from this branch.
