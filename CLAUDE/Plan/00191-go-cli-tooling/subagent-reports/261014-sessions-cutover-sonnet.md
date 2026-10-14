# Task 4.1 cut-over: ccy-sessions to the Go binary

Done in one commit. `play-claude-yolo.yml` builds `cmd/ccy-sessions` (`-trimpath`, no
`-X RootDir`) into the shared `~/.cache/ccy-go-build` directory (also used now by ccy-teams)
and installs it with `copy` (checksum) at `~/.local/bin/ccy-sessions`, mode 0755.
`files/home/.local/bin/ccy-sessions` is deleted. The four suites are Go-only; the bash
library cases remain. `qa-changed.bash` maps `go list -deps ./cmd/ccy-sessions` plus
go.mod/go.sum to the four suites (table-driven with ccy-teams). CCY 3.95.7.

Callers unchanged (same path, argv, exit codes): both systemd units, `shutdown-with-update`,
`helpers/self_update/cycle.py`.

Also changed: the Go usage-vs-bash-header test (its source is gone) was removed;
`test_cycle.py` reads the set-going wait and start window from `internal/sessions/app.go`;
`test_changed_plays.py` fixtures use the restore unit as the deployed file.

Notes: `test-ccy-tmux-targets.bash` is not invoked by `qa-all.bash` (pre-existing), so
`qa-changed` can never pick it from its suite list; it is in the mapping for when it is wired.
`test-self-update-cycle.bash` and `test-agent-bus-install.bash` fail "a non-root caller is
refused" in this root container (exit 127); unrelated to this change.
