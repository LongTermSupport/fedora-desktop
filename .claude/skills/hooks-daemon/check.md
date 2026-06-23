# Hooks Daemon Environment Check

Run a verbose, on-demand audit of the Claude Code environment and daemon
configuration.

## Usage

```bash
/hooks-daemon check
```

## What It Reports

SessionStart is deliberately quiet — it only surfaces things that need action
(a daemon update, a missing `.gitignore` entry). `check` is where you get the
full picture on demand:

- **Claude Code configuration** — each optimal setting with its current value,
  and for anything sub-optimal: why it matters, how to fix it, where to set it,
  and a docs link. Covers:
  - Agent Teams (`CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS`)
  - Effort Level (`effortLevel` / `CLAUDE_CODE_EFFORT_LEVEL`)
  - Extended Thinking (`alwaysThinkingEnabled`)
  - Max Output Tokens (`CLAUDE_CODE_MAX_OUTPUT_TOKENS`)
  - Auto Memory (`CLAUDE_CODE_DISABLE_AUTO_MEMORY`)
  - Bash Working Directory (`CLAUDE_BASH_MAINTAIN_PROJECT_WORKING_DIR`)
- **Container runtime** — docker / podman / lxc / generic, or desktop/host.
  (Also shown continuously by the status-line environment icon.)
- **Git `core.fileMode`** — warns when `false` (hooks can lose their exec bit).
- **Hook registration** — drift between `settings.json` and
  `settings.local.json`.

## Example Output

```
Hooks Daemon — Environment Check

Claude Code configuration: 4/6 optimal
  [OK  ] Agent Teams: CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS='1'
  [OK  ] Effort Level: effortLevel='high'
  [OK  ] Extended Thinking: alwaysThinkingEnabled=True
  [MISS] Max Output Tokens: Not set (default: 32000)
         Why:   ...
         Fix:   Set environment variable: export CLAUDE_CODE_MAX_OUTPUT_TOKENS="64000"
         Where: ~/.bashrc, ~/.zshrc, or settings.json env section
         Docs:  https://code.claude.com/docs/en/settings
  [OK  ] Auto Memory: Enabled (default)
  [MISS] Bash Working Directory: Not set
         ...

Container runtime:
  In a podman container

Git core.fileMode:
  true (OK)

Hook registration:
  OK — all hooks registered correctly in settings.json
```

## Notes

- The audit is **advisory** — it always exits `0` and never blocks.
- It reuses the SessionStart handlers' own check logic, so the report and the
  (quiet) session-start advisories stay in sync — a single source of truth.
- Equivalent direct call: `$PYTHON -m claude_code_hooks_daemon.daemon.cli check`.
