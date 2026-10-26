# Review round 1: plans 00183, 00177, 00167 (commit 1210f607e)

Reviewer: adversarial, read-only. Diff: `git diff F44...HEAD` (one commit).

**Verdict: FIX-FIRST.** Most of the factual claims hold. Two defects would make the host agent's
next step fail or report the wrong thing: the 00167 acceptance is not queued, and its `restore`
leg still needs a terminal the host agent will not have. The `cc` recipe in Task 3.3 also checks
the wrong tmux server.

## Claims checked, holding

- **meta-deploy capture `20261022-121154`**: `summary.txt` shows PASS for
  `play-git-configure-and-tools.yml` and for `00183-...-acceptance.bash`. The play's recap is
  `ok=27 changed=2 failed=0`. It ran the tasks "Check Out bash-git-prompt at Its Pinned Commit"
  and "Assert gitprompt.sh Calls async_run Only for Its Fetch". The acceptance log has all six
  legs OK (installed, marks fresh/3d/none/empty, failed rc=128, prompt, fetcher with
  `-F /dev/null -o BatchMode=yes`, override).
- **"at 67033c224"**: the capture does not record a commit. The F44 first-parent history makes
  67033c224 (11:38) the tip when the run started at 12:11. The next commit, ad6349cbd, is at
  12:23, and f2c59399a is an ancestor. c159e6626 drops exactly play-agent-bus, play-python,
  play-git-configure-and-tools and the 00183 acceptance. Consistent.
- **00167**:
  - The 10-21 journal records that the dddf4063 deploy passed and that the legs passed when
    re-run under a pty.
  - dead9482 is the qa-reviewer fix merge.
  - Task 2.4 reads "Passed at dddf4063".
  - In `files/var/local/claude-code/cc`, `set -e` is at line 17 and
    `CC_PROJECT_DIR=$(/var/local/claude-code/cc-project-dir)` is at line 42. The tmux
    insulation comes later, at line 67. So "the refusal ends it before tmux" holds, and the
    SC1 tick (no terminal) is sound.
- **00177**: the 10-13 21:03 entry ("First host run: seven fixes") and c2c9b4be4 (the Element
  round trip) exist as cited.
- **Offer legs, reproduced**: I built cc-project-dir with `-X main.RootDir=<this worktree>`.
  `offer-yes`, `offer-no`, `offer-retry` and `no-stdin` all pass with stdin `</dev/null`.
  - The legs keep stdout and stderr apart correctly: the redirects are inside the pty command,
    so the pty is only stdin.
  - The pattern is the one `scripts/test-terminal-modes.bash` uses, and that is a qa-all gate
    run on the host. `util-linux-script` is installed by `play-python.yml`, which passed at
    67033c224. Low flake risk.
  - False greens checked: printing the cwd, any-answer-yes, the path on stderr, no re-prompt,
    and a hang are all caught by the checks as written.
- **00183 Task 2.6 against bash-git-prompt 4501746** (`gitprompt.sh` fetched to scratch):
  - `checkUpstream` fetches at once when FETCH_HEAD is older than `GIT_PROMPT_FETCH_TIMEOUT`
    (5 min) and `git remote` is non-empty, so a FETCH_HEAD dated 3 days ago triggers the fetch
    on the first prompt.
  - The fetch is backgrounded through the overridden `async_run`, which writes
    `~/.cache/git-prompt-fetch.log` (the path is right; the play makes `~/.cache` 0700).
  - `GIT_PROMPT_START=$PS1` keeps the literal `$(yellow "$(git-fetch-age)")`, so the mark
    appears whichever of `ps1Prompt`/`setGitPrompt` sets PS1 last.
  - Hedging on either mark within 5 s is right, given the race with the truncating fetch.
  - After that, FETCH_HEAD is fresh and empty, so no refetch happens and the mark stays
    "failed or running".
- **No install-specific names** appear in the added lines.

## Findings

### FIX 1: the 00167 acceptance is not queued in meta-deploy.list (CLAUDE.md host-run rule)

Task 3.3 item 1 now says "`acceptance.bash` through meta-deploy", and the commit adds three host
legs. But `CLAUDE/Plan/meta-deploy.list` has no non-comment entries, and the commit does not touch
it. CLAUDE.md requires the run to be queued, with a description, in the same commit. Add
`00167-cc-refuses-to-start-outside-a-repo` (or its `acceptance.bash`), with a comment saying what
it does.

### FIX 2: the `restore` leg still needs a terminal from the caller, and the host agent has none

I ran it here with stdin `</dev/null` and got:
`line 93: /dev/tty: No such device or address`, then `FAIL: no terminal ...`, exit 1.

This is exactly how the 10-21 meta-deploy run failed. The plan's answer is "started from a
terminal", but the host agent is an agent, and its Bash has no controlling tty. The commit already
has the cure: `on_pty`. Run the restore case as
`CCY_SESSION_RESTORE=1` + `script -qec ... /dev/null` (no answers, or `y\n`), and require zero
offers and the refusal. Then every leg makes its own terminal, and the acceptance and
`acceptance.bash`/PLAN text stop saying "run it from a terminal". As it stands, item 1 of Task 3.3
will fail again for the very runner it is assigned to.

### FIX 3: the `cc` recipe in 00167 Task 3.3 item 2 observes the wrong tmux server, and inside tmux nothing new starts

`cc`'s sessions live on the dedicated socket `CCY_TMUX_SOCKET="ccy"`
(`files/var/local/claude-yolo/lib/tmux-session.bash:32`). Plain `tmux list-sessions` lists the
default server, so the "no new session" check after `n` passes vacuously.

Worse, "`cc` itself, in tmux" means `$TMUX` is set, and `ccy_tmux_insulate` does one of two
things:

- returns at once with "Already inside a tmux server ... not wrapping again", so no session
  "named for the checkout" ever starts;
- refuses outright, if that tmux server sits in a terminal tab's `*-spawn-*.scope`.

The recipe needs:

- the driving pane to run `cc` with `TMUX` unset (`env -u TMUX cc`), so it insulates;
- `tmux -L ccy list-sessions` before and after;
- a note that after `y` both the session offer (`ccy_tmux_offer`, when sessions exist) and the
  token chooser will prompt. Name what to answer.

### FIX 4: 00177 drift: tasks say "the owner", while the Status, journal and SC2 say "the host agent"

Task 6.4 still says "The owner tests it again on the host", and Task 7.5 "The owner tests Phase 7
on the host". The new Status line, the journal entry, README and SC2 ("met once the host agent's
Tasks 6.4 and 7.5 pass") say the host agent runs them. Pick one and make the tasks say it.

The journal's "which the host agent is running" also states, as present fact, something the
commit offers no evidence for.

### FIX 5: offer-yes does not prove the host's checkout is baked in, yet 00167 Task 3.3 item 1 claims it

The leg accepts any path that the offer names, that is its own git top, and that holds
`ansible.cfg`. A binary built from some other or old fedora-desktop clone passes. The plan text
says the leg proves "the installed binary, built with the host's checkout baked in". Either
compare `named` to the checkout acceptance.bash runs from (the play's `root_dir` is that
checkout's top), or soften the text.

### NIT

- **00183 Task 2.6 environment.** The bashrc's ssh-agent offer runs when
  `SSH_CONNECTION` is set and stdin is a tty. Its read defaults to yes on Enter, and "Press
  Enter after 10 s" would then load keys and could ask for a passphrase. Use
  `env -u SSH_AUTH_SOCK -u SSH_CONNECTION`, or better pass `-e`/the command to
  `tmux new-session`, because a running server's environment and `update-environment` decide
  what the pane gets. A plain `env -u` around the client only works through
  `update-environment`.
- **00183 Task 2.6 colour.** "The colour is in the escape codes, so no screenshot is
  needed": `tmux capture-pane -p` strips escapes. It needs `-e` to see the yellow.
- **00183 SC2.** It is ticked on component legs (`fetcher`, `override`) while the same line
  defers the real-shell proof to Task 2.6. Defensible, but "draws at once" in a real
  `bash -i` is exactly what 2.6 observes. Consider ticking after 2.6.
- **00183 Task 2.3 tick for qa-all.** The tick rests on a policy argument ("the push gate of
  every later F44 push") rather than a recorded run. Acceptable, but it is an inference.
  Task 2.3's text is also now a long history log (c4cab141, round 1, f2c59399a, 67033c224,
  c159e6626), duplicated in Delivery & Milestones. Trim it to the current state.
- **00183 ordering.** Task 2.6 sits between 2.3 and 2.4.
- **check-cc-project-dir.bash, `on_pty`.**
  - Any `script`/`timeout` failure (for example script missing, exit 127) is reported as "did
    not finish within 10 s". Check `command -v script` first, or report the status.
  - `script -c` runs the command with `$SHELL`. The `$?`/`%q` quoting assumes a POSIX/bash
    shell; set `SHELL=/bin/bash` for the call.
- **The six broken fakes** the 00167 journal cites are not kept, so the red proof cannot be
  re-run. That is fine for plan scaffolding, but the claim cannot be checked.
