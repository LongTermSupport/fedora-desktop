---
paths:
  - "scripts/qa-*.bash"
  - "scripts/qa-gates.tsv"
  - "scripts/qa-routing.tsv"
  - "scripts/qa-trees.tsv"
  - "src-go/qa/all/**"
  - "src-go/qa/gates/**"
  - "src-go/qa/changed/**"
  - "src-go/qa/docs/**"
  - "src-go/qa/cmd/qa-docs/**"
  - "ruff.toml"
  - ".qa-versions"
  - ".semgrep/**"
  - ".github/workflows/**"
---

# You are editing a QA gate — these guard every commit

A gate bug is worse than a code bug because it is silent: the gate returns a confident exit code either way. Prove every change with a control fixture that could have failed.

- [CLAUDE/QA.md](../../CLAUDE/QA.md) — what each gate runs, "Changing a Gate" for the two failure modes this repo has hit, and the ruff ruleset and version pin
- [CLAUDE/AgentNotes.md](../../CLAUDE/AgentNotes.md) — the discarded-failure-signal and partial-result classes that gates are the highest-value place to find
