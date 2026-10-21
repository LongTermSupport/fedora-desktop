# Plan 00209 M1, `bus/state`: the locks slice (L1-L12)

Branch `feat/bus-v2-m1-state-locks`. Files: `src-go/internal/bus/state/locks.go`,
`locks_test.go` (in-process), `locks_peer_test.go` (the L12 proofs against a second process),
`testdata/v1locks.py` (a v1 peer that runs `helpers/pingbus`'s own lock code from this
checkout). No other file is touched, so the merge with `feat/bus-v2-m1-state` is clean.

## The API

- `Locks{Sleep func(ctx, d) error}`: the wait between tries is injected (nil: a real timer
  that the context ends). Tests never read the clock.
- Path locks (L1, L2, L4, L6, L11): `Sync(ctx, stateDir, kind)` (watch, wait, recv only),
  `Seat(ctx, seatDir, claimMS)`, `Keeper(ctx, home)`, `Watch(ctx, home, key)`. Each opens its
  own description (`O_RDWR|O_CREAT|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC` 0600, regular files only),
  tries `LOCK_EX|LOCK_NB` three times 50 ms apart, then returns `*BusyError{Path, Holder}`
  (`errors.Is(err, ErrBusy)`; the holder's first word, `unknown` for any word v1 does not
  write). A lock it takes keeps the old text as `Previous()`, then writes its kind (and
  ` <claim>`). `Release()` truncates, `LOCK_UN`s and closes; a second release does nothing.
  `File()` is the close-on-exec descriptor to hand a child in `ExtraFiles` (L5).
- Probes (L3): `ProbeSync`, `ProbeSeat`, `ProbeKeeper`, `ProbeWatch` give `(kind, held, err)` and
  create nothing. `ReadClaim(seatDir)` and `ParseClaim(text)` (L6).
- Blocking locks (L7, L8): `WithOutbox(ctx, stateDir, fn)` (LOCK_EX on the state directory's
  own descriptor), `WithDelivery(ctx, home, fn)` and `TryDelivery` (relay/lock, relay/ made
  0700), `WithDeliveryAndOutbox(ctx, home, stateDir, fn)` (the pair as one call, L10).
- Told lock (L9): `WithTold(home, fn) (ran bool, err error)`: one try; another teller holding it
  is `false, nil` and fn does not run.
- L10 is enforced, not a convention: fn gets a context recording what it holds. Asking for the
  delivery lock under the outbox lock, the delivery lock twice, or an outbox lock under another
  returns `ErrLockOrder` before anything is opened.

## Paths defined here that the layout will own (S7-S10)

`syncLockName` `<team>/state/lock`, `seatLockName` `<seat dir>/seat.lock`, `keeperLockName`
`<session home>/keeper.lock`, `injectDirName` + `watchLockSuffix` `inject/<key>.watch`,
`relayDirName` + `deliveryLockName` `relay/lock`, `toldLockName` `told.lock`. Each is a constant
at the head of `locks.go` marked `TODO(layout)`. The functions take the directory (state dir,
seat dir, home), not a file path, so no caller joins a lock name.

## Departures (each improves on v1; none changes what the other side sees)

- LD1. The waits in L7 and L8 poll `LOCK_NB` every 10 ms through the injected sleep, so a
  context ends a wait; v1 blocks in the kernel and cannot be cancelled. flock has no FIFO order
  either way. A v1 waiter blocked in the kernel is woken at once on release while a Go poller
  wakes at its next tick, so under steady contention Go can lose the race repeatedly. That is
  acceptable for locks held only around local work (L8).
- LD2. The delivery and told locks are opened `O_NONBLOCK` and only as a regular file. v1 opens
  them without `O_NONBLOCK` and does no type check, so a FIFO at `relay/lock` or `told.lock`
  hangs v1's open.
- LD3. `relay/` and `inject/` are made 0700 with `mkdir` and refused (class `file`) when they are
  not a directory, a symlink included. v1's `makedirs(exist_ok=True)` follows a symlink.
- LD4. The state directory for the outbox lock is opened `O_NOFOLLOW`. v1 relies on its
  `ensure_dirs` having refused a symlink.
- LD5. A seat claim over 16 digits is refused when the lock is taken. v1 writes it and its own
  `parse_claim` cannot read it back.
- LD6. A watch key must be 16 lowercase hex digits (S13), so a key cannot name a path outside
  `inject/`. v1 derives it itself, so this only holds callers.
- LD7. L10 is held by the context (above). Limit: a caller that drops the context it is given
  inside fn (`context.Background()`) escapes the check. Go has no goroutine-local state, and a
  process-wide count would wrongly refuse an unrelated goroutine's legitimate send.
- LD8. `WithTold` does not run fn when another teller holds the lock. v1 yields `False` into the
  block, and both of its callers (`hooks.py:426`, `:481`) return at once on it.

## Proofs (L12)

Each peer is the test binary re-run as a child (`TestLockChild`, which skips in a normal run)
or `python3 -I testdata/v1locks.py <checkout> ...`. The v1 peer calls `inbox.acquire_lock`,
`acquire_lock_at`, `probe_lock_at`, `read_claim_at`, `TeamState.outbox`, `relay.Spool.exclusive`
and `membership.telling`, with v1's own path constants and `inject.Marks` (so S13's key is
checked too: v1 is given the socket and Go the key the test derives).

- L1, L2, L3, L11 (`TestEveryPathLockExcludesAndIsExcludedByAnotherProcess`, 6 locks x 2
  peers): the peer holds and v2 gets busy naming the peer's kind; v2's probe names it; after
  the peer releases, v2 takes the lock and finds it empty. Then v2 holds, and the peer is
  refused naming v2's kind and probes it held, then free after v2 releases.
- L2, L6 (`TestADeadV1SeatHolderLeavesItsClaimForV2`): a v1 seat holder that dies leaves
  `seat <ms>`, which v2 reads as the claim and as `Previous`. v1 reads v2's claim.
- L7, L8, L10 (`TestTheOutboxAndDeliveryLocksBlockAndAreBlockedByAnotherProcess`, outbox,
  delivery and both halves of the pair x 2 peers): the peer holds and v2 waits (the injected
  sleep is the witness), taking the lock once the peer lets go. v2 holds and the peer blocks:
  the v1 peer is seen blocked in flock in `/proc/locks`, the Go peer says `blocked` from its
  wait. The peer takes the lock only after v2 lets go.
- L8 try, L9 (`TestTheOneTryLocksRefuseAndAreRefusedByAnotherProcess`): each direction, each
  peer.
- L5 (`TestALockReachesAChildOnlyWhenHandedOver`): a v1-started child has no descriptor on the
  held lock, unless it was handed over through `ExtraFiles`. Also `FD_CLOEXEC` in-process.
- In-process: three tries 50 ms apart; a holder freed between tries; `unknown` holders;
  symlink and FIFO refusals; a missing directory; cancellation mid-retry and mid-wait; the order
  errors; the pair holding both; the told lock's one try; Previous with non-ASCII bytes as
  U+FFFD, as v1 decodes them.

Red: the tests were written first. The first build failed `undefined: Locks` / `undefined: Kind`
(vet, through the lint hook). The first run after `locks.go` had two failures, both in the
tests: the FIFO test named the wrong file, and the try-while-held test passed fn's own
context, so it got (correctly) `ErrLockOrder` instead of busy. Before that fix, the first peer
run hung: the Go child was given `-args` (a `go test` flag, not a test-binary flag), and
`io.Pipe` stdin kept `execx.Run` waiting on its copy goroutine. Fixed with `--` and OS pipes.

Mutation checks (made, then reverted): `syncLockName = "lock.mutant"` fails the v1 sync proofs
(`want busy by watch`, all three kinds). LOCK_SH in place of LOCK_EX in `holding` fails 5
proofs: the Go peer's outbox, delivery and both pair halves, plus try-delivery (`never waited`, `ran while the peer holds`). That second mutant first hung the suite, which led to a
`t.Fatal` so that a lock which never waits fails fast.

Green: `go test -race -count=3 -shuffle=on` passes; package coverage 93.7% (every `locks.go`
function at 83% or more; what is left is I/O failures such as pread or ftruncate errors);
`golangci-lint` 0 issues; `ruff check`/`format` clean on the fixture; `qa-changed` passed.

## Open questions

1. `seat exec` replaces the process (L5's `set_inheritable`). `File()` covers a child started
   with `ExtraFiles`. A handover through `syscall.Exec` needs `FD_CLOEXEC` cleared and execx to
   carry the descriptor. Left out until `seat exec` is built (YAGNI).
2. For the sibling: `Team.WithOutbox` and a `Team` sync-lock wrapper should run `EnsureDirs`
   and then call `Locks.WithOutbox` / `Locks.Sync`. These lock functions do not create the
   state directory, and refuse a missing one.
3. The L7/L8 proofs need `/proc/locks` to see the v1 waiter, and every peer proof needs
   `python3` (as `playlock`'s parity test already does).
