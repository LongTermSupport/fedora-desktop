# pingbus usage errors (issue 137)

Not plan work beyond the report destination; Plan 00194 owns pingbus v1. Bug fix only.

## Changed

- `helpers/pingbus/cli.py`: `Parser.error` no longer adds the program prefix (`main()` adds it);
  it keeps only a subcommand's name (`say: ...`). `Parser.parse_args` now refuses a stray
  positional on `say`, `report`, `ask-human` and `thread` with
  `pingbus: say reads the message from stdin: printf '%s\n' '...' | pingbus --team T say --to NAME`.
  `STDIN_FORMS` lists those commands. Stdin stays the only input.
- `tests/helpers/pingbus/test_cli_usage.py` (new): every usage-error path through the one Parser
  (unknown option, unknown command, missing value, bad type, bad choice, stray positional) must
  carry the prefix once; the stdin refusal text for the four commands.

## Evidence

- Red (before the fix): 9 failures, e.g. `pingbus: pingbus: unrecognized arguments: some text`.
- Green (after): `python3 -m unittest tests.helpers.pingbus.test_cli_usage` OK (3 tests);
  `test_cli` and `test_cli_offline` OK (148 tests).
- `./scripts/qa-changed.bash`: passed (ruff, unit tests, go, 4 suites naming the helper).

## Left / noted

- The pre-commit hook (not qa-changed) demanded the version bump described below.
- Tooling faults met: `gh issue view 137 --comments` printed nothing in this container (issue text
  not seen; the task brief was followed). The 120 s foreground limit backgrounds qa-changed on a
  loaded host (several agents ran it at once), so it took several minutes.

## Version bump and the second QA run

- Image-baked helper changed, so CCY_VERSION 3.112.2 and container 2.79.26 (the coordinator-assigned numbers).
- First qa-changed (helper and test only): passed, go gate included.
- Second qa-changed (with the version files): every check passed except the go gate, which
  timed out after 30 minutes (host heavily loaded, the QA runner queued behind another agent for
  about 25 minutes first). Nothing in the diff touches Go. Committed and pushed on that basis;
  the coordinator's full qa-all run is the real gate.
