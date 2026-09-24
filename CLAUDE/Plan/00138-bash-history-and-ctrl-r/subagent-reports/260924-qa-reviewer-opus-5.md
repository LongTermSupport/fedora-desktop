# QA Review: Plan 00138 (branch `worktree-plan-00138` vs `F44`, commit f3ec93cf)

Saved by the coordinating session: the reviewer is read-only and returned this report
inline. Resolutions are recorded in the plan's JOURNAL.

**Verdict: BLOCK**

## Blocking

1. **Ctrl+R puts the typed command line into fzf's argv.**
   `files/home/bashrc-includes/history-search.bash:66` passes `--query="${READLINE_LINE}"`.
   The host mounts `/proc` without `hidepid`, so any local UID can read the half-typed line
   (e.g. `export TOKEN=…`) while fzf is open. This violates PLAN.md's security goal and is
   the same exposure used to reject Atuin (RESEARCH-ranking-and-security.md §4.3). Fix: pass
   the query through the environment, e.g.
   `__hs_q="$READLINE_LINE" fzf … --bind 'start:transform-query:printf %s "$__hs_q"'`, and
   verify on fzf 0.74. The test pins the leak: `scripts/test-bash-history-search.bash:199`
   asserts `--query=typed` is in fzf's args.

## Fix before merge

2. **The repository tier is lost through a symlinked path.** `bash-history-rank` takes git's
   physical `--show-toplevel`, but the recorder stores the logical `$PWD`, so the comparison
   never matches. Reproduced: a repo reached via a symlinked parent ranked an unrelated newer
   command above the repo's own.
3. **VM base-image cleanup misses the new history files.**
   `files/home/.local/share/vmtest/guest-cleanup.bash` removes only `~/.bash_history`; the
   play now seeds `~/.local/state/bash/history` and the recorder writes `context`, so base
   images would keep the build transcript. Add both, for root and `/home/*`.
4. **Plan and docs are out of date.** `CLAUDE/Plan/README.md` and `PLAN.md` Delivery still
   say "implementation not started"; root `README.md` still says "Enhanced history (20K
   lines)".
5. **Acceptance does not meet its own success criterion.** Two goals are neither checked nor
   named NOT ESTABLISHABLE: "no command line in another process's argv" and "leading-space
   escape hatch".

## Nits

- The tweaks comment says `bash --norc` and `sudo -E` truncate to 500 lines; both inherit
  the exported `HISTFILESIZE=-1` (confirmed `-1` in a `--norc` child).
- The `.bash_profile` assert regex also matches a commented-out `# . ~/.bashrc`.
- A test label claims more than it proves: `probe_status` is element 0; the real proof of
  the status is the `1|A|false` record.
- `histverify` changes how `!!` behaves and is not in docs/configuration.md.
- The recorder forks a subshell for `$(history 1)` at every prompt; bash 5.3's `${ …; }`
  form would avoid it.
- With ignoredups, a command that failed and then succeeded on an immediate retry is still
  ranked as "always failed".
- Acceptance check 10 feeds the marker to `bash -i` on stdin; over SSH with no agent, the
  SSH-agent prompt block reads that stdin and the marker is lost.

## Checked and clean

- **Recorder edge cases:**
  - a multi-line `for` loop with lithist is recorded once, newlines intact;
  - the first prompt only notes where history stands;
  - an empty history gives number 0, so the first real command is recorded;
  - re-sourcing keeps the state;
  - leading-space and HISTIGNORE commands are kept out;
  - bash 5.3 gives each `PROMPT_COMMAND` element the command's own `$?`.
- **Non-interactive shells:** nothing on stdout; warnings go to stderr and only in
  interactive shells; early return for non-interactive shells, root, or a directory that
  is not the user's.
- **IaC placement:** edits to the owning play, no new play.
  - Parents are created as the account; the directory is 0700 and the seed 0600 with
    `force: false`.
  - The `.bash_profile` assert runs before the removal, for both accounts.
  - fzf and gawk are declared.
- **ps1-prompt:** appended once. bash-git-prompt, `/etc/bashrc` and `vte.sh` all tolerate an
  array `PROMPT_COMMAND`.
- **Acceptance exercises the deployed path:** the real `~/.bashrc`, `cmp` of the deployed
  files, and a COVERAGE line.
- **Public repo:** no usernames, home paths, hosts or IPs in the diff.
- **CCY:** no CCY files touched, so no version bump is needed.
- **CI:** the new gate passes 19 in CI, so the runner has gawk.

## Mechanical gates

- **`qa-all.bash` in the worktree:** the new gate passes. The other failures were already
  known:
  - ruff pin mismatch;
  - the self-update-check test;
  - self-update-cycle;
  - helper-counts-reader;
  - `ansible-syntax`, from the missing vault password file in a worktree.
- **CI on the same commit:** 0 errors; only the known failures remain.
- **Other checks:**
  - `syntax-check` on `play-basic-configs.yml`: rc 0;
  - `shellcheck -x` on every changed bash file: rc 0;
  - `plan-qa --sweep`: nothing blocking for 00138.
