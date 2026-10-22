# n3a writer (Plan 00209 N3, slice n3a, part writer: MS2 and gap 4)

Branch `feat/bus-v2-n3a-writer`, from `d27d36a0`. Commits: `3f56d70c` (StoreWriter), `b2acbf62`
(gap 4). qa-changed green on both; golangci-lint 0 issues on `./internal/teamchat/...` with and
without `--build-tags tuwunel`.

## Built

- **`member/state/writer.go`** (MS2): `StoreWriter`, `ErrNoLock`.
  - `NewStoreWriter(root *homefs.Root, l *lock.SeatLock)`: a nil lock is `ErrNoLock`, a closed
    one `lock.ErrReleased`; opens `l.Seat().Dir()` by `root.Sub(.., homefs.Private)`, so a seat
    directory that is not the caller's or not 0700 is `homefs.ErrConfig`.
  - `Load()`: `Read(dir, seat.Store(), MaxStore, DecodeStore)`; missing is `fs.ErrNotExist`,
    another uid's file `homefs.ErrConfig`.
  - `Write(Store)`: `l.Held()` first, then the team check, then `s.encode()` (the decoder runs
    over the output) and one `dir.Replace(seat.Store(), data, 0600)`. A refused write leaves no
    file.
  - `Close()` closes the directory root only; the lock stays its holder's to close.
- **(a)'s list rules** were already in `store.go` from the state part (`AddPending`: agent items
  stop at 2048-256, HUMAN and TIMEOUT use the whole cap; every other list `ErrFull`; `ledger`
  and `missed` roll), and tested by `TestAddPendingKeepsHumanRoom` and
  `TestStoreListsRefuseOrRoll`. I left them as they were.
- **Gap 4**: `seat/release.go` `runOf` and `health/online.go` (`held`, `moveOn`) use
  `memberstate.DecodeLockRecord` and `LaunchFailed()`. I deleted `seat.LockRecord`,
  `seatLockRecord`, `seat.DecodeLockRecord` and `seat/lockrecord_test.go`. Every case it held
  is also in `member/state/lockrecord_test.go`. health no longer imports seat.
  `go list -deps ./internal/teamchat/member/state` lists only homefs, paths, lock, proto,
  exits and records, so there is no cycle. The record bytes that seat's and health's tests
  build are all accepted by the strict decoder (none has `"launch_failed_ms": null`).

## Coverage

| Package                          | Coverage |
| -------------------------------- | -------- |
| `internal/teamchat/member/state` | 99.1%    |
| `internal/teamchat/seat`         | 90.5%    |
| `internal/teamchat/health`       | 91.9%    |

## Decisions (with proposed design text)

- **`Load` also refuses a store of another team** (`CodeSeat`), the same rule `Write` applies,
  so a store renamed or copied into the wrong seat directory is never taken as this seat's.
  The team is the seat directory's base name, which holds for `Layout.Seat` and
  `Store.SessionSeat`. Proposed MS2 text: "The writer refuses to write, and refuses to load, a
  store whose `team` is not its seat directory's name (code `seat`)."
- **`member/state.Write` (the plain writer) is now tested** in `writer_test.go`
  (`TestWriteReplacesAMemberFile`, through `LockRecord` as the Encoder). go-qa's deadcode had
  failed qa-changed on it, because nothing called it yet. The codecs builder's files will call
  it as well; the two tests overlap without harm.
- The aliases are `memberstate` in seat and health, because health already imports
  `teamchat/state`.

## Notes for the next part

- When a link opens a store, it is `lock.TakeSeat` or `AdoptSeat`, then `NewStoreWriter(root, seatLock)`,
  then `Load` (`fs.ErrNotExist` means start from `NewStore`). Change the store only through its
  `Add*` methods and call `Write` after each change. Close the writer before the lock.
- `StoreWriter` keeps the seat directory open by fd. A rename of the directory while it is open
  (set-aside under a `SeatHold`) does not move where it writes, so a writer is only used under
  its held lock, which a hold excludes.
