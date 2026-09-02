---
name: optimise
description: Analyse hooks daemon configuration and recommend improvements across Safety, Stop Quality, Plan Workflow, Code Quality, and Daemon Settings — the canonical config-optimisation step
argument-hint: ""
---

# /optimise - Configuration Optimiser Skill (canonical config-optimisation step)

## Description

This is the formalised "enable all relevant handlers and ensure optimal configuration"
step (Plan 00308) — the same step whether run manually, automatically at the end of
`/hooks-daemon upgrade`, or as the closing step of LLM-INSTALL.md/LLM-UPDATE.md. There
is no separate `config-optimisation` command; `/optimise` IS it.

Analyse the current hooks daemon configuration against the project's profile (languages,
tests, CI, plans) and produce a scored report across five key areas. Also compares the
project's config against `CLAUDE/UPGRADES/config-changes/` manifests to surface
capabilities introduced since the last recorded run, and can apply recommendations
automatically. Every run (report-only or apply) records itself via
`bin/hooks-daemon record-config-optimisation-run`, which silences the
`config_optimisation_reminder` SessionStart advisory until the next upgrade.

## Usage

```claude-code
# Run full analysis and show scored report
/optimise
```

No arguments — the skill profiles the project automatically.

## What It Checks

The skill analyses five areas, scoring each PASS / WARN / FAIL:

1. **Safety** — Critical blocking handlers (destructive_git, sed_blocker, security_antipattern, etc.)
2. **Stop Quality** — Handlers that prevent poor stopping behaviour (auto_continue_stop, plus the nitpick.hedging_language and nitpick.dismissive_language detectors)
3. **Plan Workflow** — Plan tracking handlers and whether the workflow is actively being used
4. **Code Quality** — TDD, QA suppression, lint-on-edit, LSP enforcement, daemon restart verification
5. **Daemon Settings** — Session-start advisories, version checks, git context injection

## What It Outputs

```
╔══════════════════════════════════════════════════════════════╗
║           Hooks Daemon Configuration Optimiser               ║
╚══════════════════════════════════════════════════════════════╝

Project Profile:
  Languages detected: Python, TypeScript
  Test directory: tests/ ✓
  CI config: .github/workflows/ ✓
  Plan directory: CLAUDE/Plan/ (5 active, 12 completed)

━━━ Area 1: Safety ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ PASS (7/7)
...
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Overall Score: 28/35 (80%)
Recommendations: ...
```

## Apply Recommendations

After viewing the report, Claude asks whether to apply recommendations:

- **"apply all"** — Enable all recommended handlers and restart daemon
- **"apply 2,3"** — Apply specific recommendations by number
- **"skip"** — View report only, make no changes

## Reference Documentation

**SINGLE SOURCE OF TRUTH:**

- Handler options and values: `.claude/hooks-daemon/docs/guides/HANDLER_REFERENCE.md`
- Configuration format: `.claude/hooks-daemon/docs/guides/CONFIGURATION.md`
- Available handlers: `.claude/HOOKS-DAEMON.md` (project root)

## Version

Introduced in: v2.29.0
