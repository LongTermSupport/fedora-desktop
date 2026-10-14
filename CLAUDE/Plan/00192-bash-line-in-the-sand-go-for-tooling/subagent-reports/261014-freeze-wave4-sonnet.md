# Freeze port wave 4 (cut-over): report

Branch `go-port/freeze`, not pushed, not merged.

## Golden capture and replay

- `golden-capture/capture.py` runs the shipped bash tools (taken from git revision `67c20b7f`, the
  last commit that had them) under fake `podman`, `sudo`, `lxc-*`, `nmcli` and `fzf`, answered from
  `src-go/internal/freeze/golden/testdata/scenarios.json`. The numbered prompts run on a
  pseudo-terminal that is also the controlling one; scenarios without a terminal have none. The
  container guard is bypassed by appending one line to a copy of the library, never by editing the
  tools; guard scenarios run the unmodified files. Records stdout, stderr, exit status and the
  commands run.
- `internal/freeze/golden/golden_test.go` plays the same scenarios through the Go entry points over
  the same world and compares byte for byte. 136 scenarios: 82 identical, 54 differ, every
  difference a numbered bug, pinned in `testdata/deviations.json` with the bugs, the reason and the
  streams, and the Go output in `testdata/deviation/`.
- The lxcfreeze usage line changed too (task 5); its three help scenarios are bug 6 deviations.

| Bug | Scenarios                            | What differs                                                                    |
| --- | ------------------------------------ | ------------------------------------------------------------------------------- |
| 1   | sudo noise on stderr                 | the `sudo: unable to resolve host` line is no longer a container                |
| 2   | fzf crash, both tools                | exit 2 from fzf is an error (exit 1), not a quiet leave                         |
| 4   | a name given twice, both tools       | acted on once                                                                   |
| 6   | every lxc sudo prompt scenario, help | `sudo.Ready` words instead of the `==>` lines; help text                        |
| 7   | every freeze with a failure          | "Thaw them with" lists successes only, nothing when none                        |
| 13  | podman network rows and `--network`  | the fourth `podman ps --filter network=` query is gone                          |
| 14  | a label record with three fields     | refused (bash accepted it)                                                      |
| 22  | `list` with a target                 | refused before any command (calls differ; the no-podman case differs in stderr) |
| 26  | every menu start                     | one load at startup, not two (calls only)                                       |

## One difference that was not on the list

Giving up after three bad replies in the drill-down ended only that step in bash (a `die` inside a
command substitution) and returned to the menu, with the message printed; the group menu's own
give-up exits 1. The Go port exited 1 in both. Not a numbered bug and not a fix, so the loop now
matches bash: `freeze.GaveUpError` (a `DieError`), said and followed by the menu when it comes from
the drill-down, fatal from the group menu. Tests: `TestLoopGivingUpInTheDrillDown...`,
`TestLoopAFatalErrorFromTheDrillDownIsStillFatal`, `TestGiveUpIsAFatalErrorOfItsOwnKind`.

## Not covered by the replay

- Ctrl-C at a numbered prompt (bash dies by SIGINT, Go exits 130 through its context): the unit
  tests hold it, a pseudo-terminal replay cannot compare a signal death with an exit code.
- The `-n` flag alone into the menu is covered; the `vanished` bucket (a container gone between
  the inventory and the act) is not, as no fake produces it without a second inventory.
- A real engine: `ACCEPTANCE-freeze.md`.

## Plays and tasks

- `tasks/build-go-command.yml` (imported, not included, so a tag on the import reaches the build
  and the install): vars `go_command_name`, `go_command_dest`, `go_command_mode`, optional
  `go_command_ldflags`. Everything `become: false`, the same scratch directory
  `~/.cache/ccy-go-build`, the same environment, `changed_when: false` on the build, checksum
  compared `copy` with `remote_src`. The command line is built with `argv`; its rendering was
  checked with Ansible's own templar for the two shapes (with and without ldflags) and equals the
  old line.
- `play-claude-yolo.yml`: both builds moved onto it (same flags, paths, modes).
- `tasks/deploy-freeze-lib.yml` became `tasks/deploy-freeze.yml` (`freeze_tool`): fzf, the bin
  directory, the shared build, and removal of `~/.local/lib/freeze` (`state: absent`, idempotent).
- `play-podfreeze.yml`, `play-lxcfreeze.yml`: `import_playbook: ../../play-golang.yml` first, one
  import of `deploy-freeze.yml`, the `podman-freeze` removal kept; the bash `copy` is gone.
- `ansible-playbook --syntax-check` passes for all three plays. The post-edit lint hook runs
  `--syntax-check` on a task file as if it were a play and reports a false failure for any file under
  `tasks/`; nothing was run to get round it.

## Deleted

`files/home/.local/bin/podfreeze`, `lxcfreeze`; `files/home/.local/lib/freeze/freeze-common.bash`;
`scripts/test-freezelib.bash`, `test-podfreeze.bash`, `test-lxcfreeze.bash` and their three
`qa-all.bash` gates; the library pair in `qa-deployed-drift.bash`; `internal/freeze/freezetest` and
the two usage tests that read the shipped bash; three allowlist entries.

## Ratchet

`scripts/go-binary-suites.tsv` replaces the `GO_BINARY_SUITES` array; the joined assignment is split
back. `qa-changed.bash` measures 543 code lines (record was 548); `qa-all.bash` 836 (was 860);
`qa-deployed-drift.bash` 165 (was 166). `meta-deploy.bash` is unchanged at 187: its pending
`play-claude-yolo.yml` line was replaced by one entry for this plan, whose `deploy.bash` runs that
play first and then the two freeze plays. The freeze rows in the table say `qa-go`, not
QA-only: they are deployed by a play, and their oracle is a Go test that the whole Go gate runs.
`test-qa-changed.bash` reads the table (71 cases).

## Gates run

`qa-go.bash` (coverage 94.7%, floor 90%), `qa-bash-limits.bash`, `qa-changed.bash`,
`test-qa-changed.bash`, `--syntax-check` on the three plays, `meta-deploy.bash --list`. Not
`qa-all.bash`.
