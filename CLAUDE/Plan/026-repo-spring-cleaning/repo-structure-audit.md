# Repository Structure Audit

**Date**: 2026-04-03
**Method**: `git ls-files`, `du -sh`, `find`, manual review
**Tracked files**: 278
**Overall health**: Good — minor cleanup needed

## ~~Backup Files Tracked in Git~~ — FALSE ALARM

These backup directories/files exist on disk but are **already gitignored** by the `.claude/*.bak` rule. They were never tracked in git. The research agent incorrectly assumed they were committed.

| Path | Size | Git Status |
|------|------|------------|
| `.claude/hooks.bak/` | 2.1M | Not tracked (gitignored) |
| `.claude/hooks.bak.20260126_071010/` | 40K | Not tracked (gitignored) |
| `.claude/hooks.bak.20260128_031122/` | 272K | Not tracked (gitignored) |
| `.claude/settings.json.bak` | 1.8K | Not tracked (gitignored) |
| `.claude/hooks-daemon/.claude/settings.json.bak` | Small | Not tracked (gitignored) |

**No action needed.**

## Orphaned Documentation (Low Severity)

| Path | Size | Description |
|------|------|-------------|
| `CCY-EXTRACTION-PLAN.md` | 437 lines | Detailed plan for extracting CCY into standalone repo. In root directory, not in `CLAUDE/Plan/` structure. Never executed. |

**Fix**: Move to `CLAUDE/Plan/Archive/ccy-extraction-plan.md` or delete if no longer planned.

## Plan Directory Organisation (Low Severity)

| Issue | Description |
|-------|-------------|
| Plan 012 location | Listed in README as `012-fix-plugin-handlers/` but actually in `Completed/` directory. Status is "Cancelled" but location suggests "Completed" |

**Fix**: Move Plan 012 to match its status or update README to reflect actual location.

## Python Bytecode in Git (Low Severity)

| Path | Count | Description |
|------|-------|-------------|
| `.claude/hooks.bak/controller/__pycache__/` | 188 files | Compiled `.cpython-311.pyc` and `.cpython-313.pyc` files (~150KB) |

**Fix**: Will be removed when backup directories are cleaned up.

## No Issues Found (Clean Categories)

| Category | Status |
|----------|--------|
| Empty files | None found |
| Large binary files | None tracked |
| Naming convention violations | None found |
| Hardcoded user paths in scripts | None found (Ansible uses `{{ user_login }}` correctly) |
| Missing `.gitignore` rules | Coverage is good |
| Symlinks | 1 found, appears intentional |
| Untracked sensitive files | `vault-pass.secret` correctly gitignored, NOT tracked |

## Untracked Directories (Informational — Not Issues)

These are properly gitignored:

| Path | Size | Description |
|------|------|-------------|
| `untracked/qobuz-player/` | 5.5G | Rust project (build artifacts) |
| `untracked/rodio/` | 39M | Rust audio library |
| `untracked/hifi.rs/` | 5.5M | Rust project |
| `untracked/blurt/` | 1.9M | Project |
| `untracked/gnome-shell/` | 52K+ | GNOME Shell source reference |
