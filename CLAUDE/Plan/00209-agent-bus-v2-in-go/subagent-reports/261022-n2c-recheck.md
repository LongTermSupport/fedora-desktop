MERGE

# N2 slice n2c: narrow re-check of the seat revoke fix (`feat/bus-v2-n2c` at 950d7f95)

This re-check covers 950d7f95, the fix for the medium in `261022-n2c-check.md`. That medium
was a planted `seat.lock`, or a mode or owner on the seat directory, that stopped `Retire`.
The fix closes it. The new findings below are all low or informational, and none of them is
the same class at medium.

## What was run

- `go test -race -count=1 ./internal/teamchat/seat/...` at 950d7f95, as uid 0: ok. The mode
  and owner tests re-ran themselves under `setpriv` with an empty bounding set.
- A revert by `go test -overlay` (`untracked/scratch/n2c-recheck/overlay.json`; nothing
  tracked changed). It puts back the 97e3ffca text of `lockprobe.go`, `release.go`, `take.go`
  and `seat.go`. A shim supplies `ErrCopyKept` and `lockRefused` so the new tests compile. The
  one test that calls the new `probeOpen` signature is dropped.
  `TestAPlanted*`, `TestAFIFOIsNeverOpened`, `TestAModeTheMemberSetStopsNoRevocation` and
  `TestAnOwnerTheMemberSetStopsNoRevocation` then give 48 FAIL lines. They fail with the
  check's own errors: `too many levels of symbolic links`, `the seat lock is not a regular file`, `not a directory` and `permission denied`. The `revoked` assertions also fire: "the
  released token is still live" and "held/ was kept".

## Points 1-5

| Point                                      | Verdict | Evidence                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| ------------------------------------------ | ------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 1. Reproductions are tests, red without    | YES     | `planted_test.go` covers every case from the check. At `seat.lock`: a link to `/dev/null`, a link to an owner file, a FIFO and a directory. In place of the seat dir: a link, a FIFO, a file and a socket. Modes 0000, 0100 and 0500, and a chown to uid 1, on both the seat dir and `sessions/<s>/`. Each runs through remove, ended release and take. The revert above makes all of them fail, so the tests do pin the fix. The capability drop is real: the child run fails on its own `revoked` checks, not because setpriv is missing.                                                                                                |
| 2. "Held" is decided from host-owned state | YES     | `held/` is `<root>/pool/<checkout>/<team>/<seat>/held` (`paths.go:475,507`). That is outside `sessions/`, the only tree mounted into a container. Every access goes through `openat2` from the root fd with `RESOLVE_BENEATH\|NO_SYMLINKS` (`fs.go:23,89`), so no member can write it or swap in a link. The session record comes through `Sessions.Restorable`, which the store only asks (`seat.go:43-46`, 00203 H5, the host registry). Carry condition: when the adapter is wired, it must read the host registry and nothing under `sessions/`. The lock now only adds liveness, through `sessionHolds` (`take.go:203`).              |
| 3. Two live holders on one seat            | NO      | A live ccy session has a record, so `Restorable` makes it `ErrHeld` whatever its lock shows (`take.go:211`). A host `cc` session has no record, but its seat dir is mounted into no container, so no member can make its lock refused. A concurrent take holds the allocation lock (`Take` checks `holds(a)`), so it serialises with release steps 2 and 5. If a member makes its own lock refused after its record is gone, it only loses its own seat. ST6 then ends the old token anyway: `Mint` logs in, logs out and logs in again (`credential.go:43-64`). So a member gains nothing it did not already have from deleting the lock. |
| 4. Every revoke path ignores member files  | MOSTLY  | Steps 1 and 2 turn `ELOOP`, `ENOTDIR`, `ENXIO`, `EACCES` and `EPERM` into refused or kept (`lockprobe.go:26`, `release.go:139,211`). `moveDone`, `heldRecord`, `retire` and `finish` read only host paths. A remove (`Leaving`) skips the notice step. Two exceptions remain, and both are low: new findings 1 and 2. No other caller exists yet. Device delete (`matrix.DeleteDevice`) and placement touch no member path.                                                                                                                                                                                                                |
| 5. FIFO opened or symlink followed         | NO      | `probe` first opens with `O_PATH` (no read, cannot block) and `fstat`s the result. Only a regular file is opened again, `O_RDONLY\|O_NONBLOCK`, and that file is checked to be the same inode (`lockprobe.go:84-112`). Every path has `O_NOFOLLOW` and `RESOLVE_NO_SYMLINKS`. `exists` uses `AT_SYMLINK_NOFOLLOW`. `sub` uses `O_DIRECTORY`, so a FIFO gets `ENOTDIR` before it is opened. `rename` moves an entry without following it. The swap window between the two opens is the one residue: see info 3.                                                                                                                             |

## New findings

### 1. Low: a member that fills the store's filesystem still stops `Retire` on every path

`release.go:104-118`, `fs.go:216-260`.

Step 2 runs `mkdirAll(released/sessions/<s>)` and then the `renameat2`. Both must allocate on
the store's filesystem. Neither `ENOSPC` nor `EDQUOT` is in `memberRefused`, so the error
returns before step 4. `sessions/<s>/` lies under the store root, read-write in the container,
so it is the same filesystem.

Scenario: a live member exfiltrates `token`, then writes until the disk is full. The owner runs
`member remove`, which fails with `no space left on device`, and the token stays live. The
error is loud, and the owner's lever still works: stop the session, free the space, retry.
That is why this is low.

Fix (cheap): when step 2 fails with any error that is not a refusal, still run `retire` before
returning, and give back `errors.Join(moveErr, retireErr)`. Alternatively, record in n2c-D14
that disk exhaustion is accepted.

### 2. Low, carry condition: steps 3 and 4 of an ended release depend on the announcer

`release.go:113-118, 261-285`.

On the ended-release and take paths, the notice comes before `Retire`. The notice is posted
from a run id the member wrote. The id is grammar-checked and at most 4 KiB, but an announcer
error stops the release before `Retire`. Area B's `Disconnected` must therefore answer an
`Outcome{Posted:false}`, never an error, for anything the member's input can cause. If it
cannot, a member who left a crafted run id keeps an ended session's token live, and blocks
takes of the seat too. A remove is unaffected, because it skips the notice. Pin this in the
area B announcer spec, or move `Retire` ahead of the notice: the subject posts the notice, not
the seat.

### 3. Info: the reopen in `probeFound` goes by path, not by fd

`lockprobe.go:91`.

In the window between the `O_PATH` look and the reopen, a member can swap in a FIFO. The FIFO
then is opened `O_RDONLY|O_NONBLOCK`, which never blocks. The inode check then refuses it. The
only effect is that the member sees a reader appear for a moment. A rootless container cannot
make device nodes, so there is nothing worse to open. Reopening through `/proc/self/fd/<n>` of
the `O_PATH` fd would remove the window. Optional.

### 4. Info: in `clearHeld`, `ErrCopyKept` hides a close error

`take.go:191-196`.

`release` returns `errors.Join(releaseErr, h.close())`. `errors.Is(err, ErrCopyKept)` then
discards the whole join, a failed `close` of the `held/` fd included. Match `ErrCopyKept` only
on `releaseHeld`'s own result, or re-join the close error.
