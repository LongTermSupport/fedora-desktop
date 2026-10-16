# Acceptance host run 4 (F44 51dce29f): the two failures fixed

Branch fix/acceptance-host-4, from F44 342fae6f. Only acceptance scripts changed: no product
file, no version bump. CCY 3.99.10.1 and container 2.75.1 stay as deployed.

## 1. Plan 00203 M2 (c): "the plain session runs bus processes other than one keeper"

**Root cause: the test.** `m2-check.bash plain` counted every line of `ps -eo args` containing
"pingbus". claude (pid 2) is started with
`--plugin-dir /opt/claude-yolo/optional/agent-bus/plugin/pingbus`, so it matched and the count
was 2. On the host's exact list, that old filter prints 2 (red).

**Fix.** `m2_procs.py` (in the plan folder; it runs in the container through
`podman exec … python3 -I`, like `m2_inbound.py`) treats a process as a bus process by its
program: argv[0], or argv[1] when argv[0] is a python interpreter (past its options), with a
basename of `pingbus` or `agent-bus`. It has three checks:

- `at-most-keeper`: M2 (c).
- `none`: the pingbus half of M2 (e). The `--plugin-dir`/`--settings` grep stays, because on
  `--no-bus` those flags are themselves the defect.
- `keeper-only`: M1 (b). It replaces the awk over the `/proc` table, which matched
  `/\/pingbus [a-z]/`. That excluded claude only because `--settings` follows the plugin path
  (the same flaw, latent). Its table is still kept as `<name>.holders`.

`test_m2_procs.py` (13 tests) has the host's exact process list. It was red first (no module),
then green: on that list the keeper is the only bus process, (c) passes, and M1 (b) passes.
Swept for the same flaw: M3, M4 and robust have no process filter. In 00161, U20's
`u20_check.py _watch_mode` already matches on the program, by python and the basename.

## 2. Plan 00161 M2.10: "the checkout is as it was before M2.0"

**Decision: a report that outlives its session is designed behaviour, not a product leak.**

- SPEC 3.3 says the keeper "writes it at its start, so a relaunch resets any stale report",
  and that the report is never authority (the seat lock is).
- Every reader (`session list`'s `sessionRow`, add's `waitHeld`, `relaunch`, `alreadyIn`,
  `waitTold`) reads only a running session's report.
- A restore replays the registry record, not the report.
- Nothing can reliably remove the report at session end:
  - the keeper dies with the container, by SIGKILL in the pid-namespace teardown;
  - the trampoline is the one end-of-session hook, and the acceptance sessions do not run
    through it;
  - `claude-yolo` is on the bash ratchet and cannot grow.

So the acceptances clean up after themselves:

- **00203** (`_acceptance-m2.inc.bash`):
  - `m2_cleanup` now also deletes `.claude/ccy/pingbus/sessions/<name>/` for every name in
    `M2_NAMES` (M1, M2, M3 and M4 all adopt theirs).
  - It removes `sessions/` only if this run created it.
  - `m2_prepare` first runs `m2_remove_stale_reports`. That deletes reports named
    `^(m1-plain|m2-plain|m2-nobus|m3-hot|m4-a|m4-b)-[0-9]+$`, skipping any whose tmux session
    or container is running. If podman cannot answer, the run fails rather than delete. The two
    `m2-plain-*` reports on the host go.
- **00161** (`_acceptance-u20.inc.bash`):
  - `u20_named_down` now also deletes the named session's report and its registry record.
    The record leaked too: a launch in ccy's tmux writes one even with `--no-restore`.
  - It removes `sessions/` only if this run made it.
  - `u20_prerequisites` runs `u20_remove_stale_reports` (`^u20-named-[0-9]+$`, same guards)
    before `u20_record` takes the M2.0 snapshot.
  - M2.10 is unchanged and stays strict.

A scratch run with stub tmux and podman checked the two sweeps:

- stale acceptance names were removed;
- a name whose tmux session or container was running was kept;
- an owner session's report was kept, and so was a name that is not a pid (`m2-plain-abc`);
- an emptied `sessions/` was pruned.

DESIGN.md M2.10 says so.

**Left open for the owner:** a real session that ends normally leaves its report until a
session of the same name starts again. It is small, never read, and rewritten on reuse. If it
should go, the place is the registry trampoline, beside the record it already removes.

## Queue

`meta-deploy.list`: `00203-teams-hot-control-and-robustness`, then
`00161-agent-team-bus-matrix/acceptance.bash`. There is no play. 00201 is not re-run, because
nothing it tests changed.
