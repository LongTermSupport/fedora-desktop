# qa-reviewer: 00109 T0.2 review fixes and login-noise fixes (05cbbd25, 5d366b3a), opus-5, 2026-09-24

Saved by the coordinator.

**Verdict: PASS WITH NITS.** The code is correct on every path the reviewer tested; the nits
are about plan or journal wording.

## Nits

1. **The plan misdescribed the SSH-agent failure.** The block never prompted: `read -p` shows
   a prompt only when input is a terminal. With stdin not a terminal it took the default yes
   and loaded keys unasked, printing on stdout.
2. **The fourth VM run was not yet journalled.** The coordinator adds the entry.
3. **"Fixed" is not yet shown by a run.** The VM item stays open until a fifth run, which
   follows the owner's pin decision.
4. **`play-displaylink.yml`:** if the paired paths ever differed, the task would silently
   keep the tree. That cannot happen and fails safe; an `assert` would be the fail-fast form.
5. **`docs/playbooks.md`** does not say the bashrc block now needs a terminal on stdin.

## Checked and clean

- **Title escape (`ps1-prompt`):** still written under a pty, both directly and from
  `PROMPT_COMMAND`, including in tmux and ptyxis. Nothing is written when stdout is captured.
- **SSH-agent block:**
  - **Interactive with a pty:** prompts on stderr; Enter loads keys, `n` loads nothing.
  - **The VM test's capture:** stdout is now empty.
  - **`ssh host cmd` and `ssh -T`:** skipped as before.
  - **A forwarded agent with keys:** skipped, unchanged.
  - **Passphrase prompts:** still reach the tty.
- **Ansible 2.19:** `blockinfile` content, no apostrophes or backticks in comments.
- **rpm stream fix:** `stdout ~ stderr` checked in both places; any other answer still fails.
- **Leaks:** none in the diff.

## Gates

`--syntax-check` passes on both changed plays. `qa-all` in the worktree fails only
`ansible-syntax`, for the missing vault file. `plan-qa --sweep` shows one block on the base
branch, not this one: `index-retention-window` on `CLAUDE/Plan/README.md`.
