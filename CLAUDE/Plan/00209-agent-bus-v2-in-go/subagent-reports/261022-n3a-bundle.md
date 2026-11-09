# n3a part bundle (Plan 00209 N3, slice n3a, step 05: MS3, gaps 2 and 3)

Worktree `/workspace/untracked/worktrees/bus-v2-n3a-bundle`, branch `feat/bus-v2-n3a-bundle`,
from `d27d36a0`. Commits: `93cf2beb` (seat), `988d93f9c` (member/bundle).

## What was built

- **`seat.DecodeMemberBundle(data) (MemberBundle, error)`** (gap 3): a `MemberBundle{Bundle, Held *HeldBundle}`. The `claim` key decides the shape: with one it is `DecodeHeldBundle`,
  without one `DecodeBundle`, each strict. A mixed shape (a `released_ms` and no claim, or a
  claim and no `released_ms`) decodes as neither.
- **Gap 2**: `seat.PlaceGuest`, `placeGuest`, `removeGuestClaims` and their three tests are
  gone. `KeepGuestToken` and its tests stay.
- **`seat.MaxBundle`, `seat.MaxSecret`**: exported caps (the record cap and the credential
  cap), so the reader reads no more than seat accepts.
- **`paths.Seat.Password()`**: `<seat dir>/password`, registered in `member_test.go`'s
  `memberNames` and `TestMemberNames`. `PoolSeat.Password` now uses the same `passwordFile`
  constant.
- **`member/bundle`** (imports seat, member/state, member/homefs, member/exits; member/state is
  unchanged and still imports no seat):
  - `ReadPair(root *homefs.Root, s paths.Seat) (Pair, error)`, with `Pair{Team, Bundle seat.MemberBundle, Token seat.Secret}`. It opens the seat directory as a `homefs` Sub
    (Private, 0700). It reads seat.json, then token, then seat.json again, and the pair counts
    only when both seat.json reads have the same dev, inode and bytes. A torn pair is read again
    once, at once, and torn twice it is `ErrTorn` (a sentinel, not an exit). A seat.json that
    disappears between the two reads also counts as torn.
  - It returns `fs.ErrNotExist` as it is when there is no seat directory or no seat.json (no
    bundle placed yet).
  - 78 `bundle` (`*exits.Error`, team set) for each of these: a `password` entry in the seat
    directory; a token that is not mode 0600 exactly; a token that fails `seat.ParseSecret`; a
    seat.json in neither shape; a seat.json whose `team` is not the directory's; a `user_id`
    that differs from `store.json`'s, read with `state.Read(..., state.MaxStore, state.DecodeStore)`; any file that is not as the member must have it (homefs
    `ErrConfig`: a link, another uid's, over its cap, not regular), or a token that is missing;
    and a seat directory whose base name is not a team id (for example a placement's
    `<team>.new-<claim>`).
  - 78 `store` for a store that does not decode, or a store file that is not as it must be.
    Other I/O errors pass through unchanged (homefs names only the path).
  - `Distinct(pairs []Pair) error`: 78 `bundle` when two pairs share a user id or a token. The
    tokens are compared with `subtle.ConstantTimeCompare`. The error names only the two teams.
  - No template, error or log line holds the token. The tests check every 78 message for it.

## Coverage

- `member/bundle`: 93.1%
- `seat`: 91.0%
- `paths`: 100%

All three run with `-race`. `golangci-lint` reports 0 issues with and without
`--build-tags tuwunel`.

`qa-changed` is red on one thing only: deadcode for `member/state.Write` (`strict.go:150`), which
the WIP state commit left unreached. The StoreWriter part will reach it. `state.Read` was also
red on the base commit, and `member/bundle` now reaches it. govulncheck GO-2026-6809 did not
show in the diff-mode run.

## Decisions, with proposed design text

1. **What a password bundle is in a member seat directory.** A member's seat directory only
   ever has a `token`, so "a password bundle" is taken to mean a `password` entry beside it.
   This is the file name a pool seat uses (`PoolSeat.Password`).
   Proposed MS3 text: "a password bundle (a `password` entry in the seat directory)".
2. **The token mode is exactly 0600.** MS3 says 0600; B11 says "0600 or tighter". The reader
   follows MS3, so a 0400 token is refused.
   Proposed B11 text: "mode 0600".
3. **The "seat" half of the mismatch rule.** A member seat directory is named for its team only
   (`<state>/<team>`), so no seat name can be compared with the directory. The seat is covered
   by the store's `user_id`: the store codec already requires the store's `seat` to match the
   handle of its `user_id`, so when the user ids are equal the seats are too.
   Proposed MS3 text: "whose `team` is not the directory it sits in, or whose `user_id` (and so
   seat) differs from the store's".
4. **Missing files.** No seat directory or no seat.json means no bundle yet, and returns
   `fs.ErrNotExist`. A seat.json present with no token is 78 `bundle`. GU3 places a fresh seat
   by renaming a whole directory and replaces the token by rename, so a stable seat.json with
   no token is a broken bundle, never a placement in progress.
5. **A store that does not decode is 78 `store`, not `bundle`.**
6. **`ReadPair` takes a root and a `paths.Seat`**, not a bare path string, so the read stays fd-
   rooted (TB4). The root is the state root or any directory above the seat directory.
7. **Observation for GU3 (no change made):** with GU3's order, token first and then seat.json,
   a reader can see seat.json's first read and second read both before the seat.json rename,
   with the token read after the token rename. That pair passes the check, as old seat.json plus
   new token. This only happens for the same identity, because an identity change goes through
   the seat hold and set-aside while no link holds the seat. The cost is then a stale token
   that the server refuses once (exit 21 `token`) until the next read. If that is not
   acceptable, the design should say that the link treats a refused token as a reason to read
   the pair again before reporting `token`.

## Notes for the next part

- GU3 (step 08) should decode an existing seat.json with `seat.DecodeMemberBundle`. An
  undecodable one is "a different identity". `paths.Layout.SeatClaim` is still in use: its
  comment says "swapped in", and GU3 renames it `RENAME_NOREPLACE`.
- The link calls `ReadPair` on each use and `Distinct` over all held seats' pairs. `ErrTorn`
  must be left for the next event (LN9), never reported as 78.
- Tests: `readPair(root, seat, between)` is the seam that changes seat.json between the two
  reads. The tests build a store by marshalling `state.NewStore` with `encoding/json`, because
  the store's encoder is unexported. They run as root (`os.Lchown` to 4242).
