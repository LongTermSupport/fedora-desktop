# Plan 00146 fallback: switch by respawning `claude` through the supervisor

This was the plan's primary design before the forwarder. It applies **only if the Phase 0
forwarder spike fails** (PLAN.md Task 0.3). Nothing here is scheduled; if the spike fails,
these tasks are moved back into PLAN.md as the plan's phases. Evidence and the container
relaunch design (b1): [RESEARCH-token-hot-swap.md](RESEARCH-token-hot-swap.md).

## Design

On a "switch to token X" request the supervisor waits until the session is idle, sends
`/exit`, and respawns `claude --resume <session-id>` with the new `CLAUDE_CODE_OAUTH_TOKEN`.
The container keeps running, and so do its ssh-agent and the tmux pane. That needs a
supervisor feature, a safe way to hand a token into a running container, and a ccy-side
command to pick the token. If the supervisor side cannot be had, the last resort is the
container relaunch (`claude-yolo` relaunches itself with `--token X -- --resume <id>`).

The supervisor (`.claude/ccy/claude-supervise.py`) is daemon-owned. The generic plugin API
the owner asked upstream for exists (hooks-daemon issue #71, Plan 00487, merged to
hooks-daemon main, not yet released when last checked), and the ccy lifecycle plugin
(`files/var/local/claude-yolo/supervisor-plugins/ccy_lifecycle.py`) already uses it to exit
for a restart at an idle point. Do not request the API again. Drafts kept for the record:
[UPSTREAM-REQUEST-supervisor-plugins-draft.md](UPSTREAM-REQUEST-supervisor-plugins-draft.md),
[UPSTREAM-REQUEST-draft.md](UPSTREAM-REQUEST-draft.md),
[subagent-reports/261002-supervisor-plugin-research-opus.md](subagent-reports/261002-supervisor-plugin-research-opus.md).

## Owner decisions it would need

1. Where child claudes read the current token after a switch: (a) `ccy-claude` walks up to
   the top-level `claude` and reads its `/proc/<pid>/environ`; (b) the supervisor keeps a
   0600 copy in container tmpfs; (c) children stay on the old account. Recommendation: (a).
2. The stale `ccy-token` container label (labels are immutable): (a) record the current
   token name in the session registry and a container-local state file and make podfreeze
   and `ccy-sessions` prefer it; (b) document the label as "token at launch".
   Recommendation: (a).

## Tasks it would add

- **Token delivery**: a container tmpfs directory not under `/workspace`; a host helper that
  writes a one-shot 0600 token file through `podman exec -i` on stdin (umask 077, temporary
  name, rename), never argv or env; tests of its failure paths.
- **Switch command**: factor "choose and validate a token by name" out of `claude-yolo` into
  `lib/token-management.bash`; `ccy --switch-token [--session <name>]` resolves the session's
  container, shows `select_token` with usage (`u`), refuses a token of the current account,
  delivers the token file, drops the switch request for the supervisor plugin; rewrites the
  session-registry record and `.last-launch.conf` (`ccy_registry_set_token`).
- **Supervisor integration**: the plugin consumes the request at an idle point, exits for a
  restart and respawns with `--resume <id>`; tests that the request carries no token, that an
  ambiguous session id is refused, and that nothing falls back to `--continue`.
- **Container relaunch (b1)** only if the in-container respawn cannot be had.

## Its risks

- A turn in progress is lost if `claude` ends mid-turn; the switch waits for idle.
- Background Bash tasks, MCP servers and sub-agents of the old `claude` end with it.
- The real token sits in the container's environment, as it does today.
