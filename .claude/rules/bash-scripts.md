---
paths:
  - "**/*.bash"
  - "**/*.sh"
  - "scripts/**"
  - "files/home/.local/bin/**"
  - "files/usr/local/bin/**"
  - "vault.bash"
  - "run.bash"
---

# You are editing Bash — read these before you write

stdout is the captured payload and every error is surfaced, never hidden. Strict mode, quoting, `local`, no in-place edits, and the two measured traps are non-negotiable.

- [CLAUDE/AgentNotes.md](../../CLAUDE/AgentNotes.md) — the bash conventions the gates only partly enforce, and the discarded-failure-signal class
- [CLAUDE/StderrHygiene.md](../../CLAUDE/StderrHygiene.md) — stdout is the captured payload; all chatter goes to stderr
- [CLAUDE/InteractiveScripts.md](../../CLAUDE/InteractiveScripts.md) — human-prompting scripts validate strictly and re-prompt on a typo
- [CLAUDE/SecurityRules.md](../../CLAUDE/SecurityRules.md) — secrets are never passed in `argv`
- [CLAUDE/QA.md](../../CLAUDE/QA.md) — `./scripts/qa-all.bash` before every commit; suppression directives are banned
- [CLAUDE/DebugCommands.md](../../CLAUDE/DebugCommands.md) — diagnostic commands you hand the user must be non-interactive
