# n2f part "health": Refresh under a held lock and the production adapters (HR1-HR5)

Branch `feat/bus-v2-n2f-health`. Commits: `0074995f` (host.ReadStamp), `8db840f0` (Refresh),
`ce7f9e2d` (adapters).

## What was built

- **`host.ReadStamp(root string, team paths.TeamID) (host.Stamp, bool, error)`**, which reads
  the stamp with `state.ReadFile` (the trust rule, 1 MiB) and decodes it with the existing
  strict `decodeStamp`. `host.Stamp{Config, Software, Tuwunel string; AppliedMS int64}`.
- **`(*health.Tick).Refresh(ctx, team paths.TeamID) error`** (HR1). It finds the team through
  `Teams.Stamped`, then runs gather and `locked`, which is the same path the tick takes after
  its lock: the stamp is checked again, the seq is checked, then step 6 and the write. It
  takes no lock. `facts.held` stops `homeserver` at the first missed probe, so `probeAgain`
  and `restart` are never reached (a test checks: one probe, no sleeps, no restart, probe
  `missed`, no event). A team with no stamp, a failed listing, a removed team, a changed seq
  and any failure are all errors.
- **`health.Stamps{Root string}`** (HR2, Teams). It lists the directories under
  `Root+paths.StatusRoot+"/teams"`. An entry is skipped when its name is not a team id or it is
  not a directory (a link counts as not a directory, because `ReadDir` does not follow it), or
  when it has no stamp. The name comes from `team.json`, decoded by `declare.DecodeTeamFile`,
  whose id must be the directory's. The origin is `migration` when `MigrationDir(t)/state.json`
  is a regular file (Lstat), `v2` when it is missing, and an error for anything else.
  `Stamp(ctx, team)` is `ReadStamp`'s found.
- **`health.StatusFiles{Root string; Tick *Tick}`**, which implements `host.Status`:
  `Refresh(ctx, team, first)` is `Tick.Refresh`. `Remove` (HR3) deletes `lxc-online.json` and
  then `status.json`, and answers `health.LineStatusRemoved` ("status record removed"), or ""
  when there was no record. A migration-origin record is refused and left in place.
- **`health.NoBackups{}`** (HR4) answers `BackupState{Newest: nil, LastRun: "none"}`. A test
  checks that the tick's record shows "the backup has not run since the team was applied".
- **`health.GuestsNotBuilt{}`** (HR5): every probe fails with `health.ErrGuestProbeNotBuilt`
  ("the LXC guest probe is not built yet"). A test checks what the tick records: the LXC
  member has `online: false` and the problem "the seat of <handle> could not be probed" (fix
  `journalctl -u teamchat-health.service`), and no disconnected notice is requested.
- Test fake change: the `backups` fake gained an `err` field.

## Coverage and QA

The QA passed: `go test -race -shuffle=on -cover`, `golangci-lint` with and without
`--build-tags tuwunel`, and `go vet`. Coverage: health 93.6% (it was 92.9% after Refresh),
health/view 93.1%, host 93.3% (not lowered). `files.go` and `refresh.go` are 86-100% per
function.

## Decisions (where SPEC-n2f is silent)

| ID     | Decision                                                                                                                                                                                                                                                                                                                                                                                                              |
| ------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| n2f-H1 | `Status.Refresh`'s `first` is ignored. `base` already gives the Team's origin to a record when there is no previous one, and `remove` deletes the record, so `first` adds nothing.                                                                                                                                                                                                                                    |
| n2f-H2 | A changed seq under Refresh is an **error**, not a no-op. While the caller holds the lock, no root writer may write, so a change means something is wrong. A no-op would leave the board showing the pre-apply record (stale members) while the verb reports success. The error fails the verb at `records` with the stamp already written, and a rerun or the next tick repairs it. A removed team is also an error. |
| n2f-H3 | Refresh runs step 6 (`lxc-online.json` and the announcer) as well as steps 1 to 5. SR1 requires `online` on every LXC member entry, and step 6 is what sets it. It runs under the caller's lock, which is the same lock the tick would hold. Under Refresh a missed probe is recorded `missed` with no re-probe, so a change verb never sleeps 20 s.                                                                  |
| n2f-H4 | `Remove` refuses an unreadable `status.json` (an error), as well as a migration-origin one, because the origin cannot be known. It removes `lxc-online.json` even when there is no record, but the CHANGED line names only the record.                                                                                                                                                                                |
| n2f-H5 | When `Stamps` finds a stamp, `team.json` or migration state it cannot trust, the whole listing fails (`RunAll` fails, exit 1), rather than skipping that team. These are root-only files, so the failure is loud, but it breaks HT1's per-team isolation for this case. A missing `teams/` directory means no teams.                                                                                                  |
| n2f-H6 | The guest probe's error text is not copied into the record. The problem names only the handle, because a real probe's error could carry arbitrary text into a field that must stay one line.                                                                                                                                                                                                                          |

## Proposed design-text fixes

- `host.Status`'s doc in `apply.go` says Remove "rewrites a migration-origin record to its
  pointer alone". HR3 refuses that record instead. The doc should say "refuses" until D's
  migrate verbs are built.
- HR1 should say "steps 1 to 6" (n2f-H3) and state the n2f-H2 rule.
