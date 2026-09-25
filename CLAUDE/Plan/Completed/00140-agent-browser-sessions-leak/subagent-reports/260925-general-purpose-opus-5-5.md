# Plan 00140: final report

The branch is done and pushed. Each browser command now allows one open session at a
time and refuses a second, so agents can no longer pile up Chromium. The host deploy and
the final in-container acceptance have not run yet.

**Plan and branch:** Plan 00140 (`CLAUDE/Plan/00140-agent-browser-sessions-leak/`, listed
under Active Plans). Branch `agent-aee0bf8cf9622a41d-d8997545`, five commits ending at
`69f51509`. Not merged into F44.

## Root cause (measured in-container, headless and lite only)

- **Every session name is its own daemon and its own Chromium**, about 14 processes
  each. Four names gave four Chromium roots.
- **Upstream's core skill, which our skill sends agents to first, says "Always use your
  own session"** via `export AGENT_BROWSER_SESSION=$(... session id ...)`. That export is
  gone by the next Bash call, so later calls land on `default` and start a second
  browser. The id also changes with the cwd and the prefix.
- **Bare `close` closes one session only.**
- **The 5-minute reaper works**, and upstream's v0.38.1 source applies it to headed
  browsers too. But each new session starts its own timer, so it cannot cap the count.
- **Found in review, and worse:** when a flag is repeated, the CLI takes the last copy.
  So `agent-browser-headless --headed true` would open a window on the desktop, and a
  caller's `--namespace` escaped any count. The Dockerfile comment claimed the opposite.

## What was built

A guard in the wrappers, not a Claude hook. ccy's settings are per-project, and a Stop
hook would block a headed window the user wants kept open.

- `files/var/local/claude-yolo/agent-browser-session-guard`. All three wrappers go
  through it.
  - **Refuses (exit 3)** a command that would start a session while
    `CCY_BROWSER_MAX_SESSIONS` (default 1) of that mode are open. The message names the
    open session and how to reuse or close it.
  - **Refuses (exit 2)** a caller's copy of a flag the wrapper sets.
  - **Never refuses** reuse of an open session or commands that start no browser
    (`close`, `session`, `skills`, `--help`).
  - It waits out the ~160 ms a just-closed session stays listed, and fails loudly if it
    cannot read the session list.
- The skill, `CCY-GUIDE.txt`, `docs/ccy.md` and `docs/playbooks.md` now tell agents to
  pass `--session <name>` on every command and close by name, not `close --all`.
- Plus: the play stages the guard, a changelog entry, the `CLAUDE/QA.md` row, and
  plan-local `acceptance.bash` and `deploy.bash`.

## Tests and QA

- `scripts/test-agent-browser-session-guard.bash` (51 cases, run in `qa-all.bash` as
  `ccy-browser-session-guard`) uses a stand-in binary that behaves like the real one. It
  failed 24 of 42 against today's pass-through wrappers, and 11 of 51 against the
  pre-review guard.
- Against the real binary in this container, `acceptance.bash --checkout` passed 16 of
  16, and nothing was left running afterwards.
- A plain `./scripts/qa-all.bash` exits 2 for worktree-only reasons:
  - `ansible-syntax` fails 82 of 82 because there is no `vault-pass.secret`;
  - `js` fails because there is no `extensions/node_modules`, and that failure stops
    every gate after it, this plan's included.
- With both bridged for one run (a dummy vault password file, and a symlink to the main
  checkout's `node_modules`, removed afterwards), everything passed. The advisory
  shellcheck warnings name none of this plan's files.

## Reviewer

- Round 1 was FIX-BEFORE-MERGE: the repeated-flag holes, and advice that would let
  parallel agents share or close each other's browsers. All fixed.
- Round 2 was PASS WITH NITS. The nits were wording and are fixed.
- Remaining advisory: `auth list`, `dashboard` and `mcp` can be refused while a session
  is open. That fails loudly; it does not leak.

## For the coordinator and owner

- **Version conflict:** F44 is at CCY 3.67.4 / container 2.37, so the branch takes
  **3.67.5 / container 2.38**. Renumber at merge if F44 has moved on. By semver this is
  a minor release; the patch number was kept as asked.
- **meta-deploy entry:** add `00140-agent-browser-sessions-leak` to `PLANS=(...)`. Its
  `deploy.bash` runs `play-claude-yolo.yml`, which rebuilds the image, so it can share
  that play with the coordinator's own ccy bump.
- **Final acceptance:** run on the host, `acceptance.bash` reports COULD NOT ESTABLISH
  (exit 2). It has to be run inside a new ccy session after the rebuild:
  `./CLAUDE/Plan/00140-agent-browser-sessions-leak/acceptance.bash`, expecting 20 of 20.
- **Owner's choice:** a project that needs two browsers at once (a two-user flow, say)
  sets `CCY_BROWSER_MAX_SESSIONS` in its `ccy.env`.
- No recovery cron was created; that belongs to the coordinator's session.
