# Regenerate Generated Docs

Force-regenerate the daemon's generated documentation to its canonical form,
**without restarting the daemon**.

## Usage

A CLI verb, not a skill subcommand (Plan 00330 — the CLI name is the one
agents use). On a self-install the wrapper is `bin/hooks-daemon`:

```bash
.claude/hooks-daemon/bin/hooks-daemon regenerate-docs
```

It is also step 14 of the housekeeping pass, where it acts without
confirmation because it is idempotent (see [housekeeping.md](housekeeping.md)).

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
  Run `regenerate-docs`, then stage the clean result — the regenerated content reflects
  your merged `.claude/hooks-daemon.yaml`, not either conflicting side.
- After hand-editing `.claude/hooks-daemon.yaml` when you want the generated docs
  refreshed but do **not** want to bounce the running daemon.
- Any time the generated docs have drifted from the live config.

## regenerate-docs vs restart

| Action            | Refreshes generated docs | Restarts daemon | Use when                                      |
| ----------------- | ------------------------ | --------------- | --------------------------------------------- |
| `regenerate-docs` | Yes                      | No              | Fixing/refreshing docs only (e.g. merge fix)  |
| `restart`         | `CLAUDE.md` block ONLY, on a home branch | Yes | Applying config/handler changes to the daemon |

A `restart` regenerates **only** the `<hooksdaemon>` block in `CLAUDE.md`, and
only while the checkout is on a home branch (`branches.home`, default the
default branch) and `branches.home_commit` is not `false`. Off a home branch it
leaves the block alone and says so only in the daemon log at INFO; SessionStart
stays silent about it, unless no home branch resolves to a local branch, when
SessionStart asks you to set `branches.home`. On a home branch with
`branches.home_commit: false` the restart never writes, and SessionStart reports
a stale block (recomputed at each SessionStart, so it goes away once the block
is current). `regenerate-docs` writes the block from any branch except that
last case, without committing it off a home branch: run it on a pull-request
branch or in a pull-request worktree and commit the result there. On a home
branch under `branches.home_commit: false` it refuses and exits non-zero. A restart never
rewrites `.claude/HOOKS-DAEMON.md`. That is deliberate: the file's
`> Generated on … (vX.Y.Z)` line records the version the project's tracked
assets were deployed from, and the upgrade reads it, so a restart must not
restamp it. The cost is that a handler added in-repo refreshes one artefact and
leaves the other stale. The daemon's own repository catches that with a QA
check (`scripts/qa/check_generated_doc_drift.py`); a client project has no such
check.

`regenerate-docs` is the only command that refreshes BOTH, which makes it the
one to run after adding or removing a handler — not just after a merge
conflict.

## Options

```bash
.claude/hooks-daemon/bin/hooks-daemon regenerate-docs --include-disabled   # list disabled handlers in HOOKS-DAEMON.md
.claude/hooks-daemon/bin/hooks-daemon regenerate-docs --output PATH         # alternate HOOKS-DAEMON.md output path
```

## Verify

```bash
git diff -- .claude/HOOKS-DAEMON.md CLAUDE.md
```

The diff should show only the regenerated, conflict-free content. If `CLAUDE.md`
is in a git repo and was changed, the daemon auto-commits the `<hooksdaemon>`
block on regeneration (only `CLAUDE.md`, no other files are touched).
