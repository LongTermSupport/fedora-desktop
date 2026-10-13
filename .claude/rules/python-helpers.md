---
paths:
  - "helpers/**"
  - "tests/helpers/**"
  - "**/*.py"
---

# You are editing Python — helpers are stdlib-only and test-first

Stdlib only, test file before source file, explicit `check=` on every subprocess call, marker lines to stdout. Run the tests with the dedicated runner, never `unittest discover`.

- [helpers/CLAUDE.md](../../helpers/CLAUDE.md) — the rules in stone, structure and invocation, tests, fail fast
- [CLAUDE/StderrHygiene.md](../../CLAUDE/StderrHygiene.md) — marker lines are stdout; diagnostics are stderr
- [CLAUDE/QA.md](../../CLAUDE/QA.md) — `./scripts/qa-changed.bash` per commit, `./scripts/qa-all.bash` per push, the ruff ruleset and version pin, the suppression ban
