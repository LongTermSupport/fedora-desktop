# Audit: durability (backup, restore, carry, health, clock, failpoint, reexec)

## Scope

Read in full from the working tree, under `src-go/internal/teamchat/`: `backup/take.go`,
`backup/meta.go`, `backup/list.go`, `backup/units.go`; `restore/engine.go` (Run, Resume, Discard,
swap, start, clean, revert), `restore/fs.go`, `restore/guard.go`, `restore/records.go`,
`restore/epoch.go`, `restore/unproven.go`; `carry/seal.go`, `carry/check.go`, `carry/drill.go`,
`carry/guard.go`, `carry/import.go`, `carry/importrun.go`, `carry/stage.go`, `carry/export.go`
(all but lines 340-463); `health/health.go`, `probe.go`, `online.go`, `reach.go`, `leaf.go`,
`refresh.go`, `restores.go`, `host.go`, `files.go`; `clock/clock.go`; `failpoint/*.go`;
`reexec/reexec.go`; and, as the code these depend on, `state/state.go`, `teamlock/teamlock.go`,
`hostnet/probe.go`, `admin/wire/wire.go`, `unitfiles/unitfiles.go:28-174`. SPEC-carry.md
sections 0-8 were read against the code.

**Not read** (so nothing below vouches for them): `backup/tar.go`, `carry/archive.go`,
`carry/manifest.go`, `carry/records.go`, `carry/lost.go`, `carry/child.go`, `restore/merge.go`,
`restore/renames.go`, `restore/validate.go`, `restore/record.go`, `restore/argv.go`, the body of
`health/view/view.go`, `clock/fake.go`, and every test file's body. The migrate verbs: the files
read say they are not built (`carry/seal.go:198`, `health/files.go:136`); the other `migrate`
grep hits (`declare/input.go`, `apply/member.go`, `admin/run.go`, `admin/wire/lxc.go`,
`status/status.go`, `restore/validate.go`) were not opened.

Tests run: `go test` over `backup`, `restore`, `carry`, `health`, `health/view`, `clock`,
`failpoint`, `reexec`: all pass (default build only; not `-race`, not the `busfailpoints` or
`tuwunel` tags). No finding below was reproduced by a new test: each is from reading the code.

Severity counts: CRITICAL 0, HIGH 4, MEDIUM 6, LOW 6.

## Suspend and resume: what the code does (owner's hard requirement)

- **The health tick does not misjudge probes across a suspend, as far as the code shows.** The
  re-probe waits are Go runtime timers (`admin/wire/wire.go:336-338`, `clock/clock.go:59-68`) and
  the probe's 5 s bound is an `http.Client` timeout (`hostnet/probe.go:22`, `:67-69`); both run
  on the monotonic clock, which does not advance in a guest S3 sleep. Three misses need three
  failed loopback GETs over about 35 s of awake time (`health/probe.go:15-19`, `:157-173`). The
  timer is monotonic too (`OnUnitActiveSec=60s`, `unitfiles/unitfiles.go:168-170`), so it does
  not fire a burst of ticks on resume.
- **The one wall-clock comparison in the restart decision is the start grace**
  (`health/probe.go:72`): see DUR-11.
- **What can restart a healthy homeserver after a resume is not the tick but systemd's
  watchdog**: see U1 (unconfirmed, and the most important thing to test in the lab).
- Lock waits use `time.Now()` values that carry a monotonic reading
  (`teamlock/teamlock.go:214-226`), as does the backup's 60 s wait (`backup/take.go:206-221`), so
  neither is cut short by a wall-clock step.

## Findings

### DUR-1 (HIGH): backups are taken daily and no shipped verb can restore one

- `restore/unproven.go:3-5`, `admin/wire/wire.go:64-66`, `:157-162`.
- `restoreProven` is a struct field that `production()` (`wire.go:69-77`) never sets, and nothing
  else sets it outside a test (`admin/wire/wire_test.go:64`). There is no flag, file or build tag:
  enabling restore is a code change. So every restore verb answers 70 on every host.
- The backup is two halves on the homeserver's own filesystem: Tuwunel's RocksDB backup (7 kept,
  `host/render.go:156-157`) and root's `<n>.tar`. Nothing copies either off the machine (DC14).
- **What the owner is left with** after a corrupt database or a bad change: the files, and no
  procedure. `docs/` holds no page on restoring a v2 team by hand (searched `docs/*.md` for
  `restore`: only v1 and ccy text). The health record meanwhile reports the backup as healthy
  (`health/health.go:263-280`), which reads as "recoverable".
- **Fix.** Either run H-C5 and H-C15 and enable the engine, or ship the smallest proven path
  (offline: stop, `--restore-backup`, start) behind its own check. Until then the status record
  should say that restore is not enabled, and a doc must give the manual steps.
- Known: W7 section 7 states the refusal. Not known: that no switch exists, and that the status
  record presents the backups as good.

### DUR-2 (HIGH): `team seal` is terminal, and does not prove first that the export will succeed

- `carry/seal.go:165-214` (validation), `:95-127` (run); `carry/export.go:470-508` (the
  homeserver tree's rules); SPEC-carry SC8, SC9.
- The seal validates `ctl/`, `ctl/guest/` and `admin/backups/` (`Admissible`, `seal.go:210`).
  It does not walk the homeserver tree, which is where the export is strictest: every directory
  must be mode 0700 and every file 0600 (`export.go:545`, `:571-572`), every name must fit the
  member grammar (`export.go:490`), nothing may be a link or a socket (`export.go:500`), and the
  whole must fit the archive cap (`export.go:175`). Each of these is found only by
  `team export`, after `sealed.json` is written, the units are stopped and disabled, and the
  stamp is gone.
- There is no `unseal`. SC9 says recovery is "the operator's rollback store", which this
  repository neither makes nor checks for.
- **Scenario.** The team that is about to move to a VM is sealed. The export refuses (one file
  of another mode, or the tree is over the 1 GiB default). The team is now down on the source,
  `team apply` refuses it, and the only verbs left are export, seal and remove.
- **Fix.** Run the export's survey and `fits` as part of the seal's validation, before
  `sealed.json` is written (names, modes, kinds and sizes can all be judged on the live tree).
  Make the seal refuse unless a drill export of the same team succeeded recently, or add a
  `team seal --check` that does everything but write.

### DUR-3 (HIGH): the carry does not know about LXC members

- `carry/seal.go:165-214` (no LXC clause; `grep -i lxc carry/*.go` finds nothing outside tests),
  `paths/carry.go:61-76`, SPEC-carry.md:223 ("`LXCRecord(t)` is not carried (C 3.9, not built)").
- That SPEC line is stale: the LXC record is written and read in production
  (`host/lxc/lxcstate/lxcstate.go:249`, `:272`, `admin/rotate.go:136`).
- The seal neither refuses a team with LXC members nor carries their record. The guest token
  copies (`ctl/guest/*`) do travel. On the target, `LXCRecord(t)` must be absent for the import
  (`paths/carry.go:69`, `carry/import.go:213-223`), and the roster still lists the LXC members.
- **Scenario.** A team with LXC members is sealed and imported on a VM. Its LXC guests are on the
  old host, and the code requires them on the homeserver's host (W3). Their seats hold the old
  bridge url. The target's health tick probes guests that do not exist there, every tick
  (`health/online.go:56-74`, `:188-190`). What the old host's LXC units do with a sealed team's
  record was not read.
- **Fix.** Decide and write the rule: either the seal refuses a team with a present LXC member
  ("return or retire them first"), or the carry defines what happens to them on both hosts.
  Correct SPEC-carry.md:223. This is W3 and W4 meeting the carry; neither names it.

### DUR-4 (HIGH): one unreadable team stops the health tick and the backup of every team

- `health/files.go:27-55` (any error from `ReadStamp`, `team.json` or the origin returns for the
  whole list), `health/health.go:165-169`, `admin/backupverbs.go:76-77`.
- `RunAll`'s own doc says "a team's failure is its own outcome and never stops another's (HT1,
  DC7)" (`health/health.go:163-164`). That holds only after the list is built.
- **Scenario.** One team's `team.json` is truncated, or its stamp has the wrong owner. `health --all` fails before any team is ticked: no status record, no bounded restart and no leaf
  renewal (X31) for any team on the host, every minute, until someone reads the journal.
  `backup --all` fails the same way, so no team is backed up either.
- **Fix.** `Stamped` returns the good teams and a per-team error list; the tick and the backup
  record a failed outcome for the bad team and carry on.

### DUR-5 (MEDIUM): the tick's restart can outlive its own unit's timeout and then leaves no trace

- `unitfiles/unitfiles.go:140` (`TimeoutStartSec=120`), `:46` (`TimeoutStopSec=330`),
  `units/units.go:73` (`JobWait` 10 min), `health/probe.go:189-215`.
- `restartLocked` waits for the restart job and writes the event only afterwards. A homeserver
  that answers its watchdog but not HTTP can take up to 330 s to stop. With the 35 s already
  spent on probes, systemd kills `teamchat-health.service` at 120 s.
- **Result.** The restart still happens (the job is systemd's), but "restarted after three
  missed probes" is never written, and a job restart does not raise `NRestarts`, so the next
  tick sees nothing: a silent restart. The kill also ends every other team's tick in that run.
- **Fix.** Write the event before asking for the restart (or use a no-wait restart and let the
  next tick confirm), and bound each team's tick below the unit's timeout.

### DUR-6 (MEDIUM): every probe error is a "missed probe", and restarts have no backoff

- `health/probe.go:68`, `:168` (`Probe(...) == nil`), `hostnet/probe.go:58-64`, `:99-120`.
- `Prober.Probe` also fails when `team.json` cannot be read or names a bad port, and on any
  non-200 answer. All three count as the homeserver not answering.
- Nothing counts consecutive restarts. After a restart the unit is inside its start grace for
  one tick, so a cause the restart cannot fix restarts a serving homeserver about every second
  tick, for ever, dropping every member's sync each time.
- **Fix.** Separate "could not probe" (a problem on the record, no restart) from "did not
  answer". Record consecutive tick restarts in the status record and stop after a small number,
  with a problem that says so.

### DUR-7 (MEDIUM): a carry record is invisible in the status record, and the fix it names is wrong

- `health/health.go:225-259`, `health/probe.go:62-66`, `:126-129`; no reference to
  `carry.Recorded` anywhere in `health/` outside tests.
- A drill killed after its stop leaves the team down with `drill.json` (SD4). A seal killed at
  step `sealing` leaves the stamp, stopped units and `restore.block`. In both the tick writes
  "the homeserver unit is inactive", fix "systemctl status ...". The real fix is
  `team export --team T --drill` or `team seal --team T` again. The restore record is surfaced
  (`health/restores.go:24-36`); the three carry records are not.
- **Fix.** Give the tick a `Carries` seam beside `Restores` and put the record's own "way on"
  text (`carry/guard.go:355-367`) on the status record.

### DUR-8 (MEDIUM): the carry has no operator documentation and its real-server proof is in no gate

- `docs/` has no page for `team seal`, `export`, `import` or `carry-check` (searched
  `docs/agent-bus*.md`). SPEC-carry.md:3-8 lists as pending: the real-Tuwunel carry test
  (item 15), host check SH1 and the drill on a scratch guest (item 16), and the lab scenario.
- `admin/carry_tuwunel_test.go` exists but carries the `tuwunel` tag, which no gate runs as a
  test (`src-go/CLAUDE.md`, Build tags: the tagged stage runs project rules only).
- SH1 (`SPEC-carry.md:590-609`) is the premise of the import's first step: systemd must take a
  root-owned tree over to the dynamic user at first start. It is marked "not verified".
- **Fix before the estate's move.** Run SH1 and a full drill (export, import `--drill`,
  carry-check) on a scratch guest, record the result in the plan, and write the operator page
  with the order: exclude the team from the first play run, import, carry-check, apply.

### DUR-9 (MEDIUM): `carry-check` proves less than its name says

- `carry/seal.go:165-181`, `carry/check.go:140-166`.
- The tail (each room's newest event) is read while the server is live, before the stop, and
  members are not stopped from sending. The check then asks only that those events exist on the
  target and that the roster event's rev and epoch match.
- Events written between the tail read and the stop are in the copy but are never checked. A
  copy that lost them, or lost anything other than those few events, passes with `ok: true`.
  The archive's per-member digests do cover the bytes; the check adds little beyond "the
  database opens".
- **Fix.** Read the tail again after the stop is not possible (the server is down), so either
  say plainly in the answer what was checked, or have the check compare a count or the newest
  event per room against a value the source recorded at its last served moment.

### DUR-10 (MEDIUM): the database half of a backup is never verified

- `backup/take.go:203-229` (waits for a meta file with a whole first line), `:239-257` (reads
  back the tar only), `backup/list.go:24-47` (`Newest` is "meta and tar both exist").
- No step asks RocksDB to verify the backup it wrote, and the tick does not validate the tar
  (`List` does, `list.go:107-126`; `State` does not). With restore refused (DUR-1), no code path
  ever opens a backup, so a backup that cannot be restored would be reported healthy for ever.
- **Fix.** Verify on a schedule (the staged restore of DUR-1 is the real proof); at least make
  `State` use the same "usable" rule as `List`.

### DUR-11 (LOW): the start grace compares a monotonic-free systemd time with the wall clock

- `health/probe.go:72`, `units/units.go:646-654` (`ActiveEnterTimestamp`, a realtime value).
- A wall-clock step between the unit's start and the tick lengthens or shortens the 60 s grace.
  A forward step (clock sync after a VM resume) can end the grace early. Use
  `ActiveEnterTimestampMonotonic` against `CLOCK_MONOTONIC`.

### DUR-12 (LOW): the leaf-renewal restart rule is wall-clock too

- `host/leaf.go:210-211` (`t.Since.Before(madeAt)`), fed from `health/leaf.go:35`.
- A terminator started before a backward clock step looks newer than a leaf made after it, so
  it is not restarted and keeps serving the old leaf. Edge case; compare by reading the served
  certificate instead (the reach probe already connects).

### DUR-13 (LOW): the health wiring leaves `restore.Records.OwnerUID` unset

- `admin/wire/wire.go:111` (`restore.Records{Root: root}`), against `restore/records.go:35-38`.
- Every other adapter is given `stateOwnerUID`. On a host both are 0, so production is right;
  in a wire test the two differ, so the tick's read of a restore record cannot be tested through
  this wiring.

### DUR-14 (LOW): the import and export do not check free space first

- `carry/importrun.go:172-199`, `carry/export.go:194-225`.
- An import needs the archive plus the staged tree on one filesystem; an export needs the tree
  plus the archive. A full disk fails cleanly (the part or the staging is removed), but only
  after most of the copy. A statfs check against the manifest's sizes would refuse at once.

### DUR-15 (LOW): the import's age rule depends on two hosts' wall clocks

- `carry/import.go:194-208`, checked again at the commit point (`carry/importrun.go:541`).
- A target VM whose clock is stale after a resume refuses a fresh export as "ahead of this
  guest's clock", or accepts an old one. It fails safe; the refusal should name clock sync as a
  possible cause.

### DUR-16 (LOW): `reexec` is a test helper in a non-test package

- `reexec/reexec.go:27` imports `testing` from a non-`_test` file; no production package imports
  it (grep). `start` waits for the ready line with no bound (`:246`), and `sigkill` signals a pid
  that may already be reaped (`:455`). Test-only risk.

## Unconfirmed

- **U1 (would be HIGH): systemd's watchdog may restart a healthy homeserver after a resume.**
  `WatchdogSec=30s` with `Restart=always` (`unitfiles/unitfiles.go:44`, `:50-53`). The watchdog
  runs on the guest's monotonic clock. A guest S3 sleep does not advance it. A VM paused by the
  hypervisor, or a host that suspends under a running guest, can make the guest's clock jump on
  resume, and then the watchdog can fire before Tuwunel has pinged. I could not test this here,
  and it depends on the hypervisor's clock handling. **Test in the VM lab**: record `NRestarts`
  of `teamchat-server@<team>`, suspend the bus VM for longer than 30 s by each method the estate
  uses, resume, and read `NRestarts` and the journal. The same jump would also fire
  `teamchat-health.service`'s 120 s timeout.
- **U2: the backup's 60 s wait may be too short for a large database.** `backup/take.go:50`,
  `:221-223`. If Tuwunel takes longer than 60 s, every run is recorded failed while Tuwunel does
  finish, leaving a meta with no tar. Not measured; needs a timing on a real team's database.
- **U3: a media file name could be refused by the export.** The member grammar requires a first
  character in `[A-Za-z0-9_]` (SPEC-carry SX4, `carry/export.go:490`). If Tuwunel stores media
  on disk under names that can start with `-`, a team with uploads would hit DUR-2. I did not
  check how the pinned Tuwunel names media files.
- **U4: what the source host's LXC units do with a sealed team** (`host/lxc/restart.go:123` reads
  LXC records). Not read.

## Bloat and debloat candidates

- **`restore/` is about 2,900 lines of production code and 3,800 of tests that no host can run**
  (DUR-1). It is either the next thing to prove, or it should leave the tree until it is. Its
  guard, `Leftovers`, `ReadApplied`, `FS` and `EpochFloor` are live (the carry uses them) and
  would stay.
- **Three near-identical directory makers**: `backup.backupDir` (`backup/take.go:278-301`),
  `carry.OS.MakeCarryDir` (`carry/export.go:667-686`) and `teamlock.lockDirectory`
  (`teamlock/teamlock.go:188-211`). One helper in `state`.
- **Two file-system adapters over the same tree**: `restore.OS` and `carry.OS`/`carry.StageOS`
  each have their own no-link open, stat, list, rename and remove-tree
  (`restore/fs.go`, `carry/export.go:589-745`, `carry/stage.go`). One fd-relative package would
  serve both.
- **Four clock interfaces with the same two methods** (`health.Clock`, `backup.Clock`,
  `teamlock.Clock`, `restore.Clock`), beside `clock.Clock`, which the host side does not use.
  The host side reads `time.Now()`; the member side reads `CLOCK_BOOTTIME`. One host clock type
  would also be the place to fix DUR-11 and DUR-12.
- `failpoint` and `clock` are small and do one thing each; nothing to remove.
