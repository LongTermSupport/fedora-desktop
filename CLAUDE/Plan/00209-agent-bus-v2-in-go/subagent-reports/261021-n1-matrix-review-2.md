FIX-FIRST

# Plan 00209 N1 Task 1.5: adversarial review round 2 of feat/bus-v2-n1-matrix (db3c8350..HEAD)

Tip reviewed: ef75c58a. No major finding. Round 1's six findings are all answered (v1 names out
apart from M38's three replay references; `Redact` makes one try and fakehs keys transaction IDs
per endpoint; the tagged `seatCycle` checks `M_UNKNOWN_TOKEN`, a second logout, the decoded marker,
a reset and wrong passwords; `RoomAccountData` reads only a 404 `M_NOT_FOUND` as no marker;
receiving presence is recorded as owed to N3; the package docs cite M45-M53). Three minor findings
remain, each a fidelity point the brief names, each cheap; then three nits.

Checked and clean:

- **The rebuild.** `git diff feat/bus-v2-m1-matrix d7a439de` over `src-go/internal/bus/matrix`,
  the SPEC and `src-go/.golangci.yml` is empty, so 31ae6433..d7a439de is M1's reviewed package
  byte for byte. `git merge-base --is-ancestor 35acceab HEAD` exits 1.
- **The move.** No tracked file under `src-go/internal/bus/`; imports and both golangci path rules
  (the gosec G401/G505 exclusion, the tagliatelle snake overrides) name `internal/teamchat/matrix`.
  `git grep -i -E 'agent-bus|agent_bus|pingbus'` under `src-go/internal/teamchat` finds exactly the
  three M38 lists (`fakehs/replay_test.go:26,28,205`).
- **The calls.** `LoginDevice` (fixed `device_id`, no display name, no refresh token, answer must
  name the device), `Logout`, `RoomAccountData`/`PutRoomAccountData`, `Redact` (one try),
  `SetPresence`/`SyncRequest.SetPresence` (the three values, refused before a request otherwise).
  `DeleteDevice` (seat.go:95-125): the challenge is taken only from a 401 with no errcode, the
  session must be a token, the flow must be exactly `[m.login.password]`; the second body is the
  password login identifier plus `session`; 401 `M_FORBIDDEN` is `ErrForbidden`; a dead token is
  judged at the first request (`M_UNKNOWN_TOKEN`); a server that asks no auth is done in one
  request; neither request is retried. The `answered()`/`do()` split keeps the auth check and the
  429 loop in one place and `do` behaves as before. No password, token or server text reaches an
  error (`detailPasswordRefused`, `detailNoPasswordFlow` are constants; `cl.malformed()` carries no
  body). Tests cover each branch (TestDeleteDeviceBody, TestDeleteDeviceRefusals,
  TestDeleteDeviceEndsEveryTokenOfTheDevice).
- **fakehs against the host checks.** H-C13: a second login as a device keeps the first token
  (`session.go` signIn), `/logout` ends every token of the device (`removeDevice`), the delete is
  401 without auth and 200 with it, ends the device's tokens and keeps the deleting token unless it
  is the device's own (TestDeletingADeviceByPasswordEndsItsTokens covers both, matching run lines
  106 and 107). HC-A1: `set_presence=offline` sets offline, the default sets online
  (TestPresenceIsKeptPerAccount). H-C14: a since ahead is held and answered with the server's own
  lower `next_batch`, and a post at or below the old since is not delivered
  (TestASinceAheadOfTheServerEndsAtItsLowerPosition). Unknown device, foreign session, another
  account named, other auth types and non-object auth are 500 + CheckModelled
  (TestADeviceDeleteBeyondTheModelIsUnmodelled).
- **Go rules.** Each new production change has its test in the same commit and the test files were
  added with or before `seat.go`; no `//nolint`; no discarded error; the only `time.Sleep` and
  `time.Now` uses predate this branch (`d7a439de` has the same four).

## Findings

1. **minor: the tagged test never runs the fallback the design actually uses: a claim's token
   deleting its own device.** `src-go/internal/teamchat/matrix/tuwunel_test.go:369-387`
   (`deleteAndReset`). H-C13's run recorded two delete facts (hostchecks-run.md:106 and 107): a
   device's token deleting *another* device, and C 3.7.3's fallback, a claim's token deleting *its
   own* device and being refused afterwards. `deleteAndReset` checks only the first (TCTAKER
   deletes TCSEAT); the second, the one C 3.7.3 builds the release on, is proven only against
   fakehs (`fakehs/session_test.go`, the `own` delete at the end of
   TestDeletingADeviceByPasswordEndsItsTokens; `seat_test.go` TestDeleteDeviceEndsEveryTokenOfTheDevice).
   SPEC M44 (lines 202-211) likewise names only "from another device". Fix: before the password
   reset, have `taker.DeleteDevice(ctx, "seat", password, "TCTAKER")` succeed and `r.dead(ctx, "a token that deleted its own device", taker)`; then sign a fresh device in for the reset step
   (or drop the reset's `taker` check and check a fresh token instead). Name the self-delete in M44.

2. **minor: fakehs accepts a UIA session for a request other than the one it challenged, which
   nothing recorded and the specification does not promise.** `fakehs/session.go:100-134`. A
   session is stored as `uiaSessions[session] = userID` (line 111) and accepted for any device
   path of that account (line 119), so a challenge for `DELETE /devices/TCSEAT` completes a
   `DELETE /devices/OTHER`. The specification has the session tied to the request it was issued
   for (a client must not change the request between stages; a server may refuse a changed one),
   and H-C13 recorded only same-request use. This is beyond the model and neither stated as the
   specification's shape nor unmodelled, against M39. The client never does it, so no client test
   is wrong today, but a later N2/N3 test could lean on it. Fix: store the device ID with the
   session (`uiaSessions[session] = uiaGrant{user, device}`), answer a mismatch as
   `unmodelled("a device delete's session given for another device")`, and add the case to
   TestADeviceDeleteBeyondTheModelIsUnmodelled; say so in M53's unmodelled list.

3. **minor: M51 and M53 label as recorded what the host checks did not record.**
   `SPEC-matrix.md:253-255` says the sync's `set_presence` "acts as host check HC-A1 recorded
   Tuwunel 1.9.4 (absent or `online` marks the caller online, and `unavailable` and `offline` each
   set themselves ...)"; HC-A1 recorded only the default sync (online) and `offline` (run lines
   88-92). Explicit `online` and `unavailable` are the specification's shape. `SPEC-matrix.md:271-276`
   says fakehs "models M52 as H-C13 recorded it (the statuses ...)", and then names "a wrong
   password is a 401 `M_FORBIDDEN` with the same session"; H-C13 recorded no wrong-password
   delete, so that status and errcode are the specification's, and `fakehs/session.go:91`'s text
   "Invalid username or password." is not a recorded text, against the package doc's promise
   (`fakehs.go`: "where it says what Tuwunel says, the text is the recorded text"). The tagged test
   (`tuwunel_test.go:372`) will record it on the host, which is right. Fix: in M51 split "recorded
   (default, `offline`)" from "the specification's shape (`online`, `unavailable`)"; in M53 mark
   the wrong-password answer and its text as the specification's shape until the tagged test runs;
   in `session.go` say so beside the text (or answer the specification's errcode with an `error`
   text the doc does not claim is Tuwunel's). Same in `syncPresence`'s doc comment (session.go,
   "as Tuwunel 1.9.4 does").

4. **nit: the H-C14 fakehs test does not prove the hold.**
   `fakehs/sync_test.go:89-101` uses a 5 ms timeout and asserts only the answer, so a fake that
   answered a since-ahead sync at once would also pass. The fake uses a real timer (pre-existing,
   `fakehs/sync.go:100`), so a wall-clock-free proof needs the server's held-sync signal: start the
   sync with a long timeout in a goroutine, wait until the server reports it held (as the existing
   held-sync tests do), post as `other`, confirm the sync is still held, then cancel it and read
   that it ended without rooms. If that costs more than it buys, say in the test's comment that
   the hold itself is the existing TestHeldSyncEndsEmptyAtItsTimeout's mechanism.

5. **nit: the second-logout check does not check the kind.** `tuwunel_test.go:429-431` checks
   `Errcode` but not `errors.Is(err, ErrAuthRefused)`, and prints `e` (nil when the call
   succeeded) instead of the error. Use `r.dead`'s pattern on the error value.

6. **nit: the tagged test has no presence step for HC-A1.** The client has no presence read, so
   HC-A1's offline fact is proven only in fakehs. Acceptable (Task 1.4's probe owns it, and
   receiving presence is N3's); worth one line in M44 saying presence is not in the tagged test.

## What I ran (worktree `src-go/`)

- `go test -race -shuffle=on -count=1 -cover ./internal/teamchat/...`: pass; `matrix` 98.9%,
  `fakehs` 92.7%.
- `go vet -tags tuwunel ./internal/teamchat/...`: clean.
- `golangci-lint run ./internal/teamchat/...`: 0 issues; `--build-tags tuwunel`: 0 issues.
- `git merge-base --is-ancestor 35acceab HEAD`: exit 1.
- `git diff feat/bus-v2-m1-matrix d7a439de -- src-go/internal/bus/matrix <SPEC> src-go/.golangci.yml`: empty.
- `git grep -i -E 'agent-bus|agent_bus|pingbus' HEAD -- src-go/internal/teamchat`: the three M38 references only.
- govulncheck not run (GO-2026-6809, Task 1.6, not counted). The tagged test cannot run in this
  container; it was read, vetted and linted with the tag.
