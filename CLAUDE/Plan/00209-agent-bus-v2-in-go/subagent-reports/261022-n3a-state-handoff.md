# n3a state handoff 1 (Plan 00209 N3, slice n3a, part state: steps 04 and 05)

Worktree `/workspace/untracked/worktrees/bus-v2-n3a`, branch `feat/bus-v2-n3a`. Read
`/workspace/untracked/scratch/n3a-part-common.txt` (the part rules; there is no parallel builder
now), `261022-n3a-handoff.md` steps 04-05 and `261022-n3a-core-handoff.md` (step 04's gathered
facts) first. The builder stopped at the context budget. The WIP commit is green: tests pass with
-race, golangci-lint reports 0 issues on the changed packages, and qa-changed was not run on it.

## Done (in the WIP commit)

- `proto.ValidSeat` (a handle's `<seat>`), with a row in `TestSmallGrammars`.
- `lock.(*SeatLock).Held()`: nil while open, `ErrReleased` after Close (the StoreWriter's check).
- `.golangci.yml`: a tagliatelle `json: snake` override for `internal/teamchat/member/state`, in its own hunk.
- `internal/teamchat/member/state` (94.4%):
  - `strict.go`: the package doc (read it), `ErrInvalid`, `*InvalidError{File, Code, Rule}`
    (codes `json`, `schema`, `version`, `seat`), `rules` (keeps the first broken rule, so checks are
    straight lists of `c.need`, which stays under cyclop), `decodeStrict` (cap, `proto.Parse`, an
    object, `v` 1, `DisallowUnknownFields`, then a **round trip**: the struct must marshal back
    to the same JSON value, which refuses null where a value is needed, keys in another case, and
    missing keys; nil slices and maps still pass the round trip, so each check refuses them by
    hand), `encode` (marshal, then run the decoder over the output), the generic `Read(root, full, limit, decode)` and `Write(root, full, Encoder)` (0600 through `homefs.Replace`).
  - `lockrecord.go`: `LockRecord{V, Run, StartedMS, LaunchFailedMS *int64}` with `LaunchFailed()`,
    `Encode`, `DecodeLockRecord`, `WriteLockRecord(*lock.SeatLock, LockRecord)`, `MaxLockRecord`
    4 KiB, `validMS` (0 to `proto.MaxSentMs`).
  - `store.go`: `Store` and its entry types for B 2.3.2's whole schema; `NewStore`; `DecodeStore`
    with every count cap and every entry size cap (the size is measured on the parsed input,
    `proto.Value.Size`, as A measures); the unexported `(*Store).encode` (no exported encoder, so
    only a StoreWriter can write a store); `Add*` growth (`ErrFull`/`*FullError{List}`; pending
    agent items stop at 2048-256, HUMAN and TIMEOUT use the rest; ledger and missed roll). The
    tests prove every list cap fires before the 2 MiB file cap, by a real full store and by the
    caps' arithmetic.

## Left, in order

1. **`writer.go` + `writer_test.go`** (MS2): `NewStoreWriter(root *homefs.Root, l *lock.SeatLock) (*StoreWriter, error)` opening `l.Seat().Dir()` with `root.Sub(.., homefs.Private)`;
   `Load() (Store, error)` (missing is `fs.ErrNotExist`, through `Read(.., MaxStore, DecodeStore)`);
   `Write(Store) error`: `l.Held()` first, `s.Team` must equal the seat directory's base name, then
   `s.encode()` and `dir.Replace(seat.Store(), data, 0o600)`, one replace per change; `Close()`
   closes the directory root only. Tests: written then loaded; refused after the lock is closed;
   a team that is not the directory's; a store owned by another uid (`os.Lchown(p, 4242, -1)`;
   tests run as root) refused on Load.
2. **The other member files**, each its own file + test, each `Encode`/`DecodeX`/`MaxX` and the
   `rules` style: `session.json` (B 2.3.3; session_id, source, transcript, socket and claude all
   null until the first SessionStart, else all set: a decision), `keeper.json` (why iff failed,
   in crash-loop|config; next_ms iff restarting; cap 4 KiB), `report.json` (desired.code null iff
   applied, else json|schema|version|seat|missing), `status.json` (B 2.9; cap 64 KiB; role null or
   coordinator|worker|probe; teams at most 64), `conversation.json`, `session.pid` (B 2.4.1 step 4),
   `desired.json` (C 3.7.5 / B 2.10: exactly v, session, generation, written_ms, seats, farewell,
   inbound; one seat per team; farewell reasons released|removed|parked; `CodeSeat` for a seat
   rule; cap 64 KiB, 64 seats, 64 farewells). Full design notes for these are in the step 04
   bullet of `261022-n3a-handoff.md` and the core handoff.
3. **Gap 4**: switch `seat/release.go` `runOf` and `health/online.go` (lines 164, 183, 188) to
   `state.DecodeLockRecord` / `LaunchFailed()`; delete `seat.LockRecord`, `seatLockRecord`,
   `DecodeLockRecord` from `seat/release.go` and `seat/lockrecord_test.go` (its cases are already in
   `member/state/lockrecord_test.go`). Check health's tests build the record bytes the new decoder
   accepts (it refuses `"launch_failed_ms": null`).
4. Commit step 04 (qa-changed first: red only on GO-2026-6809).
5. **Step 05**: `seat.DecodeMemberBundle` (exactly a `Bundle` or a `HeldBundle`, chosen by the
   `claim` key), delete `seat.PlaceGuest`, `placeGuest`, `removeGuestClaims` and their tests
   (keep `KeepGuestToken`), and `member/bundle.ReadPair` (MS3; see the slice handoff's step 05).
6. The report `subagent-reports/261022-n3a-state.md`, documenting for step 08: StoreWriter,
   `paths.Seat.StoreAside` with `lock.HoldSeat` + `homefs.Rename` for set-aside, SeatHold use, and
   ReadPair.

## Decisions taken so far (for the report, with proposed design text)

- `gate.credit` is an integer, `proto.Bucket`'s credit (P61): B 2.3.2's example `"credit": 9.5`
  cannot decode (strict JSON is integers only; a test pins it as code `json`). Proposed text: the
  example reads `"credit": 540000` and a line says "`credit` is integer credit, one token being
  60 000 (proto.Bucket, P61)".
- The member files are read and written through `homefs` (owner, mode, no link, fd-rooted), not
  `teamchat/state.ReadFile`/`WriteFile`, which check no owner (TB4 needs "the uid's").
- B 2.3.2's entry sizes hold only for 13-digit millisecond times (until the year 2286) and user
  ids of at most 128 bytes (A4): a gate key with a 15-digit time is 98 bytes, over its 96. The
  store enforces A4's 128-byte user id; the full-store test uses 13-digit times. Proposed text:
  "The sizes assume ... user ids of at most 128 bytes (A4) and 13-digit times."
- The store's rooms are main, reports, questions (n3f's side rooms must extend `storeRooms`);
  items human, peer, ping, notice, timeout (`records` kinds); a TIMEOUT item's id is
  `proto.TimeoutKey` (`$<43>~<localpart>`), every other item's an event id; send kinds text, ping,
  notice; `fresh` is true exactly when `next_batch` is empty.

## Gotchas

- The PostToolUse lint hook fires on every edit; `golangci-lint fmt <pkg>` fixes gofumpt (no
  `gofumpt` binary). Go 1.26: use `new(expr)` not a `ptr` helper, `errors.AsType`.
- `nonamedreturns` is on (the hook's "named results" advice is for `x, _ :=`): grow lists through
  a pointer instead. `funcorder`: a type's unexported methods after all its exported ones.
- A test calling a pointer method needs an addressable value: `s := sampleStore(t); s.encode()`.
