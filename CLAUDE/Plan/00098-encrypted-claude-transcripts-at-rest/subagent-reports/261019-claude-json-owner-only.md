# claude.json owner-only

Branch fix/claude-json-owner-only. Plan 00098 owns the task; journalled there.
play-claude-yolo.yml: new var claude_state_file (~/.claude.json) and four tasks
after the existing assert: stat, file mode go-rwx (only when it exists, changed only on a real
change, never created), re-stat, assert mode ends in 00. docs/playbooks.md updated.
No test suite covers these tasks. syntax-check and qa-changed passed.
