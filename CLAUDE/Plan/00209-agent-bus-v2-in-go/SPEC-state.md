# Plan 00209 SPEC-state: `bus/state`, the member side's on-disk state

The Go package `src-go/internal/bus/state` reads and writes every file a member's pingbus
keeps, at v1's paths, in v1's formats, under v1's locks, so a v1 (Python) process and a v2
(Go) process can take turns on one seat, one `state/` and one `PINGBUS_HOME` (DESIGN.md 5.1,
5.2, 7, R42, R43, R47). Host-side files (`registry.json`, `/var/lib/agent-bus/<team>/team.json`,
stamps, records) are a later stage and are out of scope, except where the member reads the same
file.

Each expectation is numbered S1..., cites the v1 code it comes from (`helpers/pingbus/<file>:<line>`
on this branch, after the F44 merge), and is proven by a Go test written first and seen red. A
place where v1 is wrong is a departure, SB1..., listed in section 12 with its reason. The shared
corpus (section 11) is walked by both the Python helper tests and the Go tests; it is built from
the formats and the found bugs, never recorded from either implementation.

## 1. The rule every file follows

- S1. A reader accepts exactly what v1 writes and refuses everything else with a typed error
  (`*state.Error`, its `Class` one of section 2), naming the file and never quoting a value
  read from it (a token may have been pasted anywhere: `config.py:9-13`).
- S2. A writer writes the version the oldest live v1 reader accepts: `v` is `1` in every file
  (DESIGN 5.1). No file gains a key while v1 reads it: v1's readers check key sets exactly
  (`inbox.py:433,549,570,606,726,743,770`, `membership.py:181-191`), so one new key makes v1
  refuse the file (Plan 00211 SPEC section 7 moved report times to their own file for this reason).
- S3. A writer writes the bytes v1's writer writes for the same value: the same separators, key
  order, `ensure_ascii` escaping and trailing newline (or none), per file (section 4). The corpus's
  valid files are those bytes; Go decodes each and re-encodes it to the same bytes.
- S4. Every read is of a regular file, never through a symlink (`O_NOFOLLOW`), never blocking on
  a FIFO (`O_NONBLOCK`), close-on-exec, of at most the file's cap plus one byte
  (`inbox.py:234-249`, `membership.py:349-366`, `config.py:452-478`). A symlink, a non-regular
  file or a file over its cap is refused (class `file` or `size`). An absent file is "absent",
  which each reader turns into its own answer (none, empty, or refused where v1 refuses absence).
- S5. JSON is read as CPython's `json.loads(raw.decode("utf-8"))` reads it, through
  `internal/pingbus/pyjson` (`DecodeUTF8`), with duplicate keys refused everywhere (SB1). An
  integer is Python's `type(v) is int` (never a bool, never `1.0`; `_is_int`,
  `inbox.py:188-189`, `limits.py:77-78`, `membership.py:177-178`).

## 2. Error classes

`Class` values (the corpus names them in `cases.json`):

| Class           | Meaning                                                                                  |
| --------------- | ---------------------------------------------------------------------------------------- |
| `version`       | `v` (or `protocol`) is an integer, but not one this reader accepts                       |
| `unknown-key`   | an object has a key the format does not name                                             |
| `missing-key`   | an object lacks a key the format requires                                                |
| `duplicate-key` | an object names one key twice                                                            |
| `type`          | a value is the wrong JSON type (a bool or float where an integer is due, `v: true`, ...) |
| `grammar`       | a string or integer of the right type fails its grammar or bound (a handle, an event ID) |
| `size`          | the file, a list or a map is over its cap                                                |
| `syntax`        | not UTF-8, not JSON, or not the single-line text format the file has                     |
| `file`          | not a regular file, a symlink, the wrong owner or mode (a token, a socket record)        |

DESIGN 5.4 names six classes; `missing-key`, `syntax` and `file` are added because v1 refuses
those cases too and folding them into another class would make the corpus lie (SB2). v1 has
no classes: the Python corpus walk checks accept/refuse only, the Go walk checks the class.

## 3. Paths: one home for every path

- S6. `PINGBUS_HOME` is `$PINGBUS_HOME` when set (absolute, else refused), else
  `$XDG_CONFIG_HOME/pingbus` when that is set and absolute (a relative one is ignored), else
  `$HOME/.config/pingbus` (`HOME` absolute, else refused) (`config.py:122-139`).
- S7. Under `PINGBUS_HOME`: `<team>/` (a bundle, or in a session home a symlink onto a seat
  directory), `<team>/member.json`, `<team>/token`, `<team>/README`, `<team>/state/`;
  `stop-guard.json` (`hooks.py:78`), `goodbye.json` (`hooks.py:79`), `watch.log`
  (`seat.py:52`), `told.json`, `told.lock`, `told-roles.json` (`membership.py:62-71`),
  `relay/lock` (`relay.py`, `Spool.exclusive`), `inject/<key>.{watch,transcript,stalled,missing,spawned}`
  (`inject.py:1-17`), `down/<team>` (`statusline.py:84-88`).
- S8. A session home (`/tmp/pingbus-home` in ccy, `seat.py:71`; a cc session's own, given by
  `--home`) adds `keeper.lock`, `keeper.log` (`seat.py:48-50`), `applied.json`, `active`
  (`membership.py:38-40`), `settings.json`, `statusline-inner` (`seat.py:65-66`) and
  `socket.json` (`notify.py:52`).
- S9. Under `<team>/state/` (`inbox.py:51-115`): `inbox/`, `consumed/`, `sync.json`,
  `outbox.json`, `lock`, `team.json`, `untrusted.json`, `room.json`, `about.json`,
  `rooms.json`, `reports.json` (Plan 00211), `forge-cache.json` (`forge.py:60`),
  `dropped.log`, `ignored.log`.
- S10. A checkout's seats are `<checkout>/.claude/ccy/pingbus/seats/<team>/<seat>/`
  (`seat.py:74`), each a bundle plus `state/`, `seat.lock` and `said-goodbye.json`
  (`seat.py:76-79`); its sessions' reports are `<checkout>/.claude/ccy/pingbus/sessions/<session>/`
  holding `applied.json` (`seat.py:709`) and `conversation.json` (`hooks.py:217-223`). Inside a
  ccy container the checkout is `/workspace` (`seat.py:75`); the conversation's directory is
  under `/workspace` for kind `ccy` and under the desired file's `checkout` for kind `cc`.
- S11. The desired membership is `$PINGBUS_MEMBERSHIP/membership.json`, with `relaunch.json`
  beside it (`membership.py:32-37`; Go writer `src-go/internal/teams/membership/desired.go:20`).
  `PINGBUS_MEMBERSHIP=off` means no bus; a relative value is refused (`membership.py:369-377`).
- S12. The status line's session record is `$PINGBUS_SESSIONS_DIR/<session_id>` or
  `/tmp/pingbus-sessions-<uid>/<session_id>` (`statusline.py` `sessions_dir`, `hooks.py:242-262`).
- S13. `inject/<key>`: `key` is the first 16 hex digits of SHA-256 of the session's socket path
  encoded UTF-8 with surrogatepass (`inject.py:44-45`).
- S14. Every path is built by one `state.Layout`-style set of functions; no caller joins a v1
  file name by hand.

## 4. Writing: atomic, with v1's modes and v1's temporaries

- S15. One atomic writer: a temporary `.<name>.<16 lowercase hex>.tmp` beside the target, opened
  `O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW|O_CLOEXEC` with the file's mode, `fchmod` to that mode,
  written, fsynced, renamed over the target, and the directory fsynced; the temporary is removed
  on any failure (`inbox.py:209-232`, `membership.py:465-478`). The temporary's name must match
  v1's `_TMP_NAME_RE` `\..+\.[0-9a-f]{16}\.tmp` (`inbox.py:121`), because v1's `pending()` skips
  only that shape and counts anything else in `inbox/` as a rejected item (`inbox.py:456-465`).
  `internal/fsx.WriteAtomic` (`<path>.tmp.<pid>`, `os.WriteFile`, no fsync, follows a symlink)
  must not be used for these files (finding F5).
- S16. Modes: every file 0600, every directory 0700 (`inbox.py:96-97`, `membership.py:468`,
  `notify.py:59`, `inject.py:39-40`, `seat.py:69-72`). `ensure_dirs` creates `state/`, `inbox/`,
  `consumed/` with 0700 and a `chmod`, and refuses one that is not a real directory
  (`inbox.py:378-384`). Reads create nothing (`inbox.py:359-360`).
- S17. The JSON styles v1 writes, by file:

| Style       | `json.dumps` call                                           | Newline | Files                                                                                                                                                                                                        |
| ----------- | ----------------------------------------------------------- | ------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| compact     | `ensure_ascii=True, separators=(",", ":")`, insertion order | none    | `sync.json`, `outbox.json`, `team.json`, `untrusted.json`, `about.json`, `room.json`, `rooms.json`, `reports.json`, inbox items, `said-goodbye.json`, `goodbye.json`, `stop-guard.json` (`inbox.py:295-307`) |
| sorted      | `ensure_ascii=True, sort_keys=True` (`", "`, `": "`)        | `\n`    | `applied.json` (home and report), `told.json`, `told-roles.json`, `conversation.json` (`membership.py:465-467`, `hooks.py:233-236`)                                                                          |
| spaced      | `ensure_ascii=True` (`", "`, `": "`), insertion order       | none    | `socket.json` (`notify.py:63`)                                                                                                                                                                               |
| sorted-bare | `sort_keys=True` (`", "`, `": "`)                           | none    | `forge-cache.json` (`forge.py:276-287`)                                                                                                                                                                      |
| go-compact  | Go `json.Marshal` (`v, generation, written_ms[, why]`)      | `\n`    | `relaunch.json` (host-written, `desired.go:112-131`); `membership.json` (host-written, `membership.go:202-226`)                                                                                              |

`ensure_ascii` writes every non-ASCII code point as `\uXXXX` (a pair above U+FFFF), and
`"`, `\\`, `\n`, `\r`, `\t`, `\b`, `\f` as their short escapes, other controls as `\u00XX`.
The encoder is added to `internal/pingbus/pyjson` (its home, "Python-compatible JSON"), with
an ordered object so insertion order survives a read-modify-write (`outbox.json`'s
`gate.recent` keeps the order v1 read it in: `limits.py:226-250`, `inbox.py:779-782`).
Floats are never written (no state file holds one); the encoder refuses them.

## 5. The bundle (read only; the host writes it)

- S18. `member.json` (`config.py:44-55,452-546`): at most 65536 bytes, a JSON object, duplicate
  keys refused; keys exactly the nine required plus optional `human_text` (bool, default true)
  and `limits` (an object of `limits.BOUNDS` keys only, each an integer in its inclusive bound,
  `limits.py:37-47,103-112`; an explicit `null` refused, `config.py:620-631`). `protocol` is the
  integer 1 (class `version` for another integer, `type` for anything else); `team` equals the
  directory's name; `server_name` a lowercase DNS name of at most 253 characters; `user_id`
  `@<agent handle>:<server_name>`; `admin` `@admin:<server_name>`; `room` a room ID;
  `token_file` exactly `"token"`; `plain_http_hosts` a list of distinct IP literals each in
  Python's canonical form (`str(ipaddress.ip_address(x)) == x`, no `%`, `config.py:558-574`);
  `base_url` `https://<host>[:port]` or `http://<ip>[:port]` with the IP listed, no
  credentials, path, query or fragment, as `urllib.parse.urlsplit` reads it
  (`config.py:577-617`). The checks run in v1's order and the first failure is the error.
  `internal/pingbus/statusline/member.go` and `ip.go` already implement this reading with
  parity tests; `bus/state` carries the same logic (finding F1: statusline's bounds lack
  `report_per_hour`, so it refuses a bundle v1 accepts).
- S19. `token` (`config.py:327-375`, R43): opened `O_RDONLY|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC`;
  on the open descriptor: a regular file, owned by the expected uid, mode with no bit outside
  0600 (class `file`); at most 4096 bytes (`size`); one or more printable ASCII characters
  `[\x21-\x7e]+`, so no space, no newline (`syntax`). Re-read and re-checked on every use and
  never kept on the member value. A refusal never quotes the content.
- S20. Two bundles of one command sharing a `user_id` or a token are refused
  (`config.py:406-416`).
- S21. A team entry that is a symlink must point at a real directory (not a link) owned by
  the user with mode exactly 0700 (`config.py:305-324`). The Element refusal for a `host`
  member (`config.py:249-296,423-449`) is the CLI's concern, not this package's; it is listed so
  the integrator places it.
- S22. `README` is the host's text for a human; the member never reads it.

## 6. Seat files

- S23. `seat.lock` content is `seat <claim_ms>`, `claim_ms` matching `0|[1-9][0-9]{0,15}`
  (`inbox.py:980-999`); a read takes no lock and returns "no claim" for an absent or empty file
  or any other text. Written only by the lock (section 9).
- S24. `said-goodbye.json` is `{"v":1,"claimed_ms":N}` (compact, `seat.py:266-268`).
  `goodbye_said` (`seat.py:271-276`) is lenient (finding F7): v2 decodes it strictly and the
  caller decides; the corpus pins v1's leniency (SB3).

## 7. Team state (`<team>/state/`)

- S25. `sync.json`: exactly `{"v":1,"next_batch":T}`, `T` 1 to 4096 printable ASCII without
  space (`inbox.py:115-117,198-201,428-438`). Written after the batch's inbox files and the
  inbox directory are fsynced (`inbox.py:400-426`); removed by `forget_sync_token` with a
  directory fsync (`inbox.py:440-448`).
- S26. Inbox and consumed items, `inbox/<event ID>.json` (`inbox.py:13-15,98-103,252-277,450-520`):
  at most `6 * 65536` bytes; a JSON object whose `event_id` equals the file name's event ID
  (`\$[A-Za-z0-9_-]{43}`); its keys a subset of `type, event_id, sender, origin_server_ts, content`, plus, for a questions-room item only, `room: "questions"` and `question` (null or
  exactly `{"n": int >= 1, "headline": 1-120 characters}`), both or neither. This package checks
  the projection; whether the event is acceptable is `protocol.validate_event`, `bus/proto`'s.
  v1 counts a stored file that fails as rejected and skips it (it never raises); v2's decoder
  returns the typed error and the listing counts it. Listing skips v1 temporaries (S15). Consume
  is a rename into `consumed/` with both directories fsynced; a missing source is "not pending".
- S27. `outbox.json` (`inbox.py:688-704,755-885`, R42): exactly `{"v":1,"pings":[...],"gate":G}`.
  Each ping exactly `event_id` (event ID), `verb` (a verb whose rule expects an ack: `bus/proto`'s
  table), `ref` (string or null), `to` (a non-empty list of distinct strings), `sent_ms` (int),
  `answered` and `reported` (lists, each a subset of `to`). `G` is null or exactly
  `{"bucket":{"credit":int>=0,"at_ms":int},"recent":{key:int,...}}` (`limits.py:205-217,239-250,276-283`);
  v1 checks the gate only when a send uses it (`inbox.py:788-801`), v2 at decode (SB4).
  Settled pings (every target answered or reported) are dropped on save (`inbox.py:779-786`).
  The TIMEOUT key `<event_id>~<localpart>` is the relay spool's name for one owed TIMEOUT
  (protocol section 12); the outbox stores `reported` targets as user IDs.
  The file is read and written only under the outbox lock (L7), saved only when the transaction
  changed it and ended without error; the send gate is charged inside it and no network work is
  done under it.
- S28. `reports.json` (Plan 00211, `inbox.py:716-736`): exactly `{"v":1,"sent":[int,...]}`,
  read and written under the outbox lock.
- S29. `team.json` (`inbox.py:326-349,524-530`): at most 65536 bytes, the team record content as
  the syncer verified it, written compact; its meaning is `protocol.parse_team_record`
  (`bus/proto`). This package reads the bytes (regular file, cap) and decodes the JSON; a
  missing file is "not joined". Saving it removes `untrusted.json`.
- S30. `untrusted.json`: exactly `{"v":1,"reason":R}`, `R` 1-200 printable ASCII
  (`inbox.py:61,204-206,554-573`). Saving it removes `team.json`, `about.json` and `rooms.json`.
- S31. `about.json`: exactly `{"v":1,"description":D}`, `D` 1-120 characters, stripped, no
  Unicode `Cc`/`Cf` (`inbox.py:532-552`, `protocol.py:464-468`). A team with no description has
  no file.
- S32. `room.json`: exactly `{"v":1,"members":{uid:"join"|"invite"},"statuses":{uid:int>=0}}`,
  each map at most 4096 entries, each user ID printable ASCII `@[\x21-\x7e]{1,254}` with a `:`
  after its second character (`inbox.py:71-72,299-302,575-590,742-752`).
- S33. `rooms.json` (Plan 00211): exactly `{"v":1,"rooms":{purpose:{"room","state","reason"}}}`,
  purpose `reports` or `questions`, `room` a room ID, `state` one of `join pending untrusted refused`, `reason` null or 1-200 printable ASCII (`inbox.py:106-111,280-292,592-611`).
- S34. `forge-cache.json` (`forge.py:60-65,180-287`): at most 16 MiB; `{"v":1,"entries":{k:int>=0}}`
  with optional `repos` `{k:{"exp":int>=0,"owner":str|null,"private":bool|null,"fork":bool|null, "default_branch":str|null}}`; anything else refused (v1: config error, "remove it").
- S35. `dropped.log` and `ignored.log` (`inbox.py:622-684`): appended `O_APPEND|O_NOFOLLOW`,
  0600, one line `team\tevent_id\tsender\treason\tms\n`; an event ID or sender that fails its
  grammar is written `-`; the reason is a drop reason (`protocol.py:192-209`) or, for
  `ignored.log`, `not-addressed`. The count is the number of `\n` bytes.

## 8. Session and home files

- S36. Desired membership (`membership.py:56-57,213-241`, host-written): at most 65536 bytes,
  duplicate keys refused; exactly `v` (int 1), `session` (`[A-Za-z0-9_][A-Za-z0-9_.@+-]{0,254}`),
  `kind` (`ccy`|`cc`), `checkout` (`/` then 0-4095 characters with no C0 control or DEL),
  `generation` (int >= 1), `written_ms` (int >= 0), `seats` (at most 64 `{team, seat}`, one per
  team), `farewell` (at most 256 `{team, seat, reason: "removed"}`). Absence is refused.
- S37. Applied membership (`membership.py:58-59,154-166,244-284`): exactly `v`, `generation`
  (int >= 0), `seats` (at most 64 `{team, seat, handle, state: "held"}`), `refused` (at most 64
  `{team, seat, why, holder}`, `why` one of `held-elsewhere no-bundle bundle-invalid not-desired`,
  `holder` `-` or `[a-z-]{1,32}`), optional `told` (`{generation >= 0, roles: [{team, handle, role}]}`) and `inbound` (bool). Written sorted (S17) in the home and in the report directory.
  Active teams are the desired seats, in desired order, whose `(team, seat)` is held in the
  applied file; the applied file alone never grants a team (`membership.py:484-490`).
- S38. `told.json` (`membership.py:62-64,287-334`): exactly `v`, `generation` (>= 0), `teams`
  (at most 64 `{team, seat, handle, role}`, role `worker`, `coordinator` or `-`), `left` (at most
  256 `{team, reason: "removed"}`). `told-roles.json` (`membership.py:71-72,416-437`): exactly
  `{v, roles: {team: worker|coordinator}}`, at most 64.
- S39. `conversation.json` (`hooks.py:213-239`): `{"v":1,"session","session_id","source", "written_ms"}`, written sorted (S17); `session_id` `[A-Za-z0-9_-]{1,128}` (finding F8),
  `source` one of `startup resume clear compact other`.
- S40. `relaunch.json` (reader only; `desired.go:112-188`): `{"v":1,"generation":int, "written_ms":int}` with optional `"why":"restart"`. Its presence is the request.
- S41. `socket.json` (`notify.py:52-90`, R43): `{"v": 1, "socket": P, "token": T}` (spaced, S17),
  mode 0600; a reader requires a regular file owned by this uid with no group or other bit, at
  most 8192 bytes of ASCII, `P` absolute, `T` non-empty. v1 returns "no record" for any failure;
  v2 returns the typed error (SB5).
- S42. `goodbye.json` (reader; dropped at stage 2, B7; `hooks.py:1090-1109`): `{"v":1,"session":S}`.
- S43. `stop-guard.json` (`hooks.py:660-723`): exactly `{v:1, pending: null|64-character string, no_waker_ms: null|int>=0, failure_ms: null|int>=0}`; shared by v1 and v2 hooks of one
  `PINGBUS_HOME`.
- S44. `active` is an empty 0600 file present exactly while the keeper holds a seat
  (`seat.py:716-721`). `down/<team>` is an empty 0600 file in a 0700 directory whose mtime is
  refreshed while the team's homeserver does not answer, honoured for 300 s
  (`statusline.py:84-88,276-299`). `inject/<key>.stalled` and `.missing` are empty marks;
  `.transcript` an absolute path on one line, at most 4096 characters, no NUL, CR or LF;
  `.spawned` decimal milliseconds, at most 16 digits (`inject.py:60-145`).
- S45. The session record (`hooks.py:242-262`, `statusline.py` `session_teams`): the session's
  teams joined by `,`, no newline, 0600, in a directory of this user's alone (0700, owned); a
  reader requires distinct team names.

## 9. Locks (DESIGN section 7, R47)

Every lock is `flock(2)` on a file description the acquisition opened itself (L11); none is a PID.

- L1. Path lock (sync `state/lock` kinds `watch`/`wait`/`recv`; `seat.lock` kind `seat` with the
  claim; `keeper.lock` kind `keeper`; `inject/<key>.watch` kind `watch`), `inbox.py:932-977`:
  open `O_RDWR|O_CREAT|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC` 0600, refuse a non-regular file; try
  `LOCK_EX|LOCK_NB` three times with 50 ms between tries (the sleep is injected, so tests need no
  clock); on the third failure read the holder's kind (the first word of the first 64 bytes;
  `unknown` for any other word), close, and return `Busy` naming it. On success keep the previous
  content (what a dead holder left), truncate, and write the kind (and ` <claim_ms>`) at offset 0.
- L2. Release: truncate to empty, `LOCK_UN`, close (`inbox.py:912-923`): the explicit unlock
  frees every duplicate of the descriptor. A holder that dies leaves its content (the claim).
- L3. Probe (`inbox.py:1024-1039`): open read-only without creating; absent is "free"; a failing
  `LOCK_SH|LOCK_NB` is "held" with the kind read; else unlock, "free". A probe holds a shared
  lock for an instant, which is why L1 tries three times.
- L4. The sync lock is taken only with kind `watch`, `wait` or `recv`, after `ensure_dirs`
  (`inbox.py:1002-1008`). A waker is a `watch` or `wait` holder (`inbox.py:1011-1014`).
- L5. A lock is close-on-exec until the caller hands its file to a child (`seat exec`,
  `inbox.py:907-910`).
- L6. Seat disconnect (`cli.py:2950-2962`) reads the claim (L4 of S23), takes the seat lock and
  compares the claim in `previous`: the API exposes `previous`.
- L7. Outbox lock (`inbox.py:688-704`): `ensure_dirs`, open the `state/` directory
  `O_RDONLY|O_DIRECTORY|O_CLOEXEC`, `LOCK_EX` blocking, held for the whole read-modify-write;
  closing the descriptor releases it.
- L8. Delivery lock `relay/lock` (`relay.py` `Spool.exclusive`): make `relay/` 0700, open
  `O_RDWR|O_CREAT|O_NOFOLLOW|O_CLOEXEC` 0600, `LOCK_EX` blocking (or non-blocking on request);
  nothing is written into it; close releases. Held by the deliverer around check, socket write and
  consume, and by `recv`/`wait` around print and consume (`cli.py:1421,1466-1471`); never across a
  homeserver call.
- L9. Told lock `told.lock` (`membership.py:440-457`): open `O_RDWR|O_CREAT|O_NOFOLLOW|O_CLOEXEC`
  0600, one `LOCK_EX|LOCK_NB` try; failing is "another teller has it" (not an error); release is
  `LOCK_UN` then close.
- L10. Order: the delivery lock before the outbox lock, never the reverse
  (`cli.py:1421`, `RESEARCH-pingbus.md` section 4). The package offers the pair as one call.
- L11. Each acquisition opens its own file description, so two goroutines of one process exclude
  each other exactly as two processes do.
- L12. Proof: each of L1, L2, L3, L7, L8, L9 and L11 is proven against a second real process (the
  test binary re-run as a child), and L1, L7, L8 and L9 against a real v1 Python process
  (`helpers/pingbus` from this checkout), in both directions: v2 holds and v1 is refused (or
  blocks), v1 holds and v2 is refused (or blocks).

## 10. Grammars borrowed until `bus/proto` lands

The grammars are `bus/proto`'s (built on a sibling branch). `bus/state` keeps minimal unexported
validators, each marked `TODO(bus/proto)`, for: team name, seat name, agent handle, user ID
(`parse_user_id`), human localpart, room ID, event ID, description, ack-expecting verb set, drop
and unheard reasons, roles. The report lists each for the integrator.

## 11. The corpus

`tests/fixtures/agent-bus-state/<kind>/{valid,invalid}/<file>` and `<kind>/cases.json`:

```json
{"kind": "sync", "cases": [
  {"file": "valid/token.json", "expect": "accept", "roundtrip": true},
  {"file": "invalid/v-true.json", "expect": "type", "v1": "accept", "departure": "SB1"}
]}
```

- `expect` is `accept` or a class of section 2. `v1` (default: `accept` for `accept`, `refuse`
  otherwise) is what v1 does: `accept`, `refuse` or `crash` (an uncaught exception, finding F4).
  A difference between `expect` and `v1` must name its departure.
- `roundtrip`: the valid file is exactly the bytes the writer writes for the decoded value; the
  Go test re-encodes and compares, and the Python test rewrites through v1's writer and compares.
- The Python walk (`tests/helpers/pingbus/test_state_corpus.py`) runs each file through v1's own
  reader for its kind and checks `v1`; the Go walk checks `expect`, the class included. Each kind
  has its v1 reader named in the Python test, so a kind with no adapter fails the walk.

## 12. Departures and findings

- SB1. Duplicate keys are refused in every state file. v1's `inbox.read_json_file` uses a plain
  `json.loads` (last value wins) for `state/` files, `said-goodbye.json`, `goodbye.json` and
  `stop-guard.json`; v1 never writes one, so refusing it loses nothing.
- SB2. Three classes beyond DESIGN 5.4's six (section 2).
- SB3. `said-goodbye.json` is decoded strictly; v1's `goodbye_said` reads anything else as "not
  said" (F7).
- SB4. `outbox.json`'s `gate` is checked at decode; v1 checks it only when a send uses it, so v1's
  lock-free `status` reads an outbox with a malformed gate. v1 never writes one.
- SB5. `socket.json`: a typed error, not "no record"; the caller still treats it as no record.
- SB6. `v: true` (and `1.0`) is refused as `type`; v1's `data["v"] != 1` accepts both in every
  `inbox`-style file (`inbox.py:433,549,570,606,726,743,770`, `notify.py:85`, `hooks.py:697`).
- SB7. Every write fsyncs the file and the directory and uses v1's temporary name; v1's
  `socket.json`, `forge-cache.json` and session-record writers fsync less and name their
  temporaries otherwise. No reader lists those directories.
- SB8. The mode is set exactly with `fchmod`; v1's membership writer leaves the mode to the
  umask under 0600.

Findings in existing code (reported, not fixed here):

- F1. `internal/pingbus/statusline/member.go` `limitBounds` lacks `report_per_hour`.
- F4. `membership.py:237,331`: `entry["reason"] not in FAREWELL_REASONS` raises `TypeError` for a
  list or object reason (a dict lookup of an unhashable value): v1 crashes instead of refusing.
- F5. `fsx.WriteAtomic`'s temporary would be read by v1's inbox listing as a rejected item (S15).
- F6. `internal/teams/membership.Read` and `ReadReport` use `encoding/json`: keys match
  case-insensitively and duplicates are accepted, unlike pingbus's strict reading.
- F7. `seat.py:271-276` (SB3).
- F8. `hooks.py` binds `SESSION_ID_RE` twice (`{1,64}` at line 87, `{1,128}` near line 211); the
  later binding is the one every call sees.
