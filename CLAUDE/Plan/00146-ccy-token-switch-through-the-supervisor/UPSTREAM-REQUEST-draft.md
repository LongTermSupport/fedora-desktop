# DRAFT: upstream feature request to claude-code-hooks-daemon

**Not filed.** Filing is the owner's call (Plan 00146 Task 1.1). If approved, the body is
generated and filed through `hooks-daemon issue-report`, never pasted into `gh issue create`
by hand: the tracker is public and an issue cannot be retracted. Before filing, re-check
the text below for anything specific to one install (container names, project names,
paths outside the container, account names): there must be none.

---

## Title

ccy supervisor: respawn `claude` with `--resume` and a changed credential, on request

## Problem

A downstream ccy launcher passes the Claude credential into the container as
`CLAUDE_CODE_OAUTH_TOKEN` at `podman run`. When that account hits its rate limit, the only
way to move the session to another account is to end the container and relaunch it with a
different token and `--resume <session-id>`. That works, but it also kills everything that
lives only in the container: an in-container ssh-agent (a passphrase re-prompt), the tmux
pane's state, and the launcher's preflight cost.

Claude Code reads `CLAUDE_CODE_OAUTH_TOKEN` once and memoises it; with the variable set it
refuses to adopt a different credential on 401, and a 429 runs no credential recovery at
all. So the credential cannot be changed inside a running `claude`. A container's own
environment cannot be changed after start either.

The supervisor is the one place that can do it: it starts `claude` itself (`pty.fork` then
`os.execvp`), so it decides the environment of every `claude` it spawns. Today it spawns
`claude` once, waits on it once, and returns its exit code.

## Proposal

A new request family, consumed at the existing idle choke point (idle and an empty input
box, subordinate to compact/continue, like the goal and operator-signal families):

1. **Request.** A request names a **credential file** and nothing else. It never carries the
   credential itself, so it can travel through the same signal channel as the other
   families. Suggested shape: a closed kind (`respawn-with-credential`) plus the path of the
   credential file, which must lie under a fixed, container-local directory the supervisor
   is configured with (not the project bind mount).
2. **Wait for idle.** The supervisor does nothing until the choke point is reached. It never
   interrupts a turn in progress.
3. **End the current `claude` cleanly.** Inject `/exit` and wait for the child to exit. Read
   the session id first, from Claude Code's `sessions/<pid>.json` for the child's pid. If the
   id cannot be read unambiguously, refuse the request loudly and leave the session running.
4. **Read and delete the credential file.** Refuse unless it is a regular file (opened with
   `O_NOFOLLOW`), owned by the supervisor's uid, mode `0600`, and within a small size bound.
   Accept only an allowlisted variable name (`CLAUDE_CODE_OAUTH_TOKEN` by default; the list
   configurable by the downstream project). Unlink the file immediately after reading it, and
   treat a failed unlink as a failed request.
5. **Respawn.** Fork a new `claude` on a new PTY with the original argv minus any
   `--continue`/`--resume`/`--fork-session`, plus `--resume <session-id>`, and the parent
   environment with the one variable replaced. Return to the normal supervision loop on the
   new child; the supervisor's own exit code is the exit code of the last child.
6. **Report.** One status-line message and one audit-log line naming the request kind, the
   session id and the outcome, never the credential value. On any failure after `/exit`
   (credential file invalid, respawn fails), say so loudly and exit with a non-zero code
   rather than leave a session with no `claude`.

## Security notes

- The credential never appears in argv, the audit log, the status line, or any file the
  supervisor writes.
- The credential file directory is container-local, so the value never lands on the host
  through the project bind mount.
- The request itself is validated the way `load_operator_signal` validates operator signals:
  a closed kind and one strictly checked path, never free text.

## Known limits (for the docs)

- The turn in progress is never lost, because the request waits for idle; the cost is that a
  long turn delays the switch.
- Background Bash tasks, MCP servers and sub-agents of the old `claude` end with it.
- The first turn on the new account re-reads the whole context with a cold prompt cache.
- Switching between two tokens of the same account does nothing for a rate limit, which is
  per account.

## Alternative if declined

The downstream project relaunches the whole container with the new token and
`--resume <session-id>`. That needs no supervisor change; it only costs the container-local
state listed above.
