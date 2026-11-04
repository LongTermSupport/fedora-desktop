# Fact check: Plan 00213 PLAN.md

The diff changes only Task 1.4's last sentence, from "the way the container entry points
already do" to "never through PATH". The rest was checked at the caller's request.

## REFUTED / qualified (most consequential first)

R1. Task 1.3 / Overview: "raise `refreshInterval` in the member kit settings". REFUTED for ccy.
`files/var/local/claude-yolo/Dockerfile` (comment at ~398-403) says the kit `settings.json` is
agent-bus-claude's (LXC, VM, host members), "not ccy's". ccy passes claude the session's own
settings written by the claim: `helpers/pingbus/seat.py:443-469` (`session_settings`) sets
`statusLine.refreshInterval = 1` for a seated session (line 463). Raising the kit file alone does
not touch ccy sessions; `seat.py` must change (the image is rebuilt, so the container-version
bump applies).

R2. Task 2.1: "The module already imports no other pingbus module on the hot path". Qualified.
`helpers/pingbus/statusline.py:308` (`read_roles`) imports `helpers.pingbus.config, inbox, protocol` whenever teams are shown, which is every bus session. Only a no-team render skips them
(docstring lines 36-39). Port scope includes the role read via `config.load_bundle` and
`inbox.cached_role`, not just the render.

R3. Task 1.4 premise (the removed text). The container entry points do not "name the system
interpreter directly": `helpers/pingbus/bundle.py:42` sets `SHEBANG = b"#!/usr/bin/env python3\n"`
for the zipapp, so every context resolves python3 through PATH. The diff drops this claim, so it
is no longer in the plan.

## Verified

- `settings.json` has `statusLine.command` = `pingbus status-line` and `refreshInterval: 1`
  (`files/opt/claude-yolo/optional/agent-bus/settings.json`).
- `plugin/pingbus/hooks/hooks.json` has exactly SessionStart, UserPromptSubmit, PostToolUse
  (matcher Bash, relay only), Stop and SessionEnd, each running `pingbus hook …`.
- `statusline.py` always exits 0 (docstring lines 24-28; `render` returns status "always 0",
  ~line 415), runs the inner command in its own process group with a timeout, and wraps team rows
  to COLUMNS.
- `helpers/pingbus` is 18 `.py` files (19 entries), 11,825 lines; the plan's "about 11.8k" holds.
- `src-go/cmd/` exists with 16 command dirs (cc-project-dir, ccy-sessions, ...).
- ccy version bump: `CLAUDE/ContainerRules.md:107-115` says files under `files/opt/claude-yolo/`
  and `helpers/pingbus/`, `helpers/agent_bus/` need the container version bump. Exception:
  `optional/agent-bus/README.*` and `agent-bus-claude` (never staged by the play). So `settings.json`,
  `hooks.json` and `plugin/` need the bump; `agent-bus-claude` and READMEs do not. Plan 1.3/2.3
  are right to say it.
- The host kit's `pingbus` is the zipapp with shebang `#!/usr/bin/env python3`
  (`bundle.py:42`), so it resolves through PATH (the pyenv shim on a pyenv host). The
  `agent-bus-claude` wrapper (`files/opt/claude-yolo/optional/agent-bus/agent-bus-claude`
  lines 85-88) puts the kit on PATH and runs `pingbus` by name. Plan playbooks use bare
  `python3` argv.

## Unverifiable here

All measured figures: 0.32 CPU-s, 0.16 CPU-s per pyenv-exec, about 2 renders/s, 0.5 thread,
14 busy threads, 9.4 short-lived, hooks-daemon issue #131, and "resolves to system anyway".
