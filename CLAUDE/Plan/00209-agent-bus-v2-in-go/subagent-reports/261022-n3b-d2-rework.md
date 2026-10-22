# n3b-D2 rework scope: LXC members keyed per seat

Plan 00209, slice n3b. The decision is SPEC-n3 section 10 row n3b-D2; the design is C 3.9.1's
"What an LXC member is" and its names table. This file names each built thing that changes, and
how, so builders can be briefed. Paths are under `src-go/internal/teamchat/`.

## The new facts every item below uses

- An entry is (team, container, checkout, seat); `seat` is required; `user` is a field.
- Seat key `<key>`: first 16 lowercase hex of SHA-256 of `<team>\n<checkout>\n<seat>`.
- Repo label `<repo>`: `filepath.Base(checkout)` through `proto.NormaliseRepo`, cut to 24,
  trailing `-`/`_` trimmed, `checkout` if empty.
- Unit `teamchat-session-<team>-<repo>-<seat>-<key>.service` (at most 94 bytes); tmux session
  the unit name less `.service`; tmux server `-S /run/teamchat-session-<key>/tmux.sock`;
  `RuntimeDirectory=teamchat-session-<key>`; session name `lxc-<key>`.
- Guest state root `~/.config/teamchat/lxc/<key>/seats/`, desired dir
  `~/.config/teamchat/lxc/<key>/membership/`.
- Host copy `/var/lib/teamchat-admin/_lxc/<container>/<key>.json`, carrying `team`, `checkout`,
  `seat` checked against the key on read.
- Record entry keyed (container, checkout, seat), with `user`, unique by key and by handle.
- Targets `--member <container>/<key>` (apply and restart); output field
  `<user>@<container>/<repo>-<seat>-<key>`.
- Guest ops take `--key K`: `place-bundle --user U --key K`, `membership --user U --key K`,
  `remove --user U --key K --team T`; `check --user U --team T` is unchanged.

## Built on feat/bus-v2-n3b (this branch's base)

### `declare` (declare.go, gate.go, input.go)

- `LXCMember`: `Seat` becomes required (`readLXCMember`'s required keys gain `keySeat`, and
  `lxcOptional` loses it). `Seat` stays an int 1-9999.
- `readLXC`'s duplicate rule: the key `team\x00container\x00user` becomes
  `team\x00container\x00checkout\x00seat`, message "repeats another entry's team, container,
  checkout and seat". The per-container seat-key collision check is not here (it needs the
  records), it is `host/lxc`'s step 01.
- `gate.go`'s `LXCEntry` (AG1): `who()` is `container/user` today, so two seats of one user in
  one container collide in the gate and in `approved.json`. It gains `Checkout` and `Seat`;
  `who()` becomes the (container, checkout, seat) form, `String`/`ParseLXCEntry` and
  `readLXCEntries` (its `keyCheckout`, `keySeat`, its duplicate check) follow; `User` stays a
  gated field of the entry. The approved-file encoding at gate.go:422 gains the two keys. C 3.5.1's
  security-field list now names checkout and seat (C line "each `teamchat_lxc_members` entry's
  team, container, checkout, seat, user and `human_text`").
- `input.go`'s `encode` writes `seat` always.
- Tests: the dedupe tests change; new ones for two repos of one user, coordinator and worker on
  one checkout, missing `seat`, and the gate seeing two seats of one user as two entries.

### `paths` (lxc.go, paths.go, member.go, ids.go)

- New `SeatKey` type (16 hex grammar) and `LXCSeatKey(team TeamID, checkout string, seat int) SeatKey`; `LXCRepoLabel(checkout string) string`.
- `LXCDesiredCopy(c ContainerName, u GuestUser)` becomes `LXCDesiredCopy(c ContainerName, k SeatKey)` (`<key>.json`).
- `LXCSession(u GuestUser)` becomes `LXCSession(k SeatKey)` (`lxc-<key>`).
- `SessionUnit(u)` becomes a function of (team, repo label, seat, key);
  `GuestRuntimeDir(u)` becomes `GuestRuntimeDir(k)` (`/run/teamchat-session-<key>`);
  `GuestUnitFile`, `GuestUnitWantsLink` follow `SessionUnit`; a new `GuestTmuxSocket(k)`.
- `GuestRuntimeParents(u, uid)` returns the login parent only; the unit parents are listed by
  the caller from the guest's `/run` (a new `IsGuestRuntimeDirName(name)` grammar check).
- New `LXCLayout(home string, k SeatKey) (Layout, error)`: state `~/.config/teamchat/lxc/<key>/seats`,
  desired `.../membership`, run root under `GuestRuntimeDir(k)`. `HomeLayout` stays for host `cc`.
- Tests in lxc_test.go, member_test.go, paths_test.go, separation_test.go, zero_test.go change;
  new: the unit name at its longest (94 bytes), the runtime directory's B socket paths within 107
  bytes, the repo label's normalisation cases.

### `host/lxc/lxcstate`

- `Entry` gains `Checkout string` and `Seat int`; `key()` becomes (container, checkout, seat);
  `User` stays a field. `check` validates checkout and seat; `Record.check` enforces one entry
  per key (it already enforces one per handle).
- `Find(c, u)`, `Drop(c, u)` and `Set`'s lookup become `(c, checkout, seat)`; `compareEntries`
  sorts by the new key.
- `ReadCopy`/`WriteCopy(root, c, u, ...)` become `(root, c, k SeatKey, ...)`; `Copy` gains `Team`,
  `Checkout`, `Seat`, checked against `k` in `decodeCopy`; `Copy.Desired(u, ...)` becomes
  `Desired(k, ...)` with `Session: paths.LXCSession(k)`. A new `DeleteCopy` for the absent phase.
- The record and copy stay `v: 1`: nothing was deployed with the per-user shape.

### `host/lxc/unit`

- `Spec` gains `Team`, `Seat`, `Key` (and the repo label from `Checkout`); `Render` writes
  C 3.9.4's new text: `Description=`, `RuntimeDirectory=teamchat-session-<key>`,
  `XDG_RUNTIME_DIR`, `ExecStart` with `tmux -S <socket> new-session -d -s <unit stem>` and
  `--state`/`--desired` from `paths.LXCLayout`. `testdata/session.service` is regenerated; the
  CL5 parse test of the `ExecStart` keeps passing (`--state` with `--desired` is allowed).

### `host/lxc/guestbus`

- `Systemd`'s `Start|Restart|Stop|ResetFailed(ctx, user GuestUser)` take the unit name (or the
  seat's name inputs) instead; `unitCall` stops calling `paths.SessionUnit(user)`. The package
  comment "the one unit a guest user has" changes. Transport and capped reader unaffected.

### `host/lxc/lxccmd`

- `guest.go`'s attach args gain `Key paths.SeatKey`; the op words append `--key <key>` for
  `place-bundle`, `membership` and `remove` (not `check`). The validation at guest.go:147 adds the
  key for those ops.

### `host/lxc/guestfacts`

- `Prefix(m declare.LXCMember, ...)` unaffected (it reads checkout and site only). `Passwd`
  unaffected; it is now called once per container and looked up per entry, which is a caller
  change, not a guestfacts change. If the repo label lives here rather than in `paths`, it is a
  new function; `paths` is preferred because every name is made there.

### `member/guest` (places.go, identity.go) and `member/cli/guest.go`

- `Identity.layout()` (places.go:61) uses `paths.HomeLayout(home, GuestRuntimeDir(user))`; it
  becomes `paths.LXCLayout(home, key)`, with `Key` on the op's input (from `--key`).
- `Membership`'s session check (places.go:264) `d.Session != paths.LXCSession(h.User)` becomes
  `!= paths.LXCSession(key)`.
- `PlaceBundle` writes under the key's state root and makes `lxc/<key>/` and `seats/` 0700 when
  absent (the existing `config()` pattern, one level deeper). `Remove` acts under the key's seat
  directory and also removes the key's desired file; `seat.lock` stays.
- `member/cli/guest.go`: the `--key` flag for the three ops (64 on a bad key), passed through.
- Tests: places_test.go's session test changes; new: two keys of one user and team apart.

### `member/cli` `run`

- No grammar change: `--state` with `--desired` already parses (CL5). B's text now names the LXC
  unit as a `--state` user; the "existing 0700 directory" rule holds because `place-bundle` makes
  it before the unit's first start.

## WIP on feat/bus-v2-n3b-p4 (read by grep only)

`host/lxc/` there (apply.go, absent.go, kit.go, restart.go, lxc.go):

- `lxc.go`: `Target{Container, User}` becomes `Target{Container, Key}` (`--member C/K`);
  `Line` gains `Checkout`/`Seat`/`Key` (or a rendered member field) and its `String` (lxc.go:75)
  prints `<user>@<container>/<repo>-<seat>-<key>`; `V1.Holds(ctx, r, u, team)` unaffected (v1 is
  per user).
- `apply.go`: the duplicate key at apply.go:208 becomes (team, container, checkout, seat) plus the
  per-container seat-key collision check across every team's records, and the "recorded for
  another user" refusal; `rec.Find(c, m.User)` (219) and `rec.Drop`/the `Entry` literal
  (993-995) use the new key; `users(g)` and `group(g, u)` (405-412, 757-760), which batch per
  user for step 08, go: `membership(ctx, g, u)` (771-808) becomes per member (per key), reading
  and writing the host copy by key; `unit(ctx, g, u)` and `removeUnit(ctx, g, u)` (849-931)
  become per member; `/etc/passwd` read once per container and each checkout read once
  (cache by checkout); the `guest check` call deduplicated per (user, team); `--seat next`
  replaced by `--seat <m.decl.Seat>` in the step 06 join; equal derived handles across entries
  of a team refused at step 03.
- `absent.go`: the `declared` set keyed by `Target{Container, User}` (69-79) becomes keyed by
  (container, checkout, seat); the synthesized `declare.LXCMember` for a record-only leaver gains
  `Checkout` and `Seat` from the record entry; `ReadCopy`/`WriteCopy` by key (132-138, 219);
  `removeUnit` per seat, unconditionally for a leaving seat (no more "user holds no team",
  224); `HomeLayout(..., GuestRuntimeDir(user))` (284) becomes `LXCLayout(home, key)`; the host
  copy deleted after `guest remove`.
- `kit.go`: `GuestRuntimeParents(m.decl.User, uid)` (222) becomes the login parent per uid plus
  a TB1 listing of the guest's `/run` for `teamchat-session-<key>` directories.
- `restart.go`: `Restart(ctx, d, container, user)` becomes `(ctx, d, container, key)`; the
  record lookup by key; `restartUnit` passes the seat's unit name.

## Not built yet, shaped by this decision

- LP1 (`health.Guests`): map (team, handle) to the record entry (container, checkout, seat,
  user) and probe `~<user>/.config/teamchat/lxc/<key>/seats/<team>/seat.lock`.
- `ccy-teams lxc-add` (N5's ccy-teams side): writes `seat` (lowest number no entry of the team
  and checkout declares and no LXC handle of the team's roster holds), computes `<key>` for
  `--member` and `lxc restart --member`.
- Area D's M1 writes `seat` into each staged `teamchat_lxc_members` entry (D's M1 row).

## Unaffected

- `guestroot` (openat2 reads and writes, TB1), `guestbus`'s transport and capped reader,
  `guestfacts`' parsers (`Passwd`, git config, env file, stat, init check), the kit digest
  write and swap (LX5's write half), the `lxc-attach` line's shape, TB2.4's output handling.
- The member's store, seat lock, link, keeper and their files (B 2.3-2.11): they live under
  whatever `<v2-state>` and `<v2-desired>` they are given.
- The ccy seat pool and sessions (C 3.7), host `cc`'s `HomeLayout`, the health tick's logic
  (HT rows), the child's member verbs (`member add --seat N` already exists), the record's
  status set and `since_ms` rule, n3b-D1's offline behaviour (now per seat, same rules).
- `apply`'s team-level `coordinator` (DC6): unchanged; one coordinator per team holds by
  construction.
