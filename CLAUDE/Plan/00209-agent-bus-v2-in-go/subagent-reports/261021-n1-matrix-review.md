FIX-FIRST

# Plan 00209 N1 Task 1.5: adversarial review of feat/bus-v2-n1-matrix (d7a439de..HEAD)

Commits reviewed: 5d5bb7be (move), 3c34cde6 (M45-M48), ffbb65a0 (M49-M51), 0052169b
(redacted_because). The move is clean: the files are byte-identical apart from import paths and
the package-doc text, the golangci path rules follow the new paths, no tracked file is left under
`internal/bus/` (an empty untracked `src-go/internal/bus/` directory remains in the worktree; git
does not track it). The take/release semantics of C 3.7.3 steps 3 and 4 are what the client gives:
`LoginDevice` sends `device_id`, no display name, no refresh token, and needs the answer to name
the device; `Logout` leaves `M_UNKNOWN_TOKEN` to the caller; a password refused is `ErrForbidden`.
No token, password or server text reaches an error. No `//nolint`, no discarded error, no wall
clock in the new tests.

## Findings

1. **major: the move puts v1 names, one of them on the wire, inside D 4.1's gate scope.**
   `src-go/internal/teamchat/matrix/client.go:49-50` (`memberAgent = "pingbus"`,
   `adminAgent = "agent-bus"`, sent as every request's `User-Agent`, SPEC M5 line 37 and MB9
   unchanged by the rename), and in the tests: `admin_test.go:62`, `client_test.go:236`,
   `fakehs/fakehs_test.go:11,13,42,67,150-155,243` (`team-a.agent-bus.internal`,
   `agent_bus.status`, `agent_bus.team`), `fakehs/sync_test.go:15`, `fakehs/rooms_test.go:63`,
   `fakehs/rooms.go:331` ("pingbus relies on"), `fakehs/replay_test.go:24,26,45,203`. D 4.1 part 1
   scans every file under `src-go/internal/teamchat/` for `agent-bus`, `agent_bus`, `pingbus`, and
   PLAN Task 1.2 puts that gate in place before the first v2 package merges; this package would
   fail it, and a v2 client announcing itself to the homeserver as `pingbus`/`agent-bus` is the
   v1 identity D keeps v2 off. Fix: default User-Agents `teamchat` and `teamchat-admin` (update M5,
   MB9 and the two tests); rename the test server names and state types to `teamchat.*`; reword the
   rooms.go comment; the replay of v1's recorded Tuwunel fixtures (`tests/helpers/pingbus/...`,
   `probe.agent-bus.internal`, `agent_bus.status`) is a deliberate v1 reference, so give it an
   `allowed.tsv` line when Task 1.2 lands, or note it in the SPEC as owed to that file now.

2. **major: Redact's retry assumes Tuwunel stores a redaction once per transaction ID, which
   nothing has recorded, and the fake encodes the guess.** `member.go:686-700` (Redact via
   `putOnce`), SPEC M49 ("a lost answer retried, as Send does (M22)"), `fakehs/rooms.go:595`
   (redact goes through `txnEvent`), `member_test.go:755-765` (asserts one redaction after a
   dropped answer). M22's once-only storage was recorded for `/send` (H4 and the M44 test's reused
   transaction ID); no fixture and no tagged test covers `/redact`'s transaction ID, and as far as
   I know conduwuit/Tuwunel's redact route builds the PDU without consulting the transaction-ID
   store, so a retried redact after a lost answer would store a second `m.room.redaction`. The
   effect is benign for the target (already redacted) but adds a second admin event to the room
   and to every backup's export, and the test proves only the fake. Also the fake's transaction
   namespace is `(user, device, room, txn)` with no endpoint (`fakehs/fakehs.go:156-158`), so a
   `Send` and a `Redact` with the same ID from one device in one room collide and the redact
   returns the message's event ID without redacting; the Matrix spec scopes a transaction ID to the
   device and the endpoint. Fix: add a redact with a replayed transaction ID to the M44 tagged
   test (the admin redacting one of its sends twice under one ID, counting `m.room.redaction` by
   `/messages`), record the answer in SPEC M49 as unverified until it runs; if Tuwunel does not
   deduplicate, drop the retry (one try; a lost answer is `ErrUnreachable` for the caller, who can
   read whether the target is redacted). Key the fake's transaction map by endpoint as well, with
   a test that a send and a redact sharing an ID are separate.

3. **minor: the tagged seatCycle checks less than H-C13 and M44 claim.**
   `tuwunel_test.go:1264-1276`. (a) The previous holder's and the logged-out token are checked
   only as `errors.Is(err, ErrAuthRefused)`; H-C13 is literally "answers `M_UNKNOWN_TOKEN`", and C
   3.7.3 release step 4 counts `M_UNKNOWN_TOKEN` as done, so check `Error.Errcode`. (b) A second
   `Logout` with the dead token is never sent to Tuwunel, so the errcode release step 4 depends on
   is the fake's only (`seat_test.go:1004-1007`). (c) The marker after release is checked with
   `json.Valid` only, not that it is the marker written (`floor_ms` 1): decode and compare, since
   "outlives both" is the claim. (d) C13 needs room account data to survive the reissue (admin
   password reset with `logout_devices`, C 3.12.2 step 5); the fake tests that
   (`fakehs/session_test.go:504-529`) but the tagged test does not, and `Admin.ResetPassword` is
   right there: add a reset between the release and the last take, logging in with the new
   password. (e) A wrong-password `LoginDevice` answering 403 (take step 3's `M_FORBIDDEN` ->
   `reissue`) is cheap to add too.

4. **minor: `RoomAccountData` reads every 404 as "no marker".** `seat.go:850-856`. A 404 with
   `M_UNRECOGNIZED` (endpoint not served, a proxy in the way) becomes `found=false`, and the member
   resumes from its join (B 2.6 step 01 (b)): the safe direction, but a server fault reported as a
   normal state, against fail fast. Fix: `found=false` only when the errcode is `M_NOT_FOUND`;
   otherwise return the error; add the injected `M_UNRECOGNIZED` case to `TestRoomAccountDataRefusals`.

5. **minor: the receiving half of presence is neither built nor recorded as owed.** A's filter
   table (DESIGN-v2-A-protocol.md:778) wants `presence` for the team's members, for `status`
   (A 6.6); `RoomFilter.JSON` hard-codes `"presence": none` (`sync.go:43`), `SyncResponse` carries
   no presence events, and fakehs neither emits presence in a sync nor marks a default sync online
   (`fakehs/session.go:344-345`). Its test asserts that a sync with `set_presence=online` leaves
   presence unchanged (`fakehs/session_test.go:563-566`), which is the opposite of the Matrix
   spec (`online` and the default mark the user online; only `offline` means no change, which is
   HC-A1's question). Plausibly N3 work, but nothing says so. Fix: name it in SPEC section 8 (or
   7a) as owed to N3 with HC-A1, and make the fake's sync `set_presence` unmodelled rather than
   asserting a no-change the spec contradicts, so an N3 test cannot lean on it.

6. **minor: package docs lag the new expectations.** `client.go:1-3` still cites "SPEC-matrix.md
   M1-M36"; the seat, redact and presence calls are M45-M51. `fakehs.go:2` cites M37-M43 while it
   now models M48 and M51. Fix: extend the ranges.

Item 3 of the brief (net/http's resend of a dropped `/sync`, client.go:14-16 and the SPEC
paragraph after M51): correct. `net/http` retries a replayable GET on a reused connection when no
answer byte was read; a sync with a `since` token is answered from the same point, the lost first
answer was never seen, and the default `set_presence` side effect is idempotent. No finding.

Design gaps checked and not owed here: the H-C13 fallback `DELETE /devices/<id>` with UIA is
conditional on H-C13 failing; the admin device listing exists (M32); presence reading is finding 5.

## What I ran (in the worktree, src-go/)

- `go test -race -shuffle=on -count=1 -cover ./internal/teamchat/...`: pass; `matrix` 98.8%
  (floor 98.7%), `fakehs` 92.6% (was 92.1%).
- `go vet -tags tuwunel ./internal/teamchat/...`: clean.
- `golangci-lint run ./internal/teamchat/...`: 0 issues; with `--build-tags tuwunel`: 0 issues.
- `git merge-base --is-ancestor 35acceab HEAD`: exit 1, as required.
- govulncheck not run; GO-2026-6809 (Go 1.26.9) is known and owned by Task 1.6, not counted here.
- The tagged Tuwunel test cannot run in this container; seatCycle was checked by reading and vet.
