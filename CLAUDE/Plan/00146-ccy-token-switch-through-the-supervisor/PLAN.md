# Plan 00146: ccy token switch through the supervisor

**Status**: Not Started (decision gate: the Phase 1 owner decisions, starting with whether
to file the upstream request in Task 1.1)
**Created**: 2026-10-02
**Owner**: joseph
**Priority**: Medium

## Overview

When a ccy session's Claude account hits its rate limit, the only way to move it to another
account today is to end the session and relaunch with a different token. Claude Code reads
`CLAUDE_CODE_OAUTH_TOKEN` once and keeps it, and a container's environment is fixed at
`podman run`, so neither the running `claude` nor the container can be given a new token.

The ccy supervisor (`.claude/ccy/claude-supervise.py`, daemon-owned, from the upstream
hooks-daemon project) starts `claude` itself with `pty.fork` + `os.execvp`, so it decides
the environment of every `claude` it spawns. The preferred design: on a "switch to token X"
request the supervisor waits until the session is idle, sends `/exit`, and respawns
`claude --resume <session-id>` with the new `CLAUDE_CODE_OAUTH_TOKEN`. The container keeps
running, and so do its ssh-agent and the tmux pane. That needs an upstream supervisor
feature, a safe way to hand a token into a running container, and a ccy-side command to pick
the token. If upstream declines, the fallback is the container relaunch the research
recommended (`claude-yolo` relaunches itself with `--token X -- --resume <id>`).

Research, evidence and the relaunch design:
[RESEARCH-token-hot-swap.md](RESEARCH-token-hot-swap.md) (its verdict is superseded by the
owner correction at its top). Upstream request text:
[UPSTREAM-REQUEST-draft.md](UPSTREAM-REQUEST-draft.md).

## Goals

- From inside a running ccy session, switch it to another token from the pool and carry on
  the same conversation, with the container, its ssh-agent and the tmux pane still running.
- The token never appears in argv, a log, the project tree, or any file that outlives the
  switch.
- After a switch, child claudes (`ccy-claude`), the session registry and the next launch all
  use the new token.
- If upstream declines the supervisor feature, the same command switches by relaunching the
  container instead.

## Non-Goals

- Changing the credential inside a running `claude` process (not possible; see the
  research, section 2).
- `/login` as a design. It stays a documented emergency escape hatch only.
- Editing `.claude/ccy/claude-supervise.py` in this repo. It is daemon-owned and replaced
  on upgrade; the supervisor side is an upstream change.
- The tmux F12 menu entry that triggers the switch. That is Plan 00147.

## Tasks

### Phase 1: Owner decisions

- [ ] 🚫 **Task 1.1**: Owner decision: file the upstream request. Options: (a) file
  [UPSTREAM-REQUEST-draft.md](UPSTREAM-REQUEST-draft.md) through `hooks-daemon issue-report`
  (never a hand-written `gh issue create`); (b) do not file, and build the container
  relaunch fallback (Phase 5) as the design. Recommendation: (a), because only the
  supervisor can respawn `claude` without losing the container. Blocked on the owner.
- [ ] 🚫 **Task 1.2**: Owner decision: where child claudes read the current token after a
  switch. Options: (a) `ccy-claude` walks up its own process ancestry to the top-level
  `claude` (the supervisor's child) and reads that process's `/proc/<pid>/environ`; (b) the
  supervisor keeps a 0600 copy of the current token in container tmpfs; (c) leave
  `ccy-claude` on `/proc/1/environ`, so children stay on the old account. Recommendation:
  (a), because it keeps `ccy-claude`'s rule that the token is never written to a file, and
  needs nothing upstream. Blocked on the owner.
- [ ] 🚫 **Task 1.3**: Owner decision: the stale `ccy-token` container label (labels are
  immutable). Options: (a) record the current token name in the session registry and a
  container-local state file, and change the readers of the label (podfreeze, `ccy-sessions`)
  to prefer it; (b) leave the label as "token at launch" and document it. Recommendation:
  (a); a label that names the wrong account misleads exactly when accounts matter. Blocked
  on the owner.

### Phase 2: Token delivery into a running container

- [ ] ⬜ **Task 2.1**: Choose the container-local directory for the one-shot token file. It
  must be tmpfs inside the container and NOT under `/workspace` (the project bind mount,
  which would put the token on the host disk). Confirm what the image mounts as tmpfs.
- [ ] ⬜ **Task 2.2**: Host-side helper that writes the token into that directory as a
  0600 file through `podman exec -i` on stdin (`umask 077`, write to a temporary name, then
  rename), never in argv or the environment of `podman exec`. Fails loudly if the container
  is not running or the write fails.
- [ ] ⬜ **Task 2.3**: Tests for the helper's argument handling and failure paths in the
  existing ccy test gates.

### Phase 3: The ccy-side switch command

- [ ] ⬜ **Task 3.1**: Factor the "choose and validate a token by name" logic out of
  `files/var/local/claude-yolo/claude-yolo` (the inline block around `:1134-1382`) into
  `lib/token-management.bash`, so launch and switch share it (format, length,
  `validate_token`, expiry).
- [ ] ⬜ **Task 3.2**: Add the switch command (working name `ccy --switch-token [--session <name>]`): resolve the session's container with `ccy_session_containers`,
  show the existing `select_token` menu with its usage view (`u`) and the current token
  marked, validate the choice, refuse a token of the same account as the current one with
  a clear message, deliver the token file (Phase 2), and drop the switch request for the
  supervisor. It follows `CLAUDE/InteractiveScripts.md` (bounded re-prompt, clean exit on
  EOF).
- [ ] ⬜ **Task 3.3**: Rewrite the session-registry record and `.last-launch.conf` to the
  new token name after a confirmed switch, so a reboot restore and the next quick launch
  use it (`ccy_registry_set_token`, beside `ccy_registry_forget_network`).
- [ ] ⬜ **Task 3.4**: Implement the Task 1.2 and 1.3 outcomes (`ccy-claude` token source;
  current-token record). Container-side changes follow `CLAUDE/ContainerRules.md`,
  including any container-version bump.
- [ ] ⬜ **Task 3.5**: `CCY_VERSION` bump, `docs/ccy-changelog.md` entry, `docs/ccy.md`
  Tokens section ("switching account mid-session", the `/login` escape hatch and its
  caveats), help text.

### Phase 4: Supervisor integration (needs the upstream feature)

- [ ] 🚫 **Task 4.1**: Wire the switch command to the upstream request format once the
  supervisor feature ships, and point the supervisor at the Task 2.1 directory. Blocked on
  Task 1.1 and on upstream.
- [ ] ⬜ **Task 4.2**: Tests: request file written with the right shape and no token in it;
  the switch refuses a session whose session id is ambiguous; nothing falls back to
  `--continue`.

### Phase 5: Fallback, only if upstream declines

- [ ] ⏸️ **Task 5.1**: Implement the container relaunch from the research (section 5):
  a host-side relaunch request holding only the token name and session id, which
  `claude-yolo` consumes after `podman run` returns and re-execs with
  `--token X -- --resume <id>`. On hold until Task 1.1 or upstream says no.

### Phase 6: Verification

- [ ] ⬜ **Task 6.1**: `deploy.bash` and `acceptance.bash` in this folder (host runs:
  `play-claude-yolo.yml`, then a switch in a live session checked for the same session id,
  the new account's usage moving, and no token file left behind).
- [ ] ⬜ **Task 6.2**: `./scripts/qa-all.bash` green; `qa-reviewer` agent over the full diff.
- [ ] 🚫 **Task 6.3**: **HOST**: run `deploy.bash` then `acceptance.bash`. Blocked on the
  owner: Ansible never runs in the ccy container.

## Risks

- **A turn in progress is lost** if `claude` ends mid-turn. The switch waits for idle and
  never kills a turn; a long turn delays the switch instead.
- **The first turn on the new account re-reads the whole context** with a cold prompt
  cache, a large one-off spend against that account's allowance.
- **Tokens of the same account do not help** with a rate limit, which is per account. The
  usage view exists to show which account has headroom.
- Background Bash tasks, MCP servers and sub-agents of the old `claude` end with it.
- The research's reading of Claude Code's credential handling comes from one release's
  minified internals. The design depends only on the documented `--resume <id>` and
  `CLAUDE_CODE_OAUTH_TOKEN`.

## Success Criteria

- [ ] A switch in a live session keeps the same session id and conversation, leaves the
  container and its ssh-agent running, and the next request is billed to the new account.
- [ ] No token value appears in argv, logs, the project tree, or a file left after the
  switch.
- [ ] `ccy-claude`, the session registry and the next launch use the new token.
- [ ] Every failure (no container, ambiguous session id, invalid token, delivery failure)
  stops with a named error and leaves the session on its old token.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00146-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- (none yet)
