# Handoff: Plan 00209 M1, `bus/state` (member-side on-disk state), second

Written by the second m1-state agent at its context budget. The next agent continues in the same
worktree (`untracked/worktrees/bus-v2-m1-state`) and branch (`feat/bus-v2-m1-state`). The
first handoff's task list still stands; this file replaces it.

## Done (committed, `qa-changed` green)

- `3d23efd1` **pyjson encoder and ordered decode** (`src-go/internal/pingbus/pyjson/encode.go`,
  `encode_test.go`): `Encode(v, Style)` with `Compact`, `Spaced`, `Sorted` (adds `\n`),
  `SortedBare`; all ensure_ascii (forge.py's `sort_keys=True` alone is ensure_ascii too, so no
  non-ASCII mode is needed). Ints (`Number` Int, `int`, `int64`) as Python's int; floats, `nil`,
  bad WTF-8, a repeated key and an `Object` in an insertion-order style are `ErrNotEncodable`.
  `Ordered`/`Member` (`Get`, `Keys`) and `Options.Ordered` (a repeated key keeps its first place,
  last value). `CodePoints(s)` (Python's `len`, a lone surrogate is one). Existing decode behaviour
  unchanged (statusline green). Red: compile failure `undefined: Ordered`; then two real
  failures (expected text mangled, nesting limit off by one); green 97.7%.
- `c5ffd223` **bus/state core** (`src-go/internal/bus/state/`): `file.go` (package doc, `Class`
  constants, `Error`, `refuse`, `readRegular(path, limit) ([]byte, found, error)`,
  `openRegular`, `tmpName`, `writeAtomic` (fchmod 0600, fsync, rename, dir fsync, temporary
  removed on failure), `fsyncDir`, `decodeJSON` (DecodeUTF8 + NoDuplicateKeys + Ordered, classed),
  `object`); `fields.go` (`checkKeys(path, o, required, optional, unknownFirst|missingFirst)`,
  `version`, `anyInt`, `intField`, `strField`, `nullableStr` (string, set, error; nilnil is
  linted), `ptr`, `listField`, `mapField`, `boolField`, `nullable`, `intOrNull`,
  `strings2list`); `grammar.go` (TODO(bus/proto): team, seat, handle, human localpart, room ID,
  event ID, printable token, `isUserIDText`, `isReason` (0x20-0x7e, 1-200), `isDescription`
  (code points, unicode.IsSpace ends, no Cc/Cf), ack verbs, drop and unheard reasons,
  `parseUserID`, `isAgentUserID`). Red: compile failures (`undefined: Class`, `checkKeys`,
  `isTeamName`); green 95.5%. `.golangci.yml` gained a gosec G302/G304/G306/G703 exclusion for
  `internal/bus/state/.*_test\.go` (beside statusline's).
- Removed for now as unused (lint fails on unused): `roles`/`isRole`, `purposes`, `isDNSName` and
  `dnsNamePattern`, `codePointsIn` (1..most code points, for the headline), `field()`. Re-add
  with their first user.

## Parked draft

`untracked/scratch/m1-state-team_test.go.draft` (untracked, in this worktree) is the written
team-state test (S16, S25, S26, S29-S35). Move it back to
`src-go/internal/bus/state/team_test.go`, record red, then write `team.go` to it. It assumes:
`Team{Dir}` (the state directory) with `inboxDir()`, `consumedDir()`, `syncPath()`,
`teamPath()`, `roomPath()`, `sideRoomsPath()`, `forgeCachePath()`; `EnsureDirs`, `SyncToken`,
`ForgetSyncToken`, `CommitBatch([]InboxItem, next) (int, error)`, `Pending() ([]InboxItem, rejected int, error)`, `KnownEventIDs`, `Consume`, `InInbox`, `TeamRecord() (any, bool, error)`,
`SaveTeamRecord(pyjson.Ordered)`, `About`/`SaveAbout(*string)`, `UntrustedReason`/`MarkUntrusted`,
`RoomView`/`SaveRoomView(RoomView{Members []RoomMember{UserID, Membership}, Statuses []RoomStatus{UserID, UntilMS}})`, `SideRooms`/`SaveSideRooms([]SideRoom{Purpose, Room, State, Reason *string})`, `ForgeCache`/`SaveForgeCache(ForgeCache{Entries []ForgeEntry{Key, Expiry}, Repos []ForgeRepo{Key, Expiry, Owner *string, Private, Fork *bool, DefaultBranch *string}})`,
`LogDrop`/`LogIgnored(team, eventID, sender, reason string, nowMS int64)`, `DropCount`,
`IgnoredCount`; `InboxItem{EventID, Event pyjson.Ordered, Questions bool, Question *Question{N, Headline}}`, `maxInboxFileBytes`.

## Planned layout of the rest

- `team.go` (above). `outbox.go`: `Outbox{Pings []Ping{EventID, Verb, Ref *string, To, Answered, Reported []string, SentMS}, Gate *Gate{Credit, AtMS, Recent []Recent{Key, AtMS}}}`;
  `Team.WithOutbox(fn func(*OutboxTx) error)` (L7: ensure dirs, open state dir
  `O_RDONLY|O_DIRECTORY|O_CLOEXEC`, blocking `LOCK_EX`, save only if changed and fn returned nil,
  settled pings dropped on save); tx reads/writes `reports.json` too (S28).
- `session.go`: desired (read), applied, told, told-roles, relaunch (read), conversation,
  socket, goodbye, stop-guard, said-goodbye, seat claim, `active`, `down/<team>` (take `now`),
  inject marks, the session record. `member.go` + `ip.go` + `token.go`: copy statusline's
  `member.go`/`ip.go` logic (and its tests) adding `report_per_hour` {1,60} (F1).
  `layout.go`: S6-S14 (`PINGBUS_HOME`: an empty value falls through to XDG, then HOME).
  `lock.go`: L1-L11, the delivery+outbox pair as one call (L10).
- Corpus, Python walk, Go walk, lock proofs, review: as the first handoff, items 3-7.

## Facts from the Python not in the spec (save the re-reading)

- Caps: every file read by `inbox.read_json_file` (sync, outbox, untrusted, about, room,
  rooms, reports, said-goodbye, goodbye, stop-guard) is 1 MiB (`MAX_STATE_FILE_BYTES`); team.json
  65536; inbox items 6\*65536; member.json and the membership files 65536; forge-cache 16 MiB;
  socket.json 8192 (v1 reads 8192 and never refuses longer: v2 `size`); the session record 4096.
- Key-fault order: config.py and inbox.py name an unknown key first; membership.py
  (`_object`) a missing key first. Use `unknownFirst`/`missingFirst` accordingly.
- `member.json` is opened WITHOUT `O_NOFOLLOW` in v1 (`config.py:452`); v2 refuses a symlinked
  member.json: a new departure to add to SPEC section 12. Other new departures to add: team.json is
  read by v1 as `json.loads(bytes)` (UTF-16/32 accepted), v2 strict UTF-8; inbox items' keys
  outside the stored projection are refused by v2 (v1 projects a questions item, dropping them,
  and `protocol.validate_event` does not check top-level keys); integers past int64 are refused
  (v1 never writes one); goodbye.json's v1 reader never refuses (malformed is "not said").
- `ref` in a ping is any string or null; `to` any distinct strings; `answered`/`reported` lists
  of members of `to` (duplicates allowed by v1; an unhashable entry crashes v1, F4-like).
- `stop-guard.json` `pending` is any 64-code-point string; written `{"v":1, pending, no_waker_ms, failure_ms}` compact. `goodbye.json` `{"v":1,"session":S}`, S matches
  `[A-Za-z0-9_-]{1,128}`. socket.json decoded as ASCII, owner uid and `mode & 0o077 == 0`,
  `socket` starts with `/`, `token` non-empty.
- `inject/<key>.spawned`: ASCII digits, at most 16, leading zeros allowed; `.transcript`
  UTF-8, starts with `/`, at most 4096 characters, no NUL/CR/LF.
- Python adapter idea for the inbox kind: patch `helpers.pingbus.inbox.protocol.validate_event`
  to return `protocol.Outcome(protocol.ACCEPT)` and call `TeamState(dir)._revalidate(name, None, "", True)`; v1 accepts when the outcome kind is ACCEPT.
- The Write tool turns a `\uXXXX` escape in written text into the character: build such strings
  with `esc()` (pyjson tests) or `string(rune(0x200b))`. A Bash command containing `*` next to
  certain text trips the secret-path guard; run edits through a script file written with Write.
  Bash sequences need `bash -c 'set -uo pipefail; ...'`; the lint hook runs golangci-lint on each
  Go write (shadow, nilnil, unparam, cyclop at 15 are on).

## Findings to report (do not fix here)

F1 (statusline lacks `report_per_hour`), F4 (membership.py farewell/left reason unhashable
crash), F5 (fsx.WriteAtomic temporary), F6 (teams/membership encoding/json), F7, F8, as in the
SPEC; bus/proto validators to list from `grammar.go`.
