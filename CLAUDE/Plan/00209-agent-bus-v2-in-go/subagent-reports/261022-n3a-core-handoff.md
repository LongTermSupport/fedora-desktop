# n3a core handoff 1 (Plan 00209 N3, slice n3a, part core)

Worktree `/workspace/untracked/worktrees/bus-v2-n3a-core`, branch `feat/bus-v2-n3a-core` (cut from
fea926e0). Read `/workspace/untracked/scratch/n3a-part-common.txt` (the part rules) and
`subagent-reports/261022-n3a-handoff.md` (the slice design, steps 01-11) first. This file is what
the core part has done and what it has learned; the next builder continues at step 04.

## Done (each committed green; qa-changed red only on GO-2026-6809)

- `d35f30a5` step 01, gap 8: `internal/teamchat/reexec` (92%). `reexec.Main(m, helpers)` in
  `TestMain`; `reexec.Helpers` maps a name to `func(*reexec.Helper) int`; the helper does its work,
  calls `h.Ready()` (prints `ready <pid>`, waits for the release line or stdin's end) and returns
  its exit code; `h.Args` are Start's arguments. Parent: `p := reexec.Start(tb, name, args...)`
  returns once ready; `p.Release()` (exit 0 required), `p.Kill()` (SIGKILL, waits), `p.Pid`; a
  cleanup kills a helper left held. A pid is signalled only after `/proc/<pid>/stat` shows it is
  this process's child (a helper whose first line is not its ready line is ended by context
  cancel, never by a signal to a claimed pid: an early version sent SIGKILL to pid 0 and killed
  the test's process group). Children get `GORACE=... atexit_sleep_ms=0` (a `-race` child
  otherwise sleeps 1 s at exit). `dynamicNames` row `{internal/teamchat/reexec, start}` in
  `src-go/qa/rules/table.go` (rules tests pass).
- `189990e6` step 02: `internal/teamchat/member/homefs` (93%). `homefs.Open(path, Private|Owned)`
  (Private: the euid's, 0700; Owned: the euid's, no group/other write, for a home and
  `~/.config`); methods take FULL paths below the root (as `paths.Seat(...).Store()` gives):
  `Path`, `Close`, `Sub(full, need)`, `MkdirAll` (0700 by fchmod, existing must be the uid's
  0700), `Mkdir` (fresh), `Create(full, data, perm)` (O_EXCL, fchmod, fsync, dir fsync),
  `Replace(full, data, perm)` (temp `.<name>.new`, unlinked first, rename, dir fsync; never
  follows a link at full), `Read(full, limit) (homefs.File{Data, Dev, Ino, Perm}, error)`
  (O_NONBLOCK, regular, the uid's, cap; missing is `fs.ErrNotExist`), `OpenLock(full) (*os.File)`
  (O_RDWR|O_NONBLOCK, made 0600 by fchmod when created, else must be the uid's regular 0600),
  `Entries(full) ([]homefs.Entry{Name, Type})` sorted, `Exists`, `Unlink`, `Rename(from, to)`
  (RENAME_NOREPLACE only; EEXIST is `fs.ErrExist`), `RemoveTree(full)` (follows no link, refuses
  the root). `homefs.ErrConfig` marks a link (ELOOP), ENOTDIR, EXDEV, wrong owner/mode/type, cap;
  it is the 78 class. `.golangci.yml` gained one hunk: gosec G301/G302/G304/G306 off for
  `internal/teamchat/member/.*_test\.go`.
- `9b4dc433` step 03: `internal/teamchat/member/lock` (93%). `lock.Clock{Sleep}`,
  `lock.SystemClock()`; `TakeKeeper(ctx, clock, root, paths.Run)`, `TakeLink(...)`,
  `(*KeeperLock).TakeSeat(ctx, root, paths.Seat) (*SeatLock)` (refuses after the keeper's
  release, `ErrReleased`), `HoldSeat(ctx, clock, root, paths.Seat) (*SeatHold)` with only
  `Release()`, `AdoptSeat(*os.File, paths.Seat) (*SeatLock)` for the link (LOCK_EX|LOCK_NB on the
  passed description: succeeds only if it holds the seat or nobody does; else Busy, closed),
  `(*SeatLock).Seat()`, `Dup()` (F_DUPFD_CLOEXEC), `WriteRecord(data)` (pwrite at 0, ftruncate to
  length, fsync: never empty between), `Close()` (close only). Keeper and link `Release()` is
  LOCK_UN then close. `*BusyError{Path}` matches `ErrBusy`. Tries: 3, 50 ms apart. Tests: a reexec
  `flock` helper (`Args: path, sh|ex`), the fake clock's first Sleep releasing it (one sleep of
  50 ms), held past three tries (two sleeps, Busy, same inode after), a SIGKILLed holder, each for
  keeper, link, TakeSeat and HoldSeat.

## Left, in order (from the slice handoff; nothing of 04 is written yet)

4. `member/state` (MS1, MS2, gap 4). Facts already gathered so you need not re-read B:
   - Strict decode: copy the shape of `seat/records.go` `decodeStrict` (proto.Parse, every key
     lowercase `[a-z_]`, every wanted key present, then `json.Decoder.DisallowUnknownFields`,
     nothing after). `proto.Parse` accepts integers only (no floats), so B 2.3.2's `"credit": 9.5` cannot decode: `proto.Bucket` is integer credit already (P61: a token is
     `msPerMinute` credit), so the store's `gate.credit` is an integer; record this as a
     decision with a design-text fix for B 2.3.2's example.
   - Schemas: store.json B 2.3.2 (DESIGN-v2-B-member.md lines 591-620; caps at line 690:
     2048 pending of 320 B, 1024 ledger 320 B, 256 pings 3 KiB / 16 targets, 1024 receipts 80 B,
     32 awaiting 2.2 KiB, 16 missed 640 B, 64 sending 128 B, 64 gate keys 96 B, file 2 MiB;
     pending: agent items only up to cap-256, HUMAN and TIMEOUT may use the last 256; ledger and
     missed roll; every other full list refuses growth). session.json, keeper.json, report.json,
     conversation.json: B 2.3.3 (lines 710-776). status.json: B 2.9 (line ~1707). session.pid:
     B 2.4.1 step 4 (line ~800): `{"v":1,"run","keeper":{"pid","start_ticks"},"session":null, "kit":"<digest>","launch":"claude|plain","inbound":"accept|refuse","kind":"ccy|lxc|host|other"}`.
     desired.json: C 3.7.5 (DESIGN-v2-C-host.md line 1392) and B 2.10 (line 1724): exactly `v, session, generation, written_ms, seats[{team,seat}]` (one seat per team), `farewell[{team, seat,reason}]`, `inbound` accept|refuse. Lock record B 2.5: `{"v":1,"run","started_ms"}`,
     plus `launch_failed_ms` after a launch failure.
   - Gap 4: `seat.DecodeLockRecord`, `seat.LockRecord{Run, StartedMS, LaunchFailed bool}` and
     `seatLockRecord` are in `seat/release.go` (lines 364-391), used by `runOf` there and by
     `health/online.go` lines 164 and 183 (`lock.LaunchFailed`, `lock.Run`); the test is
     `seat/lockrecord_test.go`. Move to member/state as the handoff says (`LaunchFailedMS *int64`), keep a `LaunchFailed()`-style accessor or update the two callers.
     `seat/records.go`'s `decodeStrict` is unexported; member/state needs its own (or export
     one shared strict decoder from proto or state: decide, keep the import graph
     member/state -> member/lock -> member/homefs, and seat/health -> member/state; member/state
     must not import seat).
   - `.golangci.yml` tagliatelle `json: snake` override for `internal/teamchat/member/state`
     is needed for the struct tags (step 10 lists it; add it in its own hunk now).
5. seat gap 3 and `member/bundle` (MS3) with gap 2's deletion of `seat.PlaceGuest` and
   `removeGuestClaims` (in `seat/guest.go`), keeping `KeepGuestToken`.

Steps 06, 07, 09 and 10 are another builder's; 08 (guest, set-aside) follows this part.

## Gotchas met

- The lint hook runs on every Edit and reports half-made states; batch edits, then run
  `golangci-lint run` once. govet `shadow` is on: in a function that has `err`, never write
  `if err := ...`; tests here use a `check(t, err)` helper for setup to avoid it. `funcorder`
  wants unexported methods after every exported one of the type. `embeddedstructfieldcheck`
  wants a blank line after an embedded field. `thelper` wants `t.Helper()` first in every
  closure taking `*testing.T`.
- Tests run as root here: `os.Lchown(path, 4242, -1)` plants another uid's file.
- `go test -race` of the lock package takes about 1 s now; it took 9 s before the GORACE fix.

## Notes for step 08 (guest)

- homefs root of an opened home: `homefs.Open(home, homefs.Owned)`, then `Sub` or `MkdirAll`
  for `~/.config/teamchat/...` (each made 0700).
- The removals hold the seat with `lock.HoldSeat`, which reaches no store writer.
- `paths.Seat.StoreAside(ms)` names the aside file; `homefs.Rename` is RENAME_NOREPLACE.
