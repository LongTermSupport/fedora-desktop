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

Pointers, not copies. Each linked document is the single source of truth.

| Read this                                                          | For                                                                 |
| ------------------------------------------------------------------ | ------------------------------------------------------------------- |
| [CLAUDE/StderrHygiene.md](../../CLAUDE/StderrHygiene.md)           | stdout is the **captured payload**; all chatter goes to `>&2`       |
| [CLAUDE/InteractiveScripts.md](../../CLAUDE/InteractiveScripts.md) | Human-prompting scripts: validate strictly, **re-prompt** on a typo |
| [CLAUDE/QA.md](../../CLAUDE/QA.md)                                 | `./scripts/qa-all.bash` before every commit                         |
| [CLAUDE/DebugCommands.md](../../CLAUDE/DebugCommands.md)           | Diagnostic commands you hand the user must be non-interactive       |

## Non-negotiable

- **`set -euo pipefail`** in every executable script. Dropping `set -e` needs a written reason.
- **Errors are never hidden.** No `2>/dev/null`, no `|| true`. Capture into a variable and
  report the reason — `2>&1` *into a capture* is collecting the error, which is the opposite.
- **Quote every expansion.** `local` every function variable; an unlocalised assignment is a global.
- **Secrets are never in `argv`** — `/proc` is world-readable. Pass by env name or stdin.
- **Never modify a file in place.** `sed -i` is banned repo-wide; write via `mktemp` + `mv`,
  and **verify the rewrite took** before installing it (an `awk` that matches nothing exits 0
  and emits a perfect copy).

## Two measured traps

- **`bash -n` can print a syntax error and still exit 0.** Assert stderr is *empty* as well
  as the exit code; `shellcheck` is the syntax authority.
- **`grep` has no `\t`.** POSIX BRE/ERE define no character escapes, so `\t` matches a
  literal `t` and the pattern silently matches nothing. Use `awk -F'\t'` and match the field.

Verification before commit: `shellcheck -x <file>` (suppression directives are banned),
`bash -n <file>`, then `./scripts/qa-all.bash`.
