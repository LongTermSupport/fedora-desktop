# SPEC: `teamchat/matrix` and `teamchat/matrix/fakehs` (Plan 00209 milestone M1, Task 1.5)

The packages are `src-go/internal/teamchat/matrix` and its `fakehs`, under D 3.3's package
root. They were first built as `bus/matrix`; the expectations are unchanged by the move.

The Matrix client-server client both sides of the v2 bus use (DESIGN.md 3.2, 9), and the
in-memory homeserver their tests run against (DESIGN.md 11). Each expectation below is
numbered, cites the v1 code or the Matrix specification it comes from, and has a Go test
written first and seen to fail. Departures from v1 are numbered MB1... in section 9.

v1 sources: `helpers/pingbus/matrix.py` (the member client), `helpers/pingbus/syncer.py` (the
filter and the sync answer rules), `helpers/pingbus/limits.py` (429 bounds),
`helpers/pingbus/config.py` (`check_base_url`), `helpers/agent_bus/admin.py` (the admin
transport and endpoints), the v1 fakes `tests/helpers/pingbus/fake_client_api.py` and
`fake_admin_api.py`, and probe H4's recorded exchanges with a real Tuwunel,
`tests/helpers/pingbus/fixtures/tuwunel/` (73 fixtures). Plan 00211's SPEC.md 4-8 names what
the side rooms need.

`teamchat/matrix` moves JSON and maps failures. It validates nothing an event carries: events,
state content and invite state are handed on as raw JSON (`json.RawMessage`) for `teamchat/proto`
to validate. It checks only what the transport needs (a path segment is safe to send, an answer
is the shape the endpoint promises).

## 1. The transport

- **M1.** One transport serves both sides: an `http.Client` whose `Transport` has no proxy
  (`Proxy: nil`, so `HTTP_PROXY`/`HTTPS_PROXY` are ignored) and whose `CheckRedirect` follows
  nothing (`matrix.py:95-105`, `admin.py:264-271`).
- **M2.** A 3xx answer is an error (`redirect refused`), never followed, to any origin; the
  redirect target receives no request at all, so the token cannot reach it (`matrix.py:217-218`;
  protocol §12).
- **M3.** The token travels only in `Authorization: Bearer <token>` on the request it was given
  to (`matrix.py:193-194`). It never appears in a URL, a query, an error, `String()`, `%v` or
  `%#v` of the client (protocol §12).
- **M4.** A token is one line of printable ASCII (`[\x21-\x7e]+`), at most 4096 bytes; anything
  else is refused when the client is built (`matrix.py:165-166`, `config.py:39,378-380`).
- **M5.** Every request carries `Accept: application/json` and a `User-Agent` (`teamchat` on the
  member side, `teamchat-admin` on the admin side, v2's names, where v1 sent `pingbus` and
  `agent-bus`), and `Content-Type: application/json` when it has
  a body (`matrix.py:189-192`). Compression is not requested (`DisableCompression`), so the body
  cap counts the bytes the server sent.
- **M6.** Each request has a time limit: 15 s on the member side (`matrix.py:32`), 30 s on the
  admin side (`admin.py:60`), and a sync's limit is its long-poll plus 15 s (`matrix.py:256-257`).
  A request past its limit is `Unreachable` and transient, with the detail `timed out`.
- **M7.** An answer body is read to at most 32 MiB; one longer is `Unreachable`
  (`answer over 33554432 bytes`), not transient (`matrix.py:33,133-140`). Response headers are
  capped at 64 KiB.
- **M8.** A 200 answer must be one JSON object (the room-state read: one JSON array of
  objects); anything else is `Unreachable` (`the answer is not a JSON object`), not transient
  (`matrix.py:226-231`, `admin.py:343-348`). Any other 2xx is a refusal (`matrix.py:226-227`).
- **M9.** A failure is a `*matrix.Error` carrying the method, the path (percent-encoded, never
  the query), the HTTP status, the errcode when it matches `M_[A-Z0-9_]{1,64}`, and a fixed
  detail this package wrote. It never carries the token, the server's `error` text, the query,
  or the text of a lower-level error (`matrix.py:12-14,46-63,151-153`).
- **M10.** The status classes (`matrix.py:63-93,156`): 401 is `ErrAuthRefused` (exit 8), 403
  `ErrForbidden` (exit 10), 404 `ErrNotFound` (exit 7), a 429 past its retries
  `ErrRateLimited` (exit 9, transient), no answer `ErrUnreachable` (exit 7, transient unless the
  answer was too long or not JSON), a 5xx a refusal that is transient, any other status a
  refusal (exit 7). `errors.Is(err, matrix.ErrNotFound)` and its siblings test the class;
  `(*Error).ExitCode()` gives the exit code.
- **M11.** A request whose caller's context is cancelled stops at once; its error is
  `Unreachable` and `errors.Is(err, context.Canceled)` (or `DeadlineExceeded`) holds. No retry
  or backoff runs after a cancellation.
- **M12.** Path segments are percent-encoded whole, every byte but `[A-Za-z0-9_.~-]`
  (`matrix.py:108-110`, Python's `quote(s, safe="")`), so `@a:b` is `%40a%3Ab` on the wire as v1
  sends it. A segment is refused before any request when it is not printable ASCII or is longer
  than 255 bytes; only a state key may be empty (MB2).
- **M13.** `Close()` releases the idle connections; a client that has finished and been closed
  leaves no goroutine behind.

## 2. Base URLs

- **M14.** The member side takes the bundle's `base_url` under v1's rule (`config.py:577-605`):
  printable ASCII, no query or fragment, scheme `http` or `https`, no credentials, no path (a
  trailing `/` is a path), a valid port when one is given, a host; `http://` only to an IP
  literal (canonical, no zone) listed in `plain_http_hosts`; `https://` to an IP literal or a
  lower-cased DNS name of at most 253 bytes. The host is compared lower-cased, as Python's
  `urlsplit` gives it.
- **M15.** The admin side takes only `http://127.0.0.1:<port>`, the port written canonically
  (`080` is refused) and from 1 to 65535, nothing after it (`admin.py:247-261`; MB3 for port 0).

## 3. Rate limits (429)

- **M16.** A 429 is retried: at most 3 tries in all; the wait before the next try is
  `Retry-After` in whole seconds (ASCII digits, surrounding space allowed), else the body's
  `retry_after_ms` (a non-negative integer), else 5 s (`limits.py:52-57,120-134`,
  `matrix.py:211-216`). A wait over 60 s is not slept: the 429 is `ErrRateLimited` at once
  (`matrix.py:213`); a wait of exactly 60 s is slept.
- **M17.** The wait goes through an injected `Clock` (`Sleep(ctx, d) error`), so tests never
  sleep and never read the wall clock; a cancelled context ends a wait at once (M11). The
  admin side follows the same rule (MB4).

## 4. Member endpoints

- **M18.** `Versions` (`GET /_matrix/client/versions`) asks without the token (`matrix.py:236-239`).
- **M19.** `Whoami` (`GET /account/whoami`) gives the user ID and device ID (`admin.py:353-358`).
- **M20.** `Sync`: `GET /sync` with `timeout` (milliseconds, 0 to 30000; anything else is
  refused before a request), `since` when given, and the inline `filter` as compact JSON with
  sorted keys (`matrix.py:244-257`). Its answer gives `next_batch`, which must be printable
  ASCII of at most 4096 bytes (`syncer.py:249-253`, `inbox.py:99`), and per room `join` (state
  events, timeline events, `limited`, `prev_batch`) and `invite` (stripped `invite_state`
  events). A limited timeline whose `prev_batch` is not a printable token is malformed
  (`syncer.py:694-695`). A malformed answer (a section that is not an object, events that are
  not a list, `limited` that is not a boolean: MB5) is `Unreachable`.
- **M21.** `RoomFilter` renders v1's filter shape exactly (`syncer.py:156-174`): presence and
  account data empty, `room.rooms` the rooms named, ephemeral and room account data empty,
  `state` with its types and `lazy_load_members`, `timeline` with its types and `limit`.
- **M22.** `Send` is `PUT /rooms/{r}/send/{type}/{txn}`; the transaction ID is
  `[A-Za-z0-9._~-]{1,64}`, made as `pb` and 32 hex digits from `crypto/rand` when none is given
  (`matrix.py:266-273`). An `Unreachable` answer (the answer was lost) is retried with the same
  transaction ID, 3 tries in all, 1 s apart through the `Clock`, so the server stores it once
  (`matrix.py:274-283`; protocol §9 send step 7). Any other failure is not retried.
- **M23.** The send and state-put answers must hold an `event_id` that looks like one (`$`, then
  printable ASCII, at most 255 bytes); otherwise `Unreachable` (`matrix.py:323-328`; MB1 for the
  room-version-12 grammar).
- **M24.** `PutState`, `GetState` (the content) and `GetStateEvent` (`format=event`, the whole
  event with its sender) (`matrix.py:289-299`); `RoomState` (`GET /rooms/{r}/state`, an array,
  `admin.py:341-348`); `GetEvent` (`matrix.py:306-307`); `Join`, `Leave` (`matrix.py:259-264`);
  `ReadReceipt` (`POST /receipt/m.read/{event}` with `{}`, `matrix.py:301-304`).
- **M25.** `Messages` (`GET /rooms/{r}/messages`) takes `from`, optional `to`, `dir` `f` or `b`, a
  positive `limit` and optional event types as `{"types":[...]}` (`matrix.py:309-321`,
  `syncer.py:66`). Its answer must hold a `chunk` list (`syncer.py:701-703`); `start` and `end`
  are handed on, `end` empty when the server sent none.
- **M26.** Content and bodies the caller passes as raw JSON must be one JSON object; anything
  else is refused before a request.

## 5. Admin endpoints (`admin.py`, Plan 00211 SPEC 5.1)

- **M27.** `RegisterNonce` and `Register` (`GET`/`POST /_synapse/admin/v1/register`, no token);
  `RegistrationMAC` is Synapse's HMAC-SHA1 over `nonce\0user\0password\0admin|notadmin`
  (`admin.py:429-447`).
- **M28.** `Login` (`POST /login`, `m.login.password` with an `m.id.user` identifier and a
  device display name, `admin.py:450-454`) gives the user ID, token and device ID.
- **M29.** `Account` (`GET /_synapse/admin/v2/users/{id}`): `found=false` for a 404
  (`admin.py:360-362`). `CreateAccount` (`PUT` the same path with the password and display
  name and **no `admin` key**, `admin.py:364-369`); the answer must be the account named, not an
  admin. `ResetPassword` (`PUT` with `logout_devices: true`, the only revoke Tuwunel has,
  `admin.py:371-374`). `SetDeactivated` and `SetLocked` (`admin.py:405-406`).
- **M30.** `MintToken` (`POST /_synapse/admin/v1/users/{id}/login` with `{}`): the answer's
  `access_token` must be a printable token (`admin.py:376-382`).
- **M31.** `ServerAdmins` (`GET /_synapse/admin/v2/users?admins=true`): the names; a `users`
  that is not a list, or a `next_token` that is not null, is malformed (`admin.py:505-513`).
- **M32.** `Devices` (`GET /_synapse/admin/v2/users/{id}/devices`): each device's ID and last-seen
  time (absent or null is none) (`admin.py:904-920`; grammar checks stay the host side's, `teamchat-admin`).
- **M33.** `Membership` reads `m.room.member` state for a user: its `membership`, or empty when
  there is no such state (404) (`admin.py:392-395`). `Invite`, `Kick` with a reason
  (`admin.py:397-403`).
- **M34.** `CreateRoom` (`POST /createRoom`) takes preset, room version, visibility, name, topic,
  a power-levels override, initial state (type, state key, content) and invites; the answer's
  `room_id` must look like one (`!`, printable ASCII, at most 255 bytes) (`admin.py:570-578`; the
  side rooms of Plan 00211 SPEC 3.1 and 5.1 add only initial state, a join rule among it).
- **M35.** `JoinedRooms` (`GET /joined_rooms`): the room IDs, for 00211's adoption after a crash
  between `createRoom` and the ID file (Plan 00211 SPEC 5.1 step 1).
- **M36.** Threads and replies (Plan 00211 SPEC 8.3) need no endpoint: `m.relates_to` is part of
  the content `Send` carries, and the question index is read with `Messages`, as v1 does
  (`syncer.py:549-572`). No `/relations` call is made (MB6).

## 6. `fakehs`: the in-memory homeserver

- **M37.** `fakehs.Server` models what both sides call, port of the v1 fakes: password login,
  whoami, `createRoom` (version 12, `private_chat`), invite, join (including a v12 `restricted`
  join rule's `m.room_membership` allow), leave, kick, send with transaction IDs per device,
  state get and put with `format=event`, the whole room state, redact, `GET /event`, read
  receipts, `/messages`, `/joined_rooms`, `/sync` with an inline filter, `timeout`
  long-polling and lazy-loaded members, and the admin API: shared-secret registration with a
  single-use nonce, `GET`/`PUT v2/users/{id}`, `?admins=true`, the token mint, `v1/reset_password`
  and `devices`. Room-version-12 auth rules as v1's fake models them: a creator's power is
  infinite, power per event type, a state key starting `@` only by that user, invite and kick
  power (`fake_client_api.py:425-449`).
- **M38.** Its answers match Tuwunel's: replaying every recorded H4 exchange, in order, against a
  fresh server gives the same status, the same `error` text and the same answer once the values
  a server makes up (tokens, IDs, timestamps, batch tokens) are set aside, with v1's one documented
  divergence (058) (`test_fakes.py:42-192`). The replay reads the fixtures where v1 keeps them,
  so the two fakes are proven against one corpus. The replay's three v1 names (the corpus path,
  the probe's server name, v1's status type) are deliberate references, each owed a line in D
  4.1's `allowed.tsv` when that gate lands (Task 1.2); nothing else under `teamchat/` names v1.
- **M39.** A request beyond the model is answered 500 `M_UNKNOWN` `fake: unmodelled: ...` and
  recorded; `CheckModelled()` reports the first, so a test that strays fails
  (`fake_client_api.py:55-56,690-692`). An unknown route is 404 `M_UNRECOGNIZED`.
- **M40.** Tuwunel's quirks are switches (`Quirks`, all on in `TuwunelQuirks()`, DESIGN.md 9):
  `AdminFalseIs500` (an `admin: false` for a non-admin account answers 500, H4 fixture 008);
  `MaxRequestBytes` (a body over it is 413 `M_TOO_LARGE`; Tuwunel's floor is 10 000 000,
  `render.py:50-51`); `LoginFailuresAllowed` (failed password logins after that many are 429 with
  `retry_after_ms`, kept in memory and emptied by a restart, `health.py:22-25`). Password reset
  with `logout_devices` (true unless set false) is the only revoke of the whole account the
  admin flow uses: no `logout/all`. A member's own device delete is M52 and M53. The quirks that are not HTTP (bare config keys, the PID-1
  signal drop, SIGUSR2 backup) belong to the installer's fakes, not this one.
- **M41.** Fault injection: `Inject` queues a canned answer (status, body, headers such as
  `Retry-After`) for the next matching requests; a fault may instead drop the connection before
  handling, drop it after handling (the effect lands, the answer is lost: a lost send), or answer a
  redirect. `Stop` refuses connections; `Restart` drops every open connection (a held sync ends
  mid-poll), keeps the state, empties the login buckets and serves again at the same address. A
  limited timeline comes from more events than the filter's `limit`, as on a real server.
- **M42.** Every HTTP request is recorded (method, path as on the wire, query, headers), so a test
  can assert the `Authorization` header, the absence of a query token, or that nothing reached a
  redirect target. Recording never holds a request body.
- **M43.** A held sync wakes when an event lands, ends empty at its timeout, and ends when the
  client's connection closes; the server's clock for timestamps is injected (`Options.Now`).

## 7. The real homeserver

- **M44.** `go test -tags tuwunel` runs the client against a real Tuwunel binary when
  `FAKEHS_TUWUNEL_BIN` names one: registration, the admin flow, a room, a send with a reused
  transaction ID, sync, messages and a receipt, and a pool seat's takes and releases (M45-M47,
  M52, as host check H-C13 recorded them on Tuwunel 1.9.4): a second login as the fixed device
  leaves the first token valid, a logout ends both with `M_UNKNOWN_TOKEN` and a second logout
  answers the same, a wrong password is `ErrForbidden` for a login and for a device delete, a
  device delete by password from another device ends the device's token and not its own, a
  token deleting its own device ends itself (C 3.7.3's fallback), an admin password reset ends
  a fresh token, and the `teamchat.floor` marker, decoded, outlives them all. Presence is not in
  the tagged test (the client reads none; HC-A1 is Task 1.4's probe). Without the variable the test skips with its
  reason; without the tag it is not built.

## 7a. Seat sessions and the resume marker (Task 1.5; B 2.3.2, B 2.6 step 01 (b), C 3.7.3)

- **M45.** `LoginDevice` (`POST /login`, `m.login.password` with an `m.id.user` identifier, the
  given `device_id`, no display name and no `refresh_token`) signs in as that device: C 3.7.3
  take step 3, with the seat's fixed device ID. A device ID that is not 1 to 255 bytes of printable
  ASCII without a space is refused before a request. The answer must name the device asked for,
  or it is malformed. The device's earlier tokens stay valid: host check H-C13 found that
  Tuwunel 1.9.4 does not end them, where the Matrix specification would, so a take ends the
  previous holder's token by `Logout` or `DeleteDevice` (M52), not by its login. A login whose
  answer was lost is not retried by the client.
- **M46.** `Logout` (`POST /logout` with `{}`) ends every token of the client's device, not only
  its own, and the device (C 3.7.3 release step 4; H-C13 recorded it). A token already dead is `ErrAuthRefused` with `Errcode` `M_UNKNOWN_TOKEN`, which the caller
  counts as done; the client does not decide that. Room account data outlives a logout.
- **M47.** `RoomAccountData` (`GET /user/{u}/rooms/{r}/account_data/{type}`) is the content as raw
  JSON, `found=false` for a 404 whose errcode is `M_NOT_FOUND`; any other 404 (`M_UNRECOGNIZED`,
  an endpoint not served) is `ErrNotFound`, so a server fault is not read as no marker. The answer must be one JSON object, and decoding it strictly is
  the caller's (B 2.6 step 01 (b)). `PutRoomAccountData` (`PUT` the same path) takes content that
  must be one JSON object, refused before a request otherwise (M26). This is the `teamchat.floor`
  resume marker of B 2.3.2.
- **M48.** `fakehs` models them as H-C13 recorded Tuwunel. A login with a `device_id` makes that
  device, or, when the account has it already, gives it one more token and leaves the earlier
  ones valid; a `device_id` that is not a string is unmodelled. `POST /logout` ends every token
  of the caller's device and deletes the device. Room account data is
  kept per account, room and type: a read of none is 404 `M_NOT_FOUND`, a write by another
  account, to a room the server does not know, or of content that is not an object is
  unmodelled (H4 recorded none of these, and none of the 404 text), and the data outlives a
  logout, a password reset with device logout and `Restart` (B 6 C13).
- **M49.** `Redact` (`PUT /rooms/{r}/redact/{event}/{txn}` with the reason when one is given)
  gives the redaction's event ID; the transaction ID is made and checked as `Send` does (M22),
  but a lost answer is not retried: whether Tuwunel stores a replayed redact once is not recorded,
  so a lost answer is a transient `ErrUnreachable` and the caller reads whether the event is
  redacted. It is the admin's one-event redact of C DC20; which reason is allowed is the caller's.
- **M50.** Presence (A 6.6, B 2.6 step 09): `SyncRequest.SetPresence` is sent as `set_presence`
  when it is not empty, and must be `online`, `offline` or `unavailable`; `SetPresence`
  (`PUT /presence/{u}/status` with `{"presence": p}`) takes the same three. Anything else is
  refused before a request.
- **M51.** `fakehs` keeps a redaction's reason in its content and marks the redacted event with
  `unsigned.redacted_because` (the specification's shape, which A 7.6 rule 02 drops on; H4
  recorded no redacted event), and keeps each account's presence:
  `PUT /presence/{u}/status` by its own account sets it, `Presence(u)` reads it for a test, and a
  sync's `set_presence` acts as host check HC-A1 recorded Tuwunel 1.9.4 for an absent one
  (online) and `offline` (it sets offline, where the specification has it change nothing), and
  as the specification says for `online` and `unavailable` (each sets itself). Another account's presence, or a value beyond the three, is unmodelled. Transaction
  IDs are kept per endpoint, and a redact under a transaction ID used before is unmodelled.
  Receiving presence (the filter's presence section, presence events in a sync, for `status`,
  A 6.6) is not built: it is owed to N3, with HC-A1.
- **M52.** `DeleteDevice` (`DELETE /devices/{d}`) deletes one of the account's devices, which ends
  every token of it: C 3.7.3's take, with the H-C13 fallback (a second login does not end the
  previous holder's token). The device ID is checked as M45 checks it. The first request (body
  `{}`) is answered with a user-interactive auth challenge: a 401 with no errcode, a `session`
  (M4's token grammar) and a flow whose only stage is `m.login.password`, else it is malformed
  (no session, not an object) or `ErrRefused` (no such flow). The second request answers it,
  `{"auth": {"type": "m.login.password", "identifier": {"type": "m.id.user", "user": u},
  "password": p, "session": s}}`, and must be answered 200 with an object. A wrong password (401
  `M_FORBIDDEN`) is `ErrForbidden`; any other 401 is `ErrAuthRefused` (a dead token answers
  `M_UNKNOWN_TOKEN` at the first request); a first answer that is not a 401, or a 401 with an
  errcode, is judged as M10 says, so a server that asks no auth is done in one request. Neither
  request is retried after a lost answer.
- **M53.** `fakehs` models M52 as H-C13 recorded it (the statuses; the bodies are the
  specification's shape): the first request is a 401 with a fresh session, the one-stage
  password flow, `params` and `completed`; the answer with the caller's own password and that
  session deletes the device and ends every token of it, the deleting token living on unless it
  is the device's own. A wrong password is a 401 `M_FORBIDDEN` with the same session: the
  specification's shape and Synapse's text, which H-C13 did not record and the tagged test (M44)
  checks on the host. An unknown device, a session not given to the caller for that device,
  another account named, or an auth type or shape beyond that is unmodelled. A sync whose `since` is ahead of the server (a token from before a
  restore) is held to its timeout and answered with the server's own lower `next_batch`, and is
  not given what lands at or below that `since`, as host check H-C14 recorded.

A dropped `/sync` may be resent by net/http (once, a GET on a reused keep-alive connection closed
before any answer byte). That is safe, because a sync with a `since` token is idempotent: the
resent request is answered from the same point, and nothing is skipped or delivered twice.

## 8. What the callers own

`teamchat/proto` (A): every grammar (room and event IDs in their room-version-12 form, user IDs,
event content), so the receive and send rules are applied there. `teamchat/member` (B): the sync
engine (which types, first sync, post-join, gap fill loops, trust re-verification). The host side,
`teamchat-admin` (C): the bootstrap
and member flows, device-ID grammar, what a non-200 means for each step.

## 9. Departures from v1

- **MB1.** Room and event IDs are checked here only as safe path segments and as `!`/`$`-led
  printable text, not by protocol's room-version-12 grammar (`![A-Za-z0-9_-]{43}`): that grammar
  is `teamchat/proto`'s, and the callers validate an ID before it is used. v1 refused a non-v12 ID in
  the client (`matrix.py:117-126`).
- **MB2.** A segment that is not printable ASCII, or over 255 bytes, is refused before a request
  for every segment, not only room and event IDs.
- **MB3.** The admin base URL refuses port 0, which v1's check accepted and which cannot be
  connected to.
- **MB4.** Admin calls get the 429 retry of M16 (v1's admin transport had none), and their errors
  follow M9: the server's `error` text, which `admin.py:318-323` printed cut to 200 characters,
  is no longer in any message, and a non-`M_` errcode is dropped.
- **MB5.** A sync answer whose `limited` is present but not a boolean, or whose `end` in
  `/messages` is not a string, is malformed (exit 7); v1 read the first as not limited (losing the
  gap) and the second as the last page.
- **MB6.** No `/relations` endpoint: nothing in 00211 needs one.
- **MB7.** `next_batch` and a limited timeline's `prev_batch` are checked by the client, where v1's
  syncer checked them; the rule is the same.
- **MB8.** One general client: `Admin` embeds `Client`, so the host has every client-server call
  (`Login`, `CreateRoom`, `Invite`, `Kick`, `Membership`, `JoinedRooms` are `Client` methods), and
  `Register` asks its single-use nonce itself (no exported `RegisterNonce`).
- **MB9.** The User-Agent defaults per side as M5 says and `Config.UserAgent` replaces it, as v2
  names its own binaries.
- **MB10.** `Send` retries only a transient `Unreachable` (no answer, timed out, cut short); an
  over-long or non-JSON answer and a cancellation are not retried.
- **MB11.** A nil list in `RoomFilter` is not sent (every room or type), an empty one asks for
  none; `/messages` `from` is optional. A sync's `leave` section is read like `join`.
- **MB12.** The token is a `Token` that formats as a placeholder whatever the verb and refuses
  `MarshalText`; the client keeps it only inside a closure, so no formatting of the struct reaches
  it. A call refused before any request is `ErrInvalid` (exit 7).
- **MB13.** A member base URL's port is canonical (no leading zero) and its IP literal canonical,
  as the admin rule; `Membership` refuses an empty user ID (a member state key is never empty).
