# Task 4.2 (Plan 00209): report and handoff

## Done: the end of entrypoint.sh is Go (commit on F44, not pushed)

CCY 3.115.0, container 2.82.0. Detail, decisions and the handoff are in
[LAUNCH-ccy.md](../LAUNCH-ccy.md); PLAN.md's Task 4.2 is 🔄 and points there; journal entry
`JOURNAL/2026-11-10/164010Z-action-e3540b.md`.

- `src-go/internal/ccystart` + `cmd/ccy-start-claude`: the v1 bus step, the supervisor wrap,
  the lifecycle and first-join relaunch plugins and the final exec, ported from `entrypoint.sh`.
  Built static into the image's build context (`tasks/claude-yolo-image.yml`, through
  `build-go-command.yml`), copied to `/usr/local/bin/ccy-start-claude` by the Dockerfile.
  `entrypoint.sh` keeps the D62 refusal (now over `TEAMCHAT_*` too) and ends in
  `exec /usr/local/bin/ccy-start-claude "$@"`: 321 code lines from 440, allowlist tightened.
- **v1 byte for byte**: `parity_test.go` runs the old tail (frozen as
  `testdata/entrypoint-tail-v1.bash.txt`) and `Start` over the same scratch tree for 57
  scenarios (all the tail cases of `test-ccy-agent-bus.bash` and `test-ccy-lifecycle.bash`, plus
  edge cases), comparing exit status, stderr, argv, inherited `CCY_CLAUDE_WRAPPER` and the
  pingbus link. It found four real differences on the first run, all fixed (e.g. `read -ra`
  reads only the wrapper's first line).
- **v2 (seam 53, container half)**: `TEAMCHAT_MEMBERSHIP=/run/teamchat/membership` beside
  `PINGBUS_MEMBERSHIP=off` links `/usr/local/bin/teamchat` and execs
  `teamchat run --claude --kind ccy --desired /run/teamchat/membership -- <supervisor line> claude ...`.
  A launch on both systems, a wrong value, a missing kit or `desired.json`, or a command other than
  claude is refused.
- `qa/rules/table.go`: a `dynamicNames` row for `ccystart.Start`.

## Decisions recorded (LAUNCH-ccy.md)

- L1: the v2 signal is `TEAMCHAT_MEMBERSHIP`, consumed and unset before the session.
- L2: `entrypoint.sh` exports the five settings `ccy-start-claude` reads, because a `ccy.env` may
  set them without `export`. The only behaviour difference from the bash: such a value is now in
  the session's environment too. The command line does not change.
- L3 (seam 55, the open input): on a machine with no status record for a team, the team is v2-live
  when the owner's seat store holds a pool seat for that id; a name that is both a declared v1 team
  and a pool seat's team is refused (64). Not built.

## Not done (the handoff, in order; LAUNCH-ccy.md "Left")

The launcher does not pass `TEAMCHAT_MEMBERSHIP` yet, so no session starts on v2. Next:
`ccy-teams system`; the launch take as a `ccy-teams internal` verb that prints the podman
arguments, read back by one `mapfile` line in `lib/agent-bus-membership.bash` (which has 4
functions and no room for a fifth), with the mount guard; the exit release through
`ccy_bus_disconnected`; RS3 with the RS2 switch; the seats sweep and its units.

## Findings for the coordinator

1. `src-go/CLAUDE.md` should list `internal/ccystart` (layout) and the build into the ccy image
   (build and deploy). I left it alone: the file has another lane's uncommitted changes, and a
   pathspec commit would have taken them.
2. The container-version gate does not judge `src-go`. `ccystart` is now image content, so a
   change to it reaches the image with no container bump required (the same gap as finding 2 of the
   stage B/C report).
3. Dogfooding: `lint_on_edit` reports issue #231 (`root_dir` undefined) on every edit of
   `tasks/claude-yolo-image.yml`, and its shellcheck runs `-x` from a directory where the
   `source "$LIB_DIR/..."` lines of `test-ccy-lifecycle.bash` do not resolve (SC1091 info
   reported as a failure). That second fault is not filed yet.
4. Host run: the ccy play must be run to rebuild the image (the existing `changed-plays` entry in
   `meta-deploy.list` covers the ccy play). Check afterwards: a ccy session starts as before, and
   `/usr/local/bin/ccy-start-claude` exists in the container.
