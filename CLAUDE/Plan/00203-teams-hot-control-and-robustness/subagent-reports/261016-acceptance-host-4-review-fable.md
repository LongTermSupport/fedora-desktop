# Review of fix/acceptance-host-4 (29769574, from F44 342fae6f)

Verdict: **FIX-FIRST**. Two blocking findings (one correctness, one merge coherence), then MERGE.

`test_m2_procs.py` run here: 13 tests, OK. The simulated merge into F44 (`git merge-tree`) conflicts
in `CLAUDE/Plan/meta-deploy.list`.

## Blocking

### B1. 00161 M2.10 still depends on luck: the three seated sessions write reports too

`CLAUDE/Plan/00161-agent-team-bus-matrix/_acceptance-u20.inc.bash:1073-1082`, `:1150-1163`,
and the developer's reading "the keeper reports when the host named the session".

The code does not say "named". `files/var/local/claude-yolo/lib/agent-bus-membership.bash:49`
runs `ccy-teams internal membership-init "${session:-$container}" …`, so an unnamed `--teams`
launch (which is exactly how `u20_launch` starts acca, accb and accc: `unset TMUX TMUX_PANE`,
`_acceptance-u20.inc.bash:367-371`) gets `session` = its **container name** in `membership.json`.
`helpers/pingbus/seat.py:339-340` passes that through as `Want.session`, and `_publish` (`:698-714`)
writes `sessions/<container-name>/applied.json` whenever it is non-None. So every seated M2.0
session leaves a report named after its container.

The host evidence confirms it. In the latest run
(`untracked/plan-runs/00161-…/acceptance/20261016-074842/u20/{before,after}/pingbus.list`) the
`before` list already holds `sessions/<project>_yolo_1`, `_2`, `_3` and the live directory shows
those three rewritten at 07:50-07:54, during the run. M2.10 did not flag them only because the
container numbers were the same as in earlier runs, so the names were already in `before`, and
the listing records names and types, not content.

Scenario that fails after this branch: the owner has one ccy session running while the acceptance
runs (the design says the owner's sessions are untouched, so this is allowed), or the host is fresh.
The acceptance containers then get `_yolo_4`, `_5`, `_6` (or `_1..3` with no prior report), three
new `sessions/<container>/` directories appear after M2.0, nothing removes them, and M2.10 fails
with the same message as run 4. The fix on this branch removes one of four leaks.

Fix: in the U20 teardown (both `u20_cleanup` and `u20_teardown_after_stop`, and `u20_end` if you
want it per session), after the container is known, remove the report of each container this run
launched when that name was absent from the `before` list:

- resolve the name once per session beside `U20_CTR[s]` (`podman inspect --format '{{.Name}}'`),
- `rm -rf -- "${U20_REPORTS}/${ctr_name}"` only when `grep -qxF "sessions/${ctr_name}	d" "$(u20_ev before)/pingbus.list"` is false (a report that pre-existed belongs to whoever reused
  that container slot before; leave it, the listing is unchanged either way),
- `|| return 1` / `failed=1` as the surrounding code does.

Then fix the wording that misled this: `DESIGN.md` M2.10 (the branch's new sentence), the comment
block at `_acceptance-u20.inc.bash:1073-1077`, the subagent report, and SPEC 3.3's reading. A
container-named session reports too; only a registry-named one is hot-controllable. Optionally,
add a U20 unit to `u20_check.py` or a shell probe that asserts "no `sessions/` entry absent from
`before` survives the teardown", so the leak class is caught rather than the name.

### B2. `CLAUDE/Plan/meta-deploy.list` conflicts with F44 and the branch's own text is stale

`CLAUDE/Plan/meta-deploy.list:22-42` on the branch, vs F44 `c8351c36` lines 22-33.

`git merge-tree F44 29769574` reports `CONFLICT (content)` in the list: F44 appends `# M5 …` +
`playbooks/imports/play-claude-yolo.yml` + `# acceptance on the M5 ccy-teams …` + `00203-…`;
the branch appends its own `# …` + `00203-…` + `# …` + `00161-…/acceptance.bash` at the same
place. The branch also says "no play: only the acceptance scripts changed", which is true of the
branch but false of the merged queue the owner will run.

Fix: merge F44 into the branch (or rebase) and hand-resolve to exactly:

1. F44's M5 comment and `playbooks/imports/play-claude-yolo.yml`;
2. one `00203-teams-hot-control-and-robustness` entry whose comment joins F44's M5 sentence
   (M4 (e) re-proves `session list`; picker steps are the owner's) with this branch's two facts
   (bus process = program; stale `m*-`/`m2-nobus-` reports removed first);
3. this branch's `00161-agent-team-bus-matrix/acceptance.bash` entry, with the B1 fix added to its
   description (the seated sessions' container-named reports go too).

Drop "no play" from the 00203 comment. Re-run the simulated merge before pushing: a committed
conflict marker is denied by the daemon, but a wrongly resolved duplicate entry is not.

## Verified and sound (no change needed)

### (1) 00203 M2 (c), `m2_procs.py`

- Program detection. `/usr/bin/python3 -I /usr/local/bin/pingbus …` → `pingbus` (options are
  stripped). Kernel shebang exec of the zipapp (`/usr/bin/python3 /opt/…/agent-bus/pingbus …`,
  the host's own keeper line, and `#!/usr/bin/env python3` → `python3 /usr/local/bin/pingbus`)
  → `pingbus`. `python3 -m pingbus` → `pingbus` (by accident of `-m` being stripped and the
  module name matching; `-m helpers.pingbus.cli` would not). Direct `/usr/local/bin/pingbus wait`
  → `pingbus`. Unreadable `cmdline` → process skipped. Zombie or mid-exec (empty cmdline) →
  skipped. Non-UTF-8 argv → replaced, not crashed.
- Could it miss a real bus process? Three shapes would, none of which run in a ccy container:
  (a) the source-tree relaunch `helpers/pingbus/cli.py:713-720` (`python3 -c "import sys; …"`)
  — the container runs the zipapp, so `pingbus_argv()` returns `[python3, <zipapp>]`, detected;
  (b) python options that take a separate argument (`python3 -W ignore script`) — never used by
  the kit; (c) `agent-bus` through its bash wrapper (`files/usr/local/bin/agent-bus`, argv[0] is
  `bash`) — a host tool that is not in the image. The watcher (`hooks.py:509-526`) runs
  `/usr/local/bin/pingbus watch --supervise` via the shebang, detected; its supervised child via
  `pingbus_argv()`, detected. Hooks run `pingbus hook …` the same way. No false pass found for
  what the container can actually run. Note (a) in the docstring if dev mode ever matters.
- The M2 (e) grep still covers `--plugin-dir`/`--settings` as the defect on `--no-bus`; `none`
  covers processes. M1 (b) `keeper-only` keeps the lock check and the "others holding a lock"
  check; the table is still written as evidence. `fail()` in `m2-check.bash` exits, so the
  `ok` after a failed probe is unreachable.
- Red-then-green: the host's exact list is the fixture; the old `[p]ingbus` filter counted 2.

### (2) 00161 M2.10 "intended" reading

- SPEC 3.3: the keeper writes the report at its start "so a relaunch resets any stale report";
  no step removes it, and the lock is the authority. `membership.ReadReport` is called from
  `hot/list.go:124` inside `sessionRow`, which is per *running* container, and from
  `hot/add.go`, `hot/role.go` on the target session (which must be running to be changed). So
  a stale report is never read as truth. The reading is correct; the gap is B1's wording.
- Deletion safety. `U20_OWN_NAMES='^u20-named-[0-9]+$'` and
  `M2_OWN_NAMES='^(m1-plain|m2-plain|m2-nobus|m3-hot|m4-a|m4-b)-[0-9]+$'` are anchored and
  used unquoted in `=~` (regex). `basename -- "${dir}"` handles the trailing slash; the glob
  with no match leaves a literal that fails `-d`. Names are those `m2_launch`/`u20_named_launch`
  give to tmux, so the record and the report share them. A running session's report is kept
  (tmux has-session or a running labelled container); podman failing to answer returns 1 and
  stops the run. The only way to hit a non-acceptance report is an owner naming a session
  `u20-named-123`, and then only if it is not running, which is harmless by SPEC 3.3.
- Fail-fast: every `rm -rf`, `rm -f`, `rmdir` is `|| return 1` or `|| status=1` with the
  status returned; `u20_remove_leftover_seats` is now gated too.
- Snapshot window: `u20_prerequisites` (leg at `acceptance.bash:260`) runs before
  `u20_record` (M2.0 snapshot, `:275`); the stale sweep is in prerequisites and sets
  `U20_REPORTS_FOUND` there. `u20_named_down` runs after M2.11 and in both cleanups, before
  `u20_check_checkout_unchanged`. `rmdir --ignore-fail-on-non-empty` is only attempted when this
  run created `sessions/`, and is safe if another session created a report meanwhile.
- Registry record. `lib/session-registry.bash:521-524`: the record is the file
  `$regdir/$name`, removed normally by the trampoline (`:610-614`) that the acceptance's tmux
  command does not use. `rm -f -- "${U20_NAMED_RECORD}"` after the tmux session and container
  are gone is safe; a `--no-restore` record is never replayed anyway (`:1360`).

## Minor, non-blocking

- `_acceptance-m1.inc.bash:21` computes `M1_PROCS_REL` from `PLAN_SCRIPT_DIR`, set by
  `_planlib.inc.bash:287`; `m2-check.bash:31` does the same from `HERE`. Two derivations of
  the same path; fine for now.
- The developer report says "every reader reads only a running session's report" — true, but
  cite `sessionRow` (per running container) rather than listing function names a reader cannot
  find.
- `m2_procs.py` docstring: say that a bus process started from a source tree
  (`python3 -c …`) is out of scope, so a future dev-mode run does not read a pass as proof.

## Re-check (fix/acceptance-host-4 at 4d009e79: 9d76fc8a, 42d9176a)

Verdict: **FIX-FIRST**, one blocking finding (B3), B2 is done.

### B1 as implemented

- Recording: `_acceptance-u20.inc.bash:409-411` (`u20_container_up`, once the seated container
  is running) and `:173` (`u20_remove_containers`, before each `podman rm`) call
  `u20_note_report`, which takes the basename of the `/run/ccy/membership` mount source
  (`:417-422`). That basename is the `membership-init` first argument
  (`agent-bus-membership.bash:49`, `${session:-$container}`), which is the `Want.session` the
  keeper publishes under, so the recorded name is the report's. Correct.
- Deleting: `u20_remove_launch_reports` (`:427-440`) runs in `u20_cleanup` (`:1280`) and
  `u20_teardown_after_stop` (`:1318`), each name absent from `before/pingbus.list`.
- Before list: `u20_record_checkout` (`:313`) writes `find -mindepth 1 -maxdepth 3 -printf '%P\t%y'` from `.claude/ccy/pingbus/`, sorted, before M2.0 (`u20_record`, `:338`) and after
  `u20_remove_stale_reports` (`:296`); `grep -qxF "sessions/<name>\td"` (`:433`) is the exact
  line that listing produces for a report directory. With no `pingbus/` the file says `absent`,
  so everything recorded is deleted, which is right. `acceptance.bash:110` sets `pipefail`, so a
  failing `find` fails the record. Reliable.
- Failure: `rm -rf … || return 1` (`:434`) is `return 1` in `u20_cleanup` and `failed=1` in
  `u20_teardown_after_stop`; a launched run with no before list is a named `[FAIL]` (`:431`).
  A delete failure fails the run.
- The 00203 claim holds: M1 (`_acceptance-m1.inc.bash:29` via `m2_launch`), M2
  (`_acceptance-m2.inc.bash:129`), M3 (`_acceptance-m3.inc.bash:132`) and M4
  (`_acceptance-m4.inc.bash:214`) all start their sessions with `tmux -L ccy new-session -s <name>`, so each reports under its tmux name and `m2_cleanup` (`:200`) removes exactly those;
  the robust slice launches no session of its own. No step needed there.

### B3 (blocking). `u20_remove_launch_reports` deletes a live session's report when a slot is reused

`_acceptance-u20.inc.bash:432-435`.

Scenario. A ccy container is `--rm` (`claude-yolo:3887`) and is named by the lowest free slot
(`common.bash:1061-1094`: `<project>_yolo`, then `_1`, `_2`, …). acca, accb and accc end at
M2.5 (`u20_end_seats`), freeing their names, and the run goes on through M2.7 (accb relaunch),
M2.9 (plain) and M2.11 (named). The owner starts a session in the checkout in that window (the
design allows it; DESIGN M2.10 says the owner's sessions are untouched). It takes the lowest free
slot, which is one of the three just freed, so its keeper rewrites `sessions/<that name>/` at
start. At teardown the name is in `U20_REPORT_NAMES` and absent from `before`, so `:434`
removes the owner's running session's report. Reports are rewritten only at keeper start, and
the M5 picker's running-sessions view reads a running container's report, so that session is
gone from hot control for the rest of its life. The same happens for names recorded at `:294`
(leftover containers of an interrupted run, removed before M2.0): their slots are free for the
whole run.

The journal's stub-podman check ("an owner's report kept") covers only the owner whose report
was already in `before`, which is not this case.

Fix. Before `rm -rf` at `:434`, keep the report when a running container holds the name, as
`u20_remove_stale_reports` already does for `u20-named-*` (`:1131-1135`), with the mount check
so a same-named non-ccy container is not mistaken for one:

```bash
if podman ps --filter "name=^${name}$" --filter status=running --format '{{.ID}}' | grep -q .; then
    printf '==> %s is running (another session took that container name), so its report stays\n' "${name}"
    continue
fi
```

M2.10 then reports the surviving entry honestly and the run fails with a diff that names the
reason, instead of passing by deleting another session's record. Add that case to the stub run
(a name absent before, a running container of that name at teardown: the report stays).

### B2 verified

`CLAUDE/Plan/meta-deploy.list` has exactly three entries, in order:
`playbooks/imports/play-claude-yolo.yml`, `00203-teams-hot-control-and-robustness`,
`00161-agent-team-bus-matrix/acceptance.bash`. Each has a comment; the first matches M5's
scope (src-go only, so no ccy version bump or image rebuild); the second joins M5's text with the
bus-process-is-a-program rule and the stale `m1-plain-`/`m2-plain-`/`m2-nobus-`/`m3-hot-`/
`m4-a-`/`m4-b-` removal, which is `M2_OWN_NAMES` in `m2_remove_stale_reports`
(`_acceptance-m2.inc.bash:81-97`); the third describes B1 as coded. "No play" is gone. Done.

### Minor, non-blocking

- `grep -qxF` at `:433` returns 2 on a read error as well as 1 on no match, and both paths
  delete. `[[ -r "${before}" ]]` beside the `-f` check at `:431` closes it.
