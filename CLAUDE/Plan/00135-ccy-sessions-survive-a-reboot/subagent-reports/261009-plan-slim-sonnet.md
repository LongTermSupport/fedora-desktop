# Plan 00135 slimming report

- PLAN.md: about 28 KB to 16,804 bytes.
- New DONE-DETAIL.md: 17,257 bytes. It holds the verbatim text of every completed task (Phases 1-8), organised by phase and task. It also holds the success-criteria evidence.
- PLAN.md keeps each completed task as a checkbox line, a one-line summary and a link such as `DONE-DETAIL.md#task-37`.
- Kept in full in PLAN.md: all open tasks (5.x, 6.3, 7.4, 8.5), the open 7.1 sub-task with its `tmux-qa-wiring.patch` link, goals, success criteria, decisions and out-of-scope.
- Links: `research/`, `subagent-reports/` and `brainstorm-ssh-key-restore/` paths are the same folder-relative paths from DONE-DETAIL.md, so they resolve.
- Journal: one `action` entry appended via `mkplan.bash --journal 135 action` to the 26-10-09 day-file.
- Checks: `hooks-daemon plan-qa --lint` reports 0 findings. `./scripts/qa-docs.bash` is OK (78 files, no broken links or anchors).
- Not committed.
