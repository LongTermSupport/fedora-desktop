# Handoff 1: Plan 00209 M1, bus/matrix and bus/matrix/fakehs

Worktree `untracked/worktrees/bus-v2-m1-matrix`, branch `feat/bus-v2-m1-matrix`. Work only there.
The lead's task text is the brief: spec first, test first, 90% coverage, `-race`, no `//nolint`, no
discarded errors, no `time.Now` in tests, an adversarial Opus review to MERGE written to
`subagent-reports/261021-m1-matrix-review.md`, the report to `subagent-reports/261021-m1-matrix.md`,
and a final message under 1500 characters.

## Committed so far

- `SPEC-matrix.md` (this plan folder): M1-M44, departures MB1-MB7. Read it first; the API names
  below are the ones it and the tests use.
- Red tests for fakehs, which do not compile yet (`undefined: Server`, seen with `go vet`). They
  are **on disk in the worktree, uncommitted**, because a commit that does not compile would fail
  qa-changed. Commit them together with the fakehs source that turns them green:
  - `src-go/internal/bus/matrix/fakehs/replay_test.go` replays the 73 recorded Tuwunel exchanges in
    `tests/helpers/pingbus/fixtures/tuwunel/` (from the package: `../../../../../tests/...`)
    through `Server.Handle`. It ports `tests/helpers/pingbus/test_fakes.py:42-192`, with the 058
    divergence.
  - `src-go/internal/bus/matrix/fakehs/fakehs_test.go` holds the rule tests: power levels, `@`
    state keys, invite and kick, the v12 create ID, transaction reuse, redact, receipts, the room
    state, `joined_rooms`, a restricted join, leave, unmodelled requests, bad bodies and tokens,
    the `MaxRequestBytes` quirk, `Inject`, and paging through messages.

## The fakehs API the tests already use

- `New(Options{ServerName, Now func() time.Time, Quirks})` and `TuwunelQuirks()`.
- `Quirks{MaxRequestBytes, AdminFalseIs500, LoginFailuresAllowed}`.
- `(*Server).Handle(ctx, method, escapedPath, url.Values, body []byte, token) (int, []byte)`.
- `AddUser(localpart, password, admin) string` and `MintToken(uid) (string, error)`.
- `RegistrationKey() string` gives the shared secret. Never name a field `.secret`: a hook blocks it.
- `CheckModelled() error` and `SetQuirks(Quirks)`.
- `Inject(Fault{Method, Path (a substring), Status, Body, Header, Drop, DropAfter, Redirect, Times})`.
- `Receipts() []Receipt{UserID, RoomID, Type, EventID}`.

Still to add, per SPEC M41-M43:

- `Start()`, `URL()`, `Stop()`, `Restart()` and `Close()`, over httptest on 127.0.0.1. A dropped
  connection uses `Hijack`.
- `Requests()`, which records the method, the escaped path, the query and the headers, but no body.

## The model to port

Port `tests/helpers/pingbus/fake_client_api.py` and `fake_admin_api.py` from branch F44
(`git show F44:<path>`) to Go, with the recorded error texts exactly. Copies of the v1 sources are
in the worktree's `untracked/scratch/v1/`.

Then add the admin calls that were never recorded but that bus/admin needs:

- `deactivated` and `locked` in `PUT v2/users`. Deactivating revokes the tokens; reactivating stays
  unmodelled.
- `GET v2/users/{id}/devices`, `/joined_rooms`, and `/kick`. A refused kick answers
  "...enough power to kick" or "...not in the room".
- The login-failure 429 quirk, whose count `Restart` empties.

The static answers (versions, server version, login flows) embed copies of the fixtures'
responses. A room or event ID is `!` or `$` followed by 43 characters of base64url of
sha256(salt + counter). The create event's ID is `$` + room[1:].

## Left to do

1. The fakehs sources: `fakehs.go`, `rooms.go`, `sync.go`, `admin.go` and `http.go`. The TDD hook
   needs each one's `_test.go` first. Still to write are:
   - `sync_test.go`: a held sync that wakes, one that times out, one whose connection closes; a
     limited timeline with its gap; the stripped state of an invite; lazy members.
   - `admin_test.go`.
   - `http_test.go`: the faults, restart, a refused connection, a redirect target that receives
     nothing, the recording.
2. `src-go/internal/bus/matrix`: the tests, then the code, for SPEC M1-M36:
   - `Client`, `NewMember` and `NewAdmin`;
   - `Error` with its kinds, sentinels and `ExitCode`;
   - `Clock`, the 429 handling, and the retry of a send with the same transaction ID;
   - `Sync` with `RoomFilter`, and the member and admin endpoints;
   - path quoting as Python's `quote(safe="")` does it.
     The tests run the client against fakehs over real loopback HTTP.
3. Two exclusions in `.golangci.yml`, each with its reason:
   - tagliatelle for `internal/bus/matrix/`, because Matrix's wire format is snake_case;
   - gosec G401 and G505 for the registration MAC, which the protocol makes HMAC-SHA1.
4. The `tuwunel` build-tagged test (M44), which skips when `FAKEHS_TUWUNEL_BIN` is not set.
5. qa-changed, the commits, the report with the red and green evidence, and the Opus review loop
   until MERGE.

## Notes for the report

- The branch base is 147 commits behind F44. The v1 Python was read from F44; nothing was merged.
- Coupling: the room and event ID grammar belongs to bus/proto (MB1), so matrix checks only that a
  segment is safe to send.
- No `/relations` endpoint (MB6): Plan 00211 needs none.
