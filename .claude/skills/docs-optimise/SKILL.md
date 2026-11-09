---
name: docs-optimise
description: Propose a restructure of this project's agent instruction files (CLAUDE.md, @-imports, .claude/rules/, nested CLAUDE.md) so less loads at session start — one canonical home per fact, progressive disclosure. Proposal only, never edits tracked files. Trigger on "optimise CLAUDE.md", "my CLAUDE.md is too big", "shrink the always-loaded context", "restructure the instruction files".
argument-hint: "[file or directory to focus on]"
disable-model-invocation: false
user-invocable: true
allowed-tools: Bash, Read, Grep, Glob, Write
---

# Documentation Optimise

This skill is a **shim**. The classification rules, the report layout and the
output paths are in [references/procedure.md](references/procedure.md); the
rules it applies are cited, not restated:

- how Claude Code loads instruction files: `contracts/claude-code-instructions/`
  in the daemon checkout (`META.json` names the audited Claude Code version)
- the daemon's documentation ruleset (R6, R7a, R7d):
  `CLAUDE/DocumentationStrategy.md`

## What to do

1. Measure first: run `.claude/skills/docs-optimise/scripts/footprint.sh`
   (it wraps `docs-qa --footprint --json`). It lists what loads at session
   start versus on demand, with line and byte totals.
2. Follow [references/procedure.md](references/procedure.md) to classify each
   always-loaded section and write the proposal.
3. Tell the user where the proposal is and how to apply the patch.

```claude-code
/docs-optimise                 # the whole instruction-file set
/docs-optimise CLAUDE.md       # focus on one file
```

## Contract

- **Proposal only.** Everything is written under
  `untracked/scratch/docs-optimise/<run-id>/`. It never edits a tracked file
  and never adds an allowlist entry.
- It never proposes edits to the daemon-injected `<hooksdaemon>` block,
  `CLAUDE.local.md` or files under `~/.claude/`; they are counted, not moved.
- It owns placement and footprint only. Its report ends by naming `/docs-qa`
  (re-verify SSoT on the touched files after the patch is applied) and
  `/doctor prompt-audit` (outdated, missing-reference or contradictory
  content); neither is re-implemented here.
