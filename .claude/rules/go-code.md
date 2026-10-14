---
paths:
  - "src-go/**"
  - "**/*.go"
---

# You are editing Go — test first, through the shared adapters, no discarded errors

Thin `cmd/` mains over `internal/` packages; every external command through `execx` (the nested `src-go/qa` module: through `qa/internal/proc`), every prompt through `ui`; a port is built to a spec of intended behaviour, test first, never to a replay of the bash; the play is the only install route.

- [src-go/CLAUDE.md](../../src-go/CLAUDE.md) — layout, rules, parity oracles, build and deploy
- [CLAUDE/QA.md](../../CLAUDE/QA.md) — `scripts/qa-go.bash`, and which suites `qa-changed.bash` runs for a `src-go/` change
