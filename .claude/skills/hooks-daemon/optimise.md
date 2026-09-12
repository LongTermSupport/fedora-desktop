# Hooks Daemon Configuration Optimiser (the config-optimisation step)

This is the formalised "enable all relevant handlers and ensure optimal configuration"
step (Plan 00308) — the same step whether run manually, automatically at the end of
`/hooks-daemon upgrade`, or as the closing step of LLM-INSTALL.md/LLM-UPDATE.md.

Analyse the current hooks daemon configuration against the project's profile (languages,
tests, CI, plans) and produce a scored report across six areas, derived from the
handler registry rather than a hand-kept list (Plan 00330). Also compares the
project's config against the installed daemon's config-changes manifests
(`.claude/hooks-daemon/CLAUDE/UPGRADES/config-changes/` on a client install; the
script prints the resolved path) to surface capabilities introduced since the
last recorded run, and can apply recommendations
automatically. Every run (report-only or apply) records itself via
`bin/hooks-daemon record-config-optimisation-run`, which silences the
`config_optimisation_reminder` SessionStart advisory until the next upgrade.

## Usage

```claude-code
/hooks-daemon optimise
```

No arguments — the step profiles the project automatically.

**Naming (Plan 00322)**: this used to be a standalone `/optimise` skill. A
top-level command that generic collides with whatever else a project or plugin
calls `optimise`, so it now lives in the daemon's own namespace with `upgrade`,
`health` and `bug-report`. A project that still has `.claude/skills/optimise/`
on disk is holding an orphan from before the move; the installer removes it.

## Running it

**Run this first** — it prints the full instruction set, which you then follow
exactly:

```bash
bash "${CLAUDE_SKILL_DIR:-.claude/skills/hooks-daemon}/scripts/optimise-invoke.sh"
```

The script resolves the project root, the config path and the daemon CLI
wrapper for this install (normal or self-install), then emits the step-by-step
analysis and apply procedure. **The summary below is orientation only — the
script's output is the procedure.** Nothing runs it for you: Claude Code loads
markdown, never a sibling script, so skipping this command means running the
step from the summary alone (Plan 00322).

## What It Checks

**Every registered handler**, no exceptions. The checklist is produced by
`bin/hooks-daemon optimise-checklist`, which walks the handler registry, so a
handler cannot ship without being scored. Each handler declares its own
RELEVANCE (`Handler.get_relevance()`): most apply everywhere; a few need
something the project may lack (`lsp_enforcement` an LSP, the npm handlers a
`package.json`, the ccy handlers an armed supervisor, the flaggable-content
trio a deployed quarantine agent). The optimal state of a relevant handler is
enabled, whatever its default; an irrelevant one is reported as "not
applicable here", never as a shortfall.

Handlers are grouped into six areas computed from their event and tags,
each scored PASS / WARN / FAIL:

1. **Safety**
2. **Agent behaviour & message quality** — Stop, SubagentStop and the nitpick detectors
3. **Plan & documentation workflow**
4. **Code & content quality**
5. **Session, environment & daemon** — SessionStart advisories and status-line components
6. **Other guards** — everything the rules above do not claim

The top-level `plan_workflow` config section and "plans in use" are checked
alongside, since no handler owns them.

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

━━━ Safety ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ PASS (21/21)
    all 21 relevant handlers enabled
━━━ Code & content quality ━━━━━━━━━━━━━━━━━━━━━━ WARN (8/9)
    8 relevant handlers enabled; 1 to enable:
    ✗ handlers.pre_tool_use.tdd_enforcement
        Enforce test-first development
    not applicable here (1):
    ○ handlers.post_tool_use.validate_eslint_on_write
        no JavaScript/TypeScript toolchain (package.json/tsconfig.json) detected
...
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Overall: 109/110 relevant handlers enabled (99%) · 6 not applicable here

Recommendations (1 improvements available):
  [1] Code & content quality: enable handlers.pre_tool_use.tdd_enforcement — ...
```

An area whose relevant handlers are all enabled collapses to a single line;
only shortfalls and not-applicable handlers are listed, so the report stays
readable however many handlers the daemon ships.

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
