# Fix: Fable FIX-FIRST on fix/claude-json-owner-only (Plan 00098)

Branch `fix/claude-json-owner-only-2`, branched from `fix/claude-json-owner-only` at c669e49e
(that branch is checked out in another worktree).

## Changes

In `playbooks/imports/play-claude-yolo.yml`:

1. The re-check `stat` and the `assert` for `~/.claude.json` now carry
   `- not ansible_check_mode` in their `when:`, so `--check` on a 0644 host no longer
   fails the assert (the `file` task only reports `changed` there).
2. Both `stat` tasks for the file have `follow: true`, so a symlinked `~/.claude.json` is
   judged by its target's mode, matching what `file` chmods.

In Plan 00098 `PLAN.md`: Task 1.2 now records `~/.claude.json` coverage (go-rwx, symlink
followed, re-stat plus assert, skipped under `--check`) and names `play-claude-yolo.yml`;
the stale `play-claude-code.yml` in the Phase 2 gate line is fixed too.

Fable's review and the first fixer's report are copied beside this file.

## Verification

- `ansible-playbook --syntax-check playbooks/imports/play-claude-yolo.yml`: exit 0.
- `scripts/qa-changed.bash`: passed (ansible-syntax, ansible-patterns, docs, ansible-pause,
  version-pins and the 13 test suites that name the play).
- No real playbook run, no qa-all, no meta-deploy entry. Not pushed, not merged.
