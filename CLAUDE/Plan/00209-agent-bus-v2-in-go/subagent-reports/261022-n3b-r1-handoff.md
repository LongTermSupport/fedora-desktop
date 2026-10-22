# n3b r1 handoff (Plan 00209 N3, slice n3b, rework step R1 after n3b-D2)

Worktree `untracked/worktrees/bus-v2-n3b-r1`, branch `feat/bus-v2-n3b-r1`. Brief:
`subagent-reports/261022-n3b-d2-rework.md`, section "Built on feat/bus-v2-n3b".

## State of the branch

- Commit `fac27f3c3`, item 1 (declare): green, qa-changed passed.
- The next commit (WIP) holds items 2 (paths) and 3 (lxcstate). Their own packages are green
  (`go test -race`, golangci clean), but **the module does not build**: the callers in items 5
  and 6 still use the old per-user API. The first errors `go vet ./...` gives:
  - `host/lxc/guestbus/guestbus.go:166` calls `paths.SessionUnit(user)`;
  - `member/guest/places.go:62` calls `paths.GuestRuntimeDir(h.User)` (now takes a `SeatKey`);
  - `member/guest/places.go:264` calls `paths.LXCSession(h.User)` (now takes a `SeatKey`);
  - `host/lxc/unit/unit.go` calls `paths.HomeLayout(s.Home, paths.GuestRuntimeDir(s.User))`.
    The module builds again once items 4-6 are done. qa-changed has not been run on the WIP
    commit, which is why it is a WIP commit.
- Flaky, not caused by this work: `host/lxc/guestroot`'s
  `TestEveryFailingCallFailsItsOperationAndLeaksNothing/RemoveTree` failed once (EIO, then an
  index-out-of-range panic in `write_test.go:485`) under `go test ./internal/teamchat/...`
  with every package running at once; run on its own it passes. Worth a look by whoever owns
  guestroot.

## Items done

### 1. declare (fac27f3c3)

- `LXCMember.Seat` is required (`readLXCMember`'s required keys gain `seat`); read by the new
  `(node).seat()` in values.go (1-9999), shared with the gate.
- Duplicate key (team, container, checkout, seat), message "repeats another entry's team,
  container, checkout and seat".
- `validCheckout` refuses `\n` and `\r`.
- `LXCEntry{Container, Checkout, Seat, User, HumanText}`; `String()` is
  `<container>/<seat>,user=<user>,human_text=<bool>,checkout=<checkout>`; `ParseLXCEntry`
  splits at the first three commas (checkout may hold `,` and `=`), requires a canonical
  decimal seat 1-9999 and declare's checkout rule. `who()` is (container, checkout, seat), so
  `--set` changing `user` is one change. approved.json entries carry `container`, `checkout`,
  `seat`, `user`, `human_text`; repeat message "repeats another entry's container, checkout
  and seat".
- `input.go`'s `encode` writes `seat` always. `host/apply_test.go`'s fixture gained `seat`.
- Coverage: declare 96.0%, hostvars 96.5%.

### 2. paths (WIP commit)

Exported API as built:

- `type SeatKey` (16 lowercase hex; `ParseSeatKey`, `String`, `MarshalText`, `UnmarshalText`;
  guarded, so its zero value panics in every name).
- `LXCSeatKey(team TeamID, checkout string, seat int) SeatKey`: panics on a checkout or seat
  declare refuses, and on a zero team.
- `LXCRepoLabel(checkout string) string` (C 3.9.1's rule; cases tested).
- `LXCDesiredCopy(c ContainerName, k SeatKey)`, `LXCSession(k SeatKey) SessionName`.
- `LXCLayout(home, runtimeDir string, k SeatKey) (Layout, error)`.
- `SessionUnit(team TeamID, checkout string, seat int) string`; it computes the repo label and
  key itself, so no caller can pass a mismatched key. The same signature serves
  `GuestUnitFile`, `GuestUnitWantsLink` and `GuestTmuxSession` (the unit name less `.service`).
- `GuestRuntimeDir(k SeatKey)`, `GuestTmuxSocket(k SeatKey)`, `GuestUnitRunRoot(k SeatKey)`.
- `GuestRuntimeParents` is gone: `GuestLoginRunRoot(uid int) (string, error)` is the login
  parent, and `ParseGuestRuntimeDirName(name string) (SeatKey, error)` is the grammar the kit
  scan uses to list `/run/teamchat-session-<key>` (the run root is then `GuestUnitRunRoot(k)`).
- `HomeLayout` is for host `cc` only now.
- Tests cover the longest unit (94 bytes, a tmux session of 86 with no `.` or `:`), B's sockets
  under the runtime directory (link 78, inbox 60), the label cases, and key distinctness.
  Coverage 99.3%.

### 3. lxcstate (WIP commit)

- `Entry{Container, Checkout, Seat, User, Handle, Status, SinceMS, Why}`; sorted by (container,
  checkout, seat); `check` validates checkout and seat. The handle rule: at most one entry that
  is not `left` per handle, and `left` entries may share it.
- `Find(c, checkout, seat)`, `Drop(c, checkout, seat)`, and `Set` by the same key.
  `ByHandle` skips `left` entries.
- New `type SeatID{Team, Checkout, Seat}` with `Key()`; `Record.SeatOf(e Entry) SeatID`.
- `Copy{V, Team, Checkout, Seat, Generation, Seats, Farewell}` (JSON in that order), `NewCopy(id)`,
  `Copy.ID()`. Every seat and farewell must be the copy's team.
- `ReadCopy(root, c, id SeatID)`: the file's identity must equal `id`.
  `WriteCopy(root, c, cp)`: the path comes from `cp.ID().Key()`.
  New `DeleteCopy(root, c, k SeatKey)`: a copy already gone is no error.
- `Desired(writtenMS)`: the session is `LXCSession(cp.ID().Key())`.
- Both files stay `v: 1`. Coverage 96.5%.

## Items left

4. `host/lxc/unit`: `Session` gains `Team`, `Seat` (plus `Checkout`, which it already has).
   `Render` writes C 3.9.4's new text (DESIGN-v2-C-host.md near line 2027: the Description,
   `RuntimeDirectory=teamchat-session-<key>`, `XDG_RUNTIME_DIR=GuestRuntimeDir(k)`, and
   `ExecStart=/usr/bin/tmux -S <GuestTmuxSocket> new-session -d -s <GuestTmuxSession> /usr/local/bin/teamchat run --claude --kind lxc --key <key> --resume-recorded -- claude`).
   Then regenerate `testdata/session.service` and make the CL5 parse test follow `--key`.
5. `guestbus` (`unitCall` takes the unit name), and `lxccmd`'s `guest.go` (`Key paths.SeatKey`;
   `--key` for place-bundle, membership and remove).
6. `member/guest` (`places.go` layout via `paths.LXCLayout(home, paths.GuestRuntimeDir(key), key)`,
   the session check, PlaceBundle making `lxc/<key>/` and `seats/` 0700) and
   `member/cli/guest.go` (`--key`, 64 on a bad key).
7. `member/cli` `run`: `--key` for `--kind lxc`.
8. `apply`: remove `--seat next`, and the `retired` class (exit 70).

Scratch helper for exact counted replacements (untracked):
`untracked/scratch/r1sub.py FILE SPEC.py`.
