# Handoff: Plan 00209 M1, `bus/state` (member-side on-disk state), third

Written by the third m1-state agent. The next agent continues in the same worktree
(`untracked/worktrees/bus-v2-m1-state`) and branch (`feat/bus-v2-m1-state`). The locks (L1-L12)
and their proofs are another agent's, on a sibling branch: do not write locks code here.

## Done (committed, `qa-changed` green, `-race`, package coverage 95%)

- `c5ffd223`, `3d23efd1` (second agent): pyjson encoder; package core (`file.go`, `fields.go`,
  `grammar.go`).
- `3bb646bb` **team state** (`team.go`, `teamrecord.go`, `team_test.go`): `Team{Dir}`:
  `EnsureDirs`, `SyncToken`/`ForgetSyncToken`, `CommitBatch`, `Pending` (event-ID order; the
  caller orders by `origin_server_ts` after `bus/proto` validates), `KnownEventIDs`, `InInbox`,
  `Consume`, `TeamRecord`/`SaveTeamRecord`, `UntrustedReason`/`MarkUntrusted`,
  `About`/`SaveAbout`, `RoomView`/`SaveRoomView`, `SideRooms`/`SaveSideRooms`,
  `LogDrop`/`LogIgnored`/`DropCount`/`IgnoredCount`, `ForgeCache`/`SaveForgeCache`.
- `8ccfce5d` **outbox** (`outbox.go`): `Team.WithOutbox(func(*OutboxTx) error)` (L7 flock on the
  state dir, inline; the locks branch may replace `flockRetry` with its own), `Ping`, `Gate`,
  `Recent`, `OutboxTx.ReportTimes`/`SaveReportTimes` (S28), `DuplicateKey`, `TimeoutKey`,
  `SplitTimeoutKey`. The writer decodes what it writes (SB15).
- `b89593cf` **layout** (`layout.go`): `ResolveHome(getenv)`, `Home` paths, `Bundle`,
  `Checkout`/`Seat`, `ReportApplied`/`ReportConversation`, `MembershipDir` (`ErrBusOff`,
  `ErrNoMembership`), `DesiredPath`, `RelaunchPath`, `SessionsDir`, `InjectKey`/`InjectPath`.
- `f98882bf` **bundle** (`member.go`, `ip.go` + `ip_test.go` copied from statusline):
  `ReadMember`, `ReadToken(b, uid)`, `CheckDistinctBundles`, `CheckBundleLink`.
- SPEC section 12: SB13 (goodbye strict), SB14 (relative sessions dir), SB15 (outbox save).

## Next: the session and home files (S23, S24, S36-S45), test first

One `membership.go` (+ `_test`) for the membership files, keys checked `missingFirst`
(membership.py `_object` names a missing key first), caps 65536, written `pyjson.Sorted`:

- desired (read only, S36): keys `v session kind checkout generation written_ms seats farewell`;
  session `sessionPattern` (layout.go); kind `ccy|cc`; checkout `^/[^\x00-\x1f\x7f]{0,4095}$`
  (check `_CHECKOUT_RE` in membership.py); generation >= 1; written_ms >= 0; seats at most 64
  `{team, seat}` one per team; farewell at most 256 `{team, seat, reason: "removed"}`.
- applied (S37): required `v generation seats refused`, optional `told`, `inbound`; seats
  `{team, seat, handle, state:"held"}`; refused `{team, seat, why, holder}`, why in
  `held-elsewhere no-bundle bundle-invalid not-desired`, holder `-` or `_HOLDER_RE`
  (membership.py); told `{generation>=0, roles:[{team, handle, role: worker|coordinator}]}`
  at most 64. Written to the home and the report dir (both sorted + newline).
- told.json (S38): `v generation teams left`; teams `{team, seat, handle, role}` role
  `worker|coordinator|-`; left at most 256 `{team, reason:"removed"}`. told-roles.json:
  `{v, roles:{team: worker|coordinator}}` at most 64.
- `isHandle` is in grammar.go (`parse_handle`); `isTeamName`, `isSeatName` too.

Then `session.go`: relaunch (read, S40; Go writer `internal/teams/membership/desired.go`),
conversation (S39, sorted; `session_id` `[A-Za-z0-9_-]{1,128}`, source in
`startup resume clear compact other`), socket.json (S41, spaced; owner uid, `mode&0o077==0`,
8192 bytes ASCII, socket starts `/`, token non-empty), goodbye (S42, SB13), stop-guard (S43,
compact `{v, pending: null|64-char string, no_waker_ms, failure_ms}`), said-goodbye (S24,
`{"v":1,"claimed_ms":N}`), seat.lock claim read (S23, no lock: `seat <0|[1-9][0-9]{0,15}>`),
`active`/`down/<team>` (S44; down honoured 300 s against a passed-in `now`), inject marks
(`.spawned` ASCII digits \<= 16, `.transcript` per the first handoff's facts), the session
record (S45: teams joined by `,`, distinct, 0600, directory 0700 owned).

Then, as the first handoff: the corpus (`tests/fixtures/agent-bus-state/`), the Python walk, the
Go walk, the review.

## Gotchas

- Hooks: `, _ :=` is denied (use a helper that returns one value); `//nolint` is denied;
  `sed` is denied (edit through a small Python script under `untracked/scratch/` or Edit);
  a Bash command with `;` or a newline needs `&&` chains; the Write tool turns `\uXXXX` in
  text into the character (build such strings by concatenation); private IPs are denied in
  any write (use `192.0.2.x`, `2001:db8::`).
- Lint: `funcorder` wants a type's unexported methods after its exported ones in each file;
  `goconst` counts string literals across the package (key names are constants in team.go);
  `shadow` is on (reuse `err` with `=`); test helpers taking a format end in `f`.

## Findings to report (do not fix here)

F1 (statusline lacks `report_per_hour`), F4, F5, F6, F7, F8 as in the SPEC; bus/proto
validators to list from `grammar.go`; statusline's `member.go`/`ip.go` duplicate
`bus/state`'s and can be replaced by it.
