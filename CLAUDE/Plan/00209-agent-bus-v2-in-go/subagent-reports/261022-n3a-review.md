# Plan 00209 N3 n3a: adversarial review, round 1

Scope: `git diff f6e4ad62..79d6d66e8` under `src-go/` (paths' member names, `reexec`,
`member/{homefs,lock,state,bundle,exits,records,cli,kit,guest}`, `cmd/teamchat`, seat's and
health's changes), against SPEC-n3 sections 1, 2, 9 (gaps 1-4, 8) and 10, and the part reports'
decisions.

Baseline at 79d6d66e8: `go test -race -cover` green on every slice package (bundle 93.1, cli
99.1, exits 98.9, guest 93.9, homefs 93.2, kit 93.3, lock 92.9, records 100, state 99.7, reexec
92.3, paths 100, seat 91.0, health 91.9; `cmd/teamchat` 50.0, its `main` only); `golangci-lint run` clean with and without `--build-tags tuwunel`.

## Verdict: FIX-FIRST

One major (the type order of LK2 can be walked around), four minors, four nits. The trust
boundary otherwise holds: every member file is reached by `openat2` from a held directory fd
with `RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS|RESOLVE_NO_MAGICLINKS` and `O_NOFOLLOW`, a read is
non-blocking with an `fstat` regular-file and owner check, a write is `O_EXCL` temp, `fsync`,
rename, directory `fsync`; no code unlinks, renames or truncates `seat.lock`, and a seat lock is
released by close only; the token is read from stdin or a 0600 file, held as `seat.Secret`
(formatted `[secret]`, never marshalled) and reaches no argv, message or diagnostic line; the
codecs are strict (proto's JSON, `v` 1, exact keys by a round trip, every list cap firing before
the file cap); the guest ops print only `CHANGED<TAB><what>` from a fixed per-op set and end in
0, 64, 75 (lock-taking ops only) or 78; the CLI grammar matches CL1-CL6.

## Findings

### 1. MAJOR: `lock.AdoptSeat` makes a `SeatLock` of any description, and the store writer never checks its lock is the seat's (LK2, MS2)

`member/lock/lock.go:170` takes any `*os.File`, `LOCK_EX|LOCK_NB`s it and returns a `SeatLock`
for whatever `seat` it is told. Any file the caller can lock (a fresh temp file, another seat's
lock, `keeper.lock`) becomes "the" seat lock, and `state.NewStoreWriter`
(`member/state/writer.go:180`) accepts it, so a store can be written with no hold of the seat's
`seat.lock`: the order LK2 says is enforced by type ("`TakeSeat` is the only way to a
`SeatLock`") is not, and MS2's "written only through a type that holds the seat's lock
description" rests on the caller's good faith. The same gap exists without `AdoptSeat`:
`NewStoreWriter` opens `seat.Dir()` below whatever root it is given, so a seat directory renamed
or replaced after `TakeSeat` (a placement, a hand edit) gets a store written beside a lock nobody
holds.

**Fix.** `AdoptSeat(root, f, seat)` checks that `f` is the regular file `seat.Lock()` is below
`root` (same device and inode, by an `fstatat(AT_SYMLINK_NOFOLLOW)` from the parent's fd), and
refuses anything else before it locks; `NewStoreWriter` checks the same of the lock it is given
against the seat directory it opened, so the store is written only into the directory whose
`seat.lock` is the held one. `homefs` gains the one by-name status read both need. Tests: a
description of another file, of another seat's lock, and of a replaced seat directory are each
refused.

### 2. MINOR: `seat set-aside --state DIR` accepts a state root that is not 0700 (TB4)

`member/guest/places.go:318` opens `--state` as `homefs.Owned` (no group or other write), but
TB4 says "the run and state directories must be the uid's, 0700, else 78", and the state roots
`seat set-aside` acts on (a member's, a ccy session's `sessions/<session>/`, which seat makes
0700\) are all private. **Fix:** open it `homefs.Private`; a test with a 0755 state root expects
78 `bundle` and the store untouched.

### 3. MINOR: an identity change leaves a window in which the pair reads as the old user with the new user's token, and the test that names it does not look (GU3, MS3)

`placement.into` (`member/guest/places.go:178`) for a different identity replaces `token` then
`seat.json`; between the two renames the pair on disk is the old user's `seat.json` with the new
user's token, and `bundle.ReadPair` accepts it (its seat.json reads agree). No link can hold the
seat then (the op holds the seat lock), but `ReadPair` takes no lock and any later reader that
does not (a `seat disconnected` from a held copy, a status probe) is given a mismatched pair
whole. `TestALinkReadingBetweenTheRenames` (`places_test.go:423`) only asserts "no error", so it
passes on the mismatched pair; the part report's observation 7 noticed the mix but kept it.

**Fix.** On an identity change, under the hold, unlink the old `seat.json` first (after the store
is set aside), then place `token`, then `seat.json`: between the renames the seat reads as no
bundle (`fs.ErrNotExist`, which MS3 leaves for the next event), never as a mismatched pair. The
same identity keeps today's order (old metadata with the user's own reissued token is the same
account). The test asserts what the read between returns: the same user's pair with the new
token for the same identity, `fs.ErrNotExist` for another.

### 4. MINOR: `TestStoreListCapsFireFirst` does not check that the refusal is the list's own (MS1)

`member/state/store_test.go:254` asserts only that each grown store is refused by something
other than the file cap, so a grow that trips an unrelated rule (a repeated id, an index, a
malformed entry) passes for any list, and a cap that stopped firing would go unseen as long as
something else refuses. **Fix:** each row names the rule it must trip (the list's count, entry
size, ping targets, the agent share of pending, the transcript path, the repeated id) and the
test matches it.

### 5. MINOR: LK1's momentary `LOCK_SH` case is not run through `guest remove` (LK1)

LK1: "Tests with a second process holding `LOCK_SH`: for 60 ms, the acquire succeeds; past all
three tries, it is `Busy`; each for `TakeSeat` and for `guest remove`." The lock package runs
both against `TakeSeat` and `HoldSeat`, and `places_test.go` runs the busy case through
`guest remove`, but the waited-out shared hold never goes through `guest remove` (the
guest-place report says so). **Fix:** a test in `member/guest` with a second process holding
`LOCK_SH` until the first wait: `Remove` succeeds after one 50 ms wait, the files go,
`seat.lock` keeps its inode.

### 6. NIT: `homefs.Create` leaves a part-written file at its final name when a write fails

`member/homefs/homefs.go:171`: a failed `fchmod`, write or `fsync` after the exclusive create
leaves the file in place under the name being created (`Replace` uses it for a temp name and
cleans that, but `Create` itself does not). Today only the fresh placement's claim directory
uses it, which the next placement removes, so nothing is lost; the next caller would not be so
lucky. **Fix:** unlink the name on a failure after the create.

### 7. NIT: a torn seat lock record can be read by the host while it is rewritten (LK4, TB2.1)

`SeatLock.WriteRecord` writes at offset 0 then truncates: a reader between the two (the health
tick reads the record of a held seat) can see the new bytes followed by the old tail, which the
strict decoder refuses. The effect is bounded by TB2.1 (one tick's entry reset to no run) and
needs a fixed-size record to remove; no change, noted for n3b's LP1, whose probe reads it.

### 8. NIT: a guest op's I/O failure exits 1 `internal` (GU1)

GU1 lists the exits 0, 64, 75, 78; `guest.Finish` sends any error that is not an
`*exits.Error` (a broken stdin, `ENOSPC`, a rename that lost a race) to 1 `internal`, which B
2.2.4 calls "a bug". The guest-place report decided this (decision 4); the host treats 1 as a
failure of the op, so nothing is hidden. Proposed design text (B 2.13): "an I/O failure inside
an op is exit 1 `internal`, with its diagnostic line before the error object."

### 9. NIT: `bundle.ReadPair` reads the seat's whole store on every use (MS3)

`checkStore` (`member/bundle/bundle.go:157`) reads and decodes up to 2 MiB of `store.json` for
each pair, and the link reads the pair on each use. Correct, and n3a has no caller; n3c, which
holds the store in memory, should pass its user id in rather than re-read it.

## What was checked and holds

- **TB4 / GU1 opens.** `homefs.open` is `openat2(root fd, rel, O_NOFOLLOW|O_CLOEXEC, RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS|RESOLVE_NO_MAGICLINKS)` for every path below a root; a root
  is opened `O_DIRECTORY|O_NOFOLLOW` and checked owner and mode; `MkdirAll` checks every existing
  level 0700; leaves are made `O_EXCL` 0600 then `fchmod`ed to the named mode; a FIFO is opened
  non-blocking and refused by `fstat` (tests with a FIFO at a read and a lock path).
- **Locks.** LK1's three tries 50 ms apart on a clock seam, `Busy` after the third; LK2's
  `TakeSeat` from a held keeper; LK3: no unlink, rename or `O_TRUNC` of a lock file anywhere,
  the inode pinned across busy, `SIGKILL` and every removal test; a seat lock and a removal hold
  are released by close alone, keeper and link by `LOCK_UN` then close; `Dup` is
  `F_DUPFD_CLOEXEC` and never unlocked.
- **Codecs.** decodeStrict refuses repeated keys, floats, booleans for integers, other-case and
  missing keys and nulls (the round trip), and quotes nothing; each file's cap and closed sets;
  the store's eight lists' counts and entry sizes, their sum under 2 MiB; the lock record's
  codec moved to `member/state` with an encoder, seat and health reading it there (gap 4).
- **Bundle.** seat.json, token, seat.json; torn twice is `ErrTorn`, never 78; token 0600, the
  caller's, one credential; a `password` entry, a third shape, another team, another user than
  the store's are 78 `bundle`; `Distinct` on user id and token (constant time).
- **Guest ops.** GU1's identity (uid, euid, egid, passwd name both ways, groups, home owned and
  not shared-writable), umask first; GU2 on the unit's PATH only, gh at its found path with its
  output captured, 3 s reach; GU3 fresh by claim and `RENAME_NOREPLACE`, same identity in place,
  another under the hold with the store set aside; GU4 canonical bytes, same bytes print
  nothing; GU5 everything but `seat.lock`, a directory refused first; SA1.
- **Output contract.** `Op.Changes` per op is the set n3b reads by `guest.ChangesOf`; a `what`
  outside it panics; an exit outside the op's contract is 1.
- **CLI.** CL1 (thin main, the YAML keepout row), CL2 (the unbuilt list by slice and its
  `teamchatrelease` test), CL3 (kind and v first, text only in strings), CL4 (the exit table as
  data, `help exit-codes` from it, templates filled with grammar-checked names, `WriteTeams`),
  CL5 (every design launch line, the refusals), CL6 (`ResolveTeam`, every single-team verb with
  two teams).
- **Kit.** KT1's files, modes, the four hooks by absolute path, `refreshInterval` 25.
