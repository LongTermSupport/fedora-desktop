# F6 field-compatibility audit: strict membership Read / ReadReport

Branch `fix/membership-strict-json` (tip before this audit 922fbf79, audit commit afda6e85).

## Verdict: SAFE

No `membership.json` or `applied.json` that any deployed writer can produce, at any version
since each file format was introduced, is refused by the strict Go readers. Every historical
shape is now a fixture in `strict_test.go`. The Go reader and pingbus's current Python readers
both accept all of them.

## 1. The files the readers read, and their writers

| Reader                  | File                                                             | Writers (all of them)                                                                                                                                                                                                                                                                                                    |
| ----------------------- | ---------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `membership.Read`       | `<membership dir>/membership.json` (desired)                     | Go only: `writeDesired` (`json.Marshal(Desired)`), reached from `ccy-teams internal membership-init` (launcher, ccy and `--kind=cc`) and `membership.Write` (`internal/teams/hot`: add, remove, farewell, restore). No Python, bash or other Go code writes it (`git log -S membership.json` over helpers/files/src-go). |
| `membership.ReadReport` | `<checkout>/.claude/ccy/pingbus/sessions/<session>/applied.json` | Python only: `helpers/pingbus/membership.write_applied(Applied.to_json())`, called by the seat keeper (`seat.py _publish`), inside a ccy container (image pingbus) or on the host for a cc session. The Go `Report` struct is marshalled only by tests.                                                                  |

`told.json`, `told-roles.json` and the LXC member record (`<applied>/lxc/<team>.json`) are not
read by these readers. The LXC kit runs no keeper and writes no `applied.json`.

## 2. Key sets each version ever wrote

### membership.json (desired)

The `Desired`, `Seat` and `Farewell` structs are byte-identical at every revision since the file
began: 235c8cc2 (M2, CCY 3.99.1 / container 2.68), c60b7ff0 (M3), b959973b (M6, `--kind=cc`) and
f460abb7. They are always these 8 keys, with no omitempty: `v, session, kind, checkout, generation, written_ms, seats[{team,seat}], farewell[{team,seat,reason}]`.

- **null:** not possible. `seats` and `farewell` are always non-nil: `ParseSeats` returns
  `[]Seat{}`, the launcher sets `Farewell: []Farewell{}`, and every `Write` ran `check()`,
  which refused nil seats or farewell before encoding.
- **Escapes:** encoding/json writes `&<>` as `&` and so on, and ends the file with a
  newline. The new fixture `desired: as writeDesired encodes it` covers both.
- **Values:** the old `check()` was looser on one point only: a checkout was `filepath.IsAbs`,
  where it is now no control character and at most 4096 characters. The launcher always required
  a clean absolute path. A checkout the new reader refuses (one with a control character) was
  already refused by pingbus's `_CHECKOUT_RE` in the container. That regex is the same at every
  revision since M1, so such a team never ran. `_SESSION_RE`, the seat grammar, the team grammar
  and `FAREWELL_REASONS` are unchanged since M1.

### applied.json (keeper report)

| Writer revision                                                     | Image range              | Keys                                                                                                                                     |
| ------------------------------------------------------------------- | ------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------- |
| 873fcd5f / 1f681d41 (M1)                                            | container 2.68 to 2.71   | `v, generation, seats[{handle,seat,state,team}], refused[{holder,seat,team,why}]`, optional `told{generation,roles[{handle,role,team}]}` |
| c60b7ff0 (M3) to HEAD (0ccf32d2, 0be311bd, c9691515, 37bee104, F44) | container 2.72 to 2.78.8 | as M1, plus optional `inbound` (bool, written only when known)                                                                           |

- **Null-capable:** none. `told` and `inbound` are omitted when None, never written as null.
  `holder` defaults to `"-"`, or is a lock kind from the closed set `watch, wait, recv, seat, keeper, unknown` (`inbox._read_kind`, the same since M1).
- **told.roles** come only from `ToldRecord.said()`, which filters to `ROLES`, so a `"-"` role
  is never written. In M1 `told` was only carried over from the keeper's own file after its own
  `parse_applied` had accepted it.
- **Constants:** `SEAT_STATES`, `WHY`, `NO_HOLDER`, `_HOLDER_RE`, `ROLES`, `MAX_SEATS` and
  `VERSION` are identical at every revision. The handle grammar in `protocol.py` is identical
  since 9d21853a (2026-10-07), before `applied.json` existed.
- **Plans 00211 and 00213** add no key to either file. `membership.py` was not touched by them,
  and their `seat.py` change (465ca2bb, the status-line pace) touches no applied, told, inbound
  or handle code.

## 3. Which old versions can still be deployed

- **ccy container:** the pingbus kit is baked into the image (`COPY optional/` to
  `/opt/claude-yolo/optional/agent-bus`). `REQUIRED_CONTAINER_VERSION` (2.78.8) forces a rebuild
  on the next launch, so an old keeper survives only in a container still running from before
  the host update. Containers older than 2.68 have no keeper and no `applied.json`. Their
  `ReadReport` result is `os.ErrNotExist`, as before. Stale reports from any keeper stay on disk
  beside the checkout, so the M1 and M3 shapes can be read long after their container exits.
- **LXC kit:** replaced on each play run, and it writes neither file.
- **Host (ccy-teams, launcher, cc pingbus):** binaries come from the current checkout. A
  `membership.json` left in `XDG_RUNTIME_DIR` by an older binary has the same struct, as shown
  above.

## 4. and 5. Checked against the strict reader, and added as fixtures

`untracked/scratch/f6/gen_applied.py` ran each revision's own `write_applied` from
`git archive <rev> helpers/pingbus` at 873fcd5f, 1f681d41, c60b7ff0, 0ccf32d2, 0be311bd,
c9691515, 37bee104 and HEAD. It wrote these shapes:

- empty
- seats and refused (every `why`; holders `seat`, `keeper`, `unknown` and `-`; a word seat; an
  lxc handle)
- told with roles
- told with no roles
- inbound false (M3 on)
- inbound true with told (M3 on)

Every shared shape is byte-identical across all revisions. `compare.py` confirmed that the
fixtures in `keeperCases()` are those exact bytes, trailing newline included.

Commit afda6e85 adds them, plus `launcherCase` (desired), to `desiredCases` and `reportCases`.
Both tests check every case:

- `TestReadersTakeExactlyWhatTheWritersWrite` (Go)
- `TestPingbusReadsTheCasesTheSameWay` (current Python `read_desired` and `read_applied`)

Both pass. `qa-changed` passes (go-qa diff mode and 6 Go-built suites). There are no
differences between the Go and Python readers to record.

## 6. Refused shapes

None. Nothing needed loosening.

## Notes (not refusals)

- The launcher now runs `checkWrite` (922fbf79). It stops a launch whose checkout has a control
  character, is over 4096 characters, or is not UTF-8. The first two were already unusable,
  because pingbus refuses them. A non-UTF-8 checkout was previously written with U+FFFD in place
  of the bad bytes, so pingbus would have read a different path. Refusing it is a change in a
  case that could never have worked as intended.
- `Report` marshalled by encoding/json with nil `Seats` or `Refused` would write `null`, which
  the reader refuses. Only tests do that, never a production writer.
