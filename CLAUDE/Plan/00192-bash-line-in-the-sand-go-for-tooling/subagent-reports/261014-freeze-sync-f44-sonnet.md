# go-port/freeze: F44 merged in

## Conflicts and how they were resolved

- `PLAN.md`: kept this branch's Task 3.1 text (the freeze port's state), took F44's Tasks 3.2 and 3.3 and the rest.
- `CLAUDE/QA.md`: took F44's table (new `qa-bash-limits` row, re-aligned columns) and re-added this branch's `qa-go` sentence about `internal/freeze/golden`.
- `scripts/bash-limits.allowlist`: took F44's file (measurer 2, statements column), dropped the three records for the deleted bash freeze files, then `qa-bash-limits.bash --update` (only tightened `qa-all.bash`, `qa-changed.bash`, `qa-deployed-drift.bash`).
- `scripts/test-qa-changed.bash`: kept the data-file lookup (`go-binary-suites.tsv`) and F44's removal of `QA_ONLY_COMMANDS` (the gate now lives under `src-go/qa/cmd`, outside the loop).
- `scripts/qa-changed.bash` merged cleanly but came out one statement over its record; the empty-dir `continue` was folded into the following `if`. `go-binary-suites.tsv`'s header line about QA tools updated to say they live under `src-go/qa/cmd`.

## Compile fixes for the new types

- `term.ExitCode` returned from `freeze.ExitCode`, `app.Run`, both engines' `Run`/`RunContext`, both mains (`os.Exit(int(...))`), and the tests and golden harness that held an `int`.
- `term.Term.WriteErr` is now private: `freeze` reads `WriteFailed()` (no reset, since a failed write ends the run anyway).

## Rule table change

The project rule `project_seam_commands` flagged `podman` run in `internal/podfreeze` (owned by `internal/sessprobe`). `commandOwners` now maps a program to a list of owners:

- `podman`: `internal/sessprobe` and `internal/podfreeze`.
- `lxc-ls`, `lxc-info`, `lxc-freeze`, `lxc-unfreeze`, `lxc-attach`, `nmcli`: `internal/lxcfreeze`.
- The message names every owner ("owned by a or b").
- Tests: new fixture packages `podfreeze`, `lxcfreeze`, `sessprobe` (owner clean, foreign program flagged) and two new red cases in `badcmd/bad_more.go` (podman and nmcli outside their owners). `CLAUDE/GoProjectRules.md` updated.
- Not weakened: no rule relaxed. Note the rule only sees `execx.Cmd` literals, so the `lxc-*` and `nmcli` names passed as strings to `sudo.Capture` are not checked by it today; the owner rows state the ownership and bind if those ever become a `Cmd`.

## QA

- `scripts/qa-go.bash`: GO-OK, 27 packages, coverage 95.0%, golangci-lint clean, project rules clean (includes the freeze golden replay and all package tests).
- `scripts/qa-bash-limits.bash`: BASH-LIMITS-OK after `--update` (tightened only).
- `scripts/test-qa-changed.bash`: 73 passed, 0 failed.
- `scripts/qa-changed.bash`: passed, all picked suites green, including the ccy-teams and ccy-sessions parity suites.
- `ansible-playbook --syntax-check`: `play-podfreeze.yml`, `play-lxcfreeze.yml` (under `playbooks/imports/optional/common/`) and `play-claude-yolo.yml` all parse.
- `qa-all.bash` not run, as instructed.
