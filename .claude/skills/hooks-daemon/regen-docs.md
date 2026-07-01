# Regenerate Generated Docs

Force-regenerate the daemon's generated documentation to its canonical form,
**without restarting the daemon**.

## Usage

```bash
/hooks-daemon regen-docs
```

## What It Regenerates

Two artifacts are rewritten from live config + handler metadata in one shot:

1. **`.claude/HOOKS-DAEMON.md`** — the active-handler summary (priorities, behaviours,
   per-event handler tables). Same output as `generate-docs`.
2. **The `<hooksdaemon>` block in your project `CLAUDE.md`** — the active-handler
   guidance the daemon injects at startup. Surrounding user content is preserved;
   only the `<hooksdaemon>...</hooksdaemon>` section is replaced.

Both are written in the daemon's canonical (formatter-aligned) form, so a later
edit will not produce churn diffs.

## When to Use

- **Resolving a git merge/rebase conflict** that left conflict markers or stale
  content inside `.claude/HOOKS-DAEMON.md` or the `CLAUDE.md` `<hooksdaemon>` block.
  Run `regen-docs`, then stage the clean result — the regenerated content reflects
  your merged `.claude/hooks-daemon.yaml`, not either conflicting side.
- After hand-editing `.claude/hooks-daemon.yaml` when you want the generated docs
  refreshed but do **not** want to bounce the running daemon.
- Any time the generated docs have drifted from the live config.

## regen-docs vs restart

| Action       | Refreshes generated docs | Restarts daemon | Use when                                      |
| ------------ | ------------------------ | --------------- | --------------------------------------------- |
| `regen-docs` | Yes                      | No              | Fixing/refreshing docs only (e.g. merge fix)  |
| `restart`    | Yes (on startup)         | Yes             | Applying config/handler changes to the daemon |

A `restart` already regenerates both artifacts on startup. `regen-docs` is the
explicit one-shot when you want the doc regeneration **without** the restart.

## Options

```bash
/hooks-daemon regen-docs --include-disabled   # list disabled handlers in HOOKS-DAEMON.md
/hooks-daemon regen-docs --output PATH         # alternate HOOKS-DAEMON.md output path
```

## Verify

```bash
git diff -- .claude/HOOKS-DAEMON.md CLAUDE.md
```

The diff should show only the regenerated, conflict-free content. If `CLAUDE.md`
is in a git repo and was changed, the daemon auto-commits the `<hooksdaemon>`
block on regeneration (only `CLAUDE.md`, no other files are touched).
