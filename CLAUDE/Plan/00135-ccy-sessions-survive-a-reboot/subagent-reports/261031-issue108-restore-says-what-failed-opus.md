# Issue #108: a restore that fails says so, and the cycle holds the reboot

Branch `fix/108-restore-says-what-failed`, from 2b2e2af9d. CCY 3.104.15 (3.104.14 went to issue #109
on the coordinator's branch); container unchanged.

## Owner decisions applied (Fable review, 2026-10-31)

1. A record with no `conversation.json` is still refused (Plan 00203 Decision 10 stands).
2. The self-update cycle holds the reboot, and says so, when any record cannot be restored.
3. Failures are loud through `verify-restore`.
4. Resuming by the recorded id is intended; documented, no record field.

## Built (tests first; red seen for each before the code)

- `src-go/internal/restore`: `GoingNotStarted` (`not-started`), `OutcomeNotStarted`,
  `StateNotStarted` (`NOT-STARTED`). `Judge` returns it before anything else, whatever runs
  under the name. `Merge` writes a held not-started entry as `none` when a later restore finds
  the session running.
- `src-go/internal/sessions/restore.go`: `planRestore` is the one judgement of whether a record
  can start (directory, conversation, arguments), shared by the restore and the check.
  `restoreRun.notStarted` is the one place a refusal is counted, and it also writes the
  manifest entry. Unreadable records take their file's name.
  `restore --check` prints `<name> RESTORABLE` / `<name> UNRESTORABLE <why>`, exit 1 naming
  each refusal. It also judges live sessions, starts nothing, asks tmux nothing and writes no
  manifest. With the restore unit off it exits 0 with a note. `restoreEnabled` is shared with
  `goingDownKind`.
- `verify.go`: lists NOT-STARTED; if any appear, it names the restore unit's journal.
  set-going leaves not-started entries alone.
- `files/var/local/claude-yolo/lib/session-registry.bash`: the manifest reader accepts
  `not-started` (shared format; no line growth, bash-limits ratchet held).
- `helpers/self_update/cycle.py`: `Host.check_restore` (`ccy-sessions restore --check` as the
  user). `_warn_and_reboot` runs it before the first warning. On non-zero the cycle finishes
  `unrestorable-sessions`, phase `warn`, exit 26 (`EXIT_UNRESTORABLE`), announced, no reboot.
  The owed marker stays, so the next cycle asks again. `published.py` classifies the outcome
  as failed. DESIGN-cycle.md documents the exit code and the outcome.
- Tests: `restore_test.go`, `verify_test.go`, `setgoing_test.go`, `manifest_test.go` (with a
  bash round-trip), `verdict_test.go`, `tests/helpers/self_update/test_cycle.py`, and
  `scripts/test-self-update-cycle.bash`, which gained a stub for `restore`, a hold case, and
  the check in every warn call list (223 passed).

## Defence Before Fix

The class is "a failure dropped from the record a later verdict reads, so the verdict reports
success". No analyzer can express it: which record a later program reads, and which branch is
a failure it must see, is meaning, not syntax. The entry, with what holds it, is in
CLAUDE/GoProjectRules.md under "Defect classes that cannot be a rule".

The sweep read `internal/sessions`, `internal/restore` and the cycle:

- Instances: the four refusals in `restoreRun.record` and the unreadable records. All are
  fixed.
- Not instances: set-going records every session it leaves alone; verify fails on a probe it
  cannot make; the cycle records every outcome before it announces it.
- Left as designed: a second same-boot restore that starts nothing at all leaves the first
  restore's manifest in place. Its unit still fails.

## Deploy

- `play-claude-yolo.yml`: the existing meta-deploy entry for 3.104.12-13, with its comment
  extended to cover 3.104.15.
- `play-self-update.yml`: not queued. It deploys the wrapper and the units, and none of them
  changed. The cycle's Python is imported from the deploy clone, which the cycle itself
  advances to signed commits. The play never updates an existing clone (`update: false`), so
  running it would change nothing.
- The first cycle that pulls this commit still runs the old cycle code, because it was loaded
  before the update. The check applies from the cycle after that one. A ccy-sessions older
  than 3.104.15 answers `--check` with a usage error (64), which holds the reboot loudly
  rather than passing it.
