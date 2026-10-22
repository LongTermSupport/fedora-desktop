# n2g handoff: backups and restore (Plan 00209 Task 2.4, C 3.12)

Worktree `/workspace/untracked/worktrees/bus-v2-n2g`, branch `feat/bus-v2-n2g`, cut from
`feat/bus-v2-n2` at f7d832be. The builder rules are `/workspace/untracked/scratch/n1-common.txt`.
Report names: `261022-n2g.md`, `261022-n2g-review.md` (`-2`, `-3`).

## Done

- **Research.** C 3.12 was read in full, with 3.2.2, 3.2.3, 3.6.1, 3.11.3, 3.14's row, H-C4,
  H-C5, H-C15 and DC8, and the existing APIs (host, units, state, teamlock, admin, admin/wire,
  ctl, apply files, health, status).
- **Tuwunel spike.** The pinned 1.9.4 binary was run in the container. The digest is
  `untracked/scratch/n2g-tuwunel-spike.md` in this worktree (untracked), and the spec's preamble
  holds its facts.
- **`SPEC-n2g.md` is written.** It covers BK, BT, HB, BV, RM, RV, RS, RC, RU, RG, RW and RT, with
  decisions n2g-D1 to D18. It is committed, but **not yet reviewed**.

## Left, in order

1. **Spec review.** One Opus adversarial review, at most 2 rounds. Write it to
   `subagent-reports/261022-n2g-spec-review.md`, fix the spec, and commit.
2. **Build TDD**, in this order. Each step is green and committed before the next.
   1. teamlock: the Guard (RG1), with every caller updated: host.Apply and host.Remove through
      `Deps.Guard`, admin's memberVerb, and health's TeamLocks with `Unguarded`.
   2. units: `Kill` (SD10) and the restore unit names. `paths` gains `RestoreUnit`,
      `RestoreSecureUnit`, `RestoreRecord`, `RestoreConfig`, `BackupTar(t, n)`, `BackupDir`,
      `LastRunRecord`, `LostExport(t, n)` and `HomeserverPrivateBackups`.
   3. `backup` package: the tar (BK), Take (BT), States (HB1).
   4. host: `Serve` exported (start, ready and addresses, refactored from `applying`), and
      `BaseSandbox` data. `Status.Refresh` takes events.
   5. `restore` package, the pure parts: the record codec, Merge (RM), validation (RV), the unit
      argv (RU1, RU2), `Records` (HB2) and `Guard` (RG2).
   6. ctl and admin: the child ops `restore.secure`, `swap`, `export`, `finish` and `revert`
      (RC), the Answer gaining `Export`, and `checkStarted` (RC7).
   7. `restore` engine: Run, Resume and Discard (RS), over fakes, with a fault and a kill at every
      step.
   8. admin verbs `backup`, `backups` and `restore` (BV, RS14, RS15), then health (HB2, HB3) and
      wire (RW1, RW2).
   9. Tuwunel-tagged tests (RT1, RT2) with
      `FAKEHS_TUWUNEL_BIN=/workspace/untracked/worktrees/bus-v2-n1-hostchecks/untracked/scratch/tuwunel-dl/tuwunel-1.9.4`.
3. **QA.** 90% coverage per package, `-race`, and golangci clean with and without
   `--build-tags tuwunel`. `./scripts/qa-changed.bash` must be fully green (go1.26.10 is in the
   container).
4. **Code review.** An Opus adversarial loop over the code, at most 3 rounds, then a narrow
   check. Commit each review file.
5. **PLAN.md and report.** PLAN.md Task 2.4 records backups and restore as inert code, in the
   same commit as the code; then the report.

## Gotchas

- **Tuwunel's quirks.**
  - A `SIGUSR2` before Tuwunel's handler is installed kills it, so always check ready first.
  - `--restore-backup` wipes a target directory that is not empty, so the staging path must be
    fresh (RV2).
  - The restored server keeps serving, so the staging unit has to be stopped.
- **`checkStarted`** requires `PATH` alone, `/` and no groups. systemd adds variables and the
  `initgroups` groups, hence `env -i` and RC7.
- **The bootstrap after a restore** cannot re-run `host.Apply`. `team.json` lacks the LXC and
  `Address` parts of the input, so a re-render would differ. Use `host.Serve` and then the
  child's `team.apply` with `Input{Team: team.json}`, which only reads `in.Team` (`apply.Run`).
- **Health record fields.** `status.Record.Restore` exists and is kept "as written" by the tick
  (`health.base`); HB2 makes the tick fill it.
- **Hooks and tooling.**
  - `sed` is blocked; use `awk 'NR>=a && NR<=b'` or Read.
  - Pipes to `head` or `tail` are blocked.
  - The TDD hook wants each test file written before its source file.
- **Import cycles.**
  - health must not import restore or backup; they satisfy health's interfaces structurally.
    backup may import health, for `BackupState`.
  - restore must not import admin. It declares its own Child-side interfaces, and admin
    implements them.
