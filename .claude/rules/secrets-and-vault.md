---
paths:
  - "environment/**"
  - "vault.bash"
  - "**/host_vars/**"
  - "**/group_vars/**"
---

# This is a PUBLIC repository — you are near the secrets

Only the reserved placeholders pass the scanner, the vault is variable-level and edited with a normal editor, and nothing scans a `gh` post or a web paste for you.

- [CLAUDE/SecurityRules.md](../../CLAUDE/SecurityRules.md) — never-commit list, `vault.bash set` versus `replace`, external posts are not scanned
- [CLAUDE/ExampleValues.md](../../CLAUDE/ExampleValues.md) — the reserved placeholder schema; private LAN ranges are not placeholders
- [CLAUDE/AgentNotes.md](../../CLAUDE/AgentNotes.md) — scrubbing identifiers before any external post
