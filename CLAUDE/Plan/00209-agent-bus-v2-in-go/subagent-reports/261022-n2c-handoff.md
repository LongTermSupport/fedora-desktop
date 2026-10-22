# N2 slice n2c: handoff 1

The builder stopped at its context budget. Worktree `/workspace/untracked/worktrees/bus-v2-n2c`,
branch `feat/bus-v2-n2c` (stacked on `feat/bus-v2-n2a` at f3aa1bb0). Read the brief
(`untracked/scratch/n1-common.txt` in the main checkout, task name "n2c"), then
[SPEC-n2c.md](../SPEC-n2c.md), which is the slice's contract.

## Done and committed (tip 88f467ae)

- `SPEC-n2c.md`: SN1, SR1-SR3, SW1-SW3, SL1, SC1-SC3, ST1-ST7, SX1-SX6, SD1-SD4, SP1-SP4, and
  decisions n2c-D1 to n2c-D7.
- `teamchat/paths` (SN1), green at 100% coverage: `SeatName` (the whole handle, n2c-D1),
  `ClaimID`, `SeatDevice`, `Store.PoolTeam`, `Store.PoolSeat` (type `PoolSeat`: `Password`,
  `Token`, `Bundle`, `ReleasedRecord`, `Held`, `HeldRecord`, `HeldToken`, `HeldBundle`,
  `HeldNotice`), `Store.PoolClaim` (a `PoolSeat`), `Store.SessionSeat` and
  `Store.ReleasedSession` (each a `paths.Seat`, so `.Lock()`, `.Token()`, `.Bundle()` name the
  files), `Store.ReleasedPool`, `Layout.SeatClaim` (a `Seat`), `GuestTokenCopy`.
- `qa-changed.bash` was red only on GO-2026-6809.

## Drafted, not committed: `src-go/internal/teamchat/seat`

The drafts are in the worktree's ignored `untracked/scratch/n2c/`, each named `<file>.draft`.
Move them back to `src-go/internal/teamchat/seat/<file>` (test files first, for the TDD hook).

- `records.go` + `records_test.go` (SR1-SR3): `Bundle` (pool seat.json), `HeldBundle` (embeds
  it, adds `Claim`, `ReleasedMS *int64`), `HeldRecord`, `ReleasedRecord`, `NoticeRecord`,
  `Secret` (redacted formatting, refuses marshalling), `Kind` (`password`, `token`), `Issued`
  with `Encode`/`DecodeIssued`. `decodeStrict` = size cap, `proto.Parse`, required keys, then
  `encoding/json` with `DisallowUnknownFields`.
- `fs.go` + `fs_test.go` (SW1-SW3): `dir`, a root held by fd; every path opened by `openat2`
  with `RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS|RESOLVE_NO_MAGICLINKS`; `create`, `replace`, `read`,
  `exists`, `mkdirAll`, `mkdirFresh`, `checkEmptyOwned`, `entries`, `unlink`, `rmdir`,
  `rename(from, to, unix.RENAME_*)`.
- `helpers_test.go` (basic helpers); `world_test.go.draft` is the fuller helper set the store
  tests need (a fakehs world, a fake clock, fake `Sessions` and `Announcer`), and
  `store_test.go.draft` the tests of `Open` and the allocation lock. Merge the two helper files
  when the store lands.

Never compiled or run: the lint hook's last report on them, to fix first:

- `tagliatelle` wants camelCase json: add a `.golangci.yml` override for
  `internal/teamchat/seat` (`json: snake`), as the matrix package has, with the reason "C 3.7.2's
  records are snake_case files";
- `gosec` G115 on `uint64(flags|...)` in `dir.open` (build the flags as `uint64` constants);
  G304 on `os.ReadFile(path)` in a test helper; G301/G302 on the 0755 modes the tests set on
  purpose (make the mode a named constant via `os.FileMode` arithmetic, or chmod through
  `unix.Chmod`);
- `govet` shadow of `err` in `records_test.go` (lines 78, 163, 173); `prealloc` (line 199);
  `gocognit` 21 on `TestRecordsRoundTripAndRefuse` (split it);
- `goconst` "claim"; `dupword` in the `rename` comment; a package comment (put the package doc in
  `seat.go`, below); `unparam` on `testBundle`'s base (fixed once the world tests use it).

## Left to build (spec section in brackets)

The API the drafts assume (from `world_test.go.draft` and `store_test.go.draft`):

- `seat.go`: package doc; `Config{StateHome, RuntimeDir, Checkout, Clock, Credential, Sessions, Announcer}`; `Open(Config) (*Store, error)` (fields `store paths.Store`, `checkout`, a root
  `*dir`), every missing part `ErrConfig`; `Clock{Now; Sleep(ctx, d)}`;
  `Sessions{Restorable(paths.SessionName) (bool, error)}`; `Seat{Team, Name}`; errors `ErrBusy`,
  `ErrNotLocked`, `ErrHeld`, `ErrReissue`, `ErrReturn`, `ErrRetired`, `ErrIncomplete`,
  `ErrLeftover`.
- `lock.go` (SL1): `Store.Lock(ctx) (*Alloc, error)`, `Alloc.Unlock`, `allocWait` 30 s,
  `allocPoll` 100 ms, `flock(LOCK_EX|LOCK_NB)` on `paths.AllocLock`, the directory 0700.
- `credential.go` (SC1-SC3): `Credential{Kind() Kind; Mint(ctx, Bundle, Secret) (Secret, error); Retire(ctx, Bundle, Secret) error}`; `Password{Clock matrix.Clock}`: login as `TCSEAT`, logout
  with that token, login again (`M_FORBIDDEN` on the first is `ErrReissue`); `Retire` logs out,
  `M_UNKNOWN_TOKEN` is done. A signed-out member client needs a placeholder token (login is
  `noAuth` in the matrix client).
- `take.go` (ST1-ST7): `Store.Take(ctx, *Alloc, Take{Seat, Session, State HandleState}) (Taken{Claim, ...}, error)`, `StateActive|Parked|Lost|NeedsReissue|Retired`.
- `release.go` (SX1-SX6, SD1-SD4): `Store.Release(ctx, Release{Seat, Session, Leaving})`,
  `Store.Announce(ctx, Seat, Session)`; `Announcer{Disconnected(ctx, Notice{Bundle string, SinceMS int64, Run paths.RunID}) (Outcome{Posted bool, Why string}, error)}`; the L3 probe of
  `paths.Seat.Lock()` with `LOCK_SH|LOCK_NB` and B 2.5's lock content.
- `place.go` (SP1, SP2): `Store.Claim(*Alloc, Seat) (*Claim, error)`, `Claim.Abandon`,
  `Claim.Fill(Issued)`, `Store.QuarantineLeftovers`.
- `guest.go` (SP3, SP4): `PlaceGuest(paths.Layout, Issued)`, `KeepGuestToken(base, team, seat, Secret)`.
- `tuwunel_test.go` (`-tags tuwunel`, ST6): as `rooms/tuwunel_test.go` does, through
  `matrix/tuwunelhs`, server name `paths.ServerName(team)`: register the admin, create the seat's
  account, place, keep a stale `LoginDevice` token, take, show the stale token dead, release,
  show the take's token dead.

Then the review loop (Opus, at most 3 rounds), the report `subagent-reports/261022-n2c.md`, and
Task 2.4's progress line in PLAN.md (this slice: C 3.7.1-3.7.3's store, take, release, the
notice's inputs and placement; not the hot reissue, 3.7.4, 3.7.5's control record, C 3.8, 3.10,
3.11, 3.12). The report must say how 7.1 swaps in (n2c-D7) and list the design-text fixes: B's
header for host `cc` against C 3.7.1 (n2c-D3), B 2.11's host `since` against `held.json`'s
`since_ms`, `<seat>` collisions across member types (n2c-D1), and the release resume's run id
(n2c-D5).
