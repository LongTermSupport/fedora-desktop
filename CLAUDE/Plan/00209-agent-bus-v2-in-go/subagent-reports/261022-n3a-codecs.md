# n3a codecs (Plan 00209 N3, slice n3a, part codecs: MS1's member files)

Branch `feat/bus-v2-n3a-codecs`, from `d27d36a0`. One commit, `a5b42fb8b`. qa-changed passed.
golangci-lint reports 0 issues with and without `--build-tags tuwunel`.

## What was built

All of it is in `src-go/internal/teamchat/member/state`, one codec per file. Each file has an
exported struct, `Encode()` (sets `v`, then runs its own decoder over the output), `DecodeX`
(through the existing `decodeStrict`, plus the `rules` checks) and `MaxX`. Reads and writes go
through the existing generic `state.Read` / `state.Write` over a `homefs.Root`.

| File                | Type           | Cap    | Source                                   |
| ------------------- | -------------- | ------ | ---------------------------------------- |
| `session.json`      | `Session`      | 8 KiB  | `session.go`, B 2.3.3                    |
| `keeper.json`       | `Keeper`       | 4 KiB  | `keeper.go`, B 2.3.3, B 2.9 (4 KiB read) |
| `report.json`       | `Report`       | 4 KiB  | `report.go`, B 2.3.3                     |
| `status.json`       | `Status`       | 64 KiB | `status.go`, B 2.9                       |
| `conversation.json` | `Conversation` | 1 KiB  | `conversation.go`, B 2.3.3               |
| `session.pid`       | `SessionPID`   | 1 KiB  | `sessionpid.go`, B 2.4.1 step 4          |
| `desired.json`      | `Desired`      | 64 KiB | `desired.go`, C 3.7.5, B 2.10            |

`values.go` holds what the files share:

- the closed sets as exported constants: `Kind*`, `Inbound*`, `Link*`, `Desired*`, `Delivery*`, `Source*`;
- `Process{PID, StartTicks}`, where a pid runs from 1 to PID_MAX_LIMIT (2^22) and ticks are 0 or more;
- the session id grammar;
- `validTranscript`, which `store.go` now uses as well (the one edit to an existing file, plus
  dropping its unused `path/filepath` import);
- `validSocket`, `optionalOf` and `optionalMS`.

`report.go` adds `SeatRef{Team, Seat}`, a text type for `"<team>/<seat>"`.

Tests (`*_test.go`):

- **Shared helpers in `values_test.go`.** `strictRefusals` runs on every file. It checks that
  each file refuses: non-JSON, a value after the JSON, a repeated key, a float, `v` 2, a missing
  `v`, an unknown key, and a key in another case. It also checks that a file of exactly its cap
  is read and one byte more is refused. `refusesAll` checks the code of each refusal, and that the
  error quotes nothing the edit wrote. `roundTrips` writes through homefs and reads back
  byte-identical. It also checks that the zero value is refused by Encode, that Write of the zero
  value writes nothing, and that a missing file is `fs.ErrNotExist`.
- **Per file:** every rule's refusal; each count cap at n+1 (64 roles, 64 teams, 64 seats, 64
  farewells, 2048 pending); and the largest valid file of each, which is proven to fit in at most
  half its cap (session.json: within the cap).

Coverage: `member/state` 99.7%.

## Decisions (proposed design text in quotes)

1. **session.json's started fields go together.** `session_id`, `source`, `transcript`,
   `socket` and `claude` are all null or all set. `end_reason` and `frame_told: true` need a
   started session.
   - `source` is one of `startup|resume|clear|compact`, Claude Code's SessionStart sources.
   - `session_id` is a lowercase UUID. The same rule applies in conversation.json.
   - `socket` must be `<abs>/cc-socks/<claude.pid>.sock` (`paths.InboxSocket`), at most 107
     bytes so it can be bound.
   - `transcript` is an absolute, clean `.jsonl` path of at most 1024 bytes (the store's bound).
   - `told.roles` holds at most 64 teams, and each role is coordinator, worker or probe.
   - B 2.3.3 proposed text: "Until the first SessionStart, `session_id`, `source`, `transcript`,
     `socket` and `claude` are all `null`; afterwards all are set. `source` is SessionStart's
     (`startup`, `resume`, `clear`, `compact`). `socket` is `cc-socks/<claude.pid>.sock`, at most
     107 bytes. The file is at most 8 KiB."
2. **keeper.json.** `why` is set exactly when `failed`, and `next_ms` exactly when `restarting`.
   `session` is set exactly for kind ccy or lxc, since those are the kinds whose recovery names
   `ccy-teams restart <session>`. Proposed: "`session` is the session's name for kind `ccy` or
   `lxc`, and `null` for `host` and `other`."
3. **report.json.**
   - `last_sync_ok_ms` is nullable, because a keeper has no sync before its first one.
   - `seat` is `SeatRef`, and its `<seat>` must be the handle's seat.
   - `pending` runs from 0 to 2048, the store's cap.
   - `desired.code` is null exactly when `applied`.
   - `store` is `ok|refused`.
   - Proposed: "`last_sync_ok_ms` is `null` until the first sync succeeds; `seat`'s `<seat>` is the
     handle's seat."
4. **status.json `failed`.** It is null or one of `unauthorized|forbidden|trust|bundle`, B17's
   four permanent failures. `pending` is at most 2048 and `missing` at most 16, the store's caps.
   Teams are distinct. Proposed in B 2.9: "`failed` is `null` or the permanent failure that stopped
   the team (B17): `unauthorized` (401), `forbidden` (403), `trust`, `bundle`."
5. **session.pid.** `session` is null or a process whose pid is not the keeper's. `kit` is a
   `paths.Digest`. `launch` is `claude|plain`.
6. **desired.json codes.**
   - `CodeSeat` covers: a seat off the handle's seat grammar, a team listed twice in `seats`, a
     farewell listed twice, and a farewell for a seat `seats` still holds.
   - A team id off its grammar is `schema`, as every typed id is in these files. So are the count
     caps.
   - A farewell for the same team at a different seat is allowed: a seat was swapped.
   - `generation` is 0 or more.
   - Proposed in B 2.3.3: "`seat` names a seat rule: a seat off A 2.3's grammar, a team twice in
     `seats`, a farewell twice, or a farewell for a seat still desired".
7. **Caps the design does not give.** session.json 8 KiB, report.json 4 KiB,
   conversation.json 1 KiB, session.pid 1 KiB, desired.json 64 KiB. For each, a test proves that
   the largest valid file fits.

## Notes for the next part

- The writer part's `StoreWriter` should reuse `Read`/`Write`'s pattern. These codecs all have
  `Encode` and satisfy `Encoder`; the store has none.
- Gap 4 (switching seat and health to `state.DecodeLockRecord`) and step 05 are not in this part.
- When merging, `store.go` has one changed hunk (Awaiting.check) and one dropped import.
