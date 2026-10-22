# n3a guest-place (Plan 00209 N3, step 08 part b)

Branch `feat/bus-v2-n3a-guest-place`, from 17bdc45f1. Commits: 7c67168e9 (ops and
`paths.StateSeat`), fa56f0213 (cli wiring).

## What was built

- `member/guest/places.go`:
  - `PlaceBundle(ctx, Home, Clock, stdin, changes)` (GU3);
  - `Membership(Home, stdin, changes)` (GU4);
  - `Remove(ctx, Home, Clock, team, changes)` (GU5);
  - `SetAside(ctx, Clock, stateDir, team, changes)` (SA1).
  - Each op's `what` values are constants: `WhatSeat`, `WhatToken`, `WhatBundle` (`seat.json`),
    `WhatStore` (`store.json`) and `WhatDesired` (`desired.json`).
  - The per-op sets are exported for TB2.4: `PlaceBundleChanges`, `MembershipChanges`,
    `RemoveChanges` and `SetAsideChanges`.
  - `Home{Root *homefs.Root, User paths.GuestUser}` is the opened home and the user. The layout
    is `paths.HomeLayout(home, paths.GuestRuntimeDir(user))`.
  - `Clock` is `Now` plus `Sleep`, so it satisfies `lock.Clock`.
  - `changes` is an unexported interface, `Changed(what string) error`.
- `paths.StateSeat(state, team)` is the seat under a given state root, for `--state DIR`. It
  is registered in `memberNames` and `memberZeroDerivations`, and paths stays at 100%.
- `member/cli/guest.go` holds the run functions for `guest place-bundle`, `guest membership`,
  `guest remove` and `seat set-aside`. They moved from `unbuilt` to `runs`, under new verb
  constants (goconst required them). `unbuilt`'s `"n3a guest"` row now lists only
  `guest check`, and two test lines changed with it.

Coverage: guest 91.2%, paths 100%, cli 99.1%. golangci-lint is clean with and without
`--build-tags tuwunel`, and qa-changed is green. Tests run as root, using `Lchown` to uid 4242.

## Behaviour and decisions

1. **GU3 identity:** the seat is the same when its seat.json decodes with
   `seat.DecodeMemberBundle` (pool or held form) to the same `team` and `user_id`. The seat is
   then the same too, because the handle is the localpart of the user_id. A missing seat.json
   in an existing directory counts as a different identity, as an undecodable one does.
2. **Absent seat:**
   - every `<team>.new-*` entry in the state root is removed (a directory with `RemoveTree`,
     anything else with `Unlink`);
   - then `SeatClaim(team, fresh claim)` is made, filled with token then seat.json (`O_EXCL`,
     0600), and renamed in `RENAME_NOREPLACE`;
   - this prints `seat` once.
   - The claim id generator is a local copy of `seat`'s unexported `newClaim`. **Proposed:**
     export one from `seat` or `paths` at the merge.
3. **Replace only where the bytes differ.** An existing file is read with its cap: token
   `MaxSecret`, seat.json `MaxBundle`, desired `MaxDesired`. A missing file is replaced. A file
   that is a link, another uid's, a FIFO or over its cap is **refused as 78**, never replaced
   (GU1's "refuses a path owned by another uid"). A file with the same bytes but a mode other
   than 0600 is rewritten.
4. **Error mapping:**
   - a `homefs.ErrConfig` anywhere under the seats is 78 `bundle` (template names the team, no
     path, so `exits.Name` cannot panic on an odd home);
   - under membership it is 78 `membership`;
   - Busy is 75 `seat-held`;
   - a stdin read failure is a plain error (exit 1).
   - A `~/.config` that is a link is refused. ~/.config is made 0700 if absent, then opened
     `Owned`; the teamchat directories are made 0700 (`MkdirAll` requires Private).
5. **GU5:**
   - all entries are listed first, and any directory refuses the whole removal before anything
     is unlinked;
   - symlinks are unlinked, never followed;
   - one `seat` line is printed if anything went.
6. **SA1:**
   - `--state DIR` is opened `Owned` and the seat directory `Private`. A missing state root or
     seat directory, or no store, is success with no line.
   - A non-absolute `--state` is 78 `bundle` (cli's `checkDir` refuses it first anyway).
   - The default is `memberLayout().State()`, the home layout of `$HOME` and
     `$XDG_RUNTIME_DIR`; an unset runtime dir is an error.
   - **Proposed design text (SA1):** "without `--state`, the state root of the caller's home
     layout".
7. **Tests:**
   - LK3 against a reexec `LOCK_EX` holder for place-bundle (identity change), remove and
     set-aside: 75, nothing changed, seat.lock the same inode, two waits between three tries;
     set-aside then succeeds after release with the same inode.
   - MS3's "link reads between the renames": `ReadPair` in the `between` seam after the token
     rename meets no error, for both the same and a new identity (bundle report observation 7
     holds: old seat.json plus new token passes).
   - Not added here: LK1's `LOCK_SH` "for 60 ms" case for `guest remove`. `lock`'s own tests
     cover the acquire, and remove calls `lock.HoldSeat` unchanged.

## Glue the merge needs (exactly)

1. In `member/cli/guest.go`, replace `var identify = identifyUnbuilt` with GU1's identity
   check, adapted to `func(Invocation) (guest.Home, error)`:
   - `Home.Root` is the home opened as a homefs root (my ops call `Sub(.config, Owned)` on it,
     so `Owned` or `Private` both work);
   - `Home.User` is `--user` parsed.
   - Delete `identifyUnbuilt` and its first assertion in `TestDefaultGuestSeams`. If Identity
     already carries a root and a user, a three-line adapter is enough.
2. In `cli/guest.go`, replace `changedLines` with the guest-check part's `Output` if it differs.
   My ops accept anything with `Changed(what string) error`; if Output's method has another
   name, rename one or wrap it.
3. If `Output`'s per-op sets also live in `output.go`, keep mine in places.go:
   `PlaceBundleChanges`, `MembershipChanges`, `RemoveChanges`, `SetAsideChanges`. Drop any
   duplicates.
4. Both parts may declare a package comment for `guest`. Mine is at the top of places.go; keep
   one.
5. Merge `unbuilt`'s `"n3a guest"` row: the guest-check part removes `guest check`, and then
   the row and its two test entries (`run_test.go` line for `guest check`, `verbs_test.go`'s
   `"guest check": "n3a guest"`) go.
6. Optionally, the merge can set umask 077 (GU1) in identify, before any op.

## Notes for the lead

- `paths.Layout.SeatClaim`'s comment still says "swapped in". GU3 now renames it
  `RENAME_NOREPLACE` into an absent seat only.
- The other builder was sent a message asking for Output's and Identity's exact API. No reply
  had arrived when this report was written.
